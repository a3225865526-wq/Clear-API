#!/usr/bin/env bash
# One-time operator-console installer. Installs the approved two-artifact bundle and
# never starts a deployment. Run as root on the authorized Clear API VPS.
set -Eeuo pipefail
umask 077

TARGET=/opt/clearapi/bin/production-deploy.sh
LOCK=/run/lock/clearapi-production-deploy.lock
EXPECTED_SHA=8685844a9b3210b511d96301ebe80c57b6fd97b5679afa87c4ebb031a083c430
EXPECTED_IDENTITY_SHA=afcefce7dcf05bf853a1c44244ddf834dd9e2d6fb023f6cb75495410fc7b7b8e
EXPECTED_LIST_SHA=51160cb02c50da6c1b13157b4d2fb2a06211195aa7324afc326cf1af0ee25d5e
# Approval is pinned to bytes, not to an arbitrary caller-provided checksum.
# The supplied release commit is recorded provenance, not a Git signature check.
EXPECTED_COMMIT=${2:-}
SOURCE_DIR=${1:-}
SOURCE=${SOURCE_DIR}/production-deploy.sh
IDENTITY_SOURCE=${SOURCE_DIR}/runtime-identity-refresh.py
ARTIFACT_LIST=${SOURCE_DIR}/runtime-identity-artifacts.sha256
IDENTITY_TARGET=/opt/clearapi/bin/runtime-identity-refresh.py
IDENTITY_EXISTED=0
IDENTITY_REPLACED=0
IDENTITY_BACKUP=
OLD_IDENTITY_SHA=
STAGE=
BACKUP=
REPLACED=0
RESTORING=0

fail() { printf 'INSTALL_FAILED: %s\n' "$*" >&2; exit 1; }
audit() {
  local status=$1 detail=$2
  [ -n "$STAGE" ] || fail "audit stage is unavailable"
  python3 - "$STAGE/audit.jsonl" "$status" "$detail" "$EXPECTED_COMMIT" <<'PY'
import json, os, stat, sys
path, status, detail, commit = sys.argv[1:]
fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
with os.fdopen(fd, 'a', encoding='utf-8') as f:
    s = os.fstat(f.fileno())
    if not stat.S_ISREG(s.st_mode) or s.st_mode & 0o077 or s.st_nlink != 1:
        raise SystemExit('audit file permissions are unsafe')
    f.write(json.dumps({'status': status, 'detail': detail, 'commit': commit}) + '\n')
    f.flush(); os.fsync(f.fileno())
print(f'{status}: {detail}')
PY
}
restore() {
  [ "$RESTORING" = 0 ] || return 1
  RESTORING=1
  if [ "$REPLACED" = 1 ]; then
    [ -f "$BACKUP" ] && [ ! -L "$BACKUP" ] || return 1
    cp --preserve=all -- "$BACKUP" "$STAGE/restore.sh" || return 1
    chown root:root "$STAGE/restore.sh" || return 1
    chmod 0750 "$STAGE/restore.sh" || return 1
    mv -fT -- "$STAGE/restore.sh" "$TARGET" || return 1
    test "$(sha256sum "$TARGET" | cut -d' ' -f1)" = "$OLD_SHA" || return 1
    test "$(stat -c '%u:%g:%a' "$TARGET")" = 0:0:750 || return 1
  fi
  if [ "$IDENTITY_REPLACED" = 1 ]; then
    if [ "$IDENTITY_EXISTED" = 1 ]; then
      cp --preserve=all -- "$IDENTITY_BACKUP" "$STAGE/identity-restore.py" || return 1
      chown root:root "$STAGE/identity-restore.py" || return 1
      chmod 0750 "$STAGE/identity-restore.py" || return 1
      mv -fT -- "$STAGE/identity-restore.py" "$IDENTITY_TARGET" || return 1
      test "$(sha256sum "$IDENTITY_TARGET" | cut -d' ' -f1)" = "$OLD_IDENTITY_SHA" || return 1
      test "$(stat -c '%u:%g:%a' "$IDENTITY_TARGET")" = 0:0:750 || return 1
    elif [ -e "$IDENTITY_TARGET" ] || [ -L "$IDENTITY_TARGET" ]; then
      [ ! -L "$IDENTITY_TARGET" ] || return 1
      test "$(sha256sum "$IDENTITY_TARGET" | cut -d' ' -f1)" = "$EXPECTED_IDENTITY_SHA" || return 1
      rm -- "$IDENTITY_TARGET" || return 1
    fi
  fi
  audit RESTORED 'original wrapper/helper state restored' || return 1
}

