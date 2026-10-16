#!/usr/bin/env bash
# Candidate v2 only; not installed or deployed by this task.
# Production baseline: 2694ca19ebb13f0f1fa3045da0d141a6fd38ecdd
# Baseline SHA256: 4fe5fefd8aacea55427eebcb1183c89df5fc9b01fa9c968071cdca71395febbb
# PG03 design: 82242b9cfbcfe80f04c4278d3d8d49716fe58177
# PG03 never starts, recreates, stops or deletes an Executor.
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
#     [--workflow-run <id>] [--migration-compatible true|false] [--restart] \
#     [--pool-mode off|pg03]
#   production-deploy.sh --reconcile  # repair current approved runtime identity
#
# Operator-owned configuration: /opt/clearapi/config/deploy.env (mode 0600,
# not in the repository). Defaults below document every supported key and
# match the live VPS as verified during CI/CD acceptance (2026-10-07).

set +x # Credentials must not be exposed even when invoked with bash -x.
set -euo pipefail
umask 077

# Reserve the caller's credential stream; checks/configuration must never read it.
exec 3<&0
exec </dev/null
unset GHCR_PULL_TOKEN
unset CLEARAPI_STORE_POLICY DEPLOY_EXPECT_REFERRAL_ENABLED DEPLOY_RUNTIME_PROBE_TOKEN_FILE
# PG03 credentials/configuration may originate only in the operator-owned file.
unset CLEARAPI_POOL_EXECUTOR_ID CLEARAPI_PG02_PROCESS_EPOCH CLEARAPI_PG03_POLICY_KEY
unset CLEARAPI_PG02_GATEWAY_PROOF_KEY CLEARAPI_POOL_CLIPROXY_KEY
unset DEPLOY_PG03_COMPOSE_FILE DEPLOY_PG03_TARGET_MANIFEST

# Operator configuration is authoritative over inherited DEPLOY_* variables.
# Derive aliases only after sourcing; absent keys retain the caller/default value.
if [ -f /opt/clearapi/config/deploy.env ]; then
  python3 - /opt/clearapi/config/deploy.env <<'PY'
import os, pathlib, stat, sys
p = pathlib.Path(sys.argv[1])
production_path = str(p) == '/opt/clearapi/config/deploy.env'
for d in reversed(p.parent.parents):
    s = os.lstat(d)
    if (not stat.S_ISDIR(s.st_mode) or (production_path and s.st_uid != 0)
            or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
        sys.exit(f'REFUSING: unsafe configuration directory {d}')
s = os.lstat(p.parent)
if (not stat.S_ISDIR(s.st_mode) or (production_path and s.st_uid != 0)
        or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
    sys.exit('REFUSING: unsafe configuration directory')
s = os.lstat(p)
if (not stat.S_ISREG(s.st_mode) or (production_path and (s.st_uid != 0 or s.st_gid != 0))
        or stat.S_IMODE(s.st_mode) != 0o600 or s.st_nlink != 1):
    sys.exit('REFUSING: deploy.env must be root:root 0600, regular and unlinked')
PY
  # shellcheck disable=SC1090
  . /opt/clearapi/config/deploy.env
else
  echo "REFUSING: /opt/clearapi/config/deploy.env is missing (operator-owned, mode 0600)" >&2; exit 2
fi
LAUNCH_BASELINE="${DEPLOY_LAUNCH_BASELINE:-closed}"
case "$LAUNCH_BASELINE" in
  closed) CLOSED_LAUNCH=true; DEFAULT_CHANNELS=1,2; DEFAULT_EMPTY_TABLES=store_products,top_ups,offline_orders,subscription_orders ;;
  commercial) CLOSED_LAUNCH=false; DEFAULT_CHANNELS=; DEFAULT_EMPTY_TABLES=store_products,top_ups,subscription_orders ;;
  *) echo "REFUSING: DEPLOY_LAUNCH_BASELINE must be closed or commercial" >&2; exit 2 ;;
esac

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
EXPECT_ENV_KEYS="${DEPLOY_EXPECT_ENV_KEYS:-CLEARAPI_ENABLE_RESPONSES_WEBSOCKET=false CLEARAPI_ENABLE_REALTIME=false}"
EXPECT_CHANNELS="${DEPLOY_EXPECT_CHANNELS:-$DEFAULT_CHANNELS}"
EXPECT_USER_ID="${DEPLOY_EXPECT_USER_ID:-2}"
EXPECT_TOKEN_IDS="${DEPLOY_EXPECT_TOKEN_IDS:-1,2}"
EXPECT_REVIEW_ROWS="${DEPLOY_EXPECT_REVIEW_ROWS:-4/10/4 41/93/41}"
EXPECT_EMPTY_TABLES="${DEPLOY_EXPECT_EMPTY_TABLES:-$DEFAULT_EMPTY_TABLES}"
POOL_OPTION_PATTERN="${DEPLOY_POOL_OPTION_PATTERN:-%pool%}"
POLICY_MODE_KEY="${DEPLOY_POLICY_MODE_KEY:-billing_reserve_policy.mode}"

# Stage 2B-2 consumes only Runtime Integrity Report v1. The Stage 2B-1
# diagnostic response is deliberately incompatible; never synthesize missing
# version, product counts or identity evidence to make it pass.
STORE_POLICY="${CLEARAPI_STORE_POLICY:-disabled}"
EXPECT_REFERRAL_ENABLED="${DEPLOY_EXPECT_REFERRAL_ENABLED:-false}"
RUNTIME_PROBE_TOKEN_FILE="${DEPLOY_RUNTIME_PROBE_TOKEN_FILE:-/opt/clearapi/secrets/runtime-probe-session}"
RUNTIME_BASELINE=""
RUNTIME_IDENTITY_DIR="${DEPLOY_RUNTIME_IDENTITY_DIR:-/opt/clearapi/runtime/new-api}"
RUNTIME_IDENTITY_TOOL="/opt/clearapi/bin/runtime-identity-refresh.py"

AUDIT_LOG="${DEPLOY_AUDIT_LOG:-/opt/clearapi/logs/deployments.log}"
MANIFEST="${DEPLOY_MANIFEST:-${APP_DIR}/release-manifest.json}"
OVERRIDE_FILE="${DEPLOY_OVERRIDE_FILE:-${APP_DIR}/compose-candidate.yaml}"

POOL_MODE=off
PG03_COMPOSE_FILE="${DEPLOY_PG03_COMPOSE_FILE:-${APP_DIR}/compose-pg03.yaml}"
PG03_TARGET_MANIFEST="${DEPLOY_PG03_TARGET_MANIFEST:-${APP_DIR}/pg03-target-manifest.json}"
PG03_EXECUTOR_REF="ghcr.io/a3225865526-wq/clearapi-executor@sha256:0838072c3fd61165ba20f78084dafe678114dd5c8b19a1e8b4acd31eee2ad821"
PG03_EXECUTOR_REVISION="e5f33dc6412d17bd058bd3de2e053c3b45b9a359"
PG03_EXECUTOR_BINARY="527b1814c7b4ef1ec52b2f416ec329192ffb50dc1e9b819763e7a82b0965c898"
PG03_ATTEMPT_ID=""
PG03_EXECUTOR_CONTAINER=""
PG03_EXECUTOR_IMAGE_ID=""
PG03_APPROVAL_SHA256=""
LEGACY_TRANSITION=0
LEGACY_TRANSITION_ENTERED=0
LEGACY_TRANSITION_TOOL=/opt/clearapi/bin/pg03-legacy-transition.py

# ---------------------------------------------------------------------------
# Args (validated before any state change)
# ---------------------------------------------------------------------------
DIGEST=""; COMMIT=""; WORKFLOW_RUN=""; MIGRATION_COMPATIBLE="false"; GHCR_TOKEN_STDIN=""; VERIFY_ONLY="0"; RESTART_ONLY="0"; RECONCILE_ONLY="0"
while [ $# -gt 0 ]; do
  case "$1" in
    --pool-mode)
      [ "$#" -ge 2 ] || { echo 'REFUSING: --pool-mode requires off or pg03' >&2; exit 2; }
      POOL_MODE="$2"; shift 2 ;;
    --legacy-transition-v1) LEGACY_TRANSITION=1; shift ;;
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
    # Identity plus Runtime Report v1 verification. Appends an audit record;
    # requires a separately supplied valid root session credential, including
    # when invoked from a systemd timer
    # (monitoring minimal set) or before any manual container operation.
    --verify) VERIFY_ONLY="1"; shift 1 ;;
    --restart) RESTART_ONLY="1"; shift 1 ;;
    --reconcile) RECONCILE_ONLY="1"; shift 1 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

# The workflow sends one newline-terminated record. Capture it only in stdin
# mode, before external probes, and never export it or include it in diagnostics.
if [ "$GHCR_TOKEN_STDIN" = "1" ]; then
  if ! IFS= read -r GHCR_PULL_TOKEN <&3 || [ -z "$GHCR_PULL_TOKEN" ]; then
    exec 3<&-
    unset GHCR_PULL_TOKEN
    echo "REFUSING: empty or incomplete GHCR credential input" >&2
    exit 2
  fi
fi
exec 3<&-

# ---------------------------------------------------------------------------
# Helpers (all defined before any step executes)
# ---------------------------------------------------------------------------
TS_NOW() { date -u +%Y-%m-%dT%H:%M:%SZ; }
audit() { # audit <event> <result> <detail>
  mkdir -p "$(dirname "$AUDIT_LOG")"
  python3 - "$AUDIT_LOG" "$(TS_NOW)" "$1" "$2" "${DIGEST:-}" "${PREV_IMAGE_ID:-}" "${COMMIT:-}" "${WORKFLOW_RUN:-}" "$3" <<'PY'
import json, os, stat, sys
path, *values = sys.argv[1:]
fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
with os.fdopen(fd, 'a') as f:
    s = os.fstat(f.fileno())
    production_path = path.startswith('/opt/clearapi/')
    if (not stat.S_ISREG(s.st_mode) or (production_path and s.st_uid != 0)
            or s.st_mode & 0o077 or s.st_nlink != 1):
        sys.exit('unsafe deployment audit file')
    f.write(json.dumps(dict(zip(('ts','event','result','digest','previous_image','commit','workflow_run','detail'), values))) + '\n')
    f.flush()
    os.fsync(f.fileno())
PY
}
log() { printf '[deploy %s] %s\n' "$(TS_NOW)" "$*" >&2; }

