#!/usr/bin/env python3
"""Incident-bound recovery of the exact Gateway; retain FAILED and isolated edge."""
import base64
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
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
          'controlled-traffic-hold.py', 'pg03-legacy-transition.py', 'pg03-schema-rehearsal.py',
          'runtime-identity-refresh.py', 'runtime-boot-recovery.py')


def require(value, message):
    if not value:
        raise CONTROL.ReleaseBlocked(message)


def authority(data, get, download, executing=False):
    """Fresh owner authority is separate from the failed deployment's authority."""
    require(set(data) == {'run', 'commit', 'rc_run', 'rehearsal_run', 'mode'}, 'unexpected recovery input')
    require(data['mode'] in ('rehearse', 'recover', 'restore'), 'invalid recovery mode')
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
    if data['mode'] in ('recover', 'restore'):
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
        require(data['mode'] in ('recover', 'restore'), 'rehearsal cannot mutate production')
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

    def snapshot(self, allow_stopped=False, restore=False, ingress_open=False):
        require(not ingress_open or restore, 'open ingress is only checked by restoration')
        if restore:
            require(not allow_stopped, 'restoration requires a running Gateway')
            LIFECYCLE.read_file(self.receipt)
        else:
            require(not self.receipt.exists() and not self.receipt.is_symlink(), 'recovery already recorded; do not replay')
        for name, expected in FROZEN.items():
            require(hashlib.sha256(LIFECYCLE.read_file(self.app / name, private=False)).hexdigest() == expected,
                    'failed incident evidence changed')
        current = LIFECYCLE.read_json(self.app / 'release-manifest.json', private=False)['releases']['current']
        require(current['status'] == 'failed' and current['deployment_attempt']['status'] == 'failed'
                and current['candidate_commit'] == COMMIT and current['image_digest'] == DIGEST, 'not the failed incident')
        hold = TRAFFIC.TrafficHold(self.app, self.command)
        if not ingress_open:
            hold.verify(hold.binding(FAILED_RUN, DIGEST, COMMIT))
        self.gateway(allow_stopped)
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

    def gateway(self, allow_stopped=False):
        gateway = json.loads(self.command(['docker', 'inspect', 'clearapi-new-api-1']))[0]
        require(gateway['Id'] == GATEWAY and gateway['Image'] == DIGEST
                and gateway['Config']['Image'] == 'ghcr.io/a3225865526-wq/clearapi@' + DIGEST
                and not gateway['State'].get('Restarting') and gateway['RestartCount'] == 0, 'Gateway changed')
        state = gateway['State']
        if not state['Running']:
            require(allow_stopped and state['Status'] == 'exited' and state['ExitCode'] == 127
                    and not state.get('OOMKilled')
                    and 'open /run/clearapi: no such file or directory' in state.get('Error', ''),
                    'Gateway is not the diagnosed missing-runtime-directory incident')
        mounts = [m for m in gateway['Mounts'] if m.get('Destination') == '/run/clearapi']
        require(len(mounts) == 1 and mounts[0].get('Type') == 'bind'
                and mounts[0].get('Source') == '/run/clearapi' and mounts[0].get('RW') is False,
                'runtime identity mount changed')
        return gateway

    def complete(self, authorization):
        attempt = self.app / ('failed-recovery-start-' + FAILED_RUN + '.json')
        require(not attempt.exists() and not attempt.is_symlink(), 'start already attempted; operator review required')
        before = self.snapshot(allow_stopped=True)
        gateway = self.gateway(allow_stopped=True)
        started = False
        try:
            if not gateway['State']['Running']:
                # Persist authority before any mutation. A failed/interrupted attempt
                # is never automatically retried, even if no final receipt exists.
                LIFECYCLE.publish(attempt, dict(schema='clearapi.failed-recovery-start/v1',
                    authorization=authorization, incident=before, original_state='exited_127_missing_runtime',
                    recorded_at=datetime.now(timezone.utc).isoformat(), public_traffic='blocked'))
                self.command(['/usr/bin/python3', '-I', '/opt/clearapi/bin/runtime-boot-recovery.py',
                              'check-installed'])
                self.command(['/opt/clearapi/bin/runtime-identity-refresh.py', '--prepare-only',
                              '--directory', '/run/clearapi'])
                started = True  # A failed docker start may still have changed runtime state.
                self.command(['docker', 'start', GATEWAY])
                self.command(['/opt/clearapi/bin/runtime-identity-refresh.py', '--reconcile',
                              '--container', GATEWAY, '--image-ref',
                              'ghcr.io/a3225865526-wq/clearapi@' + DIGEST,
                              '--expected-revision', COMMIT, '--directory', '/run/clearapi'])
                self.command(['curl', '--noproxy', '*', '--fail', '--silent', '--output', '/dev/null',
                              '--retry', '30', '--retry-delay', '1', '--retry-connrefused', '--retry-max-time', '90',
                              '--max-time', '3', 'http://127.0.0.1:3000/api/status'])
            self.command(['/opt/clearapi/bin/production-deploy-v2.sh', '--verify-failed-controlled', '--pool-mode', 'pg03',
                          '--workflow-run', FAILED_RUN, '--digest', DIGEST, '--commit', COMMIT])
            require(self.snapshot() == before, 'incident changed during postflight')
        except Exception:
            if started:
                self.command(['docker', 'stop', '--time', '150', GATEWAY])
            raise
        receipt = {'schema': 'clearapi.failed-controlled-recovery/v1', 'status': 'postflight_recovered_ingress_isolated',
                   'authorization': authorization, 'incident': before, 'recorded_at': datetime.now(timezone.utc).isoformat(),
                   'original_deployment_status': 'failed', 'public_traffic': 'blocked',
                   'provider_acceptance': 'deferred_to_owner_after_deployment', 'circuit_cleared': False,
                   'images_replaced': False, 'database_restored': False, 'same_gateway_started': started}
        try:
            LIFECYCLE.publish(self.receipt, receipt)
        except Exception:
            if started:
                self.command(['docker', 'stop', '--time', '150', GATEWAY])
            raise
        return receipt


