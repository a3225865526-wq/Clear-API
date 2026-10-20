#!/usr/bin/env python3
"""Real GNU Wget transport and incident recovery rejection/append-only cases."""
import copy
import hashlib
import http.server
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile


SCRIPTS = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('recovery', SCRIPTS / 'pg03-failed-recovery.py')
RECOVERY = importlib.util.module_from_spec(spec)
spec.loader.exec_module(RECOVERY)


class RealWgetProbeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.status = 200
        self.health_status = 200
        self.requests = []
        self.config_modes = []
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                owner.requests.append((self.path, self.headers.get('X-Management-Key')))
                if self.path == '/healthz':
                    self.send_response(owner.health_status)
                else:
                    owner.config_modes.extend(p.stat().st_mode & 0o777 for p in owner.root.glob('probe.*'))
                    self.send_response(owner.status)
                    if owner.status == 302:
                        self.send_header('Location', '/must-not-follow')
                self.end_headers()

            def log_message(self, *args):
                pass

        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)
        source = (SCRIPTS / 'production-deploy-v2.sh').read_text()
        self.shell = source.split('  # GNU Wget cannot', 1)[1].split("docker exec \"$cid\" /bin/sh -c '\n", 1)[1].split("\n  ' >/dev/null", 1)[0]
        self.shell = self.shell.replace('http://pool-executor:8317', self.url)
        self.shell = self.shell.replace('/tmp/clearapi-executor-probe.XXXXXX', str(self.root / 'probe.XXXXXX'))
        self.env = dict(os.environ, CLEARAPI_POOL_CLIPROXY_KEY='synthetic-probe-key-' + 'x' * 32)
        wget = shutil.which('wget')
        self.assertIsNotNone(wget, 'GNU Wget is required; never skip the transport regression')
        self.assertIn('GNU Wget', subprocess.check_output([wget, '--version'], text=True))
        self.wget = wget
        shim = self.root / 'wget'
        shim.write_text('#!/usr/bin/python3\nimport json,os,sys\n'
                        + 'with open(' + repr(str(self.root / 'argv.jsonl')) + ',"a") as f: f.write(json.dumps(sys.argv[1:])+"\\n")\n'
                        + 'os.execv(' + repr(wget) + ', ["wget", *sys.argv[1:]])\n')
        shim.chmod(0o700)
        self.env['PATH'] = str(self.root) + ':' + os.environ['PATH']

    def probe(self):
        result = subprocess.run(['sh', '-c', self.shell], env=self.env, capture_output=True, text=True, timeout=15)
        self.assertEqual(list(self.root.glob('probe.*')), [], 'private config must be removed on success and failure')
        self.assertNotIn(self.env['CLEARAPI_POOL_CLIPROXY_KEY'], result.stdout + result.stderr)
        argv = (self.root / 'argv.jsonl').read_text()
        self.assertNotIn(self.env['CLEARAPI_POOL_CLIPROXY_KEY'], argv, 'credential must never enter process argv')
        return result

    def test_real_authenticated_request_and_private_config_cleanup(self):
        self.assertEqual(self.probe().returncode, 0)
        self.assertEqual(self.requests, [('/healthz', None), ('/v0/management/auth-files', self.env['CLEARAPI_POOL_CLIPROXY_KEY'])])
        self.assertEqual(self.config_modes, [0o600])

    def test_authentication_failure_is_blocked_and_config_removed(self):
        self.status = 401
        self.assertNotEqual(self.probe().returncode, 0)
        self.assertEqual(self.config_modes, [0o600])

    def test_health_failure_never_sends_authentication(self):
        self.health_status = 503
        self.assertNotEqual(self.probe().returncode, 0)
        self.assertEqual(self.requests, [('/healthz', None)])

    def test_redirect_is_rejected_without_forwarding_credentials(self):
        self.status = 302
        self.assertNotEqual(self.probe().returncode, 0)
        self.assertNotIn('/must-not-follow', [p for p, _ in self.requests])

    def test_original_pipe_config_fails_before_network_request(self):
        result = subprocess.run([self.wget, '--config=/dev/stdin', '--no-proxy', '--tries=1', '--timeout=3',
                                 '-q', '-O', '/dev/null', self.url + '/healthz'],
                                input='header = X-Diagnostic: synthetic\n', capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(self.requests, [])


class RecoveryReceiptTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.app = Path(temp.name)
        self.manifest = self.app / 'release-manifest.json'
        self.original = b'{"status":"failed","attempt":"preserve-me"}\n'
        self.manifest.write_bytes(self.original)
        self.binding = {'failed_manifest_sha256': hashlib.sha256(self.original).hexdigest()}
        self.commands = []
        self.recovery = RECOVERY.Recovery(self.app, lambda args: self.commands.append(args) or '')

    def test_success_only_appends_receipt_and_retains_failed_manifest(self):
        with patch.object(self.recovery, 'snapshot', return_value=self.binding):
            receipt = self.recovery.complete({'recovery_run': '123'})
        self.assertEqual(self.manifest.read_bytes(), self.original)
        self.assertEqual(self.recovery.receipt.stat().st_mode & 0o777, 0o600)
        self.assertEqual(receipt['original_deployment_status'], 'failed')
        self.assertEqual(receipt['public_traffic'], 'blocked')
        self.assertFalse(receipt['circuit_cleared'])
        self.assertFalse(receipt['images_replaced'])
        self.assertFalse(receipt['database_restored'])
        self.assertEqual(len(self.commands), 1)
        self.assertIn('--verify-failed-controlled', self.commands[0])

    def test_failed_postflight_cannot_publish_success(self):
        self.recovery.command = lambda args: RECOVERY.require(False, 'actual postflight failure')
        with patch.object(self.recovery, 'snapshot', return_value=self.binding):
            with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'actual postflight'):
                self.recovery.complete({})
        self.assertFalse(self.recovery.receipt.exists())
        self.assertEqual(self.manifest.read_bytes(), self.original)

    def test_changed_incident_cannot_publish_success(self):
        with patch.object(self.recovery, 'snapshot', side_effect=[self.binding, {'changed': True}]):
            with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'changed during'):
                self.recovery.complete({})
        self.assertFalse(self.recovery.receipt.exists())

    def test_published_receipt_cannot_be_overwritten_or_replayed(self):
        self.recovery.receipt.write_text('original-recovery-record')
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'already recorded'):
            self.recovery.snapshot()
        self.assertEqual(self.recovery.receipt.read_text(), 'original-recovery-record')

    def test_changed_failed_manifest_is_rejected_before_any_command(self):
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'evidence changed'):
            self.recovery.snapshot()
        self.assertEqual(self.commands, [])
        self.assertFalse(self.recovery.receipt.exists())


