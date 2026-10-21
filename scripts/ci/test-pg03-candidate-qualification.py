#!/usr/bin/env python3
"""Deterministic GET fixtures; no GitHub, production, Docker or credentials."""
import base64
import copy
from datetime import datetime, timedelta, timezone
import importlib.util
import io
import json
from pathlib import Path
import sys
import unittest
import zipfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('qualification', ROOT/'scripts/pg03-candidate-qualification.py')
Q = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(Q)
TX, PLAN = Q.TX, Q.PLAN


def archive(values):
    stream = io.BytesIO()
    with zipfile.ZipFile(stream, 'w') as saved:
        for name, raw in values.items():
            saved.writestr(name, raw if type(raw) is bytes else TX.encoded(raw))
    return stream.getvalue()


class QualificationTest(unittest.TestCase):
    def setUp(self):
        self.now = datetime(2026, 10, 21, 0, 0, tzinfo=timezone.utc)
        self.control, self.isolation_control = 'f'*40, 'e'*40
        self.raw_rc = (ROOT/'evidence/pg03-rc-39511197302/release-original.zip').read_bytes()
        with zipfile.ZipFile(io.BytesIO(self.raw_rc)) as saved:
            self.manifest = json.loads(saved.read('gateway-image-release.json'))
        m = self.manifest
        self.candidate = dict(source_commit=m['source_commit'], image_digest=m['image_digest'], image_ref=m['image_ref'],
            image_id=m['new_api_image_id'], binary_sha256=m['binary_sha256'], rc_run_id=int(m['workflow_run']), rc_attempt=1)
        self.baseline = m['schema_transition']['target_schema_sha256']
        self.request = dict(schema='clearapi.successor-transaction-request/v1', transaction_id='1'*32,
            issued_at_utc='2026-10-21T00:00:00Z', expires_at_utc='2026-10-21T00:05:00Z', control_commit=self.control,
            candidate=self.candidate, evidence_sha256=dict.fromkeys(TX.EVIDENCE, 'a'*64), baseline_schema_sha256=self.baseline,
            operations=list(TX.OPERATIONS), failed_run=PLAN.FAILED_RUN, frozen_sha256=PLAN.FROZEN,
            previous_success=dict(source_commit=PLAN.PREVIOUS_COMMIT, image_digest=PLAN.PREVIOUS_DIGEST))
        self.bindings = dict(rc=dict(run_id=self.candidate['rc_run_id'], attempt=1, source_commit=m['source_commit'],
            workflow_path='.github/workflows/release-pg03-gateway.yml', event='workflow_dispatch', artifact_id=12305532882,
            artifact_name='pg03-gateway-release-39511197302-1', archive_sha256=TX.sha(self.raw_rc)),
            source_ci=dict(run_id=39510261549, attempt=1, source_commit=m['source_commit'],
                workflow_path='.github/workflows/ci.yml', event='push'),
            control_ci=dict(run_id=234, attempt=1, source_commit=self.control,
                workflow_path='.github/workflows/ci.yml', event='push'),
            isolation=dict(run_id=39517646061, attempt=1, source_commit=self.isolation_control,
                workflow_path='.github/workflows/pg03-isolated-rehearsal.yml', event='workflow_dispatch',
                artifact_id=12308671421, artifact_name='pg03-isolated-schema-39517646061-1', archive_sha256='0'*64))
        control_files = {path: TX.sha(('control '+path).encode()) for path in Q.REQUIRED_CONTROL_FILES}
        isolation_files = {path: TX.sha(('isolation '+path).encode()) for path in Q.ISOLATION_FILES}
        source_files = {path: TX.sha(('source '+path).encode()) for path in Q.SOURCE_FILES}
        self.policy = dict(schema='clearapi.candidate-qualification-policy/v1', policy_id='2'*32, state='active',
            scope='candidate_qualification_only', source_control_policy='independently_reviewed_source_control_split',
            approved_by=Q.AD.OWNER, approval_ref='owner-approval:fixture-only', repository=Q.AD.REPO, repository_id=100,
            issued_at_utc=self.request['issued_at_utc'], expires_at_utc=self.request['expires_at_utc'],
            request_sha256=TX.sha(TX.encoded(self.request)), transaction_id=self.request['transaction_id'], candidate=self.candidate,
            control_commit=self.control, max_evidence_age_seconds=86400, revoked_policy_ids=[], revoked_source_commits=[],
            revoked_image_digests=[], consumed_transaction_ids=[], control_files_sha256=control_files,
            isolation_control_files_sha256=isolation_files, allowed_control_changes=['scripts/pg03-candidate-qualification.py'],
            source_workflow_files_sha256=source_files, source_control_diff_sha256='0'*64, bindings=self.bindings)
        changed = dict(filename='scripts/pg03-candidate-qualification.py', status='added', sha='a'*40)
        projection = dict(commits=[self.control], files=[changed])
        self.policy['source_control_diff_sha256'] = TX.sha(TX.encoded(projection))
        self.responses = {Q.ROOT: dict(full_name=Q.AD.REPO, owner=dict(login=Q.AD.OWNER), id=100),
            Q.ROOT+'/git/ref/heads/main': dict(object=dict(sha=self.control)),
            Q.ROOT+'/branches/main/protection': dict(required_status_checks=dict(strict=True, contexts=sorted(Q.AD.REQUIRED)),
                enforce_admins=dict(enabled=True), allow_force_pushes=dict(enabled=False), allow_deletions=dict(enabled=False)),
            Q.ROOT+'/compare/'+m['source_commit']+'...'+self.control: dict(status='ahead',
                merge_base_commit=dict(sha=m['source_commit']), base_commit=dict(sha=m['source_commit']),
                total_commits=1, commits=[dict(sha=self.control)], files=[changed])}
        for key in ('CLEARAPI_AUTO_RC_ENABLED', 'CLEARAPI_AUTO_PRODUCTION_ENABLED'):
            self.responses[Q.ROOT+'/actions/variables/'+key] = dict(value='false')
        for commit, kind, values in ((self.control, 'control', control_files), (self.isolation_control, 'isolation', isolation_files),
                                    (m['source_commit'], 'source', source_files)):
            for path in values:
                raw = (kind+' '+path).encode()
                self.responses[Q.ROOT+'/contents/'+path+'?ref='+commit] = dict(type='file', path=path,
                    size=len(raw), encoding='base64', content=base64.b64encode(raw).decode())
        for index, (name, binding) in enumerate(self.bindings.items()):
            workflow_id = 10 if name.endswith('ci') else 20+index
            run = dict(id=binding['run_id'], run_attempt=1, head_sha=binding['source_commit'], head_branch='main',
                path=binding['workflow_path'], event=binding['event'], status='completed', conclusion='success',
                workflow_id=workflow_id, repository=dict(full_name=Q.AD.REPO, id=100),
                head_repository=dict(full_name=Q.AD.REPO, id=100), actor=dict(login=Q.AD.OWNER),
                triggering_actor=dict(login=Q.AD.OWNER), updated_at='2026-10-20T23:51:37Z')
            self.responses[Q.ROOT+'/actions/runs/'+str(binding['run_id'])] = run
            workflow = Q.ROOT+'/actions/workflows/'+binding['workflow_path'].rsplit('/', 1)[1]
            self.responses[workflow] = dict(id=workflow_id, path=binding['workflow_path'], state='active')
            query = '?branch=main&head_sha='+binding['source_commit']+('&event=push' if binding['event']=='push' else '')+'&per_page=1'
            self.responses[workflow+'/runs'+query] = dict(workflow_runs=[dict(id=binding['run_id'], run_attempt=1)])
            self.responses[Q.ROOT+'/actions/runs/'+str(binding['run_id'])+'/attempts/1/jobs?per_page=100'] = dict(
                total_count=1, jobs=[dict(status='completed', conclusion='success')])
        self.isolated = self.make_isolation()
        self.isolated_files = self.isolated
        self.raw_isolated = archive(self.isolated_files)
        self.bindings['isolation']['archive_sha256'] = TX.sha(self.raw_isolated)
        self.archives = {12305532882: self.raw_rc, 12308671421: self.raw_isolated}
        for name in ('rc', 'isolation'):
            b = self.bindings[name]
            self.responses[Q.ROOT+'/actions/artifacts/'+str(b['artifact_id'])] = dict(id=b['artifact_id'],
                expired=False, name=b['artifact_name'], size_in_bytes=len(self.archives[b['artifact_id']]),
                digest='sha256:'+b['archive_sha256'], workflow_run=dict(id=b['run_id'], head_sha=b['source_commit'],
                    head_branch='main', repository_id=100, head_repository_id=100))

    def make_isolation(self):
        candidate, isolated, rc = self.candidate, self.bindings['isolation'], self.bindings['rc']
        receipt = copy.deepcopy(self.manifest['schema_transition'])
        receipt['source_schema_sha256'] = self.baseline
        receipt['successor_baseline'] = dict(schema='clearapi.successor-schema-baseline/v1',
            rc_archive_sha256=rc['archive_sha256'], schema_source='verified_rc_target', observed_schema_sha256=self.baseline,
            production_observation_verified=False, production_acceptance='not_run',
            producer_sha256=self.policy['isolation_control_files_sha256']['scripts/pg03-successor-plan.py'],
            schema_helper_sha256=self.policy['isolation_control_files_sha256']['scripts/pg03-schema-rehearsal.py'])
        startups = []
        for index in (1, 2):
            cid, challenge = str(index)*64, str(index+2)*64
            raw = TX.encoded(dict(schema='clearapi.startup-migration-proof/v1', status='success',
                main_database='postgres', log_database='postgres', phases=['main_database','retired_options','log_database'],
                binary_sha256=candidate['binary_sha256'], challenge=challenge, hostname='fixture', pid=1)).decode()
            startups.append(dict(pass_number=index, before_schema_sha256=self.baseline, after_schema_sha256=self.baseline,
                create_exit_code=0, start_exit_code=0, stop_exit_code=0, healthy=True, container_id=cid,
                configured_hostname='fixture', hostname='fixture', process=dict(Status='exited', Running=False,
                    ExitCode=0, OOMKilled=False, error_present=False), migration=dict(status='success', source_commit=candidate['source_commit'],
                    container_id=cid, completion_receipt_json=raw, proof_sha256=TX.sha(raw.encode()), challenge=challenge)))
        result = dict(schema='clearapi.isolated-actions-rehearsal/v1', control_commit=isolated['source_commit'],
            workflow_ref=Q.AD.REPO+'/'+isolated['workflow_path']+'@refs/heads/main', run_id=str(isolated['run_id']),
            run_attempt='1', candidate_source=candidate['source_commit'], candidate_image=candidate['image_ref'],
            rc_run=str(rc['run_id']), rc_attempt=1, artifact_id=str(rc['artifact_id']), archive_sha256=rc['archive_sha256'],
            baseline_schema_sha256=self.baseline, control_sha256=self.policy['isolation_control_files_sha256'],
            production_observation_verified=False, production_acceptance='not_run', conclusion='success',
            prior_job_status='success', rehearsal='passed', success_receipt_present=True, auth_removed=True,
            final_cleanup=dict(complete=True), steps=dict.fromkeys(('prepare', 'baseline','pull','rehearse','finalize'),'passed'),
            observation=dict(candidate=dict(image_id=candidate['image_id'], binary_sha256=candidate['binary_sha256'],
                image_ref=candidate['image_ref'], source_commit=candidate['source_commit']), cleanup_complete=True,
                startups=startups, cleanup=[dict(container_id=str(index)*64, removed=True) for index in (1,2,3,4)]),
            negative_migration=dict(status='expected_migration_failure_detected', enforced_read_only=True, cleanup_complete=True,
                sqlstate='25006', candidate_image=candidate['image_ref'], success_receipt_eligible=False,
                proof_capture='unavailable_after_tmpfs_exit', cleanup=[dict(container_id=str(index)*64, removed=True) for index in (5,6)],
                process=dict(Status='exited', Running=False, ExitCode=1, OOMKilled=False, error_present=False), log_sha256='a'*64))
        return {'result.json': result, 'success.json': receipt}

    def get(self, path):
        return copy.deepcopy(self.responses[path])

    def get_bytes(self, path, limit):
        raw = self.archives[int(path.rsplit('/',2)[1])]
        self.assertLessEqual(len(raw), limit)
        return raw

    def qualify(self, **kwargs):
        return Q.qualify(self.get, self.get_bytes, lambda: copy.deepcopy(self.policy), self.request,
            now=kwargs.pop('now', lambda:self.now), **kwargs)

    def test_complete_provenance_qualifies_without_granting_execution(self):
        result = self.qualify()
        self.assertEqual(result['status'], 'PASS')
        self.assertFalse(result['execution_allowed'])
        self.assertEqual(result['production_authorization'], 'BLOCKED')
        self.assertEqual(result['production_acceptance'], 'NOT RUN')
        self.assertEqual(result['request_sha256'], TX.sha(TX.encoded(self.request)))

    def test_missing_or_unreviewed_policy_refused(self):
        for key, value in (('state','revoked'), ('scope','production_execute'),
                           ('source_control_policy','allow_if_ci_passes'), ('approved_by','someone-else'),
                           ('request_sha256','0'*64), ('repository_id',101)):
            with self.subTest(key=key):
                old = self.policy[key]
                self.policy[key] = value
                with self.assertRaises(ValueError): self.qualify()
                self.policy[key] = old

    def test_revoked_and_consumed_policy_refused(self):
        for key, value in (('revoked_policy_ids',self.policy['policy_id']),
                           ('revoked_source_commits',self.candidate['source_commit']),
                           ('revoked_image_digests',self.candidate['image_digest']),
                           ('consumed_transaction_ids',self.request['transaction_id'])):
            with self.subTest(key=key):
                self.policy[key] = [value]
                with self.assertRaises(ValueError): self.qualify()
                self.policy[key] = []

    def test_cross_transaction_replay_refused(self):
        self.request['transaction_id'] = '3'*32
        with self.assertRaises(ValueError): self.qualify()

    def test_expiry_future_timestamp_and_deadline_crossing_refused(self):
        with self.assertRaises(ValueError): self.qualify(now=lambda:self.now+timedelta(seconds=300))
        for updated in ('2026-10-19T00:00:00Z','2026-10-21T00:00:01Z'):
            self.responses[Q.ROOT+'/actions/runs/39510261549']['updated_at'] = updated
            with self.assertRaises(ValueError): self.qualify()

    def test_candidate_mutable_tag_and_binary_mismatch_refused(self):
        old = copy.deepcopy(self.request)
        for key, value in (('image_ref','ghcr.io/a3225865526-wq/clearapi:latest'),
                           ('binary_sha256','b'*64), ('rc_attempt',2), ('source_commit','b'*40)):
            self.request = copy.deepcopy(old)
            self.request['candidate'][key] = value
            self.policy['candidate'] = self.request['candidate']
            self.policy['request_sha256'] = TX.sha(TX.encoded(self.request))
            with self.subTest(key=key), self.assertRaises(ValueError): self.qualify()

    def test_live_main_controls_and_control_file_drift_refused(self):
        for path, field, value in ((Q.ROOT+'/git/ref/heads/main','object',dict(sha='a'*40)),
            (Q.ROOT+'/actions/variables/CLEARAPI_AUTO_RC_ENABLED','value','true'),
            (Q.ROOT+'/contents/scripts/pg03-candidate-qualification.py?ref='+self.control,'content',base64.b64encode(b'forged').decode())):
            old = copy.deepcopy(self.responses[path])
            self.responses[path][field] = value
            with self.subTest(path=path), self.assertRaises(ValueError): self.qualify()
            self.responses[path] = old

    def test_source_control_app_drift_or_incomplete_compare_refused(self):
        path = Q.ROOT+'/compare/'+self.candidate['source_commit']+'...'+self.control
        old = copy.deepcopy(self.responses[path])
        mutations = [dict(status='diverged'), dict(total_commits=2), dict(files=[dict(filename='model/main.go',status='modified',sha='a'*40)]),
            dict(files=[]), dict(files=[dict(filename='scripts/pg03-candidate-qualification.py',status='renamed',sha='a'*40)]),
            dict(commits=[dict(sha='a'*40)])]
        for mutation in mutations:
            self.responses[path] = dict(old, **mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError): self.qualify()

    def test_failed_skipped_unknown_jobs_and_superseded_runs_refused(self):
        for name,binding in self.bindings.items():
            path = Q.ROOT+'/actions/runs/'+str(binding['run_id'])+'/attempts/1/jobs?per_page=100'
            old = copy.deepcopy(self.responses[path])
            for value in ('failure','skipped',None):
                self.responses[path]['jobs'][0]['conclusion'] = value
                with self.subTest(name=name,value=value), self.assertRaises(ValueError): self.qualify()
            self.responses[path] = old
        path = next(path for path in self.responses if '/ci.yml/runs?' in path)
        self.responses[path]['workflow_runs'][0]['id'] = 999
        with self.assertRaises(ValueError): self.qualify()

    def test_forged_archive_expired_artifact_and_rerun_refused(self):
        path = Q.ROOT+'/actions/artifacts/12305532882'
        self.responses[path]['expired'] = True
        with self.assertRaises(ValueError): self.qualify()
        self.responses[path]['expired'] = False
        self.archives[12305532882] += b'forged'
        with self.assertRaises(ValueError): self.qualify()
        self.archives[12305532882] = self.raw_rc
        self.responses[Q.ROOT+'/actions/runs/39511197302']['run_attempt'] = 2
        with self.assertRaises(ValueError): self.qualify()

    def test_isolation_migration_replay_cleanup_and_schema_tamper_refused(self):
        for mutate in ('replay','cleanup','binary','schema','control'):
            files = copy.deepcopy(self.isolated_files)
            if mutate=='replay':
                files['result.json']['observation']['startups'][1] = copy.deepcopy(files['result.json']['observation']['startups'][0])
            elif mutate=='cleanup': files['result.json']['observation']['cleanup'][0]['removed'] = False
            elif mutate=='binary': files['result.json']['observation']['candidate']['binary_sha256'] = 'b'*64
            elif mutate=='schema': files['success.json']['target_schema_sha256'] = 'b'*64
            else: files['result.json']['control_sha256'] = {}
            with self.subTest(mutate=mutate), self.assertRaises(ValueError):
                Q.inspect_isolation({name:TX.encoded(value) for name,value in files.items()}, self.policy, self.request)

    def test_policy_revocation_rerun_and_transport_loss_mid_read_refused(self):
        calls = 0
        def provider():
            nonlocal calls
            calls += 1
            value = copy.deepcopy(self.policy)
            if calls==2: value['state']='revoked'
            return value
        with self.assertRaises(ValueError):
            Q.qualify(self.get,self.get_bytes,provider,self.request,now=lambda:self.now)
        self.assertEqual(calls,2)
        reads = 0
        def get(path):
            nonlocal reads
            if path==Q.ROOT+'/actions/runs/39511197302':
                reads += 1
                if reads>=4:
                    result=self.get(path)
                    result['run_attempt']=2
                    return result
            return self.get(path)
        with self.assertRaises(ValueError):
            Q.qualify(get,self.get_bytes,lambda:self.policy,self.request,now=lambda:self.now)
        def unavailable(path): raise TimeoutError('fixture transport unavailable')
        with self.assertRaises(TimeoutError):
            Q.qualify(unavailable,self.get_bytes,lambda:self.policy,self.request,now=lambda:self.now)

    def test_duplicate_json_and_zip_entries_refused(self):
        with self.assertRaises(ValueError): Q.AD.document(b'{"schema":1,"schema":2}')
        with self.assertRaises(ValueError): Q.zip_documents(archive({'result.json':{},'unexpected.json':{}}),('result.json','success.json'))


if __name__ == '__main__':
    unittest.main()