on_error() {
  local code=$?
  trap - ERR
  if { [ "$REPLACED" = 1 ] || [ "$IDENTITY_REPLACED" = 1 ]; } && ! restore; then
    printf 'RESTORE_FAILED: original artifact pair could not be restored\n' >&2
    [ -z "$STAGE" ] || audit RESTORE_FAILED "original artifact pair could not be restored" || true
  fi
  printf 'INSTALL_FAILED: exit=%s backup=%s audit=%s\n' "$code" "$BACKUP" "${STAGE:-unavailable}" >&2
  exit "$code"
}
trap on_error ERR
trap 'false' HUP INT TERM

[ "$(id -u)" = 0 ] || fail 'root is required'
[ -n "$SOURCE_DIR" ] || fail 'usage: clearapi-install-approved-script.sh <approved-bundle-directory> <release-commit>'
[[ "$EXPECTED_COMMIT" =~ ^[a-f0-9]{40}$ ]] || fail 'release commit must be full 40-hex'
[ -d "$SOURCE_DIR" ] && [ ! -L "$SOURCE_DIR" ] || fail 'unsafe source directory'
for artifact in "$SOURCE" "$IDENTITY_SOURCE" "$ARTIFACT_LIST"; do
  [ -f "$artifact" ] && [ ! -L "$artifact" ] || fail 'missing or unsafe required artifact'
done
test "$(sha256sum "$ARTIFACT_LIST" | cut -d' ' -f1)" = "$EXPECTED_LIST_SHA"
(cd "$SOURCE_DIR" && sha256sum --check runtime-identity-artifacts.sha256)
for directory in /opt /opt/clearapi /opt/clearapi/bin /run /run/lock; do
  [ -d "$directory" ] && [ ! -L "$directory" ] || fail "unsafe directory: $directory"
  test "$(stat -c '%u' "$directory")" = 0 || fail "directory is not root-owned: $directory"
done
test "$(stat -c '%u:%a' /opt/clearapi/bin)" = 0:755 || fail 'target directory must be root mode 0755'
LOCK_MODE="$(stat -c '%a' /run/lock)"
case "$LOCK_MODE" in
  755|1777) ;;
  *) fail 'lock directory must be root mode 0755 or 1777' ;;
esac

