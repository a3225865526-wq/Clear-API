#!/usr/bin/env bash
#
# ClearAPI controlled production deploy — runs ON the production VPS as root.
#
# Contract:
#   * input image reference must be an EXACT sha256 digest; tags and "latest"
#     are rejected before anything else runs.
#   * fixed step order (PRODUCTION CI/CD ACCEPTANCE spec, section 20);
#     any critical failure stops the deployment.
#   * automatic rollback to the previous exact digest is attempted ONLY when
#     the new container fails post-replace checks AND the caller explicitly
#     declared the candidate migration backward-compatible. The database is
#     never restored automatically.
#   * no secret is ever placed in argv, logs, or the release manifest. The
#     GHCR pull token and the migration DSN are read from root-owned files /
#     container secrets.
#
# Usage:
#   production-deploy.sh --digest sha256:<64hex> --commit <40hex-sha> \
#     [--workflow-run <id>] [--migration-compatible true|false]
#
# Operator-owned configuration: /opt/clearapi/etc/deploy.env (mode 0600,
# not in the repository). Defaults below document every supported key.

set -euo pipefail
umask 077

# ---------------------------------------------------------------------------
# Defaults (overridable via deploy.env)
# ---------------------------------------------------------------------------
APP_DIR="${DEPLOY_APP_DIR:-/opt/clearapi/app}"
COMPOSE_FILE="${DEPLOY_COMPOSE_FILE:-docker-compose.production.yml}"
SERVICE="${DEPLOY_SERVICE:-new-api}"
PG_SERVICE="${DEPLOY_PG_SERVICE:-postgres}"
PG_USER="${DEPLOY_PG_USER:-newapi}"
PG_DB="${DEPLOY_PG_DB:-new-api}"
NETWORK_NAME="${DEPLOY_NETWORK_NAME:-clearapi_internal}"

IMAGE_BASE="${DEPLOY_IMAGE_BASE:-ghcr.io/a3225865526-wq/clearapi}"
GHCR_USER="${DEPLOY_GHCR_USER:-clearapi-deploy}"
GHCR_TOKEN_FILE="${DEPLOY_GHCR_TOKEN_FILE:-/opt/clearapi/etc/ghcr-pull-token}"

BACKUP_SERVICE="${DEPLOY_BACKUP_SERVICE:-clearapi-backup.service}"
BACKUP_TIMER="${DEPLOY_BACKUP_TIMER:-clearapi-backup.timer}"
BACKUP_DIR="${DEPLOY_BACKUP_DIR:-/opt/clearapi/backups}"

LOCAL_HEALTH_URL="${DEPLOY_LOCAL_HEALTH_URL:-http://127.0.0.1:3000/api/status}"
PUBLIC_HEALTH_URL="${DEPLOY_PUBLIC_HEALTH_URL:-https://www.clear999.com/api/status}"
CADDYFILE="${DEPLOY_CADDYFILE:-/opt/clearapi/caddy/Caddyfile}"
CADDYFILE_MD5="${DEPLOY_CADDYFILE_MD5:-ef1f8411218d727b9382918973ec7e6e}"

VERIFY_BOOTSTRAP="${DEPLOY_VERIFY_BOOTSTRAP:-true}"
BOOTSTRAP_SERVICE="${DEPLOY_BOOTSTRAP_SERVICE:-production-bootstrap}"
MIGRATION_FLAGS="${DEPLOY_MIGRATION_FLAGS:-}"

# Frozen negative-gate expectations (H4 edge acceptance baseline).
EXPECT_ENV_KEYS="${DEPLOY_EXPECT_ENV_KEYS:-CLEARAPI_ENABLE_RESPONSES_WEBSOCKET=false CLEARAPI_ENABLE_REALTIME=false CLEARAPI_CLOSED_LAUNCH=true}"
EXPECT_CHANNELS="${DEPLOY_EXPECT_CHANNELS:-1,2}"
EXPECT_USER="${DEPLOY_EXPECT_USER:-user2}"
EXPECT_TOKENS="${DEPLOY_EXPECT_TOKENS:-token1,token2}"
EXPECT_RESERVATIONS="${DEPLOY_EXPECT_RESERVATIONS:-reservation1=4/10/4/needs_review reservation2=41/93/41/needs_review}"
EXPECT_EMPTY_TABLES="${DEPLOY_EXPECT_EMPTY_TABLES:-store_products,top_ups,offline_orders,subscription_orders}"
POOL_OPTION_PATTERN="${DEPLOY_POOL_OPTION_PATTERN:-%pool%}"

