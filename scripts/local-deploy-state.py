#!/usr/bin/env python3
"""Current deployment metadata after adopting the existing running instance.

The failed release and isolation journal remain immutable incident evidence.
This module reads metadata only; it never grants authority or operates Docker.
"""
import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile

NAME = 'local-deploy-state.json'
BASE_FILES = ('docker-compose.prod.yml', 'compose-pg03.yaml', 'compose-candidate.yaml',
              'compose-website-recovery-39957948359.json')
FROZEN = {
    'release-manifest.json': '6c308146551dfab395e7202f775b23121ec460757e6b56b7f107decb5cae63d3',
    'pg03-target-manifest.json': '521e60ce3edbe4cd073decaedd70134daf3d22d8039d0fed4de427b2318cb39e',
    'controlled-launch-state.json': '57a7152a312eba02e2c81b2973981894640c2088782827e6d2d112b2235dff4d',
}


def read_file(path, private=False):
    path = Path(path)
    for parent in reversed(path.parents):
        info = parent.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX):
            raise ValueError('unsafe deployment metadata directory')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or info.st_nlink != 1 or info.st_mode & 0o022
                or private and stat.S_IMODE(info.st_mode) != 0o600):
            raise ValueError('unsafe deployment metadata file: '+str(path))
        raw = stream.read(1048577)
    if len(raw) > 1048576:
        raise ValueError('oversized deployment metadata')
    return raw


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate deployment metadata field')
        result[key] = value
    return result


def environment_file(app):
    return Path(app).parent / 'config/pg03/pg03.env'


def compose_environment():
    # --env-file must win over an inherited shell's interpolation variables.
    return {key: value for key, value in os.environ.items()
            if key in ('PATH', 'HOME', 'LANG') or key.startswith('LC_')}


def publish(path, data):
    path = Path(path)
    # Validate the destination parent through the same reader before publication.
    read_file(path.parent / 'release-manifest.json')
    raw = (json.dumps(data, sort_keys=True, indent=2) + '\n').encode()
    fd, temporary = tempfile.mkstemp(prefix='.local-adoption-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            os.fchmod(stream.fileno(), 0o600)
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, path, follow_symlinks=False)
        directory = os.open(path.parent, os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        os.unlink(temporary)


def load(app):
    app = Path(app)
    data = json.loads(read_file(app / NAME, private=True), object_pairs_hook=unique)
    if (data.get('schema') != 'clearapi.local-deploy-state/v1'
            or data['adoption']['original_incident'] != FROZEN
            or set(data['adoption']['compose_sha256']) != set(BASE_FILES)):
        raise ValueError('unsupported running-instance adoption')
    for name, digest in {**FROZEN, **data['adoption']['compose_sha256']}.items():
        if hashlib.sha256(read_file(app / name)).hexdigest() != digest:
            raise ValueError('adopted incident or original Compose changed')
    if hashlib.sha256(read_file(environment_file(app), private=True)).hexdigest() != data['adoption']['environment_sha256']:
        raise ValueError('adopted Compose environment changed')
    return data


def manifest_path(app):
    app = Path(app)
    candidate = app / NAME
    if candidate.exists() or candidate.is_symlink():
        load(app)
        return candidate
    return app / 'release-manifest.json'


def compose_args(app, override=None):
    app = Path(app)
    load(app)
    read_file(environment_file(app), private=True)
    args = ['docker', 'compose', '--project-directory', str(app), '--env-file', str(environment_file(app))]
    for name in BASE_FILES:
        args += ['-f', str(app / name)]
    args += ['-f', str(override or app / 'compose-local.yaml')]
    return args
