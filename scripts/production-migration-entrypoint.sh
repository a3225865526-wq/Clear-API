#!/bin/sh
# Optional reviewed host mount for the fixed candidate, never an image change.
# The host rotates this non-secret challenge while the Gateway is stopped and
# keeps this directory mounted read-only. /tmp must be a fresh tmpfs each start.
# No arguments, caller path or environment variable can select another command.
set -eu
PATH=/usr/bin:/bin
export PATH
[ "$#" -eq 0 ] || exit 1
[ "$(id -u)" = 0 ] || exit 1
[ -d /run/clearapi ] && [ ! -L /run/clearapi ] || exit 1
[ "$(stat -c '%u:%g:%a' /run/clearapi)" = '0:0:750' ] || exit 1
[ -f /run/clearapi/migration-challenge ] && [ ! -L /run/clearapi/migration-challenge ] || exit 1
[ "$(stat -c '%u:%g:%a:%h:%s' /run/clearapi/migration-challenge)" = '0:0:600:1:65' ] || exit 1
IFS= read -r challenge < /run/clearapi/migration-challenge || exit 1
[ "${#challenge}" -eq 64 ] || exit 1
case "$challenge" in *[!a-f0-9]*) exit 1 ;; esac
# Refuse stale proof instead of removing or overwriting evidence.
[ ! -e /tmp/clearapi-migration-proof.json ] && [ ! -L /tmp/clearapi-migration-proof.json ] || exit 1
[ ! -e /tmp/clearapi-migration-proof.json.started ] && [ ! -L /tmp/clearapi-migration-proof.json.started ] || exit 1
CLEARAPI_MIGRATION_PROOF_CHALLENGE=$challenge
export CLEARAPI_MIGRATION_PROOF_CHALLENGE
exec /entrypoint-secure.sh /new-api
