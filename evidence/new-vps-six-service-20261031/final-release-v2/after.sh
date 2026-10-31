#!/bin/sh
set -eu
dockerd --host=unix:///var/run/docker.sock --data-root=/vps/docker --storage-driver=overlay2 >/tmp/dockerd.log 2>&1 &
exec /usr/sbin/sshd -D -e
