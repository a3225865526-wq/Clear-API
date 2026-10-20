#!/usr/bin/env python3
"""Incident-bound postflight recovery; never replace services or edit FAILED evidence."""
import base64
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import zipfile

sys.dont_write_bytecode = True


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + '.py'))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


CONTROL = load('pg03-release-control')
LIFECYCLE = load('pg03-legacy-transition')
TRAFFIC = load('controlled-traffic-hold')
FAILED_RUN = '39347012653'
COMMIT = 'c50a53c1b89e121bcade7dac41d3f11e53a9832a'
DIGEST = 'sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02'
GATEWAY = 'f8f963a7df5f5114a02a7660e18121411c6aa5f284924d60b514a638dc10d24f'
EXECUTOR = '1081a1b576ad95e521c1d390b3cfe4d4f9c0eba880779a0391a7e9aef673e57f'
FROZEN = {
    'release-manifest.json': '6c308146551dfab395e7202f775b23121ec460757e6b56b7f107decb5cae63d3',
    'pg03-target-manifest.json': '521e60ce3edbe4cd073decaedd70134daf3d22d8039d0fed4de427b2318cb39e',
    'controlled-launch-state.json': '57a7152a312eba02e2c81b2973981894640c2088782827e6d2d112b2235dff4d',
    'compose-candidate.yaml': '077407c6ebe7d69f501f6c758fc449db4084061e6e73319f5d7e44b6cf8a2b6e',
    'compose-pg03.yaml': '5b237a189639a044d4a883fc258140ac67c462402a855b691593c9ada9f661c0',
}
WORKFLOW = '.github/workflows/failed-production-recovery.yml'
ASSETS = ('production-deploy-v2.sh', 'pg03-release-control.py', 'pg03-failed-recovery.py',
          'controlled-traffic-hold.py', 'pg03-legacy-transition.py', 'pg03-schema-rehearsal.py')


def require(value, message):
    if not value:
        raise CONTROL.ReleaseBlocked(message)