REPLACE_STARTED=0
WORK_DIR=""
EPHEMERAL_DOCKER_CONFIG=""
cleanup() {
  [ -z "$EPHEMERAL_DOCKER_CONFIG" ] || rm -rf -- "$EPHEMERAL_DOCKER_CONFIG"
  [ -z "$WORK_DIR" ] || rm -rf -- "$WORK_DIR"
}
trap cleanup EXIT
die() {
  trap - ERR INT TERM HUP
  log "FAIL: $*"
  audit "deploy_failed" "failed" "$*" || log "AUDIT_FAILED: deployment failure could not be persisted"
  if [ "$LEGACY_TRANSITION_ENTERED" = 1 ]; then
    legacy_transition_phase fail || log 'maintenance journal requires manual reconciliation'
    compose stop --timeout 150 "$SERVICE" || log 'MAINTENANCE CONTAINMENT FAILED: verify Gateway stopped manually'
    log 'LEGACY TRANSITION FAILED CLOSED: no old-image restart, database restore or Pool rollback'
  fi
  if [ "$POOL_MODE" = pg03 ]; then
    pg03_mark_failed || log 'PG03 failure manifest could not be persisted; manual review required'
    log 'PG03 BLOCKED: preserving Gateway, Executor, auth, receipts, checkpoint and data; no automatic rollback'
    exit 1
  fi
  if [ "$REPLACE_STARTED" = "1" ] && [ "$MIGRATION_COMPATIBLE" = "true" ]; then
    rollback "$*" || log "ROLLBACK_FAILED: MANUAL REVIEW REQUIRED"
    exit 1
  fi
  if [ "$REPLACE_STARTED" = "1" ]; then
    log "no automatic rollback (migration_compatible=${MIGRATION_COMPATIBLE}) — MANUAL REVIEW REQUIRED"
  fi
  exit 1
}
legacy_transition_phase() {
  python3 -I "$LEGACY_TRANSITION_TOOL" phase "$1" "$WORKFLOW_RUN" "$DIGEST" "$COMMIT" "${@:2}"
}
compose() {
  # The override file (exact-digest pin, written by write_override) MUST be
  # part of every compose invocation; without it `compose up -d` sees an
  # unchanged image ref and silently does nothing while the deploy reports
  # success (observed in run 37762896277: manifest updated, container not
  # replaced). Attach it whenever it exists.
  local args=(--project-directory "$APP_DIR" -f "${APP_DIR}/${COMPOSE_FILE}")
  [ "$POOL_MODE" != pg03 ] || args+=(-f "$PG03_COMPOSE_FILE")
  [ -f "$OVERRIDE_FILE" ] && args+=(-f "$OVERRIDE_FILE")
  # PG03 requires root and uses the exported operator environment directly.
  # sudo env_reset would otherwise discard Compose interpolation credentials.
  if [ "$POOL_MODE" = pg03 ]; then docker compose "${args[@]}" "$@"; return; fi
  sudo docker compose "${args[@]}" "$@"
}
candidate_compose() {
  # Only temporary jobs see this override; the live digest pin is unchanged.
  local args=(--project-directory "$APP_DIR" -f "${APP_DIR}/${COMPOSE_FILE}")
  [ "$POOL_MODE" != pg03 ] || args+=(-f "$PG03_COMPOSE_FILE")
  [ -f "$OVERRIDE_FILE" ] && args+=(-f "$OVERRIDE_FILE")
  if [ "$POOL_MODE" = pg03 ]; then docker compose "${args[@]}" -f "$CANDIDATE_OVERRIDE" "$@"; return; fi
  sudo docker compose "${args[@]}" -f "$CANDIDATE_OVERRIDE" "$@"
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
  [ "$POOL_MODE" != pg03 ] || args+=(-f "$PG03_COMPOSE_FILE")
  [ -f "$OVERRIDE_FILE" ] && args+=(-f "$OVERRIDE_FILE")
  # A failed probe must yield an empty string instead of aborting the wrapper
  # under set -e, so the caller can log the drift verdict and audit it.
  { if [ "$POOL_MODE" = pg03 ]; then
      docker compose "${args[@]}" config --format json 2>/dev/null
    else
      sudo docker compose "${args[@]}" config --format json 2>/dev/null
    fi \
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
  compose exec -T "$PG_SERVICE" psql -U "$PG_USER" -d "$PG_DB" -Atq -v ON_ERROR_STOP=1 -c "$1" \
    || die "read-only database probe failed"
}
verify_commercial_configuration() {
  local rows active_pairs pricing
  rows="$(psql_q "/* clearapi-commercial-config */ SELECT concat_ws(E'\t', c.id::text, c.status::text, replace(replace(replace(coalesce(c.models, ''), E'\t', ' '), E'\r', ' '), E'\n', ' '), replace(replace(replace(coalesce(c.\"group\", ''), E'\t', ' '), E'\r', ' '), E'\n', ' '), coalesce(a.model, ''), coalesce(a.\"group\", ''), coalesce(a.enabled::text, '')) FROM channels c FULL OUTER JOIN abilities a ON a.channel_id = c.id WHERE c.status = 1 OR a.enabled = true ORDER BY c.id, a.model, a.\"group\";")" \
    || die "launch baseline: commercial configuration query failed"
  active_pairs="$(printf '%s\n' "$rows" | python3 -c '
import sys

channels = {}
abilities = []
for line in sys.stdin:
    line = line.rstrip("\n")
    fields = line.split("\t")
    if len(fields) != 7:
        raise SystemExit("invalid configuration row")
    channel_id, status, models, groups, model, group, enabled = fields
    if not channel_id.isdigit() or status not in {"1", "2", "3"}:
        raise SystemExit("invalid channel identity or status")
    channel = channels.setdefault(channel_id, {"status": status, "models": set(), "groups": set()})
    if channel["status"] != status:
        raise SystemExit("inconsistent channel status")
    channel["models"].update(item.strip() for item in models.split(",") if item.strip())
    channel["groups"].update(item.strip() for item in groups.split(",") if item.strip())
    if enabled == "true":
        abilities.append((channel_id, model.strip(), group.strip()))
if not channels:
    raise SystemExit("no configured channels")
if not abilities:
    raise SystemExit("no enabled abilities")
active_channels = {channel_id for channel_id, _, _ in abilities}
if any(channel["status"] == "1" and channel_id not in active_channels for channel_id, channel in channels.items()):
    raise SystemExit("enabled channel has no enabled abilities")
for channel_id, model, group in abilities:
    channel = channels.get(channel_id)
    if channel is None or channel["status"] != "1":
        raise SystemExit("enabled ability is not attached to an enabled channel")
    if not model or not group or model not in channel["models"] or group not in channel["groups"]:
        raise SystemExit("enabled ability does not match its channel model/group configuration")
    print(model + "\t" + group)
')" || die "launch baseline: invalid commercial channel/model/group configuration"
  pricing="$(psql_q "/* clearapi-commercial-pricing */ SELECT concat_ws(E'\t', key, replace(replace(replace(value, E'\t', ' '), E'\r', ' '), E'\n', ' ')) FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'GroupRatio', 'SelfUseModeEnabled', 'billing_setting.billing_mode', 'billing_setting.billing_expr') ORDER BY key;")" \
    || die "launch baseline: commercial pricing query failed"
  ACTIVE_PAIRS="$active_pairs" python3 -c '
import json
import math
import os
import sys

pricing = {}
for line in sys.stdin:
    line = line.rstrip("\n")
    if not line:
        continue
    fields = line.split("\t")
    if len(fields) != 2:
        raise SystemExit("invalid pricing row")
    pricing[fields[0]] = fields[1]
if not pricing:
    raise SystemExit("missing pricing options")

def read_map(name):
    raw = pricing.get(name, "")
    if not raw:
        return {}
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise SystemExit(name + " must be a JSON object")
    return value

prices = read_map("ModelPrice")
ratios = read_map("ModelRatio")
modes = read_map("billing_setting.billing_mode")
expressions = read_map("billing_setting.billing_expr")
groups = read_map("GroupRatio")
self_use = pricing.get("SelfUseModeEnabled", "false").lower() == "true"
def positive(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) and value > 0
models = os.environ.get("ACTIVE_PAIRS", "")
for pair in models.splitlines():
    model, group = pair.split("\t", 1)
    has_legacy_price = positive(prices[model]) if model in prices else positive(ratios.get(model))
    has_expression = modes.get(model) == "tiered_expr" and isinstance(expressions.get(model), str) and expressions[model].strip()
    if not (has_legacy_price or has_expression or self_use):
        raise SystemExit("enabled model has no effective price: " + model)
    if not positive(groups.get(group, 1)):
        raise SystemExit("enabled group has no positive ratio: " + group)
' <<< "$(printf '%s\n' "$pricing")" || die "launch baseline: missing or invalid commercial pricing"
  log "launch baseline: dynamic commercial configuration PASS"
}
verify_commercial_billing() {
  local invalid
  invalid="$(psql_q "/* clearapi-commercial-billing */ SELECT (SELECT count(*) FROM users WHERE quota < 0 OR used_quota < 0) + (SELECT count(*) FROM tokens WHERE used_quota < 0 OR (NOT unlimited_quota AND remain_quota < 0)) + (SELECT count(*) FROM offline_orders o WHERE o.money_cents <= 0 OR o.quota_per_code <= 0 OR o.code_count <= 0 OR o.status NOT IN ('paid', 'void') OR (SELECT count(*) FROM offline_order_codes l WHERE l.order_id = o.id) <> o.code_count) + (SELECT count(*) FROM offline_order_codes l LEFT JOIN offline_orders o ON o.id = l.order_id LEFT JOIN redemptions r ON r.id = l.redemption_id WHERE o.id IS NULL OR r.id IS NULL OR r.quota <> o.quota_per_code OR r.source <> 'OFFLINE_ORDER');")" \
    || die "commercial billing query failed"
  [ "$invalid" = 0 ] || die "commercial billing: invalid wallet, token or offline-order accounting"
  log "commercial billing: wallet/token/offline-order invariants PASS"
}
verify_runtime_gate() { # phase expected-image-id; failure never falls back to SQL
  local phase="$1" expected_image="$2" cid identity binary receipt
  cid="$(service_id)" || return 1
  [ -n "$cid" ] || { log 'Runtime Gate BLOCKED: PROBE_UNAVAILABLE'; return 1; }
  identity="$(sudo docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}}' "$cid")" \
    || { log 'Runtime Gate BLOCKED: PROBE_UNAVAILABLE'; return 1; }
  # A separate read-only, networkless image invocation supplies the expected
  # binary digest. Report build_identity is NOT an OCI image digest or revision.
  binary="$(sudo docker run --rm --network none --read-only --entrypoint sha256sum "$expected_image" /new-api)" \
    || { log 'Runtime Gate BLOCKED: PROBE_UNAVAILABLE'; return 1; }
  # Bind the fixed local endpoint to the inspected service before sending a
  # credential. No configurable remote URL, proxies or redirects are allowed.
  [ "$(sudo docker port "$cid" 3000/tcp)" = '127.0.0.1:3000' ] \
    || { log 'Runtime Gate BLOCKED: PROBE_UNAVAILABLE'; return 1; }
  if ! receipt="$(python3 -I - "$RUNTIME_PROBE_TOKEN_FILE" "$identity" "$expected_image" "$binary" \
      "$STORE_POLICY" "$EXPECT_REFERRAL_ENABLED" "$RUNTIME_BASELINE" "$RESTART_ONLY" <<'PY'
import datetime, hashlib, json, os, pathlib, re, stat, sys, urllib.error, urllib.request, uuid

def blocked(code):
    print(json.dumps({'status': 'BLOCKED', 'code': code}))
    raise SystemExit(1)

def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            blocked('PROBE_UNAVAILABLE')
        result[key] = value
    return result

try:
    token_path, identity, expected_image, binary, policy, referral, baseline, restarting = sys.argv[1:]
    if policy != 'disabled' or referral not in ('true', 'false'):
        blocked('RUNTIME_CONFIG_MISMATCH')
    parts = identity.split()
    if len(parts) != 3 or not re.fullmatch(r'[a-f0-9]{64}', parts[0]) or parts[2] != expected_image:
        blocked('PROBE_UNAVAILABLE')
    container_id, started_at, _ = parts
    if not re.fullmatch(r'[a-f0-9]{64}  /new-api', binary):
        blocked('PROBE_UNAVAILABLE')
    expected_build = 'sha256:' + binary.split()[0]
    path = pathlib.Path(token_path)
    if not path.is_absolute():
        blocked('PROBE_UNAVAILABLE')
    for directory in path.parents:
        s = os.lstat(directory)
        if (not stat.S_ISDIR(s.st_mode) or s.st_uid != 0
                or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
            blocked('PROBE_UNAVAILABLE')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as f:
        s = os.fstat(f.fileno())
        if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                or stat.S_IMODE(s.st_mode) != 0o600 or s.st_nlink != 1):
            blocked('PROBE_UNAVAILABLE')
        token = f.read(16385).decode('ascii').strip()
    if len(token) > 16384 or not re.fullmatch(r'[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', token):
        blocked('PROBE_UNAVAILABLE')

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request('http://127.0.0.1:3000/api/clearapi/runtime-integrity',
        headers={'Authorization': 'Bearer ' + token, 'Cache-Control': 'no-store'})
    requested_at = datetime.datetime.now(datetime.timezone.utc)
    try:
        response = opener.open(request, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        status = response.status
        payload = response.read(65537)
    if len(payload) > 65536:
        blocked('PROBE_UNAVAILABLE')
    body = json.loads(payload, object_pairs_hook=unique_object)
    if type(body) is not dict:
        blocked('PROBE_UNAVAILABLE')
    if body.get('success') is False:
        code = body.get('code')
        blocked(code if code in ('RUNTIME_CONFIG_MISMATCH', 'APPROVAL_VIEW_INVALID',
            'PRODUCT_VIEW_INVALID', 'PROBE_UNAVAILABLE') else 'PROBE_UNAVAILABLE')
    if status != 200 or body.get('success') is not True or type(body.get('data')) is not dict:
        blocked('PROBE_UNAVAILABLE')
    report = body['data']
    fields = {
        'schema_version': str, 'report_version': str, 'status': str, 'instance_id': str,
        'container_id': str, 'started_at': str, 'build_identity': str,
        'config_generation': str, 'config_digest': str,
        'online_payment_enabled': bool, 'store_enabled': bool, 'billing_mode': str,
        'referral_enabled': bool, 'pool_enabled': bool, 'approval_schema': int,
        'catalog_version': int, 'approval_view_digest': str, 'product_view_digest': str,
        'effective_product_count': int, 'generated_at': str,
    }
    if set(report) != set(fields) or any(type(report.get(key)) is not kind for key, kind in fields.items()):
        blocked('PROBE_UNAVAILABLE')
    if report['schema_version'] != '1' or report['report_version'] != '1' or report['status'] != 'PASS':
        blocked('PROBE_UNAVAILABLE')
    # Container lifecycle metadata is independently inspected, while the UUID
    # identifies the current process. Neither request/env values nor UUID alone
    # establish container identity. Old flat/nested contracts are not accepted.
    instance = uuid.UUID(report['instance_id'])
    if (instance.version != 4 or str(instance) != report['instance_id']
            or report['container_id'] != container_id or report['started_at'] != started_at):
        blocked('PROBE_UNAVAILABLE')
    if report['build_identity'] != expected_build:
        blocked('PROBE_UNAVAILABLE')
    digest = r'sha256:[a-f0-9]{64}'
    if not re.fullmatch(digest, report['config_generation']) or not re.fullmatch(digest, report['config_digest']):
        blocked('RUNTIME_CONFIG_MISMATCH')
    config = {key: report[key] for key in ('online_payment_enabled', 'store_enabled',
        'billing_mode', 'referral_enabled', 'pool_enabled')}
    expected_config = 'sha256:' + hashlib.sha256(json.dumps(config, separators=(',', ':')).encode()).hexdigest()
    if report['config_digest'] != expected_config:
        blocked('RUNTIME_CONFIG_MISMATCH')
    if (report['online_payment_enabled'] or report['store_enabled'] or report['pool_enabled']
            or report['billing_mode'] != 'enforce'
            or report['referral_enabled'] != (referral == 'true')):
        blocked('RUNTIME_CONFIG_MISMATCH')
    if report['approval_schema'] != 1:
        blocked('APPROVAL_VIEW_INVALID')
    if (report['catalog_version'] <= 0 or report['effective_product_count'] != 0
            or not re.fullmatch(digest, report['product_view_digest'])):
        blocked('PRODUCT_VIEW_INVALID')
    if not re.fullmatch(digest, report['approval_view_digest']):
        blocked('APPROVAL_VIEW_INVALID')
    timestamp = datetime.datetime.fromisoformat(report['generated_at'].replace('Z', '+00:00'))
    now = datetime.datetime.now(datetime.timezone.utc)
    if timestamp.tzinfo is None or not (-2 <= (now-timestamp).total_seconds() <= 15):
        blocked('PROBE_UNAVAILABLE')
    if timestamp < requested_at - datetime.timedelta(seconds=2):
        blocked('PROBE_UNAVAILABLE')
    receipt = {key: report[key] for key in ('instance_id', 'container_id', 'started_at',
        'config_generation', 'config_digest', 'catalog_version',
        'approval_view_digest', 'product_view_digest', 'effective_product_count')}
    if baseline:
        previous = json.loads(baseline)
        if (restarting != '1' and previous['container_id'] == receipt['container_id'] and
                (previous['instance_id'] != receipt['instance_id'] or previous['started_at'] != receipt['started_at'])):
            blocked('PROBE_UNAVAILABLE')
        if restarting == '1' and (previous['instance_id'] == receipt['instance_id'] or previous['started_at'] == receipt['started_at']):
            blocked('PROBE_UNAVAILABLE')
        if previous['container_id'] != receipt['container_id'] and previous['instance_id'] == receipt['instance_id']:
            blocked('PROBE_UNAVAILABLE')
        if any(previous[key] != receipt[key] for key in ('config_generation', 'config_digest')):
            blocked('RUNTIME_CONFIG_MISMATCH')
        if previous['approval_view_digest'] != receipt['approval_view_digest']:
            blocked('APPROVAL_VIEW_INVALID')
        if any(previous[key] != receipt[key] for key in (
                'catalog_version', 'product_view_digest', 'effective_product_count')):
            blocked('PRODUCT_VIEW_INVALID')
    print(json.dumps(receipt, sort_keys=True))
except Exception:
    # Never emit HTTP bodies, file contents, credentials or exception details.
    blocked('PROBE_UNAVAILABLE')
PY
)"; then
    [ -n "$receipt" ] || receipt='{"status":"BLOCKED","code":"PROBE_UNAVAILABLE"}'
    log "Runtime Gate $receipt"
    return 1
  fi
  # Reject a replacement/race while the HTTP report was being collected.
  [ "$(sudo docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}}' "$(service_id)")" = "$identity" ] \
    || { log 'Runtime Gate BLOCKED: PROBE_UNAVAILABLE'; return 1; }
  if [ "$phase" = before ]; then RUNTIME_BASELINE="$receipt"; fi
  log "Runtime Gate PASS (${phase})"
}
verify_launch_baseline() {
  local actual approved other status ch expected_email_verification
  if [ "$LAUNCH_BASELINE" = commercial ]; then
    expected_email_verification=true
  else
    expected_email_verification=false
  fi
  # Email verification is determined by the selected operator-approved launch
  # baseline; it is not an independent caller-controlled override.
  actual="$(compose exec -T "$SERVICE" printenv CLEARAPI_CLOSED_LAUNCH)" \
    || die "launch baseline: CLOSED_LAUNCH unavailable"
  [ "$actual" = "$CLOSED_LAUNCH" ] || die "launch baseline: CLOSED_LAUNCH expected ${CLOSED_LAUNCH}"
  # Check effective process options, not only persisted values (closed-launch
  # enforcement can override options during application startup).
  curl -fsS --max-time 15 "$LOCAL_HEALTH_URL" | python3 -c '
import json, sys
d = json.load(sys.stdin)
expected = sys.argv[1] == "commercial"
expected_email_verification = sys.argv[2] == "true"
v = d.get("data", {})
if (d.get("success") is not True
        or any(v.get(k) is not expected for k in
               ("register_enabled", "password_register_enabled"))
        or v.get("password_login_enabled") is not True
        or v.get("email_verification") is not expected_email_verification):
    sys.exit(1)
' "$LAUNCH_BASELINE" "$expected_email_verification" \
    || die "launch baseline: effective registration/email verification mismatch"
actual="$(psql_q "SELECT value FROM options WHERE key='EmailVerificationEnabled';")"
[ "$actual" = "$expected_email_verification" ] \
  || die "launch baseline: database EmailVerificationEnabled mismatch"
  if [ "$LAUNCH_BASELINE" = commercial ]; then
    verify_commercial_configuration
    other="$(psql_q "SELECT count(*) FROM channels WHERE status NOT IN (1, 2, 3);")"
  else
    other="$(psql_q "SELECT count(*) FROM channels WHERE status<>2;")"
  fi
  [ "$other" = 0 ] || die "launch baseline: unapproved channel state"
  for ch in $(printf '%s' "$EXPECT_CHANNELS" | tr ',' ' '); do
    status="$(psql_q "SELECT status FROM channels WHERE id=${ch};")"
    [ "$status" = 2 ] || die "launch baseline: channel ${ch} expected disabled"
  done
}

verify_stop_policy() {
  compose config --format json | python3 -c '
import json, re, sys
s = json.load(sys.stdin)["services"][sys.argv[1]]
e = s.get("environment") or {}
timeout = int(e.get("SHUTDOWN_TIMEOUT_SECONDS", "120"))
# Compose canonical JSON can normalize 150s into 2m30s.
grace = s.get("stop_grace_period", "0s")
if not re.fullmatch(r"(?:[0-9]+(?:\.[0-9]+)?[hms])+", str(grace)):
    sys.exit(1)
seconds = sum(float(n) * {"h":3600,"m":60,"s":1}[u]
              for n, u in re.findall(r"([0-9]+(?:\.[0-9]+)?)([hms])", str(grace)))
if timeout != 120 or seconds < timeout + 30 or s.get("stop_signal") != "SIGTERM":
    sys.exit(1)
' "$SERVICE" || die "stop policy: require SIGTERM, 120s application shutdown and at least 150s grace"
}
health_retry() { # health_retry <url> <retries> <sleep_s> [<require_header>]
  local url="$1" retries="$2" sleep_s="$3" header="${4:-}" i=0
  if [ -z "$WORK_DIR" ]; then WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/clearapi-deploy.XXXXXXXX")"; fi
  while [ "$i" -lt "$retries" ]; do
    if [ -n "$header" ]; then
      if curl -fsS -D "$WORK_DIR/health-headers" -o "$WORK_DIR/health-body" --max-time 15 "$url" \
          && grep -qi "^${header}:" "$WORK_DIR/health-headers"; then
        return 0
      fi
    else
      if curl -fsS -o "$WORK_DIR/health-body" --max-time 15 "$url"; then
        return 0
      fi
    fi
    i=$((i + 1)); sleep "$sleep_s"
  done
  return 1
}
write_override() { # write_override <image ref> — exact digest ref, or exact local image for legacy rollback
  python3 -I "$RUNTIME_IDENTITY_TOOL" --prepare-only --directory "$RUNTIME_IDENTITY_DIR" || return 1
  cat > "$OVERRIDE_FILE" <<EOF
# Generated by production-deploy.sh at $(TS_NOW). Exact-digest pin.
services:
  ${SERVICE}:
    image: $1
    volumes:
      - type: bind
        source: ${RUNTIME_IDENTITY_DIR}
        target: /run/clearapi
        read_only: true
        bind:
          create_host_path: false
EOF
  chmod 0600 "$OVERRIDE_FILE"
}
refresh_runtime_identity() {
  python3 -I "$RUNTIME_IDENTITY_TOOL" --reconcile --container "$1" --image-ref "$2" \
    --expected-revision "$3" --directory "$RUNTIME_IDENTITY_DIR"
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
set_pg03_success() {
  [ "$POOL_MODE" = pg03 ] || return 0
  python3 -I - "$MANIFEST" <<'PY'
import json, os, sys, tempfile
path = sys.argv[1]
with open(path) as f:
    m = json.load(f)
c = m['releases']['current']
if c.get('pool_mode') != 'pg03':
    raise SystemExit('PG03 manifest mode missing')
c.setdefault('deployment_attempt', {})['status'] = 'success'
c.setdefault('validation', {}).update({
    'result': 'passed', 'gateway_executor_connection': 'passed',
    'runtime_integrity_v1': 'passed', 'pg03_runtime_gate': 'passed',
    'health': 'passed', 'billing': 'passed'})
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix='.pg03-success-')
with os.fdopen(fd, 'w') as f:
    json.dump(m, f, indent=2); f.write('\n'); f.flush(); os.fsync(f.fileno())
os.replace(tmp, path)
PY
}
rollback() {
  log "rollback: switching back to previous exact image (${ROLLBACK_REF})"
  write_override "$ROLLBACK_REF" || return 1
  if ! compose up -d --timeout 150 "$SERVICE"; then
    audit "rollback" "failed" "compose up failed for previous image ${ROLLBACK_REF}"
    log "ROLLBACK FAILED — MANUAL INTERVENTION REQUIRED"
    return 1
  fi
  RID="$(service_id)" || return 1
  RUN_IMG="$(sudo docker inspect --format '{{.Image}}' "$RID")" || return 1
  local restored_ref restored_revision
  restored_ref="$(compose_effective_image)" || return 1
  restored_revision="$(sudo docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$RUN_IMG")" || return 1
  refresh_runtime_identity "$RID" "$restored_ref" "$restored_revision" || return 1
  if [ "$RUN_IMG" != "$PREV_IMAGE_ID" ]; then
    audit "rollback" "failed" "running image ${RUN_IMG} does not match recorded previous ${PREV_IMAGE_ID}"
    log "ROLLBACK IMAGE MISMATCH — MANUAL INTERVENTION REQUIRED"
    return 1
  fi
  if health_retry "$LOCAL_HEALTH_URL" 30 2 && health_retry "$PUBLIC_HEALTH_URL" 10 3 "CF-Ray"; then
    # Restore the complete pre-deploy manifest, not the failed candidate current.
    # Retain failed candidate provenance and reason in the manifest and audit.
    python3 - "$MANIFEST" "$PRE_DEPLOY_MANIFEST" "$*" <<'PY' || return 1
import json, os, sys, tempfile
path, backup, reason = sys.argv[1:]
with open(path) as f: failed = json.load(f)['releases']['current']
with open(backup) as f: restored = json.load(f)
restored.setdefault('failed_deployments', []).append({'candidate': failed, 'reason': reason})
fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix='.manifest-rollback-')
with os.fdopen(fd, 'w') as f:
    json.dump(restored, f, indent=2)
    f.write('\n'); f.flush(); os.fsync(f.fileno())
os.replace(tmp, path)
PY
    verify_running_identity || return 1
    verify_runtime_gate after "$PREV_IMAGE_ID" || return 1
    audit "rollback" "rolled_back" "previous image ${PREV_IMAGE_ID} healthy again" || return 1
    log "rollback complete; deployment remains FAILED but production is healthy on the previous image"
  else
    audit "rollback" "failed" "previous image deployed but health checks fail; MANUAL INTERVENTION REQUIRED"
    log "ROLLBACK DEPLOYED BUT UNHEALTHY — MANUAL INTERVENTION REQUIRED"
    return 1
  fi
}

# PG03 gates are additive. Runtime Integrity v1 and Billing gates below are
# unchanged: database Pool master remains OFF, including in PG03 preparation.
pg03_load_pair() { # approved target for deploy; current release for other modes
  local source="$1" kind="$2" metadata
  metadata="$(python3 -I - "$source" "$kind" "$PG03_COMPOSE_FILE" \
      "$PG03_EXECUTOR_REF" "$PG03_EXECUTOR_REVISION" "$PG03_EXECUTOR_BINARY" "$LEGACY_TRANSITION" <<'PY'
import hashlib, json, os, pathlib, re, stat, sys

def secure_file(name, private=False):
    p = pathlib.Path(name)
    if not p.is_absolute():
        raise ValueError()
    for directory in p.parents:
        s = os.lstat(directory)
        if (not stat.S_ISDIR(s.st_mode) or s.st_uid != 0
                or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
            raise ValueError()
    fd = os.open(p, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as f:
        s = os.fstat(f.fileno())
        if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                or s.st_nlink != 1 or (private and stat.S_IMODE(s.st_mode) != 0o600)
                or (not private and s.st_mode & 0o022)):
            raise ValueError()
        raw = f.read(131073)
    if len(raw) > 131072:
        raise ValueError()
    return raw

def unique(pairs):
    result = {}
    for k, v in pairs:
        if k in result:
            raise ValueError()
        result[k] = v
    return result

try:
    name, kind, overlay, pinned_ref, pinned_revision, pinned_binary, legacy_transition = sys.argv[1:]
    secure_file(overlay)
    raw = secure_file(name)
    m = json.loads(raw, object_pairs_hook=unique)
    c = m['releases']['current'] if kind == 'current' else m
    if c['pool_mode'] != 'pg03':
        raise ValueError()
    g, e = c['components']['gateway'], c['components']['executor']
    for component in (g, e):
        if (not re.fullmatch(r'[^\s@]+@sha256:[a-f0-9]{64}', component['image_ref'])
                or component['image_ref'].split('@')[1] != component['image_digest']
                or not re.fullmatch(r'[a-f0-9]{40}', component['oci_revision'])):
            raise ValueError()
    gateway_id = g.get('image_id', '')
    executor_id = e.get('image_id', '')
    if kind == 'current' and (not re.fullmatch(r'sha256:[a-f0-9]{64}', gateway_id)
                              or not re.fullmatch(r'sha256:[a-f0-9]{64}', executor_id)):
        raise ValueError()
    if gateway_id and not re.fullmatch(r'sha256:[a-f0-9]{64}', gateway_id):
        raise ValueError()
    if executor_id and not re.fullmatch(r'sha256:[a-f0-9]{64}', executor_id):
        raise ValueError()
    if (e['image_ref'] != pinned_ref or e['oci_revision'] != pinned_revision
            or e['binary_sha256'] != pinned_binary
            or e['executor_id'] != os.environ['CLEARAPI_POOL_EXECUTOR_ID']
            or e['process_epoch'] != os.environ['CLEARAPI_PG02_PROCESS_EPOCH']):
        raise ValueError()
    if kind == 'current':
        if (g['image_ref'] != c['image_ref'] or g['image_digest'] != c['image_digest']
                or g['image_id'] != c['new_api_image_id'] or g['oci_revision'] != c['candidate_commit']
                or c.get('status') not in ('success', 'pending')):
            raise ValueError()
    else:
        validation = m.get('validation', {})
        autonomous = validation.get('authorization')
        if autonomous is not None:
            if (validation.get('reviewers') != [] or autonomous != {
                    'mode': 'owner-dispatch-v1', 'actor': 'a3225865526-wq',
                    'run': validation.get('approval_run')}
                    or not re.fullmatch(r'[1-9][0-9]*', autonomous.get('run', ''))):
                raise ValueError()
        elif validation.get('approval') != 'passed':
            raise ValueError()
    if kind == 'target' and bool(m.get('legacy_transition')) != (legacy_transition == '1'):
        raise ValueError()
    if not re.fullmatch(r'sha256:[a-f0-9]{64}', e['image_id']):
        raise ValueError()
    print(g['image_ref'], g['image_digest'], g['oci_revision'], gateway_id or '-',
          executor_id or '-', hashlib.sha256(raw).hexdigest())
except Exception:
    sys.exit('PG03 BLOCKED: unsafe or inconsistent approved component manifest/overlay')
PY
)" || return 1
  read -r PG03_GATEWAY_REF PG03_GATEWAY_DIGEST PG03_GATEWAY_REVISION PG03_GATEWAY_IMAGE_ID \
    PG03_EXECUTOR_IMAGE_ID PG03_APPROVAL_SHA256 <<< "$metadata"
}

pg03_load_environment() {
  [ "$POOL_MODE" = pg03 ] || return 0
  set +x
  local name metadata value
  for name in CLEARAPI_POOL_EXECUTOR_ID CLEARAPI_PG02_PROCESS_EPOCH CLEARAPI_PG03_POLICY_KEY CLEARAPI_PG02_GATEWAY_PROOF_KEY CLEARAPI_POOL_CLIPROXY_KEY; do
    if [[ -v "$name" ]]; then
      echo 'PG03 BLOCKED: conflicting operator configuration source' >&2
      return 1
    fi
  done
  metadata="$(python3 -I - <<'PYPG03ENV'
import os, pathlib, re, stat, sys

keys = ('CLEARAPI_POOL_EXECUTOR_ID', 'CLEARAPI_PG02_PROCESS_EPOCH',
        'CLEARAPI_PG03_POLICY_KEY', 'CLEARAPI_PG02_GATEWAY_PROOF_KEY',
        'CLEARAPI_POOL_CLIPROXY_KEY')

def read_secure(name):
    p = pathlib.Path(name)
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in p.parts[1:-1]:
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd)
            fd = next_fd
            s = os.fstat(fd)
            if s.st_uid != 0 or s.st_gid != 0 or s.st_mode & 0o022:
                raise ValueError()
        file_fd = os.open(p.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
        with os.fdopen(file_fd, 'rb') as f:
            s = os.fstat(f.fileno())
            if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                    or stat.S_IMODE(s.st_mode) != 0o600 or s.st_nlink != 1):
                raise ValueError()
            raw = f.read(65537)
        if len(raw) > 65536:
            raise ValueError()
        return raw.decode('utf-8')
    finally:
        os.close(fd)

try:
    deploy = read_secure('/opt/clearapi/config/deploy.env')
    for line in deploy.splitlines():
        match = re.match(r'\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=', line)
        if match and match[1] in keys:
            raise ValueError()
    values = {}
    for line in read_secure('/opt/clearapi/config/pg03/pg03.env').splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        match = re.fullmatch(r'\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=([A-Za-z0-9_-]+|\x27[A-Za-z0-9_-]+\x27|"[A-Za-z0-9_-]+")\s*', line)
        if not match or match[1] not in keys or match[1] in values:
            raise ValueError()
        values[match[1]] = match[2].strip('\x27"')
    if set(values) != set(keys):
        raise ValueError()
    if (not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', values[keys[0]])
            or not re.fullmatch(r'[a-f0-9]{32}', values[keys[1]])
            or any(not re.fullmatch(r'[A-Za-z0-9_-]{32,4096}', values[k]) for k in keys[2:])):
        raise ValueError()
except Exception:
    sys.exit('PG03 BLOCKED: unsafe, conflicting or invalid PG03 environment file')
for key in keys:
    print(key + '=' + values[key])
PYPG03ENV
)" || return 1
  while IFS='=' read -r name value; do
    printf -v "$name" '%s' "$value"
    export "$name"
  done <<< "$metadata"
  unset metadata value
}

