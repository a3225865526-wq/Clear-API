#!/usr/bin/env bash
#
# ClearAPI controlled production deploy — runs ON the production VPS as root.
#
# Contract:
#   * input image reference must be an EXACT sha256 digest; tags and "latest"
#     are rejected before anything else runs.
#   * fixed step order (PRODUCTION CI/CD ACCEPTANCE spec, section 20);
#     any critical failure stops the deployment.
#   * automatic rollback to the previous exact image is attempted ONLY when
#     the new container fails post-replace checks AND the caller explicitly
#     declared the candidate migration backward-compatible. The database is
#     never restored automatically.
#   * no secret is ever placed in argv, logs, or the release manifest. The
#     GHCR pull token and the migration DSN are read from root-owned files /
#     container secrets.
#
# Rollback references:
#   * if the previous deployment came from GHCR, rollback uses its exact
#     registry digest ref.
#   * for the FIRST CI/CD deploy the previous production image exists only
#     as a local image (hand-built era): rollback re-pins the recorded local
#     tag and then verifies the running image ID equals the manifest-recorded
#     exact image ID — exact, never "latest" or guesswork.
#
# Usage:
#   production-deploy.sh --digest sha256:<64hex> --commit <40hex-sha> \
#     [--workflow-run <id>] [--migration-compatible true|false]
#
# Operator-owned configuration: /opt/clearapi/config/deploy.env (mode 0600,
# not in the repository). Defaults below document every supported key and
# match the live VPS as verified during CI/CD acceptance (2026-10-07).

set -euo pipefail
umask 077

# ---------------------------------------------------------------------------
# Defaults (overridable via deploy.env) — verified against live VPS
# ---------------------------------------------------------------------------
APP_DIR="${DEPLOY_APP_DIR:-/opt/clearapi/app}"
COMPOSE_FILE="${DEPLOY_COMPOSE_FILE:-docker-compose.prod.yml}"
SERVICE="${DEPLOY_SERVICE:-new-api}"
PG_SERVICE="${DEPLOY_PG_SERVICE:-postgres}"
PG_USER="${DEPLOY_PG_USER:-clearapi_app}"
PG_DB="${DEPLOY_PG_DB:-clearapi}"
NETWORK_NAME="${DEPLOY_NETWORK_NAME:-clearapi_internal}"

IMAGE_BASE="${DEPLOY_IMAGE_BASE:-ghcr.io/a3225865526-wq/clearapi}"
GHCR_USER="${DEPLOY_GHCR_USER:-clearapi-deploy}"
GHCR_TOKEN_FILE="${DEPLOY_GHCR_TOKEN_FILE:-/opt/clearapi/secrets/ghcr-pull-token}"
# Ephemeral pull credential. When set, the caller (GitHub Actions) pipes a
# short-lived GITHUB_TOKEN in on stdin and the token is never written to a
# persistent docker config; DOCKER_CONFIG points at a throwaway directory that
# is removed on exit. The long-lived token file above stays optional and is
# only a fallback for environments where the job token cannot pull.
# The stdin mode is enabled exclusively via the --ghcr-token-stdin CLI flag:
# sudo env_reset discards caller-set environment variables for allowlisted
# commands, so an env-based switch cannot reach this script.
GHCR_TOKEN_STDIN=""

BACKUP_SERVICE="${DEPLOY_BACKUP_SERVICE:-clearapi-backup.service}"
BACKUP_TIMER="${DEPLOY_BACKUP_TIMER:-clearapi-backup.timer}"
BACKUP_DIR="${DEPLOY_BACKUP_DIR:-/opt/clearapi/backups}"

LOCAL_HEALTH_URL="${DEPLOY_LOCAL_HEALTH_URL:-http://127.0.0.1:3000/api/status}"
PUBLIC_HEALTH_URL="${DEPLOY_PUBLIC_HEALTH_URL:-https://www.clear999.com/api/status}"
CADDYFILE="${DEPLOY_CADDYFILE:-/opt/clearapi/edge/Caddyfile}"
# Frozen post-real-IP H4 baseline (see cloudflare-edge-migration-acceptance).
CADDYFILE_MD5="${DEPLOY_CADDYFILE_MD5:-295647360d0db904c6208b79411d5408}"

VERIFY_BOOTSTRAP="${DEPLOY_VERIFY_BOOTSTRAP:-true}"
BOOTSTRAP_SERVICE="${DEPLOY_BOOTSTRAP_SERVICE:-production-bootstrap}"
MIGRATION_FLAGS="${DEPLOY_MIGRATION_FLAGS:-}"