def authority(data, get, download, executing=False):
    """Fresh owner authority is separate from the failed deployment's authority."""
    require(set(data) == {'run', 'commit', 'rc_run', 'rehearsal_run', 'mode'}, 'unexpected recovery input')
    require(data['mode'] in ('rehearse', 'recover'), 'invalid recovery mode')
    require(re.fullmatch(r'[a-f0-9]{40}', data['commit']), 'invalid assets commit')
    require(all(re.fullmatch(r'[1-9][0-9]*', str(data[k])) for k in ('run', 'rc_run')), 'invalid recovery run')
    run = get('actions/runs/' + str(data['run']))
    require(run['path'] == WORKFLOW and run['head_sha'] == data['commit']
            and run['head_branch'] == 'main' and run['event'] == 'workflow_dispatch'
            and run['run_attempt'] == 1 and run['status'] == 'in_progress'
            and run['repository']['full_name'] == CONTROL.REPO
            and run['head_repository']['full_name'] == CONTROL.REPO
            and get('git/ref/heads/main')['object']['sha'] == data['commit'], 'untrusted recovery workflow')
    CONTROL.autonomous_authority(data, get)
    failed = get('actions/runs/' + FAILED_RUN)
    require(failed['path'] == '.github/workflows/deploy-production.yml'
            and failed['head_sha'] == COMMIT and failed['head_branch'] == 'main'
            and failed['repository']['full_name'] == CONTROL.REPO
            and failed['event'] == 'workflow_dispatch' and failed['run_attempt'] == 1
            and failed['status'] == 'completed' and failed['conclusion'] == 'failure', 'failed incident changed')
    rc_data = dict(commit=data['commit'], digest=DIGEST, rc_run=data['rc_run'])
    rc = CONTROL.approved_rc(rc_data, get)
    artifacts = get(f"actions/runs/{rc['id']}/artifacts?per_page=100")
    matches = [a for a in artifacts['artifacts'] if a['name'] == f"pg03-gateway-release-{rc['id']}-{rc['run_attempt']}" and not a['expired']]
    require(len(matches) == 1, 'missing recovery assets RC')
    raw = download(matches[0]['id'])
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        candidate = json.loads(archive.read('gateway-image-release.json'), object_pairs_hook=CONTROL.unique)
    rc_data['digest'] = candidate['image_digest']
    manifest, _ = CONTROL.release_manifest(rc_data, rc, artifacts, lambda ident: raw if ident == matches[0]['id'] else download(ident))
    require(manifest['v2_wrapper_sha256'] == CONTROL.V2_SHA, 'recovery assets RC wrapper mismatch')
    if data['mode'] == 'recover':
        require(re.fullmatch(r'[1-9][0-9]*', str(data['rehearsal_run'])), 'recovery rehearsal required')
        rehearsal = get('actions/runs/' + str(data['rehearsal_run']))
        require(rehearsal['path'] == WORKFLOW and rehearsal['head_sha'] == data['commit']
                and rehearsal['head_branch'] == 'main' and rehearsal['event'] == 'workflow_dispatch'
                and rehearsal['repository']['full_name'] == CONTROL.REPO and rehearsal['run_attempt'] == 1
                and rehearsal['status'] == 'completed' and rehearsal['conclusion'] == 'success', 'recovery rehearsal not successful')
        proofs = get(f"actions/runs/{rehearsal['id']}/artifacts?per_page=100")['artifacts']
        proofs = [a for a in proofs if a['name'] == 'failed-recovery-rehearsal' and not a['expired']]
        require(len(proofs) == 1, 'missing recovery rehearsal proof')
        with zipfile.ZipFile(io.BytesIO(download(proofs[0]['id']))) as archive:
            require(archive.namelist() == ['recovery-rehearsal.json'], 'unexpected recovery proof inventory')
            proof = json.loads(archive.read('recovery-rehearsal.json'), object_pairs_hook=CONTROL.unique)
        require(proof == {'schema': 'clearapi.failed-recovery-rehearsal/v1', 'source': data['commit'],
                          'rc_run': str(data['rc_run']), 'failed_run': FAILED_RUN, 'tests': 'passed',
                          'v2_sha256': CONTROL.V2_SHA}, 'recovery rehearsal binding mismatch')
    else:
        require(not data['rehearsal_run'], 'unexpected rehearsal reference')
    if executing:
        require(data['mode'] == 'recover', 'rehearsal cannot mutate production')
        jobs = get(f"actions/runs/{data['run']}/attempts/1/jobs?per_page=100")['jobs']
        jobs = [j for j in jobs if j['name'] == 'Authenticate failed recovery']
        require(len(jobs) == 1 and jobs[0]['conclusion'] == 'success', 'recovery authorization job missing')
    return {'assets_commit': data['commit'], 'assets_rc_run': str(data['rc_run']),
            'recovery_run': str(data['run']), 'rehearsal_run': str(data['rehearsal_run']), 'failed_run': FAILED_RUN}


