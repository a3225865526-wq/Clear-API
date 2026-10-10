#!/usr/bin/env bash
# One-time operator-console installer. It installs only the audited wrapper and
# never starts a deployment. Run as root on the authorized Clear API VPS.
set -Eeuo pipefail
umask 077

TARGET=/opt/clearapi/bin/production-deploy.sh
LOCK=/run/lock/clearapi-production-deploy.lock
EXPECTED_SHA=7c1798e8a84d3e0e521f4fad28ee27f0039b75330b2021d0c7ac505885e3467c
EXPECTED_COMMIT=f1797fbb3752100c665ddf85d9dfae89b7765ad7
RELEASE_COMMIT=${2:-}
SOURCE=${1:-}
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
  [ "$REPLACED" = 1 ] || return 0
  [ "$RESTORING" = 0 ] || return 1
  RESTORING=1
  if [ -f "$BACKUP" ] && [ ! -L "$BACKUP" ]; then
    cp --preserve=all -- "$BACKUP" "$STAGE/restore.sh" || return 1
    chown root:root "$STAGE/restore.sh" || return 1
    chmod 0750 "$STAGE/restore.sh" || return 1
    mv -fT -- "$STAGE/restore.sh" "$TARGET" || return 1
    test "$(sha256sum "$TARGET" | cut -d' ' -f1)" = "$OLD_SHA" || return 1
    test "$(stat -c '%u:%g:%a' "$TARGET")" = 0:0:750 || return 1
    audit RESTORED "old wrapper restored after post-replace verification failure" || return 1
    return 0
  fi
  return 1
}
on_error() {
  local code=$?
  trap - ERR
  if [ "$REPLACED" = 1 ] && ! restore; then
    printf 'RESTORE_FAILED: original wrapper could not be restored\n' >&2
    [ -z "$STAGE" ] || audit RESTORE_FAILED "original wrapper could not be restored" || true
  fi
  printf 'INSTALL_FAILED: exit=%s backup=%s audit=%s\n' "$code" "$BACKUP" "${STAGE:-unavailable}" >&2
  exit "$code"
}
trap on_error ERR

[ "$(id -u)" = 0 ] || fail 'root is required'
[ -n "$SOURCE" ] || fail 'usage: clearapi-install-approved-script.sh /root/input/approved-wrapper.sh'
[[ "$RELEASE_COMMIT" == "$EXPECTED_COMMIT" ]] || fail 'release commit does not match the approved installer identity'
[ -f "$SOURCE" ] && [ ! -L "$SOURCE" ] || fail 'source must be a regular non-symlink file'
for directory in /opt /opt/clearapi /opt/clearapi/bin /run /run/lock; do
  [ -d "$directory" ] && [ ! -L "$directory" ] || fail "unsafe directory: $directory"
  test "$(stat -c '%u' "$directory")" = 0 || fail "directory is not root-owned: $directory"
done
test "$(stat -c '%u:%a' /opt/clearapi/bin)" = 0:755 || fail 'target directory must be root mode 0755'
test "$(stat -c '%u:%a' /run/lock)" = 0:755 || fail 'lock directory must be root mode 0755'

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
audit PREPARED "source commit ${EXPECTED_COMMIT}; old_sha ${OLD_SHA}; backup ${BACKUP}"

install -o root -g root -m 0750 -- "$SOURCE" "$STAGE/production-deploy.new.sh"
test "$(sha256sum "$STAGE/production-deploy.new.sh" | cut -d' ' -f1)" = "$EXPECTED_SHA"
bash -n "$STAGE/production-deploy.new.sh"
test "$(stat -c '%u:%g:%a' "$STAGE/production-deploy.new.sh")" = 0:0:750
test "$(stat -c %d "$STAGE")" = "$(stat -c %d /opt/clearapi/bin)"

test -s /etc/machine-id
id clearapi-deploy >/dev/null
sudo -n /usr/bin/sha256sum "$TARGET" >/dev/null
audit VERIFIED "vps=$(hostname -f 2>/dev/null || hostname); deploy_account=clearapi-deploy"

mv -fT -- "$STAGE/production-deploy.new.sh" "$TARGET"
REPLACED=1
test "$(sha256sum "$TARGET" | cut -d' ' -f1)" = "$EXPECTED_SHA"
test "$(stat -c '%u:%g:%a' "$TARGET")" = 0:0:750
bash -n "$TARGET"
sudo -n /usr/bin/sha256sum "$TARGET" | grep -q "$EXPECTED_SHA"
audit INSTALLED "new_sha ${EXPECTED_SHA}; no container replacement performed"

printf 'INSTALL_PASS: target=%s sha=%s backup=%s audit=%s\n' "$TARGET" "$EXPECTED_SHA" "$BACKUP" "$STAGE/audit.jsonl"