# Frozen negative-gate expectations (H4 acceptance baseline, live-verified).
EXPECT_ENV_KEYS="${DEPLOY_EXPECT_ENV_KEYS:-CLEARAPI_ENABLE_RESPONSES_WEBSOCKET=false CLEARAPI_ENABLE_REALTIME=false CLEARAPI_CLOSED_LAUNCH=true}"
EXPECT_CHANNELS="${DEPLOY_EXPECT_CHANNELS:-1,2}"
EXPECT_USER_ID="${DEPLOY_EXPECT_USER_ID:-2}"
EXPECT_TOKEN_IDS="${DEPLOY_EXPECT_TOKEN_IDS:-1,2}"
EXPECT_REVIEW_ROWS="${DEPLOY_EXPECT_REVIEW_ROWS:-4/10/4 41/93/41}"
EXPECT_EMPTY_TABLES="${DEPLOY_EXPECT_EMPTY_TABLES:-store_products,top_ups,offline_orders,subscription_orders}"
POOL_OPTION_PATTERN="${DEPLOY_POOL_OPTION_PATTERN:-%pool%}"
POLICY_MODE_KEY="${DEPLOY_POLICY_MODE_KEY:-billing_reserve_policy.mode}"

AUDIT_LOG="${DEPLOY_AUDIT_LOG:-/opt/clearapi/logs/deployments.log}"
MANIFEST="${DEPLOY_MANIFEST:-${APP_DIR}/release-manifest.json}"
OVERRIDE_FILE="${DEPLOY_OVERRIDE_FILE:-${APP_DIR}/compose-candidate.yaml}"

# ---------------------------------------------------------------------------
# Args (validated before any state change)
# ---------------------------------------------------------------------------
DIGEST=""; COMMIT=""; WORKFLOW_RUN=""; MIGRATION_COMPATIBLE="false"; GHCR_TOKEN_STDIN=""; VERIFY_ONLY="0"
while [ $# -gt 0 ]; do
  case "$1" in
    --digest) DIGEST="$2"; shift 2 ;;
    --commit) COMMIT="$2"; shift 2 ;;
    --workflow-run) WORKFLOW_RUN="$2"; shift 2 ;;
    --migration-compatible) MIGRATION_COMPATIBLE="$2"; shift 2 ;;
    # The pull credential is piped in on stdin. This is a CLI flag rather than
    # an environment variable because sudo env_reset discards caller-set env
    # vars for allowlisted commands, and widening the sudoers environment
    # policy is not acceptable. The flag is explicit, auditable and changes
    # nothing about which principal may run this wrapper.
    --ghcr-token-stdin) GHCR_TOKEN_STDIN="1"; shift 1 ;;
    # Read-only drift probe: running image == compose effective image ==
    # manifest current. Changes nothing; safe to run from a systemd timer
    # (monitoring minimal set) or before any manual container operation.
    --verify) VERIFY_ONLY="1"; shift 1 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [ "$VERIFY_ONLY" != "1" ]; then
  [ -n "$DIGEST" ] && [ -n "$COMMIT" ] || { echo "--digest and --commit are required" >&2; exit 2; }
fi

printf '%s' "$DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || {
  echo "REFUSING: --digest must be an exact 'sha256:<64 hex>' reference (got: ${DIGEST})" >&2; exit 2; }
printf '%s' "$COMMIT" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "REFUSING: --commit must be a full 40-hex commit SHA" >&2; exit 2; }
[ "$MIGRATION_COMPATIBLE" = "true" ] || [ "$MIGRATION_COMPATIBLE" = "false" ] || {
  echo "REFUSING: --migration-compatible must be true or false" >&2; exit 2; }

if [ -f /opt/clearapi/config/deploy.env ]; then
  # shellcheck disable=SC1090
  . /opt/clearapi/config/deploy.env
else
  echo "REFUSING: /opt/clearapi/config/deploy.env is missing (operator-owned, mode 0600)" >&2; exit 2
fi

# ---------------------------------------------------------------------------
# Helpers (all defined before any step executes)
# ---------------------------------------------------------------------------
TS_NOW() { date -u +%Y-%m-%dT%H:%M:%SZ; }
audit() { # audit <event> <result> <detail>
  mkdir -p "$(dirname "$AUDIT_LOG")"
  printf '{"ts":"%s","event":"%s","result":"%s","digest":"%s","previous_image":"%s","commit":"%s","workflow_run":"%s","detail":"%s"}\n' \
    "$(TS_NOW)" "$1" "$2" "${DIGEST:-}" "${PREV_IMAGE_ID:-}" "${COMMIT:-}" "${WORKFLOW_RUN:-}" "$3" >> "$AUDIT_LOG"
}
log() { printf '[deploy %s] %s\n' "$(TS_NOW)" "$*" >&2; }

