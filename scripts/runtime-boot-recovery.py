#!/usr/bin/env python3
"""Restore volatile identity prerequisites; never start containers or open ingress.

Default check is read-only. Installation and prepare-directory require separate
operator authorization. Frozen release/incident evidence is never changed.
"""
import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
APP = Path('/opt/clearapi/app')
DIRECTORY = Path('/run/clearapi')
HELPER = Path('/opt/clearapi/bin/runtime-identity-refresh.py')
HELPER_SHA = 'afcefce7dcf05bf853a1c44244ddf834dd9e2d6fb023f6cb75495410fc7b7b8e'
CONTAINER = 'clearapi-new-api-1'
SCRIPT = '/opt/clearapi/bin/runtime-boot-recovery.py'
FAILED_COMMIT = 'c50a53c1b89e121bcade7dac41d3f11e53a9832a'
FAILED_DIGEST = 'sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02'
FROZEN = {
    'release-manifest.json': '6c308146551dfab395e7202f775b23121ec460757e6b56b7f107decb5cae63d3',
    'pg03-target-manifest.json': '521e60ce3edbe4cd073decaedd70134daf3d22d8039d0fed4de427b2318cb39e',
    'controlled-launch-state.json': '57a7152a312eba02e2c81b2973981894640c2088782827e6d2d112b2235dff4d',
}
ASSETS = {
    '/etc/tmpfiles.d/clearapi-runtime.conf': 'd /run/clearapi 0750 root root -\n',
    '/etc/systemd/system/clearapi-runtime-directory.service': '''[Unit]
Description=Validate ClearAPI volatile runtime directory before Docker
DefaultDependencies=no
Requires=systemd-tmpfiles-setup.service
After=systemd-tmpfiles-setup.service
Before=docker.service

[Service]
Type=oneshot
ExecStart=/usr/bin/python3 -I /opt/clearapi/bin/runtime-boot-recovery.py prepare-directory
RemainAfterExit=yes
TimeoutStartSec=15
UMask=0027
''',
    '/etc/systemd/system/docker.service.d/clearapi-runtime.conf': '''[Unit]
Requires=clearapi-runtime-directory.service
After=clearapi-runtime-directory.service
''',
    '/etc/systemd/system/clearapi-runtime-identity.service': '''[Unit]
Description=Reconcile ClearAPI identity at boot without starting containers
After=docker.service clearapi-runtime-directory.service

[Service]
Type=oneshot
ExecStart=/usr/bin/python3 -I /opt/clearapi/bin/runtime-boot-recovery.py reconcile
TimeoutStartSec=60
UMask=0027

[Install]
WantedBy=multi-user.target
''',
}


class Blocked(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Blocked(message)


def safe_parents(path):
    require(path.is_absolute() and '..' not in path.parts, 'absolute canonical path required')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0
                and not (info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX),
                'unsafe parent directory')


def read_safe(path):
    safe_parents(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_gid == 0
                and info.st_nlink == 1 and not info.st_mode & 0o022, 'unsafe evidence file')
        raw = stream.read(1048577)
    require(len(raw) <= 1048576, 'oversized evidence')
    return raw


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def approved_identity(app=APP):
    raw = read_safe(app / 'release-manifest.json')
    current = json.loads(raw, object_pairs_hook=unique)['releases']['current']
    ref, revision, digest, image_id = (current[k] for k in
                                      ('image_ref', 'candidate_commit', 'image_digest', 'new_api_image_id'))
    require(re.fullmatch(r'ghcr\.io/a3225865526-wq/clearapi@sha256:[a-f0-9]{64}', ref)
            and re.fullmatch(r'[a-f0-9]{40}', revision)
            and re.fullmatch(r'sha256:[a-f0-9]{64}', image_id)
            and ref.rsplit('@', 1)[1] == digest, 'invalid release identity')
    if current.get('status') == 'failed':
        require(revision == FAILED_COMMIT and digest == FAILED_DIGEST
                and current.get('deployment_attempt', {}).get('status') == 'failed',
                'failed release is not the authorized incident')
        for name, expected in FROZEN.items():
            content = raw if name == 'release-manifest.json' else read_safe(app / name)
            require(hashlib.sha256(content).hexdigest() == expected, 'frozen incident evidence changed')
    else:
        require(current.get('status') == 'success', 'release is not successful')
    return ref, revision, image_id