class FailedManifestParserTest(unittest.TestCase):
    def test_failed_manifest_is_only_accepted_in_explicit_readonly_mode(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            overlay = root / 'overlay.yaml'
            overlay.write_text('services: {}\n')
            manifest = root / 'manifest.json'
            gateway = dict(image_ref='registry/gateway@' + RECOVERY.DIGEST, image_digest=RECOVERY.DIGEST,
                           image_id=RECOVERY.DIGEST, oci_revision=RECOVERY.COMMIT)
            executor = dict(image_ref=RECOVERY.CONTROL.EXECUTOR, image_digest=RECOVERY.CONTROL.EXECUTOR.split('@')[1],
                            image_id='sha256:' + 'e' * 64, oci_revision=RECOVERY.CONTROL.EXECUTOR_REVISION,
                            binary_sha256=RECOVERY.CONTROL.EXECUTOR_BINARY, executor_id='test-executor', process_epoch='f' * 32)
            current = dict(pool_mode='pg03', components=dict(gateway=gateway, executor=executor),
                           image_ref=gateway['image_ref'], image_digest=RECOVERY.DIGEST, new_api_image_id=RECOVERY.DIGEST,
                           candidate_commit=RECOVERY.COMMIT, deployment_attempt=dict(status='failed'))
            source = (SCRIPTS / 'production-deploy-v2.sh').read_text()
            function = 'pg03_load_pair() {' + source.split('pg03_load_pair() {', 1)[1].split('\npg03_load_environment()', 1)[0]
            env = dict(os.environ, PG03_COMPOSE_FILE=str(overlay), PG03_EXECUTOR_REF=RECOVERY.CONTROL.EXECUTOR,
                       PG03_EXECUTOR_REVISION=RECOVERY.CONTROL.EXECUTOR_REVISION, PG03_EXECUTOR_BINARY=RECOVERY.CONTROL.EXECUTOR_BINARY,
                       LEGACY_TRANSITION='0', CONTROLLED_LAUNCH='0', CLEARAPI_POOL_EXECUTOR_ID='test-executor',
                       CLEARAPI_PG02_PROCESS_EPOCH='f' * 32)
            for status, flag, passes in [('failed', '0', False), ('failed', '1', True), ('success', '1', False),
                                          ('success', '0', True), ('pending', '1', False)]:
                current['status'] = status
                manifest.write_text(json.dumps(dict(releases=dict(current=current))))
                result = subprocess.run(['bash', '-c', function + '\npg03_load_pair "$1" current', 'test-parser', str(manifest)],
                                        env=dict(env, FAILED_RECOVERY_VERIFY=flag), capture_output=True, text=True)
                with self.subTest(status=status, flag=flag):
                    self.assertEqual(result.returncode == 0, passes, result.stderr)


def archive(files):
    output = io.BytesIO()
    with zipfile.ZipFile(output, 'w') as zipped:
        for name, value in files.items():
            zipped.writestr(name, json.dumps(value))
    return output.getvalue()


class RecoveryAuthorityTest(unittest.TestCase):
    def setUp(self):
        self.source = 'a' * 40
        self.data = dict(run='123', commit=self.source, rc_run='456', rehearsal_run='', mode='rehearse')
        run = dict(path=RECOVERY.WORKFLOW, head_sha=self.source, head_branch='main', event='workflow_dispatch',
                   run_attempt=1, status='in_progress', repository={'full_name': RECOVERY.CONTROL.REPO},
                   head_repository={'full_name': RECOVERY.CONTROL.REPO})
        failed = dict(path='.github/workflows/deploy-production.yml', head_sha=RECOVERY.COMMIT,
                      head_branch='main', event='workflow_dispatch', run_attempt=1, status='completed',
                      conclusion='failure', repository={'full_name': RECOVERY.CONTROL.REPO})
        self.responses = {'actions/runs/123': run, 'git/ref/heads/main': {'object': {'sha': self.source}},
                          'actions/runs/' + RECOVERY.FAILED_RUN: failed,
                          'actions/runs/456/artifacts?per_page=100': {'artifacts': [{'id': 10, 'name': 'pg03-gateway-release-456-1', 'expired': False}]}}
        self.archives = {10: archive({'gateway-image-release.json': {'image_digest': RECOVERY.DIGEST}})}

    def authorize(self, executing=False):
        with patch.object(RECOVERY.CONTROL, 'autonomous_authority') as owner, \
             patch.object(RECOVERY.CONTROL, 'approved_rc', return_value={'id': 456, 'run_attempt': 1}) as rc, \
             patch.object(RECOVERY.CONTROL, 'release_manifest', return_value=({'v2_wrapper_sha256': RECOVERY.CONTROL.V2_SHA}, 'hash')):
            result = RECOVERY.authority(self.data, self.responses.__getitem__, self.archives.__getitem__, executing)
            owner.assert_called_once()
            rc.assert_called_once()
            return result

    def test_owner_and_fresh_rc_are_checked_for_rehearsal(self):
        self.assertEqual(self.authorize()['failed_run'], RECOVERY.FAILED_RUN)

    def test_fork_wrong_source_attempt_or_workflow_is_rejected(self):
        original = copy.deepcopy(self.responses['actions/runs/123'])
        for key, value in [('head_branch', 'other'), ('head_sha', 'b' * 40), ('run_attempt', 2),
                           ('path', '.github/workflows/ci.yml'), ('event', 'push'),
                           ('repository', {'full_name': 'attacker/repo'})]:
            self.responses['actions/runs/123'] = dict(original, **{key: value})
            with self.subTest(key=key), self.assertRaises(RECOVERY.CONTROL.ReleaseBlocked):
                self.authorize()

    def test_incident_success_or_retried_failure_is_rejected(self):
        incident = self.responses['actions/runs/' + RECOVERY.FAILED_RUN]
        for key, value in [('conclusion', 'success'), ('run_attempt', 2), ('head_sha', 'b' * 40)]:
            original = copy.deepcopy(incident)
            incident[key] = value
            with self.subTest(key=key), self.assertRaises(RECOVERY.CONTROL.ReleaseBlocked):
                self.authorize()
            incident.update(original)

    def test_recover_requires_a_same_source_successful_rehearsal(self):
        self.data.update(mode='recover')
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'rehearsal required'):
            self.authorize()
        self.data['rehearsal_run'] = '789'
        self.responses['actions/runs/789'] = dict(self.responses['actions/runs/123'], id=789, status='completed', conclusion='success', head_sha='b' * 40)
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'not successful'):
            self.authorize()

    def test_rehearsal_can_never_execute_production_write(self):
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'cannot mutate'):
            self.authorize(executing=True)

    def test_unknown_input_or_missing_rc_fails_closed(self):
        self.data['unsafe_path'] = '/tmp/arbitrary'
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'unexpected recovery input'):
            self.authorize()

    def test_exact_rehearsal_and_completed_authorizer_are_required_for_execution(self):
        self.data.update(mode='recover', rehearsal_run='789')
        self.responses['actions/runs/789'] = dict(self.responses['actions/runs/123'], id=789, status='completed', conclusion='success')
        self.responses['actions/runs/789/artifacts?per_page=100'] = {'artifacts': [dict(id=11, name='failed-recovery-rehearsal', expired=False)]}
        self.responses['actions/runs/123/attempts/1/jobs?per_page=100'] = {'jobs': [dict(name='Authenticate failed recovery', conclusion='success')]}
        proof = dict(schema='clearapi.failed-recovery-rehearsal/v1', source=self.source, rc_run='456',
                     failed_run=RECOVERY.FAILED_RUN, tests='passed', v2_sha256=RECOVERY.CONTROL.V2_SHA)
        self.archives[11] = archive({'recovery-rehearsal.json': proof})
        self.assertEqual(self.authorize(executing=True)['recovery_run'], '123')
        self.responses['actions/runs/123/attempts/1/jobs?per_page=100']['jobs'][0]['conclusion'] = 'failure'
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'authorization job missing'):
            self.authorize(executing=True)
        self.responses['actions/runs/789/artifacts?per_page=100']['artifacts'][0]['expired'] = True
        with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'rehearsal proof'):
            self.authorize(executing=True)

    def test_unreviewed_installed_asset_cannot_reach_production(self):
        stdin = SimpleNamespace(buffer=io.BytesIO(json.dumps(dict(self.data, token='synthetic-token')).encode()))
        with patch.object(RECOVERY.sys, 'argv', ['recovery', 'recover']), patch.object(RECOVERY.sys, 'stdin', stdin), \
             patch.object(RECOVERY, 'authority', return_value={}), patch.object(RECOVERY.os, 'geteuid', return_value=0), \
             patch.object(RECOVERY.CONTROL, 'api', return_value={'encoding': 'base64', 'content': 'dW5yZXZpZXdlZA=='}), \
             patch.object(RECOVERY.LIFECYCLE, 'read_file', return_value=b'reviewed asset'), \
             patch.object(RECOVERY.Recovery, 'complete') as complete:
            with self.assertRaisesRegex(RECOVERY.CONTROL.ReleaseBlocked, 'asset mismatch'):
                RECOVERY.main()
            complete.assert_not_called()

    def test_wrapper_and_recovery_helper_pins_match_reviewed_bytes(self):
        source = (SCRIPTS / 'production-deploy-v2.sh').read_text()
        self.assertIn('FAILED_RECOVERY_TOOL_SHA=' + hashlib.sha256((SCRIPTS / 'pg03-failed-recovery.py').read_bytes()).hexdigest(), source)
        self.assertEqual(hashlib.sha256((SCRIPTS / 'production-deploy-v2.sh').read_bytes()).hexdigest(), RECOVERY.CONTROL.V2_SHA)


if __name__ == '__main__':
    unittest.main()
