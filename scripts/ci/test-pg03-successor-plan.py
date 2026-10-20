#!/usr/bin/env python3
"""Offline successor evidence binding and failure-closed regression cases."""
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile
from contextlib import contextmanager

sys.dont_write_bytecode = True
SCRIPTS = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('successor', SCRIPTS / 'pg03-successor-plan.py')
PLAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PLAN)
ACTION_SPEC = importlib.util.spec_from_file_location('isolated_actions', SCRIPTS / 'ci/pg03-isolated-rehearsal.py')
ACTION = importlib.util.module_from_spec(ACTION_SPEC)
ACTION_SPEC.loader.exec_module(ACTION)
TX_SPEC = importlib.util.spec_from_file_location('successor_transaction', SCRIPTS / 'pg03-successor-transaction.py')
TX = importlib.util.module_from_spec(TX_SPEC)
TX_SPEC.loader.exec_module(TX)


def negative_probe_fixture():
    return dict(status='expected_migration_failure_detected', enforced_read_only=True,
        sqlstate='25006', candidate_image=ACTION.IMAGE, cleanup_complete=True,
        success_receipt_eligible=False, proof_capture='unavailable_after_tmpfs_exit', log_sha256='f'*64,
        process=dict(Status='exited', Running=False, ExitCode=1, OOMKilled=False, error_present=False),
        cleanup=[dict(container_id=str(i)*64, removed=True) for i in (1, 2)])