def load_helper(path=HELPER):
    require(hashlib.sha256(read_safe(path)).hexdigest() == HELPER_SHA, 'identity helper drift')
    spec = importlib.util.spec_from_file_location('runtime_identity', path)
    helper = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helper)
    return helper


def directory_state(directory=DIRECTORY):
    safe_parents(directory)
    if not directory.exists() and not directory.is_symlink():
        return 'missing'
    info = directory.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0
            and stat.S_IMODE(info.st_mode) == 0o750, 'unsafe runtime directory')
    return 'ready'


@contextmanager
def deployment_lock(path=Path('/run/lock/clearapi-production-deploy.lock')):
    safe_parents(path)
    fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_gid == 0
                and info.st_nlink == 1 and not info.st_mode & 0o022, 'unsafe deployment lock')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        os.close(fd)


def check_image(helper, expected, directory=DIRECTORY):
    ref, revision, image_id = expected
    # Fixed projections intentionally exclude Config.Env, secrets, logs and SQL.
    data = helper.docker_json('inspect', '--format',
                             '[{"Image":{{json .Image}},"Ref":{{json .Config.Image}},'
                             '"Running":{{json .State.Running}},"Restarting":{{json .State.Restarting}}}]',
                             CONTAINER)[0]
    require(data['Image'] == image_id and data['Ref'] == ref, 'container image drift')
    image = helper.docker_json('image', 'inspect', '--format',
                              '[{"Id":{{json .Id}},"Digests":{{json .RepoDigests}},'
                              '"Revision":{{index .Config.Labels "org.opencontainers.image.revision" | json}}}]',
                              image_id)[0]
    require(image['Id'] == image_id and ref in (image['Digests'] or [])
            and image['Revision'] == revision, 'image digest or revision drift')
    if data['Running'] and not data['Restarting']:
        helper.observe(CONTAINER, ref, revision, directory)
        return 'running'
    return 'stopped'


def reconcile(helper, expected, directory=DIRECTORY, wait_seconds=45):
    require(directory_state(directory) == 'ready', 'runtime directory missing')
    deadline = time.monotonic() + wait_seconds
    while True:
        require(approved_identity() == expected, 'release changed while waiting')
        if check_image(helper, expected, directory) == 'running':
            result = helper.reconcile(CONTAINER, expected[0], expected[1], directory)
            require(approved_identity() == expected, 'release changed during reconciliation')
            return result
        require(time.monotonic() < deadline, 'Gateway not running; no start attempted')
        time.sleep(min(2, max(0, deadline - time.monotonic())))


def check_assets():
    missing = 0
    for name, content in ASSETS.items():
        path = Path(name)
        if path.parent.exists():
            safe_parents(path)
        else:
            require(path.parent == Path('/etc/systemd/system/docker.service.d'), 'unexpected missing parent')
            safe_parents(path.parent)
        if path.exists() or path.is_symlink():
            require(read_safe(path) == content.encode(), 'installed asset differs; manual review required')
        else:
            missing += 1
    return 'ready' if missing == 0 else 'pending'


def install():
    require(Path(__file__).resolve() == Path(SCRIPT), 'install approved script at fixed path first')
    read_safe(Path(SCRIPT))
    load_helper()
    approved_identity()
    directory_state()
    # Validate every existing target before the first write. Never overwrite drift.
    check_assets()
    for name, content in ASSETS.items():
        path = Path(name)
        if not path.parent.exists():
            path.parent.mkdir(mode=0o755)
        if not path.exists():
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
            with os.fdopen(fd, 'wb') as stream:
                stream.write(content.encode())
                stream.flush()
                os.fsync(stream.fileno())
    subprocess.run(['systemctl', 'daemon-reload'], check=True, timeout=15)
    subprocess.run(['systemctl', 'enable', 'clearapi-runtime-identity.service'], check=True, timeout=15)