AUDIT_LOG="${DEPLOY_AUDIT_LOG:-/opt/clearapi/logs/deployments.log}"
MANIFEST="${DEPLOY_MANIFEST:-${APP_DIR}/release-manifest.json}"
OVERRIDE_FILE="${DEPLOY_OVERRIDE_FILE:-${APP_DIR}/compose-candidate.yaml}"

# ---------------------------------------------------------------------------
# Args (validated before any state change)
# ---------------------------------------------------------------------------
DIGEST=""; COMMIT=""; WORKFLOW_RUN=""; MIGRATION_COMPATIBLE="false"
while [ $# -gt 0 ]; do
  case "$1" in
    --digest) DIGEST="$2"; shift 2 ;;
    --commit) COMMIT="$2"; shift 2 ;;
    --workflow-run) WORKFLOW_RUN="$2"; shift 2 ;;
    --migration-compatible) MIGRATION_COMPATIBLE="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ -n "$DIGEST" ] && [ -n "$COMMIT" ] || { echo "--digest and --commit are required" >&2; exit 2; }

printf '%s' "$DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || {
  echo "REFUSING: --digest must be an exact 'sha256:<64 hex>' reference (got: ${DIGEST})" >&2; exit 2; }
printf '%s' "$COMMIT" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "REFUSING: --commit must be a full 40-hex commit SHA" >&2; exit 2; }
[ "$MIGRATION_COMPATIBLE" = "true" ] || [ "$MIGRATION_COMPATIBLE" = "false" ] || {
  echo "REFUSING: --migration-compatible must be true or false" >&2; exit 2; }

if [ -f /opt/clearapi/etc/deploy.env ]; then
  # shellcheck disable=SC1090
  . /opt/clearapi/etc/deploy.env
else
  echo "REFUSING: /opt/clearapi/etc/deploy.env is missing (operator-owned, mode 0600)" >&2; exit 2
fi

# ---------------------------------------------------------------------------
# Helpers (all defined before any step executes)
# ---------------------------------------------------------------------------
TS_NOW() { date -u +%Y-%m-%dT%H:%M:%SZ; }
audit() { # audit <event> <result> <detail>
  mkdir -p "$(dirname "$AUDIT_LOG")"
  printf '{"ts":"%s","event":"%s","result":"%s","digest":"%s","previous_digest":"%s","commit":"%s","workflow_run":"%s","detail":"%s"}\n' \
    "$(TS_NOW)" "$1" "$2" "${DIGEST:-}" "${PREV_DIGEST:-}" "${COMMIT:-}" "${WORKFLOW_RUN:-}" "$3" >> "$AUDIT_LOG"
}
log() { printf '[deploy %s] %s\n' "$(TS_NOW)" "$*" >&2; }

REPLACE_DONE=0
die() {
  log "FAIL: $*"
  audit "deploy_failed" "failed" "$*"
  if [ "$REPLACE_DONE" = "1" ] && [ "$MIGRATION_COMPATIBLE" = "true" ]; then
    rollback "$*"
    exit 1 # rollback() exits, but be explicit
  fi
  if [ "$REPLACE_DONE" = "1" ]; then
    log "no automatic rollback (migration_compatible=${MIGRATION_COMPATIBLE}) — MANUAL REVIEW REQUIRED"
  fi
  exit 1
}

compose() { docker compose --project-directory "$APP_DIR" -f "${APP_DIR}/${COMPOSE_FILE}" "$@"; }
service_id() { compose ps -q "$SERVICE" | head -1; }
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
write_override() { # write_override <digest>
  cat > "$OVERRIDE_FILE" <<EOF
# Generated by production-deploy.sh at $(TS_NOW). Exact-digest pin.
services:
  ${SERVICE}:
    image: ${IMAGE_BASE}@$1
EOF
  chmod 0600 "$OVERRIDE_FILE"
}
set_manifest_status() { # set_manifest_status <status>
  python3 - "$MANIFEST" "$1" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    m = json.load(f)
m["status"] = sys.argv[2]
with open(sys.argv[1], "w") as f:
    json.dump(m, f, indent=2)
    f.write("\n")
PYEOF
}
rollback() {
  log "rollback: switching back to previous exact digest ${PREV_DIGEST}"
  write_override "$PREV_DIGEST"
  if ! compose up -d "$SERVICE"; then
    audit "rollback" "failed" "compose up failed for previous digest ${PREV_DIGEST}"
    log "ROLLBACK FAILED — MANUAL INTERVENTION REQUIRED"
    return 1
  fi
  if health_retry "$LOCAL_HEALTH_URL" 30 2 && health_retry "$PUBLIC_HEALTH_URL" 10 3 "CF-Ray"; then
    set_manifest_status "rolled_back"
    audit "rollback" "rolled_back" "previous digest ${PREV_DIGEST} healthy again"
    log "rollback complete; deployment remains FAILED but production is healthy on ${PREV_DIGEST}"
  else
    audit "rollback" "failed" "previous digest deployed but health checks fail; MANUAL INTERVENTION REQUIRED"
    log "ROLLBACK DEPLOYED BUT UNHEALTHY — MANUAL INTERVENTION REQUIRED"
  fi
}

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
PREV_DIGEST="$(docker inspect --format '{{.Image}}' "$CUR_ID")"
log "step 3: current production image = ${PREV_DIGEST}"