pg03_configure() {
  case "$POOL_MODE" in off|pg03) ;; *) echo 'REFUSING: --pool-mode must be off or pg03' >&2; return 1 ;; esac
  if [ "$LEGACY_TRANSITION" = 1 ]; then
    [ "$POOL_MODE" = pg03 ] && [ "$VERIFY_ONLY$RESTART_ONLY$RECONCILE_ONLY" = 000 ] \
      && [ "$MIGRATION_COMPATIBLE" = false ] && [[ "$WORKFLOW_RUN" =~ ^[1-9][0-9]*$ ]] \
      && [ "$APP_DIR" = /opt/clearapi/app ] && [ "$SERVICE" = new-api ] \
      || { echo 'REFUSING: legacy transition is an explicitly reviewed PG03 maintenance deploy only' >&2; return 1; }
    RUNTIME_PROBE_TOKEN_FILE=/opt/clearapi/secrets/legacy-transition-session
  fi
  # Do not silently use off semantics on an existing PG03 manifest.
  if [ -f "$MANIFEST" ]; then
    python3 -I - "$MANIFEST" "$POOL_MODE" <<'PY'
import json, sys
try:
    with open(sys.argv[1]) as f:
        mode = json.load(f).get('releases', {}).get('current', {}).get('pool_mode', 'off')
    if mode not in ('off', 'pg03') or (mode == 'pg03' and sys.argv[2] != 'pg03'):
        raise ValueError()