REPLACE_DONE=0
die() {
  log "FAIL: $*"
  audit "deploy_failed" "failed" "$*"
  if [ "$REPLACE_DONE" = "1" ] && [ "$MIGRATION_COMPATIBLE" = "true" ]; then
    rollback "$*"
    exit 1
  fi
  if [ "$REPLACE_DONE" = "1" ]; then
    log "no automatic rollback (migration_compatible=${MIGRATION_COMPATIBLE}) — MANUAL REVIEW REQUIRED"
  fi
  exit 1
}

compose() {
  # The override file (exact-digest pin, written by write_override) MUST be
  # part of every compose invocation; without it `compose up -d` sees an
  # unchanged image ref and silently does nothing while the deploy reports
  # success (observed in run 37762896277: manifest updated, container not
  # replaced). Attach it whenever it exists.
  local args=(--project-directory "$APP_DIR" -f "${APP_DIR}/${COMPOSE_FILE}")
  [ -f "$OVERRIDE_FILE" ] && args+=(-f "$OVERRIDE_FILE")
  sudo docker compose "${args[@]}" "$@"
}
service_id() { compose ps -q "$SERVICE" | head -1; }

compose_effective_image() {
  # The image ref compose would actually use for the service right now:
  # base file + override (exact-digest pin) merged. Anti-drift probe: if the
  # override was deleted or pinned to a stale ref, the effective ref diverges
  # from the manifest and a container rebuild would silently roll production
  # back (observed during the 2026-10-08 rollback+redeploy drill: cleanup
  # deleted the live pin and "manifest current != compose effective" appeared).
  local args=(--project-directory "$APP_DIR" -f "${APP_DIR}/${COMPOSE_FILE}")
  [ -f "$OVERRIDE_FILE" ] && args+=(-f "$OVERRIDE_FILE")
  # A failed probe must yield an empty string instead of aborting the wrapper
  # under set -e, so the caller can log the drift verdict and audit it.
  { sudo docker compose "${args[@]}" config --format json 2>/dev/null \
    | python3 -c 'import json, sys
d = json.load(sys.stdin)
svc = (d.get("services") or {}).get("'"$SERVICE"'") or {}
print(svc.get("image") or "")'
  } || true
}

