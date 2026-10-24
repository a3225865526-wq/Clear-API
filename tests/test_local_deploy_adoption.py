"""Focused root-only adoption contracts; temporary files and controlled commands."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

SCRIPTS=Path(__file__).resolve().parents[1]/'scripts'


def load(name):
    spec=importlib.util.spec_from_file_location(name,SCRIPTS/(name+'.py'))
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module


ADAPTER=load('local-deploy-vps')
BOOT=load('runtime-boot-recovery')
BACKUP=load('backup-identity')
RECOVERY=load('pg03-failed-recovery')


@unittest.skipUnless(hasattr(os,'geteuid') and os.geteuid()==0,'Linux root fixture required')
class AdoptionTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name);self.app=self.root/'app';self.app.mkdir(mode=0o700)
        self.state=ADAPTER.STATE
        for name in {*self.state.FROZEN,*self.state.BASE_FILES}:
            p=self.app/name;p.write_text(json.dumps({'fixture':name,'status':'failed'}));p.chmod(0o600)
        self.env=self.state.environment_file(self.app);self.env.parent.mkdir(parents=True)
        self.env.write_text('CLEARAPI_POOL_EXECUTOR_ID=synthetic\n');self.env.chmod(0o600)
        self.frozen={n:hashlib.sha256((self.app/n).read_bytes()).hexdigest() for n in self.state.FROZEN}
        self.original={n:(self.app/n).read_bytes() for n in self.state.FROZEN}
        self.addCleanup(patch.stopall)
        patch.object(self.state,'FROZEN',self.frozen).start()
        patch.object(BOOT.STATE,'FROZEN',self.frozen).start()
        patch.object(ADAPTER,'LOCK',self.root/'deploy.lock').start()
        patch.object(ADAPTER,'TEST_CONFIG',None).start()
        self.rev='1'*40;self.ref='ghcr.io/a3225865526-wq/clearapi@sha256:'+'2'*64
        self.engine='sha256:'+'3'*64;self.cid='4'*64
        self.config=b'{"architecture":"amd64","fixture":"adopted-config"}'
        self.config_id='sha256:'+hashlib.sha256(self.config).hexdigest()
        self.before={'gateway':{'container_id':self.cid,'image_ref':self.ref,'image_id':self.engine,
                     'commit':self.rev,'started_at':'2026-01-01T00:00:00Z'},'shared':{},
                     'compose_hash':'5'*64,'compose_sha256':{n:hashlib.sha256((self.app/n).read_bytes()).hexdigest()
                         for n in self.state.BASE_FILES},'environment_sha256':hashlib.sha256(self.env.read_bytes()).hexdigest()}
        self.source='6'*40

    def adopt(self, execute=True, drift=False):
        edge=Mock();module=types.SimpleNamespace(Recovery=lambda *a:None,
            WebsiteRestoration=lambda **kw:types.SimpleNamespace(edge=edge),TRAFFIC=Mock(),
            FAILED_RUN='39347012653',DIGEST='sha256:'+'a'*64,COMMIT='b'*40)
        spec=types.SimpleNamespace(loader=types.SimpleNamespace(exec_module=lambda m:None))
        run=Mock(side_effect=['{}','{"drift":true}' if drift else '{}'])
        with patch.object(ADAPTER,'adoption_snapshot',return_value=self.before), \
                patch.object(ADAPTER,'image_id',return_value=self.config_id),patch.object(ADAPTER,'run',run), \
                patch.object(ADAPTER.importlib.util,'spec_from_file_location',return_value=spec), \
                patch.object(ADAPTER.importlib.util,'module_from_spec',return_value=module):
            result=ADAPTER.adopt_running(self.app,self.source,execute)
        return result,run,edge

    def assert_history(self):
        self.assertEqual(self.original,{n:(self.app/n).read_bytes() for n in self.original})

    def test_adoption_preserves_failed_and_only_creates_current_metadata(self):
        result,run,edge=self.adopt()
        self.assertFalse(result['gateway_replaced'])
        current=self.state.load(self.app)['releases']['current']
        self.assertEqual(current['status'],'adopted')
        self.assertEqual(current['new_api_image_id'],self.config_id)
        self.assertEqual(current['docker_engine_image_id'],self.engine)
        self.assertEqual((self.app/self.state.NAME).stat().st_mode & 0o777,0o600)
        self.assertTrue(all('config' in call.args for call in run.call_args_list))
        edge.assert_called_with(False)
        self.assert_history()
        with self.assertRaisesRegex(ADAPTER.DeployError,'already adopted'):self.adopt()

    def test_readonly_check_and_config_drift_never_publish_adoption(self):
        result,run,_=self.adopt(execute=False)
        self.assertEqual(result['mode'],'read_only');run.assert_not_called()
        self.assertFalse((self.app/self.state.NAME).exists())
        with self.assertRaisesRegex(ADAPTER.DeployError,'change live configuration'):self.adopt(drift=True)
        self.assertFalse((self.app/'compose-local.yaml').exists())
        self.assertFalse((self.app/self.state.NAME).exists());self.assert_history()

    def test_incident_or_environment_drift_never_falls_back_to_old_failed_manifest(self):
        self.adopt()
        for path in (self.app/'controlled-launch-state.json',self.env):
            raw=path.read_bytes();path.write_bytes(raw+b' ')
            with self.assertRaises(ValueError):self.state.manifest_path(self.app)
            path.write_bytes(raw)
        (self.app/self.state.NAME).chmod(0o666)
        with self.assertRaises(ValueError):self.state.manifest_path(self.app)

    def test_boot_and_backup_agree_on_adopted_engine_and_config_identity(self):
        self.adopt()
        self.assertEqual(BOOT.approved_identity(self.app),(self.ref,self.rev,self.engine))
        trace=[]
        def docker(*args):
            trace.append(args)
            if args[0]=='compose':
                return self.cid if 'ps' in args else json.dumps({'services':{'new-api':{'image':self.ref}}})
            if args[0]=='inspect':
                return json.dumps([dict(Id=self.cid,Image=self.engine,Created='2026-01-01T00:00:00Z',State={'Running':True})])
            if args[:2]==('image','inspect'):
                return json.dumps([{'Id':self.engine,'RepoDigests':[self.ref],
                    'Config':{'Labels':{'org.opencontainers.image.revision':self.rev}}}])
            if args[0]=='save':
                with tarfile.open(args[2],'w') as bundle:
                    for name,raw in [('manifest.json',b'[{"Config":"config.json"}]'),('config.json',self.config)]:
                        entry=tarfile.TarInfo(name);entry.size=len(raw);bundle.addfile(entry,io.BytesIO(raw))
                return ''
            self.fail('unexpected Docker mutation')
        spec=types.SimpleNamespace(loader=types.SimpleNamespace(exec_module=lambda m:None))
        with patch.object(BACKUP,'docker',side_effect=docker), \
                patch.object(BACKUP.importlib.util,'spec_from_file_location',return_value=spec), \
                patch.object(BACKUP.importlib.util,'module_from_spec',return_value=self.state):
            identity=BACKUP.capture(self.app)
        self.assertEqual(identity['docker_image_id'],self.config_id)
        self.assertEqual(identity['docker_engine_image_id'],self.engine)
        self.assertEqual(identity['deployment_state_file'],self.state.NAME)
        compose=next(args for args in trace if args[0]=='compose' and 'ps' in args)
        self.assertEqual(compose.count('-f'),5);self.assertIn('--env-file',compose)
        self.assert_history()

    def test_receive_updates_local_state_and_terminal_overlay_preserving_original_four_layers(self):
        self.adopt()
        c={'app_dir':str(self.app),'service':'new-api','compose_file':'docker-compose.prod.yml',
           'runtime_identity_dir':'/opt/clearapi/runtime/new-api'}
        a=types.SimpleNamespace(commit='7'*40,image_id='sha256:'+'8'*64,
                                previous_image_id=self.config_id,scope='frontend')
        old={'commit':self.rev,'image_id':self.config_id,'image_ref':self.ref,'container_id':self.cid}
        new={'commit':a.commit,'image_id':a.image_id,'image_ref':'clearapi-local:'+a.commit,'container_id':'9'*64}
        with contextlib.ExitStack() as stack:
            for name in ('identity_prepare','identity_refresh','wait_health','verify_manifest','verify_override_scope'):
                stack.enter_context(patch.object(ADAPTER,name))
            stack.enter_context(patch.object(ADAPTER,'preflight',return_value=old))
            stack.enter_context(patch.object(ADAPTER,'receive_archive'))
            stack.enter_context(patch.object(ADAPTER,'deployed',return_value=new))
            run=stack.enter_context(patch.object(ADAPTER,'run',return_value=''))
            with contextlib.redirect_stdout(io.StringIO()):ADAPTER.apply(c,a,'a'*64,123)
        self.assertEqual(self.state.load(self.app)['releases']['current']['status'],'success')
        up=next(call.args for call in run.call_args_list if 'up' in call.args)
        self.assertEqual(up.count('-f'),5);self.assertIn('--no-deps',up)
        self.assertIn(new['image_ref'],(self.app/'compose-local.yaml').read_text())
        self.assert_history()

    def test_formal_backup_rejects_a_fresh_ciphertext_bound_to_another_gateway(self):
        self.adopt()
        base=self.root/'backups';(base/'manifests').mkdir(parents=True)
        bid='20261024T010000Z';enc=base/'encrypted'/bid/'test.gpg';enc.parent.mkdir(parents=True)
        raw=b'isolated ciphertext fixture';enc.write_bytes(raw)
        c={'app_dir':str(self.app),'backup_dir':str(base),'backup_service':'clearapi-backup.service'}
        record={'backup_schema_version':2,'backup_identity_schema_version':1,'backup_id':bid,
            'running_identity':{'oci_revision':self.rev,'docker_image_id':self.config_id,'container_id':'wrong'},
            'encryption':{'encrypted_artifact':{'name':'test.gpg','bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}}}
        def run(*args):
            if args[:2]==('systemctl','start'):
                path=base/'manifests'/(bid+'.json');path.write_text(json.dumps(record));path.chmod(0o600)
            return 'success'
        live={'commit':self.rev,'image_id':self.config_id,'container_id':self.cid}
        with patch.object(ADAPTER,'run',side_effect=run),patch.object(ADAPTER,'deployed',return_value=live):
            with self.assertRaisesRegex(ADAPTER.DeployError,'current running Gateway'):ADAPTER.backup(c)
        self.assert_history()

    def test_inherited_compose_values_cannot_override_the_existing_root_environment(self):
        with patch.dict(os.environ,{'CLEARAPI_POOL_ENABLED':'true','COMPOSE_FILE':'foreign.yml'}):
            clean=self.state.compose_environment()
        self.assertNotIn('CLEARAPI_POOL_ENABLED',clean);self.assertNotIn('COMPOSE_FILE',clean)


if __name__=='__main__':unittest.main()
