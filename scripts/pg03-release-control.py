#!/usr/bin/env python3
"""Create an exact PG03 target from verified production release authority.

Root-owned, allowlisted entry point. Credentials arrive on stdin and are never
logged or persisted. It cannot run deployment commands or write arbitrary paths.
"""
import hashlib
from contextlib import contextmanager
import fcntl
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
EXECUTOR = 'ghcr.io/a3225865526-wq/clearapi-executor@sha256:9ba0e5ffb0ccbcf4f1f23d272e4c1b7bb0fe86af93429c823928358ff99d34e5'
EXECUTOR_REVISION = '228ecbaa6f71ade146be789c9a3c42522a7f92a8'
EXECUTOR_BINARY = '6dff0b07e6b231abbe5a4845f2f4e588d9b3252c3d84b33f75b3ed79d336240b'
V2_SHA = '652b222f3b0f3b71a8681e1b0f6b66e919a80f983bbac4739d7ef1f391df9c0c'
TARGET = Path('/opt/clearapi/app/pg03-target-manifest.json')


class ReleaseBlocked(ValueError):
    pass


@contextmanager
def deployment_lock(path=Path('/run/lock/clearapi-production-deploy.lock')):
    # Coordinate review/reissue/prepare with the existing V2 deployment lock.
    for parent in path.parents:
        info = parent.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid()
                or (info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX)):
            raise ReleaseBlocked('unsafe deployment lock directory')
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_gid != os.getegid() or info.st_nlink != 1 or info.st_mode & 0o022):
            raise ReleaseBlocked('unsafe deployment lock')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ReleaseBlocked('deployment lock held') from None
        yield
    finally:
        os.close(fd)


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
            or run['status'] != 'in_progress' or run['repository']['full_name'] != REPO
            or get('git/ref/heads/main')['object']['sha'] != commit):
        raise ReleaseBlocked('deployment is not running reviewed main')
    if run['event'] == 'workflow_run':
        if run['path'] != '.github/workflows/automatic-deploy.yml':
            raise ReleaseBlocked('untrusted automatic deployment workflow')
        automatic_release_authority(data, get)
        return [], approved_rc(data, get)
    if (run['event'] != 'workflow_dispatch'
            or run['path'] != '.github/workflows/deploy-production.yml'):
        raise ReleaseBlocked('untrusted deployment workflow')
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


def automatic_release_authority(data, get):
    """Tie Deploy V2 to the completed proof-authorizing job in the trusted workflow."""
    if (data.get('executor_repository', '') + '@' + data.get('executor_digest', '') != EXECUTOR
            or data.get('executor_binary_sha256') != EXECUTOR_BINARY
            or data.get('executor_source_commit') != EXECUTOR_REVISION):
        raise ReleaseBlocked('Executor does not match the authenticated rehearsal proof')
    run_id = str(data['run'])
    run = get('actions/runs/' + run_id)
    if (run['event'] != 'workflow_run' or run['path'] != '.github/workflows/automatic-deploy.yml'
            or run['head_sha'] != data['commit'] or run['head_branch'] != 'main'
            or run['status'] != 'in_progress' or run['repository']['full_name'] != REPO
            or get('git/ref/heads/main')['object']['sha'] != data['commit']):
        raise ReleaseBlocked('automatic release workflow identity mismatch')
    workflow = get('actions/workflows/automatic-deploy.yml')
    if workflow['path'] != run['path'] or workflow['state'] != 'active' or workflow['id'] != run['workflow_id']:
        raise ReleaseBlocked('automatic release workflow is inactive or mismatched')
    enabled = get('actions/variables/CLEARAPI_AUTO_PRODUCTION_ENABLED')
    if enabled.get('name') != 'CLEARAPI_AUTO_PRODUCTION_ENABLED' or enabled.get('value') != 'true':
        raise ReleaseBlocked('automatic production release is disabled')
    environment = get('environments/production')
    if (environment.get('id') != 23693288049 or environment.get('name') != 'production'
            or environment.get('deployment_branch_policy') != {
                'protected_branches': False, 'custom_branch_policies': True}
            or any(r.get('type') != 'branch_policy'
                   for r in environment.get('protection_rules', []))):
        raise ReleaseBlocked('automatic production environment policy mismatch')
    policies = get('environments/production/deployment-branch-policies')['branch_policies']
    if len(policies) != 1 or policies[0].get('name') != 'main' or policies[0].get('type') != 'branch':
        raise ReleaseBlocked('production must allow only main')
    rehearsal_id = str(data.get('rehearsal_run', ''))
    if not re.fullmatch(r'[1-9][0-9]*', rehearsal_id):
        raise ReleaseBlocked('missing authenticated deployment rehearsal')
    rehearsal = get('actions/runs/' + rehearsal_id)
    if (rehearsal['path'] != '.github/workflows/deployment-rehearsal.yml'
            or rehearsal['event'] != 'workflow_run' or rehearsal['head_sha'] != data['commit']
            or rehearsal['head_branch'] != 'main' or rehearsal['status'] != 'completed'
            or rehearsal['conclusion'] != 'success' or rehearsal['run_attempt'] != 1
            or rehearsal['repository']['full_name'] != REPO
            or rehearsal['head_repository']['full_name'] != REPO):
        raise ReleaseBlocked('deployment rehearsal identity mismatch')
    jobs = get('actions/runs/' + run_id + '/jobs?per_page=100')['jobs']
    authorizers = [job for job in jobs
                   if job['name'].endswith('Validate release authorization before production credentials')]
    if len(authorizers) != 1:
        raise ReleaseBlocked('automatic proof authorization job is missing or duplicated')
    job = authorizers[0]
    step = next((step for step in job.get('steps', [])
                 if step['name'] == 'Authenticate automatic proof before production credentials'), None)
    if (job['status'] != 'completed' or job['conclusion'] != 'success'
            or step is None or step['status'] != 'completed' or step['conclusion'] != 'success'):
        raise ReleaseBlocked('automatic proof authorization did not pass')
    return {'mode': 'automatic-proof-v1', 'run': run_id, 'rehearsal_run': rehearsal_id}


