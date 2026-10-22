#!/usr/bin/env python3
"""Tests for read-only Caddy state comparison and local audit append semantics."""
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch


SCRIPTS = Path(__file__).resolve().parents[1]
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('caddy_reconcile', SCRIPTS / 'caddy-state-reconcile.py')
RECONCILE = importlib.util.module_from_spec(spec)
spec.loader.exec_module(RECONCILE)


def inspect_result(edge):
    return subprocess.CompletedProcess(
        ['docker', 'inspect', RECONCILE.CONTAINER], 0,
        json.dumps([{
            'Id': edge['id'],
            'Image': edge['image'],
            'State': {'Running': edge['running'], 'Paused': edge['paused']},
            'HostConfig': {'RestartPolicy': edge['restart_policy']},
        }]),
        '',
    )


class CaddyStateReconcileTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='clearapi-caddy-reconcile-')
        self.root = Path(self.temp.name)
        audit_root = patch.object(RECONCILE, 'LOCAL_AUDIT_ROOT', self.root)
        audit_root.start()
        self.addCleanup(audit_root.stop)
        self.state = self.root / 'controlled-launch-state.json'
        self.original_edge = {
            'id': 'c' * 64,
            'image': 'sha256:' + 'd' * 64,
            'running': True,
            'paused': False,
            'restart_policy': {'Name': 'unless-stopped', 'MaximumRetryCount': 0},
        }
        self.isolated_edge = {
            **self.original_edge,
            'running': False,
            'restart_policy': {'Name': 'no', 'MaximumRetryCount': 0},
        }
        self.write_state(self.original_edge)

    def tearDown(self):
        self.temp.cleanup()

    def write_state(self, edge, schema='clearapi.controlled-traffic/v1', stage='isolated'):
        self.state.write_text(json.dumps({'schema': schema, 'stage': stage, 'edge': edge}), encoding='utf-8')

    def command(self, edge=None):
        def output(args, **kwargs):
            if args[0] == 'systemctl':
                return subprocess.CompletedProcess(args, 0, '', '')
            return inspect_result(edge or self.isolated_edge)
        return Mock(side_effect=output)

    def test_normal_isolated_state_is_pass_without_mutation(self):
        before = self.state.read_bytes()
        result = RECONCILE.probe(self.state, self.command(), observed_at='2026-10-20T00:00:00Z')
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(result['differences'], {})
        self.assertEqual(result['persisted'], self.original_edge)
        self.assertEqual(result['expected'], self.isolated_edge)
        self.assertEqual(set(result['snapshot_differences']), {'running', 'restart_policy'})
        self.assertEqual(self.state.read_bytes(), before)

    def test_only_guard_and_projected_local_inspect_are_called(self):
        command = self.command()
        RECONCILE.probe(self.state, command)
        self.assertEqual([call.args[0] for call in command.call_args_list], [
            ['systemctl', 'is-active', '--quiet', 'docker.service'],
            ['docker', '--host', 'unix:///var/run/docker.sock', 'inspect', '--format',
             RECONCILE.INSPECT_FORMAT, RECONCILE.CONTAINER],
        ])
        self.assertNotIn('Env', RECONCILE.INSPECT_FORMAT)

    def test_inactive_or_unknown_docker_is_never_contacted(self):
        for outcome in (subprocess.CompletedProcess([], 3, '', ''),
                        FileNotFoundError('systemctl'), subprocess.TimeoutExpired(['systemctl'], 5)):
            with self.subTest(outcome=type(outcome).__name__):
                command = Mock(side_effect=outcome) if isinstance(outcome, Exception) else Mock(return_value=outcome)
                result = RECONCILE.probe(self.state, command)
                self.assertEqual(result['status'], 'UNKNOWN')
                self.assertEqual(command.call_count, 1)
                self.assertEqual(command.call_args.args[0][0], 'systemctl')

    def test_running_container_is_fail_and_proposes_review(self):
        result = RECONCILE.probe(self.state, self.command(self.original_edge),
                                 observed_at='2026-10-20T00:00:00Z')
        self.assertEqual(result['status'], 'FAIL')
        self.assertIn('running', result['differences'])
        self.assertEqual(result['suggested_action'], 'retain_isolation_and_reconcile_after_authorized_review')

    def test_identity_pause_and_restart_policy_drift_are_fail(self):
        cases = ({'id': 'e' * 64}, {'image': 'sha256:' + 'f' * 64}, {'paused': True},
                 {'restart_policy': {'Name': 'unless-stopped', 'MaximumRetryCount': 0}})
        for change in cases:
            with self.subTest(change=change):
                result = RECONCILE.probe(self.state, self.command(dict(self.isolated_edge, **change)))
                self.assertEqual(result['status'], 'FAIL')
                self.assertEqual(set(result['differences']), set(change))

    def test_invalid_isolated_snapshot_is_unknown(self):
        self.write_state(self.isolated_edge)
        result = RECONCILE.probe(self.state, self.command())
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'persistent_edge_snapshot_invalid')

    def test_invalid_stage_is_unknown(self):
        self.write_state(self.original_edge, stage='isolating')
        result = RECONCILE.probe(self.state, self.command())
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'persistent_journal_not_isolated')

    def test_missing_container_is_unknown_with_no_default_pass(self):
        command = Mock(side_effect=[subprocess.CompletedProcess([], 0, '', ''),
                                    subprocess.CompletedProcess([], 1, '', 'no such container')])
        result = RECONCILE.probe(self.state, command)
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'docker_inspect_failed')

    def test_docker_unavailable_is_unknown(self):
        command = Mock(side_effect=[subprocess.CompletedProcess([], 0, '', ''), FileNotFoundError('docker')])
        result = RECONCILE.probe(self.state, command)
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'FileNotFoundError')

    def test_probe_timeout_is_unknown(self):
        command = Mock(side_effect=[subprocess.CompletedProcess([], 0, '', ''),
                                    subprocess.TimeoutExpired(['docker'], 15)])
        result = RECONCILE.probe(self.state, command)
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'TimeoutExpired')

    def test_invalid_persistent_state_is_unknown(self):
        self.state.write_text('{', encoding='utf-8')
        result = RECONCILE.probe(self.state, self.command())
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'persistent_state_invalid')

    def test_missing_or_malformed_snapshot_is_unknown(self):
        invalid = [[], {}, {'schema': 'wrong', 'stage': 'isolated', 'edge': self.original_edge},
                   {'schema': 'clearapi.controlled-traffic/v1', 'stage': 'isolated', 'edge': None}]
        for change in ({'running': None}, {'paused': 0}, {'id': ''}, {'image': 'unknown'},
                       {'restart_policy': []}, {'restart_policy': {'Name': 'no'}},
                       {'restart_policy': {'Name': 'unless-stopped', 'MaximumRetryCount': True}}):
            invalid.append({'schema': 'clearapi.controlled-traffic/v1', 'stage': 'isolated',
                            'edge': dict(self.original_edge, **change)})
        for value in invalid:
            with self.subTest(value=value):
                self.state.write_text(json.dumps(value), encoding='utf-8')
                result = RECONCILE.probe(self.state, self.command())
                self.assertEqual(result['status'], 'UNKNOWN')
                self.assertEqual(result['observed'], self.isolated_edge)

    def test_inspect_invalid_json_duplicate_fields_and_shape_are_unknown(self):
        for output in ('{', '[]', '[{}]', '[null]', '[{"Id":"one","Id":"two"}]'):
            with self.subTest(output=output):
                command = Mock(side_effect=[subprocess.CompletedProcess([], 0, '', ''),
                                            subprocess.CompletedProcess([], 0, output, '')])
                result = RECONCILE.probe(self.state, command)
                self.assertEqual(result['status'], 'UNKNOWN')
                self.assertEqual(result['persisted'], self.original_edge)
        malformed = dict(self.isolated_edge, running=None)
        self.assertEqual(RECONCILE.probe(self.state, self.command(malformed))['status'], 'UNKNOWN')

    def test_missing_persistent_state_and_duplicate_fields_are_unknown(self):
        result = RECONCILE.probe(self.root / 'missing.json', self.command())
        self.assertEqual(result['status'], 'UNKNOWN')
        self.assertEqual(result['reason'], 'persistent_state_missing')
        self.state.write_text('{"stage":"isolated","stage":"isolating"}', encoding='utf-8')
        self.assertEqual(RECONCILE.probe(self.state, self.command())['status'], 'UNKNOWN')

    def test_append_audit_log_is_append_only_and_rejects_production_path(self):
        log = self.root / 'audit.jsonl'
        first = {'status': 'FAIL', 'n': 1}
        second = {'status': 'UNKNOWN', 'n': 2}
        RECONCILE.append_audit_record(log, first)
        before = log.read_bytes()
        RECONCILE.append_audit_record(log, second)
        self.assertTrue(log.read_bytes().startswith(before))
        self.assertEqual([json.loads(line) for line in log.read_text().splitlines()], [first, second])
        with self.assertRaisesRegex(ValueError, 'production audit paths'):
            RECONCILE.append_audit_record(Path('/opt/clearapi/audit.jsonl'), first)

    def test_audit_rejects_outside_directory_and_state_alias_without_writes(self):
        before = self.state.read_bytes()
        with self.assertRaisesRegex(ValueError, 'local evidence'):
            RECONCILE.append_audit_record(self.root.parent / 'unapproved-audit.jsonl', {})
        with self.assertRaisesRegex(ValueError, 'alias'):
            RECONCILE.append_audit_record(self.state, {}, self.state)
        alias = self.root / 'alias.jsonl'
        os.link(self.state, alias)
        with self.assertRaisesRegex(ValueError, 'alias'):
            RECONCILE.append_audit_record(alias, {}, self.state)
        with self.assertRaisesRegex(ValueError, 'hard links'):
            RECONCILE.append_audit_record(alias, {})
        self.assertEqual(self.state.read_bytes(), before)

    def test_cli_emits_and_appends_full_unknown_record_with_nonzero_exit(self):
        audit = self.root / 'audit.jsonl'
        with patch.object(sys, 'argv', ['reconcile', '--state', str(self.state), '--audit-log', str(audit)]), \
                patch.object(RECONCILE.subprocess, 'run', side_effect=FileNotFoundError('systemctl')), \
                patch.object(sys, 'stdout', new_callable=io.StringIO) as output:
            self.assertEqual(RECONCILE.main(), 1)
        record = json.loads(output.getvalue())
        self.assertEqual(record['status'], 'UNKNOWN')
        self.assertEqual(record, json.loads(audit.read_text()))
        self.assertEqual(record['persisted'], self.original_edge)
        self.assertIn('observed_at_utc', record)

    def test_stdout_only_probes_without_creating_any_audit_file(self):
        before = {path.name: path.read_bytes() for path in self.root.iterdir()}
        with patch.object(sys, 'argv', ['reconcile', '--state', str(self.state), '--stdout-only']), \
                patch.object(RECONCILE.subprocess, 'run', self.command()), \
                patch.object(RECONCILE, 'append_audit_record') as append, \
                patch.object(sys, 'stdout', new_callable=io.StringIO) as output:
            self.assertEqual(RECONCILE.main(), 0)
        append.assert_not_called()
        self.assertEqual(json.loads(output.getvalue())['status'], 'PASS')
        self.assertEqual({path.name: path.read_bytes() for path in self.root.iterdir()}, before)

    @unittest.skipUnless(os.name == 'posix', 'POSIX symlink fixture')
    def test_audit_symlink_does_not_change_existing_record(self):
        target = self.root / 'existing.jsonl'
        target.write_text('{"previous":true}\n')
        alias = self.root / 'symlink.jsonl'
        alias.symlink_to(target)
        with self.assertRaisesRegex(ValueError, 'symlinks'):
            RECONCILE.append_audit_record(alias, {})
        self.assertEqual(target.read_text(), '{"previous":true}\n')


if __name__ == '__main__':
    unittest.main()
