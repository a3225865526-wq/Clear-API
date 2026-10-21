#!/usr/bin/env python3
"""Synthetic offline contract tests; no database, production backup or key access."""
from datetime import datetime, timezone
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('restore_audit', Path(__file__).parents[1] / 'pg03-restore-acceptance.py')
AUDIT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AUDIT)
SPEC = importlib.util.spec_from_file_location('offline_restore', Path(__file__).parents[1] / 'pg03-offline-restore.py')
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)


def encoded(value):
    return json.dumps(value, sort_keys=True).encode()


def archive(entries):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w:gz', format=tarfile.USTAR_FORMAT) as tar:
        for name, value in entries.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(value) if value is not None else 0, 0o600
            if value is None:
                info.type, info.mode = tarfile.DIRTYPE, 0o700
            tar.addfile(info, io.BytesIO(value) if value is not None else None)
    return output.getvalue()


class RestoreAuditTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.now = datetime(2026, 10, 21, 1, tzinfo=timezone.utc)
        self.sha = AUDIT.digest
        identity = dict(identity_mode='stopped_failed_gateway', container_id='f'*64,
                        oci_revision='e'*40, docker_image_id='sha256:'+'d'*64,
                        ghcr_digest='sha256:'+'c'*64,
                        ghcr_repo_digest='ghcr.io/a3225865526-wq/clearapi@sha256:'+'c'*64,
                        release_manifest_sha256='b'*64, compose_base_sha256='a'*64,
                        compose_override_sha256='9'*64)
        self.candidate = dict(source_commit='a'*40, image_digest='sha256:'+'b'*64,
                              image_ref='ghcr.io/a3225865526-wq/clearapi@sha256:'+'b'*64,
                              image_id='sha256:'+'c'*64, binary_sha256='d'*64, rc_run_id=12, rc_attempt=1)
        pg03_files = {name: None for name in AUDIT.PG03_DIRECTORIES}
        pg03_files.update({name: encoded(dict(guard_version=3, revision=1)) for name in AUDIT.PG03_GUARDS})
        pg03_files['opt/clearapi/app/compose-pg03.yaml'] = b'fixture'
        pg03 = {name: dict(mode=0o700 if value is None else 0o600, uid=0, gid=0,
                    directory=value is None, mtime_ns=1,
                    **({} if value is None else dict(bytes=len(value), sha256=self.sha(value))))
                for name, value in pg03_files.items()}
        payloads = {key+'.tar.gz': archive({name: b'test-only' for name in
                    (set(AUDIT.REQUIRED_ARCHIVE_FILES.get(key, ())) |
                     {prefix+'fixture' for prefix in AUDIT.REQUIRED_ARCHIVE_ROOTS.get(key, ())})})
                    for key in ('config_archive', 'legal_archive', 'edge_archive', 'secrets_archive', 'deploy_control_archive')}
        payloads.update({'main.dump': b'PGDMP synthetic', 'globals.sql': b'-- fixture',
                         'pg03.tar.gz': archive(pg03_files),
                         'pg03.json': encoded(pg03)})
        names = {key: key+'.tar.gz' for key in ('config_archive', 'legal_archive', 'edge_archive',
                 'secrets_archive', 'deploy_control_archive')}
        names.update(database_dump='main.dump', globals='globals.sql', pg03_state_archive='pg03.tar.gz',
                     pg03_state_snapshot='pg03.json')
        self.payloads = payloads
        self.bundle = archive(payloads)
        self.write('bundle.tar.gz', self.bundle)
        self.write('encrypted.gpg', b'synthetic ciphertext; not OpenPGP')
        self.manifest = dict(backup_id='20261021T001000Z', created_at_utc='2026-10-21T00:10:00Z',
            environment='production-internal', backup_identity_schema_version=2,
            stopped_writer_identity=identity, pg03_recovery=dict(consistency='unchanged_across_dump_and_bundle'),
            artifacts={key: dict(name=name, sha256=self.sha(payloads[name])) for key, name in names.items()},
            bundle=dict(name='bundle.tar.gz', sha256=self.sha(self.bundle)),
            database_recovery_point=dict(dump_started_at_utc='2026-10-21T00:05:00Z',
                dump_completed_at_utc='2026-10-21T00:09:00Z', dump_sha256=self.sha(payloads['main.dump'])),
            encryption=dict(key_fingerprint='A'*40,
                method='GnuPG OpenPGP; ECDH Curve25519; AES256 + OCB AEAD (--force-ocb)',
                encrypted_artifact=dict(name='encrypted.gpg', bytes=37, sha256=self.sha(b'synthetic ciphertext; not OpenPGP'))))
        self.manifest['encryption']['encrypted_artifact']['bytes'] = len(b'synthetic ciphertext; not OpenPGP')
        self.inventory = dict(main=dict(catalog_tables=['public.options'], tables={
            'public.options': dict(rows=2, rows_sha256='1'*64)}, schema_sha256='2'*64,
            indexes_sha256='3'*64, constraints_sha256='4'*64, sequences_sha256='5'*64,
            migration_state_sha256='6'*64, validated_constraints=1, unvalidated_constraints=0,
            uniqueness_violations=0))
        self.plan = dict(schema='clearapi.offline-restore-plan/v1', repository='a3225865526-wq/Clear-API',
            issued_at_utc='2026-10-21T00:00:00Z', expires_at_utc='2026-10-21T02:00:00Z', challenge='7'*64,
            backup_id=self.manifest['backup_id'], stopped_writer_identity=identity, key_fingerprint='A'*40,
            candidate=self.candidate, databases=dict(main=dict(target_database='clearapi_restore_fixture',
                dump_artifact='database_dump')), log_layout='shared', target_container_id='8'*64,
            production_container_ids=['f'*64], postgres_image_ref='postgres:17.6-alpine@sha256:'+'9'*64,
            evidence_sha256={})
        self.docs = {'backup-manifest.json': self.manifest, 'source-inventory.json': self.inventory,
            'restored-inventory.json': copy.deepcopy(self.inventory), 'pg03-restored-inventory.json': pg03,
            'docker-inspect.json': dict(Id='8'*64, HostConfig=dict(NetworkMode='none'), Mounts=[],
                                       Config=dict(Image=self.plan['postgres_image_ref']))}
        self.docs['backup-completion.json'] = dict(schema='clearapi.successor-backup-completion/v1',
            backup_id=self.plan['backup_id'], manifest_sha256=self.sha(encoded(self.manifest)),
            stopped_writer_identity=identity, invocation_returncode=0, plaintext_cleanup='verified_absent',
            encrypted_sha256=self.manifest['encryption']['encrypted_artifact']['sha256'],
            encrypted_bytes=self.manifest['encryption']['encrypted_artifact']['bytes'])
        self.write('stdout.txt', b'')
        self.write('stderr.txt', b'')
        self.write('decrypt-status.txt', b'[GNUPG:] DECRYPTION_OKAY\n')
        outcome = dict(returncode=0, started_at_utc='2026-10-21T00:20:00Z',
            completed_at_utc='2026-10-21T00:21:00Z', stdout_file='stdout.txt', stderr_file='stderr.txt',
            stdout_sha256=self.sha(b''), stderr_sha256=self.sha(b''), argv=[])
        commands = {name: copy.deepcopy(outcome) for name in ('decrypt', 'restore_main', 'cleanup')}
        commands['decrypt'].update(completed_at_utc='2026-10-21T00:20:01Z',
            stdout_file='decrypt-status.txt', stdout_sha256=self.sha(b'[GNUPG:] DECRYPTION_OKAY\n'),
            argv=['gpg', '--batch', '--no-tty', '--status-fd=1', '--output', 'bundle.tar.gz', '--decrypt', 'encrypted.gpg'])
        commands['restore_main']['started_at_utc'] = '2026-10-21T00:20:02Z'
        commands['cleanup'].update(started_at_utc='2026-10-21T00:21:01Z',
            completed_at_utc='2026-10-21T00:21:02Z', argv=['docker', 'rm', '-f', '8'*64])
        commands['restore_main']['argv'] = ['docker', 'exec', '8'*64, 'pg_restore', '--exit-on-error',
            '--no-owner', '--no-acl', '-U', 'postgres', '--dbname=clearapi_restore_fixture', '/restore/main.dump']
        self.docs['restore-run.json'] = dict(schema='clearapi.offline-restore-run/v1',
            started_at_utc='2026-10-21T00:19:00Z', completed_at_utc='2026-10-21T00:22:00Z',
            challenge='7'*64, candidate=self.candidate, backup_completion_sha256=self.sha(encoded(
                self.docs['backup-completion.json'])), postgres_version_num='170006', private_key_recorded=False,
            production_connections=[], cleanup_remaining_containers=[], commands=commands)
        self.pin()

    def write(self, name, value):
        (self.root / name).write_bytes(value)

    def pin(self):
        for name, value in self.docs.items():
            self.write(name, encoded(value))
        self.plan['evidence_sha256'] = {name:self.sha(encoded(value)) for name, value in self.docs.items()}
        self.write('restore-plan.json', encoded(self.plan))
        self.plan_hash = self.sha(encoded(self.plan))

    def test_consistent_content_does_not_authenticate_or_authorize(self):
        result = AUDIT.audit(self.root, self.plan_hash, self.now)
        self.assertEqual(result['content_status'], 'PASS')
        self.assertEqual(result['production_readiness'], 'BLOCKED')
        self.assertEqual(result['producer_authentication'], 'UNKNOWN')
        self.assertIs(result['production_execution_allowed'], False)
        self.assertNotEqual(result['schema'], 'clearapi.successor-restore-acceptance/v1')

    def test_pinned_documents_cannot_drift(self):
        self.write('restored-inventory.json', b'{}')
        with self.assertRaisesRegex(ValueError, 'hash mismatch'):
            AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_restore_approval_can_follow_completed_backup_without_backdating(self):
        self.plan['issued_at_utc'] = '2026-10-21T00:20:01Z'
        self.pin()
        self.assertEqual(AUDIT.audit(self.root, self.plan_hash, self.now)['content_status'], 'PASS')
        self.manifest['created_at_utc'] = '2026-10-19T00:10:00Z'
        self.pin()
        with self.assertRaisesRegex(ValueError, 'recovery point mismatch'):
            AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_expiry_replay_and_candidate_drift(self):
        for mutate in (lambda: self.plan.update(expires_at_utc='2026-10-21T00:30:00Z'),
                       lambda: self.docs['restore-run.json'].update(challenge='0'*64),
                       lambda: self.docs['restore-run.json'].update(candidate={} )):
            with self.subTest(mutate=mutate):
                original_plan, original_docs = copy.deepcopy(self.plan), copy.deepcopy(self.docs)
                mutate()
                self.pin()
                with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)
                self.plan, self.docs = original_plan, original_docs

    def test_database_drift_and_incomplete_invariants(self):
        for key, value in (('indexes_sha256', 'a'*64), ('migration_state_sha256', 'b'*64),
                           ('uniqueness_violations', 1), ('unvalidated_constraints', 1),
                           ('sequences_sha256', 'c'*64), ('catalog_tables', [])):
            with self.subTest(key=key):
                self.docs['restored-inventory.json'] = copy.deepcopy(self.inventory)
                self.docs['restored-inventory.json']['main'][key] = value
                self.pin()
                with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_failed_or_unknown_restore_is_rejected(self):
        for value in (1, None, False, '0'):
            with self.subTest(value=value):
                self.docs['restore-run.json']['commands']['restore_main']['returncode'] = value
                self.pin()
                with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_target_cannot_be_production_or_networked(self):
        self.docs['docker-inspect.json']['HostConfig']['NetworkMode'] = 'bridge'
        self.pin()
        with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)
        self.docs['docker-inspect.json']['HostConfig']['NetworkMode'] = 'none'
        self.plan['production_container_ids'].append(self.plan['target_container_id'])
        self.pin()
        with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_missing_log_dump_and_pg03_drift_are_rejected(self):
        self.plan.update(log_layout='separate')
        self.plan['databases']['log'] = dict(target_database='clearapi_restore_log', dump_artifact='log_database_dump')
        self.pin()
        with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)
        del self.plan['databases']['log']
        self.plan['log_layout'] = 'shared'
        self.docs['pg03-restored-inventory.json'] = {}
        self.pin()
        with self.assertRaisesRegex(ValueError, 'PG03'): AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_unsafe_archive_names_links_and_duplicates_are_rejected(self):
        for name in ('../escape', '/absolute', 'a/../b', 'C:/escape', 'back\\slash'):
            with self.subTest(name=name), self.assertRaises(ValueError): AUDIT.safe_name(name)
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode='w') as tar:
            link = tarfile.TarInfo('safe')
            link.type, link.linkname = tarfile.SYMTYPE, 'elsewhere'
            tar.addfile(link)
        with tarfile.open(fileobj=io.BytesIO(raw.getvalue())) as tar:
            with self.assertRaises(ValueError): list(AUDIT.archive_members(tar))
        raw = archive({'safe': b'x'})
        with tarfile.open(fileobj=io.BytesIO(raw)) as tar:
            members = [*tar.getmembers(), *tar.getmembers()]
            with self.assertRaises(ValueError): list(AUDIT.archive_members(members))

    def test_ciphertext_and_plaintext_payload_hash_mismatch(self):
        self.write('encrypted.gpg', b'tampered')
        with self.assertRaisesRegex(ValueError, 'ciphertext'): AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_shape_only_success_and_duplicate_json_fail(self):
        self.docs['restore-run.json']['commands'] = {'real_pg_restore':'pass'}
        self.pin()
        with self.assertRaises(ValueError): AUDIT.audit(self.root, self.plan_hash, self.now)
        self.write('restore-plan.json', b'{"schema":1,"schema":2}')
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            AUDIT.document(self.root, 'restore-plan.json')

    def test_nested_payload_hash_and_archive_inventory(self):
        self.payloads['globals.sql'] = b'changed without manifest update'
        bundle = archive(self.payloads)
        self.write('bundle.tar.gz', bundle)
        self.manifest['bundle']['sha256'] = self.sha(bundle)
        self.docs['backup-completion.json']['manifest_sha256'] = self.sha(encoded(self.manifest))
        self.docs['restore-run.json']['backup_completion_sha256'] = self.sha(encoded(self.docs['backup-completion.json']))
        self.pin()
        with self.assertRaisesRegex(ValueError, 'artifact hash mismatch'):
            AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_success_status_without_real_restore_argv_is_rejected(self):
        self.docs['restore-run.json']['commands']['restore_main']['argv'] = ['echo', 'pass']
        self.pin()
        with self.assertRaisesRegex(ValueError, 'non-restore'):
            AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_decrypt_status_and_command_output_tampering(self):
        self.write('decrypt-status.txt', b'[GNUPG:] DECRYPTION_FAILED\n')
        self.docs['restore-run.json']['commands']['decrypt']['stdout_sha256'] = self.sha(
            b'[GNUPG:] DECRYPTION_FAILED\n')
        self.pin()
        with self.assertRaisesRegex(ValueError, 'decrypt integrity'):
            AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_missing_material_is_not_synthesized(self):
        (self.root / 'restored-inventory.json').unlink()
        with self.assertRaises(OSError): AUDIT.audit(self.root, self.plan_hash, self.now)

    def test_missing_configuration_and_pg03_roots_fail(self):
        self.payloads['config_archive.tar.gz'] = archive({})
        bundle = archive(self.payloads)
        self.write('bundle.tar.gz', bundle)
        self.manifest['bundle']['sha256'] = self.sha(bundle)
        self.manifest['artifacts']['config_archive']['sha256'] = self.sha(self.payloads['config_archive.tar.gz'])
        self.docs['backup-completion.json']['manifest_sha256'] = self.sha(encoded(self.manifest))
        self.docs['restore-run.json']['backup_completion_sha256'] = self.sha(encoded(self.docs['backup-completion.json']))
        self.pin()
        with self.assertRaisesRegex(ValueError, 'coverage incomplete'):
            AUDIT.audit(self.root, self.plan_hash, self.now)