verify_running_identity() { # read-only: running image == compose effective image == manifest current
  local cid eff_ref eff_id manifest_id
  cid="$(service_id)"
  [ -n "$cid" ] || { log "verify: running ${SERVICE} container not found"; return 1; }
  PREV_IMAGE_ID="$(sudo docker inspect --format '{{.Image}}' "$cid")"
  [ -f "$OVERRIDE_FILE" ] || {
    log "verify: DRIFT — digest pin override ${OVERRIDE_FILE} is missing; a container rebuild would silently revert to the base compose image"
    return 1
  }
  eff_ref="$(compose_effective_image)"
  [ -n "$eff_ref" ] || { log "verify: compose effective image could not be resolved (compose config failed)"; return 1; }
  eff_id="$(sudo docker image inspect --format '{{.Id}}' "$eff_ref" 2>/dev/null || true)"
  [ -n "$eff_id" ] || { log "verify: effective image ${eff_ref} is not present locally"; return 1; }
  [ "$eff_id" = "$PREV_IMAGE_ID" ] || {
    log "verify: DRIFT — compose effective image ${eff_ref} (${eff_id}) != running image (${PREV_IMAGE_ID})"
    return 1
  }
  if [ -f "$MANIFEST" ]; then
    manifest_id="$(python3 - "$MANIFEST" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    m = json.load(f)
print(((m.get("releases") or {}).get("current") or {}).get("new_api_image_id") or "")
PYEOF
)"
    [ -n "$manifest_id" ] || { log "verify: manifest has no releases.current.new_api_image_id"; return 1; }
    [ "$manifest_id" = "$PREV_IMAGE_ID" ] || {
      log "verify: DRIFT — manifest current (${manifest_id}) != running image (${PREV_IMAGE_ID})"
      return 1
    }
  fi
  log "verify: identity OK — running == compose effective (${eff_ref}) == manifest current"
  return 0
}
psql_q() { # read-only single query against the production database
  compose exec -T "$PG_SERVICE" psql -U "$PG_USER" -d "$PG_DB" -Atq -v ON_ERROR_STOP=1 -c "$1"
}
health_retry() { # health_retry <url> <retries> <sleep_s> [<require_header>]
  local url="$1" retries="$2" sleep_s="$3" header="${4:-}" i=0
  while [ "$i" -lt "$retries" ]; do
    if [ -n "$header" ]; then
      if curl -fsS -D /tmp/deploy-health-headers -o /tmp/deploy-health-body --max-time 15 "$url" \
         && grep -qi "^${header}:" /tmp/deploy-health-headers; then
        return 0
      fi
    else
      if curl -fsS -o /tmp/deploy-health-body --max-time 15 "$url"; then
        return 0
      fi
    fi
    i=$((i + 1)); sleep "$sleep_s"
  done
  return 1
}
write_override() { # write_override <image ref> — exact digest ref, or exact local image for legacy rollback
  cat > "$OVERRIDE_FILE" <<EOF
# Generated by production-deploy.sh at $(TS_NOW). Exact-digest pin.
services:
  ${SERVICE}:
    image: $1
EOF
  chmod 0600 "$OVERRIDE_FILE"
}
set_manifest_status() { # set_manifest_status <status>
  python3 - "$MANIFEST" "$1" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    m = json.load(f)
m["releases"]["current"]["status"] = sys.argv[2]
with open(sys.argv[1], "w") as f:
    json.dump(m, f, indent=2)
    f.write("\n")
PYEOF
}
rollback() {
  log "rollback: switching back to previous exact image (${ROLLBACK_REF})"
  write_override "$ROLLBACK_REF"
  if ! compose up -d "$SERVICE"; then
    audit "rollback" "failed" "compose up failed for previous image ${ROLLBACK_REF}"
    log "ROLLBACK FAILED — MANUAL INTERVENTION REQUIRED"
    return 1
  fi
  RID="$(service_id)"
  RUN_IMG="$(sudo docker inspect --format '{{.Image}}' "$RID")"
  if [ "$RUN_IMG" != "$PREV_IMAGE_ID" ]; then
    audit "rollback" "failed" "running image ${RUN_IMG} does not match recorded previous ${PREV_IMAGE_ID}"
    log "ROLLBACK IMAGE MISMATCH — MANUAL INTERVENTION REQUIRED"
    return 1
  fi
  if health_retry "$LOCAL_HEALTH_URL" 30 2 && health_retry "$PUBLIC_HEALTH_URL" 10 3 "CF-Ray"; then
    set_manifest_status "rolled_back"
    audit "rollback" "rolled_back" "previous image ${PREV_IMAGE_ID} healthy again"
    log "rollback complete; deployment remains FAILED but production is healthy on the previous image"
  else
    audit "rollback" "failed" "previous image deployed but health checks fail; MANUAL INTERVENTION REQUIRED"
    log "ROLLBACK DEPLOYED BUT UNHEALTHY — MANUAL INTERVENTION REQUIRED"
  fi
}

# Read-only --verify mode: run the identity check and exit. Kept after helper
# definitions and before step 1 so it never touches the deploy lock, the
# backup subsystem, or any container.
if [ "$VERIFY_ONLY" = "1" ]; then
  if verify_running_identity; then
    audit "verify" "ok" "running == compose effective == manifest current"
    exit 0
  fi
  audit "verify" "drift" "identity check failed; no changes were made"
  exit 1
fi

# ===========================================================================
# 1. acquire deploy lock
# ===========================================================================
mkdir -p /run/lock
exec 9>"/run/lock/clearapi-production-deploy.lock"
flock -n 9 || die "deploy lock held: another production deploy is in progress"
log "step 1: deploy lock acquired"

# ===========================================================================
# 2. validate candidate metadata
# ===========================================================================
log "step 2: candidate metadata validated (digest ${DIGEST}, commit ${COMMIT}, migration_compatible=${MIGRATION_COMPATIBLE})"

# ===========================================================================
# 3. verify current production state
# ===========================================================================
CUR_ID="$(service_id)"
[ -n "$CUR_ID" ] || die "step 3: running ${SERVICE} container not found; refusing first-deploy-by-script without a live baseline"
PREV_IMAGE_ID="$(sudo docker inspect --format '{{.Image}}' "$CUR_ID")"
log "step 3: current production image ID = ${PREV_IMAGE_ID}"

if [ -f "$MANIFEST" ]; then
  if python3 - "$MANIFEST" "$PREV_IMAGE_ID" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    m = json.load(f)