class Recovery:
    def __init__(self, app=Path('/opt/clearapi/app'), command=None):
        self.app = app
        self.command = command or self.production_command
        self.receipt = app / ('failed-recovery-' + FAILED_RUN + '.json')

    @staticmethod
    def production_command(args):
        result = subprocess.run(args, capture_output=True, text=True, timeout=180)
        require(result.returncode == 0, 'recovery postflight command failed; retain isolation and FAILED evidence')
        return result.stdout.strip()

    def snapshot(self):
        require(not self.receipt.exists() and not self.receipt.is_symlink(), 'recovery already recorded; do not replay')
        for name, expected in FROZEN.items():
            require(hashlib.sha256(LIFECYCLE.read_file(self.app / name, private=False)).hexdigest() == expected,
                    'failed incident evidence changed')
        current = LIFECYCLE.read_json(self.app / 'release-manifest.json', private=False)['releases']['current']
        require(current['status'] == 'failed' and current['deployment_attempt']['status'] == 'failed'
                and current['candidate_commit'] == COMMIT and current['image_digest'] == DIGEST, 'not the failed incident')
        hold = TRAFFIC.TrafficHold(self.app, self.command)
        hold.verify(hold.binding(FAILED_RUN, DIGEST, COMMIT))
        gateway = json.loads(self.command(['docker', 'inspect', 'clearapi-new-api-1']))[0]
        require(gateway['Id'] == GATEWAY and gateway['Image'] == DIGEST and gateway['State']['Running']
                and not gateway['State'].get('Restarting') and gateway['RestartCount'] == 0, 'Gateway changed')
        executor = CONTROL.executor_identity(lambda *args: self.command(['docker', *args]))
        require(executor['container_id'] == EXECUTOR, 'Executor changed')
        schema = load('pg03-schema-rehearsal')
        dump = self.command(['docker', 'exec', 'clearapi-postgres-1', 'pg_dump', '-U', 'clearapi_app', '-d',
                             'clearapi', '--schema-only', '--no-owner', '--no-privileges'])
        require(schema.schema_digest(dump) == '3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035',
                'failed incident database schema changed')
        sql = """BEGIN READ ONLY;
SELECT json_build_array(
 (SELECT count(*) FROM billing_reservations WHERE state='needs_review'),
 (SELECT count(*) FROM billing_reservations r WHERE state='settled' AND EXISTS
  (SELECT 1 FROM billing_review_markers m WHERE m.reservation_id=r.id)),
 (SELECT count(*) FROM finance_sync_events WHERE status='needs_review'),
 (SELECT count(*) FROM billing_completion_outboxes WHERE state='pending'));
ROLLBACK;"""
        counts = self.command(['docker', 'exec', 'clearapi-postgres-1', 'psql', '-U', 'clearapi_app', '-d', 'clearapi',
                               '-X', '-Atq', '-v', 'ON_ERROR_STOP=1', '-c', sql])
        require(json.loads(counts) == [2, 3, 10, 0], 'protected financial baseline changed')
        backup = Path('/opt/clearapi/backups/manifests/20261020T002138Z.json')
        raw = LIFECYCLE.read_file(backup, private=False)
        require(hashlib.sha256(raw).hexdigest() == 'a9b9c32cd168c5a68740e20f1c949cff41da665b14d7aeb5790404dd0d795166', 'backup identity changed')
        blob = Path('/opt/clearapi/backups/encrypted/20261020T002138Z/clearapi-backup-20261020T002138Z.tar.gz.gpg')
        # Existing encrypted backup is verified, never restored or decrypted here.
        require(hashlib.sha256(LIFECYCLE.read_file(blob, maximum=12323351)).hexdigest()
                == '0c8757c8d204cddd7cc5b3b9a7835823e376093b9ee344f5a8bd8c219d3b3b31', 'encrypted backup changed')
        return {'failed_manifest_sha256': FROZEN['release-manifest.json'], 'gateway': GATEWAY, 'executor': EXECUTOR,
                'target_sha256': FROZEN['pg03-target-manifest.json']}

    def complete(self, authorization):
        before = self.snapshot()
        self.command(['/opt/clearapi/bin/production-deploy-v2.sh', '--verify-failed-controlled', '--pool-mode', 'pg03',
                      '--workflow-run', FAILED_RUN, '--digest', DIGEST, '--commit', COMMIT])
        require(self.snapshot() == before, 'incident changed during postflight')
        receipt = {'schema': 'clearapi.failed-controlled-recovery/v1', 'status': 'postflight_recovered_ingress_isolated',
                   'authorization': authorization, 'incident': before, 'recorded_at': datetime.now(timezone.utc).isoformat(),
                   'original_deployment_status': 'failed', 'public_traffic': 'blocked',
                   'provider_acceptance': 'deferred_to_owner_after_deployment', 'circuit_cleared': False,
                   'images_replaced': False, 'database_restored': False}
        LIFECYCLE.publish(self.receipt, receipt)
        return receipt


def main():
    require(sys.argv[1:] in (['authorize'], ['recover']), 'invalid recovery entry point')
    data = json.loads(sys.stdin.buffer.read(32769), object_pairs_hook=CONTROL.unique)
    token = data.pop('token')
    get = lambda path: CONTROL.api(path, token)
    download = lambda ident: CONTROL.api(f'actions/artifacts/{ident}/zip', token, archive=True)
    executing = sys.argv[1] == 'recover'
    authorization = authority(data, get, download, executing=executing)
    if not executing:
        print(json.dumps(authorization))
        return
    require(os.geteuid() == 0, 'root recovery required')
    for name in ASSETS:
        blob = get('contents/scripts/' + name + '?ref=' + data['commit'])
        require(blob['encoding'] == 'base64' and base64.b64decode(blob['content']) ==
                LIFECYCLE.read_file(Path(__file__).with_name(name), private=False), 'installed recovery asset mismatch')
    with CONTROL.deployment_lock():
        Recovery().complete(authorization)
    print('FAILED evidence retained; controlled postflight recovered; public ingress remains blocked; circuit unchanged')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('FAILED RECOVERY BLOCKED: retain original evidence and isolated edge; no automatic retry')
