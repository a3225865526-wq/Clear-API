#!/bin/sh
# Root-only initial enrollment on the NEW target, after independent bundle SHA
# verification and explicit installation authorization. Never update the old VPS.
set -eu
umask 077
test "$(id -u)" -eq 0
test "$#" -eq 6 || { echo 'usage: install-stack-target.sh PACKAGE SERVER_JSON ENV SECRETS_DIR SSH_PUBLIC_KEY BACKUP_PUBLIC_KEY' >&2; exit 2; }
pkg=$(realpath "$1")
server=$(realpath "$2")
envfile=$(realpath "$3")
secrets=$(realpath "$4")
sshkey=$(realpath "$5")
backupkey=$(realpath "$6")
test ! -e /opt/clearapi/config/local-deploy.enabled
test ! -e /opt/clearapi/app/local-deploy-state.json
test ! -e /opt/clearapi/config/stack-target.enabled
id cleardeploy >/dev/null
for command in python3 docker sudo visudo gpg sshd logrotate; do command -v "$command" >/dev/null; done
docker compose version >/dev/null
python3 "$pkg/package.py" verify "$pkg"
mkdir -p /opt/clearapi/bin /opt/clearapi/config/secrets
chmod 755 /opt/clearapi /opt/clearapi/bin
chmod 700 /opt/clearapi/config /opt/clearapi/config/secrets
for name in local-deploy-vps.py local-deploy-stack.py local-deploy-state.py; do
  destination=$name
  test "$name" != local-deploy-vps.py || destination=clearapi-local-deploy.py
  install -o root -g root -m 750 "$pkg/$name" "/opt/clearapi/bin/$destination"
done
install -o root -g root -m 600 "$server" /opt/clearapi/config/stack-target.json
install -o root -g root -m 600 "$envfile" /opt/clearapi/config/stack.env
for name in token-key admin-password terms.txt privacy.txt; do
  install -o root -g root -m 600 "$secrets/$name" "/opt/clearapi/config/secrets/$name"
done
for name in releases incoming backups receipts data/gateway data/logs sub2data/app; do
  mkdir -p "/opt/clearapi/stack/$name"
done
chmod 700 /opt/clearapi/stack /opt/clearapi/stack/releases /opt/clearapi/stack/incoming /opt/clearapi/stack/backups /opt/clearapi/stack/receipts
install -o root -g root -m 444 "$pkg/stack.sudoers.example" /opt/clearapi/config/stack.sudoers.review
visudo -cf /opt/clearapi/config/stack.sudoers.review >/dev/null
install -o root -g root -m 440 /opt/clearapi/config/stack.sudoers.review /etc/sudoers.d/clearapi-stack
mkdir -p /home/cleardeploy/.ssh
install -o cleardeploy -g cleardeploy -m 600 "$sshkey" /home/cleardeploy/.ssh/authorized_keys
chown cleardeploy:cleardeploy /home/cleardeploy/.ssh
chmod 700 /home/cleardeploy/.ssh
# Check the actual SSH policy without changing/reloading the global SSH daemon.
sshd -T -C user=cleardeploy,host=localhost,addr=127.0.0.1 | grep -qx 'passwordauthentication no'
sshd -T -C user=cleardeploy,host=localhost,addr=127.0.0.1 | grep -qx 'kbdinteractiveauthentication no'
gpg --batch --import "$backupkey" >/dev/null 2>&1
install -o root -g root -m 600 "$pkg/logrotate.conf" /opt/clearapi/config/stack.logrotate
logrotate --debug /opt/clearapi/config/stack.logrotate >/dev/null 2>&1
mkdir -p /etc/logrotate.d
install -o root -g root -m 644 "$pkg/logrotate.conf" /etc/logrotate.d/clearapi-six-stack
python3 -c 'import json;from pathlib import Path;c=json.loads(Path("/opt/clearapi/config/stack-target.json").read_text());Path("/opt/clearapi/config/stack-target.enabled").write_text("STACK_TARGET_APPROVED_V1:"+c["target_id"]+"\n")'
chmod 600 /opt/clearapi/config/stack-target.enabled
# This validates protected metadata, daemon and helper identities. No deployment.
python3 /opt/clearapi/bin/clearapi-local-deploy.py stack-preflight
