#!/bin/sh
set -eu
ssh-keygen -A
dockerd --host=unix:///var/run/docker.sock --feature containerd-snapshotter=false --storage-driver=overlay2 > /tmp/dockerd.log 2>&1 &
exec /usr/sbin/sshd -D -e