class SuccessorPlanTest(unittest.TestCase):
    def setUp(self):
        self.now = datetime(2026, 10, 20, 12, tzinfo=timezone.utc)
        self.source = 'a' * 40
        self.digest = 'sha256:' + 'b' * 64
        self.schema_hash = 'c' * 64
        self.candidate = dict(source_commit=self.source, candidate_commit=self.source,
            oci_revision=self.source, workflow_commit=self.source, image_digest=self.digest,
            image_ref='ghcr.io/a3225865526-wq/clearapi@' + self.digest,
            new_api_image_id=self.digest, binary_sha256='d' * 64, workflow_run=123,
            workflow_run_attempt=1, platform='linux/amd64', deployment_compatible=True,
            runtime_identity=dict(image_id=self.digest, binary_path='/new-api', binary_sha256='d' * 64))
        self.observation = dict(observed_at_utc='2026-10-20T11:59:00Z', incident_run=PLAN.FAILED_RUN,
            frozen_sha256=PLAN.FROZEN.copy(),
            predecessor=dict(source_commit=PLAN.FAILED_COMMIT, image_digest=PLAN.FAILED_DIGEST,
                status='failed', deployment_attempt_status='failed', executor_start='forbidden'),
            gateway_state='exited_127_missing_runtime_directory', runtime_identity_dir='/run/clearapi',
            identity_mount=dict(source='/run/clearapi', target='/run/clearapi', read_only=True,
                create_host_path=False), executor_container_id=PLAN.EXECUTOR_CONTAINER,
            executor_state='running_healthy', ingress='isolated_stopped_restart_no',
            public_web_listeners=[], database_schema_sha256=self.schema_hash,
            previous_success=dict(source_commit=PLAN.PREVIOUS_COMMIT, image_digest=PLAN.PREVIOUS_DIGEST))
        self.schema = dict(schema_version=PLAN.SCHEMA.SCHEMA,
            postgres_image_ref=PLAN.SCHEMA.POSTGRES, postgres_version_num='170006',
            startup_passes=2, profile='isolated_schema_only_master_single_postgres',
            source_schema_sha256=self.schema_hash, target_schema_sha256=self.schema_hash,
            candidate_image_id=self.digest, candidate_binary_sha256='d' * 64, source_commit=self.source)
        self.receipts = {gate: json.dumps(dict(schema='clearapi.gate-evidence/v1', gate=gate,
            conclusion='success', source_commit=self.source, gateway_digest=self.digest,
            rc_run_id=123, rc_run_attempt=1)).encode() for gate in PLAN.GATES}

    def review(self):
        return PLAN.review(self.candidate, self.observation, self.schema, self.receipts, self.now)

    def test_matching_inputs_produce_only_an_offline_proposal_and_preserve_inputs(self):
        before = copy.deepcopy((self.candidate, self.observation, self.schema, self.receipts))
        # This function cannot invoke a shell, Docker, a database or a network client.
        with patch.object(subprocess, 'run', side_effect=AssertionError('no commands permitted')):
            result = self.review()
        self.assertIs(result['execution_allowed'], False)
        self.assertEqual(result['executor_start'], 'forbidden')
        self.assertEqual(result['public_traffic'], 'blocked')
        self.assertEqual(result['previous_success']['image_digest'], PLAN.PREVIOUS_DIGEST)
        self.assertEqual((self.candidate, self.observation, self.schema, self.receipts), before)
        self.assertTrue(result['remaining_gates'])

    def test_historical_rehearsal_target_is_not_a_rehearsal_from_current_database(self):
        # RC39357150402's actual transition, independently read from its artifact.
        self.observation['database_schema_sha256'] = '3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035'
        self.schema.update(
            source_schema_sha256='578bc6f83dfafb47056c74324d13833a6333152685f4500316b0cd67f88333fd',
            target_schema_sha256=self.observation['database_schema_sha256'])
        with self.assertRaisesRegex(ValueError, 'source schema'):
            self.review()

    def test_candidate_source_digest_binary_and_rc_identity_must_match(self):
        cases = [('oci_revision', 'f' * 40), ('image_ref', 'mutable:latest'),
                 ('image_digest', PLAN.PREVIOUS_DIGEST), ('source_commit', PLAN.FAILED_COMMIT),
                 ('binary_sha256', 'e' * 64), ('workflow_run', True),
                 ('workflow_run_attempt', True), ('deployment_compatible', 'true')]
        for key, value in cases:
            with self.subTest(key=key):
                original = self.candidate.copy()
                self.candidate[key] = value
                with self.assertRaises(ValueError):
                    self.review()
                self.candidate = original

    def test_stale_future_and_naive_observations_cannot_be_relabelled_fresh(self):
        for timestamp in ('2026-10-20T11:54:59Z', '2026-10-20T12:00:01Z',
                          '2026-10-20T12:00:00', None):
            with self.subTest(timestamp=timestamp):
                self.observation['observed_at_utc'] = timestamp
                with self.assertRaises(ValueError):
                    self.review()

    def test_failed_manifest_or_isolation_drift_and_old_identity_path_are_rejected(self):
        cases = [('frozen_sha256', {}), ('gateway_state', 'running'),
                 ('runtime_identity_dir', '/opt/clearapi/runtime/new-api'),
                 ('identity_mount', dict(self.observation['identity_mount'], create_host_path=True)),
                 ('executor_container_id', 'e' * 64), ('executor_state', 'stopped'),
                 ('ingress', 'open'), ('public_web_listeners', ['443']), ('previous_success', {})]
        for key, value in cases:
            with self.subTest(key=key):
                original = self.observation.copy()
                self.observation[key] = value
                with self.assertRaises(ValueError):
                    self.review()
                self.observation = original

    def test_failed_predecessor_must_not_be_rewritten_or_executor_start_enabled(self):
        for key, value in (('status', 'success'), ('deployment_attempt_status', 'success'),
                           ('executor_start', 'allowed')):
            with self.subTest(key=key):
                original = self.observation['predecessor'].copy()
                self.observation['predecessor'][key] = value
                with self.assertRaises(ValueError):
                    self.review()
                self.observation['predecessor'] = original

    def test_missing_schema_second_start_or_additional_migration_is_blocked(self):
        for key, value in (('startup_passes', 1), ('candidate_binary_sha256', 'e' * 64),
                           ('source_commit', 'f' * 40), ('target_schema_sha256', 'e' * 64)):
            with self.subTest(key=key):
                original = self.schema.copy()
                self.schema[key] = value
                with self.assertRaises(ValueError):
                    self.review()
                self.schema = original
        self.observation.pop('database_schema_sha256')
        with self.assertRaises(ValueError):
            self.review()

    def test_healthy_executor_cannot_substitute_for_authenticated_provider_accounting_gate(self):
        self.receipts.pop('gateway_executor_provider_usage_billing')
        with self.assertRaisesRegex(ValueError, 'complete candidate gate'):
            self.review()

    def test_numeric_mount_flags_and_fractional_startup_count_are_rejected(self):
        self.observation['identity_mount']['read_only'] = 1
        with self.assertRaisesRegex(ValueError, 'JSON booleans'):
            self.review()
        self.observation['identity_mount']['read_only'] = True
        self.schema['startup_passes'] = 2.0
        with self.assertRaisesRegex(ValueError, 'must be an integer'):
            self.review()

    def test_failed_stale_duplicate_or_bool_typed_receipts_are_blocked(self):
        gate = 'backup_restore'
        original = self.receipts[gate]
        for key, value in (('conclusion', 'not_run'), ('gateway_digest', PLAN.FAILED_DIGEST),
                           ('source_commit', PLAN.FAILED_COMMIT), ('rc_run_id', 122),
                           ('rc_run_attempt', True)):
            with self.subTest(key=key):
                receipt = json.loads(original)
                receipt[key] = value
                self.receipts[gate] = json.dumps(receipt).encode()
                with self.assertRaises(ValueError):
                    self.review()
        self.receipts[gate] = original[:-1] + b',"conclusion":"success"}'
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            self.review()

    def test_cli_missing_evidence_returns_failure_without_reflecting_input_or_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'candidate.json'
            path.write_text('{"secret":"SYNTHETIC_DO_NOT_REFLECT"}')
            before = path.read_bytes()
            result = subprocess.run([sys.executable, '-B', str(SCRIPTS / 'pg03-successor-plan.py'),
                                     '--bundle', directory], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn('SYNTHETIC_DO_NOT_REFLECT', result.stdout + result.stderr)
            self.assertEqual(list(Path(directory).iterdir()), [path])
            self.assertEqual(path.read_bytes(), before)


class SuccessorSchemaTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive = self.root / 'rc.zip'
        self.output = self.root / 'successor.json'
        self.sql = b'-- schema-only fixture\nCREATE TABLE example (id bigint);\n'
        self.schema_hash = PLAN.SCHEMA.schema_digest(self.sql.decode())
        self.source = 'a' * 40
        self.digest = 'sha256:' + 'b' * 64
        self.receipt = dict(schema_version=PLAN.SCHEMA.SCHEMA,
            postgres_image_ref=PLAN.SCHEMA.POSTGRES, postgres_version_num='170006',
            startup_passes=2, profile='isolated_schema_only_master_single_postgres',
            source_schema_sha256='c' * 64, target_schema_sha256=self.schema_hash,
            candidate_image_id=self.digest, candidate_binary_sha256='d' * 64, source_commit=self.source)
        self.manifest = dict(source_commit=self.source, candidate_commit=self.source,
            workflow_commit=self.source, oci_revision=self.source, image_digest=self.digest,
            image_ref='ghcr.io/a3225865526-wq/clearapi@' + self.digest,
            platform='linux/amd64', new_api_image_id=self.digest, binary_sha256='d' * 64,
            schema_transition=self.receipt)
        self.files = {'gateway-image-release.json': json.dumps(self.manifest).encode(),
            'schema-rehearsal.json': json.dumps(self.receipt).encode(),
            'schema-rehearsal.target.sql': self.sql, 'binary-build-info.txt': b'synthetic identity\n'}
        self.write_archive()

    def write_archive(self):
        with zipfile.ZipFile(self.archive, 'w') as archive:
            for name, raw in self.files.items():
                archive.writestr(name, raw)
            archive.writestr('artifact-hashes.json', json.dumps({
                name: hashlib.sha256(raw).hexdigest() for name, raw in self.files.items()}))
        self.archive_sha = hashlib.sha256(self.archive.read_bytes()).hexdigest()

    def baseline(self):
        return PLAN.successor_baseline(self.archive, self.archive_sha, self.schema_hash)

    def rehearse(self):
        return PLAN.rehearse_successor_schema(self.archive, self.archive_sha, self.schema_hash, self.output)

    def fake_startup(self, ref, source, schema, output, **options):
        self.assertEqual((ref, source), (self.manifest['image_ref'], self.source))
        self.assertEqual(schema.read_bytes(), self.sql)
        self.assertIs(options['require_migration_evidence'], True)
        self.assertEqual(options['expected_schema'], self.schema_hash)
        receipt = dict(self.receipt, source_schema_sha256=self.schema_hash)
        output.write_text(json.dumps(receipt))
        output.with_suffix('.target.sql').write_bytes(self.sql)
        return receipt

    def test_verified_rc_target_is_an_input_without_claiming_a_rehearsal_or_mutating_archive(self):
        before = self.archive.read_bytes()
        with patch.object(PLAN.SCHEMA, 'rehearse', side_effect=AssertionError('inspection must not start Docker')):
            candidate, sql = self.baseline()
        self.assertEqual(candidate['image_digest'], self.digest)
        self.assertEqual(sql, self.sql)
        self.assertEqual(self.archive.read_bytes(), before)
        self.assertFalse(self.output.exists())

    def test_external_archive_hash_and_internal_payload_hash_are_independent_gates(self):
        with self.assertRaisesRegex(ValueError, 'archive digest'):
            PLAN.successor_baseline(self.archive, 'f' * 64, self.schema_hash)
        # An inventory can remain parseable while its payload no longer matches.
        with zipfile.ZipFile(self.archive) as archive:
            saved = {name: archive.read(name) for name in archive.namelist()}
        saved['schema-rehearsal.target.sql'] += b'-- drift\n'
        with zipfile.ZipFile(self.archive, 'w') as archive:
            for name, raw in saved.items():
                archive.writestr(name, raw)
        self.archive_sha = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaisesRegex(ValueError, 'payload digest'):
            self.baseline()

    def test_unknown_zip_entry_schema_or_candidate_drift_never_starts_docker(self):
        self.files['unexpected.txt'] = b'extra'
        self.write_archive()
        with patch.object(PLAN.SCHEMA, 'rehearse') as startup:
            with self.assertRaisesRegex(ValueError, 'inventory'):
                self.rehearse()
            startup.assert_not_called()
        del self.files['unexpected.txt']
        self.manifest['oci_revision'] = 'e' * 40
        self.files['gateway-image-release.json'] = json.dumps(self.manifest).encode()
        self.write_archive()
        with self.assertRaisesRegex(ValueError, 'identity'):
            self.baseline()

    def test_current_schema_must_equal_actual_rc_target_and_sql_digest(self):
        with self.assertRaisesRegex(ValueError, 'supplied database schema'):
            PLAN.successor_baseline(self.archive, self.archive_sha, 'e' * 64)
        self.files['schema-rehearsal.target.sql'] = b'-- unexpected schema\n'
        self.write_archive()
        with self.assertRaisesRegex(ValueError, 'supplied database schema'):
            self.baseline()

    def test_isolated_result_is_published_only_after_exact_candidate_and_schema_checks(self):
        with patch.object(PLAN.SCHEMA, 'rehearse', side_effect=self.fake_startup) as startup:
            result = self.rehearse()
        startup.assert_called_once()
        self.assertEqual(json.loads(self.output.read_text()), result)
        self.assertEqual(self.output.with_suffix('.target.sql').read_bytes(), self.sql)
        self.assertFalse(result['successor_baseline']['production_observation_verified'])
        self.assertEqual(result['successor_baseline']['production_acceptance'], 'not_run')
        self.assertEqual(self.baseline()[0], self.manifest)

    def test_missing_image_or_failed_local_startup_publishes_no_successor_receipt(self):
        with patch.object(PLAN.SCHEMA, 'rehearse', side_effect=RuntimeError('image unavailable')):
            with self.assertRaises(RuntimeError):
                self.rehearse()
        self.assertFalse(self.output.exists())
        self.assertFalse(self.output.with_suffix('.target.sql').exists())

    def test_changed_schema_or_wrong_binary_after_startup_publishes_no_receipt(self):
        for key, value in (('target_schema_sha256', 'e' * 64), ('candidate_binary_sha256', 'e' * 64)):
            with self.subTest(key=key), patch.object(PLAN.SCHEMA, 'rehearse', return_value=dict(
                    self.receipt, source_schema_sha256=self.schema_hash, **{key: value})):
                with self.assertRaises(ValueError):
                    self.rehearse()
                self.assertFalse(self.output.exists())

    def test_existing_or_partial_output_is_preserved_and_cannot_be_replayed(self):
        self.output.with_suffix('.target.sql').write_bytes(b'original evidence')
        with patch.object(PLAN.SCHEMA, 'rehearse') as startup:
            with self.assertRaisesRegex(ValueError, 'preserve existing'):
                self.rehearse()
            startup.assert_not_called()
        self.assertEqual(self.output.with_suffix('.target.sql').read_bytes(), b'original evidence')


class IsolatedStartupTest(unittest.TestCase):
    """Docker substitutes verify failure/cleanup contracts, never real migration."""
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.sql = '-- synthetic schema\nCREATE TABLE example (id bigint);\n'
        self.schema = self.root / 'baseline.sql'
        self.schema.write_bytes(self.sql.encode())
        self.output = self.root / 'receipt.json'
        self.nonce = 'a' * 32
        self.image = 'sha256:' + 'b' * 64
        self.binary = 'c' * 64
        self.source = 'd' * 40
        self.calls = []
        self.created = []
        self.pass_number = 0
        self.fail_pass = None
        self.drift_pass = None
        self.fail_stop = False
        self.fail_cleanup = False
        self.process = dict(Status='exited', Running=False, ExitCode=0, OOMKilled=False, Error='')
        self.observation = {}

    def command(self, args, raw=None):
        self.calls.append(args)
        if args[:2] == ['image', 'inspect']:
            return json.dumps([dict(Id=self.image, Os='linux', Architecture='amd64',
                RepoDigests=[ACTION.IMAGE], Config=dict(Labels={
                    'org.opencontainers.image.revision': self.source,
                    'org.opencontainers.image.source': 'https://github.com/a3225865526-wq/Clear-API'}))])
        if args[0] == 'create':
            cid = format(len(self.created)+1, '064x')
            self.created.append(cid)
            if '--entrypoint' in args and args[args.index('--entrypoint')+1] == '/new-api':
                self.pass_number += 1
            return cid
        if args[:2] == ['start', '-a']:
            return self.binary + '  /new-api'
        if args[0] == 'start':
            return args[-1]
        if args[0] == 'exec':
            if args[-1] == 'hostname': return args[1][:12]
            if 'pg_dump' in args:
                return self.sql + ('-- drift\n' if self.drift_pass == self.pass_number else '')
            if 'pg_isready' in args:
                return 'accepting connections'
            if 'psql' in args:
                if 'SHOW server_version_num' in args:
                    return '170006'
                self.assertEqual(raw, self.sql.encode())
                return ''
            if 'wget' in args:
                return json.dumps(dict(success=self.fail_pass != self.pass_number))
        if args[0] == 'inspect':
            if args[2] == '{{.Config.Hostname}}': return args[-1][:12]
            if args[2] == '{{json .HostConfig.LogConfig}}':
                return json.dumps(dict(Type='json-file', Config=dict(mode='blocking')))
            if args[2] == '{{.State.Running}}':
                return 'false'
            if args[2] == '{{json .State}}':
                return json.dumps(self.process)
            return self.nonce
        if args[0] == 'stop':
            if self.fail_stop:
                raise RuntimeError('SYNTHETIC_SECRET')
            return args[-1]
        if args[0] == 'rm':
            if self.fail_cleanup and args[-1] == self.created[-1]:
                raise RuntimeError('SYNTHETIC_SECRET')
            return args[-1]
        self.fail('unexpected Docker command')

    def rehearse(self):
        return PLAN.SCHEMA.rehearse(ACTION.IMAGE, self.source, self.schema, self.output,
            command=self.command, observation=self.observation, nonce=self.nonce,
            expected_identity=dict(image_id=self.image, binary_sha256=self.binary),
            expected_schema=PLAN.SCHEMA.schema_digest(self.sql))

    def test_two_fresh_gateways_share_one_isolated_database_and_publish_after_cleanup(self):
        self.rehearse()
        startups = self.observation['startups']
        self.assertEqual(len(startups), 2)
        self.assertNotEqual(startups[0]['container_id'], startups[1]['container_id'])
        for startup in startups:
            self.assertEqual(startup['before_schema_sha256'], startup['after_schema_sha256'])
            self.assertTrue(startup['healthy'])
            self.assertEqual(startup['stop_exit_code'], 0)
            self.assertEqual(startup['process']['ExitCode'], 0)
        creates = [a for a in self.calls if a[0] == 'create']
        self.assertEqual(len(creates), 4)  # one binary probe, DB, two Gateways
        self.assertEqual([a[a.index('--network')+1] for a in creates],
                         ['none', 'none', 'container:'+self.created[1], 'container:'+self.created[1]])
        for args in creates:
            self.assertIn('--read-only', args)
            self.assertIn('--pull', args)
            self.assertNotIn('-p', args)
            self.assertNotIn('-v', args)
            self.assertNotIn('--mount', args)
        self.assertTrue(self.observation['cleanup_complete'])
        self.assertEqual(len(self.observation['cleanup']), 4)
        self.assertTrue(self.output.exists())

    def test_failure_on_either_start_and_schema_drift_prevent_receipt(self):
        for kind in ('fail_pass', 'drift_pass'):
            for number in (1, 2):
                with self.subTest(kind=kind, number=number):
                    self.setUp()
                    setattr(self, kind, number)
                    with self.assertRaises(ValueError):
                        self.rehearse()
                    self.assertFalse(self.output.exists())
                    self.assertTrue(self.observation['cleanup_complete'])

    def test_running_oom_error_and_nonzero_exit_are_not_successful_stop(self):
        for key, value in (('Status', 'running'), ('Running', True), ('OOMKilled', True),
                           ('Error', 'SYNTHETIC_SECRET'), ('ExitCode', 137), ('ExitCode', False)):
            with self.subTest(key=key, value=value):
                self.setUp()
                self.process[key] = value
                with self.assertRaisesRegex(ValueError, 'exit cleanly'):
                    self.rehearse()
                self.assertFalse(self.output.exists())
                self.assertNotIn('SYNTHETIC_SECRET', json.dumps(self.observation))

    def test_cleanup_failure_still_attempts_all_owned_resources_and_denies_receipt(self):
        self.fail_cleanup = True
        with self.assertRaisesRegex(ValueError, 'cleanup incomplete'):
            self.rehearse()
        self.assertEqual(len([a for a in self.calls if a[0] == 'rm']), 4)
        self.assertFalse(self.observation['cleanup_complete'])
        self.assertFalse(self.output.exists())

    def test_stop_failure_and_wrong_binary_do_not_start_next_gateway(self):
        self.fail_stop = True
        with self.assertRaises(RuntimeError):
            self.rehearse()
        self.assertEqual(self.pass_number, 1)
        self.assertFalse(self.output.exists())
        self.setUp()
        with patch.object(self, 'command', wraps=self.command) as command:
            with self.assertRaisesRegex(ValueError, 'binary identity'):
                PLAN.SCHEMA.rehearse(ACTION.IMAGE, self.source, self.schema, self.output,
                    command=command, observation=self.observation, nonce=self.nonce,
                    expected_identity=dict(image_id=self.image, binary_sha256='f'*64))
        self.assertEqual(self.pass_number, 0)
        self.assertFalse(self.output.exists())


def completed_startup_fixture(startup, number):
    cid, challenge = str(number)*64, str(number+2)*64
    raw = json.dumps(dict(schema='clearapi.startup-migration-proof/v1', status='success',
        challenge=challenge, hostname=cid[:12], pid=1, binary_sha256=ACTION.BINARY,
        phases=['main_database', 'retired_options', 'log_database'], main_database='postgres', log_database='postgres'))
    migration = dict(status='success', source_commit=ACTION.SOURCE, container_id=cid,
        completion_receipt_json=raw, challenge=challenge, proof_sha256=hashlib.sha256(raw.encode()).hexdigest())
    return dict(startup, pass_number=number, container_id=cid, migration=migration,
                hostname=cid[:12], configured_hostname=cid[:12])


class IsolatedActionsTest(unittest.TestCase):
    def test_log_capture_refuses_rotation_dropping_or_unsupported_driver(self):
        PLAN.SCHEMA.validate_log_config(json.dumps(dict(Type='json-file', Config=dict(mode='blocking'))))
        for value in (dict(Type='local',Config={}),dict(Type='json-file',Config=dict(mode='non-blocking')),
                      dict(Type='json-file',Config={'mode':'blocking','max-size':'4m'})):
            with self.assertRaises(ValueError): PLAN.SCHEMA.validate_log_config(json.dumps(value))

    def test_unqualified_or_unavailable_log_inspect_never_starts_gateway(self):
        for configuration in ('{}', None):
            fixture = IsolatedStartupTest()
            fixture.setUp()
            self.addCleanup(fixture.doCleanups)
            original = fixture.command
            def command(args, raw=None):
                if args[:3] == ['inspect','--format','{{json .HostConfig.LogConfig}}']:
                    if configuration is None: raise OSError('SYNTHETIC_LOG_INSPECT_FAILURE')
                    return configuration
                return original(args,raw)
            with self.assertRaises((ValueError,OSError)):
                PLAN.SCHEMA.rehearse(ACTION.IMAGE,fixture.source,fixture.schema,fixture.output,
                    command=command,observation=fixture.observation,nonce=fixture.nonce,require_migration_evidence=True)
            self.assertNotIn(['start',fixture.created[-1]],fixture.calls)
            self.assertFalse(fixture.output.exists())
            self.assertTrue(fixture.observation['cleanup_complete'])

    def test_negative_migration_uses_only_a_new_read_only_database_and_cleans_both_containers(self):
        self.negative_probe('')

    def test_missing_wrong_or_conflicting_negative_evidence_never_passes(self):
        for fault in ('guard', 'exit', 'missing_log', 'sqlstate', 'cleanup'):
            with self.subTest(fault=fault): self.negative_probe(fault)

    def negative_probe(self, fault):
        state, created, removed = dict(nonce='a'*32), [], []
        def command(root, args):
            if args[0] == 'create':
                cid = str(len(created)+1)*64
                created.append(cid)
                self.assertIn(args[args.index('--network')+1], ('none', 'container:'+'1'*64))
                for forbidden in ('-p', '-v', '--mount'): self.assertNotIn(forbidden, args)
                return cid
            if args[0] == 'start': return args[-1]
            if args[0] == 'exec':
                if 'pg_isready' in args: return 'ready'
                if args[-1] == 'SHOW default_transaction_read_only': return 'off' if fault == 'guard' else 'on'
                self.assertEqual(args[-1], 'ALTER ROLE postgres SET default_transaction_read_only = on')
                return ''
            if args[0] == 'inspect':
                if args[2] == '{{json .HostConfig.LogConfig}}':
                    return json.dumps(dict(Type='json-file', Config=dict(mode='blocking')))
                if args[2] == '{{json .State}}':
                    return json.dumps(dict(Status='exited', Running=False, ExitCode=0 if fault=='exit' else 1,
                                           OOMKilled=False, Error=''))
                return state['nonce']
            if args[0] == 'logs':
                return '' if fault=='missing_log' else ('SQLSTATE 42P01' if fault=='sqlstate'
                                                      else 'SQLSTATE 25006 SYNTHETIC_SECRET')
            if args[0] == 'rm':
                removed.append(args[-1])
                if fault=='cleanup' and args[-1]=='2'*64: raise OSError('SYNTHETIC_SECRET')
                return ''
            self.fail('unexpected negative probe transport')
        with patch.object(ACTION, 'docker', side_effect=command):
            if fault:
                with self.assertRaises(ValueError): ACTION.rehearse_read_only_failure(Path('unused'), state)
                with self.assertRaises(ValueError): ACTION.validate_negative_probe(state['negative_migration'])
            else:
                ACTION.rehearse_read_only_failure(Path('unused'), state)
                ACTION.validate_negative_probe(state['negative_migration'])
        self.assertEqual(set(removed), set(created))
        self.assertNotIn('SYNTHETIC_SECRET', json.dumps(state))

    def test_fixed_rc_rejects_attempt_source_digest_archive_and_expiry_mismatch(self):
        run = dict(id=int(ACTION.RC), run_attempt=1, head_sha=ACTION.SOURCE, head_branch='main',
            status='completed', conclusion='success', path='.github/workflows/release-pg03-gateway.yml',
            event='workflow_run', repository=dict(full_name=ACTION.REPO),
            head_repository=dict(full_name=ACTION.REPO))
        artifact = dict(id=int(ACTION.ARTIFACT), expired=False, name='pg03-gateway-release-'+ACTION.RC+'-1',
                        size_in_bytes=100, workflow_run=dict(id=int(ACTION.RC), head_sha=ACTION.SOURCE))
        ACTION.validate_rc(run, artifact)
        for field, value in (('run_attempt', 2), ('run_attempt', True), ('head_sha', 'f'*40),
                             ('conclusion', 'failure'), ('path', '.github/workflows/ci.yml')):
            with self.subTest(field=field), self.assertRaises(ValueError):
                ACTION.validate_rc(dict(run, **{field: value}), artifact)
        for field, value in (('expired', True), ('id', 1), ('size_in_bytes', 99999999)):
            with self.subTest(field=field), self.assertRaises(ValueError):
                ACTION.validate_rc(run, dict(artifact, **{field: value}))

    def test_pull_failure_erases_auth_and_never_retries_or_falls_back_to_tag(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            calls = []

            def command(root, args, raw=None, config=None):
                calls.append(args)
                if args[0] == 'pull':
                    raise RuntimeError('SYNTHETIC_TOKEN')
                return ''

            with patch.dict(os.environ, GH_TOKEN='SYNTHETIC_TOKEN'), patch.object(ACTION, 'docker', side_effect=command):
                with self.assertRaises(RuntimeError):
                    ACTION.pull(root)
            self.assertFalse((root / 'registry-auth').exists())
            self.assertEqual([a for a in calls if a[0] == 'pull'],
                             [['pull', '--platform', 'linux/amd64', ACTION.IMAGE]])

    def test_command_environment_excludes_inherited_secrets_and_remote_daemon(self):
        with patch.dict(os.environ, GH_TOKEN='SYNTHETIC_TOKEN', DOCKER_HOST='tcp://production',
                        SQL_DSN='SYNTHETIC_DSN', GH_DEBUG='api'), patch.object(subprocess, 'run') as run:
            ACTION.execute(['docker', 'version'])
        environment = run.call_args.kwargs['env']
        for key in ('GH_TOKEN', 'SQL_DSN', 'DOCKER_HOST', 'GH_DEBUG'):
            self.assertNotIn(key, environment)
        self.assertTrue(run.call_args.kwargs['capture_output'])

    @unittest.skipUnless(os.name == 'posix', 'Linux directory durability contract')
    def test_final_cleanup_or_prior_failure_never_publishes_success(self):
        for prior, cleanup in (('failure', True), ('success', False), ('cancelled', True)):
            with self.subTest(prior=prior, cleanup=cleanup), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                state = dict(nonce='a'*32, rehearsal='passed')
                with patch.dict(os.environ, PRIOR_STATUS=prior), patch.object(ACTION, 'cleanup',
                        return_value=dict(complete=cleanup)), self.assertRaises(ValueError):
                    ACTION.finalize(root, state)
                self.assertFalse((root / 'public/success.json').exists())
                self.assertEqual(json.loads((root / 'public/result.json').read_text())['conclusion'],
                                 'failure_or_unknown')

    @unittest.skipUnless(os.name == 'posix', 'Linux directory durability contract')
    def test_final_success_requires_both_startups_schema_exit_and_cleanup_evidence(self):
        startup = dict(healthy=True, before_schema_sha256=ACTION.BASELINE,
            after_schema_sha256=ACTION.BASELINE, create_exit_code=0, start_exit_code=0, stop_exit_code=0,
            process=dict(Status='exited', Running=False, ExitCode=0, OOMKilled=False, error_present=False),
            migration=dict(status='success'))
        state = dict(nonce='a'*32, rehearsal='passed', negative_migration=negative_probe_fixture(), observation=dict(cleanup_complete=True,
            candidate=dict(image_id='sha256:'+'b'*64),
            startups=[completed_startup_fixture(startup, i) for i in (1, 2)]))
        receipt = dict(schema_version=PLAN.SCHEMA.SCHEMA, postgres_image_ref=PLAN.SCHEMA.POSTGRES,
            postgres_version_num='170006', startup_passes=2,
            profile='isolated_schema_only_master_single_postgres', source_schema_sha256=ACTION.BASELINE,
            target_schema_sha256=ACTION.BASELINE, source_commit=ACTION.SOURCE,
            candidate_image_id='sha256:'+'b'*64, candidate_binary_sha256=ACTION.BINARY)
        for drift in (None, 'healthy', 'after_schema_sha256', 'stop_exit_code', 'process', 'migration', 'negative_migration'):
            with self.subTest(drift=drift), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                current = copy.deepcopy(state)
                if drift == 'negative_migration': current['negative_migration'] = {}
                elif drift:
                    current['observation']['startups'][1][drift] = None
                (root / 'schema.json').write_text(json.dumps(receipt))
                with patch.dict(os.environ, PRIOR_STATUS='success'), patch.object(ACTION, 'cleanup',
                        return_value=dict(complete=True)):
                    if drift:
                        with self.assertRaises(ValueError):
                            ACTION.finalize(root, current)
                    else:
                        ACTION.finalize(root, current)
                self.assertEqual((root / 'public/success.json').exists(), drift is None)

    def test_cleanup_checks_nonce_and_continues_after_one_removal_fails(self):
        nonce = 'a'*32
        first, second = '1'*64, '2'*64
        removed = []

        def command(root, args):
            if args[0] == 'ps':
                return first+'\n'+second
            if args[0] == 'inspect':
                return nonce
            if args[0] == 'rm':
                removed.append(args[-1])
                if args[-1] == first:
                    raise RuntimeError('synthetic removal failure')
                return ''
            self.fail('unexpected cleanup command')

        with patch.object(ACTION, 'docker', side_effect=command):
            result = ACTION.cleanup(Path('unused'), nonce)
        self.assertEqual(removed, [first, second])
        self.assertFalse(result['complete'])

    def test_workflow_is_manual_read_only_pinned_and_outside_production_chain(self):
        try:
            import yaml
        except ImportError:
            self.skipTest('PyYAML unavailable; workflow static parser must run separately')
        root = SCRIPTS.parent
        workflow = yaml.load((root / '.github/workflows/pg03-isolated-rehearsal.yml').read_text(),
                             Loader=yaml.BaseLoader)
        self.assertEqual(set(workflow['on']), {'workflow_dispatch'})
        required = dict(contents='read', packages='read', actions='read')
        self.assertEqual(workflow['permissions'], required)
        job = workflow['jobs']['rehearse']
        self.assertEqual(job['permissions'], required)
        self.assertEqual(job['runs-on'], 'ubuntu-24.04')
        self.assertNotIn('runner.', json.dumps(job.get('env', {})),
                         'runner context is unavailable in job-level env')
        self.assertNotIn('environment', job)
        self.assertNotIn('uses', job)
        text = json.dumps(workflow)
        for forbidden in ('secrets.', 'secrets: inherit', 'PROD_SSH', 'workflow_call', 'self-hosted'):
            self.assertNotIn(forbidden, text)
        for step in job['steps']:
            if 'uses' in step:
                self.assertRegex(step['uses'], r'@[a-f0-9]{40}$')
        for path in (root / '.github/workflows').glob('*.yml'):
            other = yaml.load(path.read_text(encoding='utf-8'), Loader=yaml.BaseLoader)
            consumers = other.get('on', {}).get('workflow_run', {}) or {}
            self.assertNotIn(workflow['name'], consumers.get('workflows', []))
            self.assertNotIn('*', consumers.get('workflows', []))


@unittest.skipUnless(os.name == 'posix', 'Linux directory durability contract')
class EvidencePublicationTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.environment = patch.dict(os.environ, PRIOR_STATUS='success', GITHUB_OUTPUT=str(self.root / 'output'))
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.cleanup = patch.object(ACTION, 'cleanup', return_value=dict(complete=True))
        self.cleanup.start()
        self.addCleanup(self.cleanup.stop)
        # Publication-only fixture: this is not produced by the legacy log collector.
        startup = dict(healthy=True, before_schema_sha256=ACTION.BASELINE, after_schema_sha256=ACTION.BASELINE,
            create_exit_code=0, start_exit_code=0, stop_exit_code=0, migration=dict(status='success'),
            process=dict(Status='exited', Running=False, ExitCode=0, OOMKilled=False, error_present=False))
        self.state = dict(nonce='a'*32, rehearsal='passed', negative_migration=negative_probe_fixture(), observation=dict(cleanup_complete=True,
            candidate=dict(image_id='sha256:'+'b'*64),
            startups=[completed_startup_fixture(startup, i) for i in (1, 2)]))
        receipt = dict(schema_version=PLAN.SCHEMA.SCHEMA, postgres_image_ref=PLAN.SCHEMA.POSTGRES,
            postgres_version_num='170006', startup_passes=2,
            profile='isolated_schema_only_master_single_postgres', source_schema_sha256=ACTION.BASELINE,
            target_schema_sha256=ACTION.BASELINE, source_commit=ACTION.SOURCE,
            candidate_image_id='sha256:'+'b'*64, candidate_binary_sha256=ACTION.BINARY)
        (self.root / 'schema.json').write_text(json.dumps(receipt))

    def test_raw_proof_conflict_denies_both_publication_and_readback(self):
        for key in ('completion_receipt_json', 'proof_sha256', 'challenge', 'container_id'):
            with self.subTest(key=key), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / 'schema.json').write_bytes((self.root / 'schema.json').read_bytes())
                invalid = copy.deepcopy(self.state)
                invalid['observation']['startups'][1]['migration'][key] = 'f'*64
                with self.assertRaises(ValueError): ACTION.finalize(root, invalid)
                self.assertFalse((root / 'public/success.json').exists())
        ACTION.finalize(self.root, self.state)
        path = self.root / 'public/result.json'
        raw = path.read_bytes()
        for key in ('completion_receipt_json', 'proof_sha256', 'challenge', 'container_id'):
            changed = json.loads(raw)
            changed['observation']['startups'][1]['migration'][key] = 'f'*64
            path.write_text(json.dumps(changed))
            with patch.dict(os.environ, FINALIZE_OUTCOME='success'), self.assertRaises(ValueError):
                ACTION.verify_public(self.root)
        path.write_bytes(raw)
        with patch.dict(os.environ, FINALIZE_OUTCOME='success'): ACTION.verify_public(self.root)

    def test_all_required_evidence_is_durable_before_public_success(self):
        calls = []
        original = ACTION.persist_json

        def persist(path, value):
            self.assertFalse((self.root / 'public').exists())
            calls.append(path.name)
            original(path, value)

        with patch.object(ACTION, 'persist_json', side_effect=persist):
            ACTION.finalize(self.root, self.state)
        self.assertEqual(calls, ['state.json', 'success.json', 'result.json'])
        result = json.loads((self.root / 'public/result.json').read_text())
        self.assertEqual(result['conclusion'], 'success')
        self.assertEqual(result['steps']['finalize'], 'passed')
        self.assertTrue((self.root / 'public/success.json').is_file())
        self.assertFalse((self.root / 'output').exists())
        with patch.dict(os.environ, FINALIZE_OUTCOME='success'):
            ACTION.verify_public(self.root)
        self.assertEqual((self.root / 'output').read_text(), 'evidence_ready=true\n')

    def test_success_or_intermediate_write_failure_publishes_failure_and_raises(self):
        for filename in ('success.json', 'state.json'):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / 'schema.json').write_bytes((self.root / 'schema.json').read_bytes())
                failed = False
                original = ACTION.persist_json

                def persist(path, value):
                    nonlocal failed
                    if path.name == filename and not failed:
                        failed = True
                        # Even a partial file must stay outside the public directory.
                        path.write_bytes(b'{"partial":')
                        raise OSError('synthetic evidence write failure')
                    original(path, value)

                with patch.object(ACTION, 'persist_json', side_effect=persist), self.assertRaises(ValueError):
                    ACTION.finalize(root, copy.deepcopy(self.state))
                result = json.loads((root / 'public/result.json').read_text())
                self.assertEqual(result['conclusion'], 'failure_or_unknown')
                self.assertFalse(result['success_receipt_present'])
                self.assertFalse((root / 'public/success.json').exists())

    def test_result_write_failure_never_exposes_success_and_propagates_nonzero(self):
        original = ACTION.persist_json

        def persist(path, value):
            if path.name == 'result.json':
                path.write_bytes(b'{"conclusion":"success"')
                raise OSError('synthetic disk failure')
            original(path, value)

        with patch.object(ACTION, 'persist_json', side_effect=persist), self.assertRaises(OSError):
            ACTION.finalize(self.root, self.state)
        self.assertFalse((self.root / 'public').exists())
        self.assertFalse((self.root / 'output').exists())

    def test_directory_sync_failure_withdraws_success_and_publishes_failure(self):
        original = ACTION.sync_directory
        failed = False

        def sync(path):
            nonlocal failed
            if path == self.root and not failed:
                failed = True
                raise OSError('synthetic parent directory sync failure')
            original(path)

        with patch.object(ACTION, 'sync_directory', side_effect=sync), self.assertRaises(ValueError):
            ACTION.finalize(self.root, self.state)
        self.assertEqual(json.loads((self.root / 'public/result.json').read_text())['conclusion'],
                         'failure_or_unknown')
        self.assertFalse((self.root / 'public/success.json').exists())

    def test_old_success_is_retained_outside_upload_paths_and_never_reused(self):
        public = self.root / 'public'
        public.mkdir()
        old = b'{"conclusion":"success","run":"old"}'
        (public / 'result.json').write_bytes(old)
        (public / 'success.json').write_bytes(old)
        with self.assertRaises(ValueError):
            ACTION.finalize(self.root, self.state)
        result = json.loads((public / 'result.json').read_text())
        self.assertEqual(result['conclusion'], 'failure_or_unknown')
        self.assertFalse((public / 'success.json').exists())
        retained = list(self.root.glob('retained-public-*'))
        self.assertEqual(len(retained), 1)
        self.assertEqual((retained[0] / 'result.json').read_bytes(), old)

    def test_cli_failure_returns_nonzero_without_docker_or_success_output(self):
        run_root = self.root / 'pg03-isolated-1-1'
        run_root.mkdir()
        # Invalid nonce prevents any Docker command in this subprocess.
        (run_root / 'state.json').write_text(json.dumps(dict(nonce=None, rehearsal='not_run')))
        env = dict(os.environ, RUNNER_TEMP=str(self.root), REHEARSAL_ROOT=str(run_root),
                   GITHUB_RUN_ID='1', GITHUB_RUN_ATTEMPT='1', PRIOR_STATUS='failure')
        result = subprocess.run([sys.executable, '-B', str(SCRIPTS / 'ci/pg03-isolated-rehearsal.py'),
                                 'finalize'], env=env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('passed', result.stdout)
        self.assertEqual(json.loads((run_root / 'public/result.json').read_text())['conclusion'],
                         'failure_or_unknown')

    def test_readback_rejects_old_success_when_retention_is_impossible(self):
        public = self.root / 'public'
        public.mkdir()
        (public / 'result.json').write_text(json.dumps(dict(conclusion='success', success_receipt_present=True)))
        (public / 'success.json').write_text('{}')
        with patch.object(ACTION, 'quarantine_public', side_effect=OSError('synthetic read-only directory')):
            with self.assertRaises(OSError):
                ACTION.finalize(self.root, self.state)
        with patch.dict(os.environ, FINALIZE_OUTCOME='failure'), self.assertRaises(ValueError):
            ACTION.verify_public(self.root)
        self.assertFalse((self.root / 'output').exists())


class MigrationEvidenceTest(unittest.TestCase):
    def proof(self, container='b'*64, challenge='e'*64, binary='c'*64):
        return dict(schema='clearapi.startup-migration-proof/v1', status='success',
            challenge=challenge, hostname=container[:12], pid=1, binary_sha256=binary,
            phases=['main_database', 'retired_options', 'log_database'],
            main_database='postgres', log_database='postgres')

    def structured(self, document, logs=None, source='d'*40, after='a'*64):
        return PLAN.SCHEMA.structured_migration_evidence(document,
            self.log('database migration started') if logs is None else logs,
            'b'*64, source, 'e'*64, 'c'*64, 'a'*64, after, 'b'*12)

    def test_candidate_completion_binds_fresh_instance_binary_and_all_migration_phases(self):
        evidence = self.structured(json.dumps(self.proof()))
        self.assertEqual(evidence['status'], 'success')
        self.assertEqual(evidence['reason'], 'candidate_completed_startup_migrations')
        self.assertTrue(evidence['proof_sha256'])
        self.assertEqual(evidence['completion_receipt_json'], json.dumps(self.proof()))
        self.assertEqual(evidence['challenge'], 'e'*64)

    def test_persisted_migration_receipt_rejects_missing_tampered_and_replayed_bindings(self):
        observation = dict(startups=[completed_startup_fixture({}, i) for i in (1, 2)])
        ACTION.validate_persisted_migrations(observation)
        for key, value in (('completion_receipt_json', '{}'), ('proof_sha256', 'f'*64),
                           ('challenge', 'f'*64), ('container_id', 'f'*64), ('source_commit', 'f'*40)):
            with self.subTest(key=key):
                changed = copy.deepcopy(observation)
                changed['startups'][1]['migration'][key] = value
                with self.assertRaises(ValueError): ACTION.validate_persisted_migrations(changed)
        for key in ('binary_sha256', 'hostname', 'pid'):
            changed = copy.deepcopy(observation)
            migration = changed['startups'][1]['migration']
            raw = json.loads(migration['completion_receipt_json'])
            raw[key] = True if key == 'pid' else 'f'*64
            migration['completion_receipt_json'] = json.dumps(raw)
            migration['proof_sha256'] = hashlib.sha256(json.dumps(raw).encode()).hexdigest()
            with self.assertRaises(ValueError): ACTION.validate_persisted_migrations(changed)
        observation['startups'][1] = copy.deepcopy(observation['startups'][0])
        with self.assertRaises(ValueError): ACTION.validate_persisted_migrations(observation)

    def test_stale_wrong_binary_partial_failed_and_duplicate_proofs_are_unknown(self):
        for key, value in (('challenge', 'f'*64), ('hostname', 'c'*12), ('pid', True),
                           ('binary_sha256', 'e'*64), ('status', 'failure'),
                           ('phases', ['main_database']), ('log_database', 'mysql')):
            with self.subTest(key=key):
                proof = self.proof()
                proof[key] = value
                self.assertEqual(self.structured(json.dumps(proof))['status'], 'unknown')
        valid = json.dumps(self.proof())
        for proof in (None, '', '{', '[]', valid[:-1]+',"status":"success"}', 'x'*16385):
            with self.subTest(kind=type(proof).__name__):
                self.assertEqual(self.structured(proof)['status'], 'unknown')
        self.assertEqual(self.structured(valid, source=PLAN.SCHEMA.LEGACY_MIGRATION_SOURCE)['status'], 'unknown')

    def test_structured_success_cannot_override_log_failure_missing_capture_or_schema_conflict(self):
        proof = json.dumps(self.proof())
        self.assertEqual(self.structured(proof, logs='')['status'], 'unknown')
        self.assertEqual(self.structured(proof, logs='x'*2097153)['status'], 'unknown')
        self.assertEqual(self.structured(proof,
            logs=self.log('failed to seed default vip levels: SYNTHETIC_SECRET'))['status'], 'failure')
        self.assertEqual(self.structured(proof, after='c'*64)['status'], 'failure')

    def test_network_container_hostname_is_bound_to_live_and_configured_value(self):
        for mismatch in (False, True):
            fixture=IsolatedStartupTest()
            fixture.setUp()
            self.addCleanup(fixture.doCleanups)
            original=fixture.command
            challenge=None
            def command(args, raw=None):
                nonlocal challenge
                if args[0]=='create' and '--entrypoint' in args and args[args.index('--entrypoint')+1]=='/new-api':
                    challenge=next(a.split('=',1)[1] for a in args if a.startswith('CLEARAPI_MIGRATION_PROOF_CHALLENGE='))
                if args[:3]==['inspect','--format','{{.Config.Hostname}}']:return 'shared-database-host'
                if args[0]=='exec' and args[-1]=='hostname':return 'wrong-live-host' if mismatch else 'shared-database-host'
                if args[0]=='exec' and 'head' in args:
                    value=self.proof(args[1],challenge,fixture.binary)
                    value['hostname']='shared-database-host'
                    return json.dumps(value)
                if args[0]=='logs':return self.log('database migration started')
                return original(args,raw)
            if mismatch:
                with self.assertRaisesRegex(ValueError,'hostname conflict'):
                    PLAN.SCHEMA.rehearse(ACTION.IMAGE,fixture.source,fixture.schema,fixture.output,
                        command=command,observation=fixture.observation,nonce=fixture.nonce,require_migration_evidence=True)
                self.assertFalse(fixture.output.exists())
            else:
                PLAN.SCHEMA.rehearse(ACTION.IMAGE,fixture.source,fixture.schema,fixture.output,
                    command=command,observation=fixture.observation,nonce=fixture.nonce,require_migration_evidence=True)
                for startup in fixture.observation['startups']:
                    self.assertEqual(startup['hostname'],'shared-database-host')
                    self.assertNotEqual(startup['hostname'],startup['container_id'][:12])
                    self.assertEqual(startup['migration']['status'],'success')
            self.assertTrue(fixture.observation['cleanup_complete'])

    def test_two_proof_enabled_starts_require_each_live_container_receipt(self):
        fixture = IsolatedStartupTest()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        original = fixture.command
        challenges = []

        def command(args, raw=None):
            if args[0] == 'create' and '--entrypoint' in args and args[args.index('--entrypoint')+1] == '/new-api':
                challenges.append(next(a.split('=', 1)[1] for a in args
                    if a.startswith('CLEARAPI_MIGRATION_PROOF_CHALLENGE=')))
            if args[0] == 'exec' and 'head' in args:
                return json.dumps(self.proof(args[1], challenges[-1], fixture.binary))
            if args[0] == 'logs':
                return self.log('database migration started')
            return original(args, raw)

        PLAN.SCHEMA.rehearse(ACTION.IMAGE, fixture.source, fixture.schema, fixture.output,
            command=command, observation=fixture.observation, nonce=fixture.nonce,
            expected_schema=PLAN.SCHEMA.schema_digest(fixture.sql), require_migration_evidence=True)
        self.assertEqual(len(challenges), 2)
        self.assertNotEqual(*challenges)
        self.assertEqual([s['migration']['status'] for s in fixture.observation['startups']],
                         ['success', 'success'])
        self.assertTrue(fixture.observation['cleanup_complete'])
        self.assertTrue(fixture.output.exists())

    def test_missing_live_proof_on_either_start_denies_receipt_and_cleans(self):
        for failed_pass in (1, 2):
            with self.subTest(failed_pass=failed_pass):
                fixture = IsolatedStartupTest()
                fixture.setUp()
                self.addCleanup(fixture.doCleanups)
                original = fixture.command
                challenge = None

                def command(args, raw=None):
                    nonlocal challenge
                    if args[0] == 'create' and '--entrypoint' in args and args[args.index('--entrypoint')+1] == '/new-api':
                        challenge = next(a.split('=', 1)[1] for a in args
                            if a.startswith('CLEARAPI_MIGRATION_PROOF_CHALLENGE='))
                    if args[0] == 'exec' and 'head' in args:
                        if fixture.pass_number == failed_pass:
                            raise RuntimeError('synthetic missing receipt')
                        return json.dumps(self.proof(args[1], challenge, fixture.binary))
                    if args[0] == 'logs':
                        return self.log('database migration started')
                    return original(args, raw)

                with self.assertRaisesRegex(ValueError, 'migration outcome BLOCKED'):
                    PLAN.SCHEMA.rehearse(ACTION.IMAGE, fixture.source, fixture.schema, fixture.output,
                        command=command, observation=fixture.observation, nonce=fixture.nonce,
                        require_migration_evidence=True)
                self.assertEqual(len(fixture.observation['startups']), failed_pass)
                self.assertEqual(fixture.observation['startups'][-1]['migration']['status'], 'unknown')
                self.assertTrue(fixture.observation['cleanup_complete'])
                self.assertFalse(fixture.output.exists())

    def log(self, message):
        return '2026-10-20T16:00:00.000000001Z [SYS] 2026/10/20 - 16:00:00 | '+message+' \n'

    def inspect(self, text, before='a'*64, after='a'*64):
        return PLAN.SCHEMA.migration_evidence(text, 'b'*64, PLAN.SCHEMA.LEGACY_MIGRATION_SOURCE, before, after)

    def test_complete_legacy_startup_capture_cannot_claim_migration_success(self):
        evidence = self.inspect(self.log('database migration started')+self.log('server exited'))
        self.assertTrue(evidence['capture_complete'])
        self.assertEqual(evidence['parsed_records'], 2)
        self.assertEqual(evidence['status'], 'unknown')
        self.assertEqual(evidence['reason'], 'missing_migration_completion_contract')

    def test_known_migration_errors_are_typed_without_reflecting_their_payload(self):
        for message in ('failed to migrate options primary key: SYNTHETIC_SECRET',
                        'Warning: failed to migrate subscription_plans.price_amount to decimal: SYNTHETIC_SECRET'):
            with self.subTest(message=message):
                evidence = self.inspect(self.log(message))
                self.assertEqual(evidence['status'], 'failure')
                self.assertEqual(evidence['reason'], 'recorded_migration_error')
                self.assertNotIn('SYNTHETIC_SECRET', json.dumps(evidence))

    def test_ordinary_error_or_quoted_error_is_not_a_migration_error(self):
        for message in ('provider request failed: ordinary business error',
                        'business message: failed to migrate options primary key: quoted text'):
            evidence = self.inspect(self.log(message))
            self.assertEqual(evidence['error_kinds'], [])
            self.assertEqual(evidence['status'], 'unknown')

    def test_missing_unparseable_or_oversized_logs_fail_closed(self):
        for document in ('', '[SYS] truncated', 'x'*2097153):
            evidence = self.inspect(document)
            self.assertFalse(evidence['capture_complete'])
            self.assertEqual(evidence['status'], 'unknown')

    def test_migration_observation_conflicting_with_schema_is_failure(self):
        evidence = self.inspect(self.log('database migration started'), after='c'*64)
        self.assertEqual(evidence['status'], 'failure')
        self.assertEqual(evidence['reason'], 'schema_conflicts_with_migration_evidence')

    def test_error_with_exit_zero_and_unchanged_schema_still_blocks_receipt(self):
        fixture = IsolatedStartupTest()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        fixture.source = PLAN.SCHEMA.LEGACY_MIGRATION_SOURCE
        original = fixture.command

        def command(args, raw=None):
            if args[0] == 'logs':
                return self.log('failed to migrate options primary key: SYNTHETIC_SECRET')
            return original(args, raw)

        with self.assertRaisesRegex(ValueError, 'migration outcome BLOCKED'):
            PLAN.SCHEMA.rehearse(ACTION.IMAGE, fixture.source, fixture.schema, fixture.output,
                command=command, observation=fixture.observation, nonce=fixture.nonce,
                expected_schema=PLAN.SCHEMA.schema_digest(fixture.sql), require_migration_evidence=True)
        startup = fixture.observation['startups'][0]
        self.assertEqual(startup['process']['ExitCode'], 0)
        self.assertEqual(startup['before_schema_sha256'], startup['after_schema_sha256'])
        self.assertEqual(startup['migration']['status'], 'failure')
        self.assertTrue(fixture.observation['cleanup_complete'])
        self.assertFalse(fixture.output.exists())

    def test_unavailable_logs_record_unknown_and_cleanup_without_receipt(self):
        fixture = IsolatedStartupTest()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        original = fixture.command

        def command(args, raw=None):
            if args[0] == 'logs':
                raise subprocess.TimeoutExpired('synthetic log capture', 90)
            return original(args, raw)

        with self.assertRaisesRegex(ValueError, 'evidence unavailable'):
            PLAN.SCHEMA.rehearse(ACTION.IMAGE, fixture.source, fixture.schema, fixture.output,
                command=command, observation=fixture.observation, nonce=fixture.nonce,
                require_migration_evidence=True)
        self.assertEqual(fixture.observation['startups'][0]['migration']['status'], 'unknown')
        self.assertTrue(fixture.observation['cleanup_complete'])
        self.assertFalse(fixture.output.exists())

    def test_log_capture_keeps_stderr_where_syserror_is_written(self):
        result = subprocess.CompletedProcess([], 0, stdout=b'ordinary log\n', stderr=b'migration error\n')
        with patch.object(subprocess, 'run', return_value=result):
            self.assertEqual(ACTION.execute(['docker', 'logs', 'synthetic'], include_stderr=True),
                             b'ordinary log\nmigration error\n')


class SuccessorRequestTest(unittest.TestCase):
    def setUp(self):
        self.now = datetime(2026, 10, 21, tzinfo=timezone.utc)
        self.request = dict(schema='clearapi.successor-transaction-request/v1',
            transaction_id='1'*32, issued_at_utc='2026-10-21T00:00:00Z',
            expires_at_utc='2026-10-21T00:05:00Z', control_commit='2'*40,
            candidate=dict(source_commit='3'*40, image_digest='sha256:'+'4'*64,
                image_ref='ghcr.io/a3225865526-wq/clearapi@sha256:'+'4'*64,
                image_id='sha256:'+'5'*64, binary_sha256='6'*64, rc_run_id=123, rc_attempt=1),
            evidence_sha256={name: '7'*64 for name in TX.EVIDENCE}, baseline_schema_sha256='8'*64,
            operations=list(TX.OPERATIONS), failed_run=PLAN.FAILED_RUN,
            frozen_sha256=PLAN.FROZEN.copy(), previous_success=dict(source_commit=PLAN.PREVIOUS_COMMIT,
                                                                     image_digest=PLAN.PREVIOUS_DIGEST))

    def test_strict_request_does_not_accept_commands_legacy_authority_or_long_leases(self):
        TX.validate_request(TX.encoded(self.request), self.now)
        cases = [('operations', ['replace_gateway']), ('expires_at_utc', '2026-10-21T00:05:01Z'),
                 ('issued_at_utc', '2026-10-21T00:00:01Z'), ('frozen_sha256', {}),
                 ('failed_run', '123'), ('token', 'SYNTHETIC_SECRET')]
        for key, value in cases:
            with self.subTest(key=key):
                changed = dict(self.request, **{key: value})
                with self.assertRaises(ValueError): TX.validate_request(TX.encoded(changed), self.now)
        changed = copy.deepcopy(self.request)
        changed['candidate']['rc_attempt'] = True
        with self.assertRaises(ValueError): TX.validate_request(TX.encoded(changed), self.now)
        with self.assertRaises(ValueError): TX.validate_request(TX.encoded(self.request), self.now + timedelta(seconds=300))
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            TX.validate_request(TX.encoded(self.request)[:-1] + b',"failed_run":"123"}', self.now)

    def test_no_default_production_adapter_or_mutation_cli(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(subprocess, 'run',
                side_effect=AssertionError('no shell or network permitted')):
            with self.assertRaisesRegex(ValueError, 'adapter not implemented'):
                TX.Transaction(Path(directory), now=lambda: self.now).execute(TX.encoded(self.request))
            self.assertEqual(list(Path(directory).iterdir()), [])


class TransactionFixture:
    """Trusted-adapter substitute; only test-owned files and no Docker/network."""
    def __init__(self, case):
        self.case, self.calls, self.fail, self.live = case, [], None, None
        self.bad_evidence = self.bad_authority = self.containment_failure = False
        self.expire_on_auth = None
        self.expire_after = None
        self.numeric_containment = False
        self.tamper = None
        self.auth_count = 0

    @contextmanager
    def deployment_lock(self):
        import fcntl
        with (self.case.app / 'test-deploy.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            try: yield
            finally: fcntl.flock(lock, fcntl.LOCK_UN)

    def authenticate(self, request, request_sha):
        self.auth_count += 1
        self.case.assertEqual(request_sha, TX.sha(TX.encoded(self.case.request)))
        if self.expire_on_auth == self.auth_count: self.case.now += timedelta(seconds=301)
        if self.bad_authority: raise ValueError('SYNTHETIC_SECRET')

    def verify_evidence(self, request):
        if self.bad_evidence: raise ValueError('SYNTHETIC_SECRET')

    def observe(self, request):
        if self.live is not None and self.expire_after == 'observe_commit':
            self.case.now += timedelta(seconds=301)
        return copy.deepcopy(self.live or self.case.before)

    def mutation(self, phase):
        self.case.assertTrue(next(self.case.app.glob('*-attempt.json')).is_file(), 'durable claim before mutation')
        self.calls.append(phase)
        if self.fail == phase: raise TimeoutError('SYNTHETIC_SECRET')
        if self.expire_after == phase: self.case.now += timedelta(seconds=301)

    def prepare_runtime(self, request, context):
        self.mutation('prepare_runtime')
        if self.tamper == 'request': request['candidate']['source_commit'] = 'f'*40

    def replace_gateway(self, request, context):
        self.mutation('replace_gateway')
        return '9'*64

    def postflight(self, request, context, restarted):
        phase = 'verify_restart' if restarted else 'verify_startup'
        self.mutation(phase)
        value = dict(self.case.before, gateway_container_id='9'*64, gateway_running=True,
            candidate_identity=copy.deepcopy(request['candidate']), process_started_ticks='102' if restarted else '101',
            migration_challenge=context['migration_challenge'], migration_proof_sha256=TX.sha(context['migration_challenge'].encode()),
            runtime_evidence_sha256='a'*64, restart_evidence_sha256='b'*64 if restarted else None)
        if self.tamper == 'protected': value['protected_state_sha256'] = 'c'*64
        if self.tamper == 'challenge': value['migration_challenge'] = 'd'*64
        if self.tamper == 'process' and restarted: value['process_started_ticks'] = '101'
        self.live = copy.deepcopy(value)
        return value

    def verify_startup(self, request, context): return self.postflight(request, context, False)

    def verify_restart(self, request, context): return self.postflight(request, context, True)

    def commit_successor(self, request, context):
        self.mutation('commit_successor')
        if self.tamper == 'commit_process': self.live['process_started_ticks'] = '777'
        return 'e'*64

    def contain(self, request, context):
        self.calls.append('contain')
        self.case.assertEqual(request['candidate'], self.case.request['candidate'])
        if self.containment_failure: raise RuntimeError('SYNTHETIC_SECRET')
        result = dict(frozen_sha256=PLAN.FROZEN.copy(), executor_container_id=PLAN.EXECUTOR_CONTAINER,
                    executor_process_epoch=self.case.before['executor_process_epoch'],
                    ingress='isolated_stopped_restart_no', public_web_listeners=[],
                    successor_gateway_stopped=True, database_restored=False, executor_started=False)
        if self.numeric_containment:
            result.update(successor_gateway_stopped=1, database_restored=0, executor_started=0)
        return result


@unittest.skipUnless(os.name == 'posix' and os.geteuid() == 0, 'root-owned POSIX durable transaction fixtures')
class SuccessorTransactionTest(unittest.TestCase):
    def setUp(self):
        SuccessorRequestTest.setUp(self)
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.app = Path(temporary.name)
        self.before = dict(schema='clearapi.successor-live-projection/v1', observed_at_utc='2026-10-21T00:00:00Z',
            frozen_sha256=PLAN.FROZEN.copy(), gateway_container_id=TX.GATEWAY, gateway_running=False,
            executor_container_id=PLAN.EXECUTOR_CONTAINER, executor_process_epoch='a'*32,
            ingress='isolated_stopped_restart_no', public_web_listeners=[],
            database_schema_sha256=self.request['baseline_schema_sha256'], protected_state_sha256='b'*64,
            startup_write_scope_sha256=self.request['evidence_sha256']['startup_write_scope'],
            candidate_identity=None, process_started_ticks=None, migration_challenge=None,
            migration_proof_sha256=None, runtime_evidence_sha256=None, restart_evidence_sha256=None)
        self.request['evidence_sha256']['production_observation'] = TX.sha(TX.encoded(self.before))
        for name in PLAN.FROZEN: (self.app / name).write_bytes(b'preserve historical fixture')
        self.io = TransactionFixture(self)

    def execute(self):
        return TX.Transaction(self.app, self.io, now=lambda: self.now).execute(TX.encoded(self.request))

    def test_success_is_ordered_one_use_and_preserves_incident_bytes(self):
        result = self.execute()
        self.assertEqual(self.io.calls, list(TX.OPERATIONS))
        self.assertEqual(result['original_deployment_status'], 'failed')
        self.assertEqual(result['public_traffic'], 'blocked')
        self.assertEqual(len(list(self.app.glob('*-result.json'))), 1)
        reader = TX.Transaction(self.app, self.io, now=lambda: self.now)
        inspected = reader.read_result(TX.encoded(self.request), 0)
        self.assertIs(inspected['execution_allowed'], False)
        for code in (None, False, 1):
            with self.assertRaises(ValueError): reader.read_result(TX.encoded(self.request), code)
        saved = next(self.app.glob('*-2-replace_gateway.json'))
        prior = saved.read_bytes()
        saved.write_text('{"status":"completed"}')
        with self.assertRaises(ValueError): reader.read_result(TX.encoded(self.request), 0)
        saved.write_bytes(prior)
        for name in PLAN.FROZEN: self.assertEqual((self.app / name).read_bytes(), b'preserve historical fixture')
        count = len(self.io.calls)
        with self.assertRaisesRegex(ValueError, 'consumed'): self.execute()
        self.assertEqual(len(self.io.calls), count)

    def test_each_failure_consumes_attempt_contains_and_never_retries(self):
        for phase in TX.OPERATIONS:
            with self.subTest(phase=phase):
                self.setUp()
                self.io.fail = phase
                with self.assertRaises(ValueError): self.execute()
                self.assertEqual(self.io.calls[-1], 'contain')
                self.assertEqual(list(self.app.glob('*-result.json')), [])
                failure = json.loads(next(self.app.glob('*-failure.json')).read_text())
                self.assertTrue(failure['containment_verified'])
                self.assertFalse(failure['automatic_retry'])
                self.assertNotIn('SYNTHETIC_SECRET', next(self.app.glob('*-failure.json')).read_text())
                with self.assertRaisesRegex(ValueError, 'consumed'): self.execute()

    def test_unverified_authority_receipts_or_observation_never_reserve_or_mutate(self):
        for field in ('bad_authority', 'bad_evidence', 'observation'):
            with self.subTest(field=field):
                self.setUp()
                if field == 'observation': self.before['gateway_running'] = True
                else: setattr(self.io, field, True)
                with self.assertRaises(ValueError): self.execute()
                self.assertEqual(self.io.calls, [])
                self.assertEqual(list(self.app.glob('*-attempt.json')), [])

    def test_expiry_during_authority_refresh_prevents_mutation_but_keeps_claim(self):
        self.io.expire_on_auth = 2
        with self.assertRaises(ValueError): self.execute()
        self.assertEqual(self.io.calls, [])
        self.assertEqual(len(list(self.app.glob('*-attempt.json'))), 1)
        for phase in ('commit_successor', 'observe_commit'):
            with self.subTest(phase=phase):
                self.setUp()
                self.io.expire_after = phase
                with self.assertRaises(ValueError): self.execute()
                self.assertEqual(self.io.calls[-1], 'contain')
                self.assertEqual(list(self.app.glob('*-result.json')), [])
        self.setUp()
        link = os.link
        def expire_during_result(source, target, **kwargs):
            link(source, target, **kwargs)
            if str(target).endswith('-result.json'): self.now += timedelta(seconds=301)
        with patch.object(os, 'link', side_effect=expire_during_result):
            with self.assertRaises(ValueError): self.execute()
        self.assertEqual(self.io.calls[-1], 'contain')
        self.assertEqual(len(list(self.app.glob('*-failure.json'))), 1)
        with self.assertRaisesRegex(ValueError, 'conflicting failure'):
            TX.Transaction(self.app, self.io, now=lambda: self.now).read_result(TX.encoded(self.request), 0)

    def test_protected_state_challenge_and_restart_replay_fail_closed(self):
        for kind in ('protected', 'challenge', 'process', 'commit_process', 'request'):
            with self.subTest(kind=kind):
                self.setUp()
                self.io.tamper = kind
                with self.assertRaises(ValueError): self.execute()
                self.assertEqual(self.io.calls[-1], 'contain')
                self.assertEqual(list(self.app.glob('*-result.json')), [])

    def test_unknown_containment_never_reports_blocked_ingress_as_verified(self):
        self.io.fail = 'replace_gateway'
        self.io.containment_failure = True
        with self.assertRaises(ValueError): self.execute()
        failure = json.loads(next(self.app.glob('*-failure.json')).read_text())
        self.assertFalse(failure['containment_verified'])
        self.assertEqual(failure['public_traffic'], 'UNKNOWN')
        self.setUp()
        self.io.fail = 'replace_gateway'
        self.io.numeric_containment = True
        with self.assertRaises(ValueError): self.execute()
        failure = json.loads(next(self.app.glob('*-failure.json')).read_text())
        self.assertFalse(failure['containment_verified'])

    def test_claim_sync_failure_and_lock_contention_never_mutate(self):
        with patch.object(os, 'fsync', side_effect=OSError('SYNTHETIC_SECRET')):
            with self.assertRaises(OSError): self.execute()
        self.assertEqual(self.io.calls, [])
        with self.io.deployment_lock():
            with self.assertRaises(BlockingIOError): self.execute()
        self.assertEqual(self.io.calls, [])
        partial = self.app / ('pg03-successor-' + self.request['transaction_id'] + '-1-prepare_runtime.json')
        partial.write_bytes(b'preserve old stage evidence')
        with self.assertRaisesRegex(ValueError, 'consumed'): self.execute()
        self.assertEqual(self.io.calls, [])
        self.assertEqual(partial.read_bytes(), b'preserve old stage evidence')


if __name__ == '__main__':
    unittest.main()
