"""No network, Docker, GHCR, VPS or real file outside an isolated tempdir."""
import contextlib
import fcntl
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch

SRC=Path(__file__).resolve().parents[1]/'scripts'/'local-deploy-vps.py'
spec=importlib.util.spec_from_file_location('clearapi_local_vps',SRC)
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
OLD='1'*40
NEW='2'*40
OLDID='sha256:'+'a'*64
NEWID='sha256:'+'b'*64
OLDDIGEST='sha256:'+'c'*64
NEWDIGEST='sha256:'+'d'*64
OLDREF=m.REGISTRY+'@'+OLDDIGEST
NEWREF=m.REGISTRY+'@'+NEWDIGEST

class MockDeploy(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base=Path(self.tmp.name)
        self.app=self.base/'app'
        self.app.mkdir()
        self.override=self.app/'compose-candidate.yaml'
        self.override.write_bytes(m.override_yaml({'service':'new-api','runtime_identity_dir':'/opt/clearapi/runtime/new-api'},OLDREF))
        self.manifest=self.app/'release-manifest.json'
        self.original={'schema_version':1,'releases':{'current':{
            'status':'success','candidate_commit':OLD,'new_api_image_id':OLDID,
            'image_digest':OLDDIGEST,'image_ref':OLDREF},'previous':{}}}
        self.manifest.write_text(json.dumps(self.original))
        self.c={'app_dir':str(self.app),'service':'new-api',
                'runtime_identity_dir':'/opt/clearapi/runtime/new-api',
                'local_url':'http://127.0.0.1:3000/api/status',
                'public_url':'https://www.clear999.com/api/status'}
        self.a=types.SimpleNamespace(commit=NEW,image_id=NEWID,image_digest=NEWDIGEST,
                                     previous_image_id=OLDID,scope='frontend')
        self.old={'commit':OLD,'image_id':OLDID,'image_ref':OLDREF,'healthy':True}
        self.command_log=[]
        self.fail_health=False
        self.fail_rollback=False
        self.patches=[
            patch.object(m,'LOCK',self.base/'lock'),
            patch.object(m,'preflight',side_effect=lambda _:dict(self.old)),
            patch.object(m,'pull_candidate',return_value=None),
            patch.object(m,'verify_override_scope',return_value=None),
            patch.object(m,'run',side_effect=self.fake_run),
            patch.object(m,'compose_args',return_value=['docker','compose']),
            patch.object(m,'deployed',side_effect=self.fake_deployed),
            patch.object(m,'verify_database',return_value=None),
            patch.object(m,'probe',side_effect=self.fake_probe),
            patch.object(m,'identity_prepare',return_value=None),
            patch.object(m,'identity_refresh',return_value=None),
            patch.object(m,'verify_manifest',return_value=self.original)]
        for pt in self.patches:pt.start();self.addCleanup(pt.stop)

    def fake_run(self,*args,**kwargs):
        self.command_log.append(args)
        if self.fail_rollback and args[:3]==('docker','compose','up') and OLDREF in self.override.read_text():
            raise m.DeployError('mock compose rollback failed')
        return ''
    def fake_deployed(self,_):
        return {'image_id':NEWID,'commit':NEW,'image_ref':NEWREF} if NEWREF in self.override.read_text() else dict(self.old)
    def fake_probe(self,_):
        if self.fail_health and NEWREF in self.override.read_text():
            raise m.DeployError('mock after-switch health failed')

    def test_frontend_success(self):
        out=io.StringIO()
        with contextlib.redirect_stdout(out):m.apply(self.c,self.a)
        r=json.loads(out.getvalue())
        self.assertEqual(r['repo_digest'],NEWDIGEST)
        self.assertEqual(r['result'],'success')
        self.assertEqual(json.loads(self.manifest.read_text())['releases']['current']['image_digest'],NEWDIGEST)
        self.assertEqual(json.loads(self.manifest.read_text())['releases']['current']['status'],'success')
        self.assertIn(NEWREF,self.override.read_text())
        self.assertEqual(sum(1 for args in self.command_log if args[:3]==('docker','compose','up')),1)

    def test_frontend_failed_health_restores_both_manifest_and_override(self):
        self.fail_health=True
        with self.assertRaisesRegex(m.DeployError,'previous exact image restored and healthy'):
            m.apply(self.c,self.a)
        self.assertEqual(json.loads(self.manifest.read_text()),self.original)
        self.assertIn(OLDREF,self.override.read_text())
        self.assertEqual(sum(1 for args in self.command_log if args[:3]==('docker','compose','up')),2)

    def test_failed_rollback_reports_incident(self):
        self.fail_health=True
        self.fail_rollback=True
        with self.assertRaisesRegex(m.DeployError,'ROLLBACK FAILED'):
            m.apply(self.c,self.a)

    def test_backend_failure_no_auto_rollback(self):
        self.a.scope='backend';self.fail_health=True
        with patch.object(m,'backup',return_value='20261023T120000Z'):
            with self.assertRaisesRegex(m.DeployError,'MANUAL RECOVERY'):
                m.apply(self.c,self.a)
        self.assertIn(NEWREF,self.override.read_text())
        self.assertEqual(json.loads(self.manifest.read_text())['releases']['current']['status'],'pending')
        self.assertEqual(sum(1 for args in self.command_log if args[:3]==('docker','compose','up')),1)

    def test_missing_backup_blocks_before_switch(self):
        self.a.scope='backend'
        with patch.object(m,'backup',side_effect=m.DeployError('no backup evidence')):
            with self.assertRaisesRegex(m.DeployError,'no backup evidence'):
                m.apply(self.c,self.a)
        self.assertIn(OLDREF,self.override.read_text())
        self.assertEqual(self.command_log,[])

    def test_predecessor_mismatch(self):
        self.a.previous_image_id='sha256:'+'e'*64
        with self.assertRaisesRegex(m.DeployError,'predecessor changed'):
            m.apply(self.c,self.a)
        self.assertEqual(self.command_log,[])

    def test_lock_conflict(self):
        with (self.base/'lock').open('a+b') as lock:
            fcntl.flock(lock.fileno(),fcntl.LOCK_EX|fcntl.LOCK_NB)
            with self.assertRaisesRegex(m.DeployError,'holds the lock'):
                m.apply(self.c,self.a)
        self.assertEqual(self.command_log,[])

    def test_bad_digest(self):
        self.a.image_digest='sha256:invalid'
        with self.assertRaisesRegex(m.DeployError,'invalid candidate image identity'):
            m.apply(self.c,self.a)

    def test_registry_verification_failure_blocks_without_compose(self):
        with patch.object(m,'pull_candidate',side_effect=m.DeployError('RepoDigest mismatch')):
            with self.assertRaisesRegex(m.DeployError,'RepoDigest mismatch'):
                m.apply(self.c,self.a)
        self.assertIn(OLDREF,self.override.read_text())
        self.assertEqual(self.command_log,[])

class Security(unittest.TestCase):
    def test_override_preview_preserves_non_image_configuration(self):
        with tempfile.TemporaryDirectory() as folder:
            c={'app_dir':folder,'compose_file':'docker-compose.prod.yml',
               'service':'new-api','runtime_identity_dir':'/opt/clearapi/runtime/new-api'}
            old={'services':{'new-api':{'image':OLDREF,'environment':{'REGISTER':'true'}},
                             'postgres':{'image':'postgres:17'}}}
            candidate=json.loads(json.dumps(old))
            candidate['services']['new-api']['image']=NEWREF
            with patch.object(m,'compose_args',return_value=['docker','compose']), \
                 patch.object(m,'run',side_effect=[json.dumps(old),json.dumps(candidate)]):
                m.verify_override_scope(c,NEWREF)
            candidate['services']['new-api']['environment']['REGISTER']='false'
            with patch.object(m,'compose_args',return_value=['docker','compose']), \
                 patch.object(m,'run',side_effect=[json.dumps(old),json.dumps(candidate)]):
                with self.assertRaisesRegex(m.DeployError,'non-image configuration'):
                    m.verify_override_scope(c,NEWREF)
            self.assertEqual(list(Path(folder).iterdir()),[])

    def test_no_local_tag_identity_allowed(self):
        with self.assertRaisesRegex(m.DeployError,'invalid fixed GHCR'):
            m.override_yaml({'service':'new-api','runtime_identity_dir':'/opt/clearapi/runtime/new-api'},'clearapi-local:'+OLD)
    def test_registry_name_locked(self):
        with self.assertRaises(m.DeployError):
            m.override_yaml({'service':'new-api','runtime_identity_dir':'/opt/clearapi/runtime/new-api'},'ghcr.io/attacker/evil@'+NEWDIGEST)
    def test_manifest_new_identity_preserves_predecessor(self):
        old={'image_id':OLDID}
        oldman={'releases':{'current':{'status':'success','candidate_commit':OLD,'image_digest':OLDDIGEST},'previous':{}}}
        candidate=json.loads(m.manifest_next(oldman,old,NEW,NEWREF,NEWID,'pending'))
        self.assertEqual(candidate['releases']['previous'],oldman['releases']['current'])
        self.assertEqual(candidate['releases']['current']['new_api_image_id'],NEWID)
        self.assertEqual(candidate['releases']['current']['status'],'pending')
    def test_identity_input_validation(self):
        self.assertFalse(m.COMMIT.fullmatch('a'*39+';'))
        self.assertFalse(m.SHA.fullmatch('sha256:BAD'))

if __name__=='__main__':unittest.main()
