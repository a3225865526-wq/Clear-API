#!/usr/bin/env python3
"""Deterministic transition faults using real journals and stubbed external I/O.

Database transactions, real Runtime producers, Linux binaries and encrypted
restore are exercised separately by the existing model/HTTP/backup fixtures.
"""
import base64
import copy
import importlib.util
import json
import os
import functools
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('legacy_transition', SCRIPTS / 'pg03-legacy-transition.py')
M = importlib.util.module_from_spec(spec)
spec.loader.exec_module(M)


class FixtureIO:
    def __init__(self):
        self.source = {'container_id': 'a' * 64, 'docker_image_id': 'sha256:' + 'b' * 64,
            'oci_revision': 'c' * 40, 'ghcr_repo_digest': 'old@sha256:' + 'd' * 64,
            'ghcr_digest': 'sha256:' + 'd' * 64, 'container_created': '2026-10-13T00:00:00Z',
            'release_manifest_sha256': 'e' * 64, 'compose_base_sha256': 'f' * 64,
            'compose_override_sha256': '1' * 64, 'started_at': '2026-10-13T00:00:01Z',
            'binary_sha256': '2' * 64, 'process_started_ticks': '42', 'restart_policy': {'Name': 'always'}}
        self.candidate_value = {'image_ref': M.CONTROL.IMAGE + '@sha256:' + '3' * 64,
            'image_digest': 'sha256:' + '3' * 64, 'image_id': 'sha256:' + '4' * 64,
            'commit': '5' * 40, 'binary_sha256': '6' * 64, 'retire_binary_sha256': '7' * 64,
            'rc_run': '99', 'rc_manifest_sha256': '8' * 64}
        self.edge_value = {'id': '9' * 64, 'image': 'sha256:' + 'a' * 64,
            'started_at': '2026-10-13T00:00:00Z', 'running': True, 'paused': False}
        self.catalog = {'version': 5, 'approval_schema': 1, 'unified': False,
            'legacy_products': [{'id': 1, 'enabled': True}],
            'legacy_catalog': [{'id': 'credit-1', 'published': True}],
            'approval_products': [{'id': 'credit-' + str(i), 'state': 'draft', 'published': False, 'approval_id': 0} for i in range(8)]}
        self.catalog['_raw_digest'] = 'b' * 64
        self.review = {'plan': {'catalog_version': 5, 'catalog_digest': 'b' * 64,
            'snapshot_digest': 'c' * 64, 'source_digest': 'd' * 64, 'missing_store_ids': [1],
            'prepare_event_id': 1, 'prepare_digest': 'e' * 64, 'actor_id': 1, 'evidence_ref': 'review/fixture'},
            'confirmation': 'RETIRE_LEGACY v5:fixture'}
        self.business = {'balances': 'unchanged', 'needs_review': '4/10/4 41/93/41',
            'checkpoint.journal': 'f' * 64, 'admission.json': '1' * 64, 'channel2': 2}
        self.current = copy.deepcopy(self.source)
        self.running = True
        self.new = False
        self.auth_status = 503
        self.retire_fault = ''
        self.events = []

    def gateway(self):
        if not self.running:
            raise M.Blocked('Gateway not running')
        return copy.deepcopy(self.current)

    def edge(self):
        return copy.deepcopy(self.edge_value)

    def edge_paused(self, expected, paused):
        if any(self.edge_value[k] != expected[k] for k in ('id', 'image', 'started_at')):
            raise M.Blocked('edge changed')
        self.edge_value['paused'] = paused
        self.events.append('edge-paused' if paused else 'edge-released')

    def runtime(self, token):
        if token != self.token:
            return 401, {'success': False}
        if self.auth_status != 503:
            return self.auth_status, {'success': False, 'code': 'AUTH_REQUIRED'}
        if not self.new:
            return 503, {'success': False, 'status': 'BLOCKED', 'code': 'APPROVAL_VIEW_INVALID'}
        return 200, {'success': True, 'data': {'status': 'PASS',
            'container_id': self.current['container_id'], 'started_at': self.current['started_at'],
            'build_identity': 'sha256:' + self.current['binary_sha256'], 'effective_product_count': 0,
            'store_enabled': False, 'online_payment_enabled': False, 'pool_enabled': False,
            'referral_enabled': False, 'billing_mode': 'enforce'}}

    def candidate(self, candidate):
        if candidate != self.candidate_value:
            raise M.Blocked('candidate mismatch')

    def retirement(self, candidate, actor=None, evidence=None, apply=False):
        if not apply:
            return copy.deepcopy(self.review)
        if self.running or not self.edge_value['paused']:
            raise M.Blocked('writers not isolated')
        if self.retire_fault == 'rollback':
            raise M.Blocked('transaction rolled back')
        self.events.append('retirement-committed')
        self.catalog.update(version=6, legacy_retirement={'plan': copy.deepcopy(self.review['plan']), 'event_id': 2})
        if self.retire_fault == 'uncertain':
            raise OSError('synthetic-private-transport-detail')

    def snapshot(self):
        return copy.deepcopy(self.business), copy.deepcopy(self.catalog)

    def stopped(self, source):
        if self.running or self.current['container_id'] != source['container_id']:
            raise M.Blocked('wrong stopped writer')

    def disable_old_restart(self, source):
        self.stopped(source)
        self.events.append('old-restart-disabled')

    def stop_gateway(self):
        self.running = False
        self.events.append('gateway-stopped')

    def start_candidate(self):
        self.current = dict(self.source, container_id='c' * 64, docker_image_id=self.candidate_value['image_id'],
            oci_revision=self.candidate_value['commit'], binary_sha256=self.candidate_value['binary_sha256'],
            started_at='2026-10-16T00:00:00Z')
        self.running, self.new = True, True
        self.events.append('candidate-started')


class TransitionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.paths = M.Paths(Path(self.temp.name))
        for p in (self.paths.app, self.paths.session.parent, self.paths.audit.parent,
                  self.paths.backups / 'manifests', self.paths.pool):
            p.mkdir(parents=True, exist_ok=True)
        self.now = int(time.time())
        self.io = FixtureIO()
        self.t = M.Transition(self.paths, self.io, now=lambda: self.now)
        claims = {'iat': self.now, 'exp': self.now + 900, 'sub': '1', 'token_use': 'access'}
        payload = base64.urlsafe_b64encode(M.encoded(claims)).decode().rstrip('=')
        self.io.token = 'fixture.' + payload + '.synthetic-private-signature'
        self.data = {'token': 'synthetic-private-github-token', 'commit': self.io.candidate_value['commit'],
            'digest': self.io.candidate_value['image_digest'], 'rc_run': '99', 'evidence_ref': 'review/fixture',
            'login_response': {'success': True, 'data': {'access_token': self.io.token,
                'token_type': 'bearer', 'access_expires_at': self.now + 900}}}
        self.manifest = {'image_ref': self.io.candidate_value['image_ref'], 'image_digest': self.io.candidate_value['image_digest'],
            'new_api_image_id': self.io.candidate_value['image_id'], 'source_commit': self.io.candidate_value['commit'],
            'binary_sha256': self.io.candidate_value['binary_sha256'], 'legacy_retire_binary_sha256': self.io.candidate_value['retire_binary_sha256']}
        self.args = ('100', self.io.candidate_value['image_digest'], self.io.candidate_value['commit'])

    def review(self):
        with patch.object(M.CONTROL, 'approved_rc', return_value={'run_attempt': 1}), \
                patch.object(M.CONTROL, 'api', return_value={}), \
                patch.object(M.CONTROL, 'release_manifest', return_value=(self.manifest, '8' * 64)):
            return self.t.review(copy.deepcopy(self.data))

    def authorize(self):
        sha = self.review()
        data = dict(self.data, run='100', transition_sha256=sha)
        title = 'Deploy pg03 ' + data['digest'] + ' ' + sha
        M.CONTROL.maintenance_authorization(data, self.manifest, '8' * 64, ['a3225865526-wq'],
            lambda _: {'display_title': title}, self.paths)
        return data

    def phase(self, name, *args):
        return self.t.phase(name, *self.args, *args)

    def backup(self):
        from datetime import datetime, timezone
        stamp = datetime.fromtimestamp(self.now, timezone.utc)
        ident = stamp.strftime('%Y%m%dT%H%M%SZ')
        path = self.paths.backups / 'encrypted' / ident
        path.mkdir(parents=True)
        raw = b'synthetic backup transport fixture; real encryption tested separately'
        artifact = path / ('clearapi-backup-' + ident + '.tar.gz.gpg')
        artifact.write_bytes(raw)
        artifact.chmod(0o600)
        manifest = {'backup_identity_schema_version': 1, 'backup_id': ident, 'created_at_utc': stamp.isoformat(),
            'pg03_recovery': {'consistency': 'unchanged_across_dump_and_bundle', 'scope': ['compose-pg03.yaml', 'config/pg03',
                'pool-executor/auths', 'pool-executor/pool', 'pool-executor/receipts']},
            'artifacts': {k: {'sha256': 'a' * 64} for k in ('database_dump', 'pg03_state_archive', 'pg03_state_snapshot')},
            'running_identity': {k: v for k, v in self.io.source.items() if k not in
                ('started_at', 'binary_sha256', 'process_started_ticks', 'restart_policy')},
            'encryption': {'encrypted_artifact': {'name': artifact.name, 'bytes': len(raw), 'sha256': M.digest(raw)}}}
        M.publish(self.paths.backups / 'manifests' / (ident + '.json'), manifest)
        self.phase('backup', stamp.isoformat())
        return artifact

    def enter(self):
        self.authorize()
        self.phase('preflight')
        self.backup()
        self.phase('begin')
        self.phase('isolate')
        self.io.stop_gateway()

    def test_complete_transition_is_one_use_and_never_calls_old_fail_pass(self):
        self.enter()
        self.phase('retire')
        self.assertIn('old-restart-disabled', self.io.events)
        self.assertLess(self.io.events.index('old-restart-disabled'), self.io.events.index('retirement-committed'))
        self.io.start_candidate()
        self.phase('postflight')
        self.assertTrue(self.io.edge_value['paused'])
        self.phase('release-edge')
        self.assertFalse(self.io.edge_value['paused'])
        M.publish(self.paths.manifest, {'releases': {'current': {'status': 'success', 'image_digest': self.args[1]}}})
        self.phase('complete')
        original = list(self.io.events)
        self.assertEqual(self.phase('status'), 'completed')
        self.assertEqual(self.io.events, original)
        with self.assertRaises(ValueError):
            self.phase('retire')
        journal = self.paths.audit.read_text()
        self.assertIn('OLD_RUNTIME_BLOCKED_NOT_PASS', journal)
        self.assertNotIn('synthetic-private', journal)
        self.assertEqual(self.io.events.count('retirement-committed'), 1)

    def test_missing_or_mismatched_authority_cannot_consume_or_stop(self):
        self.review()
        with self.assertRaises(OSError):
            self.phase('preflight')
        self.assertFalse(self.paths.state.exists())
        self.assertTrue(self.io.running)

    def test_authenticated_failure_expiry_identity_and_invariant_drift_reject(self):
        for change in ('401', '403', 'identity', 'billing', 'checkpoint', 'expired', 'digest'):
            with self.subTest(change=change):
                # A separate fixture keeps every authority genuinely one-use.
                case = TransitionTest()
                case.setUp()
                try:
                    case.authorize()
                    if change in ('401', '403'):
                        case.io.auth_status = int(change)
                    elif change == 'identity':
                        case.io.current['process_started_ticks'] = '43'
                    elif change == 'billing':
                        case.io.business['balances'] = 'changed'
                    elif change == 'checkpoint':
                        case.io.business['checkpoint.journal'] = 'changed'
                    elif change == 'expired':
                        case.now += 601
                    else:
                        case.args = ('100', 'sha256:' + 'f' * 64, case.args[2])
                    with case.assertRaises(ValueError):
                        case.phase('preflight')
                    case.assertFalse(case.paths.state.exists())
                    case.assertTrue(case.io.running)
                finally:
                    case.doCleanups()

    def test_forged_review_title_and_candidate_cannot_publish_authority(self):
        sha = self.review()
        data = dict(self.data, run='100', transition_sha256=sha)
        for title, reviewers in [('normal deployment', ['a3225865526-wq']),
                ('Deploy pg03 ' + data['digest'] + ' ' + sha, ['unapproved'])]:
            with self.subTest(title=title), self.assertRaises(ValueError):
                M.CONTROL.maintenance_authorization(data, self.manifest, '8' * 64, reviewers,
                    lambda _: {'display_title': title}, self.paths)
        self.assertFalse(self.paths.authorization.exists())

    def test_preparation_refuses_auth_failure_and_unrelated_scope(self):
        self.io.auth_status = 401
        with self.assertRaises(ValueError):
            self.review()
        self.assertFalse(self.paths.request.exists())
        self.io.auth_status = 503
        self.io.catalog['legacy_products'][0]['id'] = 2
        with self.assertRaises(ValueError):
            self.review()

    def test_backup_tamper_blocks_before_writer_isolation(self):
        self.authorize()
        artifact = self.backup()
        artifact.write_bytes(b'changed')
        with self.assertRaises(ValueError):
            self.phase('begin')
        self.assertFalse(self.paths.state.exists())
        self.assertTrue(self.io.running)

    def test_transaction_rollback_and_ambiguous_commit_remain_stopped(self):
        for fault in ('rollback', 'uncertain'):
            with self.subTest(fault=fault):
                case = TransitionTest()
                case.setUp()
                try:
                    case.enter()
                    case.io.retire_fault = fault
                    with case.assertRaises((ValueError, OSError)):
                        case.phase('retire')
                    case.phase('fail')
                    case.assertFalse(case.io.running)
                    case.assertTrue(case.io.edge_value['paused'])
                    case.assertEqual(case.io.events.count('retirement-committed'), int(fault == 'uncertain'))
                    with case.assertRaises(ValueError):
                        case.phase('status')
                finally:
                    case.doCleanups()

    def test_committed_retirement_start_or_postflight_failure_never_restarts_old(self):
        self.enter()
        self.phase('retire')
        # Candidate startup failed: no candidate ever reports PASS.
        with self.assertRaises(ValueError):
            self.phase('postflight')
        self.phase('fail')
        self.assertFalse(self.io.running)
        self.assertTrue(self.io.edge_value['paused'])
        self.assertIn('legacy_retirement', self.io.catalog)
        self.assertEqual(M.read_json(self.paths.state)['stage'], 'failed')

    def test_postflight_rejects_new_identity_auth_and_early_public_reopen(self):
        for change in ('identity', 'auth', 'early-edge'):
            with self.subTest(change=change):
                case = TransitionTest()
                case.setUp()
                try:
                    case.enter()
                    case.phase('retire')
                    case.io.start_candidate()
                    if change == 'identity':
                        case.io.current['docker_image_id'] = 'sha256:'+'0'*64
                    elif change == 'auth':
                        case.io.auth_status = 403
                    else:
                        case.io.edge_value['paused'] = False
                    with case.assertRaises(ValueError):
                        case.phase('postflight')
                    case.phase('fail')
                    case.assertFalse(case.io.running)
                    case.assertTrue(case.io.edge_value['paused'])
                    case.assertIn('legacy_retirement', case.io.catalog)
                finally:
                    case.doCleanups()

    def test_journal_symlink_and_proof_tamper_fail_closed(self):
        self.authorize()
        before = self.paths.request.read_bytes()
        request = M.read_json(self.paths.request)
        request['review']['plan']['missing_store_ids'] = []
        self.paths.request.write_bytes(M.encoded(request))
        with self.assertRaises(ValueError):
            self.phase('preflight')
        self.paths.request.write_bytes(before)
        target = self.paths.app / 'unrelated'
        target.write_bytes(b'preserve')
        target.chmod(0o600)
        self.paths.state.symlink_to(target)
        with self.assertRaises(OSError):
            self.phase('status')
        self.assertEqual(target.read_bytes(), b'preserve')