except Exception:
    sys.exit('REFUSING: invalid manifest mode or unapproved PG03 mode exit')
PY
    [ "$?" = 0 ] || return 1
  fi
  [ "$POOL_MODE" = pg03 ] || return 0
  if [ "$VERIFY_ONLY" = 0 ] && [ "$RESTART_ONLY" = 0 ] && [ "$RECONCILE_ONLY" = 0 ]; then
    [ -n "$DIGEST" ] && [ -n "$COMMIT" ] || { echo 'PG03 BLOCKED: --digest and --commit are required' >&2; return 1; }
    printf '%s' "$DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' \
      || { echo 'PG03 BLOCKED: invalid Gateway digest' >&2; return 1; }
    printf '%s' "$COMMIT" | grep -Eq '^[0-9a-f]{40}$' \
      || { echo 'PG03 BLOCKED: invalid Gateway commit' >&2; return 1; }
    [ "$MIGRATION_COMPATIBLE" = true ] || [ "$MIGRATION_COMPATIBLE" = false ] \
      || { echo 'PG03 BLOCKED: invalid migration compatibility flag' >&2; return 1; }
  fi
  [ "$(id -u)" = 0 ] || { echo 'PG03 requires root; no sudo environment policy changes are allowed' >&2; return 1; }
  [ -z "$MIGRATION_FLAGS" ] || { echo 'PG03 BLOCKED: schema migration flags are forbidden' >&2; return 1; }
  [ "$VERIFY_BOOTSTRAP" = true ] || { echo 'PG03 BLOCKED: bootstrap verification is mandatory' >&2; return 1; }
  [ "$SERVICE" = new-api ] || { echo 'PG03 BLOCKED: audited overlay requires new-api' >&2; return 1; }
  pg03_load_environment || return 1
  for name in CLEARAPI_POOL_EXECUTOR_ID CLEARAPI_PG02_PROCESS_EPOCH CLEARAPI_PG03_POLICY_KEY CLEARAPI_PG02_GATEWAY_PROOF_KEY CLEARAPI_POOL_CLIPROXY_KEY; do
    [ -n "${!name:-}" ] || { echo "PG03 BLOCKED: missing operator variable $name" >&2; return 1; }
    export "$name"
  done
  python3 -I - <<'PY'