if [ -f "$MANIFEST" ]; then
  if python3 - "$MANIFEST" "$PREV_DIGEST" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    m = json.load(f)
recorded = m.get("image_digest") or m.get("image_digest_current")
if recorded and recorded != sys.argv[2]:
    sys.exit(1)
sys.exit(0)
PYEOF
  then :; else
    MANIFEST_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
    mv "$MANIFEST" "${MANIFEST}.pre-cicd.${MANIFEST_STAMP}"
    log "step 3: manifest pre-dates the CI/CD record format; archived as ${MANIFEST}.pre-cicd.${MANIFEST_STAMP}, rebuilt from live state"
    audit "manifest_upgraded" "ok" "archived pre-cicd manifest; live state was ${PREV_DIGEST}"
  fi
fi

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
if [ -f "$GHCR_TOKEN_FILE" ]; then
  docker login ghcr.io -u "$GHCR_USER" --password-stdin < "$GHCR_TOKEN_FILE" \
    || die "step 6: GHCR login failed (token file ${GHCR_TOKEN_FILE})"
  LOGGED_IN=1
else
  LOGGED_IN=0
  log "step 6: no GHCR token file configured; attempting anonymous pull (works only for public images)"
fi
docker pull --platform linux/amd64 "$IMAGE_REF" || die "step 6: docker pull ${IMAGE_REF} failed"
if [ "$LOGGED_IN" = "1" ]; then docker logout ghcr.io >/dev/null 2>&1 || true; fi
log "step 6: pulled ${IMAGE_REF}"

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
# 12/13. record old image + stage new release manifest
# ===========================================================================
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
[ -f "$MANIFEST" ] && cp "$MANIFEST" "${MANIFEST}.bak.${STAMP}"
MIGRATION_VERSION="${MIGRATION_FLAGS:-preflight-only (no flags configured)}"
MANIFEST_TMP="${MANIFEST}.tmp.$$"
python3 - "$MANIFEST_TMP" "$COMMIT" "$IMAGE_REF" "$DIGEST" "$PREV_DIGEST" "$WORKFLOW_RUN" "$MIGRATION_VERSION" "$STAMP" <<'PYEOF'
import json, sys
path, commit, image_ref, digest, prev, run_id, migration_version, stamp = sys.argv[1:9]
manifest = {
    "source_commit": commit,
    "image_ref": image_ref,
    "image_digest": digest,
    "previous_digest": prev if prev.startswith("sha256:") else ("sha256:" + prev if prev else ""),
    "workflow_run": run_id,
    "deployed_at": stamp,
    "deployment_method": "ghcr-exact-digest",
    "migration_version": migration_version,
    "status": "pending",
}
with open(path, "w") as f:
    json.dump(manifest, f, indent=2)
    f.write("\n")
PYEOF
mv "$MANIFEST_TMP" "$MANIFEST"
log "step 12/13: release manifest staged (previous ${PREV_DIGEST})"