# Create the lock with O_NOFOLLOW/O_EXCL if it does not exist, then acquire it.
python3 - "$LOCK" <<'PY'
import os, pathlib, stat, sys
p = pathlib.Path(sys.argv[1])
try:
    fd = os.open(p, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    os.close(fd)
except FileExistsError:
    pass
s = os.lstat(p)
if not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_nlink != 1 or s.st_mode & 0o077:
    raise SystemExit('unsafe deployment lock')
PY
exec 9>"$LOCK"
flock -n 9 || fail 'production deployment lock is held'

STAGE=$(mktemp -d /opt/clearapi/bin/.script-install.XXXXXXXX)
chmod 0700 "$STAGE"; chown root:root "$STAGE"
OLD_SHA=$(sha256sum "$TARGET" | cut -d' ' -f1)
test "$(stat -c '%u:%g:%a' "$TARGET")" = 0:0:750 || fail 'existing wrapper must be root:root 0750'
BACKUP="$STAGE/production-deploy.old.sh"
cp --preserve=all -- "$TARGET" "$BACKUP"
test "$(sha256sum "$BACKUP" | cut -d' ' -f1)" = "$OLD_SHA"
if [ -e "$IDENTITY_TARGET" ] || [ -L "$IDENTITY_TARGET" ]; then
  [ -f "$IDENTITY_TARGET" ] && [ ! -L "$IDENTITY_TARGET" ] || fail 'unsafe existing identity helper'
  test "$(stat -c '%u:%g:%a' "$IDENTITY_TARGET")" = 0:0:750
  IDENTITY_EXISTED=1
  OLD_IDENTITY_SHA=$(sha256sum "$IDENTITY_TARGET" | cut -d' ' -f1)
  IDENTITY_BACKUP="$STAGE/runtime-identity.old.py"
  cp --preserve=all -- "$IDENTITY_TARGET" "$IDENTITY_BACKUP"
  test "$(sha256sum "$IDENTITY_BACKUP" | cut -d' ' -f1)" = "$OLD_IDENTITY_SHA"
fi

audit PREPARED "supplied release commit ${EXPECTED_COMMIT}; wrapper backup ${BACKUP}; helper existed ${IDENTITY_EXISTED}"

install -o root -g root -m 0750 -- "$SOURCE" "$STAGE/production-deploy.new.sh"
test "$(sha256sum "$STAGE/production-deploy.new.sh" | cut -d' ' -f1)" = "$EXPECTED_SHA"
bash -n "$STAGE/production-deploy.new.sh"
install -o root -g root -m 0750 -- "$IDENTITY_SOURCE" "$STAGE/runtime-identity.new.py"
test "$(sha256sum "$STAGE/runtime-identity.new.py" | cut -d' ' -f1)" = "$EXPECTED_IDENTITY_SHA"
test "$(stat -c '%u:%g:%a' "$STAGE/runtime-identity.new.py")" = 0:0:750
python3 - "$STAGE/runtime-identity.new.py" <<'PY'
import ast, pathlib, sys
ast.parse(pathlib.Path(sys.argv[1]).read_text(encoding='utf-8'))
PY
test "$(stat -c '%u:%g:%a' "$STAGE/production-deploy.new.sh")" = 0:0:750
test "$(stat -c %d "$STAGE")" = "$(stat -c %d /opt/clearapi/bin)"

test -s /etc/machine-id
id clearapi-deploy >/dev/null
sudo -n /usr/bin/sha256sum "$TARGET" >/dev/null
audit VERIFIED "vps=$(hostname -f 2>/dev/null || hostname); deploy_account=clearapi-deploy"

IDENTITY_REPLACED=1
mv -fT -- "$STAGE/runtime-identity.new.py" "$IDENTITY_TARGET"
test "$(sha256sum "$IDENTITY_TARGET" | cut -d' ' -f1)" = "$EXPECTED_IDENTITY_SHA"
test "$(stat -c '%u:%g:%a' "$IDENTITY_TARGET")" = 0:0:750
REPLACED=1
mv -fT -- "$STAGE/production-deploy.new.sh" "$TARGET"
test "$(sha256sum "$TARGET" | cut -d' ' -f1)" = "$EXPECTED_SHA"
test "$(stat -c '%u:%g:%a' "$TARGET")" = 0:0:750
bash -n "$TARGET"
sudo -n /usr/bin/sha256sum "$TARGET" | grep -q "$EXPECTED_SHA"
sudo -n /usr/bin/sha256sum "$IDENTITY_TARGET" | grep -q "$EXPECTED_IDENTITY_SHA"
audit INSTALLED "wrapper_sha ${EXPECTED_SHA}; identity_sha ${EXPECTED_IDENTITY_SHA}; no container operation performed"

printf 'INSTALL_PASS: wrapper=%s identity=%s wrapper_sha=%s identity_sha=%s audit=%s\n' "$TARGET" "$IDENTITY_TARGET" "$EXPECTED_SHA" "$EXPECTED_IDENTITY_SHA" "$STAGE/audit.jsonl"
