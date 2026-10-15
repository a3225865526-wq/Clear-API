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


SCRIPTS = Path(__file__).resolve().parents[1]


def module(name):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / (name + '.py'))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


CONTROL = module('pg03-release-control')
PROBE = module('runtime-probe-session')


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

    def invoke(self, mode='pg03', failure=''):
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
            env = dict(os.environ, PATH=str(root)+os.pathsep+os.environ['PATH'],
                       TRACE=str(trace), SCRIPTS=str(SCRIPTS), FAILURE=failure,
                       GITHUB_ENV=str(root/'env'), GITHUB_SHA='b'*40,
                       EXPECTED_COMMIT='b'*40, IMAGE_DIGEST='sha256:'+'a'*64,
                       GITHUB_RUN_ID='100', GITHUB_REF='refs/heads/main',
                       CONFIRMATION='DEPLOY_PRODUCTION', MIGRATION_COMPATIBLE='false',
                       POOL_MODE=mode, RC_RUN_ID='99', PROD_SSH_ALIAS='isolated-vps',
                       GH_TOKEN='synthetic-ci-token', GH_PULL_TOKEN='synthetic-ci-token')
            selected = ['Validate manual gate inputs', 'Verify PG03 control tool and prepare approved pair',
                        'Run controlled deploy on VPS']
            for name in selected:
                if name.startswith('Verify PG03') and mode == 'off':
                    continue
                script = self.steps[name]['run']
                result = subprocess.run(['bash','-c',script],env=env,capture_output=True,text=True)
                self.assertNotIn('synthetic-ci-token', result.stdout+result.stderr)
                if result.returncode:
                    return result.returncode, trace.read_text() if trace.exists() else ''
                if name == selected[0]:
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


class ReleaseApprovalTest(unittest.TestCase):
    def setUp(self):
        self.data = {'run':'100','rc_run':'99','commit':'b'*40,'digest':'sha256:'+'a'*64}
        run = {'head_sha':'b'*40,'head_branch':'main','event':'workflow_dispatch',
               'status':'in_progress','path':'.github/workflows/deploy-production.yml',
               'repository':{'full_name':CONTROL.REPO}}
        self.evidence = {'actions/runs/100': run,
            'git/ref/heads/main': {'object':{'sha':'b'*40}},
            'actions/runs/100/approvals':[{'state':'approved','user':{'login':'a3225865526-wq'},
                'environments':[{'id':23693288049,'name':'production'}]}],
            'actions/runs/99':dict(run,status='completed',conclusion='success',run_attempt=1,
                                 path='.github/workflows/release-pg03-gateway.yml'),
            'actions/workflows/ci.yml/runs?event=push&head_sha='+'b'*40+'&per_page=100':
                {'workflow_runs':[{'head_sha':'b'*40,'head_branch':'main','status':'completed','conclusion':'success'}]}}

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

    def test_rc_artifact_digest_revision_and_attempt_must_match(self):
        m={'source_commit':'b'*40,'oci_revision':'b'*40,'workflow_commit':'b'*40,
           'source_repository':'https://github.com/'+CONTROL.REPO,'workflow_run':'99',
           'workflow_run_attempt':'1','image_digest':self.data['digest'],
           'image_ref':CONTROL.IMAGE+'@'+self.data['digest'],'platform':'linux/amd64',
           'deployment_compatible':True,'missing_deployment_binaries':[],
           'new_api_image_id':'sha256:'+'c'*64,'binary_sha256':'d'*64,
           'verification':{k:'passed' for k in ('source_commit','targeted_accountpool_routechain_tests',
               'deployment_binaries','platform','oci_revision','digest_pull','binary_roundtrip')}}
        for field,value in [(None,None),('image_digest','sha256:'+'e'*64),
                            ('oci_revision','f'*40),('workflow_run_attempt','2'),
                            ('deployment_compatible',False)]:
            fixture=dict(m)
            if field: fixture[field]=value
            archive=io.BytesIO()
            with zipfile.ZipFile(archive,'w') as z:z.writestr('gateway-image-release.json',json.dumps(fixture))
            call=lambda: CONTROL.release_manifest(self.data,{'run_attempt':1},
                {'artifacts':[{'name':'pg03-gateway-release-99-1','expired':False,'id':123}]},lambda _:archive.getvalue())
            if field:
                with self.subTest(field=field),self.assertRaises(ValueError):call()
            else:self.assertEqual(call()[0]['image_digest'],self.data['digest'])

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