class WebsiteRestoration:
    """One-use opening of the original edge after the exact isolated recovery."""
    CADDY_ID = '54c8550bbaca837ae906dfeb28a51ac54223da0405d908940caeb1d1920c00bf'
    CADDY_IMAGE = 'sha256:f2a1290d0463aad60660d4ec134943f183ee2a5f6c3eb7bf32dd984f2f020772'
    CONFIG = Path('/opt/clearapi/edge/Caddyfile')
    CONFIG_SHA = '6b1246669fbab3287a5c9e99f041cbcff0c44ebbcedb9c0a751310f7b1b05969'

    def __init__(self, recovery=None, get=None):
        self.recovery = recovery or Recovery()
        self.command = self.recovery.command
        self.get = get
        self.attempt = self.recovery.app / ('website-restoration-attempt-' + FAILED_RUN + '.json')
        self.result = self.recovery.app / ('website-restoration-result-' + FAILED_RUN + '.json')
        self.failure_result = self.recovery.app / ('website-restoration-failure-' + FAILED_RUN + '.json')

    def edge(self, running):
        # Do not retrieve the container environment or raw Caddy configuration.
        projection = ('{"Id":{{json .Id}},"Image":{{json .Image}},'
                      '"Ref":{{json .Config.Image}},"Entrypoint":{{json .Config.Entrypoint}},'
                      '"Cmd":{{json .Config.Cmd}},"Running":{{json .State.Running}},'
                      '"Paused":{{json .State.Paused}},"Restarting":{{json .State.Restarting}},'
                      '"RestartPolicy":{{json .HostConfig.RestartPolicy}},'
                      '"NetworkMode":{{json .HostConfig.NetworkMode}},"Mounts":{{json .Mounts}}}')
        edge = json.loads(self.command(['docker', 'inspect', '--format', projection, 'clearapi-edge-caddy']))
        require(edge['Id'] == self.CADDY_ID and edge['Image'] == self.CADDY_IMAGE
                and edge['Ref'] == 'caddy@' + self.CADDY_IMAGE and edge['Entrypoint'] is None
                and edge['Cmd'] == ['caddy', 'run', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile']
                and edge['Running'] is running and not edge['Paused'] and not edge['Restarting']
                and edge['NetworkMode'] == 'host'
                and edge['RestartPolicy'] == {'Name': 'unless-stopped' if running else 'no', 'MaximumRetryCount': 0},
                'original Caddy identity, process or network changed')
        mounts = [m for m in edge['Mounts'] if m.get('Destination') == '/etc/caddy/Caddyfile']
        require(len(mounts) == 1 and mounts[0].get('Type') == 'bind'
                and mounts[0].get('Source') == str(self.CONFIG) and mounts[0].get('RW') is False,
                'Caddy configuration mount changed')
        require(hashlib.sha256(LIFECYCLE.read_file(self.CONFIG, private=False)).hexdigest() == self.CONFIG_SHA,
                'frozen Caddy configuration changed')
        state = LIFECYCLE.read_json(self.recovery.app / 'controlled-launch-state.json')
        require(state['edge']['id'] == self.CADDY_ID and state['edge']['image'] == self.CADDY_IMAGE,
                'Caddy differs from original isolation journal')
        return {'container_id': edge['Id'], 'image_id': edge['Image'], 'config_sha256': self.CONFIG_SHA}

    def public_health(self, run):
        pages = {}
        self.public_observations = {}
        deadline = time.monotonic() + 30
        for path in ('/api/status', '/', '/login', '/dashboard/overview'):
            while True:
                try:
                    response = self.command(['curl', '-q', '--noproxy', '*', '--silent', '--show-error',
                        '--proto', '=https', '--max-time', str(min(5, max(0.1, deadline-time.monotonic()))),
                        '--max-filesize', '262144', '--header', 'Cache-Control: no-store',
                        '--header', 'X-ClearAPI-Probe: website-health-v1', '--dump-header', '-',
                        '--write-out', '\nCLEARAPI_HTTP:%{http_code}',
                        'https://www.clear999.com' + path + '?clearapi_restore=' + str(run)])
                    break
                except (OSError, CONTROL.ReleaseBlocked):
                    require(time.monotonic() < deadline, 'public HTTPS startup deadline exceeded')
                    time.sleep(min(1, max(0, deadline - time.monotonic())))
            raw, separator, status = response.rpartition('\nCLEARAPI_HTTP:')
            require(separator and re.fullmatch(r'\d{3}', status), 'invalid curl response metadata')
            head, separator, body = raw.replace('\r\n', '\n').partition('\n\n')
            require(separator, 'missing HTTPS response headers')
            headers = dict(line.split(':', 1) for line in head.splitlines() if ':' in line)
            headers = {k.lower(): v.strip() for k, v in headers.items()}
            self.public_observations[path] = dict(status=int(status), cf_ray=headers.get('cf-ray'),
                date=headers.get('date'), body_sha256=hashlib.sha256(body.encode()).hexdigest(),
                error_codes=re.findall(r'error code:\s*(\d+)', body, re.I))
            require(status == '200' and headers.get('cf-ray'),
                    'public HTTPS unavailable: status=' + status + ' Ray=' + headers.get('cf-ray', 'absent'))
            require(len(body.encode()) <= 262144, 'oversized public response')
            if path == '/api/status':
                require(json.loads(body, object_pairs_hook=CONTROL.unique).get('success') is True,
                        'public status is not healthy')
            else:
                require('text/html' in headers.get('content-type', '').lower()
                        and '<html' in body.lower() and '<script' in body.lower()
                        and any(marker in body for marker in ('id="root"', "id='root'", 'id=root')),
                        'application page unavailable')
            pages[path] = 'status_https_200_via_cloudflare' if path == '/api/status' else 'application_shell_https_200_via_cloudflare'
        return {'pages': pages, 'edge_responses': self.public_observations,
                'client': 'native curl; X-ClearAPI-Probe: website-health-v1',
                'normal_browser': 'not_tested', 'user_login': 'not_tested', 'admin_session': 'not_tested',
                'provider_acceptance': 'deferred_to_owner_after_deployment'}

    def restore(self, authorization):
        require(not any(p.exists() or p.is_symlink() for p in (self.attempt, self.result, self.failure_result)),
                'website opening already attempted; operator review required')
        raw = LIFECYCLE.read_file(self.recovery.receipt)
        receipt = json.loads(raw, object_pairs_hook=CONTROL.unique)
        prior = receipt.get('authorization', {})
        require(receipt.get('schema') == 'clearapi.failed-controlled-recovery/v1'
                and receipt.get('status') == 'postflight_recovered_ingress_isolated'
                and receipt.get('original_deployment_status') == 'failed' and receipt.get('public_traffic') == 'blocked'
                and receipt.get('circuit_cleared') is False and receipt.get('images_replaced') is False
                and receipt.get('database_restored') is False
                and all(prior.get(k) == authorization.get(k) for k in
                        ('assets_commit', 'assets_rc_run', 'rehearsal_run', 'failed_run'))
                and prior.get('failed_run') == FAILED_RUN
                and re.fullmatch(r'[1-9][0-9]*', str(prior.get('recovery_run', '')))
                and prior['recovery_run'] != authorization.get('recovery_run'),
                'successful same-assets isolated recovery required')
        require(self.get is not None, 'fresh recovery workflow verification required')
        run = self.get('actions/runs/' + str(prior['recovery_run']))
        require(run['path'] == WORKFLOW and run['head_sha'] == authorization['assets_commit']
                and run['head_branch'] == 'main' and run['event'] == 'workflow_dispatch'
                and run['run_attempt'] == 1 and run['status'] == 'completed' and run['conclusion'] == 'success'
                and run['repository']['full_name'] == CONTROL.REPO
                and run['head_repository']['full_name'] == CONTROL.REPO, 'recovery workflow not successful')
        CONTROL.autonomous_authority({'run': prior['recovery_run']}, self.get)
        jobs = self.get('actions/runs/' + str(prior['recovery_run']) + '/attempts/1/jobs?per_page=100')['jobs']
        jobs = [j for j in jobs if j['name'] == 'Recover incident or restore verified website']
        require(len(jobs) == 1 and jobs[0]['status'] == 'completed' and jobs[0]['conclusion'] == 'success',
                'successful recovery phase job required')
        before = self.recovery.snapshot(restore=True)
        require(receipt.get('incident') == before, 'recovery receipt does not identify current incident')
        self.command(['/usr/bin/python3', '-I', '/opt/clearapi/bin/runtime-boot-recovery.py', 'check-installed'])
        self.command(['/opt/clearapi/bin/production-deploy-v2.sh', '--verify-failed-controlled', '--pool-mode', 'pg03',
                      '--workflow-run', FAILED_RUN, '--digest', DIGEST, '--commit', COMMIT])
        require(self.recovery.snapshot(restore=True) == before, 'incident changed before opening')
        expected_edge = self.edge(False)
        base = dict(schema='clearapi.website-restoration/v1', authorization=authorization,
                    recovery_receipt_sha256=hashlib.sha256(raw).hexdigest(), incident=before,
                    edge=expected_edge, original_deployment_status='failed', circuit_cleared=False,
                    provider_acceptance='deferred_to_owner_after_deployment')
        def verify():
            require(self.recovery.snapshot(restore=True, ingress_open=True) == before, 'incident changed after opening')
            require(LIFECYCLE.read_file(self.recovery.receipt) == raw, 'isolated recovery record changed')
        return self.open_edge(base, expected_edge, verify, authorization['recovery_run'])

    def restore_adopted(self, authorization, adapter):
        """Root-authorized restoration of the separately observed current instance.

        Does not manufacture the legacy GitHub recovery receipt or clear FAILED.
        """
        authorization = dict(authorization)
        reviewed = authorization.pop('reviewed_failure_sha256', None)
        require(set(authorization) == {'source_commit', 'scope'}
                and authorization['scope'] == 'existing-gateway-original-caddy-v1',
                'explicit existing-instance restoration scope required')
        if reviewed is not None:
            # A new owner-reviewed attempt preserves the prior failure and latch.
            raw_failure = LIFECYCLE.read_file(self.failure_result)
            failure = json.loads(raw_failure, object_pairs_hook=CONTROL.unique)
            require(hashlib.sha256(raw_failure).hexdigest() == reviewed
                    and failure.get('schema') == 'clearapi.website-restoration/v1'
                    and failure.get('method') == 'existing-running-adoption-v1'
                    and failure.get('status') == 'website_restoration_failed'
                    and failure.get('containment_verified') is True
                    and failure.get('public_traffic') == 'blocked'
                    and failure.get('authorization') == authorization,
                    'exact contained failure must be reviewed before another opening')
            LIFECYCLE.read_file(self.attempt)
            self.attempt = self.attempt.with_name(self.attempt.stem + '-reviewed-' + reviewed + '.json')
            self.failure_result = self.failure_result.with_name(self.failure_result.stem + '-reviewed-' + reviewed + '.json')
        require(not any(p.exists() or p.is_symlink() for p in (self.attempt,self.result,self.failure_result)),
                'website opening already attempted; operator review required')
        app=self.recovery.app
        state=adapter.STATE.load(app)
        require(state['adoption']['source_commit'] == authorization['source_commit']
                and state['releases']['current']['status'] == 'adopted',
                'restoration must identify the initial running-instance adoption')
        before=adapter.adoption_snapshot(app)
        require(all(state['adoption'].get(k)==v for k,v in before.items()), 'adopted instance changed')
        if reviewed is not None:
            require(failure.get('incident') == before, 'reviewed failure identifies another instance')
        expected_edge=self.edge(False)
        hold=TRAFFIC.TrafficHold(app,self.command)
        hold.verify(hold.binding(FAILED_RUN,DIGEST,COMMIT))
        self.command(['/usr/bin/python3','-I','/opt/clearapi/bin/runtime-boot-recovery.py','check-installed'])
        self.command(['/usr/bin/python3','-I','/opt/clearapi/bin/runtime-boot-recovery.py','check'])
        config={'app_dir':str(app),'service':'new-api','backup_service':'clearapi-backup.service',
                'backup_dir':'/opt/clearapi/backups','compose_file':'docker-compose.prod.yml'}
        backup_id=adapter.backup(config)
        raw=adapter.STATE.read_file(app/adapter.STATE.NAME,private=True)
        require(adapter.adoption_snapshot(app)==before, 'instance changed during backup')
        base=dict(schema='clearapi.website-restoration/v1',authorization=authorization,
                  method='existing-running-adoption-v1',adoption_sha256=hashlib.sha256(raw).hexdigest(),
                  adoption_identity_sha256=hashlib.sha256(json.dumps(state['adoption'],sort_keys=True).encode()).hexdigest(),
                  incident=before,backup_id=backup_id,edge=expected_edge,
                  original_deployment_status='failed',circuit_cleared=False,
                  gateway_replaced=False,database_restored=False)
        if reviewed is not None:
            base['reviewed_failure_sha256'] = reviewed
        def verify():
            adapter.STATE.load(app)
            require(adapter.adoption_snapshot(app)==before, 'instance changed after opening')
            require(adapter.STATE.read_file(app/adapter.STATE.NAME,private=True)==raw, 'adoption changed')
            origin=self.command(['curl','--noproxy','*','--fail','--silent','--max-time','5',
                '--resolve','www.clear999.com:443:127.0.0.1','https://www.clear999.com/api/status'])
            require(json.loads(origin).get('success') is True, 'origin HTTPS Gateway forwarding failed')
            redirect=self.command(['curl','--noproxy','*','--silent','--max-time','5',
                '--resolve','www.clear999.com:80:127.0.0.1','-o','/dev/null','-w','%{http_code} %{redirect_url}',
                'http://www.clear999.com/'])
            require(redirect=='308 https://www.clear999.com/', 'origin HTTP redirect failed')
            return {'https_gateway_forwarding': 'PASS', 'http_redirect': '308 https://www.clear999.com/'}
        return self.open_edge(base,expected_edge,verify,authorization['source_commit'])

    def open_edge(self, base, expected_edge, verify, health_nonce):
        LIFECYCLE.publish(self.attempt, dict(base, stage='opening', recorded_at=datetime.now(timezone.utc).isoformat()))
        origin = None
        try:
            self.command(['docker', 'update', '--restart=unless-stopped', self.CADDY_ID])
            self.command(['docker', 'start', self.CADDY_ID])
            require(self.edge(True) == expected_edge, 'Caddy changed during opening')
            origin = verify()
            health = self.public_health(health_nonce)
            verify()
            require(self.edge(True) == expected_edge, 'Caddy changed during verification')
            result = dict(base, status='website_restored', public_traffic='open', origin=origin, health=health,
                          recorded_at=datetime.now(timezone.utc).isoformat())
            LIFECYCLE.publish(self.result, result)
            return result
        except BaseException as error:
            # Both containment commands must be attempted even if one fails.
            contained = True
            for args in (['docker', 'stop', '--time', '150', self.CADDY_ID],
                         ['docker', 'update', '--restart=no', self.CADDY_ID]):
                try:
                    self.command(args)
                except Exception:
                    contained = False
            try:
                hold = TRAFFIC.TrafficHold(self.recovery.app, self.command)
                hold.verify(hold.binding(FAILED_RUN, DIGEST, COMMIT))
                self.edge(False)
            except Exception:
                contained = False
            if not self.failure_result.exists() and not self.failure_result.is_symlink():
                LIFECYCLE.publish(self.failure_result, dict(base, status='website_restoration_failed',
                    public_traffic='blocked' if contained else 'UNKNOWN', containment_verified=contained,
                    error=str(error), origin=origin, edge_responses=getattr(self, 'public_observations', {}),
                    recorded_at=datetime.now(timezone.utc).isoformat()))
            raise


def restoration_interrupted(signum, frame):
    raise InterruptedError('website restoration interrupted')


def main():
    if sys.argv[1:] == ['restore-adopted']:
        require(os.geteuid()==0, 'root restoration required')
        for signum in (signal.SIGTERM,signal.SIGHUP,signal.SIGINT):
            signal.signal(signum,restoration_interrupted)
        data=json.loads(sys.stdin.buffer.read(4097),object_pairs_hook=CONTROL.unique)
        path=Path(__file__).with_name('clearapi-local-deploy.py')
        if not path.exists():path=Path(__file__).with_name('local-deploy-vps.py')
        spec=importlib.util.spec_from_file_location('local_adapter',path)
        adapter=importlib.util.module_from_spec(spec);spec.loader.exec_module(adapter)
        with adapter.deploy_lock():
            result=WebsiteRestoration().restore_adopted(data,adapter)
        print(json.dumps(result,sort_keys=True))
        return
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
    if data['mode'] == 'restore':
        for signum in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
            signal.signal(signum, restoration_interrupted)
    with CONTROL.deployment_lock():
        if data['mode'] == 'restore':
            WebsiteRestoration(get=get).restore(authorization)
        else:
            Recovery().complete(authorization)
    if data['mode'] == 'restore':
        print('Website HTTPS and application pages verified; real login and Provider acceptance remain untested; FAILED and circuit unchanged')
    else:
        print('FAILED evidence retained; controlled postflight recovered; public ingress remains blocked; circuit unchanged')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('FAILED RECOVERY BLOCKED: retain original evidence and isolated edge; no automatic retry')