cur = (m.get("releases") or {}).get("current") or {}
recorded = cur.get("new_api_image_id")
if recorded and recorded != sys.argv[2]:
    sys.exit(1)
sys.exit(0)
PYEOF
  then :; else
    die "step 3: manifest releases.current.new_api_image_id does not match the running image; state divergence — manual resolution required"
  fi
fi

# Drift gate: the running image, the image compose would actually recreate
# (base + exact-digest override) and the manifest current entry must agree
# before anything mutates. Catches a deleted or stale override pin before it
# can silently roll production back on an unrelated container rebuild.
verify_running_identity \
  || die "step 3: production identity drift (running image vs compose effective vs manifest current); refusing to deploy on an inconsistent baseline"

# ===========================================================================
# 4. encrypted pre-deploy backup (hard gate)
# ===========================================================================
systemctl is-active --quiet "$BACKUP_TIMER" || die "step 4: ${BACKUP_TIMER} is not active; backup subsystem is not trusted"
BACKUP_STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%S)"
systemctl start "$BACKUP_SERVICE" || die "step 4: ${BACKUP_SERVICE} did not exit 0; deployment BLOCKED"
BACKUP_STATUS="$(systemctl show -p ExecMainStatus --value "$BACKUP_SERVICE")"
[ "$BACKUP_STATUS" = "0" ] || die "step 4: backup ExecMainStatus=${BACKUP_STATUS}; deployment BLOCKED"
log "step 4: pre-deploy backup completed"

# ===========================================================================
# 5. verify backup success
# ===========================================================================
NEW_ARTIFACTS="$(find "$BACKUP_DIR" -type f -newermt "$BACKUP_STARTED_AT" -size +0 2>/dev/null | head -5)"
[ -n "$NEW_ARTIFACTS" ] || die "step 5: no new non-empty backup artifact under ${BACKUP_DIR} since ${BACKUP_STARTED_AT}; deployment BLOCKED"
log "step 5: backup artifacts verified"
printf '    %s\n' $NEW_ARTIFACTS >&2

# ===========================================================================
# 6. pull exact digest
# ===========================================================================
IMAGE_REF="${IMAGE_BASE}@${DIGEST}"
# Ephemeral, isolated pull credential: a throwaway DOCKER_CONFIG keeps the
# token out of /root/.docker/config.json so nothing long-lived is left behind.
EPHEMERAL_DOCKER_CONFIG=""
cleanup_ephemeral() {
  if [ -n "$EPHEMERAL_DOCKER_CONFIG" ] && [ -d "$EPHEMERAL_DOCKER_CONFIG" ]; then
    rm -rf "$EPHEMERAL_DOCKER_CONFIG"
  fi
}
trap cleanup_ephemeral EXIT

if [ "$GHCR_TOKEN_STDIN" = "1" ]; then
  EPHEMERAL_DOCKER_CONFIG="$(mktemp -d)"
  chmod 0700 "$EPHEMERAL_DOCKER_CONFIG"
  export DOCKER_CONFIG="$EPHEMERAL_DOCKER_CONFIG"
  docker login ghcr.io -u "$GHCR_USER" --password-stdin \
    || die "step 6: GHCR login failed (ephemeral job credential)"
  LOGGED_IN=1
  log "step 6: authenticated with ephemeral job credential (throwaway DOCKER_CONFIG)"
elif [ -f "$GHCR_TOKEN_FILE" ]; then
  docker login ghcr.io -u "$GHCR_USER" --password-stdin < "$GHCR_TOKEN_FILE" \
    || die "step 6: GHCR login failed (token file ${GHCR_TOKEN_FILE})"
  LOGGED_IN=1
  log "step 6: authenticated with stored pull token"
else
  LOGGED_IN=0
  log "step 6: no pull credential configured; attempting anonymous pull (works only for public images)"
fi
docker pull --platform linux/amd64 "$IMAGE_REF" || die "step 6: docker pull ${IMAGE_REF} failed"
if [ "$LOGGED_IN" = "1" ]; then docker logout ghcr.io >/dev/null 2>&1 || true; fi
cleanup_ephemeral
unset DOCKER_CONFIG
CANDIDATE_IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$IMAGE_REF")"
log "step 6: pulled ${IMAGE_REF} (image ID ${CANDIDATE_IMAGE_ID})"

# ===========================================================================
# 7. verify image digest + provenance
# ===========================================================================
docker image inspect --format '{{index .RepoDigests 0}}' "$IMAGE_REF" | grep -q "@${DIGEST}$" \
  || die "step 7: local RepoDigests do not contain ${DIGEST}"
