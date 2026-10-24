"""Opt-in real Windows PowerShell -> pinned loopback SSH -> helper -> Compose.

Reuses the explicitly owned DinD host; never reads production credentials.
Run with --runtime pointing to the existing disposable test key directory.
Artifacts, raw outputs and snapshots are saved in --evidence (no keys/dumps).
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time

REPO = Path(__file__).resolve().parents[1]
HOST = 'clearapi-e2e-host'
DAEMON = 'f931987c-f3aa-4151-a0bf-ad2b7d45fd8d'
APP = '/opt/clearapi-test/app'
CONFIG = '/opt/clearapi-test/config/test-config.json'
HELPER = '/opt/clearapi/bin/clearapi-local-deploy.py'
OLD = 'a032815cb6244a7ee5e812de82f8300c02554e99'
BASE = 'clearapi-local:' + OLD


def run(*args, check=True, **kwargs):
    if kwargs.get('text'):
        kwargs.setdefault('encoding', 'utf-8')
    p = subprocess.run(args, capture_output=True, **kwargs)
    if check and p.returncode:
        raise RuntimeError(f'{args[:3]}: {p.stderr[-3000:]!r}')
    return p


def remote(code):
    return run('docker', 'exec', '-i', HOST, 'python3', '-c', code,
               text=True).stdout.strip()


def install(path, destination, mode=0o600):
    run('docker', 'cp', str(path), HOST + ':' + destination)
    remote(f'import os; os.chmod({destination!r}, {mode})')


def json_file(name, value):
    path = runtime / name
    path.write_text(json.dumps(value), encoding='utf-8')
    return path


def ps(command, archive=None):
    args = ['pwsh', '-NoProfile', '-File', str(REPO / 'tests/local_deploy_transport.ps1'),
            '-Repository', str(REPO), '-Connection', str(connection), '-Command', command]
    if archive:
        args += ['-Archive', str(archive)]
    return run(*args, check=False, text=True)


def helper(mode='preflight', config=CONFIG):
    return f'sudo -n {HELPER} --config-path {config} {mode}'


def snapshot():
    return json.loads(remote(f'''
import subprocess,json,hashlib,pathlib
def cmd(*a): return subprocess.check_output(a,text=True).strip()
base=['docker','compose','--project-directory','{APP}','-f','{APP}/docker-compose.prod.yml','-f','{APP}/compose-candidate.yaml']
ids=cmd(*base,'ps','-a','-q').splitlines()
containers=json.loads(cmd('docker','inspect',*ids))
print(json.dumps({{
 'files':{{n:hashlib.sha256(pathlib.Path('{APP}',n).read_bytes()).hexdigest() for n in ('docker-compose.prod.yml','compose-candidate.yaml','release-manifest.json')}},
 'containers':{{c['Name']:{{'id':c['Id'],'image':c['Image'],'started':c['State']['StartedAt'],'running':c['State']['Running']}} for c in containers}},
 'marker':cmd(*base,'exec','-T','postgres','psql','-U','clearapitest','-d','clearapitest','-Atc','SELECT value FROM local_deploy_probe'),
 'manifest':json.loads(pathlib.Path('{APP}/release-manifest.json').read_text())
}}))
'''))


def record(name, process, before, expected, *, restored=False, unchanged=False):
    after = snapshot()
    output = process.stdout + process.stderr
    (out / (name + '.log')).write_text(output, encoding='utf-8')
    assert process.returncode != 0, (name, 'unexpected success', output)
    assert expected in output, (name, expected, output)
    assert before['marker'] == after['marker'], name
    for key, value in before['containers'].items():
        if not key.endswith('new-api-1'):
            assert after['containers'][key] == value, (name, 'shared service changed')
    if unchanged:
        assert before == after, (name, 'deployment mutated')
    if restored:
        assert before['files'] == after['files'], (name, 'config not restored')
        old = next(v for k, v in before['containers'].items() if k.endswith('new-api-1'))
        new = next(v for k, v in after['containers'].items() if k.endswith('new-api-1'))
        assert new['running'] and new['image'] == old['image'], name
        health = ps(helper())
        assert health.returncode == 0, health.stderr
    results[name] = {'result': 'PASS', 'exit': process.returncode, 'expected': expected,
                     'before': before, 'after': after,
                     'implementation_sha256': {n: hashlib.sha256((REPO / n).read_bytes()).hexdigest()
                         for n in ('scripts/deploy-local.ps1','scripts/local-deploy-vps.py','scripts/backup-identity.py')}}
    save()
    print(name + ': PASS', flush=True)


def save():
    (out / 'results.json').write_text(json.dumps({
        'source_commit': run('git', '-C', str(REPO), 'rev-parse', 'HEAD', text=True).stdout.strip(),
        'implementation_sha256': {name: hashlib.sha256((REPO / name).read_bytes()).hexdigest()
                                  for name in ('scripts/deploy-local.ps1', 'scripts/local-deploy-vps.py',
                                               'scripts/runtime-identity-refresh.py', 'scripts/backup-identity.py')},
        'docker_daemon_id': DAEMON, 'endpoint': '127.0.0.1:2222',
        'scope': 'disposable local DinD only; no production connection', 'cases': results
    }, indent=2) + '\n', encoding='utf-8')


def candidate(kind, revision):
    extra = {'crash': 'ENTRYPOINT ["/bin/false"]\n',
             'timeout': 'ENTRYPOINT ["/bin/sh", "-c", "while true; do sleep 30 | nc -l -p 3000; done"]\n',
             'good': ''}[kind]
    source = BASE if kind == 'good' else 'alpine:3.20'
    dockerfile = runtime / (kind + '.Dockerfile')
    dockerfile.write_text(f'FROM {source}\nLABEL org.opencontainers.image.revision="{revision}"\n' + extra)
    install(dockerfile, '/tmp/e2e.Dockerfile')
    remote(f'import subprocess; subprocess.run(["docker","build","--network=none","--pull=false","-t","clearapi-local:{revision}","-f","/tmp/e2e.Dockerfile","/tmp"],check=True,capture_output=True)')
    remote(f'import subprocess; subprocess.run(["docker","save","-o","/tmp/e2e.tar","clearapi-local:{revision}"],check=True)')
    archive = runtime / (kind + '.tar')
    run('docker', 'cp', HOST + ':/tmp/e2e.tar', str(archive))
    import tarfile
    with tarfile.open(archive) as bundle:
        entries = json.load(bundle.extractfile('manifest.json'))
        image = 'sha256:' + hashlib.sha256(bundle.extractfile(entries[0]['Config']).read()).hexdigest()
    return {'revision': revision, 'id': image, 'archive': archive,
            'sha': hashlib.sha256(archive.read_bytes()).hexdigest(), 'bytes': archive.stat().st_size}


def receive(c, scope='frontend', **overrides):
    previous = json.loads(ps(helper()).stdout)['image_id']
    values = dict(c, previous=previous, scope=scope)
    values.update(overrides)
    return helper('receive') + (
        ' --commit {revision} --image-id {id} --previous-image-id {previous}'
        ' --scope {scope} --archive-sha256 {sha} --archive-bytes {bytes}').format(**values)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--runtime', type=Path, required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--case', default='all')
    args = parser.parse_args()
    runtime, out = args.runtime.resolve(), args.evidence.resolve()
    out.mkdir(parents=True, exist_ok=True)
    previous = json.loads((out / 'results.json').read_text()) if (out / 'results.json').exists() else {}
    results = previous.get('cases', {})
    for case in results.values():
        case.setdefault('implementation_sha256', previous.get('implementation_sha256', {}))
    assert run('docker', 'port', HOST, '22', text=True).stdout.strip() == '127.0.0.1:2222'
    assert remote('import subprocess; print(subprocess.check_output(["docker","info","--format","{{.ID}}"],text=True).strip())') == DAEMON
    # Confirm pin from the owned container itself before using the existing key.
    hostkey = run('docker', 'exec', HOST, 'cat', '/etc/ssh/ssh_host_ed25519_key.pub', text=True).stdout.split()[:2]
    assert (runtime / 'known_hosts').read_text().split()[1:3] == hostkey
    connection = json_file('connection.json', {'host': '127.0.0.1', 'port': 2222,
        'user': 'root', 'testTarget': True, 'remoteConfigPath': CONFIG,
        'identityFile': str(runtime / 'id_ed25519'), 'knownHostsFile': str(runtime / 'known_hosts')})
    for source, destination in [('scripts/local-deploy-vps.py', HELPER),
                               ('scripts/backup-identity.py', '/opt/clearapi/bin/backup-identity.py'),
                               ('scripts/runtime-identity-refresh.py', '/opt/clearapi/bin/runtime-identity-refresh.py')]:
        install(REPO / source, destination, 0o755)
    # Owned fixture only; preserve existing DB volume and unrelated outer containers.
    remote(f'''
import json,pathlib,subprocess
p=pathlib.Path('{APP}/docker-compose.prod.yml')
s=p.read_text().replace('ports: ["3000:3000"]','ports: ["127.0.0.1:3000:3000"]')
if 'stop_grace_period:' not in s: s=s.replace('  new-api:\\n','  new-api:\\n    stop_grace_period: 1s\\n')
if 'stop_signal:' not in s: s=s.replace('  new-api:\\n','  new-api:\\n    stop_signal: SIGKILL\\n')
p.write_text(s)
subprocess.run(['docker','compose','--project-directory','{APP}','-f',str(p),'-f','{APP}/compose-candidate.yaml','up','-d'],check=True,capture_output=True)
subprocess.run(['docker','compose','--project-directory','{APP}','-f',str(p),'-f','{APP}/compose-candidate.yaml','exec','-T','postgres','psql','-U','clearapitest','-d','clearapitest','-c',"CREATE TABLE IF NOT EXISTS local_deploy_probe (value text NOT NULL); INSERT INTO local_deploy_probe SELECT 'preserve-me' WHERE NOT EXISTS (SELECT 1 FROM local_deploy_probe)"],check=True,capture_output=True)
''')
    deadline = time.monotonic() + 60
    while ps(helper()).returncode:
        assert time.monotonic() < deadline, 'baseline preflight failed'
        time.sleep(1)
    (out / 'baseline-preflight.json').write_text(ps(helper()).stdout)
    if args.case == 'full-entry':
        # Restore the actual previous image as an explicit fixture operation so
        # the shipping entrypoint can redeploy HEAD with its normal predecessor check.
        remote(f'''
import json,pathlib,subprocess
p=pathlib.Path('{APP}/release-manifest.json')
m=json.loads(p.read_text()); old=m['releases']['previous']; current=m['releases']['current']
assert old['candidate_commit'] != current['candidate_commit']
override=pathlib.Path('{APP}/compose-candidate.yaml')
override.write_text(override.read_text().replace(current['image_ref'],old['image_ref']))
m['releases']['current']=old; m['releases']['previous']={{}}; p.write_text(json.dumps(m))
subprocess.run(['docker','compose','--project-directory','{APP}','-f','{APP}/docker-compose.prod.yml','-f',str(override),'up','-d','--no-deps','new-api'],check=True,capture_output=True)
subprocess.run(['python3','-I','/opt/clearapi/bin/runtime-identity-refresh.py','--reconcile','--container','app-new-api-1','--image-ref',old['image_ref'],'--expected-revision',old['candidate_commit'],'--directory','/opt/clearapi-test/runtime/new-api'],check=True,capture_output=True)
''')
        deadline = time.monotonic() + 60
        while ps(helper()).returncode:
            assert time.monotonic() < deadline
            time.sleep(1)
        before = snapshot()
        p = run('pwsh', '-NoProfile', '-File', str(REPO / 'scripts/deploy-local.ps1'),
                '-TestTarget', '-ConfigPath', str(connection), check=False, text=True)
        (out / 'full-entry-success.log').write_text(p.stdout + p.stderr)
        assert p.returncode == 0, p.stderr
        receipt = json.loads(p.stdout)
        assert receipt['result'] == 'SUCCESS' and receipt['target_mode'] == 'isolated-test-v1'
        assert receipt['public_url'] is None and receipt['backup_id']
        after = snapshot()
        assert before['marker'] == after['marker']
        for key, value in before['containers'].items():
            if not key.endswith('new-api-1'): assert after['containers'][key] == value
        assert before['files']['docker-compose.prod.yml'] == after['files']['docker-compose.prod.yml']
        current = after['manifest']['releases']['current']
        assert current['new_api_image_id'] == receipt['image_id'] and current['status'] == 'success'
        assert current['archive_sha256'] == receipt['archive_sha256']
        check = ps('python3 -I /opt/clearapi/bin/runtime-identity-refresh.py --check --container app-new-api-1 '
                   f'--image-ref {current["image_ref"]} --expected-revision {current["candidate_commit"]} --directory /opt/clearapi-test/runtime/new-api')
        assert check.returncode == 0, check.stderr
        (out / 'success-runtime-check.log').write_text(check.stdout)
        proof = remote(f"from pathlib import Path; print(Path('/opt/clearapi-test/backups/manifests/{receipt['backup_id']}.json').read_text())")
        (out / 'success-backup-proof.json').write_text(proof)
        capture = ps(f'python3 -I /opt/clearapi/bin/backup-identity.py capture --app-dir {APP}')
        assert capture.returncode == 0, capture.stderr
        (out / 'success-production-backup-identity.json').write_text(capture.stdout)
        results['full-entry-success'] = {'result': 'PASS', 'exit': p.returncode,
                                         'receipt': receipt, 'before': before, 'after': after}
        save()
        print('full-entry-success: PASS', flush=True)
        raise SystemExit(0)
    crash = candidate('crash', '1' * 40) if args.case in ('all', 'frontend-crash-rollback', 'backend') else None
    timeout = candidate('timeout', '2' * 40) if args.case in ('all', 'health-timeout-rollback') else None
    good = candidate('good', '3' * 40) if args.case in ('all', 'backup', 'identity', 'concurrency') else None
    for name, image, expected in [('frontend-crash-rollback', crash, 'previous exact image restored and healthy'),
                                   ('health-timeout-rollback', timeout, 'health readiness timed out; health probe failed (TimeoutError)')]:
        if args.case not in ('all', name): continue
        before = snapshot()
        start = time.monotonic()
        process = ps(receive(image), image['archive'])
        elapsed = time.monotonic() - start
        record(name, process, before, expected, restored=True)
        results[name]['elapsed_seconds'] = elapsed
        save()
    if args.case in ('all', 'backup'):
        cfg = json.loads(remote(f'import pathlib; print(pathlib.Path({CONFIG!r}).read_text())'))
        # Real pg_dump failure, not the test_backup_fail synthetic switch.
        bad = dict(cfg, db_name='nonexistent_e2e_database')
        install(json_file('backup-fail.json', bad), '/opt/clearapi-test/config/backup-fail.json')
        before = snapshot()
        command = receive(good, scope='backend').replace(CONFIG, '/opt/clearapi-test/config/backup-fail.json')
        record('backup-failure', ps(command, good['archive']), before,
               'isolated PostgreSQL backup failed', unchanged=True)
    if args.case in ('all', 'identity'):
        for name, override, expected in [
            ('archive-sha-mismatch', {'sha': '0' * 64}, 'archive checksum mismatch'),
            ('archive-size-mismatch', {'bytes': good['bytes'] + 1}, 'archive byte count mismatch'),
            ('archive-image-mismatch', {'id': 'sha256:' + '0' * 64}, 'archive config ID mismatches local build')]:
            before = snapshot()
            record(name, ps(receive(good, **override), good['archive']), before, expected, unchanged=True)
        before = snapshot()
        # Correct archive/config ID, incorrect declared source revision.
        remote('import subprocess; subprocess.run(["docker","tag","clearapi-local:' + good['revision'] + '","clearapi-local:' + '4' * 40 + '"],check=True)')
        record('archive-revision-mismatch', ps(receive(good, revision='4' * 40), good['archive']),
               before, 'loaded image revision or architecture mismatch', unchanged=True)
    if args.case in ('all', 'concurrency'):
        before = snapshot()
        command = receive(good)
        first = subprocess.Popen(['pwsh', '-NoProfile', '-File', str(REPO / 'tests/local_deploy_transport.ps1'),
            '-Repository', str(REPO), '-Connection', str(connection), '-Command', command],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 30
            while True:
                locked = remote('''
import fcntl
with open('/run/lock/clearapi-production-deploy.lock','ab') as f:
 try: fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB); print('free')
 except BlockingIOError: print('held')
''')
                if locked == 'held': break
                assert first.poll() is None and time.monotonic() < deadline, 'first receive did not acquire lock'
                time.sleep(.2)
            record('concurrent-deploy-blocked', ps(command, good['archive']), before,
                   'another production deploy holds the lock', unchanged=True)
        finally:
            first_out, first_err = first.communicate(timeout=30)
            (out / 'concurrent-lock-holder.log').write_bytes(first_out + first_err)
        assert first.returncode and b'archive byte count mismatch' in first_err
    if args.case in ('all', 'isolation'):
        cfg = json.loads(remote(f'import pathlib; print(pathlib.Path({CONFIG!r}).read_text())'))
        remote("from pathlib import Path; p=Path('/opt/clearapi-test/escape-runtime'); p.symlink_to('/opt/clearapi/runtime/new-api',target_is_directory=True) if not p.is_symlink() else None")
        for name, delta in [
            ('non-loopback-url', {'local_url': 'http://192.0.2.1:3000/api/status'}),
            ('production-compose', {'app_dir': '/opt/clearapi/app'}),
            ('production-runtime', {'runtime_identity_dir': '/opt/clearapi/runtime/new-api'}),
            ('daemon-id', {'docker_daemon_id': 'incorrect-daemon'}),
            ('test-contract', {'mode': 'production'}),
            ('production-backup', {'backup_dir': '/opt/clearapi/backups'}),
            ('symlink-runtime', {'runtime_identity_dir': '/opt/clearapi-test/escape-runtime'}),
            ('traversal-app', {'app_dir': '/opt/clearapi-test/../clearapi/app'}),
            ('traversal-runtime', {'runtime_identity_dir': '/opt/clearapi-test/../clearapi/runtime/new-api'})]:
            path = '/opt/clearapi-test/config/invalid.json'
            install(json_file('invalid.json', dict(cfg, **delta)), path)
            before = snapshot()
            process = ps(helper(config=path))
            expected = 'unsafe isolated test config'
            if name in ('daemon-id', 'test-contract'): expected = 'isolated test config identity mismatch'
            if name.startswith(('traversal-', 'symlink-')): expected = 'unsafe isolated test path'
            record('isolation-' + name, process, before, expected, unchanged=True)
        spoofed = dict(cfg, local_url='http://127.0.0.1:3000@192.0.2.1/api/status',
                       public_url='http://127.0.0.1:3000@192.0.2.1/api/status')
        install(json_file('invalid.json', spoofed), '/opt/clearapi-test/config/invalid.json')
        before = snapshot()
        record('isolation-url-userinfo', ps(helper(config='/opt/clearapi-test/config/invalid.json')),
               before, 'unsafe isolated test URL', unchanged=True)
        before = snapshot()
        process = run('pwsh', '-NoProfile', '-File', str(REPO / 'scripts/deploy-local.ps1'),
                      '-DryRun', '-TestTarget', '-ConfigPath', str(json_file('non-loopback.json',
                      {'host': '192.0.2.1', 'testTarget': True, 'remoteConfigPath': CONFIG})),
                      check=False, text=True)
        record('isolation-windows-target', process, before, 'loopback configuration', unchanged=True)
        # Use production-mode readers inside this disposable host only.
        # No production target/configuration or key is contacted.
        remote("from pathlib import Path; Path('/opt/clearapi/config').mkdir(parents=True,exist_ok=True,mode=0o700)")
        install(json_file('test-as-production.json', cfg), '/opt/clearapi/config/local-deploy.json')
        remote("from pathlib import Path; p=Path('/opt/clearapi/config/local-deploy.enabled'); p.write_text('LOCAL_DEPLOY_CUTOVER_APPROVED_V1'); p.chmod(0o600)")
        before = snapshot()
        record('isolation-test-config-in-production-mode', ps(f'sudo -n {HELPER} preflight'), before,
               'unsupported layout or local cutover not activated', unchanged=True)
        process = run('pwsh', '-NoProfile', '-File', str(REPO / 'scripts/deploy-local.ps1'),
                      '-DryRun', '-ConfigPath', str(connection), check=False, text=True)
        record('isolation-windows-test-without-switch', process, before,
               'Test configuration requires -TestTarget', unchanged=True)
        remote("from pathlib import Path; import shutil; Path('/opt/clearapi/runtime/new-api').mkdir(parents=True,exist_ok=True,mode=0o750); shutil.copy2('/opt/clearapi-test/runtime/new-api/runtime-identity.json','/opt/clearapi/runtime/new-api/runtime-identity.json')")
        current = before['manifest']['releases']['current']
        record('isolation-runtime-identity-production-mount', ps(
            'python3 -I /opt/clearapi/bin/runtime-identity-refresh.py --check --container app-new-api-1 '
            f'--image-ref {current["image_ref"]} --expected-revision {current["candidate_commit"]} --directory /opt/clearapi/runtime/new-api'),
            before, 'Runtime identity BLOCKED', unchanged=True)
        proof = remote("from pathlib import Path; print(str(sorted(Path('/opt/clearapi-test/backups/manifests').glob('*.json'))[-1]))")
        record('isolation-test-backup-proof-production-reader', ps(
            f'python3 -I /opt/clearapi/bin/backup-identity.py check --app-dir {APP} --snapshot {proof}'),
            before, 'running identity changed during backup', unchanged=True)
        capture = ps(f'python3 -I /opt/clearapi/bin/backup-identity.py capture --app-dir {APP}')
        assert capture.returncode == 0, capture.stderr
        (out / 'archive-backup-production-identity.json').write_text(capture.stdout)
    if args.case in ('all', 'isolation', 'sudo'):
        for name, command, expected in [
            ('sudo-default-preflight', f'{HELPER} preflight', 'unsupported layout or local cutover not activated'),
            ('sudo-forbids-test-config', f'{HELPER} --config-path {CONFIG} preflight', 'sudo: a password is required'),
            ('sudo-forbids-shell', '/bin/sh -c id', 'sudo: a password is required'),
            ('sudo-receive-parser-forbids-test-config', f'{HELPER} receive --commit {"5"*40} --image-id sha256:{"0"*64} --previous-image-id sha256:{"0"*64} --archive-sha256 {"0"*64} --archive-bytes 1 --scope frontend --config-path {CONFIG}', 'unrecognized arguments: --config-path')]:
            before = snapshot()
            process = ps('sudo -u clearadmin /usr/bin/sudo -n ' + command)
            record(name, process, before, expected, unchanged=True)
    if args.case in ('all', 'backend'):
        before = snapshot()
        remote(f"from pathlib import Path; import shutil; [shutil.copy2(Path('{APP}')/n, Path('{APP}')/(n+'.e2e-backend-before')) for n in ('compose-candidate.yaml','release-manifest.json')]")
        process = ps(receive(crash, scope='backend'), crash['archive'])
        record('backend-failure-manual-recovery', process, before, 'migration state UNKNOWN; MANUAL RECOVERY required')
        after = results['backend-failure-manual-recovery']['after']
        assert after['manifest']['releases']['current']['status'] == 'pending'
        assert after['manifest']['releases']['current']['candidate_commit'] == crash['revision']
        # Explicit test-operator cleanup, not automatic backend rollback.
        remote(f'''
from pathlib import Path
import shutil,subprocess
for n in ('compose-candidate.yaml','release-manifest.json'):
 shutil.copy2(Path('{APP}')/(n+'.e2e-backend-before'),Path('{APP}')/n)
subprocess.run(['docker','compose','--project-directory','{APP}','-f','{APP}/docker-compose.prod.yml','-f','{APP}/compose-candidate.yaml','up','-d','--no-deps','new-api'],check=True,capture_output=True)
''')
        current = before['manifest']['releases']['current']
        process = ps('python3 -I /opt/clearapi/bin/runtime-identity-refresh.py --reconcile --container app-new-api-1 '
                     f'--image-ref {current["image_ref"]} --expected-revision {current["candidate_commit"]} --directory /opt/clearapi-test/runtime/new-api')
        assert process.returncode == 0, process.stderr
        (out / 'backend-explicit-test-operator-recovery.log').write_text(process.stdout)
    save()