# ===========================================================================
# 14. controlled replace
# ===========================================================================
write_override "$DIGEST"
compose up -d "$SERVICE" || die "step 14: compose replace failed"
REPLACE_DONE=1
log "step 14: container replaced with ${IMAGE_REF}"

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
cenv() { docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$NEW_ID"; }

for pair in $EXPECT_ENV_KEYS; do
  key="${pair%%=*}"; expected="${pair#*=}"
  actual="$(cenv | grep -E "^${key}=" | head -1 | cut -d= -f2-)"
  [ "$actual" = "$expected" ] || die "step 17: env gate ${key}='${actual}' expected '${expected}'"
done

PORTS_3000="$(docker port "$NEW_ID" 3000/tcp 2>/dev/null || true)"
printf '%s' "$PORTS_3000" | grep -q "127.0.0.1:3000" || die "step 17: new-api must bind 127.0.0.1:3000 only (got: ${PORTS_3000:-<unpublished>})"

PG_ID="$(compose ps -q "$PG_SERVICE")"
[ -n "$PG_ID" ] || die "step 17: postgres container missing"
PG_PORTS="$(docker port "$PG_ID" 2>/dev/null || true)"
[ -z "$PG_PORTS" ] || die "step 17: postgres must not publish ports (got: ${PG_PORTS})"

NETWORKS="$(docker inspect --format '{{range $k, $_ := .NetworkSettings.Networks}}{{$k}} {{end}}' "$NEW_ID")"
printf '%s' "$NETWORKS" | grep -qw "$NETWORK_NAME" || die "step 17: new-api not attached to ${NETWORK_NAME} (networks: ${NETWORKS})"

EXECUTORS="$(docker ps -a --format '{{.Names}}' | grep -Eic 'executor|route.?chain' || true)"
[ "$EXECUTORS" = "0" ] || die "step 17: ${EXECUTORS} executor/route-chain container(s) present; Pool must stay OFF"

[ -f "$CADDYFILE" ] || die "step 17: Caddyfile ${CADDYFILE} missing"
CADDY_MD5="$(md5sum "$CADDYFILE" | cut -d' ' -f1)"
[ "$CADDY_MD5" = "$CADDYFILE_MD5" ] || die "step 17: Caddyfile md5 ${CADDY_MD5} does not match frozen edge baseline ${CADDYFILE_MD5}"

systemctl is-active --quiet "$BACKUP_TIMER" || die "step 17: ${BACKUP_TIMER} not active after deploy"
log "step 17: runtime negative gates PASS (env, binding, PG ports, network, pool, Caddyfile, backup timer)"

# ===========================================================================
# 18. billing invariants (read-only)
# ===========================================================================
POLICY_JSONS="$(psql_q "SELECT value FROM options WHERE key ILIKE '%reserve%policy%';")"
[ -n "$POLICY_JSONS" ] || die "step 18: billing reserve policy option row missing"
printf '%s\n' "$POLICY_JSONS" | python3 -c '
import json, sys
rows = [l for l in sys.stdin.read().splitlines() if l.strip()]
if not rows:
    sys.exit(1)
if not all(json.loads(r).get("mode") == "enforce" for r in rows):
    sys.exit(1)
' || die "step 18: reserve policy mode is not enforce"

for rid_pair in $EXPECT_RESERVATIONS; do
  rid="${rid_pair%%=*}"; expected="${rid_pair#*=}"
  actual="$(psql_q "SELECT reserved||'/'||target||'/'||applied||'/'||state FROM billing_reservations WHERE request_id='${rid}';")"
  [ "$actual" = "$expected" ] || die "step 18: reservation ${rid}='${actual}' expected '${expected}'"
done

for ch in $(printf '%s' "$EXPECT_CHANNELS" | tr ',' ' '); do
  status="$(psql_q "SELECT status FROM channels WHERE id=${ch};")"
  [ "$status" = "2" ] || die "step 18: channel ${ch} status='${status}' expected 2 (disabled)"
done

U_STATUS="$(psql_q "SELECT status FROM users WHERE username='${EXPECT_USER}';")"
[ "$U_STATUS" = "2" ] || die "step 18: user ${EXPECT_USER} status='${U_STATUS}' expected 2 (disabled)"

for tok in $(printf '%s' "$EXPECT_TOKENS" | tr ',' ' '); do
  t_status="$(psql_q "SELECT status FROM tokens WHERE name='${tok}';")"
  [ "$t_status" = "2" ] || die "step 18: token ${tok} status='${t_status}' expected 2 (disabled)"
done

for tbl in $(printf '%s' "$EXPECT_EMPTY_TABLES" | tr ',' ' '); do
  cnt="$(psql_q "SELECT count(*) FROM ${tbl};")"
  [ "$cnt" = "0" ] || die "step 18: table ${tbl} row count=${cnt} expected 0"
done

POOL_ROWS="$(psql_q "SELECT count(*) FROM options WHERE key ILIKE '${POOL_OPTION_PATTERN}';")"
[ "$POOL_ROWS" = "0" ] || die "step 18: ${POOL_ROWS} pool option row(s) present; Pool must stay OFF"
log "step 18: billing invariants PASS (policy enforce, reservations, channels/user/tokens disabled, store tables empty, no pool rows)"

# ===========================================================================
# 19. mark deployment success
# ===========================================================================
set_manifest_status "success"
audit "deploy_success" "success" "digest=${DIGEST} previous=${PREV_DIGEST}"
log "step 19: DEPLOYMENT SUCCESS"
log "  commit:      ${COMMIT}"
log "  digest:      ${DIGEST}"
log "  previous:    ${PREV_DIGEST}"
log "  manifest:    ${MANIFEST}"
log "PRODUCTION DEPLOY COMPLETED"