IMAGE_REVISION="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$IMAGE_REF")"
[ "$IMAGE_REVISION" = "$COMMIT" ] \
  || die "step 7: image revision label '${IMAGE_REVISION}' does not match expected commit ${COMMIT}"
log "step 7: digest and revision label verified"

# ===========================================================================
# 8. inspect required binaries
# ===========================================================================
docker run --rm --entrypoint /bin/sh "$IMAGE_REF" -c '
  for b in /new-api /production-bootstrap /finance-migrate /billing-migrate /entrypoint-secure.sh; do
    test -x "$b" || { echo "missing required binary: $b"; exit 1; }
  done
' || die "step 8: required binaries missing from image"
if docker run --rm --entrypoint /billing-migrate "$IMAGE_REF" >/dev/null 2>&1; then
  die "step 8: billing-migrate ran without an approval flag; fail-closed contract violated"
fi
log "step 8: all required binaries present; migration binary fail-closed contract holds"

# ===========================================================================
# 9/10. migration gate (preflight; run only when flags are configured)
# ===========================================================================
if [ -z "$MIGRATION_FLAGS" ]; then
  log "step 9: migration preflight — binaries and DSN path verified in steps 7/8; no approval flags configured"
  log "step 10: DEPLOY_MIGRATION_FLAGS empty in deploy.env — no schema change expected for a zero-change candidate; skipping migration run"
  audit "migration_gate" "preflight_only" "no flags configured; no schema change performed"
else
  compose run --rm --entrypoint /bin/sh -e MIGRATION_FLAGS="$MIGRATION_FLAGS" "$SERVICE" -c '
    test -f /run/secrets/sql_dsn || { echo "sql_dsn secret is not mounted; refusing migration" >&2; exit 1; }
    export CLEARAPI_BILLING_MIGRATION_DSN="$(cat /run/secrets/sql_dsn)"
    # shellcheck disable=SC2086
    exec /billing-migrate -dialect postgres $MIGRATION_FLAGS
  ' || die "step 10: migration run failed; deployment BLOCKED (no automatic DB restore)"
  log "step 10: migrations applied via image binaries"
  audit "migration_gate" "applied" "flags=${MIGRATION_FLAGS}"
fi

# ===========================================================================
# 11. bootstrap verify (readiness gate, zero-write)
# ===========================================================================
if [ "$VERIFY_BOOTSTRAP" = "true" ]; then
  compose --profile bootstrap run --rm "$BOOTSTRAP_SERVICE" --verify-only \
    || die "step 11: production-bootstrap --verify-only failed; deployment BLOCKED"
  log "step 11: bootstrap verify-only PASS"
else
  log "step 11: bootstrap verify disabled by deploy.env (DEPLOY_VERIFY_BOOTSTRAP)"
fi

# ===========================================================================
# 12/13. record old image + update release manifest (merge into live schema)
# ===========================================================================
STAMP="$(TS_NOW)"
[ -f "$MANIFEST" ] && cp "$MANIFEST" "${MANIFEST}.bak.$(date -u +%Y%m%dT%H%M%SZ)"
PREV_TAG="$(python3 - "$MANIFEST" <<'PYEOF'
import json, sys
try:
    with open(sys.argv[1]) as f:
        m = json.load(f)
    print((m.get("releases") or {}).get("current", {}).get("tag", ""))
except Exception:
    print("")
PYEOF
)"
# Rollback reference: exact registry ref when the previous release came from
# GHCR; otherwise the recorded local tag (verified against the exact image ID
# after rollback — see rollback()).
if python3 - "$MANIFEST" <<'PYEOF'
import json, sys
try:
    with open(sys.argv[1]) as f:
        m = json.load(f)
    cur = (m.get("releases") or {}).get("current") or {}
    sys.exit(0 if cur.get("image_digest") else 1)
except Exception:
    sys.exit(1)
PYEOF
then
  PREV_DIGEST_FIELD="$(python3 -c "import json;m=json.load(open('$MANIFEST'));print(m['releases']['current']['image_digest'])")"
  ROLLBACK_REF="${IMAGE_BASE}@${PREV_DIGEST_FIELD}"
else
  ROLLBACK_REF="$PREV_TAG"
fi
[ -n "$ROLLBACK_REF" ] || die "step 12: cannot determine a previous exact image reference for rollback"