class FakeOfflineCommands:
    def __init__(self, spec):
        self.spec, self.calls, self.alive = spec, [], False
        self.break_network, self.fail_restore = False, False
        self.temporary_server_reads = 0

    def run(self, argv, stdin=None, hash_only=False):
        self.calls.append((argv, stdin))
        args = argv[1:]
        stdout, stderr, code = b'', b'', 0
        if args[:3] == ['ps', '-aq', '--no-trunc']:
            stdout = b'8'*64+b'\n' if self.alive else b''
        elif args[:2] == ['image', 'inspect']:
            stdout = encoded([dict(Id='sha256:'+'9'*64, RepoDigests=[RUNNER.PG_IMAGE])])
        elif args[0] == 'create':
            self.alive, stdout = True, b'8'*64+b'\n'
        elif args[0] == 'inspect':
            stdout = encoded([dict(Id='8'*64, Image='sha256:'+'9'*64,
                Config=dict(Image=RUNNER.PG_IMAGE, Labels={'clearapi.offline-restore':self.spec['authorization_id']}),
                Mounts=[], HostConfig=dict(NetworkMode='bridge' if self.break_network else 'none',
                    Binds=None, PortBindings={}, Privileged=False, PidMode='', Devices=[],
                    Tmpfs={name:'rw,noexec,nosuid,nodev,mode=0700' for name in ('/var/lib/postgresql/data','/restore')}))])
        elif args[:2] == ['rm', '-f']:
            self.alive = False
        elif args[0] == 'exec' and 'pg_restore' in args:
            code = 1 if self.fail_restore else 0
        elif args[0] == 'exec' and 'pg_dump' in args:
            stdout = b'-- schema fixture\n'
        elif args[0] == 'exec' and args[-2:] == ['cat', '/proc/1/comm']:
            stdout = b'bash\n' if self.temporary_server_reads else b'postgres\n'
            self.temporary_server_reads = max(0, self.temporary_server_reads-1)
        elif args[:2] == ['exec', '-i']:
            sql = stdin.decode().removeprefix(RUNNER.SESSION)
            if sql == 'SHOW server_version_num;': stdout = b'170006\n'
            elif "c.relkind IN ('r','p','m')" in sql: stdout = b'["public.options"]\n'
            elif "c.relkind='f'" in sql: stdout = b'0\n'
            elif sql == 'SELECT count(*) FROM public.options;': stdout = b'1\n'
            elif sql.startswith('COPY '): stdout = b'{"key": "bootstrap.completion_marker", "value": "fixture"}\n'
            elif 'SELECT n.nspname,c.relname,pg_get_indexdef' in sql: stdout = b'index-definition\n'
            elif 'SELECT n.nspname,c.relname,k.conname' in sql: stdout = b'constraint-definition\n'
            elif 'FROM pg_sequences' in sql: stdout = b'[]\n'
            elif "to_regclass('public.options')" in sql: stdout = b't\n'
            elif sql.startswith('SELECT to_jsonb(t)'): stdout = b'{"key": "bootstrap.completion_marker", "value": "fixture"}\n'
            elif 'json_build_object(\'validated\'' in sql: stdout = b'{"validated":1,"unvalidated":0}\n'
            elif 'AND (NOT i.indisvalid' in sql: stdout = b'0\n'
            elif 'WHERE i.indisunique' in sql: stdout = b'["public.options_pkey"]\n'
            elif sql.startswith('REINDEX INDEX '): stdout = b''
            else: raise AssertionError('unexpected SQL: '+sql)
        elif args[0] not in ('start', 'cp', 'exec'):
            raise AssertionError('unexpected command')
        index = len(self.calls)
        return dict(argv=argv, returncode=code, started_at_utc=f'2026-10-21T00:30:{index:02d}Z',
            completed_at_utc=f'2026-10-21T00:30:{index:02d}.001Z', stdout=b'' if hash_only else stdout,
            stderr=stderr, stdout_sha256=AUDIT.digest(stdout), stderr_sha256=AUDIT.digest(stderr),
            stdout_bytes=len(stdout), stderr_bytes=len(stderr))