import os, re, sys
try:
    if (not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', os.environ['CLEARAPI_POOL_EXECUTOR_ID'])
            or not re.fullmatch(r'[a-f0-9]{32}', os.environ['CLEARAPI_PG02_PROCESS_EPOCH'])):
        raise ValueError()
    for key in ('CLEARAPI_PG03_POLICY_KEY', 'CLEARAPI_PG02_GATEWAY_PROOF_KEY', 'CLEARAPI_POOL_CLIPROXY_KEY'):
        if not re.fullmatch(r'[A-Za-z0-9_-]{32,4096}', os.environ[key]):
            raise ValueError()
except Exception:
    sys.exit('PG03 BLOCKED: invalid operator identity or credential format')
PY
  [ "$?" = 0 ] || return 1
  if [ "$VERIFY_ONLY" = 1 ] || [ "$RESTART_ONLY" = 1 ] || [ "$RECONCILE_ONLY" = 1 ]; then
    pg03_load_pair "$MANIFEST" current || return 1
  else
    [ -n "$PG03_TARGET_MANIFEST" ] || { echo 'PG03 BLOCKED: operator-approved target manifest is required' >&2; return 1; }
    pg03_load_pair "$PG03_TARGET_MANIFEST" target || return 1
    if [ -n "$DIGEST" ] && [ -n "$COMMIT" ]; then
      [ "${IMAGE_BASE}@${DIGEST}" = "$PG03_GATEWAY_REF" ] && [ "$COMMIT" = "$PG03_GATEWAY_REVISION" ] \
        || { echo 'PG03 BLOCKED: Gateway target digest/revision differs from approved manifest' >&2; return 1; }
    fi
  fi
  if [ "$VERIFY_ONLY" = 0 ] && [ "$RESTART_ONLY" = 0 ] && [ "$RECONCILE_ONLY" = 0 ]; then
    PG03_EXECUTOR_IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$PG03_EXECUTOR_REF" 2>/dev/null)" || return 1
  fi
}

pg03_verify_image() { # local-only inspection; never pull or start an Executor
  docker image inspect "$1" 2>/dev/null | python3 -I -c '
import json, sys
try:
    images = json.load(sys.stdin)
    if len(images) != 1: raise ValueError()
    image = images[0]
    if (image["Id"] != sys.argv[2] or sys.argv[1] not in image.get("RepoDigests", [])
            or image.get("Os") != "linux" or image.get("Architecture") != "amd64"
            or image["Config"]["Labels"]["org.opencontainers.image.revision"] != sys.argv[3]):
        raise ValueError()
except Exception:
    sys.exit("PG03 BLOCKED: local component digest/platform/revision/image identity mismatch")
' "$1" "$2" "$3"
}