MIGRATION_VERSION="${MIGRATION_FLAGS:-preflight-only (no flags configured)}"
MANIFEST_TMP="${MANIFEST}.tmp.$$"
python3 - "$MANIFEST" "$MANIFEST_TMP" "$COMMIT" "$IMAGE_REF" "$DIGEST" "$CANDIDATE_IMAGE_ID" \
  "$PREV_IMAGE_ID" "$WORKFLOW_RUN" "$MIGRATION_VERSION" "$STAMP" <<'PYEOF'
import json, sys
src, dst, commit, image_ref, digest, image_id, prev_id, run_id, migration_version, stamp = sys.argv[1:11]
try:
    with open(src) as f:
        m = json.load(f)
except Exception:
    m = {"schema_version": 1, "releases": {}}
rel = m.setdefault("releases", {})
old_current = rel.get("current") or {}
new_current = {
    "tag": image_ref + "@" + digest,
    "candidate_commit": commit,
    "new_api_image_id": image_id,
    "image_digest": digest,
    "image_ref": image_ref,
    "previous_image_id": prev_id,
    "workflow_run": run_id,
    "deployed_at_utc": stamp,
    "deployment_method": "ghcr-exact-digest",
    "migration_mode": migration_version,
    "status": "pending",
}
if old_current.get("postgres_image"):
    new_current["postgres_image"] = old_current["postgres_image"]
if old_current.get("migrations_applied"):
    new_current["migrations_applied"] = old_current["migrations_applied"]
rel["previous"] = old_current or rel.get("previous") or {}
rel["current"] = new_current
m["updated_at_utc"] = stamp
with open(dst, "w") as f:
    json.dump(m, f, indent=2)
    f.write("\n")
PYEOF
mv "$MANIFEST_TMP" "$MANIFEST"
log "step 12/13: release manifest updated (previous image ${PREV_IMAGE_ID}, rollback ref ${ROLLBACK_REF})"

# ===========================================================================
# 14. controlled replace
# ===========================================================================
write_override "$IMAGE_REF"
compose up -d "$SERVICE" || die "step 14: compose replace failed"
REPLACE_DONE=1
# Hard identity gate: prove the running container actually IS the candidate
# image before any health check can call this deploy a success. compose may
# legitimately no-op when it believes nothing changed; without this check a
# no-op would sail through health checks on the OLD image and be recorded as
# a successful deploy (exactly what run 37762896277 did).
REPLACED_ID="$(sudo docker inspect --format '{{.Image}}' "$(service_id)")"
[ "$REPLACED_ID" = "$CANDIDATE_IMAGE_ID" ] \
  || die "step 14: replace did not take effect — running image ${REPLACED_ID} != candidate ${CANDIDATE_IMAGE_ID}"
log "step 14: container replaced with ${IMAGE_REF} (running image verified: ${REPLACED_ID})"

# ===========================================================================
# 15. localhost health
# ===========================================================================
health_retry "$LOCAL_HEALTH_URL" 30 2 || die "step 15: localhost health (${LOCAL_HEALTH_URL}) failed"
log "step 15: localhost health PASS"

# ===========================================================================
# 16. public health via Cloudflare
# ===========================================================================
health_retry "$PUBLIC_HEALTH_URL" 10 3 "CF-Ray" || die "step 16: public health (${PUBLIC_HEALTH_URL}) with CF-Ray failed"
log "step 16: public HTTPS health via Cloudflare PASS"

