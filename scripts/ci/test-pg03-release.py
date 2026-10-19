#!/usr/bin/env python3
"""Offline workflow invocation, real-review rejection and probe-install contracts."""
import base64
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest
import urllib.error
import zipfile
from unittest.mock import patch


SCRIPTS = Path(__file__).resolve().parents[1]


def module(name):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / (name + '.py'))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


CONTROL = module('pg03-release-control')
PROBE = module('runtime-probe-session')
INSPECTION = module('ci/inspect-vps-authorization')


class VPSAuthorizationInspectionTest(unittest.TestCase):
    def setUp(self):
        self.commands = []
        self.bad_hash = False
        self.runtime_exit = 0
        self.identity_exit = 0

    def remote(self, command):
        self.commands.append(command)
        if command == '/usr/bin/id -un':
            return {'exit_code': self.identity_exit, 'stdout': 'clearapi-deploy\n', 'stderr': ''}
        if command == '/usr/bin/sudo -n -l':
            return {'exit_code': 0, 'stdout': 'fixture: hash probes and V2 only', 'stderr': ''}
        prefix = '/usr/bin/sudo -n /usr/bin/sha256sum '
        if command.startswith(prefix):
            target = command[len(prefix):]
            source = next(source for source, path in INSPECTION.ASSETS.items() if path == target)
            digest = hashlib.sha256((INSPECTION.ROOT / source).read_bytes()).hexdigest()
            if self.bad_hash:
                digest = '0' * 64
            return {'exit_code': 0, 'stdout': digest + '  ' + target + '\n', 'stderr': ''}
        if command == '/usr/bin/sudo -n /opt/clearapi/bin/production-deploy-v2.sh --verify':
            return {'exit_code': self.runtime_exit, 'stdout': 'fixture verification result', 'stderr': ''}
        raise AssertionError('Unexpected remote command: ' + command)

    def test_inspection_never_installs_logs_in_or_deploys(self):
        with patch.object(INSPECTION, 'ssh', side_effect=self.remote):
            result = INSPECTION.inspect()
        self.assertEqual(result['status'], 'VERIFIED')
        self.assertTrue(result['runtime_gate_passed'])
        for field in ('production_deployed', 'installation_attempted', 'session_creation_attempted'):
            self.assertFalse(result[field])
        self.assertEqual(len(self.commands), 9)
        self.assertTrue(all(command in ('/usr/bin/id -un', '/usr/bin/sudo -n -l',
            '/usr/bin/sudo -n /opt/clearapi/bin/production-deploy-v2.sh --verify')
            or command.startswith('/usr/bin/sudo -n /usr/bin/sha256sum /opt/clearapi/')
            for command in self.commands))

    def test_changed_installed_bytes_and_failed_ssh_never_invoke_runtime(self):
        for bad_hash, identity_exit in ((True, 0), (False, 255)):
            with self.subTest(bad_hash=bad_hash, identity_exit=identity_exit):
                self.commands = []
                self.bad_hash, self.identity_exit = bad_hash, identity_exit
                with patch.object(INSPECTION, 'ssh', side_effect=self.remote):
                    result = INSPECTION.inspect()
                self.assertEqual(result['status'], 'BLOCKED')
                self.assertFalse(any('--verify' in command for command in self.commands))

    def test_probe_failure_is_not_an_authorization_or_runtime_pass(self):
        self.runtime_exit = 1
        with patch.object(INSPECTION, 'ssh', side_effect=self.remote):
            result = INSPECTION.inspect()
        self.assertEqual(result['status'], 'BLOCKED')
        self.assertFalse(result['runtime_gate_passed'])

    def test_manual_inspection_preserves_existing_environment_and_excludes_pull_steps(self):
        workflow = (SCRIPTS.parent / '.github/workflows/ghcr-pull-rehearsal.yml').read_text()
        self.assertIn('  workflow_dispatch:', workflow)
        self.assertNotIn('  workflow_run:', workflow)
        self.assertIn('    environment: production', workflow)
        self.assertIn('  cancel-in-progress: false', workflow)
        self.assertNotIn('PROD_SSH_PRIVATE_KEY }}\n', (SCRIPTS / 'ci/inspect-vps-authorization.py').read_text())
        for name in ('Verify installed rehearsal wrapper matches this commit', 'Run pull rehearsal on VPS'):
            block = workflow.split('      - name: ' + name, 1)[1].split('      - name:', 1)[0]
            self.assertIn('if: ${{ !inputs.inspection_only }}', block)

    def test_ssh_options_preserve_pinned_host_key_and_noninteractive_authentication(self):
        response = subprocess.CompletedProcess([], 0, 'fixture', '')
        with patch.object(INSPECTION.subprocess, 'run', return_value=response) as run:
            INSPECTION.ssh('/usr/bin/id -un')
        args = run.call_args.args[0]
        self.assertIn('BatchMode=yes', args)
        self.assertIn('StrictHostKeyChecking=yes', args)
        self.assertIn('-T', args)
        with patch.dict(os.environ, {'PROD_SSH_ALIAS': '-oProxyCommand=unsafe'}):
            with self.assertRaises(ValueError):
                INSPECTION.ssh('/usr/bin/id -un')