pg03_executor_gate() {
  local all_ids cid before after binary
  all_ids="$(docker ps -a --format '{{.ID}} {{.Names}} {{.Label "com.docker.compose.service"}}' 2>/dev/null \
    | python3 -I -c '
import re, sys
matches = []
for line in sys.stdin:
    fields = line.split()
    if len(fields) >= 2 and (re.search(r"executor|route.?chain", fields[1], re.I)
                            or (len(fields) > 2 and fields[2] == "pool-executor")):
        matches.append(fields[0])
if len(matches) != 1: sys.exit("PG03 BLOCKED: exactly one prepared Executor is required (including stopped containers)")
print(matches[0])
')" || return 1
  cid="$(compose ps -a -q pool-executor 2>/dev/null)" || return 1
  [[ "$cid" =~ ^[a-f0-9]{64}$ ]] && [[ "$cid" = "$all_ids"* ]] \
    || { log 'PG03 BLOCKED: prepared Executor is missing, duplicated or outside the Compose project'; return 1; }
  pg03_verify_image "$PG03_EXECUTOR_REF" "$PG03_EXECUTOR_IMAGE_ID" "$PG03_EXECUTOR_REVISION" || return 1
  before="$(docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}}' "$cid" 2>/dev/null)" || return 1
  docker inspect "$cid" 2>/dev/null | python3 -I -c '
import json, os, re, sys
try:
    containers = json.load(sys.stdin)
    if len(containers) != 1: raise ValueError()
    c = containers[0]
    env = dict(item.split("=", 1) for item in c["Config"]["Env"])
    if (c["Id"] != sys.argv[1] or c["Image"] != sys.argv[2]
            or c["Config"]["Image"] != sys.argv[3] or not c["State"]["Running"]
            or c["State"].get("Restarting") or c["State"].get("Health", {}).get("Status") != "healthy"
            or c["Config"]["Labels"]["com.docker.compose.service"] != "pool-executor"
            or c["HostConfig"].get("NetworkMode") == "host"
            or c["HostConfig"].get("PortBindings")
            or any(c["NetworkSettings"].get("Ports", {}).values())
            or sys.argv[4] not in c["NetworkSettings"]["Networks"]
            or c["Config"].get("Entrypoint") != ["/CLIProxyAPI/CLIProxyAPI"]
            or c["Config"].get("Cmd") != ["--config", "/CLIProxyAPI/config.yaml", "--local-model"]
            or env.get("CLEARAPI_POOL_ROLE") != "service"
            or env.get("CLEARAPI_POOL_EXECUTOR_ID") != os.environ["CLEARAPI_POOL_EXECUTOR_ID"]
            or env.get("CLEARAPI_PG02_PROCESS_EPOCH") != os.environ["CLEARAPI_PG02_PROCESS_EPOCH"]):
        raise ValueError()
    for key in ("CLEARAPI_PG03_POLICY_KEY", "CLEARAPI_PG02_GATEWAY_PROOF_KEY", "MANAGEMENT_PASSWORD"):
        if not re.fullmatch(r"[A-Za-z0-9_-]{32,4096}", env.get(key, "")): raise ValueError()
    mounts = {m["Destination"]: m for m in c["Mounts"]}
    for dst, src, rw in (
        ("/CLIProxyAPI/config.yaml", "/opt/clearapi/config/pg03/executor.yaml", False),
        ("/var/lib/pool-executor/auths", "/data/clearapi/pool-executor/auths", True),
        ("/var/lib/pool-executor/pool", "/data/clearapi/pool-executor/pool", True),
        ("/var/lib/pool-executor/receipts", "/data/clearapi/pool-executor/receipts", True)):
        m = mounts[dst]
        if m["Type"] != "bind" or m["Source"] != src or m["RW"] is not rw: raise ValueError()
except Exception:
    sys.exit("PG03 BLOCKED: prepared Executor runtime identity/configuration mismatch")
' "$cid" "$PG03_EXECUTOR_IMAGE_ID" "$PG03_EXECUTOR_REF" "$NETWORK_NAME" || return 1
  # Hash the running PID 1 executable, not a potentially different on-disk binary.
  binary="$(docker exec "$cid" sha256sum /proc/1/exe 2>/dev/null)" || return 1
  [ "$binary" = "$PG03_EXECUTOR_BINARY  /proc/1/exe" ] || { log 'PG03 BLOCKED: running Executor binary mismatch'; return 1; }
  after="$(docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}}' "$cid" 2>/dev/null)" || return 1
  [ "$before" = "$after" ] || { log 'PG03 BLOCKED: Executor changed during validation'; return 1; }
  PG03_EXECUTOR_CONTAINER="$cid"
}

pg03_compose_gate() {
  compose config --format json 2>/dev/null | python3 -I -c '
import json, re, sys
try:
    c = json.load(sys.stdin)
    g, e = c["services"]["new-api"], c["services"]["pool-executor"]
    ge, ee = g["environment"], e["environment"]
    if (e["image"] != sys.argv[1] or e.get("ports") or e.get("network_mode") == "host"
            or "internal" not in e["networks"] or "internal" not in g["networks"]
            or "pool-executor" in (g.get("depends_on") or {}) or ge.get("CLEARAPI_POOL_ENABLED") != "true"
            or ge.get("CLEARAPI_POOL_CLIPROXY_URL") != "http://pool-executor:8317"):
        raise ValueError()
    for env, names in ((ge, ("CLEARAPI_POOL_CLIPROXY_KEY", "CLEARAPI_PG03_POLICY_KEY", "CLEARAPI_PG02_GATEWAY_PROOF_KEY")),
                       (ee, ("MANAGEMENT_PASSWORD", "CLEARAPI_PG03_POLICY_KEY", "CLEARAPI_PG02_GATEWAY_PROOF_KEY"))):
        for k in names:
            if not re.fullmatch(r"[A-Za-z0-9_-]{32,4096}", env.get(k, "")): raise ValueError()
except Exception:
    sys.exit("PG03 BLOCKED: effective overlay or credential export chain invalid")
' "$PG03_EXECUTOR_REF"
}

pg03_runtime_gate() { # mandatory paired Gateway/Executor checks; no application dispatch
  [ "$POOL_MODE" = pg03 ] || return 0
  local cid before after
  local expected_gateway_ref="$PG03_GATEWAY_REF" expected_gateway_id="$PG03_GATEWAY_IMAGE_ID"
  local expected_gateway_revision="$PG03_GATEWAY_REVISION" expected_executor_id="$PG03_EXECUTOR_IMAGE_ID"
  pg03_load_pair "$MANIFEST" current || return 1
  [ "$PG03_GATEWAY_REF" = "$expected_gateway_ref" ] && [ "$PG03_GATEWAY_IMAGE_ID" = "$expected_gateway_id" ] \
    && [ "$PG03_GATEWAY_REVISION" = "$expected_gateway_revision" ] && [ "$PG03_EXECUTOR_IMAGE_ID" = "$expected_executor_id" ] \
    || { log 'PG03 BLOCKED: live component manifest differs from approved target pair'; return 1; }
  pg03_executor_gate || return 1
  pg03_compose_gate || return 1
  pg03_verify_image "$PG03_GATEWAY_REF" "$PG03_GATEWAY_IMAGE_ID" "$PG03_GATEWAY_REVISION" || return 1
  [ "$(compose_effective_image)" = "$PG03_GATEWAY_REF" ] || { log 'PG03 BLOCKED: effective Gateway digest differs from component manifest'; return 1; }
  cid="$(service_id)" || return 1
  before="$(docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}}' "$cid" 2>/dev/null)" || return 1
  docker inspect "$cid" 2>/dev/null | python3 -I -c '
import json, re, sys
try:
    cs = json.load(sys.stdin)
    if len(cs) != 1: raise ValueError()
    c = cs[0]; env = dict(item.split("=", 1) for item in c["Config"]["Env"])
    if (c["Id"] != sys.argv[3] or c["Image"] != sys.argv[1] or c["Config"]["Image"] != sys.argv[2]
            or not c["State"]["Running"] or c["State"].get("Restarting")
            or sys.argv[4] not in c["NetworkSettings"]["Networks"]
            or env.get("CLEARAPI_POOL_ENABLED") != "true"
            or env.get("CLEARAPI_POOL_CLIPROXY_URL") != "http://pool-executor:8317"):
        raise ValueError()
    for k in ("CLEARAPI_POOL_CLIPROXY_KEY", "CLEARAPI_PG03_POLICY_KEY", "CLEARAPI_PG02_GATEWAY_PROOF_KEY"):
        if not re.fullmatch(r"[A-Za-z0-9_-]{32,4096}", env.get(k, "")): raise ValueError()
except Exception:
    sys.exit("PG03 BLOCKED: Gateway runtime identity or required credential presence invalid")
' "$PG03_GATEWAY_IMAGE_ID" "$PG03_GATEWAY_REF" "$cid" "$NETWORK_NAME" || return 1
  # wget is already part of the Gateway image. Authentication stays in stdin,
  # never argv; do not print management responses, follow redirects or proxies.
  docker exec "$cid" /bin/sh -c '
    set +x
    test -n "$CLEARAPI_POOL_CLIPROXY_KEY" || exit 1
    wget --no-proxy --max-redirect=0 --timeout=5 --tries=1 -q -O /dev/null http://pool-executor:8317/healthz || exit 1
    printf "header = X-Management-Key: %s\n" "$CLEARAPI_POOL_CLIPROXY_KEY" |
      wget --config=/dev/stdin --no-proxy --max-redirect=0 --timeout=5 --tries=1 -q -O /dev/null http://pool-executor:8317/v0/management/auth-files
  ' >/dev/null 2>&1 || { log 'PG03 BLOCKED: Gateway to Executor health/authenticated connection unavailable'; return 1; }
  after="$(docker inspect --format '{{.Id}} {{.State.StartedAt}} {{.Image}}' "$cid" 2>/dev/null)" || return 1
  [ "$before" = "$after" ] || { log 'PG03 BLOCKED: Gateway changed during connection check'; return 1; }
  pg03_executor_gate || return 1
  log 'PG03 Runtime Gate PASS (paired digests, identity, count, secret presence and Gateway connection)'
}

pg03_mark_failed() {
  [ -n "$PG03_ATTEMPT_ID" ] || return 0
  python3 -I - "$MANIFEST" "$PG03_ATTEMPT_ID" <<'PY'
import json, os, sys, tempfile
try:
    path, attempt = sys.argv[1:]
    with open(path) as f: m = json.load(f)
    c = m['releases']['current']
    if c.get('deployment_attempt', {}).get('id') != attempt:
        sys.exit(0)
    c['status'] = 'failed'
    c['deployment_attempt']['status'] = 'failed'
    c['validation']['result'] = 'blocked'
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix='.pg03-failed-')
    with os.fdopen(fd, 'w') as f:
        json.dump(m, f, indent=2); f.write('\n'); f.flush(); os.fsync(f.fileno())
    os.replace(tmp, path)
except Exception:
    sys.exit('PG03 failure status persistence unavailable')
PY
}

pg03_configure || { echo 'REFUSING: deployment mode prerequisites failed; no deployment performed' >&2; exit 2; }
if [ "$POOL_MODE" = pg03 ]; then
  trap 'die "PG03 command failed; manual review required"' ERR
  pg03_executor_gate && pg03_compose_gate || die 'PG03 prepared Executor/overlay gate BLOCKED'
fi