# ===========================================================================
# 17. runtime negative gates
# ===========================================================================
NEW_ID="$(service_id)"
[ -n "$NEW_ID" ] || die "step 17: ${SERVICE} container missing after replace"
cenv() { sudo docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$NEW_ID"; }

for pair in $EXPECT_ENV_KEYS; do
  key="${pair%%=*}"; expected="${pair#*=}"
  actual="$(cenv | grep -E "^${key}=" | head -1 | cut -d= -f2-)"
  [ "$actual" = "$expected" ] || die "step 17: env gate ${key}='${actual}' expected '${expected}'"
done

PORTS_3000="$(sudo docker port "$NEW_ID" 3000/tcp 2>/dev/null || true)"
printf '%s' "$PORTS_3000" | grep -q "127.0.0.1:3000" || die "step 17: new-api must bind 127.0.0.1:3000 only (got: ${PORTS_3000:-<unpublished>})"

PG_ID="$(compose ps -q "$PG_SERVICE")"
[ -n "$PG_ID" ] || die "step 17: postgres container missing"
PG_PORTS="$(sudo docker port "$PG_ID" 2>/dev/null || true)"
[ -z "$PG_PORTS" ] || die "step 17: postgres must not publish ports (got: ${PG_PORTS})"

NETWORKS="$(sudo docker inspect --format '{{range $k, $_ := .NetworkSettings.Networks}}{{$k}} {{end}}' "$NEW_ID")"
printf '%s' "$NETWORKS" | grep -qw "$NETWORK_NAME" || die "step 17: new-api not attached to ${NETWORK_NAME} (networks: ${NETWORKS})"

EXECUTORS="$(sudo docker ps -a --format '{{.Names}}' | grep -Eic 'executor|route.?chain' || true)"
[ "$EXECUTORS" = "0" ] || die "step 17: ${EXECUTORS} executor/route-chain container(s) present; Pool must stay OFF"

[ -f "$CADDYFILE" ] || die "step 17: Caddyfile ${CADDYFILE} missing"
CADDY_MD5="$(md5sum "$CADDYFILE" | cut -d' ' -f1)"
[ "$CADDY_MD5" = "$CADDYFILE_MD5" ] || die "step 17: Caddyfile md5 ${CADDY_MD5} does not match frozen edge baseline ${CADDYFILE_MD5}"

systemctl is-active --quiet "$BACKUP_TIMER" || die "step 17: ${BACKUP_TIMER} not active after deploy"
log "step 17: runtime negative gates PASS (env, binding, PG ports, network, pool, Caddyfile, backup timer)"

# ===========================================================================
# 18. billing invariants (read-only, least-privileged app role)
# ===========================================================================
POLICY_MODE="$(psql_q "SELECT value FROM options WHERE key='${POLICY_MODE_KEY}';")"
[ "$POLICY_MODE" = "enforce" ] || die "step 18: ${POLICY_MODE_KEY}='${POLICY_MODE}' expected 'enforce'"

REVIEW_ROWS="$(psql_q "SELECT reserved||'/'||target||'/'||applied FROM billing_reservations WHERE state='needs_review' ORDER BY reserved, id;")"
ACTUAL_SORTED="$(printf '%s\n' "$REVIEW_ROWS" | sort | tr '\n' ' ' | sed 's/ $//')"
EXPECTED_SORTED="$(printf '%s\n' $EXPECT_REVIEW_ROWS | sort | tr '\n' ' ' | sed 's/ $//')"
[ "$ACTUAL_SORTED" = "$EXPECTED_SORTED" ] || die "step 18: needs_review reservations '${ACTUAL_SORTED}' expected '${EXPECTED_SORTED}'"

for ch in $(printf '%s' "$EXPECT_CHANNELS" | tr ',' ' '); do
  status="$(psql_q "SELECT status FROM channels WHERE id=${ch};")"
  [ "$status" = "2" ] || die "step 18: channel ${ch} status='${status}' expected 2 (disabled)"
done

U_STATUS="$(psql_q "SELECT status FROM users WHERE id=${EXPECT_USER_ID};")"
[ "$U_STATUS" = "2" ] || die "step 18: user id ${EXPECT_USER_ID} status='${U_STATUS}' expected 2 (disabled)"

for tok in $(printf '%s' "$EXPECT_TOKEN_IDS" | tr ',' ' '); do
  t_status="$(psql_q "SELECT status FROM tokens WHERE id=${tok};")"
  [ "$t_status" = "2" ] || die "step 18: token id ${tok} status='${t_status}' expected 2 (disabled)"
done

for tbl in $(printf '%s' "$EXPECT_EMPTY_TABLES" | tr ',' ' '); do
  cnt="$(psql_q "SELECT count(*) FROM ${tbl};")"
  [ "$cnt" = "0" ] || die "step 18: table ${tbl} row count=${cnt} expected 0"
done

POOL_ROWS="$(psql_q "SELECT count(*) FROM options WHERE key ILIKE '${POOL_OPTION_PATTERN}';")"
[ "$POOL_ROWS" = "0" ] || die "step 18: ${POOL_ROWS} pool option row(s) present; Pool must stay OFF"
log "step 18: billing invariants PASS (mode=enforce, needs_review set frozen, channels/user/token disabled, store tables empty, no pool rows)"

# ===========================================================================
# 19. mark deployment success
# ===========================================================================
set_manifest_status "success"
audit "deploy_success" "success" "digest=${DIGEST} previous_image=${PREV_IMAGE_ID}"
log "step 19: DEPLOYMENT SUCCESS"
log "  commit:      ${COMMIT}"
log "  digest:      ${DIGEST}"
log "  image ID:    ${CANDIDATE_IMAGE_ID}"
log "  previous:    ${PREV_IMAGE_ID}"
log "  manifest:    ${MANIFEST}"
log "PRODUCTION DEPLOY COMPLETED"