class OfflineRunnerTest(unittest.TestCase):
    def setUp(self):
        self.fixture = RestoreAuditTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.spec = {k:v for k,v in self.fixture.plan.items() if k not in ('evidence_sha256','target_container_id')}
        self.spec.update(schema='clearapi.offline-restore-execute-spec/v1', authorization_id='a'*32,
                         postgres_image_ref=RUNNER.PG_IMAGE, input_sha256={})

    def test_default_plan_has_no_transport_and_cannot_authorize(self):
        RUNNER.validate_spec(self.prepare_spec(), self.fixture.now)
        plan = RUNNER.command_plan(self.spec)
        self.assertEqual(plan['mode'], 'PLAN')
        self.assertEqual(plan['network'], 'none')
        self.assertIs(plan['production_execution_allowed'], False)

    def prepare_spec(self):
        fixture = self.fixture
        fixture.docs['source-inventory.json'] = dict(main=dict(catalog_tables=['public.options'],
            tables={'public.options':dict(rows=1, rows_sha256=AUDIT.digest(
                b'{"key": "bootstrap.completion_marker", "value": "fixture"}\n'))},
            schema_sha256=AUDIT.digest(b'-- schema fixture\n'), indexes_sha256=AUDIT.digest(b'index-definition\n'),
            constraints_sha256=AUDIT.digest(b'constraint-definition\n'), sequences_sha256=AUDIT.digest(RUNNER.encoded([])),
            migration_state_sha256=AUDIT.digest(b'{"key": "bootstrap.completion_marker", "value": "fixture"}\n'),
            validated_constraints=1, unvalidated_constraints=0, uniqueness_violations=0))
        predecrypt = dict(schema='clearapi.offline-predecryption/v1', challenge=self.spec['challenge'],
            candidate=self.spec['candidate'], backup_completion_sha256=AUDIT.digest(encoded(fixture.docs['backup-completion.json'])),
            command=fixture.docs['restore-run.json']['commands']['decrypt'])
        fixture.docs['predecryption-run.json'] = predecrypt
        for name in ('backup-manifest.json','backup-completion.json','source-inventory.json','predecryption-run.json'):
            fixture.write(name, encoded(fixture.docs[name]))
            self.spec['input_sha256'][name] = AUDIT.digest(encoded(fixture.docs[name]))
        return self.spec

    def test_invalid_candidate_and_expired_spec_fail_before_commands(self):
        self.prepare_spec()
        self.spec['candidate']['image_digest'] = 'latest'
        with self.assertRaises(ValueError): RUNNER.validate_spec(self.spec, self.fixture.now)
        self.spec['candidate']['image_digest'] = 'sha256:'+'b'*64
        self.spec['expires_at_utc'] = '2026-10-21T00:10:00Z'
        with self.assertRaises(ValueError): RUNNER.validate_spec(self.spec, self.fixture.now)

    def test_execute_without_signature_authorization_runs_no_process(self):
        self.prepare_spec()
        path = self.fixture.root/'execute-spec.json'
        path.write_bytes(encoded(self.spec))
        argv = ['pg03-offline-restore.py', 'execute', '--spec', str(path), '--spec-sha256', AUDIT.digest(path.read_bytes())]
        output = io.StringIO()
        with patch.object(RUNNER, 'now', return_value=self.fixture.now), patch.object(sys, 'argv', argv), \
             patch.object(sys, 'stdout', output), patch.object(RUNNER.subprocess, 'Popen', side_effect=AssertionError('process forbidden')):
            self.assertEqual(RUNNER.main(), 1)
        self.assertEqual(json.loads(output.getvalue())['status'], 'BLOCKED')

    @unittest.skipIf(os.name == 'nt', 'Linux root runner; Windows validates offline contracts only')
    def test_actual_runner_uses_owned_commands_collects_data_and_audits(self):
        self.prepare_spec()
        output = self.fixture.root/'result'
        output.mkdir(mode=0o700)
        transport = FakeOfflineCommands(self.spec)
        transport.temporary_server_reads = 1  # temporary server already answers pg_isready
        runner = RUNNER.OfflineRestore(self.spec, transport, self.fixture.root, output, lambda:None)
        pg03 = self.fixture.docs['pg03-restored-inventory.json']
        with patch.object(RUNNER, 'now', return_value=self.fixture.now), \
             patch.object(RUNNER.STATE, 'restore', side_effect=lambda a,s,d:d.mkdir(mode=0o700)), \
             patch.object(RUNNER.STATE, 'inventory', return_value=pg03):
            result = runner.run()
        self.assertEqual(result['content_status'], 'PASS')
        self.assertEqual(result['production_readiness'], 'BLOCKED')
        self.assertFalse(transport.alive)
        sql = [stdin for _, stdin in transport.calls if stdin]
        self.assertTrue(any(b'REINDEX INDEX public.options_pkey;' in query for query in sql))
        commands = [argv for argv,_ in transport.calls]
        self.assertEqual(sum('pg_restore' in argv for argv in commands), 1)
        self.assertTrue(all(argv[0] == '/usr/bin/docker' for argv in commands))
        self.assertFalse(any('gpg' in argv or 'ssh' in argv for argv in commands))
        process_probes = [i for i, argv in enumerate(commands) if argv[-2:] == ['cat', '/proc/1/comm']]
        first_sql = next(i for i, (_, stdin) in enumerate(transport.calls) if stdin)
        self.assertEqual(len(process_probes), 2)
        self.assertGreater(first_sql, process_probes[1], 'temporary initdb server must never admit SQL')

    @unittest.skipIf(os.name == 'nt', 'Linux root runner; Windows validates offline contracts only')
    def test_network_drift_stops_before_start_or_restore(self):
        self.prepare_spec()
        output = self.fixture.root/'result'
        output.mkdir(mode=0o700)
        transport = FakeOfflineCommands(self.spec)
        transport.break_network = True
        runner = RUNNER.OfflineRestore(self.spec, transport, self.fixture.root, output, lambda:None)
        with self.assertRaisesRegex(ValueError, 'isolation drift'): runner.run()
        self.assertFalse(any('start' in argv or 'pg_restore' in argv for argv,_ in transport.calls))

    @unittest.skipIf(os.name == 'nt', 'Linux root runner; Windows validates offline contracts only')
    def test_nonzero_restore_never_generates_success(self):
        self.prepare_spec()
        output = self.fixture.root/'result'
        output.mkdir(mode=0o700)
        transport = FakeOfflineCommands(self.spec)
        transport.fail_restore = True
        runner = RUNNER.OfflineRestore(self.spec, transport, self.fixture.root, output, lambda:None)
        with self.assertRaisesRegex(ValueError, 'subprocess failed'): runner.run()
        self.assertFalse((output/'audit-result.json').exists())

    @unittest.skipIf(os.name == 'nt', 'Linux durable one-use claims')
    def test_claim_cannot_be_replayed_or_overwritten(self):
        claim = self.fixture.root/'claim.json'
        RUNNER.write_file(claim, b'{"state":"claimed"}')
        with self.assertRaises(FileExistsError): RUNNER.write_file(claim, b'{"state":"retry"}')
        self.assertEqual(claim.read_bytes(), b'{"state":"claimed"}')

    @unittest.skipIf(os.name == 'nt', 'POSIX bounded subprocess transport')
    def test_transport_hashes_rows_without_retaining_plaintext(self):
        with patch.object(RUNNER, 'DOCKER', sys.executable):
            transport = RUNNER.LocalCommands(lambda:None)
            result = transport.run([sys.executable, '-c', 'print("synthetic row")'], hash_only=True)
        self.assertEqual(result['stdout'], b'')
        self.assertEqual(result['stdout_sha256'], AUDIT.digest(b'synthetic row\n'))
        self.assertEqual(result['returncode'], 0)

    @unittest.skipIf(os.name == 'nt', 'POSIX nonblocking stdin and process-group containment')
    def test_nonreading_stdin_cannot_block_lease_expiry(self):
        checks = []
        def lease():
            checks.append(True)
            if len(checks) == 5:
                raise ValueError('fixture lease expired')
        with patch.object(RUNNER, 'DOCKER', sys.executable):
            transport = RUNNER.LocalCommands(lease)
            with self.assertRaisesRegex(ValueError, 'lease expired'):
                transport.run([sys.executable, '-c', 'import time; time.sleep(60)'], stdin=b'x'*1000000)


