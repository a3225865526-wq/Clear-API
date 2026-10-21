#!/usr/bin/env python3
"""Linux-root temporary-tree fixtures; no actual Docker or production access."""
import copy
from datetime import datetime, timedelta, timezone
import importlib.util
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('successor_host', Path(__file__).parents[1]/'pg03-successor-host.py')
HOST = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HOST)
TX, PLAN = HOST.TX, HOST.PLAN


@unittest.skipUnless(HOST.os.name == 'posix' and HOST.os.geteuid() == 0, 'Linux root installation contract')
class HostTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for name in ('run/lock', 'run/clearapi', 'opt/clearapi/app', 'opt/clearapi/bin'):
            (self.root/name).mkdir(parents=True, exist_ok=True, mode=0o755)
        (self.root/'run/clearapi').chmod(0o750)
        self.now = datetime(2026,10,21,tzinfo=timezone.utc)
        frozen = {}
        for name in PLAN.FROZEN:
            raw = ('retained incident '+name).encode()
            self.write('opt/clearapi/app/'+name, raw)
            frozen[name] = TX.sha(raw)
        self.addCleanup(patch.stopall)
        patch.object(PLAN,'FROZEN',frozen).start()
        candidate = dict(source_commit='a'*40,image_digest='sha256:'+'b'*64,
            image_ref='ghcr.io/a3225865526-wq/clearapi@sha256:'+'b'*64,image_id='sha256:'+'b'*64,
            binary_sha256='c'*64,rc_run_id=123,rc_attempt=1)
        self.request = dict(schema='clearapi.successor-transaction-request/v1',transaction_id='1'*32,
            issued_at_utc='2026-10-21T00:00:00Z',expires_at_utc='2026-10-21T00:05:00Z',control_commit='d'*40,
            candidate=candidate,evidence_sha256=dict.fromkeys(TX.EVIDENCE,'e'*64),baseline_schema_sha256='f'*64,
            operations=list(TX.OPERATIONS),failed_run=PLAN.FAILED_RUN,frozen_sha256=frozen,
            previous_success=dict(source_commit=PLAN.PREVIOUS_COMMIT,image_digest=PLAN.PREVIOUS_DIGEST))
        self.cid, self.commands, self.starts = '7'*64, [], 0
        self.proof, self.post = None,None
        self.baseline = dict(schema='clearapi.successor-live-projection/v1',observed_at_utc=self.request['issued_at_utc'],
            frozen_sha256=frozen,gateway_container_id=TX.GATEWAY,gateway_running=False,
            executor_container_id=PLAN.EXECUTOR_CONTAINER,executor_process_epoch='3'*32,
            ingress='isolated_stopped_restart_no',public_web_listeners=[],database_schema_sha256='f'*64,
            protected_state_sha256='2'*64,startup_write_scope_sha256='e'*64,candidate_identity=None,
            process_started_ticks=None,migration_challenge=None,migration_proof_sha256=None,
            runtime_evidence_sha256=None,restart_evidence_sha256=None)
        self.request['evidence_sha256']['production_observation'] = TX.sha(TX.encoded(self.baseline))
        self.request_sha = TX.sha(TX.encoded(self.request))
        assets = {}
        for name in HOST.ASSETS:
            raw = ('test-only installed asset '+name).encode()
            self.write(name.lstrip('/'),raw)
            assets[name] = TX.sha(raw)
        compose = ['/opt/clearapi/app/docker-compose.prod.yml','/opt/clearapi/app/compose-successor.yaml']
        compose_hashes = {}
        for name in compose:
            raw = ('test-only approved compose '+name).encode()
            self.write(name.lstrip('/'),raw)
            compose_hashes[name]=TX.sha(raw)
        self.identity = dict(identity_mode='stopped_failed_gateway',container_id=TX.GATEWAY)
        self.policy = dict(schema='clearapi.successor-site-policy/v1',policy_id='4'*32,
            request_sha256=self.request_sha,control_commit=self.request['control_commit'],candidate=candidate,
            frozen_sha256=frozen,issued_at_utc=self.request['issued_at_utc'],expires_at_utc=self.request['expires_at_utc'],
            asset_sha256=assets,compose_files=compose,compose_files_sha256=compose_hashes,
            effective_nonsecret=dict(candidate_image=candidate['image_ref'],
                identity_mount=dict(source='/run/clearapi',target='/run/clearapi',read_only=True,create_host_path=False),
                migration_launcher=dict(source='/opt/clearapi/bin/production-migration-entrypoint.sh',
                    target='/entrypoint-migration.sh',read_only=True,create_host_path=False),
                entrypoint=['/entrypoint-migration.sh'],command=[],tmpfs_paths=['/tmp'],gateway_public_listeners=[],dependency_start=False),
            effective_configuration_sha256='5'*64,permission_sha256='6'*64,executor_identity_sha256='7'*64,
            writer_inventory_sha256='8'*64,startup_write_scope_sha256='e'*64,stopped_writer_identity=self.identity)
        self.grant = dict(schema='clearapi.successor-production-grant/v1',request_sha256=self.request_sha,
            transaction_id=self.request['transaction_id'],candidate=candidate,operations=list(TX.OPERATIONS),
            expires_at_utc=self.request['expires_at_utc'],public_traffic='blocked',executor_start='forbidden',database_restore='forbidden')
        self.collector = SimpleNamespace(authenticate=lambda *args:True,site_projection=self.site_projection,
            verify_dependencies=lambda *args:True,observe=lambda *args:copy.deepcopy(self.post or self.baseline),
            verify_postflight=self.postflight,authorize_containment=lambda request,cid,*args:cid==self.cid,
            containment=lambda *args:self.contained())
        self.host = self.make_host()
        self.checked = dict(backup=dict(stopped_writer_identity=self.identity),scope=dict(effective_configuration_sha256='5'*64))

    def write(self,name,raw):
        path=self.root/name
        path.parent.mkdir(parents=True,exist_ok=True,mode=0o755)
        path.write_bytes(raw)
        path.chmod(0o600)

    def site_projection(self,*args):
        return dict(schema='clearapi.successor-site-projection/v1',
            **{key:copy.deepcopy(self.policy[key]) for key in ('effective_nonsecret','effective_configuration_sha256',
                'compose_files_sha256','permission_sha256','executor_identity_sha256','writer_inventory_sha256')},
            all_entry_routes_frozen=True,sole_executor=True,executor_container_id=PLAN.EXECUTOR_CONTAINER,lock_owned=True)

    def runner(self,argv,deadline):
        self.commands.append(argv)
        stdout=b''
        if argv[-4:]==['ps','-a','-q','new-api']: stdout=(self.cid+'\n').encode()
        if 'start' in argv:
            self.starts+=1
            challenge=(self.root/'run/clearapi/migration-challenge').read_text().strip()
            self.proof=TX.encoded(dict(schema='clearapi.startup-migration-proof/v1',status='success',
                main_database='postgres',log_database='postgres',phases=['main_database','retired_options','log_database'],
                binary_sha256=self.request['candidate']['binary_sha256'],challenge=challenge,hostname='fixture',pid=1))
        if argv[-2:]==['cat','/tmp/clearapi-migration-proof.json']: stdout=self.proof
        return dict(returncode=0,stdout=stdout)

    def postflight(self,request,context,deadline):
        self.post=dict(self.baseline,gateway_container_id=self.cid,gateway_running=True,
            candidate_identity=self.request['candidate'],process_started_ticks=str(100+self.starts),
            migration_challenge=context['migration_challenge'],migration_proof_sha256=TX.sha(self.proof),
            runtime_evidence_sha256='a'*64,restart_evidence_sha256='b'*64 if self.starts==2 else None)
        return dict(verified=True,configured_hostname='fixture',live_hostname='fixture',observation=copy.deepcopy(self.post))

    def contained(self):
        return dict(frozen_sha256=PLAN.FROZEN,executor_container_id=PLAN.EXECUTOR_CONTAINER,
            executor_process_epoch='3'*32,ingress='isolated_stopped_restart_no',public_web_listeners=[],
            successor_gateway_stopped=True,database_restored=False,executor_started=False)

    def make_host(self,**extra):
        return HOST.ProductionHost(root=self.root,site_policy=lambda:copy.deepcopy(self.policy),
            site_authority=lambda *args:True,bounded_runner=extra.pop('bounded_runner',self.runner),
            collector=self.collector,now=lambda:self.now,**extra)

    def deadline(self): return self.now+timedelta(seconds=20)

    def prime(self,host=None):
        host=host or self.host
        host.authorize(self.request,self.grant,self.deadline())
        with patch.object(HOST.AD,'validate_dependencies',return_value=self.checked):
            host.verify_dependencies(self.request,{},self.deadline())
        host.observe(self.request,self.deadline())

    def reserve(self):
        HOST.STORE.publish(self.root/'opt/clearapi/app'/('pg03-successor-'+self.request['transaction_id']+'-attempt.json'),
            dict(schema='clearapi.successor-attempt/v1',request_sha256=self.request_sha,
                transaction_id=self.request['transaction_id'],status='reserved_outcome_unknown',request=self.request,public_traffic='blocked'))

    def execute(self):
        host, request = self.host,self.request
        io=SimpleNamespace(deployment_lock=host.deployment_lock,
            authenticate=lambda req,sha:host.authorize(req,self.grant,self.deadline()),
            verify_evidence=lambda req:host.verify_dependencies(req,{},self.deadline()),
            observe=lambda req:host.observe(req,self.deadline()),
            contain=lambda req,ctx:host.contain(req,ctx,self.deadline()))
        for name in TX.OPERATIONS:
            setattr(io,name,lambda req,ctx,phase=name:host.invoke(phase,req,ctx,self.deadline())['value'])
        with patch.object(HOST.AD,'validate_dependencies',return_value=self.checked):
            return TX.Transaction(self.root/'opt/clearapi/app',io,now=lambda:self.now).execute(TX.encoded(request))

    def test_full_kernel_sequence_preserves_incident_and_saves_two_fresh_proofs(self):
        original={name:(self.root/'opt/clearapi/app'/name).read_bytes() for name in PLAN.FROZEN}
        result=self.execute()
        self.assertEqual(result['status'],'controlled_successor_verified_ingress_blocked')
        self.assertEqual(self.starts,2)
        app,prefix=self.root/'opt/clearapi/app','pg03-successor-'+self.request['transaction_id']
        first=HOST.STORE.read_json(app/(prefix+'-startup-proof.json'))
        second=HOST.STORE.read_json(app/(prefix+'-restart-proof.json'))
        self.assertNotEqual(first['migration_challenge'],second['migration_challenge'])
        self.assertNotEqual(first['receipt_sha256'],second['receipt_sha256'])
        self.assertEqual(first['gateway_container_id'],second['gateway_container_id'])
        self.assertTrue((app/(prefix+'-manifest.json')).is_file())
        self.assertEqual(original,{name:(app/name).read_bytes() for name in PLAN.FROZEN})
        replace=next(argv for argv in self.commands if 'up' in argv)
        self.assertIn('--no-start',replace)
        self.assertFalse(any('restart' in argv for argv in self.commands))
        self.assertTrue(all(argv[0] in ('/usr/bin/docker','/usr/bin/python3') for argv in self.commands))
        with self.assertRaises(ValueError): self.execute()

    def test_default_host_cannot_activate(self):
        unbound=HOST.ProductionHost(now=lambda:self.now)
        with self.assertRaises(ValueError):
            with unbound.deployment_lock(): self.fail('unbound host activated')
        self.assertEqual(self.commands,[])

    def test_missing_claim_dependencies_and_repeat_action_never_run(self):
        with self.host.deployment_lock():
            self.host.authorize(self.request,self.grant,self.deadline())
            with self.assertRaises(ValueError): self.host.invoke('prepare_runtime',self.request,{},self.deadline())
        self.assertEqual(self.commands,[])
        self.host=self.make_host()
        with self.host.deployment_lock():
            self.prime()
            with self.assertRaises(ValueError): self.host.invoke('prepare_runtime',self.request,{},self.deadline())
        self.host=self.make_host()
        with self.host.deployment_lock():
            self.prime()
            self.reserve()
            self.host.invoke('prepare_runtime',self.request,{},self.deadline())
            with self.assertRaises(ValueError): self.host.invoke('prepare_runtime',self.request,{},self.deadline())
        count=len(self.commands)
        fresh=self.make_host()
        with fresh.deployment_lock():
            self.prime(fresh)
            with self.assertRaises(ValueError): fresh.invoke('prepare_runtime',self.request,{},self.deadline())
        self.assertEqual(len(self.commands),count)

    def test_asset_compose_frozen_and_authority_drift_are_refused(self):
        for name in ('opt/clearapi/bin/production-migration-entrypoint.sh','opt/clearapi/app/compose-successor.yaml',
                     'opt/clearapi/app/release-manifest.json','opt/clearapi/bin/pg03-successor-plan.py'):
            original=(self.root/name).read_bytes()
            self.write(name,b'changed')
            host=self.make_host()
            with host.deployment_lock(),self.assertRaises(ValueError):
                host.authorize(self.request,self.grant,self.deadline())
            self.write(name,original)
        self.host.site_authority=lambda *args:None
        with self.host.deployment_lock(),self.assertRaises(ValueError):self.prime()

    def test_unverified_restore_source_or_backup_identity_is_refused(self):
        with self.host.deployment_lock():
            self.host.authorize(self.request,self.grant,self.deadline())
            self.collector.verify_dependencies=lambda *args:False
            with patch.object(HOST.AD,'validate_dependencies',return_value=self.checked),self.assertRaises(ValueError):
                self.host.verify_dependencies(self.request,{},self.deadline())
        self.assertFalse(self.host.dependencies_verified)
        self.assertEqual(self.commands,[])

    def test_unknown_mutation_is_consumed_and_contains_owned_cid_after_expiry(self):
        original=self.runner
        def runner(argv,deadline):
            if 'start' in argv:
                self.commands.append(argv)
                raise TimeoutError('fixture UNKNOWN')
            return original(argv,deadline)
        self.host=self.make_host(bounded_runner=runner)
        with self.assertRaises(ValueError):self.execute()
        self.assertTrue(self.host.failed)
        self.assertTrue(any(argv[-4:]==['stop','--time','150',self.cid] for argv in self.commands))
        count=len(self.commands)
        with self.assertRaises(ValueError):self.execute()
        self.assertEqual(len(self.commands),count)
        self.now+=timedelta(seconds=301)
        with self.host.deployment_lock():
            self.assertEqual(self.host.contain(self.request,dict(gateway_container_id=None),self.deadline()),self.contained())

    def test_forged_proof_and_postflight_unknown_fail_without_success_manifest(self):
        self.collector.verify_postflight=lambda *args:dict(verified=False,configured_hostname='fixture',live_hostname='fixture',observation={})
        with self.assertRaises(ValueError):self.execute()
        self.assertFalse((self.root/'opt/clearapi/app'/('pg03-successor-'+self.request['transaction_id']+'-manifest.json')).exists())

    def test_forged_raw_completion_receipt_is_rejected(self):
        original=self.runner
        def runner(argv,deadline):
            result=original(argv,deadline)
            if argv[-2:]==['cat','/tmp/clearapi-migration-proof.json']:
                return dict(returncode=0,stdout=b'{}')
            return result
        self.host=self.make_host(bounded_runner=runner)
        with self.assertRaises(ValueError):self.execute()
        self.assertTrue(self.host.failed)

    def test_missing_imported_tool_pin_cannot_qualify_installation(self):
        del self.policy['asset_sha256']['/opt/clearapi/bin/pg03-schema-rehearsal.py']
        with self.host.deployment_lock(),self.assertRaises(ValueError):self.prime()
        self.assertEqual(self.commands,[])

    def test_runtime_directory_must_be_installed_before_any_mutation(self):
        (self.root/'run/clearapi').chmod(0o755)
        with self.host.deployment_lock(),self.assertRaises(ValueError):self.prime()
        self.assertEqual(self.commands,[])

    def test_live_observation_unknown_poison_prevents_direct_action(self):
        def disconnected(*args):raise TimeoutError('fixture disconnected')
        with self.host.deployment_lock():
            self.prime()
            self.reserve()
            self.collector.observe=disconnected
            with self.assertRaises(ValueError):self.host.observe(self.request,self.deadline())
            self.assertTrue(self.host.failed)
            with self.assertRaises(ValueError):self.host.invoke('prepare_runtime',self.request,{},self.deadline())
        self.assertEqual(self.commands,[])

    def test_scope_drift_during_command_prevents_phase_success(self):
        original=self.runner
        def runner(argv,deadline):
            result=original(argv,deadline)
            if argv[0]=='/usr/bin/python3':self.write('opt/clearapi/bin/production-migration-entrypoint.sh',b'drift during action')
            return result
        self.host=self.make_host(bounded_runner=runner)
        with self.assertRaises(ValueError):self.execute()
        self.assertTrue(self.host.failed)
        self.assertFalse((self.root/'opt/clearapi/app'/('pg03-successor-'+self.request['transaction_id']+'-1-prepare_runtime.json')).exists())


if __name__=='__main__':unittest.main()