# Verification mode: check identity and Runtime Report v1, audit, and exit.
# Kept after helper definitions and before step 1 so it never touches the deploy lock, the
# backup subsystem, or replace any container. The binary digest check uses a
# temporary networkless, read-only container and does not start the application.
[ "$((VERIFY_ONLY + RESTART_ONLY + RECONCILE_ONLY))" -le 1 ] || die 'choose verify, restart or reconcile'
[[ "$RUNTIME_IDENTITY_DIR" =~ ^/[a-zA-Z0-9_./-]+$ ]] || die 'unsafe runtime identity directory'
if [ "$RECONCILE_ONLY" = 1 ]; then
  mkdir -p /run/lock
  exec 9>/run/lock/clearapi-production-deploy.lock
  flock -n 9 || die 'deploy lock held; identity reconcile blocked'
  verify_running_identity || die 'reconcile production image drift'
  APPROVED_METADATA="$(python3 - "$MANIFEST" <<'PY'
import json, re, sys
with open(sys.argv[1]) as f:
    current = json.load(f)['releases']['current']
ref, revision, digest = current['image_ref'], current['candidate_commit'], current['image_digest']
if (not re.fullmatch(r'[^\s@]+@sha256:[a-f0-9]{64}', ref)
        or not re.fullmatch(r'[a-f0-9]{40}', revision)
        or ref.rsplit('@', 1)[1] != digest or current.get('status') != 'success'):
    raise SystemExit('Approved release metadata unavailable')
print(ref, revision)
PY
)" || die 'reconcile approved release metadata unavailable'
  read -r APPROVED_REF APPROVED_REVISION <<< "$APPROVED_METADATA"
  [ "$(compose_effective_image)" = "$APPROVED_REF" ] || die 'reconcile approved digest drift'
  refresh_runtime_identity "$(service_id)" "$APPROVED_REF" "$APPROVED_REVISION" || die 'identity reconcile BLOCKED'
  verify_running_identity || die 'reconcile container changed'
  verify_runtime_gate before "$PREV_IMAGE_ID" || die 'Runtime Gate BLOCKED after reconcile'
  pg03_runtime_gate || die 'PG03 Runtime Gate BLOCKED after reconcile'
  audit 'reconcile' 'success' "image=${PREV_IMAGE_ID}"
  exit 0
fi
if [ "$VERIFY_ONLY" = "1" ]; then
  if verify_running_identity; then
    if ! verify_runtime_gate before "$PREV_IMAGE_ID"; then
      audit "verify" "blocked" 'Runtime Integrity Gate BLOCKED'
      exit 1
    fi
    pg03_runtime_gate || die 'PG03 Runtime Gate BLOCKED during verification'
    audit "verify" "ok" "running == compose effective == manifest current; Runtime Gate PASS"
    exit 0
  fi
  audit "verify" "drift" "identity check failed; no changes were made"
  exit 1
fi


# Deploy-only validation stays strict; verification needs no candidate metadata.
[ -n "$DIGEST" ] && [ -n "$COMMIT" ] || { echo "--digest and --commit are required" >&2; exit 2; }
printf '%s' "$DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$' || {
  echo "REFUSING: --digest must be an exact 'sha256:<64 hex>' reference (got: ${DIGEST})" >&2; exit 2; }
printf '%s' "$COMMIT" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "REFUSING: --commit must be a full 40-hex commit SHA" >&2; exit 2; }
[ "$MIGRATION_COMPATIBLE" = "true" ] || [ "$MIGRATION_COMPATIBLE" = "false" ] || {
  echo "REFUSING: --migration-compatible must be true or false" >&2; exit 2; }

# ===========================================================================
# 1. acquire deploy lock
# ===========================================================================
mkdir -p /run/lock
exec 9>"/run/lock/clearapi-production-deploy.lock"
flock -n 9 || die "deploy lock held: another production deploy is in progress"
log "step 1: deploy lock acquired"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/clearapi-deploy.XXXXXXXX")"
if [ "$LEGACY_TRANSITION" = 1 ]; then
  TRANSITION_STATUS="$(legacy_transition_phase status)" || die 'maintenance authorization/state BLOCKED'
  if [ "$TRANSITION_STATUS" = completed ]; then
    audit legacy_transition_replay no_action 'previous one-use transition completed; no new Runtime PASS claimed'
    exit 0
  fi
  [ "$TRANSITION_STATUS" = new ] || die 'unknown maintenance journal state'
fi

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
verify_launch_baseline
verify_stop_policy
if [ "$LEGACY_TRANSITION" = 1 ]; then
  legacy_transition_phase preflight || die 'legacy maintenance preflight BLOCKED; old Runtime remains FAIL'
  audit legacy_transition_preflight old_runtime_blocked 'APPROVAL_VIEW_INVALID; separate maintenance authorization; never a Runtime PASS'
else
  verify_runtime_gate before "$PREV_IMAGE_ID" || die 'Runtime Integrity Gate BLOCKED before deployment'
fi
if [ "$RESTART_ONLY" = 1 ]; then
  CURRENT_REF="$(compose_effective_image)"
  CURRENT_REVISION="$(sudo docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$PREV_IMAGE_ID")"
  [ "$CURRENT_REF" = "${IMAGE_BASE}@${DIGEST}" ] && [ "$CURRENT_REVISION" = "$COMMIT" ] \
    || die 'restart metadata does not match running release'
  if [ "$POOL_MODE" = pg03 ]; then
    pg03_runtime_gate || die 'PG03 Runtime Gate BLOCKED before restart'
    compose restart --no-deps --timeout 150 "$SERVICE" || die 'controlled restart failed'
  else
    compose restart --timeout 150 "$SERVICE" || die 'controlled restart failed'
  fi
  refresh_runtime_identity "$(service_id)" "$CURRENT_REF" "$COMMIT" || die 'restart identity refresh failed'
  verify_running_identity || die 'restart image identity drift'
  verify_runtime_gate after "$PREV_IMAGE_ID" || die 'Runtime Gate BLOCKED after restart'
  pg03_runtime_gate || die 'PG03 Runtime Gate BLOCKED after restart'
  audit 'restart' 'success' "digest=${DIGEST}"
  exit 0
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
if [ "$LEGACY_TRANSITION" = 1 ]; then
  legacy_transition_phase backup "$BACKUP_STARTED_AT" || die 'fresh encrypted maintenance backup BLOCKED'
fi

# ===========================================================================
# 6. pull exact digest
# ===========================================================================
IMAGE_REF="${IMAGE_BASE}@${DIGEST}"
# Ephemeral, isolated pull credential: a throwaway DOCKER_CONFIG keeps the
# token out of /root/.docker/config.json so nothing long-lived is left behind.
cleanup_ephemeral() {
  if [ -n "$EPHEMERAL_DOCKER_CONFIG" ] && [ -d "$EPHEMERAL_DOCKER_CONFIG" ]; then
    rm -rf "$EPHEMERAL_DOCKER_CONFIG"
  fi
}

if [ "$GHCR_TOKEN_STDIN" = "1" ]; then
  EPHEMERAL_DOCKER_CONFIG="$(mktemp -d)"
  chmod 0700 "$EPHEMERAL_DOCKER_CONFIG"
  export DOCKER_CONFIG="$EPHEMERAL_DOCKER_CONFIG"
  if ! printf '%s' "$GHCR_PULL_TOKEN" | docker login ghcr.io -u "$GHCR_USER" --password-stdin; then
    unset GHCR_PULL_TOKEN
    die "step 6: GHCR login failed (ephemeral job credential)"
  fi
  unset GHCR_PULL_TOKEN
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
if [ "$POOL_MODE" = pg03 ]; then
  # The target manifest approves the exact Gateway ref/revision. The local
  # Docker image ID is bound only after the pull, then recorded in the release
  # manifest; a remote daemon ID must not be trusted as a target assertion.
  PG03_GATEWAY_IMAGE_ID="$CANDIDATE_IMAGE_ID"
  pg03_verify_image "$IMAGE_REF" "$PG03_GATEWAY_IMAGE_ID" "$PG03_GATEWAY_REVISION" \
    || die "step 7: PG03 Gateway digest/provenance verification failed"
fi
log "step 7: digest and revision label verified"
CANDIDATE_OVERRIDE="$WORK_DIR/compose-jobs.yaml"
printf 'services:\n  %s:\n    image: %s\n  %s:\n    image: %s\n' \
  "$SERVICE" "$IMAGE_REF" "$BOOTSTRAP_SERVICE" "$IMAGE_REF" > "$CANDIDATE_OVERRIDE"
candidate_compose --profile bootstrap config --format json | python3 -c '
import json, sys
services=json.load(sys.stdin)["services"]
if any(services[s].get("image") != sys.argv[3] for s in sys.argv[1:3]):
    sys.exit("candidate job image mismatch")
' "$SERVICE" "$BOOTSTRAP_SERVICE" "$IMAGE_REF" || die "candidate job image mismatch"

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
if [ "$LEGACY_TRANSITION" = 1 ]; then
  docker run --rm --network none --read-only --entrypoint /bin/sh "$IMAGE_REF" -c 'test -x /legacy-store-retire' \
    || die 'verified retirement binary missing from candidate'
fi

# ===========================================================================
# 9/10. migration gate (preflight; run only when flags are configured)
# ===========================================================================
if [ -z "$MIGRATION_FLAGS" ]; then
  log "step 9: migration preflight — binaries and DSN path verified in steps 7/8; no approval flags configured"
  log "step 10: DEPLOY_MIGRATION_FLAGS empty in deploy.env — no schema change expected for a zero-change candidate; skipping migration run"
  audit "migration_gate" "preflight_only" "no flags configured; no schema change performed"
else
  candidate_compose run --rm --no-deps --pull never --entrypoint /bin/sh -e MIGRATION_FLAGS="$MIGRATION_FLAGS" "$SERVICE" -c '
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
  candidate_compose --profile bootstrap run --rm --no-deps --pull never "$BOOTSTRAP_SERVICE" --verify-only \
    || die "step 11: production-bootstrap --verify-only failed; deployment BLOCKED"
  log "step 11: bootstrap verify-only PASS"
else
  log "step 11: bootstrap verify disabled by deploy.env (DEPLOY_VERIFY_BOOTSTRAP)"
fi

# ===========================================================================
# 12/13. record old image + update release manifest (merge into live schema)
# ===========================================================================
if [ "$LEGACY_TRANSITION" = 1 ]; then
  # All executable/source, encrypted backup, bootstrap and existing baseline
  # checks above are still mandatory. Consume once BEFORE quiescing the writer.
  legacy_transition_phase begin || die 'maintenance one-use entry BLOCKED'
  LEGACY_TRANSITION_ENTERED=1
  set -E
  trap 'die "unexpected maintenance command failure"' ERR
  trap 'die "maintenance interrupted; preserve stopped state"' INT TERM HUP
  legacy_transition_phase isolate || die 'public writer isolation failed'
  compose stop --timeout 150 "$SERVICE" || die 'cannot stop old Gateway writer'
  legacy_transition_phase retire || die 'retirement failed or commit uncertain; preserve maintenance state'