class WorkflowContractTest(unittest.TestCase):
    def setUp(self):
        self.workflow = (SCRIPTS.parent / '.github/workflows/deploy-production.yml').read_text()
        self.steps = {}
        for block in self.workflow.split('      - name: ')[1:]:
            name, body = block.split('\n', 1)
            if '        run: |\n' in body:
                self.steps[name] = {'run': textwrap.dedent(body.split('        run: |\n', 1)[1])}

    def test_approval_and_concurrency_remain_hard_gates(self):
        self.assertIn('\n    environment: production\n',self.workflow)
        self.assertIn('\nconcurrency:\n  group: production\n  cancel-in-progress: false\n',self.workflow)
        self.assertNotIn('gh run cancel', self.workflow)
        self.assertEqual(hashlib.sha256((SCRIPTS / 'production-deploy-v2.sh').read_bytes()).hexdigest(), CONTROL.V2_SHA)

    def test_automatic_call_requires_prior_proof_job_and_stays_on_v2(self):
        prepare = self.workflow.split('      - name: Verify PG03 control tool and prepare approved pair', 1)[1].split('      - name:', 1)[0]
        self.assertIn("if: env.POOL_MODE == 'pg03'", prepare)
        self.assertIn('  workflow_call:\n', self.workflow)
        self.assertIn('    needs: authorize\n', self.workflow)
        self.assertIn('    environment: production\n', self.workflow)
        authorize = self.workflow.split('  authorize:\n', 1)[1].split('\n  deploy:\n', 1)[0]
        self.assertNotIn('environment: production', authorize)
        self.assertIn('automatic-release.py authorize', authorize)
        self.assertIn('AUTO_ENABLED: ${{ vars.CLEARAPI_AUTO_PRODUCTION_ENABLED }}', authorize)
        self.assertIn("[[ \"$MIGRATION_COMPATIBLE\" == false ]]", self.workflow)
        self.assertIn("DEPLOY_WRAPPER=/opt/clearapi/bin/production-deploy-v2.sh", self.workflow)
        deploy_v2 = (SCRIPTS / 'production-deploy-v2.sh').read_text()
        self.assertIn("'mode': 'automatic-proof-v1'", (SCRIPTS / 'pg03-release-control.py').read_text())
        self.assertIn("autonomous.get('mode') == 'automatic-proof-v1'", deploy_v2)
        self.assertIn("set(autonomous) == {'mode', 'run', 'rehearsal_run'}", deploy_v2)
        automatic = (SCRIPTS.parent / '.github/workflows/automatic-deploy.yml').read_text()
        self.assertIn('workflows: [Deployment rehearsal]', automatic)
        self.assertIn("vars.CLEARAPI_AUTO_PRODUCTION_ENABLED == 'true'", automatic)
        self.assertIn("github.event.workflow_run.head_branch == 'main'", automatic)
        self.assertIn("uses: ./.github/workflows/deploy-production.yml", automatic)

    def test_rc_configures_and_verifies_the_actual_containerd_store(self):
        workflow = (SCRIPTS.parent / '.github/workflows/release-pg03-gateway.yml').read_text()
        block = workflow.split('      - name: Match production containerd image store\n', 1)[1].split('      - name:', 1)[0]
        script = textwrap.dedent(block.split('        run: |\n', 1)[1])
        for invalid_json, correct_store in ((False, True), (True, True), (False, False)):
            with self.subTest(invalid_json=invalid_json, correct_store=correct_store), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                config = root / 'daemon.json'
                config.write_text('invalid-json' if invalid_json else json.dumps({'features': {'other-feature': True}, 'debug': False}))
                trace = root / 'restart'
                (root / 'sudo').write_text('#!/bin/sh\nexec "$@"\n')
                (root / 'systemctl').write_text('#!/bin/sh\nprintf restart > "$TEST_RESTART"\n')
                status = [['driver-type', 'io.containerd.snapshotter.v1']] if correct_store else []
                (root / 'docker').write_text('#!/bin/sh\nprintf \'%s\\n\' \'%s\'\n' % ('%s', json.dumps(status)))
                for name in ('sudo', 'systemctl', 'docker'): (root / name).chmod(0o755)
                invocation = script.replace('/etc/docker/daemon.json', str(config)).replace('/tmp/rc-image-store.json', str(root / 'store.json'))
                result = subprocess.run(['bash', '-c', invocation], capture_output=True, text=True,
                    env=dict(os.environ, PATH=str(root)+os.pathsep+os.environ['PATH'], TEST_RESTART=str(trace)))
                self.assertEqual(result.returncode == 0, not invalid_json and correct_store)
                if invalid_json:
                    self.assertFalse(trace.exists(), 'invalid configuration must not restart Docker')
                else:
                    self.assertTrue(trace.exists())
                    self.assertEqual(json.loads(config.read_text()), {'features': {'other-feature': True, 'containerd-snapshotter': True}, 'debug': False})

    def invoke(self, mode='pg03', failure='', transition='', confirmation='DEPLOY_PRODUCTION'):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            trace = root / 'trace.jsonl'
            ssh = root / 'ssh'
            ssh.write_text('''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
cmd=sys.argv[-1]
raw=sys.stdin.read()
if 'sha256sum' in cmd:
    name=cmd.split()[-1].split('/')[-1]
    if name=='clearapi-backup.sh': name='production-backup.sh'
    import hashlib
    print(hashlib.sha256((Path(os.environ['SCRIPTS'])/name).read_bytes()).hexdigest())
else:
    if 'prepare' in cmd:
        data=json.loads(raw)
        assert data['token']=='synthetic-ci-token'
        assert data['commit']==os.environ['EXPECTED_COMMIT']
    else:
        assert raw.strip()=='synthetic-ci-token'
    with open(os.environ['TRACE'],'a') as f: f.write(json.dumps(cmd)+'\\n')
    if os.environ['FAILURE'] in ('prepare','deploy') and (('prepare' in cmd)==(os.environ['FAILURE']=='prepare')): sys.exit(37)
''')
            ssh.chmod(0o755)
            commit = 'b' * 40
            git = root / 'git'
            git.write_text('#!/usr/bin/env python3\nimport os,sys\nif sys.argv[1:] == ["rev-parse", "HEAD"]: print(os.environ["EXPECTED_COMMIT"])\nelse: sys.exit(2)\n')
            git.chmod(0o755)
            env = dict(os.environ, PATH=str(root)+os.pathsep+os.environ['PATH'],
                       TRACE=str(trace), SCRIPTS=str(SCRIPTS), FAILURE=failure,
                       GITHUB_ENV=str(root/'env'), GITHUB_SHA=commit,
                       GITHUB_OUTPUT=str(root/'output'),
                       GITHUB_REPOSITORY='a3225865526-wq/Clear-API',
                       EXPECTED_COMMIT=commit, IMAGE_DIGEST='sha256:'+'a'*64,
                       GITHUB_RUN_ID='100', GITHUB_REF='refs/heads/main',
                       CONFIRMATION=confirmation, MIGRATION_COMPATIBLE='false', LEGACY_TRANSITION_SHA256=transition,
                       INPUT_DIGEST='sha256:'+'a'*64, INPUT_COMMIT=commit,
                       INPUT_POOL_MODE=mode, INPUT_RC_RUN_ID='99',
                       INPUT_TRANSITION_SHA256=transition, INPUT_MIGRATION='false',
                       INPUT_CONFIRMATION=confirmation,
                       REHEARSAL_RUN_ID='', PREVIOUS_SOURCE_COMMIT='', EXPECTED_EXECUTOR_REPOSITORY='',
                       EXPECTED_EXECUTOR_DIGEST='', EXPECTED_EXECUTOR_BINARY_SHA256='', EXPECTED_EXECUTOR_SOURCE_COMMIT='',
                       POOL_MODE=mode, RC_RUN_ID='99', PROD_SSH_ALIAS='isolated-vps',
                       GH_TOKEN='synthetic-ci-token', GH_PULL_TOKEN='synthetic-ci-token')
            selected = ['Validate manual gate inputs', 'Validate authorized release identity',
                        'Verify PG03 control tool and prepare approved pair',
                        'Run controlled deploy on VPS']
            for name in selected:
                if name.startswith('Verify PG03') and mode == 'off':
                    continue
                script = self.steps[name]['run']
                result = subprocess.run(['bash','-c',script],env=env,capture_output=True,text=True)
                self.assertNotIn('synthetic-ci-token', result.stdout+result.stderr)
                if result.returncode:
                    trace_text = trace.read_text() if trace.exists() else ''
                    return result.returncode, trace_text + result.stderr
                if name == 'Validate authorized release identity':
                    env.update(line.split('=',1) for line in (root/'env').read_text().splitlines())
            return result.returncode, trace.read_text()

    def test_pg03_and_pool_off_invoke_their_actual_wrappers(self):
        for mode in ('pg03','off'):
            with self.subTest(mode=mode):
                rc, trace = self.invoke(mode)
                self.assertEqual(rc,0,trace)
                if mode == 'pg03':
                    self.assertIn('production-deploy-v2.sh',trace)
                    self.assertIn('--pool-mode pg03',trace)
                    self.assertIn('pg03-release-control.py prepare',trace)
                else:
                    self.assertIn('production-deploy.sh',trace)
                    self.assertNotIn('prepare',trace)
                    self.assertNotIn('--pool-mode',trace)

    def test_failure_never_falls_back_to_old_wrapper(self):
        for failure in ('prepare','deploy'):
            rc, trace = self.invoke(failure=failure)
            self.assertEqual(rc,37)
            self.assertNotIn('/production-deploy.sh',trace)
            if failure == 'prepare':
                self.assertNotIn('production-deploy-v2.sh',trace)

    def test_unknown_mode_is_rejected_before_ssh(self):
        rc, trace = self.invoke('unknown')
        self.assertNotEqual(rc,0)
        self.assertEqual(trace,'')

    def test_maintenance_requires_its_exact_confirmation_and_cannot_use_pool_off(self):
        code, trace = self.invoke(transition='a'*64, confirmation='DEPLOY_LEGACY_RETIREMENT_V1')
        self.assertEqual(code, 0, trace)
        self.assertIn('--legacy-transition-v1', trace)
        for mode, sha, confirmation in [('off','a'*64,'DEPLOY_LEGACY_RETIREMENT_V1'),
                ('pg03','a'*64,'DEPLOY_PRODUCTION'), ('pg03','invalid','DEPLOY_LEGACY_RETIREMENT_V1')]:
            with self.subTest(mode=mode, sha=sha):
                code, trace = self.invoke(mode, transition=sha, confirmation=confirmation)
                self.assertNotEqual(code, 0)
                self.assertEqual(trace, '')


