#!/usr/bin/env bash
# Isolated image acceptance only; never connects to an operator database.
set -euo pipefail
IMAGE="${1:?image reference required}"
OUTPUT="${2:?evidence directory required}"
mkdir -p "$OUTPUT"
OUTPUT="$(cd "$OUTPUT" && pwd)"
ID="pg03-wrapper-${RANDOM}-$$"
NETWORK="${ID}-net"
PG="${ID}-pg"
GATEWAY="${ID}-gateway"
SECRETS="$(mktemp -d)"
cleanup() {
  docker rm -f "$GATEWAY" "$PG" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
  rm -rf "$SECRETS"
}
trap cleanup EXIT
chmod 0700 "$SECRETS"
printf 'postgres://postgres:isolated-wrapper-test@%s:5432/postgres?sslmode=disable' "$PG" > "$SECRETS/sql_dsn"
printf '%048d' 1 > "$SECRETS/session_secret"
printf '%048d' 2 > "$SECRETS/crypto_secret"
printf '%064d' 3 > "$SECRETS/token.key"
printf 'isolated-wrapper-test-password' > "$SECRETS/admin.password"
printf 'Isolated PG03 acceptance terms' > "$SECRETS/terms.txt"
printf 'Isolated PG03 acceptance privacy' > "$SECRETS/privacy.txt"
chmod 0600 "$SECRETS"/*
docker network create --internal "$NETWORK" >/dev/null
docker run -d --name "$PG" --network "$NETWORK" -e POSTGRES_PASSWORD=isolated-wrapper-test postgres:17.6 >/dev/null
ready=false
for _ in {1..60}; do
  if docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]]
docker exec "$PG" psql -U postgres -Atc 'SELECT version()' > "$OUTPUT/postgres-version.txt"
ENV_ARGS=(
  -e CLEARAPI_PRODUCTION=true -e GIN_MODE=release -e SESSION_COOKIE_SECURE=true
  -e CLEARAPI_DURABLE_WALLET=true -e CLEARAPI_BILLING_OUTBOX=true
  -e CLEARAPI_BILLING_RECONCILIATION_SINGLE_INSTANCE=true -e CLEARAPI_TASK_SETTLEMENT=true
  -e CLEARAPI_STRICT_PREPAID=true -e CLEARAPI_TOKEN_ENCRYPTION=true
  -e CLEARAPI_DEPLOYMENT_INSTANCES=1 -e CLEARAPI_ENABLE_REALTIME=false
  -e CLEARAPI_ENABLE_RESPONSES_WEBSOCKET=false
  -e DEBUG=false -e DIFY_DEBUG=false -e ENABLE_PPROF=false -e BATCH_UPDATE_ENABLED=false
  -e CLEARAPI_FINANCE_ENABLED=true -e TRUSTED_PROXIES=none
  -e SESSION_COOKIE_TRUSTED_URL=https://wrapper-test.invalid
  -e MAX_UPSTREAM_RESPONSE_MB=32 -e MAX_REQUEST_BODY_MB=32
  -e RELAY_CONCURRENCY_GLOBAL=32 -e RELAY_CONCURRENCY_USER=32
  -e RELAY_CONCURRENCY_TOKEN=32 -e RELAY_CONCURRENCY_IP=32
  -e CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE=/run/secrets/token.key
  -v "$SECRETS:/run/secrets:ro"
)
# Secret delivery, CR/LF normalization, exec/exit propagation; prints no secrets.
docker run --rm --network none --entrypoint /entrypoint-secure.sh "${ENV_ARGS[@]}" "$IMAGE" /bin/sh -c '
  test ${#SESSION_SECRET} -eq 48 && test ${#CRYPTO_SECRET} -eq 48
  test -n "$SQL_DSN" && test -f "$CLEARAPI_TOKEN_ENCRYPTION_KEY_FILE"
' > "$OUTPUT/entrypoint.log" 2>&1
set +e
docker run --rm --network none --entrypoint /entrypoint-secure.sh "$IMAGE" /bin/sh -c 'exit 37'
CODE=$?
set -e
[[ "$CODE" == 37 ]]
docker run --rm --network none --entrypoint /bin/sh "$IMAGE" -c '
  for b in /new-api /production-bootstrap /finance-migrate /billing-migrate /entrypoint-secure.sh; do test -x "$b" || exit 1; done
  sha256sum /new-api /production-bootstrap /finance-migrate /billing-migrate /entrypoint-secure.sh
' > "$OUTPUT/binary-sha256.txt"
for binary in finance-migrate billing-migrate; do
  set +e
  docker run --rm --network none --entrypoint "/$binary" "$IMAGE" > "$OUTPUT/$binary.log" 2>&1
  CODE=$?
  set -e
  [[ "$CODE" == 2 ]]
done
EXECUTE_ARGS=(--execute --admin-username root --admin-password-file /run/secrets/admin.password
  --terms-file /run/secrets/terms.txt --privacy-file /run/secrets/privacy.txt
  --terms-version isolated-v1 --privacy-version isolated-v1 --approved-by isolated-acceptance
  --candidate-commit "${SOURCE_COMMIT:-$(git rev-parse HEAD)}")
set +e
docker run --rm --network "$NETWORK" --entrypoint /entrypoint-secure.sh "${ENV_ARGS[@]}" "$IMAGE" /production-bootstrap "${EXECUTE_ARGS[@]}" > "$OUTPUT/bootstrap-stage1.log" 2>&1
CODE=$?
set -e
grep -q '"status":"finance_owner_configuration_required"' "$OUTPUT/bootstrap-stage1.log"
OWNER="$(docker exec "$PG" psql -U postgres -Atc "SELECT id FROM users WHERE username='root'")"
[[ "$OWNER" =~ ^[1-9][0-9]*$ ]]
ENV_ARGS+=(-e "CLEARAPI_FINANCE_OWNER_USER_ID=$OWNER")
docker run --rm --network "$NETWORK" --entrypoint /entrypoint-secure.sh "${ENV_ARGS[@]}" "$IMAGE" /production-bootstrap "${EXECUTE_ARGS[@]}" > "$OUTPUT/bootstrap-stage2.log" 2>&1
grep -q '"status":"bootstrapped"' "$OUTPUT/bootstrap-stage2.log"
# Enforce read-only SQL at the database session level, not just a code promise.
docker exec "$PG" psql -U postgres -v ON_ERROR_STOP=1 -c 'ALTER ROLE postgres SET default_transaction_read_only=on' >/dev/null
docker exec "$PG" pg_dump -U postgres --no-owner --no-privileges postgres | sed '/^\\\(un\)\?restrict /d' > "$OUTPUT/db-before.sql"
docker run --rm --network "$NETWORK" --entrypoint /entrypoint-secure.sh "${ENV_ARGS[@]}" "$IMAGE" /production-bootstrap --verify-only > "$OUTPUT/verify-only.log" 2>&1
grep -q '"status":"verified"' "$OUTPUT/verify-only.log"
! grep -q 'not_bootstrapped' "$OUTPUT/verify-only.log"
docker exec "$PG" pg_dump -U postgres --no-owner --no-privileges postgres | sed '/^\\\(un\)\?restrict /d' > "$OUTPUT/db-after.sql"
cmp "$OUTPUT/db-before.sql" "$OUTPUT/db-after.sql"
# Gateway startup uses the real production gate and remains on an internal network.
docker exec -e PGOPTIONS='-c default_transaction_read_only=off' "$PG" psql -U postgres -v ON_ERROR_STOP=1 -c 'ALTER ROLE postgres RESET default_transaction_read_only' >/dev/null
docker run -d --name "$GATEWAY" --network "$NETWORK" --entrypoint /entrypoint-secure.sh "${ENV_ARGS[@]}" "$IMAGE" /new-api >/dev/null
ready=false
for _ in {1..60}; do
  if docker exec "$GATEWAY" wget -qO- http://127.0.0.1:3000/api/status > "$OUTPUT/gateway-status.json" 2>/dev/null; then ready=true; break; fi
  sleep 1
done
docker logs "$GATEWAY" > "$OUTPUT/gateway-startup.log" 2>&1
[[ "$ready" == true ]]
python3 - "$OUTPUT" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
assert json.loads((p/'gateway-status.json').read_text())['success'] is True
assert (p/'db-before.sql').read_bytes() == (p/'db-after.sql').read_bytes()
result = dict(entrypoint='passed', binaries='passed', migration_fail_closed='passed',
              bootstrap_verify_only='verified', verify_only_read_only_session='passed',
              verify_only_zero_write='passed', gateway_startup='passed',
              database='isolated synthetic PostgreSQL', production_database_accessed=False,
              executor_started=False)
(p/'validation.json').write_text(json.dumps(result, indent=2)+'\n')
PY
# Retain fingerprints rather than synthetic database contents in release artifacts.
sha256sum "$OUTPUT/db-before.sql" "$OUTPUT/db-after.sql" > "$OUTPUT/database-sha256.txt"
rm "$OUTPUT/db-before.sql" "$OUTPUT/db-after.sql"
echo 'Isolated production wrapper acceptance PASS'