fi
STAMP="$(TS_NOW)"
PRE_DEPLOY_MANIFEST="${MANIFEST}.bak.$(date -u +%Y%m%dT%H%M%SZ).$$"
cp -p "$MANIFEST" "$PRE_DEPLOY_MANIFEST"
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
if [ "$POOL_MODE" = pg03 ]; then PG03_ATTEMPT_ID="$(python3 -I -c 'import uuid; print(uuid.uuid4())')"; fi
python3 - "$MANIFEST" "$MANIFEST_TMP" "$COMMIT" "$IMAGE_REF" "$DIGEST" "$CANDIDATE_IMAGE_ID" \
  "$PREV_IMAGE_ID" "$WORKFLOW_RUN" "$MIGRATION_VERSION" "$STAMP" "$POOL_MODE" \
  "$PG03_EXECUTOR_REF" "$PG03_EXECUTOR_IMAGE_ID" "$PG03_EXECUTOR_REVISION" "$PG03_EXECUTOR_BINARY" \
  "$PG03_EXECUTOR_CONTAINER" "$PG03_ATTEMPT_ID" "$PG03_APPROVAL_SHA256" "$PG03_TARGET_MANIFEST" <<'PYEOF'
import hashlib, json, os, sys
src, dst, commit, image_ref, digest, image_id, prev_id, run_id, migration_version, stamp = sys.argv[1:11]
pool_mode, executor_ref, executor_id, executor_revision, executor_binary, executor_container, attempt_id, approval_sha = sys.argv[11:19]
try:
    with open(src) as f:
        m = json.load(f)
except Exception:
    m = {"schema_version": 1, "releases": {}}
rel = m.setdefault("releases", {})
old_current = rel.get("current") or {}
new_current = {
    "tag": image_ref,
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
if pool_mode == "pg03":
    new_current["pool_mode"] = "pg03"
    new_current["components"] = {
        "gateway": {"image_ref": image_ref, "image_digest": digest,
                    "image_id": image_id, "oci_revision": commit},
        "executor": {"image_ref": executor_ref, "image_digest": executor_ref.split("@", 1)[1],
                     "image_id": executor_id, "oci_revision": executor_revision,
                     "binary_sha256": executor_binary, "container_id": executor_container,
                     "executor_id": os.environ['CLEARAPI_POOL_EXECUTOR_ID'],
                     "process_epoch": os.environ['CLEARAPI_PG02_PROCESS_EPOCH']},
    }
    new_current["deployment_attempt"] = {
        "id": attempt_id, "status": "pending", "executor_start": "forbidden"}
    new_current["validation"] = {
        "approval": "passed", "manifest_sha256": approval_sha,
        "executor_prepared": True, "secrets": "presence-only",
        "gateway_executor_connection": "pending"}
    with open(sys.argv[19], 'rb') as f:
        target_raw = f.read(131073)
    if hashlib.sha256(target_raw).hexdigest() != approval_sha:
        raise SystemExit('PG03 target changed before recording deployment')
    authority = json.loads(target_raw)['validation'].get('authorization')
    if authority is not None:
        del new_current['validation']['approval']
        new_current['validation']['authorization'] = authority
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
# PG03 never asks Compose to start the Executor; it was checked before this
# point and the overlay has no dependency that could implicitly start it.
# Explicit timeout also protects the OLD container on the first rollout;
# its persisted stop timeout may predate the new Compose grace policy.
REPLACE_STARTED=1
if [ "$POOL_MODE" = pg03 ]; then
  pg03_executor_gate && pg03_compose_gate || die 'PG03 Executor/overlay changed before Gateway replacement'
  compose up -d --no-deps --timeout 150 "$SERVICE" || die "step 14: compose replace failed (runtime state may have changed)"
else
  compose up -d --timeout 150 "$SERVICE" || die "step 14: compose replace failed (runtime state may have changed)"
fi
# Hard identity gate: prove the running container actually IS the candidate
# image before any health check can call this deploy a success. compose may
# legitimately no-op when it believes nothing changed; without this check a
# no-op would sail through health checks on the OLD image and be recorded as
# a successful deploy (exactly what run 37762896277 did).
REPLACED_ID="$(sudo docker inspect --format '{{.Image}}' "$(service_id)")"
[ "$REPLACED_ID" = "$CANDIDATE_IMAGE_ID" ] \
  || die "step 14: replace did not take effect — running image ${REPLACED_ID} != candidate ${CANDIDATE_IMAGE_ID}"
refresh_runtime_identity "$(service_id)" "$IMAGE_REF" "$COMMIT" || die 'deploy identity refresh failed'
log "step 14: container replaced with ${IMAGE_REF} (running image verified: ${REPLACED_ID})"

# ===========================================================================
# 15. localhost health
# ===========================================================================
health_retry "$LOCAL_HEALTH_URL" 30 2 || die "step 15: localhost health (${LOCAL_HEALTH_URL}) failed"
log "step 15: localhost health PASS"

# ===========================================================================
# 16. public health via Cloudflare
# ===========================================================================
if [ "$LEGACY_TRANSITION" = 0 ]; then
  health_retry "$PUBLIC_HEALTH_URL" 10 3 "CF-Ray" || die "step 16: public health (${PUBLIC_HEALTH_URL}) with CF-Ray failed"
  log "step 16: public HTTPS health via Cloudflare PASS"
fi

# ===========================================================================
# 17. runtime negative gates
# ===========================================================================
NEW_ID="$(service_id)"
[ -n "$NEW_ID" ] || die "step 17: ${SERVICE} container missing after replace"
cenv() { compose exec -T "$SERVICE" printenv "$1"; }

for pair in $EXPECT_ENV_KEYS; do
  key="${pair%%=*}"; expected="${pair#*=}"
  actual="$(cenv "$key")" || die "step 17: required env key ${key} unavailable"
  [ "$actual" = "$expected" ] || die "step 17: env gate ${key}='${actual}' expected '${expected}'"
done
verify_launch_baseline
verify_stop_policy
verify_runtime_gate after "$CANDIDATE_IMAGE_ID" || die 'Runtime Integrity Gate BLOCKED after replacement'
pg03_runtime_gate || die 'PG03 Runtime Gate BLOCKED after replacement'

PORTS_3000="$(sudo docker port "$NEW_ID" 3000/tcp 2>/dev/null || true)"
[ "$PORTS_3000" = "127.0.0.1:3000" ] || die "step 17: new-api must bind 127.0.0.1:3000 only (got: ${PORTS_3000:-<unpublished>})"

PG_ID="$(compose ps -q "$PG_SERVICE")"
[ -n "$PG_ID" ] || die "step 17: postgres container missing"
PG_PORTS="$(sudo docker port "$PG_ID" 2>/dev/null || true)"
[ -z "$PG_PORTS" ] || die "step 17: postgres must not publish ports (got: ${PG_PORTS})"

NETWORKS="$(sudo docker inspect --format '{{range $k, $_ := .NetworkSettings.Networks}}{{$k}} {{end}}' "$NEW_ID")"
printf '%s' "$NETWORKS" | grep -qw "$NETWORK_NAME" || die "step 17: new-api not attached to ${NETWORK_NAME} (networks: ${NETWORKS})"

if [ "$POOL_MODE" = off ]; then
  EXECUTORS="$(sudo docker ps -a --format '{{.Names}}' | grep -Eic 'executor|route.?chain' || true)"
  [ "$EXECUTORS" = "0" ] || die "step 17: ${EXECUTORS} executor/route-chain container(s) present; Pool must stay OFF"
else
  pg03_runtime_gate || die "step 17: PG03 Runtime Gate failed"
fi

[ -f "$CADDYFILE" ] || die "step 17: Caddyfile ${CADDYFILE} missing"
CADDY_MD5="$(md5sum "$CADDYFILE" | cut -d' ' -f1)"
[ "$CADDY_MD5" = "$CADDYFILE_MD5" ] || die "step 17: Caddyfile md5 ${CADDY_MD5} does not match frozen edge baseline ${CADDYFILE_MD5}"

systemctl is-active --quiet "$BACKUP_TIMER" || die "step 17: ${BACKUP_TIMER} not active after deploy"
log "step 17: runtime negative gates PASS (env, binding, PG ports, network, pool, Caddyfile, backup timer)"

# ===========================================================================
# 18. billing invariants (read-only, least-privileged app role)
# ===========================================================================
if [ "$LAUNCH_BASELINE" = commercial ]; then
  verify_commercial_billing
fi
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
  # Product emptiness is proven by the versioned runtime view, never by a
  # raw table count or a self-approved static product fingerprint.
  [ "$tbl" != store_products ] || continue
  cnt="$(psql_q "SELECT count(*) FROM ${tbl};")"
  [ "$cnt" = "0" ] || die "step 18: table ${tbl} row count=${cnt} expected 0"
done

POOL_ROWS="$(psql_q "SELECT count(*) FROM options WHERE key ILIKE '${POOL_OPTION_PATTERN}';")"
[ "$POOL_ROWS" = "0" ] || die "step 18: ${POOL_ROWS} pool option row(s) present; Pool must stay OFF"
log "step 18: billing invariants PASS (mode=enforce, needs_review set frozen, channels/user/token disabled, order tables empty, runtime product gate, no pool rows)"
if [ "$LEGACY_TRANSITION" = 1 ]; then
  legacy_transition_phase postflight || die 'new process Runtime, accounting or Pool invariants BLOCKED'
  legacy_transition_phase release-edge || die 'verified public edge release failed'
  health_retry "$PUBLIC_HEALTH_URL" 10 3 "CF-Ray" || die 'maintenance public HTTPS health with CF-Ray failed'
fi

# ===========================================================================
# 19. mark deployment success
# ===========================================================================
set_pg03_success || die 'PG03 success manifest update failed'
set_manifest_status "success"
if [ "$LEGACY_TRANSITION" = 1 ]; then
  legacy_transition_phase complete || die 'maintenance completion evidence could not be persisted'
fi
audit "deploy_success" "success" "digest=${DIGEST} previous_image=${PREV_IMAGE_ID}"
log "step 19: DEPLOYMENT SUCCESS"
log "  commit:      ${COMMIT}"
log "  digest:      ${DIGEST}"
log "  image ID:    ${CANDIDATE_IMAGE_ID}"
log "  previous:    ${PREV_IMAGE_ID}"
log "  manifest:    ${MANIFEST}"
log "PRODUCTION DEPLOY COMPLETED"