def approved_rc(data, get):
    commit = data['commit']
    if (not re.fullmatch(r'[a-f0-9]{40}', commit)
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', data['digest'])
            or not re.fullmatch(r'[1-9][0-9]*', str(data['rc_run']))
            or get('git/ref/heads/main')['object']['sha'] != commit):
        raise ReleaseBlocked('RC must identify current main')
    rc = get('actions/runs/' + str(data['rc_run']))
    workflow = get('actions/workflows/release-pg03-gateway.yml')
    if (str(rc['id']) != str(data['rc_run'])
            or rc['head_sha'] != commit or rc['head_branch'] != 'main'
            or rc['event'] not in ('workflow_dispatch', 'workflow_run') or rc['status'] != 'completed'
            or rc['conclusion'] != 'success'
            or rc['path'] != '.github/workflows/release-pg03-gateway.yml'
            or workflow['path'] != rc['path'] or workflow['state'] != 'active'
            or rc['workflow_id'] != workflow['id']
            or rc['repository']['full_name'] != REPO
            or rc['head_repository']['full_name'] != REPO
            or rc['head_repository']['id'] != rc['repository']['id']
            or type(rc['run_attempt']) is not int or rc['run_attempt'] < 1):
        raise ReleaseBlocked('RC source or workflow mismatch')
    latest = get('actions/workflows/release-pg03-gateway.yml/runs?branch=main&head_sha='
                 + commit + '&per_page=1')['workflow_runs']
    if (not latest or latest[0]['id'] != rc['id']
            or latest[0]['run_attempt'] != rc['run_attempt']):
        raise ReleaseBlocked('superseded RC run or attempt')
    runs = get('actions/workflows/ci.yml/runs?branch=main&event=push&head_sha='
               + commit + '&per_page=1')['workflow_runs']
    if not runs:
        raise ReleaseBlocked('same-source push CI required')
    ci = get('actions/runs/' + str(runs[0]['id']))
    ci_workflow = get('actions/workflows/ci.yml')
    if (ci['id'] != runs[0]['id'] or ci['run_attempt'] != runs[0]['run_attempt']
            or ci['head_sha'] != commit or ci['head_branch'] != 'main'
            or ci['event'] != 'push' or ci['status'] != 'completed' or ci['conclusion'] != 'success'
            or ci['repository']['full_name'] != REPO or ci['head_repository']['full_name'] != REPO
            or ci['repository']['id'] != rc['repository']['id']
            or ci['head_repository']['id'] != rc['repository']['id']
            or ci['path'] != '.github/workflows/ci.yml' or ci_workflow['path'] != ci['path']
            or ci_workflow['state'] != 'active' or ci['workflow_id'] != ci_workflow['id']):
        raise ReleaseBlocked('latest same-source main push CI required')
    for run in (rc, ci):
        jobs = get(f"actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs?per_page=100")
        if (not jobs['jobs'] or jobs['total_count'] != len(jobs['jobs'])
                or any(j['status'] != 'completed' or j['conclusion'] != 'success' for j in jobs['jobs'])):
            raise ReleaseBlocked('RC or source CI has missing, failed or skipped jobs')
    return dict(rc, verified_source_ci=ci)