class V2MaintenanceChainTest(unittest.TestCase):
    """Run the actual V2 shell, validators and transition phases end to end.

    Reuse the established host-command transport harness. Docker/systemd/HTTP
    remain explicit stubs; the shell order, credential ownership, manifest,
    approval consumption, secure files and failure containment are real code.
    """
    def setUp(self):
        with patch.dict(os.environ, DEPLOY_SOURCE=str(SCRIPTS / 'production-deploy-v2.sh')):
            spec = importlib.util.spec_from_file_location('v2_base_tests', SCRIPTS / 'ci/test-production-deploy.py')
            self.base = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(self.base)
        # PG03's existing secure environment reader deliberately refuses /tmp.
        original = tempfile.TemporaryDirectory
        with patch.object(self.base.tempfile, 'TemporaryDirectory', functools.partial(original, dir='/root')):
            self.base.WrapperModesTest.setUp(self)

    def execute(self, failure=''):
        real_run = subprocess.run
        def invoke(args, **kwargs):
            if args[:2] != ['bash', '-c']:
                return real_run(args, **kwargs)
            self.prepare_chain(failure)
            kwargs['env'].update(POOL_MODE='pg03', LEGACY_TRANSITION_SHA256='a'*64)
            return real_run(args, **kwargs)
        with patch.object(self.base.subprocess, 'run', invoke):
            return self.base.WorkflowStdinTest.run_chain(self, failure=failure)

    def prepare_chain(self, failure):
        paths = M.Paths(self.root)
        for p in (paths.app, paths.session.parent, paths.audit.parent, paths.backups / 'manifests'):
            p.mkdir(parents=True, exist_ok=True)
        executor = {'image_ref': M.CONTROL.EXECUTOR, 'image_digest': M.CONTROL.EXECUTOR.split('@')[1],
            'image_id': 'sha256:'+'e'*64, 'oci_revision': M.CONTROL.EXECUTOR_REVISION,
            'binary_sha256': M.CONTROL.EXECUTOR_BINARY, 'container_id': 'e'*64,
            'executor_id': 'fixture-executor', 'process_epoch': 'f'*32}
        io = FixtureIO()
        io.token = 'fixture.header.signature'
        io.source.update(container_id='c'*64, docker_image_id='sha256:'+'8'*64,
            started_at=self.base.STARTED_AT, binary_sha256=self.base.BINARY_SHA)
        io.current = copy.deepcopy(io.source)
        io.candidate_value.update(image_ref=M.CONTROL.IMAGE+'@'+self.base.DIGEST,
            image_digest=self.base.DIGEST, image_id='sha256:'+'7'*64,
            commit=self.base.COMMIT, binary_sha256=self.base.BINARY_SHA)
        now = int(time.time())
        request = {'schema_version': M.SCHEMA, 'transition_id': '6f6ebdbe-9e74-4d25-b608-64eecbe0c0b1',
            'created_at': now, 'expires_at': now+600, 'source': io.source, 'edge': io.edge_value,
            'candidate': io.candidate_value, 'review': io.review, 'invariants': io.business,
            'old_runtime': {'http_status': 503, 'status': 'BLOCKED', 'code': 'APPROVAL_VIEW_INVALID'}}
        M.publish(paths.request, request)
        M.publish(paths.plan, io.review)
        paths.session.write_text(io.token+'\n')
        paths.session.chmod(0o600)
        M.publish(paths.authorization, {'schema_version': M.SCHEMA, 'transition_id': request['transition_id'],
            'request_sha256': M.digest(M.read_file(paths.request)), 'approval_run': '123',
            'reviewers': ['a3225865526-wq'], 'candidate': io.candidate_value})
        target = {'schema_version': 1, 'pool_mode': 'pg03', 'components': {
            'gateway': {'image_ref': io.candidate_value['image_ref'], 'image_digest': self.base.DIGEST,
                'oci_revision': self.base.COMMIT, 'image_id': io.candidate_value['image_id']}, 'executor': executor},
            'validation': {'approval': 'passed'}, 'legacy_transition': {'schema_version': M.SCHEMA,
                'request_sha256': M.digest(M.read_file(paths.request))}}
        M.publish(paths.app / 'pg03-target-manifest.json', target)
        paths.app.joinpath('compose-pg03.yaml').write_text('services: {}\n')
        # The actual target/environment parsers consume these protected files.
        env_file = self.root / 'pg03.env'
        env_file.write_text("CLEARAPI_POOL_EXECUTOR_ID=fixture-executor\nCLEARAPI_PG02_PROCESS_EPOCH="+'f'*32+
            "\nCLEARAPI_PG03_POLICY_KEY="+'p'*43+"\nCLEARAPI_PG02_GATEWAY_PROOF_KEY="+'g'*43+
            "\nCLEARAPI_POOL_CLIPROXY_KEY="+'k'*43+'\n')
        env_file.chmod(0o600)
        self.manifest = paths.manifest
        self.manifest.write_text(json.dumps({'releases': {'current': {'new_api_image_id': io.source['docker_image_id'],
            'image_digest': self.base.DIGEST}}}))
        # The original harness keeps its public paths for assertions; the real
        # V2 app directory is independently relocated into the fixture tree.
        with self.config.open('a') as f:
            f.write('\nDEPLOY_APP_DIR='+str(paths.app)+'\nDEPLOY_PG03_COMPOSE_FILE='+str(paths.app/'compose-pg03.yaml')+
                '\nDEPLOY_PG03_TARGET_MANIFEST='+str(paths.app/'pg03-target-manifest.json')+'\nDEPLOY_BACKUP_DIR='+str(paths.backups)+'\nDEPLOY_MANIFEST='+str(paths.manifest)+'\n')
        source = self.wrapper.read_text().replace('/opt/clearapi/app', str(paths.app))
        source = source.replace('/opt/clearapi/config/pg03/pg03.env', str(env_file))
        source = source.replace('/opt/clearapi/secrets/legacy-transition-session', str(paths.session))
        source = source.replace('/opt/clearapi/bin/pg03-legacy-transition.py', str(self.root/'transition-cli.py'))
        self.wrapper.write_text(source)
        payload = {'source': io.source, 'candidate': io.candidate_value, 'catalog': io.catalog,
            'business': io.business, 'edge': io.edge_value, 'review': io.review}
        (self.root/'transport.json').write_text(json.dumps(payload))
        runner = '''import importlib.util,json,os,sys
from pathlib import Path
root=Path(os.environ['TEST_ROOT'])
spec=importlib.util.spec_from_file_location('transition_fixture',sys.argv[1])
fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)
M=fixture.M
data=json.loads((root/'transport.json').read_text())
io=fixture.FixtureIO()
io.source=data['source'];io.candidate_value=data['candidate'];io.catalog=data['catalog'];io.business=data['business'];io.edge_value=data['edge'];io.review=data['review']
io.current=dict(io.source)
io.token='fixture.header.signature'
io.new=(root/'replaced').exists()
io.running=not (root/'stopped').exists()
if io.new:io.current.update(container_id='a'*64,docker_image_id=io.candidate_value['image_id'],oci_revision=io.candidate_value['commit'],binary_sha256=io.candidate_value['binary_sha256'])
io.retire_fault='rollback' if os.environ['TEST_FAILURE']=='retire-rollback' else ''
t=M.Transition(M.Paths(root),io)
try:
 result=t.phase(*sys.argv[3:])
 print(result)
except Exception:
 print('fixture boundary failed closed',file=sys.stderr)
 sys.exit(1)
finally:
 data.update(catalog=io.catalog,business=io.business,edge=io.edge_value)
 (root/'transport.json').write_text(json.dumps(data))
 if not io.running:(root/'stopped').touch()
 with open(os.environ['TEST_TRACE'],'a') as f:f.write(json.dumps({'name':'transition-phase','args':sys.argv[3:],'stage':M.read_json(t.paths.state)['stage'] if t.paths.state.exists() else None})+'\\n')
'''
        cli = 'import sys,runpy\nsys.argv.insert(1,'+repr(str(Path(__file__)))+')\n'+runner
        (self.root/'transition-cli.py').write_text(cli)
        host = self.root/'host-command'
        original = host.read_text().replace("command=a[-1].replace('/opt/clearapi/bin/production-deploy.sh',str(root/'wrapper.sh'))",
            "command=a[-1].replace('/opt/clearapi/bin/production-deploy.sh',str(root/'wrapper.sh'))")
        extension = r'''
data=json.loads((root/'transport.json').read_text())
candidate=data['candidate']; old=data['source']; executor_ref='EXECUTOR_REF'; executor_id='sha256:'+'e'*64
if name=='systemctl':
 if a[0]=='start':
  if fail=='backup':sys.exit(1)
  import datetime
  now=datetime.datetime.now(datetime.timezone.utc); ident=now.strftime('%Y%m%dT%H%M%SZ')
  target=root/'opt/clearapi/backups'; path=target/'encrypted'/ident;path.mkdir(parents=True)
  raw=b'explicit synthetic backup transport fixture';artifact=path/('clearapi-backup-'+ident+'.tar.gz.gpg');artifact.write_bytes(raw);artifact.chmod(0o600)
  m={'backup_identity_schema_version':1,'backup_id':ident,'created_at_utc':now.isoformat(),'running_identity':{k:v for k,v in old.items() if k not in ('started_at','binary_sha256','process_started_ticks','restart_policy')},'pg03_recovery':{'consistency':'unchanged_across_dump_and_bundle','scope':['compose-pg03.yaml','config/pg03','pool-executor/auths','pool-executor/pool','pool-executor/receipts']},'artifacts':{k:{'sha256':'a'*64} for k in ('database_dump','pg03_state_archive','pg03_state_snapshot')},'encryption':{'encrypted_artifact':{'name':artifact.name,'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}}}
  manifest=target/'manifests'/(ident+'.json');manifest.write_text(json.dumps(m));manifest.chmod(0o600)
 elif a[0]=='show':print('0')
 sys.exit(0)
if name=='sleep':sys.exit(0)
if name=='curl' and any(x.startswith('https://') for x in a) and (data['edge']['paused'] or fail=='public-health'):sys.exit(1)
if name=='docker':
 if a[0]=='pull':(root/'logged-in').touch();sys.exit(0)
 if a[:2]==['image','inspect']:
  ref=a[-1];is_executor=ref==executor_ref;is_candidate=ref in (candidate['image_ref'],candidate['image_id'])
  image_id=executor_id if is_executor else candidate['image_id'] if is_candidate else old['docker_image_id']
  revision='EXECUTOR_REVISION' if is_executor else candidate['commit'] if is_candidate else old['oci_revision']
  if '--format' not in a:print(json.dumps([{'Id':image_id,'RepoDigests':[ref],'Os':'linux','Architecture':'amd64','Config':{'Labels':{'org.opencontainers.image.revision':revision}}}]))
  elif a[a.index('--format')+1]=='{{.Id}}':print(image_id)
  elif 'RepoDigests' in a[a.index('--format')+1]:print(candidate['image_ref'])
  else:print(revision)
  sys.exit(0)
 if a[:2]==['ps','-a']:print('e'*12+' fixture-executor pool-executor');sys.exit(0)
 if a[0]=='inspect':
  cid=a[-1];ex=cid=='e'*64;new=cid=='a'*64
  image=executor_id if ex else candidate['image_id'] if new else old['docker_image_id']
  ref=executor_ref if ex else candidate['image_ref'] if new else 'registry/example@'+'sha256:'+'a'*64
  if '--format' in a:
   fmt=a[a.index('--format')+1]
   print(cid+' 2026-10-13T00:00:00Z '+image if 'StartedAt' in fmt else 'clearapi_internal' if 'NetworkSettings' in fmt else image)
  else:
   env=['CLEARAPI_POOL_ROLE=service','CLEARAPI_POOL_EXECUTOR_ID=fixture-executor','CLEARAPI_PG02_PROCESS_EPOCH='+'f'*32,'CLEARAPI_PG03_POLICY_KEY='+'p'*43,'CLEARAPI_PG02_GATEWAY_PROOF_KEY='+'g'*43,'MANAGEMENT_PASSWORD='+'k'*43,'CLEARAPI_POOL_CLIPROXY_KEY='+'k'*43,'CLEARAPI_POOL_ENABLED=true','CLEARAPI_POOL_CLIPROXY_URL=http://pool-executor:8317']
   mounts=[{'Destination':dst,'Source':src,'Type':'bind','RW':rw} for dst,src,rw in [('/CLIProxyAPI/config.yaml','/opt/clearapi/config/pg03/executor.yaml',False),('/var/lib/pool-executor/auths','/data/clearapi/pool-executor/auths',True),('/var/lib/pool-executor/pool','/data/clearapi/pool-executor/pool',True),('/var/lib/pool-executor/receipts','/data/clearapi/pool-executor/receipts',True)]]
   print(json.dumps([{'Id':cid,'Image':image,'State':{'Running':True,'Restarting':False,'Health':{'Status':'healthy'}},'Config':{'Image':ref,'Env':env,'Entrypoint':['/CLIProxyAPI/CLIProxyAPI'],'Cmd':['--config','/CLIProxyAPI/config.yaml','--local-model'],'Labels':{'com.docker.compose.service':'pool-executor' if ex else 'new-api'}},'HostConfig':{'NetworkMode':'clearapi_internal','PortBindings':{}},'NetworkSettings':{'Networks':{'clearapi_internal':{}},'Ports':{}},'Mounts':mounts}]))
  sys.exit(0)
 if a[0]=='exec':
  if 'sha256sum' in a:print('EXECUTOR_BINARY'+'  /proc/1/exe')
  sys.exit(0)
 if a[0]=='compose':
  if 'ps' in a:print('e'*64 if a[-1]=='pool-executor' else 'postgres-container' if a[-1]=='postgres' else 'a'*64 if (root/'replaced').exists() else 'c'*64);sys.exit(0)
  if 'stop' in a:(root/'stopped').touch();sys.exit(0)
  if 'up' in a:
   if fail=='candidate-start':sys.exit(1)
   (root/'replaced').touch();(root/'stopped').unlink(missing_ok=True);sys.exit(0)
  if 'config' in a:
   new=any('compose-jobs.yaml' in x for x in a) or (root/'replaced').exists()
   image=candidate['image_ref'] if new else 'registry/example@sha256:'+'a'*64
   env={'SHUTDOWN_TIMEOUT_SECONDS':'120','CLEARAPI_POOL_ENABLED':'true','CLEARAPI_POOL_CLIPROXY_URL':'http://pool-executor:8317','CLEARAPI_POOL_CLIPROXY_KEY':'k'*43,'CLEARAPI_PG03_POLICY_KEY':'p'*43,'CLEARAPI_PG02_GATEWAY_PROOF_KEY':'g'*43}
   g={'image':image,'environment':env,'networks':{'internal':{}},'stop_signal':'SIGTERM','stop_grace_period':'2m30s'}
   e={'image':executor_ref,'environment':dict(env,MANAGEMENT_PASSWORD='k'*43),'networks':{'internal':{}}}
   print(json.dumps({'services':{'new-api':g,'production-bootstrap':{'image':image},'pool-executor':e}}));sys.exit(0)
'''
        extension = extension.replace('EXECUTOR_REF', M.CONTROL.EXECUTOR).replace('EXECUTOR_REVISION', M.CONTROL.EXECUTOR_REVISION).replace('EXECUTOR_BINARY', M.CONTROL.EXECUTOR_BINARY)
        host.write_text(original.replace("fail=os.environ['TEST_FAILURE']", "fail=os.environ['TEST_FAILURE']\n"+extension))
        (self.root/'sleep').symlink_to(host)

    def test_actual_v2_retires_only_after_backup_and_stopping_then_verifies_new(self):
        result, trace, _ = self.execute()
        self.assertEqual(result.returncode, 0, result.stderr)
        phases = [r['args'][0] for r in trace if r['name']=='transition-phase']
        self.assertEqual(phases, ['status','preflight','backup','begin','isolate','retire','postflight','release-edge','complete'])
        self.assertNotIn('Runtime Gate PASS (before)', result.stderr)
        self.assertIn('Runtime Gate PASS (after)', result.stderr)
        self.assertIn('DEPLOYMENT SUCCESS', result.stderr)
        self.assertEqual(M.read_json(M.Paths(self.root).state)['stage'], 'passed')
        self.assertFalse(json.loads((self.root/'transport.json').read_text())['edge']['paused'])
        self.assertTrue(all('--no-deps' in r['args'] for r in trace if r['name']=='docker' and 'up' in r['args']))

    def test_actual_v2_commit_then_start_failure_keeps_maintenance_and_event(self):
        result, trace, _ = self.execute('candidate-start')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('DEPLOYMENT SUCCESS', result.stderr)
        data = json.loads((self.root/'transport.json').read_text())
        self.assertIn('legacy_retirement', data['catalog'])
        self.assertTrue(data['edge']['paused'])
        self.assertTrue((self.root/'stopped').exists())
        self.assertEqual(M.read_json(M.Paths(self.root).state)['stage'], 'failed')
        self.assertFalse(any(r['name']=='docker' and 'restart' in r['args'] for r in trace))

    def test_actual_v2_public_health_failure_closes_edge_and_preserves_committed_history(self):
        result, _, _ = self.execute('public-health')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('DEPLOYMENT SUCCESS', result.stderr)
        data = json.loads((self.root/'transport.json').read_text())
        self.assertIn('legacy_retirement', data['catalog'])
        self.assertTrue(data['edge']['paused'])
        self.assertTrue((self.root/'stopped').exists())
        self.assertEqual(M.read_json(M.Paths(self.root).state)['stage'], 'failed')

    def test_actual_v2_transaction_failure_never_starts_a_candidate(self):
        result, trace, _ = self.execute('retire-rollback')
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.root/'stopped').exists())
        self.assertNotIn('legacy_retirement', json.loads((self.root/'transport.json').read_text())['catalog'])
        self.assertFalse(any(r['name']=='docker' and 'up' in r['args'] for r in trace))

if __name__ == '__main__':
    unittest.main()
