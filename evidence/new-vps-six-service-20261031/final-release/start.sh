#!/bin/sh
set -eu
if [ ! -f /vps.img ]; then
  truncate -s 50000000000 /vps.img
  mkfs.ext4 -F -m 0 /vps.img >/tmp/mkfs.log 2>&1
fi
mkdir -p /vps /opt/clearapi
mount -o loop /vps.img /vps
mkdir -p /vps/clearapi
mount --bind /vps/clearapi /opt/clearapi
mkdir -p /run/sshd /root/.ssh
ssh-keygen -A
exec dind sh /lab-after.sh
