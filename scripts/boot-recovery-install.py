#!/usr/bin/env python3
"""Incident-specific, fail-closed boot asset transaction; never start a service.

The seven released payloads and the historical inventory remain unchanged.
This installer is a NEW, separately reviewable local asset, not part of that RC.
"""
import argparse
import ast
import base64
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
INVENTORY_SHA = 'b25e5a9c3deee05b51c7aaf3d0c74167c0be223929e2f1191d01ebc5492917d2'
SOURCE = '69a66431646b5cc8912bf97a739e08de07316443'
TREE = '7231fa53f2a1bb8cebb7ab314f9f038442ac3de6'
UNIT = 'clearapi-runtime-identity.service'
LINK = '/etc/systemd/system/multi-user.target.wants/' + UNIT
UNIT_PATH = '/etc/systemd/system/' + UNIT
DIRECTORIES = ('/etc/systemd/system/docker.service.d', '/etc/systemd/system/multi-user.target.wants')
UNITS = ('docker.service', 'clearapi-runtime-directory.service', UNIT)
SCRIPT_PATHS = tuple('/opt/clearapi/bin/' + name for name in (
    'runtime-boot-recovery.py', 'runtime-identity-refresh.py', 'pg03-failed-recovery.py'))
CONFIG_PATHS = ('/etc/tmpfiles.d/clearapi-runtime.conf',
                '/etc/systemd/system/clearapi-runtime-directory.service',
                '/etc/systemd/system/docker.service.d/clearapi-runtime.conf', UNIT_PATH)
TARGETS = SCRIPT_PATHS + CONFIG_PATHS