class ReleaseApprovalTest(unittest.TestCase):
    def setUp(self):
        self.data = {'run':'100','rc_run':'99','commit':'b'*40,'digest':'sha256:'+'a'*64}
        run = {'head_sha':'b'*40,'head_branch':'main','event':'workflow_dispatch',
               'status':'in_progress','path':'.github/workflows/deploy-production.yml',
               'repository':{'full_name':CONTROL.REPO}}
        rc = dict(run, id=99, status='completed', conclusion='success', run_attempt=1,
                  workflow_id=10, path='.github/workflows/release-pg03-gateway.yml',
                  repository={'id': 7, 'full_name': CONTROL.REPO},
                  head_repository={'id': 7, 'full_name': CONTROL.REPO})
        ci = dict(rc, id=98, workflow_id=11, event='push', path='.github/workflows/ci.yml')
        self.evidence = {'actions/runs/100': run,
            'git/ref/heads/main': {'object':{'sha':'b'*40}},
            'actions/runs/100/approvals':[{'state':'approved','user':{'login':'a3225865526-wq'},
                'environments':[{'id':23693288049,'name':'production'}]}],
            'actions/runs/99': rc, 'actions/runs/98': ci,
            'actions/workflows/release-pg03-gateway.yml': {
                'id': 10, 'path': rc['path'], 'state': 'active'},
            'actions/workflows/ci.yml': {'id': 11, 'path': ci['path'], 'state': 'active'},
            'actions/workflows/release-pg03-gateway.yml/runs?branch=main&head_sha='+'b'*40+'&per_page=1':
                {'workflow_runs': [{'id': 99, 'run_attempt': 1}]},
            'actions/workflows/ci.yml/runs?branch=main&event=push&head_sha='+'b'*40+'&per_page=1':
                {'workflow_runs': [{'id': 98, 'run_attempt': 1}]}}
        for ident in (98, 99):
            self.evidence[f'actions/runs/{ident}/attempts/1/jobs?per_page=100'] = {
                'total_count': 1, 'jobs': [{'status': 'completed', 'conclusion': 'success'}]}

    def test_only_real_same_source_production_review_approves(self):
        reviewers,_ = CONTROL.approved_run(self.data,self.evidence.__getitem__)
        self.assertEqual(reviewers,['a3225865526-wq'])
        cases = [('actions/runs/100','status','waiting'),
                 ('actions/runs/100','status','completed'),
                 ('actions/runs/100','head_branch','unmerged'),
                 ('actions/runs/99','conclusion','failure'),
                 ('actions/runs/99','head_sha','c'*40)]
        for path,field,value in cases:
            evidence=copy.deepcopy(self.evidence); evidence[path][field]=value
            with self.subTest(field=field,value=value), self.assertRaises(ValueError):
                CONTROL.approved_run(self.data,evidence.__getitem__)
        for reviews in ([],[{'state':'rejected'}],
                        [{'state':'approved','user':{'login':'not-reviewer'},'environments':[]}],
                        [{'state':'approved','user':{'login':'a3225865526-wq'},'environments':[{'id':1,'name':'staging'}]}]):
            evidence=copy.deepcopy(self.evidence);evidence['actions/runs/100/approvals']=reviews
            with self.subTest(reviews=reviews),self.assertRaises(ValueError):
                CONTROL.approved_run(self.data,evidence.__getitem__)

    def manifest_fixture(self):
        return {'source_commit':'b'*40,'candidate_commit':'b'*40,'oci_revision':'b'*40,'workflow_commit':'b'*40,
           'source_repository':'https://github.com/'+CONTROL.REPO,'workflow_run':'99',
           'workflow_ref':CONTROL.REPO+'/.github/workflows/release-pg03-gateway.yml@refs/heads/main',
           'workflow_run_attempt':'1','image_digest':self.data['digest'],
           'image_repository':CONTROL.IMAGE,
           'image_ref':CONTROL.IMAGE+'@'+self.data['digest'],'platform':'linux/amd64',
           'deployment_compatible':True,'missing_deployment_binaries':[],
           'new_api_image_id':'sha256:'+'c'*64,'binary_sha256':'d'*64,
           'runtime_identity':{'image_id':'sha256:'+'c'*64,'binary_path':'/new-api','binary_sha256':'d'*64},
           'source_ci_runs':[{'databaseId':98,'headSha':'b'*40,'status':'completed','conclusion':'success'}],
           'verification':{k:'passed' for k in ('source_commit','targeted_accountpool_routechain_tests',
               'deployment_binaries','platform','oci_revision','digest_pull','binary_roundtrip')}}

    def artifact_fixture(self, manifest, corrupt_inventory=False):
        files = {'gateway-image-release.json': json.dumps(manifest).encode(),
                 'binary-build-info.txt': b'synthetic build info',
                 'schema-rehearsal.json': b'{}', 'schema-rehearsal.target.sql': b'-- synthetic schema'}
        inventory = {name: hashlib.sha256(raw).hexdigest() for name, raw in files.items()}
        if corrupt_inventory:
            inventory['gateway-image-release.json'] = '0' * 64
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, 'w') as z:
            for name, raw in files.items():
                z.writestr(name, raw)
            z.writestr('artifact-hashes.json', json.dumps(inventory))
        raw = archive.getvalue()
        artifact = {'name': 'pg03-gateway-release-99-1', 'expired': False, 'id': 123,
                    'digest': 'sha256:' + hashlib.sha256(raw).hexdigest(),
                    'workflow_run': {'id': 99, 'head_sha': 'b' * 40, 'head_branch': 'main',
                                     'repository_id': 7, 'head_repository_id': 7}}
        return artifact, raw

    def test_rc_artifact_digest_revision_and_attempt_must_match(self):
        for field,value in [(None,None),('image_digest','sha256:'+'e'*64),
                            ('oci_revision','f'*40),('workflow_run_attempt','2'),
                            ('workflow_run','100'), ('workflow_commit','f'*40),
                            ('candidate_commit','f'*40), ('source_commit','f'*40),
                            ('workflow_ref','foreign/workflow'), ('image_repository','foreign/image'),
                            ('runtime_identity',{}), ('source_ci_runs',[]),
                            ('deployment_compatible',False)]:
            fixture=self.manifest_fixture()
            if field: fixture[field]=value
            artifact, raw = self.artifact_fixture(fixture)
            rc = CONTROL.approved_rc(self.data, self.evidence.__getitem__)
            call=lambda: CONTROL.release_manifest(self.data,rc,
                {'artifacts':[artifact]},lambda _:raw)
            if field:
                with self.subTest(field=field),self.assertRaises(ValueError):call()
            else:self.assertEqual(call()[0]['image_digest'],self.data['digest'])

    def test_dispatch_and_automatic_rc_use_the_same_identity_and_artifact_checks(self):
        for event in ('workflow_dispatch', 'workflow_run'):
            with self.subTest(event=event):
                self.evidence['actions/runs/99']['event'] = event
                rc = CONTROL.approved_rc(self.data, self.evidence.__getitem__)
                artifact, raw = self.artifact_fixture(self.manifest_fixture())
                manifest, _ = CONTROL.release_manifest(self.data, rc, {'artifacts': [artifact]}, lambda _: raw)
                self.assertEqual(manifest['image_digest'], self.data['digest'])

    def test_untrusted_failed_stale_or_forged_rc_is_rejected(self):
        cases = [('id', 101), ('event', 'pull_request'), ('head_branch', 'feature'),
                 ('conclusion', 'failure'), ('conclusion', 'cancelled'), ('conclusion', 'skipped'),
                 ('status', 'in_progress'), ('head_sha', 'f' * 40), ('workflow_id', 12),
                 ('path', '.github/workflows/fake.yml'), ('run_attempt', 0),
                 ('repository', {'id': 7, 'full_name': 'foreign/repo'}),
                 ('head_repository', {'id': 8, 'full_name': CONTROL.REPO}),
                 ('head_repository', {'id': 7, 'full_name': 'foreign/fork'})]
        for event in ('workflow_dispatch', 'workflow_run'):
            for field, value in cases:
                rejected = copy.deepcopy(self.evidence)
                rejected['actions/runs/99']['event'] = event
                rejected['actions/runs/99'][field] = value
                with self.subTest(event=event, field=field, value=value), self.assertRaises(ValueError):
                    CONTROL.approved_rc(self.data, rejected.__getitem__)
        for suffix, value in [('main', 'f' * 40), ('run', 101), ('attempt', 2)]:
            rejected = copy.deepcopy(self.evidence)
            if suffix == 'main':
                rejected['git/ref/heads/main']['object']['sha'] = value
            else:
                latest = rejected['actions/workflows/release-pg03-gateway.yml/runs?branch=main&head_sha='+'b'*40+'&per_page=1']['workflow_runs'][0]
                latest['id' if suffix == 'run' else 'run_attempt'] = value
            with self.subTest(stale=suffix), self.assertRaises(ValueError):
                CONTROL.approved_rc(self.data, rejected.__getitem__)

    def test_automatic_rc_requires_trusted_latest_push_ci_and_complete_jobs(self):
        for field, value in [('event', 'pull_request'), ('head_branch', 'feature'),
                             ('head_sha', 'f' * 40), ('status', 'in_progress'),
                             ('conclusion', 'failure'), ('workflow_id', 12), ('run_attempt', 2),
                             ('head_repository', {'id': 7, 'full_name': 'foreign/fork'})]:
            rejected = copy.deepcopy(self.evidence)
            rejected['actions/runs/99']['event'] = 'workflow_run'
            rejected['actions/runs/98'][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                CONTROL.approved_rc(self.data, rejected.__getitem__)
        for ident in (98, 99):
            for jobs in ({'total_count': 0, 'jobs': []}, {'total_count': 101, 'jobs': []},
                         {'total_count': 1, 'jobs': [{'status': 'completed', 'conclusion': 'skipped'}]},
                         {'total_count': 1, 'jobs': [{'status': 'completed', 'conclusion': 'failure'}]}):
                rejected = copy.deepcopy(self.evidence)
                rejected[f'actions/runs/{ident}/attempts/1/jobs?per_page=100'] = jobs
                with self.subTest(ident=ident, jobs=jobs), self.assertRaises(ValueError):
                    CONTROL.approved_rc(self.data, rejected.__getitem__)

    def test_expired_foreign_duplicate_or_corrupt_artifact_is_rejected(self):
        rc = CONTROL.approved_rc(self.data, self.evidence.__getitem__)
        for change in ('expired', 'duplicate', 'producer', 'repository', 'source', 'digest', 'inventory', 'attempt', 'duplicate_entry'):
            artifact, raw = self.artifact_fixture(self.manifest_fixture(), corrupt_inventory=change == 'inventory')
            if change == 'expired': artifact['expired'] = True
            if change == 'producer': artifact['workflow_run']['id'] = 101
            if change == 'repository': artifact['workflow_run']['head_repository_id'] = 8
            if change == 'source': artifact['workflow_run']['head_sha'] = 'f' * 40
            if change == 'digest': artifact['digest'] = 'sha256:' + '0' * 64
            if change == 'attempt': artifact['name'] = 'pg03-gateway-release-99-2'
            if change == 'duplicate_entry':
                stream = io.BytesIO(raw)
                with zipfile.ZipFile(stream, 'a') as z:
                    z.writestr('unexpected.json', '{}')
                raw = stream.getvalue()
                artifact['digest'] = 'sha256:' + hashlib.sha256(raw).hexdigest()
            artifacts = [artifact, artifact] if change == 'duplicate' else [artifact]
            with self.subTest(change=change), self.assertRaises(ValueError):
                CONTROL.release_manifest(self.data, rc, {'artifacts': artifacts}, lambda _: raw)

    def test_owner_dispatch_without_review_requires_the_actual_automatic_policy(self):
        evidence = copy.deepcopy(self.evidence)
        evidence['actions/runs/100/approvals'] = []
        run = evidence['actions/runs/100']
        run['actor'] = run['triggering_actor'] = {'login': 'a3225865526-wq'}
        evidence['environments/production'] = {
            'id': 23693288049, 'name': 'production',
            'deployment_branch_policy': {'protected_branches': False, 'custom_branch_policies': True},
            'protection_rules': [{'type': 'branch_policy'}]}
        evidence['environments/production/deployment-branch-policies'] = {
            'branch_policies': [{'name': 'main', 'type': 'branch'}]}
        reviewers, _ = CONTROL.approved_run(self.data, evidence.__getitem__)
        self.assertEqual(reviewers, [], 'automatic authority must not fabricate a reviewer')
        cases = [
            ('actions/runs/100', 'actor', {'login': 'other'}),
            ('actions/runs/100', 'triggering_actor', {'login': 'other'}),
            ('environments/production', 'id', 1),
            ('environments/production', 'protection_rules', [{'type': 'required_reviewers'}]),
            ('environments/production', 'protection_rules', [{'type': 'wait_timer'}]),
            ('environments/production/deployment-branch-policies', 'branch_policies', [{'name': '*', 'type': 'branch'}]),
            ('actions/runs/99', 'head_sha', 'c' * 40),
            ('actions/runs/99', 'conclusion', 'failure'),
        ]
        for path, field, value in cases:
            rejected = copy.deepcopy(evidence)
            rejected[path][field] = value
            with self.subTest(path=path, field=field), self.assertRaises(ValueError):
                CONTROL.approved_run(self.data, rejected.__getitem__)
        evidence['actions/runs/100/approvals'] = [{'state': 'rejected'}]
        with self.assertRaises(ValueError):
            CONTROL.approved_run(self.data, evidence.__getitem__)

    def test_automatic_workflow_requires_authenticated_job_and_paired_executor(self):
        data = dict(self.data, rehearsal_run='101',
            executor_repository=CONTROL.EXECUTOR.split('@', 1)[0],
            executor_digest=CONTROL.EXECUTOR.split('@', 1)[1],
            executor_binary_sha256=CONTROL.EXECUTOR_BINARY,
            executor_source_commit=CONTROL.EXECUTOR_REVISION)
        evidence = copy.deepcopy(self.evidence)
        run = evidence['actions/runs/100']
        run.update(event='workflow_run', path='.github/workflows/automatic-deploy.yml', workflow_id=12)
        evidence['actions/workflows/automatic-deploy.yml'] = {
            'id': 12, 'path': '.github/workflows/automatic-deploy.yml', 'state': 'active'}
        evidence['actions/variables/CLEARAPI_AUTO_PRODUCTION_ENABLED'] = {
            'name': 'CLEARAPI_AUTO_PRODUCTION_ENABLED', 'value': 'true'}
        evidence['environments/production'] = {
            'id': 23693288049, 'name': 'production',
            'deployment_branch_policy': {'protected_branches': False, 'custom_branch_policies': True},
            'protection_rules': [{'type': 'branch_policy'}]}
        evidence['environments/production/deployment-branch-policies'] = {
            'branch_policies': [{'name': 'main', 'type': 'branch'}]}
        evidence['actions/runs/101'] = {
            'id': 101, 'path': '.github/workflows/deployment-rehearsal.yml',
            'event': 'workflow_run', 'head_sha': 'b' * 40, 'head_branch': 'main',
            'status': 'completed', 'conclusion': 'success', 'run_attempt': 1,
            'repository': {'full_name': CONTROL.REPO},
            'head_repository': {'full_name': CONTROL.REPO}}
        evidence['actions/runs/100/jobs?per_page=100'] = {'jobs': [{
            'name': 'deploy / Validate release authorization before production credentials',
            'status': 'completed', 'conclusion': 'success', 'steps': [{
                'name': 'Authenticate automatic proof before production credentials',
                'status': 'completed', 'conclusion': 'success'}]}]}
        reviewers, _ = CONTROL.approved_run(data, evidence.__getitem__)
        self.assertEqual(reviewers, [], 'proof authorization must not fabricate a reviewer')

        for path, field, value in (
            ('actions/variables/CLEARAPI_AUTO_PRODUCTION_ENABLED', 'value', 'false'),
            ('actions/runs/101', 'conclusion', 'failure'),
            ('actions/runs/100/jobs?per_page=100', 'jobs', []),
        ):
            rejected = copy.deepcopy(evidence)
            rejected[path][field] = value
            with self.subTest(path=path, field=field), self.assertRaises(ValueError):
                CONTROL.approved_run(data, rejected.__getitem__)
        rejected_data = dict(data, executor_digest='sha256:' + '0' * 64)
        with self.assertRaisesRegex(ValueError, 'Executor'):
            CONTROL.approved_run(rejected_data, evidence.__getitem__)

    @unittest.skipUnless(os.geteuid()==0,'secure root file ownership requires isolated root runner')
    def test_existing_target_and_symlink_are_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            target=Path(directory)/'target.json'
            CONTROL.install_target(target,b'approved-evidence')
            self.assertEqual(target.stat().st_mode & 0o777,0o600)
            CONTROL.install_target(target,b'approved-evidence')
            with self.assertRaises(ValueError):CONTROL.install_target(target,b'new')
            self.assertEqual(target.read_bytes(),b'approved-evidence')
            link=Path(directory)/'link';link.symlink_to(target)
            with self.assertRaises(OSError):CONTROL.install_target(link,b'new')

    @unittest.skipUnless(os.geteuid()==0,'root entry point contract')
    def test_unexpected_library_exception_cannot_print_the_credential(self):
        # Even a library ValueError containing a credential must be redacted.
        script = '''import runpy,sys,urllib.request
def fail(*args,**kwargs):raise ValueError('synthetic-sensitive-token')
urllib.request.build_opener=fail
sys.argv=[sys.argv[1],'prepare']
runpy.run_path(sys.argv[0],run_name='__main__')
'''
        payload=dict(self.data,token='synthetic-sensitive-token')
        r=subprocess.run(['python3','-c',script,str(SCRIPTS/'pg03-release-control.py')],
            input=json.dumps(payload),capture_output=True,text=True)
        self.assertNotEqual(r.returncode,0)
        self.assertNotIn('synthetic-sensitive-token',r.stdout+r.stderr)
        self.assertIn('BLOCKED',r.stderr)


class ProbeSessionTest(unittest.TestCase):
    def response(self, expiry):
        claims=base64.urlsafe_b64encode(json.dumps({'exp':expiry}).encode()).decode().rstrip('=')
        return json.dumps({'success':True,'data':{'token_type':'Bearer',
            'access_token':'header.'+claims+'.synthetic_signature','access_expires_at':expiry}})

    def test_completed_fresh_login_required(self):
        self.assertTrue(PROBE.login_access(self.response(1900),1000))
        for raw in (self.response(1000),self.response(1050),'{"success":true,"data":{"flow_token":"pending"}}','{}'):
            with self.subTest(raw=raw),self.assertRaises(ValueError):PROBE.login_access(raw,1000)

    def test_live_server_rejection_and_container_mismatch_fail_closed(self):
        class Response:
            status=200
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,_):return json.dumps({'success':True,'data':{
                'status':'PASS','schema_version':'1','report_version':'1',
                'container_id':'current','started_at':'now','build_identity':'sha256:binary'}}).encode()
        class Opener:
            def open(self,request,timeout):return Response()
        container={'Id':'current','State':{'StartedAt':'now'}}
        self.assertEqual(PROBE.live_report('synthetic',container,'binary',Opener())['status'],'PASS')
        with self.assertRaises(ValueError):PROBE.live_report('synthetic',{'Id':'other','State':{'StartedAt':'now'}},'binary',Opener())
        class Denied:
            def open(self,*args,**kwargs):raise urllib.error.HTTPError('local',401,'denied',{},None)
        with self.assertRaises(urllib.error.HTTPError):PROBE.live_report('synthetic',container,'binary',Denied())

    def test_operator_login_preserves_encryption_and_required_mfa(self):
        calls=[]
        final=json.loads(self.response(1900))
        bodies=[{'success':True,'data':{'enabled':True,'kid':'synthetic-public-key','public_key':'public-only'}},
                {'success':True,'data':{'require_verification':True,'flow_token':'synthetic-flow',
                    'methods':[{'method':'2fa','available':True}]}},final]
        class Response:
            status=200
            def __init__(self,body):self.body=body
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,_):return json.dumps(self.body).encode()
        class Opener:
            def open(self,request,timeout):
                calls.append((request.full_url,json.loads(request.data) if request.data else None))
                return Response(bodies[len(calls)-1])
        secrets=iter(['synthetic-password','123456'])
        result=PROBE.interactive_login(Opener(),lambda _:'synthetic-root',
            lambda _:next(secrets),lambda password,key:'encrypted-fixture')
        self.assertEqual(json.loads(result),final)
        self.assertNotIn('password',calls[1][1])
        self.assertEqual(calls[1][1]['password_encrypted'],'encrypted-fixture')
        self.assertEqual(calls[2][1],{'flow_token':'synthetic-flow','method':'2fa','code':'123456'})
        calls.clear();bodies[1]['data']['methods']=[{'method':'passkey','available':True}]
        with self.assertRaises(ValueError):
            PROBE.interactive_login(Opener(),lambda _:'synthetic-root',lambda _:'synthetic-password',lambda *_:'encrypted')
        self.assertEqual(len(calls),2,'unsupported proof must not issue an unverified session')

    def test_rsa_oaep_transport_round_trip_uses_no_password_argv(self):
        with tempfile.TemporaryDirectory() as directory:
            private=Path(directory)/'test-only.pem'
            subprocess.run(['openssl','genpkey','-algorithm','RSA','-pkeyopt','rsa_keygen_bits:2048',
                '-out',str(private)],check=True,capture_output=True)
            public=subprocess.run(['openssl','pkey','-in',str(private),'-pubout'],check=True,capture_output=True).stdout.decode()
            encrypted=PROBE.encrypt_password('synthetic-test-password',public)
            result=subprocess.run(['openssl','pkeyutl','-decrypt','-inkey',str(private),
                '-pkeyopt','rsa_padding_mode:oaep','-pkeyopt','rsa_oaep_md:sha256',
                '-pkeyopt','rsa_mgf1_md:sha256'],input=base64.b64decode(encrypted),check=True,capture_output=True)
            self.assertEqual(result.stdout,b'synthetic-test-password')


if __name__=='__main__':
    unittest.main(verbosity=2)