@unittest.skipUnless(os.name == 'posix' and getattr(os, 'geteuid', lambda: -1)() == 0,
                     'Linux root detached-signature entrypoint contract')
class OfflineExecuteAuthorizationTest(unittest.TestCase):
    """Real temporary OpenSSL signatures; fake Docker and mount metadata only."""
    def setUp(self):
        self.fixture = RestoreAuditTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.root = self.fixture.root
        self.instant = self.fixture.now
        self.openssl = shutil.which('openssl')
        self.assertIsNotNone(self.openssl, 'OpenSSL is required for this authorization contract')
        self.key, self.public = self.root/'synthetic-signing.key', self.root/'synthetic-signing.pub'
        self.signing(['genpkey', '-algorithm', 'ED25519', '-out', str(self.key)])
        self.key.chmod(0o600)
        self.signing(['pkey', '-in', str(self.key), '-pubout', '-out', str(self.public)])
        self.public.chmod(0o600)
        self.fake_docker = self.root/'fixture-docker'
        RUNNER.write_file(self.fake_docker, b'fixture only; never executed')
        self.installed = self.root/'installed'
        self.installed.mkdir(mode=0o700)
        for name in ('pg03-offline-restore.py','pg03-restore-acceptance.py','pg03-backup-state.py'):
            RUNNER.write_file(self.installed/name,(Path(RUNNER.__file__).parent/name).read_bytes())
        self.inputs, self.outputs, self.claims = (self.root/name for name in ('inputs','outputs','claims'))
        for directory in (self.inputs, self.outputs, self.claims): directory.mkdir(mode=0o700)
        self.spec = {k:v for k,v in self.fixture.plan.items() if k not in ('evidence_sha256','target_container_id')}
        self.spec.update(schema='clearapi.offline-restore-execute-spec/v1', authorization_id='a'*32,
            postgres_image_ref=RUNNER.PG_IMAGE, input_sha256=dict.fromkeys(
                ('backup-manifest.json','backup-completion.json','source-inventory.json','predecryption-run.json'), 'b'*64))
        (self.inputs/self.spec['authorization_id']).mkdir(mode=0o700)
        self.spec_path, self.authorization, self.signature = (self.root/name for name in ('spec.json','authorization.json','authorization.sig'))
        RUNNER.write_file(self.spec_path, RUNNER.encoded(self.spec))
        self.auth = dict(schema='clearapi.offline-restore-authorization/v1', approved_by='a3225865526-wq',
            action='independent_offline_restore_only', spec_sha256=AUDIT.digest(self.spec_path.read_bytes()),
            authorization_id=self.spec['authorization_id'], issued_at_utc='2026-10-21T00:59:00Z',
            expires_at_utc='2026-10-21T01:30:00Z')
        self.policy_path = self.root/'policy.json'
        policy = dict(schema='clearapi.offline-restore-policy/v1', environment='isolated_offline_restore',
            public_key_file=str(self.public), machine_id_sha256=AUDIT.digest(b'synthetic-machine-id'),
            public_key_sha256=AUDIT.digest(self.public.read_bytes()),
            docker_binary_sha256=AUDIT.digest(self.fake_docker.read_bytes()),
            openssl_binary_sha256=AUDIT.file_hash(Path(self.openssl).parent,Path(self.openssl).name)[0],
            runner_sha256=AUDIT.file_hash(Path(RUNNER.__file__).parent,Path(RUNNER.__file__).name)[0],
            module_sha256={name:AUDIT.file_hash(Path(RUNNER.__file__).parent,name)[0]
                for name in ('pg03-restore-acceptance.py','pg03-backup-state.py')}, offline_daemon_id='synthetic-offline-daemon')
        RUNNER.write_file(self.policy_path,RUNNER.encoded(policy))
        self.sign_authorization()
        self.docker_calls = []

    def signing(self, arguments):
        result = subprocess.run([self.openssl,*arguments],stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=False)
        self.assertEqual(result.returncode,0,'temporary synthetic OpenSSL operation failed')

    def sign_authorization(self):
        self.authorization.write_bytes(RUNNER.encoded(self.auth))
        self.authorization.chmod(0o600)
        self.signing(['pkeyutl','-sign','-rawin','-inkey',str(self.key),'-in',str(self.authorization),'-out',str(self.signature)])
        self.signature.chmod(0o600)

    def execute(self):
        args = SimpleNamespace(policy=str(self.policy_path), policy_sha256=AUDIT.digest(self.policy_path.read_bytes()),
            spec=self.spec_path,spec_sha256=AUDIT.digest(self.spec_path.read_bytes()), authorization=self.authorization,
            authorization_sha256=AUDIT.digest(self.authorization.read_bytes()),signature=self.signature)
        original_run = RUNNER.LocalCommands.run
        original_read_bytes, original_read_text = Path.read_bytes, Path.read_text
        def read_bytes(path):
            return b'synthetic-machine-id' if str(path)=='/etc/machine-id' else original_read_bytes(path)
        def read_text(path,*a,**kw):
            if str(path)=='/proc/self/mountinfo':
                return ('1 0 0:1 / / rw - ext4 fixture rw\n'
                    +'2 1 0:2 / '+str(self.inputs)+' rw - tmpfs fixture rw\n'
                    +'3 1 0:3 / '+str(self.outputs)+' rw - tmpfs fixture rw\n')
            return original_read_text(path,*a,**kw)
        def run(transport,argv,*a,**kw):
            if argv[0] == self.openssl:
                return original_run(transport,argv,*a,**kw)
            self.docker_calls.append(argv)
            self.assertEqual(argv,[str(self.fake_docker),'info','--format','{{.ID}}'])
            return dict(returncode=0,stdout=b'synthetic-offline-daemon\n')
        with patch.multiple(RUNNER,POLICY_PATH=self.policy_path,PUBLIC_KEY=self.public,INPUT_ROOT=self.inputs,
                OUTPUT_ROOT=self.outputs,CLAIM_ROOT=self.claims,DOCKER=str(self.fake_docker),OPENSSL=self.openssl,
                __file__=str(self.installed/'pg03-offline-restore.py')), \
             patch.object(RUNNER,'now',return_value=self.instant), patch.object(Path,'read_bytes',read_bytes), \
             patch.object(Path,'read_text',read_text), patch.object(RUNNER.LocalCommands,'run',run), \
             patch.object(RUNNER.OfflineRestore,'run',return_value=dict(status='LOCAL_SIGNATURE_CONTRACT_ONLY')):
            return RUNNER.execute(args)

    def test_actual_signature_allows_one_claim_and_replay_cannot_replace_it(self):
        self.assertEqual(self.execute()['status'],'LOCAL_SIGNATURE_CONTRACT_ONLY')
        claim = self.claims/(self.spec['authorization_id']+'.json')
        original = claim.read_bytes()
        with self.assertRaises(FileExistsError): self.execute()
        self.assertEqual(claim.read_bytes(),original)

    def test_invalid_detached_signature_and_changed_signed_authorization_fail_before_docker(self):
        signature = self.signature.read_bytes()
        self.signature.write_bytes(bytes([signature[0]^1])+signature[1:])
        with self.assertRaisesRegex(ValueError,'signature rejected'): self.execute()
        self.assertFalse(list(self.claims.iterdir()))
        self.assertEqual(self.docker_calls,[])
        self.signature.write_bytes(signature)
        self.auth['authorization_id']='c'*32
        self.authorization.write_bytes(RUNNER.encoded(self.auth))
        with self.assertRaisesRegex(ValueError,'authorization mismatch'): self.execute()
        self.assertFalse(list(self.claims.iterdir()))

    def test_expired_or_wrong_scope_real_signed_authorization_cannot_claim(self):
        for field,value in (('expires_at_utc','2026-10-21T00:59:59Z'),('action','production_deploy')):
            with self.subTest(field=field):
                original = copy.deepcopy(self.auth)
                self.auth[field]=value
                self.sign_authorization()
                with self.assertRaisesRegex(ValueError,'authorization mismatch'): self.execute()
                self.assertFalse(list(self.claims.iterdir()))
                self.assertEqual(self.docker_calls,[])
                self.auth=original