class Blocked(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Blocked(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encode(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode()


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON key')
        result[key] = value
    return result


def decode(raw):
    return json.loads(raw, object_pairs_hook=unique)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def safe_parents(path):
    require(path.is_absolute() and '..' not in path.parts, 'unsafe absolute path')
    for parent in reversed(path.parents):
        if not parent.exists() and not parent.is_symlink():
            continue  # Only the transaction's two allowlisted directories may be absent.
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0
                and (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX), 'unsafe parent')


def stable_stat(info):
    # Reading may legitimately advance atime; it must not change file identity.
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid,
            info.st_gid, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def snapshot(path, linked_stage=None):
    """Read through a no-follow descriptor and reject unpreservable ACL/xattrs."""
    safe_parents(path)
    try:
        info = path.lstat()
    except FileNotFoundError:
        return {'kind': 'absent'}
    require(info.st_uid == info.st_gid == 0, 'unexpected owner')
    require(not os.listxattr(path, follow_symlinks=False), 'ACL/xattrs require separate review')
    result = {'uid': info.st_uid, 'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode),
              'dev': info.st_dev, 'ino': info.st_ino}
    links = 1
    if linked_stage is not None and (linked_stage.exists() or linked_stage.is_symlink()):
        staged = linked_stage.lstat()
        if (staged.st_dev, staged.st_ino) == (info.st_dev, info.st_ino):
            links = 2
    if stat.S_ISLNK(info.st_mode):
        require(info.st_nlink == links, 'unexpected symlink hard link')
        target = os.readlink(path)
        result.update(kind='link', target=target, sha256=sha(target.encode()), nlink=1)
    elif stat.S_ISDIR(info.st_mode):
        require(not info.st_mode & 0o022, 'writable directory')
        result.update(kind='directory')
    else:
        require(stat.S_ISREG(info.st_mode) and not info.st_mode & 0o022, 'unsafe target type/mode')
        require(info.st_nlink == links, 'unexpected hard link')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as stream:
            require(stable_stat(os.fstat(stream.fileno())) == stable_stat(info), 'target changed before read')
            raw = stream.read(1048577)
            require(stable_stat(os.fstat(stream.fileno())) == stable_stat(info), 'target changed during read')
        require(len(raw) <= 1048576 and stable_stat(path.lstat()) == stable_stat(info), 'oversized or changed target')
        result.update(kind='file', sha256=sha(raw), size=len(raw), nlink=1, data=base64.b64encode(raw).decode())
    require(stable_stat(path.lstat()) == stable_stat(info), 'target changed during snapshot')
    return result


def durable_create(path, raw, mode=0o600):
    safe_parents(path)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as stream:
        os.fchown(stream.fileno(), 0, 0)
        os.fchmod(stream.fileno(), mode)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    sync_directory(path.parent)


def payloads(inventory_path, source_dir):
    raw = inventory_path.read_bytes()
    require(sha(raw) == INVENTORY_SHA, 'inventory identity mismatch')
    inventory = decode(raw)
    require(inventory['source'] == dict(repository='a3225865526-wq/Clear-API', commit=SOURCE, root_tree=TREE),
            'source identity mismatch')
    require(inventory['rc']['run_id'] == '39357150402' and inventory['rc']['attempt'] == 1
            and inventory['rc']['original_rc_contains_this_inventory'] is False, 'RC identity mismatch')
    require([asset['destination_path'] for asset in inventory['assets']] == list(TARGETS), 'target inventory mismatch')
    tree = ast.parse((source_dir / 'scripts/runtime-boot-recovery.py').read_bytes())
    assets = ast.literal_eval(next(n.value for n in tree.body if isinstance(n, ast.Assign)
                                  and any(isinstance(t, ast.Name) and t.id == 'ASSETS' for t in n.targets)))
    result = {}
    for asset in inventory['assets']:
        destination = asset['destination_path']
        raw = ((source_dir / asset['source_path']).read_bytes() if asset['kind'] == 'source_file'
               else assets[destination].encode('utf-8'))
        require(sha(raw) == asset['sha256'] and len(raw) == asset['size_bytes'], 'staged asset mismatch')
        # Explicit installation policy, distinct from immutable inventory's Git mode.
        result[destination] = {'raw': raw, 'mode': 0o750 if destination in SCRIPT_PATHS else 0o644}
    return result


class Systemd:
    """Only explicit reload/enable writes. No shell, start, restart, preset or disable."""
    properties = ('LoadState', 'UnitFileState', 'FragmentPath', 'DropInPaths', 'Requires',
                  'Wants', 'After', 'Before', 'NeedDaemonReload', 'ActiveState', 'SubState')

    def command(self, *args):
        return subprocess.run(['/usr/bin/systemctl', *args], check=True, capture_output=True,
                              text=True, timeout=20).stdout

    def state(self):
        jobs = self.command('list-jobs', '--no-legend', '--no-pager').strip()
        require(not jobs, 'pending systemd jobs')
        result = {}
        for unit in UNITS:
            text = self.command('show', unit, '--property=' + ','.join(self.properties))
            values = dict(line.split('=', 1) for line in text.splitlines() if '=' in line)
            require(set(values) == set(self.properties), 'incomplete systemd projection')
            for key in ('DropInPaths', 'Requires', 'Wants', 'After', 'Before'):
                values[key] = ' '.join(sorted(values[key].split()))
            result[unit] = values
        return result

    def reload(self):
        self.command('daemon-reload')

    def enable(self):
        # The one WantedBy link is already created and journaled by our transaction.
        # Suppress enable's implicit reload; the explicit reload is independently checked.
        self.command('--no-reload', 'enable', UNIT)


class Transaction:
    def __init__(self, root, archive, artifacts, systemd):
        self.root = Path(root)
        self.archive = Path(archive)
        self.artifacts = (None if artifacts is None else
                          {name: {'raw': bytes(item['raw']), 'mode': item['mode']} for name, item in artifacts.items()})
        self.systemd = systemd
        require(os.geteuid() == 0 and self.root.is_absolute(), 'Linux root required')
        if artifacts is not None:
            require(tuple(artifacts) == TARGETS, 'unexpected transaction target set')
            for name, item in artifacts.items():
                require(item['mode'] == (0o750 if name in SCRIPT_PATHS else 0o644), 'unexpected target mode')
        self._lock_fd = None

    def path(self, name):
        require(name in TARGETS + (LINK,) + DIRECTORIES, 'path outside installation allowlist')
        return self.root / name.lstrip('/')

    @contextmanager
    def lock(self):
        if self._lock_fd is not None:
            yield
            return
        path = self.root / 'run/lock/clearapi-production-deploy.lock'
        safe_parents(path)
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        try:
            info = os.fstat(fd)
            require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0
                    and info.st_nlink == 1 and not info.st_mode & 0o077, 'unsafe deployment lock')
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self._lock_fd = fd
            yield
        finally:
            self._lock_fd = None
            os.close(fd)

    def related_links(self):
        result = {}
        for prefix in ('etc/systemd/system', 'run/systemd/system'):
            base = self.root / prefix
            if not base.exists():
                continue
            safe_parents(base / 'inspection')
            for entry in base.iterdir():
                paths = [entry]
                if not entry.is_symlink() and entry.is_dir() and entry.name.endswith(('.wants', '.requires')):
                    safe_parents(entry / 'inspection')
                    paths.extend(entry.iterdir())
                for path in paths:
                    if path.is_symlink() and (path.name == UNIT or os.readlink(path).split('/')[-1] == UNIT):
                        name = '/' + path.relative_to(self.root).as_posix()
                        require(name == LINK and os.readlink(path) == UNIT_PATH, 'unexpected unit link/mask/alias')
                        result[name] = snapshot(path)
        return result

    def capture(self):
        require(self.artifacts is not None, 'rollback-only transaction cannot prepare/install')
        for name in TARGETS + (LINK,):
            parent = self.path(name).parent
            require(parent.is_dir() or '/' + parent.relative_to(self.root).as_posix() in DIRECTORIES,
                    'unreviewed missing parent directory')
        entries = {name: snapshot(self.path(name)) for name in TARGETS + (LINK,) + DIRECTORIES}
        for name in TARGETS:
            saved = entries[name]
            require(saved['kind'] in ('absent', 'file'), 'non-regular installation target')
            if saved['kind'] == 'file':
                require(saved['sha256'] == sha(self.artifacts[name]['raw']), 'unknown existing asset; do not replace')
                require(saved['mode'] == self.artifacts[name]['mode'], 'existing permission conflict; do not normalize')
        require(entries[LINK]['kind'] in ('absent', 'link'), 'enable link conflict')
        if entries[LINK]['kind'] == 'link':
            require(entries[LINK]['target'] == UNIT_PATH, 'unknown enable link')
        for name in DIRECTORIES:
            require(entries[name]['kind'] in ('absent', 'directory'), 'parent directory conflict')
        state = self.systemd.state()
        for unit, values in state.items():
            require(values['NeedDaemonReload'] == 'no', 'uncommitted systemd changes')
            require(values['UnitFileState'] not in ('masked', 'masked-runtime', 'enabled-runtime'), 'unit mask/runtime state')
            if unit != 'docker.service':
                require(values['FragmentPath'] in ('', '/etc/systemd/system/' + unit)
                        and not values['DropInPaths'], 'unreviewed unit override')
        return dict(schema='clearapi.boot-preinstall/v1', inventory_sha256=INVENTORY_SHA,
                    source_commit=SOURCE, source_tree=TREE, rc_run='39357150402', rc_attempt=1,
                    root=str(self.root), entries=entries, related_links=self.related_links(), systemd=state)

    def prepare(self):
        with self.lock():
            return self._prepare()

    def _prepare(self):
        require(self.archive.is_absolute(), 'absolute archive path required')
        for name in TARGETS + (LINK,) + DIRECTORIES:
            target = self.path(name)
            require(self.archive != target and self.archive not in target.parents
                    and target.parent not in self.archive.parents, 'archive overlaps installation paths')
        baseline = self.capture()  # Complete validation before archive or target mutations.
        safe_parents(self.archive)
        self.archive.mkdir(mode=0o700)  # Exclusive transaction directory, never reuse an attempt.
        os.chown(self.archive, 0, 0)
        os.chmod(self.archive, 0o700)
        sync_directory(self.archive.parent)
        require(self.archive.stat().st_dev == self.root.stat().st_dev, 'archive must share target filesystem')
        for name in TARGETS + (LINK,):
            parent = self.path(name).parent
            while not parent.exists():
                parent = parent.parent
            require(parent.stat().st_dev == self.archive.stat().st_dev, 'cross-filesystem transaction unsupported')
        raw = encode(baseline)
        durable_create(self.archive / 'baseline.json', raw)
        digest = sha(raw)
        durable_create(self.archive / 'baseline.sha256', (digest + '\n').encode())
        verify_archive(self.archive, digest)
        require(self.capture() == baseline, 'baseline changed during archive; no install allowed')
        self.event('prepared', baseline_sha256=digest)
        return digest

    def event(self, kind, **fields):
        rows = self.journal()
        row = dict(seq=len(rows), previous=rows[-1]['hash'] if rows else '0' * 64,
                   event=kind, **fields)
        row['hash'] = sha(encode(row))
        path = self.archive / 'journal.jsonl'
        fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'ab') as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0
                    and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, 'unsafe journal')
            stream.write(encode(row))
            stream.flush()
            os.fsync(stream.fileno())
        sync_directory(self.archive)

    def journal(self):
        path = self.archive / 'journal.jsonl'
        if not path.exists() and not path.is_symlink():
            return []
        saved = snapshot(path)
        require(saved['kind'] == 'file' and saved['mode'] == 0o600, 'unsafe journal')
        rows = []
        for raw in base64.b64decode(saved['data']).splitlines():
            row = decode(raw)
            digest = row.pop('hash')
            require(row['seq'] == len(rows) and row['previous'] == (rows[-1]['hash'] if rows else '0' * 64)
                    and sha(encode(row)) == digest, 'journal integrity failure')
            rows.append(dict(row, hash=digest))
        return rows

    def create(self, name, raw=None, directory=False):
        path = self.path(name)
        require(snapshot(path)['kind'] == 'absent', 'new target conflict')
        if directory:
            # A crash between mkdir and its durable completion has no proven inode:
            # recovery retains the directory and reports INCOMPLETE, never guesses.
            self.event('directory_intent', path=name)
            path.mkdir(mode=0o755)
            os.chown(path, 0, 0)
            os.chmod(path, 0o755)
            sync_directory(path.parent)
            self.event('created', path=name, after=snapshot(path))
            return
        stage = self.archive / ('stage-' + str(len(self.journal())))
        if name == LINK:
            os.symlink(UNIT_PATH, stage)
            os.chown(stage, 0, 0, follow_symlinks=False)
            sync_directory(self.archive)
        else:
            durable_create(stage, raw, self.artifacts[name]['mode'])
        expected = snapshot(stage)
        self.event('publish_intent', path=name, stage=stage.name, after=expected)
        # Atomic no-replace publication. A link race fails instead of overwriting.
        os.link(stage, path, follow_symlinks=False)
        sync_directory(path.parent)
        stage.unlink()
        sync_directory(self.archive)
        require(snapshot(path) == expected, 'published identity changed')
        self.event('created', path=name, after=expected)

    def verify_installed(self, baseline):
        owned = {row['path']: row['after'] for row in self.journal() if row['event'] == 'created'}
        for name, item in self.artifacts.items():
            current = snapshot(self.path(name))
            require(current['kind'] == 'file' and current['sha256'] == sha(item['raw'])
                    and current['mode'] == item['mode'], 'installed asset verification failure')
            require(current == owned.get(name, baseline['entries'][name]), 'installed object identity changed')
        link = snapshot(self.path(LINK))
        require(link['kind'] == 'link' and link['target'] == UNIT_PATH, 'enable link verification failure')
        require(link == owned.get(LINK, baseline['entries'][LINK]), 'enable link identity changed')
        for name in DIRECTORIES:
            require(snapshot(self.path(name)) == owned.get(name, baseline['entries'][name]), 'directory identity changed')
        self.related_links()
        current = self.systemd.state()
        for unit in UNITS:
            require(current[unit]['NeedDaemonReload'] == 'no', 'systemd reload incomplete')
            for key in ('ActiveState', 'SubState'):
                require(current[unit][key] == baseline['systemd'][unit][key], 'service state changed; stop')
        require(current[UNIT]['UnitFileState'] == 'enabled', 'unit not enabled')
        for unit in UNITS[1:]:
            require(current[unit]['LoadState'] == 'loaded'
                    and current[unit]['FragmentPath'] == '/etc/systemd/system/' + unit
                    and not current[unit]['DropInPaths'], 'loaded unit identity mismatch')
        directory = current[UNITS[1]]
        docker = current['docker.service']
        identity = current[UNIT]
        require('systemd-tmpfiles-setup.service' in directory['Requires'].split()
                and 'systemd-tmpfiles-setup.service' in directory['After'].split()
                and 'docker.service' in directory['Before'].split()
                and UNITS[1] in docker['Requires'].split() and UNITS[1] in docker['After'].split()
                and CONFIG_PATHS[2] in docker['DropInPaths'].split()
                and 'docker.service' in identity['After'].split()
                and 'docker.service' not in (identity['Requires'] + ' ' + identity['Wants']).split(),
                'effective ordering mismatch')

    def install(self, digest):
        with self.lock():
            return self._install(digest)

    def _install(self, digest):
        require(self.artifacts is not None, 'rollback-only transaction cannot install')
        baseline = verify_archive(self.archive, digest)
        require(baseline['root'] == str(self.root), 'archive belongs to another root')
        require(self.capture() == baseline, 'pre-install baseline drift')
        journal = self.journal()
        require([row['event'] for row in journal] == ['prepared']
                and journal[0]['baseline_sha256'] == digest, 'transaction is not fresh/bound')
        try:
            self.event('install_begin')
            for name in DIRECTORIES:
                if baseline['entries'][name]['kind'] == 'absent':
                    self.create(name, directory=True)
            for name, item in self.artifacts.items():
                if baseline['entries'][name]['kind'] == 'absent':
                    self.create(name, item['raw'])
            if baseline['entries'][LINK]['kind'] == 'absent':
                self.create(LINK)
            self.event('reload_intent')
            self.systemd.reload()
            self.event('enable_intent')
            self.systemd.enable()
            self.verify_installed(baseline)
            self.event('installed')
        except BaseException as error:
            self.event('install_failed', error_type=type(error).__name__)
            self.rollback(digest)
            raise

    def rollback(self, digest):
        with self.lock():
            return self._rollback(digest)

    def _rollback(self, digest):
        baseline = verify_archive(self.archive, digest)
        require(baseline['root'] == str(self.root), 'archive belongs to another root')
        rows = self.journal()
        require(rows and rows[0]['event'] == 'prepared' and rows[0]['baseline_sha256'] == digest, 'unbound journal')
        require(not any(row['event'] in ('rolled_back', 'rollback_failed') for row in rows), 'rollback already attempted; no automatic retry')
        self.event('rollback_begin')
        try:
            owned = {}
            unresolved_directories = set()
            for row in rows:
                if row['event'] == 'directory_intent':
                    unresolved_directories.add(row['path'])
                if row['event'] in ('publish_intent', 'created'):
                    name = row['path']
                    self.path(name)
                    require(baseline['entries'][name]['kind'] == 'absent', 'refuse rollback of pre-existing asset')
                    owned[name] = row
                    if row['event'] == 'created':
                        unresolved_directories.discard(name)
            for name, row in reversed(list(owned.items())):
                path = self.path(name)
                stage = None
                if row.get('stage'):
                    require(Path(row['stage']).name == row['stage'] and row['stage'].startswith('stage-'), 'unsafe stage path')
                    stage = self.archive / row['stage']
                current = snapshot(path, linked_stage=stage)
                if current['kind'] == 'absent':
                    continue  # Failed atomic publication never touched this target.
                require(current == row['after'], 'rollback target changed; preserve unknown asset')
                self.event('undo_intent', path=name, identity=current)
                if current['kind'] == 'directory':
                    path.rmdir()  # Never recurse or remove another object's contents.
                else:
                    path.unlink()
                sync_directory(path.parent)
                self.event('undone', path=name)
            for name in unresolved_directories:
                require(snapshot(self.path(name))['kind'] == 'absent', 'unproven directory creation; preserve for review')
            for name, expected in baseline['entries'].items():
                require(snapshot(self.path(name)) == expected, 'rollback baseline verification failed')
            require(self.related_links() == baseline['related_links'], 'unit links changed')
            if any(row['event'] == 'reload_intent' for row in rows):
                self.event('rollback_reload_intent')
                self.systemd.reload()  # One bounded attempt, never start/restart a service.
            require(self.systemd.state() == baseline['systemd'], 'systemd baseline not restored')
            self.event('rolled_back')
        except BaseException as error:
            self.event('rollback_failed', error_type=type(error).__name__)
            raise Blocked('rollback incomplete; preserve evidence/isolation; no automatic retry')


