#!/usr/bin/env python3
"""Create an exact PG03 target from verified production release authority.

Root-owned, allowlisted entry point. Credentials arrive on stdin and are never
logged or persisted. It cannot run deployment commands or write arbitrary paths.
"""
import hashlib
import io
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

REPO = 'a3225865526-wq/Clear-API'
IMAGE = 'ghcr.io/a3225865526-wq/clearapi'
EXECUTOR = 'ghcr.io/a3225865526-wq/clearapi-executor@sha256:0838072c3fd61165ba20f78084dafe678114dd5c8b19a1e8b4acd31eee2ad821'
EXECUTOR_REVISION = 'e5f33dc6412d17bd058bd3de2e053c3b45b9a359'
EXECUTOR_BINARY = '527b1814c7b4ef1ec52b2f416ec329192ffb50dc1e9b819763e7a82b0965c898'
V2_SHA = '92d89d54be647b53d923b20fb3b8e97100e10d11edfdd2257baeee8178534abe'
TARGET = Path('/opt/clearapi/app/pg03-target-manifest.json')


class ReleaseBlocked(ValueError):
    pass


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ReleaseBlocked('duplicate JSON field')
        result[key] = value
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def api(path, token, archive=False):
    url = 'https://api.github.com/repos/' + REPO + '/' + path
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, headers={
        'Authorization': 'Bearer ' + token, 'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'ClearAPI-PG03-release'})
    try:
        response = opener.open(request, timeout=30)
    except urllib.error.HTTPError as error:
        # Artifact storage is a different origin: never forward Authorization.
        if not archive or error.code != 302:
            raise ReleaseBlocked('GitHub evidence unavailable') from None
        location = error.headers.get('Location', '')
        if not location.startswith('https://'):
            raise ReleaseBlocked('invalid artifact redirect')
        response = opener.open(location, timeout=30)
    with response:
        raw = response.read(1048577)
    if len(raw) > 1048576:
        raise ReleaseBlocked('oversized GitHub evidence')
    return raw if archive else json.loads(raw, object_pairs_hook=unique)


def approved_run(data, get):
    commit = data['commit']
    if (not re.fullmatch(r'[a-f0-9]{40}', commit)
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', data['digest'])
            or any(not re.fullmatch(r'[1-9][0-9]*', str(data[k])) for k in ('run', 'rc_run'))):
        raise ReleaseBlocked('invalid release identity')
    run = get('actions/runs/' + str(data['run']))
    if (run['head_sha'] != commit or run['head_branch'] != 'main'
            or run['event'] != 'workflow_dispatch' or run['status'] != 'in_progress'
            or run['path'] != '.github/workflows/deploy-production.yml'
            or run['repository']['full_name'] != REPO
            or get('git/ref/heads/main')['object']['sha'] != commit):
        raise ReleaseBlocked('deployment is not running reviewed main')
    reviews = get('actions/runs/' + str(data['run']) + '/approvals')
    if any(r['state'] == 'rejected' for r in reviews):
        raise ReleaseBlocked('production review rejected')
    reviewers = [r['user']['login'] for r in reviews
                 if r['state'] == 'approved' and r['user']['login'] == 'a3225865526-wq'
                 and any(e['id'] == 23693288049 and e['name'] == 'production'
                         for e in r['environments'])]
    if not reviewers:
        autonomous_authority(data, get)
    rc = approved_rc(data, get)
    return sorted(set(reviewers)), rc


def autonomous_authority(data, get):
    """Verify owner dispatch and the actual environment policy, never invent review."""
    run = get('actions/runs/' + str(data['run']))
    owner = REPO.split('/')[0]
    if (run.get('actor', {}).get('login') != owner
            or run.get('triggering_actor', {}).get('login') != owner):
        raise ReleaseBlocked('autonomous deployment requires owner dispatch')
    environment = get('environments/production')
    if (environment.get('id') != 23693288049 or environment.get('name') != 'production'
            or environment.get('deployment_branch_policy') != {
                'protected_branches': False, 'custom_branch_policies': True}
            or any(r.get('type') != 'branch_policy'
                   for r in environment.get('protection_rules', []))):
        raise ReleaseBlocked('autonomous production environment policy mismatch')
    policies = get('environments/production/deployment-branch-policies')['branch_policies']
    if len(policies) != 1 or policies[0].get('name') != 'main' or policies[0].get('type') != 'branch':
        raise ReleaseBlocked('production must allow only main')
    return {'mode': 'owner-dispatch-v1', 'actor': owner, 'run': str(data['run'])}


def approved_rc(data, get):
    commit = data['commit']
    if (not re.fullmatch(r'[a-f0-9]{40}', commit)
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', data['digest'])
            or not re.fullmatch(r'[1-9][0-9]*', str(data['rc_run']))
            or get('git/ref/heads/main')['object']['sha'] != commit):
        raise ReleaseBlocked('RC must identify current main')
    rc = get('actions/runs/' + str(data['rc_run']))
    if (rc['head_sha'] != commit or rc['head_branch'] != 'main'
            or rc['event'] != 'workflow_dispatch' or rc['status'] != 'completed'
            or rc['conclusion'] != 'success'
            or rc['path'] != '.github/workflows/release-pg03-gateway.yml'
            or rc['repository']['full_name'] != REPO):
        raise ReleaseBlocked('RC source or workflow mismatch')
    ci = get('actions/workflows/ci.yml/runs?event=push&head_sha=' + commit + '&per_page=100')
    if not any(r['head_sha'] == commit and r.get('head_branch') == 'main' and r['status'] == 'completed'
               and r['conclusion'] == 'success' for r in ci['workflow_runs']):
        raise ReleaseBlocked('same-source push CI required')
    return rc


def release_manifest(data, rc, artifacts, download):
    name = f"pg03-gateway-release-{data['rc_run']}-{rc['run_attempt']}"
    matches = [a for a in artifacts['artifacts'] if a['name'] == name and not a['expired']]
    if len(matches) != 1:
        raise ReleaseBlocked('exact RC artifact required')
    with zipfile.ZipFile(io.BytesIO(download(matches[0]['id']))) as z:
        entries = [i for i in z.infolist() if i.filename == 'gateway-image-release.json']
        if len(entries) != 1 or entries[0].file_size > 131072:
            raise ReleaseBlocked('invalid RC manifest archive')
        raw = z.read(entries[0])
    m = json.loads(raw, object_pairs_hook=unique)
    required_checks = ('source_commit', 'targeted_accountpool_routechain_tests',
                       'deployment_binaries', 'platform', 'oci_revision',
                       'digest_pull', 'binary_roundtrip')
    if (m['source_commit'] != data['commit'] or m['oci_revision'] != data['commit']
            or m['workflow_commit'] != data['commit']
            or m['source_repository'] != 'https://github.com/' + REPO
            or str(m['workflow_run']) != str(data['rc_run'])
            or str(m['workflow_run_attempt']) != str(rc['run_attempt'])
            or m['image_digest'] != data['digest']
            or m['image_ref'] != IMAGE + '@' + data['digest']
            or m['platform'] != 'linux/amd64' or m['deployment_compatible'] is not True
            or m['missing_deployment_binaries']
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', m['new_api_image_id'])
            or not re.fullmatch(r'[a-f0-9]{64}', m['binary_sha256'])
            or any(m['verification'].get(k) != 'passed' for k in required_checks)):
        raise ReleaseBlocked('RC manifest does not identify this candidate')
    return m, hashlib.sha256(raw).hexdigest()


def executor_identity(docker):
    ids = docker('ps', '-aq', '--filter', 'label=com.docker.compose.service=pool-executor').splitlines()
    if len(ids) != 1:
        raise ReleaseBlocked('exactly one prepared Executor required')
    c = json.loads(docker('inspect', ids[0]))[0]
    env = dict(v.split('=', 1) for v in c['Config']['Env'])
    i = json.loads(docker('image', 'inspect', EXECUTOR))[0]
    if (c['Image'] != i['Id'] or c['Config']['Image'] != EXECUTOR
            or EXECUTOR not in i.get('RepoDigests', [])
            or i['Config']['Labels']['org.opencontainers.image.revision'] != EXECUTOR_REVISION
            or i['Os'] != 'linux' or i['Architecture'] != 'amd64'
            or not c['State']['Running'] or c['State'].get('Restarting')
            or c['State'].get('Health', {}).get('Status') != 'healthy'
            or c['HostConfig'].get('PortBindings') or c['HostConfig'].get('NetworkMode') == 'host'
            or 'clearapi_internal' not in c['NetworkSettings']['Networks']
            or any(c['NetworkSettings'].get('Ports', {}).values())
            or docker('exec', ids[0], 'sha256sum', '/proc/1/exe') != EXECUTOR_BINARY + '  /proc/1/exe'
            or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', env['CLEARAPI_POOL_EXECUTOR_ID'])
            or not re.fullmatch(r'[a-f0-9]{32}', env['CLEARAPI_PG02_PROCESS_EPOCH'])):
        raise ReleaseBlocked('prepared Executor identity mismatch')
    after = json.loads(docker('inspect', ids[0]))[0]
    if (any(c[k] != after[k] for k in ('Id', 'Image'))
            or c['State']['StartedAt'] != after['State']['StartedAt']
            or not after['State']['Running'] or after['State'].get('Restarting')
            or after['State'].get('Health', {}).get('Status') != 'healthy'):
        raise ReleaseBlocked('Executor changed during verification')
    return {'image_ref': EXECUTOR, 'image_digest': EXECUTOR.split('@')[1],
            'image_id': i['Id'], 'oci_revision': EXECUTOR_REVISION,
            'binary_sha256': EXECUTOR_BINARY, 'container_id': c['Id'],
            'executor_id': env['CLEARAPI_POOL_EXECUTOR_ID'],
            'process_epoch': env['CLEARAPI_PG02_PROCESS_EPOCH']}


def install_target(target, raw):
    for directory in (target.parent, *target.parent.parents):
        s = directory.lstat()
        if (not stat.S_ISDIR(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
            raise ReleaseBlocked('unsafe target directory')
    # Publish a complete file exclusively. A retry of the identical approved run
    # is idempotent; another run or pair cannot overwrite an earlier approval.
    fd, temporary = tempfile.mkstemp(prefix='.pg03-target-', dir=target.parent)
    try:
        with os.fdopen(fd, 'wb') as f:
            f.write(raw)
            f.flush()
            os.fsync(f.fileno())
        try:
            os.link(temporary, target, follow_symlinks=False)
        except FileExistsError:
            fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(fd, 'rb') as f:
                s = os.fstat(f.fileno())
                if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                        or stat.S_IMODE(s.st_mode) != 0o600 or s.st_nlink != 1
                        or f.read(len(raw) + 1) != raw):
                    raise ReleaseBlocked('existing approval differs; preserve for operator review')
    finally:
        os.unlink(temporary)


def maintenance_authorization(data, manifest, manifest_sha, reviewers, get, paths=None):
    if not data.get('transition_sha256'):
        return None
    # GitHub's immutable display title binds the actual production review to
    # the dispatch request digest; caller-supplied approval booleans cannot.
    run = get('actions/runs/' + str(data['run']))
    expected = 'Deploy pg03 ' + data['digest'] + ' ' + data['transition_sha256']
    authorization = None if reviewers else autonomous_authority(data, get)
    if (reviewers not in ([], ['a3225865526-wq']) or run.get('display_title') != expected
            or not re.fullmatch(r'[a-f0-9]{64}', data['transition_sha256'])):
        raise ReleaseBlocked('production review is not for this maintenance request')
    location = importlib.util.spec_from_file_location('pg03_legacy_transition', Path(__file__).with_name('pg03-legacy-transition.py'))
    module = importlib.util.module_from_spec(location)
    location.loader.exec_module(module)
    paths = paths or module.Paths()
    raw = module.read_file(paths.request)
    request = json.loads(raw, object_pairs_hook=unique)
    candidate = {'image_ref': manifest['image_ref'], 'image_digest': manifest['image_digest'],
        'image_id': manifest['new_api_image_id'], 'commit': manifest['source_commit'],
        'binary_sha256': manifest['binary_sha256'], 'retire_binary_sha256': manifest.get('legacy_retire_binary_sha256'),
        'rc_run': str(data['rc_run']), 'rc_manifest_sha256': manifest_sha}
    if (hashlib.sha256(raw).hexdigest() != data['transition_sha256'] or request['schema_version'] != module.SCHEMA
            or request['candidate'] != candidate or request.get('schema_transition') != manifest.get('schema_transition')
            or not re.fullmatch(r'[a-f0-9]{64}', candidate['retire_binary_sha256'] or '')
            or request['expires_at'] <= time.time()):
        raise ReleaseBlocked('maintenance request identity, RC or expiry mismatch')
    authority = {'schema_version': module.SCHEMA, 'transition_id': request['transition_id'],
        'request_sha256': data['transition_sha256'], 'approval_run': str(data['run']),
        'reviewers': reviewers, 'candidate': request['candidate'], 'schema_transition': request['schema_transition']}
    if authorization is not None:
        authority['authorization'] = authorization
    install_target(paths.authorization, (json.dumps(authority, sort_keys=True) + '\n').encode())
    return {'schema_version': module.SCHEMA, 'request_sha256': data['transition_sha256']}


def main():
    if os.geteuid() != 0 or sys.argv[1:] != ['prepare']:
        raise ReleaseBlocked('root prepare entry point required')
    data = json.loads(sys.stdin.buffer.read(32769), object_pairs_hook=unique)
    token = data.pop('token')
    get = lambda path: api(path, token)
    reviewers, rc = approved_run(data, get)
    m, manifest_sha = release_manifest(data, rc,
        get('actions/runs/' + str(data['rc_run']) + '/artifacts?per_page=100'),
        lambda ident: api('actions/artifacts/' + str(ident) + '/zip', token, archive=True))
    if hashlib.sha256(Path('/opt/clearapi/bin/production-deploy-v2.sh').read_bytes()).hexdigest() != V2_SHA:
        raise ReleaseBlocked('installed V2 identity mismatch')
    def docker(*args):
        return subprocess.run(['docker', *args], check=True, capture_output=True,
                              text=True, timeout=30).stdout.strip()
    e = executor_identity(docker)
    transition = maintenance_authorization(data, m, manifest_sha, reviewers, get)
    target = {'schema_version': 1, 'pool_mode': 'pg03', 'components': {
        'gateway': {'image_ref': m['image_ref'], 'image_digest': m['image_digest'],
                    'image_id': m['new_api_image_id'], 'oci_revision': m['oci_revision'],
                    'binary_sha256': m['binary_sha256']}, 'executor': e},
        'validation': {'reviewers': reviewers,
                       'approval_run': str(data['run']), 'rc_run': str(data['rc_run']),
                       'rc_manifest_sha256': manifest_sha}}
    if reviewers:
        target['validation']['approval'] = 'passed'
    else:
        target['validation']['authorization'] = autonomous_authority(data, get)
    if transition is not None:
        target['legacy_transition'] = transition
    install_target(TARGET, (json.dumps(target, indent=2) + '\n').encode())
    print('PG03 exact target created from verified release authority and RC artifact')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Neither HTTP errors, Docker output nor credential-bearing input belongs in logs.
        reason = str(error) if type(error) is ReleaseBlocked else type(error).__name__
        print('PG03 target preparation BLOCKED: ' + reason + '; no deployment attempted', file=sys.stderr)
        sys.exit(1)