def check_installed(command=subprocess.run):
    require(Path(__file__).resolve() == Path(SCRIPT), 'installed entry point required')
    read_safe(Path(SCRIPT))
    load_helper()
    approved_identity()
    directory_state()  # Missing volatile directory is expected before recovery.
    require(check_assets() == 'ready', 'boot assets not installed')
    properties = ('LoadState', 'UnitFileState', 'NeedDaemonReload', 'FragmentPath',
                  'DropInPaths', 'After', 'Before', 'Requires', 'Wants')
    loaded = {}
    for unit in ('docker.service', 'clearapi-runtime-directory.service', 'clearapi-runtime-identity.service'):
        result = command(['systemctl', 'show', unit, '--property=' + ','.join(properties)],
                         check=True, capture_output=True, text=True, timeout=10)
        values = dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)
        require(values.get('LoadState') == 'loaded' and values.get('NeedDaemonReload') == 'no',
                'systemd assets not loaded; daemon reload required')
        loaded[unit] = values
    directory = loaded['clearapi-runtime-directory.service']
    identity = loaded['clearapi-runtime-identity.service']
    docker = loaded['docker.service']
    for name, values in (('clearapi-runtime-directory.service', directory),
                         ('clearapi-runtime-identity.service', identity)):
        require(values.get('FragmentPath') == '/etc/systemd/system/' + name
                and not values.get('DropInPaths'), 'unexpected boot unit override')
    require(identity.get('UnitFileState') == 'enabled', 'identity boot unit not enabled')
    require('systemd-tmpfiles-setup.service' in directory.get('After', '').split()
            and 'systemd-tmpfiles-setup.service' in directory.get('Requires', '').split()
            and 'docker.service' in directory.get('Before', '').split()
            and 'clearapi-runtime-directory.service' in docker.get('After', '').split()
            and 'clearapi-runtime-directory.service' in docker.get('Requires', '').split()
            and '/etc/systemd/system/docker.service.d/clearapi-runtime.conf'
            in docker.get('DropInPaths', '').split()
            and 'docker.service' in identity.get('After', '').split()
            and 'docker.service' not in (identity.get('Requires', '') + ' ' + identity.get('Wants', '')).split(),
            'effective systemd ordering differs')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', nargs='?', default='check', choices=(
        'check', 'check-installed', 'plan', 'install', 'prepare-directory', 'reconcile'))
    args = parser.parse_args()
    try:
        if args.mode == 'plan':
            print(json.dumps(ASSETS, indent=2))
            return
        require(os.geteuid() == 0, 'root required')
        if args.mode == 'install':
            install()
            print('Boot assets installed and enabled; no service started; runtime directory unchanged')
        elif args.mode == 'check-installed':
            check_installed()
            print('Installed boot assets PASS; effective ordering and enablement verified; no service started')
        elif args.mode == 'prepare-directory':
            directory_state()
            config = Path('/etc/tmpfiles.d/clearapi-runtime.conf')
            require(read_safe(config).decode() == ASSETS[str(config)], 'tmpfiles configuration drift')
            subprocess.run(['/usr/bin/systemd-tmpfiles', '--create', str(config)], check=True, timeout=10)
            require(directory_state() == 'ready', 'runtime directory not prepared')
            print('Runtime directory ready; no container operation performed')
        else:
            helper, expected = load_helper(), approved_identity()
            if args.mode == 'check':
                directory = directory_state()
                state = check_image(helper, expected)
                assets = check_assets()
                identity = 'unavailable'
                if directory == 'ready' and state == 'running':
                    try:
                        helper.refresh(CONTAINER, expected[0], expected[1], DIRECTORY, check=True)
                        identity = 'current'
                    except helper.StaleIdentityError:
                        identity = 'stale'
                print('Read-only check PASS: runtime=' + directory + '; Gateway=' + state
                      + '; assets=' + assets + '; identity=' + identity)
            else:
                with deployment_lock():
                    result = reconcile(helper, expected)
                print('Runtime identity ' + result + '; no container operation performed')
    except Exception as error:
        # No command output or raw evidence is printed, even on failures.
        parser.exit(1, 'Runtime boot recovery BLOCKED: ' + type(error).__name__ + '; retain isolation.\n')


if __name__ == '__main__':
    main()
