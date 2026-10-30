#!/bin/sh
# Offline maintenance backup. Caller must fence ingress and stop all writers first.
# Output includes secrets: caller supplies a private directory and encrypts off-host.
set -eu
umask 077
test "$#" -eq 2 || { echo 'usage: backup.sh ENV_FILE PRIVATE_NEW_BACKUP_DIR' >&2; exit 2; }
envfile=$(realpath "$1")
out=$2
cd "$(dirname "$0")"
dc() { docker compose -p "${COMPOSE_PROJECT_NAME:-clearapi-stack}" --env-file "$envfile" -f compose.yaml "$@"; }
for service in new-api sub2api edge; do
  test -z "$(dc ps --status running -q "$service")" || { echo 'STOP: fence and stop writers first' >&2; exit 1; }
done
mkdir -m 700 "$out"
dc exec -T gateway-postgres pg_dump -U clearapi -d clearapi -Fc > "$out/gateway.dump"
dc exec -T gateway-postgres pg_dumpall -U clearapi --globals-only > "$out/gateway-globals.sql"
dc exec -T sub2api-postgres pg_dump -U sub2api -d sub2api -Fc > "$out/sub2api.dump"
dc exec -T sub2api-postgres pg_dumpall -U sub2api --globals-only > "$out/sub2api-globals.sql"
dc exec -T sub2api-redis redis-cli SAVE >/dev/null
redis=$(dc ps -q sub2api-redis)
docker cp "$redis:/data/dump.rdb" "$out/redis.rdb"
cp "$envfile" "$out/production.env"
cp compose.yaml nginx.conf images.lock.json "$out/"
# Stable filesystem snapshots after stopping writers. Preserve UID and permissions.
root=$(sed -n 's/^ROOT=//p' "$envfile" | tr -d '\r')
subroot=$(sed -n 's/^SUB2_ROOT=//p' "$envfile" | tr -d '\r')
test -n "$root" && test -n "$subroot"
tar -cpf "$out/app-state.tar" -C "$root" gateway logs
tar -cpf "$out/sub2api-app.tar" -C "$subroot" app
(cd "$out"; sha256sum *.dump *.sql *.rdb *.tar production.env compose.yaml nginx.conf images.lock.json > SHA256SUMS)
echo 'Backup complete; encrypt off-host, verify decryption and isolated restore before cutover.'