def verify_archive(archive, expected_sha):
    """Independent read-only verifier; no systemd backend or installed files needed."""
    archive = Path(archive)
    info = archive.lstat()
    safe_parents(archive)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0
            and stat.S_IMODE(info.st_mode) == 0o700, 'unsafe archive directory')
    saved = snapshot(archive / 'baseline.json')
    seal = snapshot(archive / 'baseline.sha256')
    require(saved['kind'] == seal['kind'] == 'file' and saved['mode'] == seal['mode'] == 0o600, 'unsafe archive files')
    raw = base64.b64decode(saved['data'])
    require(sha(raw) == expected_sha and base64.b64decode(seal['data']) == (expected_sha + '\n').encode(), 'archive seal mismatch')
    baseline = decode(raw)
    require(baseline['schema'] == 'clearapi.boot-preinstall/v1'
            and baseline['inventory_sha256'] == INVENTORY_SHA and baseline['source_commit'] == SOURCE
            and baseline['source_tree'] == TREE and baseline['rc_run'] == '39357150402'
            and baseline['rc_attempt'] == 1, 'archive identity mismatch')
    require(set(baseline['entries']) == set(TARGETS + (LINK,) + DIRECTORIES), 'incomplete archive target set')
    for name, entry in baseline['entries'].items():
        if entry['kind'] == 'absent':
            require(entry == {'kind': 'absent'}, 'invalid missing-state record')
            continue
        require(entry['uid'] == entry['gid'] == 0 and entry['dev'] >= 0 and entry['ino'] > 0, 'invalid archive metadata')
        if name in TARGETS:
            require(entry['kind'] == 'file', 'missing original file payload')
            require(entry['nlink'] == 1 and entry['mode'] == (0o750 if name in SCRIPT_PATHS else 0o644), 'invalid archived file mode/link count')
            raw = base64.b64decode(entry['data'], validate=True)
            require(sha(raw) == entry['sha256'] and len(raw) == entry['size'], 'original payload integrity failure')
        elif name == LINK:
            require(entry['kind'] == 'link' and entry['target'] == UNIT_PATH and entry['nlink'] == 1
                    and entry['sha256'] == sha(UNIT_PATH.encode()), 'invalid archived enable link')
        else:
            require(entry['kind'] == 'directory', 'invalid archived directory')
    require(set(baseline['systemd']) == set(UNITS), 'incomplete systemd baseline')
    return baseline


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('verify', 'archive', 'install', 'rollback'))
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--archive-sha256')
    parser.add_argument('--inventory', type=Path)
    parser.add_argument('--source-dir', type=Path)
    parser.add_argument('--confirm')
    args = parser.parse_args()
    try:
        if args.action == 'verify':
            verify_archive(args.archive, args.archive_sha256)
            print('Archive integrity PASS; read-only')
            return
        require(args.confirm == 'BOOT_' + args.action.upper() + '_RC39357150402_1', 'explicit operation confirmation required')
        artifacts = None
        if args.action != 'rollback':
            require(args.inventory is not None and args.source_dir is not None, 'pinned bundle required')
            artifacts = payloads(args.inventory, args.source_dir)
        transaction = Transaction(Path('/'), args.archive, artifacts, Systemd())
        with transaction.lock():
            if args.action == 'archive':
                print('Archive SHA256 ' + transaction.prepare())
            elif args.action == 'install':
                transaction.install(args.archive_sha256)
                print('Install verified; no service started; future boot behavior changed')
            else:
                transaction.rollback(args.archive_sha256)
                print('Rollback baseline verified; no service started')
    except (Blocked, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, 'BOOT TRANSACTION BLOCKED: ' + type(error).__name__ + '; preserve isolation and audit.\n')


if __name__ == '__main__':
    main()