class SyntheticPostgresCommands(RUNNER.LocalCommands):
    """Diagnostics only for fresh hosted synthetic databases, never real data."""
    def run(self, argv, **kwargs):
        result = super().run(argv, **kwargs)
        if result['returncode'] != 0:
            print('Synthetic PostgreSQL command failed:', result['returncode'],
                  result['stderr'][:4096].decode(errors='replace'))
        return result


@unittest.skipUnless(os.environ.get('PG03_REAL_OFFLINE_RESTORE') == 'true', 'opt-in hosted nonproduction PostgreSQL test')
class RealPostgresRestoreTest(unittest.TestCase):
    def test_real_custom_dump_restore_main_log_full_inventory(self):
        self.assertEqual(os.environ.get('GITHUB_ACTIONS'), 'true', 'hosted CI only')
        self.assertEqual(os.environ.get('RUNNER_ENVIRONMENT'), 'github-hosted', 'self-hosted hosts forbidden')
        self.assertEqual(os.environ.get('GITHUB_REPOSITORY'), 'a3225865526-wq/Clear-API')
        self.assertEqual(os.geteuid(), 0)
        self.assertFalse(Path('/opt/clearapi/app/release-manifest.json').exists())
        self.assertFalse(Path('/data/clearapi').exists())
        deadline = time.monotonic()+900
        def lease():
            if time.monotonic() >= deadline:
                raise ValueError('synthetic restore test deadline')
        transport = SyntheticPostgresCommands(lease)
        pull = transport.run([RUNNER.DOCKER, 'pull', RUNNER.PG_IMAGE])
        self.assertEqual(pull['returncode'], 0, 'pinned public PostgreSQL image unavailable')
        nonce = os.urandom(16).hex()
        spec = dict(authorization_id=nonce, production_container_ids=['f'*64], challenge='c'*64)
        temporary = tempfile.TemporaryDirectory(prefix='clearapi-synthetic-restore-')
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        runners = []
        try:
            for suffix in ('source','restored'):
                output = root/suffix
                output.mkdir(mode=0o700)
                runner = RUNNER.OfflineRestore(spec, transport, root, output, lease)
                runners.append(runner)
                result = runner.call(['create', '--pull=never', '--name', 'clearapi-synthetic-'+nonce+'-'+suffix,
                    '--label', 'clearapi.offline-restore='+nonce, '--network', 'none',
                    '--tmpfs', '/var/lib/postgresql/data:rw,noexec,nosuid,nodev,mode=0700',
                    '--tmpfs', '/restore:rw,noexec,nosuid,nodev,mode=0700',
                    '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', RUNNER.PG_IMAGE])
                runner.cid = result['stdout'].decode().strip()
                runner.inspect_target()
                runner.call(['start',runner.cid])
                for attempt in range(60):
                    process = runner.call(['exec',runner.cid,'cat','/proc/1/comm'],allow_failure=True)
                    ready = runner.call(['exec',runner.cid,'pg_isready','-U','postgres'],allow_failure=True)
                    if process['returncode'] == 0 and process['stdout'].strip() == b'postgres' and ready['returncode'] == 0:
                        break
                    self.assertLess(attempt,59,'synthetic PostgreSQL not ready')
                    time.sleep(0.2)
                self.assertEqual(runner.sql('postgres','SHOW server_version_num;').strip(),b'170006')
            source, target = runners
            baseline, restored = {}, {}
            seeds = dict(main="""
                CREATE TABLE public.options(key text PRIMARY KEY,value text NOT NULL);
                INSERT INTO public.options VALUES ('bootstrap.completion_marker','synthetic-only'),('SchemaVersion','9');
                CREATE TABLE public.accounts(id bigserial PRIMARY KEY,name text UNIQUE);
                CREATE UNIQUE INDEX accounts_lower_name ON public.accounts(lower(name)) WHERE name IS NOT NULL;
                INSERT INTO public.accounts(name) VALUES ('synthetic-alpha'),('synthetic-beta');
                CREATE SEQUENCE public.entry_seq;
                CREATE TABLE public.entries(id bigint PRIMARY KEY DEFAULT nextval('public.entry_seq'),
                    account_id bigint REFERENCES public.accounts(id),amount numeric CHECK(amount>=0),
                    payload jsonb,created_at timestamptz NOT NULL);
                CREATE INDEX entries_account ON public.entries(account_id);
                INSERT INTO public.entries(id,account_id,amount,payload,created_at)
                    VALUES (7,1,12.50,'{"fixture":true}','2026-10-21T00:00:00Z');
                SELECT setval('public.entry_seq',41,false);
                """, log="""
                CREATE TABLE public.logs(id bigserial PRIMARY KEY,request_id text UNIQUE,message text NOT NULL);
                CREATE INDEX logs_message ON public.logs(message);
                INSERT INTO public.logs(request_id,message) VALUES('synthetic-request','isolated synthetic event');
                SELECT setval('public.logs_id_seq',19,true);
                """)
            for logical, seed in seeds.items():
                source_db, target_db = 'clearapi_restore_source_'+logical,'clearapi_restore_target_'+logical
                source.call(['exec',source.cid,'createdb','-U','postgres',source_db])
                source.sql(source_db,seed)  # writes only to this newly owned synthetic source
                baseline[logical] = source.inventory(source_db)
                dumped = source.call(['exec',source.cid,'pg_dump','-U','postgres','-d',source_db,'-Fc'])
                path = root/(logical+'.dump')
                RUNNER.write_file(path,dumped['stdout'])
                self.assertTrue(path.read_bytes().startswith(b'PGDMP'))
                target.call(['cp',str(path),target.cid+':/restore/'+path.name])
                target.call(['exec',target.cid,'createdb','-U','postgres',target_db])
                target.call(['exec',target.cid,'pg_restore','--exit-on-error','--no-owner','--no-acl',
                    '-U','postgres','--dbname='+target_db,'/restore/'+path.name])
                restored[logical] = target.inventory(target_db)
            AUDIT.database_inventory(baseline, {'main':{},'log':{}})
            AUDIT.database_inventory(restored, {'main':{},'log':{}})
            self.assertEqual(restored,baseline,'full synthetic restore inventory drift')
            self.assertEqual(target.sql('clearapi_restore_target_main','SELECT last_value,is_called FROM public.entry_seq;').strip(),b'41|f')
            self.assertEqual(target.sql('clearapi_restore_target_log','SELECT last_value,is_called FROM public.logs_id_seq;').strip(),b'19|t')
        finally:
            for runner in reversed(runners):
                if runner.cid is not None:
                    runner.inspect_target()
                    runner.call(['rm','-f',runner.cid])
                    runner.cid = None


if __name__ == '__main__':
    unittest.main()
