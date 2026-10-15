#!/usr/bin/env python3
"""Snapshot PG03 files and restore only into an empty, isolated directory.

Never restore in place: the operator must reconcile live anti-rollback high-water
marks and database recovery before any separately approved production recovery.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import stat
import sys
import tarfile

ROOTS = ('opt/clearapi/app/compose-pg03.yaml', 'opt/clearapi/config/pg03',
         'data/clearapi/pool-executor')
MAX_FILE = 128 * 1024 * 1024


def read_file(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as f:
        s = os.fstat(f.fileno())
        if not stat.S_ISREG(s.st_mode) or s.st_nlink != 1 or s.st_size > MAX_FILE:
            raise ValueError('unsafe state file')
        raw = f.read(MAX_FILE + 1)
        after = os.fstat(f.fileno())
    if len(raw) > MAX_FILE or (s.st_ino, s.st_size, s.st_mtime_ns) != (after.st_ino, after.st_size, after.st_mtime_ns):
        raise ValueError('state file changed')
    return raw


def inventory(root):
    files = {}
    owner = 0 if root == Path('/') else os.geteuid()
    for name in ROOTS:
        base = root / name
        for parent in base.parents:
            if parent.is_symlink() or not parent.is_dir():
                raise ValueError('unsafe state parent')
        if not base.exists() or base.is_symlink():
            raise ValueError('missing PG03 recovery root')
        paths = [base, *sorted(base.rglob('*'))] if base.is_dir() else [base]
        for p in paths:
            s = p.lstat()
            if (s.st_uid != owner or s.st_gid != os.getegid()
                    or s.st_mode & 0o022 or stat.S_ISLNK(s.st_mode)
                    or not (stat.S_ISREG(s.st_mode) or stat.S_ISDIR(s.st_mode))):
                raise ValueError('unsafe state ownership or type')
            relative = p.relative_to(root).as_posix()
            # Keys, credentials and persistent anti-rollback state stay private.
            if (relative.startswith('data/') or relative.endswith('pg03.env')) and s.st_mode & 0o077:
                raise ValueError('private state permissions required')
            entry = {'mode': stat.S_IMODE(s.st_mode), 'uid': s.st_uid,
                     'gid': s.st_gid, 'mtime_ns': s.st_mtime_ns,
                     'directory': p.is_dir()}
            if not entry['directory']:
                raw = read_file(p)
                entry.update(bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest())
            files[relative] = entry
            if len(files) > 100000:
                raise ValueError('state snapshot too large')
    for name in ('checkpoint.journal', 'admission.json'):
        value = json.loads(read_file(root / 'data/clearapi/pool-executor/pool' / name))
        if value['guard_version'] != 3 or type(value['revision']) is not int or value['revision'] < 0:
            raise ValueError('invalid anti-rollback state')
    for name in ('auths', 'receipts', 'pool'):
        if not (root / 'data/clearapi/pool-executor' / name).is_dir():
            raise ValueError('incomplete PG03 state roots')
    return files


def capture(root, archive, snapshot):
    frozen = inventory(root)
    # Exclusively create: never replace a historical archive or snapshot.
    fd = os.open(archive, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as output, tarfile.open(fileobj=output, mode='w:gz') as tar:
        for name, entry in frozen.items():
            info = tarfile.TarInfo(name)
            info.mode, info.uid, info.gid = entry['mode'], entry['uid'], entry['gid']
            if entry['directory']:
                info.type = tarfile.DIRTYPE
                tar.addfile(info)
            else:
                raw = read_file(root / name)
                if hashlib.sha256(raw).hexdigest() != entry['sha256']:
                    raise ValueError('state changed while archiving')
                info.size = len(raw)
                tar.addfile(info, io.BytesIO(raw))
    if inventory(root) != frozen:
        raise ValueError('PG03 snapshot changed')
    fd = os.open(snapshot, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as f:
        json.dump(frozen, f, sort_keys=True)
    archive.chmod(0o600)
    snapshot.chmod(0o600)


def restore(archive, snapshot, destination):
    # No in-place recovery, overwrite, revision rollback or implicit service action.
    destination = destination.absolute()
    if (destination.exists() or destination.is_symlink()
            or not any(p in destination.parents for p in (Path('/tmp'), Path('/var/tmp')))):
        raise ValueError('restore destination must not exist')
    for p in destination.parents:
        if p.is_symlink() or not p.is_dir():
            raise ValueError('unsafe restore parent')
    expected = json.loads(read_file(snapshot))
    with tarfile.open(archive, 'r:gz') as tar:
        members = tar.getmembers()
        if len(members) != len(expected) or len({m.name for m in members}) != len(members):
            raise ValueError('archive membership mismatch')
        # Validate everything before creating the destination or extracting bytes.
        for m in members:
            name = PurePosixPath(m.name)
            if (name.is_absolute() or '..' in name.parts or str(name) != m.name
                    or m.name not in expected or not (m.isfile() or m.isdir())
                    or not any(m.name == r or m.name.startswith(r + '/') for r in ROOTS)
                    or m.size > MAX_FILE):
                raise ValueError('unsafe archive entry')
            e = expected[m.name]
            if m.mode != e['mode'] or m.uid != e['uid'] or m.gid != e['gid'] or m.isdir() != e['directory']:
                raise ValueError('archive metadata mismatch')
            if m.isfile():
                raw = tar.extractfile(m).read(MAX_FILE + 1)
                if len(raw) != e['bytes'] or hashlib.sha256(raw).hexdigest() != e['sha256']:
                    raise ValueError('archive content mismatch')
        destination.mkdir(mode=0o700)
        for m in members:
            p = destination / m.name
            p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            if m.isdir():
                p.mkdir(exist_ok=True, mode=m.mode)
            else:
                with p.open('xb') as f:
                    f.write(tar.extractfile(m).read(MAX_FILE + 1))
            os.chmod(p, m.mode)
            if os.geteuid() == 0:
                os.chown(p, m.uid, m.gid)
        for name in sorted(expected, key=lambda n: len(PurePosixPath(n).parts), reverse=True):
            timestamp = expected[name]['mtime_ns']
            os.utime(destination / name, ns=(timestamp, timestamp), follow_symlinks=False)
    if inventory(destination) != expected:
        raise ValueError('isolated restore verification failed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('capture', 'check', 'restore'))
    parser.add_argument('--root', type=Path, default=Path('/'))
    parser.add_argument('--archive', type=Path)
    parser.add_argument('--snapshot', type=Path, required=True)
    parser.add_argument('--destination', type=Path)
    args = parser.parse_args()
    if args.mode == 'capture':
        capture(args.root, args.archive, args.snapshot)
    elif args.mode == 'check':
        if inventory(args.root) != json.loads(read_file(args.snapshot)):
            raise ValueError('PG03 state changed during backup')
    else:
        restore(args.archive, args.snapshot, args.destination)
    print('PG03 state ' + args.mode + ' PASS (isolated restore only)')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        print('PG03 state verification BLOCKED; no production restore attempted', file=sys.stderr)
        sys.exit(1)