def release_manifest(data, rc, artifacts, download):
    name = f"pg03-gateway-release-{data['rc_run']}-{rc['run_attempt']}"
    matches = [a for a in artifacts['artifacts'] if a['name'] == name and not a['expired']]
    if len(matches) != 1:
        raise ReleaseBlocked('exact RC artifact required')
    artifact = matches[0]
    producer = artifact['workflow_run']
    if (producer['id'] != rc['id'] or producer['head_sha'] != data['commit']
            or producer['head_branch'] != 'main'
            or producer['repository_id'] != rc['repository']['id']
            or producer['head_repository_id'] != rc['repository']['id']):
        raise ReleaseBlocked('artifact producer does not identify this RC')
    archive = download(artifact['id'])
    if artifact['digest'] != 'sha256:' + hashlib.sha256(archive).hexdigest():
        raise ReleaseBlocked('RC artifact archive digest mismatch')
    files = {'gateway-image-release.json', 'binary-build-info.txt',
             'schema-rehearsal.json', 'schema-rehearsal.target.sql'}
    with zipfile.ZipFile(io.BytesIO(archive)) as z:
        entries = z.infolist()
        if (len(entries) != 5 or {i.filename for i in entries} != files | {'artifact-hashes.json'}
                or any(i.file_size > 262144 for i in entries)
                or sum(i.file_size for i in entries) > 1048576):
            raise ReleaseBlocked('invalid RC manifest archive')
        inventory = json.loads(z.read('artifact-hashes.json'), object_pairs_hook=unique)
        if (set(inventory) != files
                or any(hashlib.sha256(z.read(filename)).hexdigest() != inventory[filename] for filename in files)):
            raise ReleaseBlocked('RC artifact inventory mismatch')
        raw = z.read('gateway-image-release.json')
    m = json.loads(raw, object_pairs_hook=unique)
    required_checks = ('source_commit', 'targeted_accountpool_routechain_tests',
                       'deployment_binaries', 'platform', 'oci_revision',
                       'digest_pull', 'binary_roundtrip')
    ci = rc['verified_source_ci']
    if (m['source_commit'] != data['commit'] or m['candidate_commit'] != data['commit']
            or m['oci_revision'] != data['commit']
            or m['workflow_commit'] != data['commit']
            or m['source_repository'] != 'https://github.com/' + REPO
            or m['workflow_ref'] != REPO + '/.github/workflows/release-pg03-gateway.yml@refs/heads/main'
            or str(m['workflow_run']) != str(data['rc_run'])
            or str(m['workflow_run_attempt']) != str(rc['run_attempt'])
            or m['image_digest'] != data['digest']
            or m['image_repository'] != IMAGE
            or m['image_ref'] != IMAGE + '@' + data['digest']
            or m['platform'] != 'linux/amd64' or m['deployment_compatible'] is not True
            or m['missing_deployment_binaries']
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', m['new_api_image_id'])
            or not re.fullmatch(r'[a-f0-9]{64}', m['binary_sha256'])
            or m['runtime_identity'] != {'image_id': m['new_api_image_id'],
                'binary_path': '/new-api', 'binary_sha256': m['binary_sha256']}
            or not any(r.get('databaseId') == ci['id'] and r.get('headSha') == data['commit']
                       and r.get('status') == 'completed' and r.get('conclusion') == 'success'
                       for r in m['source_ci_runs'])
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
    module.lifecycle_ready(paths)
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


def prepare():
    transition_module = importlib.util.spec_from_file_location('maintenance_lifecycle', Path(__file__).with_name('pg03-legacy-transition.py'))
    lifecycle = importlib.util.module_from_spec(transition_module)
    transition_module.loader.exec_module(lifecycle)
    lifecycle.lifecycle_ready(lifecycle.Paths())
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
    if data.get('rehearsal_run'):
        expected_executor = {
            'image_ref': data.get('executor_repository', '') + '@' + data.get('executor_digest', ''),
            'image_digest': data.get('executor_digest', ''),
            'binary_sha256': data.get('executor_binary_sha256', ''),
            'oci_revision': data.get('executor_source_commit', ''),
        }
        if any(expected_executor[key] != e[key] for key in expected_executor):
            raise ReleaseBlocked('prepared Executor differs from authenticated rehearsal proof')
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
        target['validation']['authorization'] = (
            automatic_release_authority(data, get) if data.get('rehearsal_run')
            else autonomous_authority(data, get))
    if transition is not None:
        target['legacy_transition'] = transition
    install_target(TARGET, (json.dumps(target, indent=2) + '\n').encode())
    print('PG03 exact target created from verified release authority and RC artifact')


def main():
    if os.geteuid() != 0 or sys.argv[1:] != ['prepare']:
        raise ReleaseBlocked('root prepare entry point required')
    with deployment_lock():
        prepare()


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Neither HTTP errors, Docker output nor credential-bearing input belongs in logs.
        reason = str(error) if type(error) is ReleaseBlocked else type(error).__name__
        print('PG03 target preparation BLOCKED: ' + reason + '; no deployment attempted', file=sys.stderr)
        sys.exit(1)
