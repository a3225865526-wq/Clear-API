#!/usr/bin/env python3
import json
import ast
from copy import deepcopy
import hashlib
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parents[2]
PRODUCER = ROOT / "scripts/ci/produce-gate-receipt.py"
ASSEMBLER = ROOT / "scripts/ci/assemble-release-proof.py"
WORKFLOW = ROOT / ".github/workflows/deployment-rehearsal.yml"
VERIFIER = ROOT / "scripts/ci/verify-rehearsal-input.py"


class ProducerContractTest(unittest.TestCase):
    def test_workflow_runs_all_consumer_gates_and_assembles_proof(self):
        workflow = WORKFLOW.read_text()
        for gate in (
            "runtime_integrity", "billing", "backup_restore", "executor_pairing",
            "sqlite_upgrade_idempotency", "mysql_upgrade_idempotency",
            "postgres_upgrade_idempotency", "gateway_executor_provider_usage_billing",
            "replay_and_duplicate_charge", "state_isolation", "safe_rollback",
        ):
            self.assertIn(f"--gate {gate}", workflow)
            self.assertIn(f"receipts/{gate}.json", workflow)
        self.assertIn("assemble-release-proof.py", workflow)
        self.assertIn("deployment-proof-${{ github.run_id }}-${{ github.run_attempt }}", workflow)

    def test_off_business_and_pool_gates_execute_real_tests(self):
        workflow = WORKFLOW.read_text()
        self.assertIn("--gate backup_restore --output receipts/backup_restore.json -- python3 -B scripts/ci/test-backup-identity.py", workflow)
        self.assertIn("--gate safe_rollback --output receipts/safe_rollback.json -- python3 -B scripts/ci/test-production-deploy.py -k rollback", workflow)
        self.assertIn("--gate ordinary_provider_usage_billing", workflow)
        self.assertIn("go test -count=1 ./e2e/commercial -run '^TestCommercialLifecycle$'", workflow)
        self.assertIn("--gate pool_disabled", workflow)
        self.assertIn("go test -count=1 ./model -run '^(TestPoolMasterFailClosed|TestPoolGateMatrix|TestPoolRoutingOptionSyncDoesNotRestoreOlderEnabledRevision)$'", workflow)
        self.assertNotIn("--kind ordinary-provider", workflow)
        self.assertNotIn("--kind pool-disabled", workflow)

    def test_off_workflow_selects_its_exact_gates_and_never_claims_executor_pairing(self):
        workflow = WORKFLOW.read_text()
        assembly = workflow.split('- name: Assemble proof only after all gates pass', 1)[1]
        script = textwrap.dedent(assembly.split('run: |', 1)[1].split('      - name:', 1)[0])
        script = script.replace('scripts/ci/assemble-release-proof.py', str(ASSEMBLER))
        script = script.replace('--manifest rc/gateway-image-release.json \\\n+            "${SCHEMA_ARGS[@]}" --receipts', '--manifest rc/gateway-image-release.json --schema-rehearsal rc/schema-rehearsal.json \\\n+            "${SCHEMA_ARGS[@]}" --receipts')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'rc').mkdir()
            (root / 'receipts').mkdir()
            manifest = {'source_commit': 'a' * 40, 'image_digest': 'sha256:' + 'b' * 64,
                        'workflow_run': 123, 'workflow_run_attempt': 1, 'binary_sha256': 'c' * 64,
                        'schema_transition': {'schema_version': 'pg03-schema-rehearsal-v1',
                            'pool_mode': 'off', 'candidate_binary_sha256': 'c' * 64,
                            'candidate_image_digest': 'sha256:' + 'b' * 64,
                            'candidate_image_id': 'sha256:' + 'e' * 64,
                            'production_schema': 'unverified',
                            'profile': 'isolated_schema_only_master_single_postgres',
                            'postgres_image_ref': 'postgres@sha256:00bc86618629af00d2937fdc5a5d63db3ff8450acf52f0636ec813c7f4902929',
                            'postgres_version_num': '170006',
                            'source_commit': 'a' * 40, 'startup_passes': 2,
                            'source_schema_sha256': '1' * 64, 'target_schema_sha256': '1' * 64},
                        'schema_rehearsal_sha256': ''}
            (root / 'rc/schema-rehearsal.json').write_text(json.dumps(manifest['schema_transition'], indent=2))
            manifest['schema_rehearsal_sha256'] = __import__('hashlib').sha256(
                (root / 'rc/schema-rehearsal.json').read_bytes()).hexdigest()
            (root / 'rc/gateway-image-release.json').write_text(json.dumps(manifest))
            gates = {'runtime_integrity', 'billing', 'backup_restore', 'pool_disabled',
                     'sqlite_upgrade_idempotency', 'mysql_upgrade_idempotency',
                     'postgres_upgrade_idempotency', 'ordinary_provider_usage_billing',
                     'replay_and_duplicate_charge', 'state_isolation', 'safe_rollback'}
            for gate in gates:
                (root / 'receipts' / (gate + '.json')).write_text(json.dumps({
                    'schema': 'clearapi.gate-evidence/v2', 'pool_mode': 'off', 'gate': gate,
                    'conclusion': 'success', 'source_commit': manifest['source_commit'],
                    'gateway_digest': manifest['image_digest'], 'rc_run_id': 123, 'rc_run_attempt': 1}))
            environment = dict(os.environ, REHEARSAL_POOL_MODE='off', RELEASE_STAGE='commercial',
                               PREVIOUS_SOURCE_COMMIT='d' * 40)
            environment['PYTHONPATH'] = str(ROOT / 'scripts/ci')
            for name in tuple(environment):
                if name.startswith('CLEARAPI_EXECUTOR_'):
                    environment.pop(name)
            result = subprocess.run(['bash', '-c', script], cwd=root, env=environment,
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            proof = json.loads((root / 'release-proof.json').read_text())
            self.assertEqual(proof['pool_mode'], 'off')
            self.assertEqual(set(proof['checks']), gates)
            self.assertNotIn('executor', proof)

    def test_off_rc_uses_commit_workflow_artifact_and_schema_identity(self):
        workflow = WORKFLOW.read_text()
        self.assertIn("release-candidate.yml", workflow)
        self.assertIn('clearapi-off-rc-${REHEARSAL_RC_RUN_ID}-${REHEARSAL_RC_RUN_ATTEMPT}', workflow)
        policy = (ROOT / 'scripts/ci/release_proof.py').read_text()
        self.assertIn('validate_off_rc_artifact', workflow + policy)
        self.assertIn("'schema') != 'clearapi.release-candidate/v1'", policy)
        self.assertIn("'pool_mode') != 'off'", policy)
        self.assertIn("'candidate_image_digest') != manifest.get('image_digest')", policy)
        self.assertIn("'candidate_binary_sha256') != manifest.get('binary_sha256')", policy)

    def test_failed_command_persists_bound_failure_receipt(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "runtime_integrity.json"
            env = dict(os.environ, REHEARSAL_SOURCE_COMMIT="a" * 40,
                       REHEARSAL_GATEWAY_DIGEST="sha256:" + "b" * 64,
                       REHEARSAL_RC_RUN_ID="123", REHEARSAL_RC_RUN_ATTEMPT="1")
            result = subprocess.run([sys.executable, str(PRODUCER), "--gate", "runtime_integrity",
                                     "--output", str(output), "--", sys.executable, "-c", "raise SystemExit(7)"],
                                    env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            receipt = json.loads(output.read_text())
            self.assertEqual(receipt["conclusion"], "failure")
            self.assertEqual(receipt["source_commit"], "a" * 40)
            self.assertEqual(receipt["rc_run_attempt"], 1)

    def test_assembler_rejects_missing_gate(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest = root / "manifest.json"
            manifest.write_text(json.dumps({"source_commit": "a" * 40, "image_digest": "sha256:" + "b" * 64,
                                            "workflow_run": 123, "workflow_run_attempt": 1,
                                            "binary_sha256": "c" * 64}))
            receipts = root / "receipts"
            receipts.mkdir()
            result = subprocess.run([sys.executable, str(ASSEMBLER), "--manifest", str(manifest),
                                     "--receipts", str(receipts), "--output", str(root / "proof.json"),
                                     "--executor-repository", "ghcr.io/a3225865526-wq/executor",
                                     "--executor-source-commit", "d" * 40,
                                     "--executor-digest", "sha256:" + "e" * 64,
                                     "--executor-binary-sha256", "f" * 64,
                                     "--previous-source-commit", "1" * 40], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing receipt", result.stderr + result.stdout)

    def test_controlled_assembler_defers_only_provider_and_never_invents_pass(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest = {'source_commit': 'a' * 40, 'image_digest': 'sha256:' + 'b' * 64,
                        'workflow_run': 123, 'workflow_run_attempt': 1, 'binary_sha256': 'c' * 64,
                        'schema_transition': {'schema_version': 'pg03-schema-rehearsal-v1',
                            'source_commit': 'a' * 40, 'startup_passes': 2,
                            'source_schema_sha256': '1' * 64, 'target_schema_sha256': '1' * 64}}
            (root / 'manifest.json').write_text(json.dumps(manifest))
            gates = set(re.findall(r'--gate ([a-z_]+)', WORKFLOW.read_text()))
            for gate in gates - {'gateway_executor_provider_usage_billing'}:
                (root / (gate + '.json')).write_text(json.dumps({
                    'schema': 'clearapi.gate-evidence/v1', 'gate': gate, 'conclusion': 'success',
                    'source_commit': manifest['source_commit'], 'gateway_digest': manifest['image_digest'],
                    'rc_run_id': 123, 'rc_run_attempt': 1}))
            command = [sys.executable, str(ASSEMBLER), '--manifest', str(root / 'manifest.json'),
                       '--receipts', str(root), '--output', str(root / 'proof.json'),
                       '--executor-repository', 'ghcr.io/a3225865526-wq/executor',
                       '--executor-source-commit', 'd' * 40, '--executor-digest', 'sha256:' + 'e' * 64,
                       '--executor-binary-sha256', 'f' * 64, '--previous-source-commit', '2' * 40]
            result = subprocess.run(command + ['--release-stage', 'controlled'], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            proof = json.loads((root / 'proof.json').read_bytes())
            self.assertEqual(proof['schema'], 'clearapi.controlled-release/v1')
            self.assertEqual(proof['public_traffic'], 'blocked')
            self.assertEqual(proof['provider_acceptance'], 'deferred_to_owner_after_deployment')
            self.assertNotIn('gateway_executor_provider_usage_billing', proof['checks'])
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('missing receipt', result.stderr)
            path = root / 'billing.json'
            receipt = json.loads(path.read_text())
            receipt['conclusion'] = 'failure'
            path.write_text(json.dumps(receipt))
            result = subprocess.run(command + ['--release-stage', 'controlled'], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('gate failed', result.stderr)

    def test_assembler_binds_changed_schema_and_rejects_stale_receipt(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest = {"source_commit": "a" * 40, "image_digest": "sha256:" + "b" * 64,
                        "workflow_run": 123, "workflow_run_attempt": 1, "binary_sha256": "c" * 64,
                        "schema_transition": {"schema_version": "pg03-schema-rehearsal-v1",
                            "source_commit": "a" * 40, "startup_passes": 2,
                            "source_schema_sha256": "1" * 64, "target_schema_sha256": "1" * 64}}
            workflow = WORKFLOW.read_text()
            gates = set(re.findall(r"--gate ([a-z_]+)", workflow))
            for gate in gates:
                (root / f"{gate}.json").write_text(json.dumps({
                    "schema": "clearapi.gate-evidence/v1", "gate": gate, "conclusion": "success",
                    "source_commit": manifest["source_commit"], "gateway_digest": manifest["image_digest"],
                    "rc_run_id": 123, "rc_run_attempt": 1}))
            command = [sys.executable, str(ASSEMBLER), "--manifest", str(root / "manifest.json"),
                       "--receipts", str(root), "--output", str(root / "proof.json"),
                       "--executor-repository", "ghcr.io/a3225865526-wq/executor",
                       "--executor-source-commit", "d" * 40, "--executor-digest", "sha256:" + "e" * 64,
                       "--executor-binary-sha256", "f" * 64, "--previous-source-commit", "2" * 40]
            (root / "manifest.json").write_text(json.dumps(manifest))
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            # Remove only this test-owned synthetic proof before negative cases.
            (root / "proof.json").unlink()
            manifest["schema_transition"]["target_schema_sha256"] = "3" * 64
            (root / "manifest.json").write_text(json.dumps(manifest))
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            proof = json.loads((root / "proof.json").read_text())
            self.assertEqual(proof["migration"]["mode"], "in_place_startup_migration")
            manifest["schema_transition"]["target_schema_sha256"] = "1" * 64
            (root / "manifest.json").write_text(json.dumps(manifest))
            receipt = json.loads((root / "backup_restore.json").read_text())
            receipt["rc_run_id"] = 122
            (root / "backup_restore.json").write_text(json.dumps(receipt))
            (root / "proof.json").unlink()
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertIn("RC run", result.stderr)
            self.assertFalse((root / "proof.json").exists())

    def test_operator_evidence_cannot_cross_kind_rc_schema_or_target(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "manifest.json"
            manifest = {"source_commit": "a" * 40, "image_digest": "sha256:" + "b" * 64,
                        "workflow_run": "123", "workflow_run_attempt": "1",
                        "schema_transition": {"source_schema_sha256": "1" * 64,
                                              "target_schema_sha256": "2" * 64}}
            path.write_text(json.dumps(manifest))
            base = {"schema": "clearapi.rehearsal-evidence/v1", "conclusion": "success",
                    "source_commit": manifest["source_commit"], "gateway_digest": manifest["image_digest"],
                    "rc_run_id": 123, "rc_run_attempt": 1,
                    "manifest_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                    "target_id": "unit-test-target", **manifest["schema_transition"]}
            for kind in ("backup", "rollback", "provider"):
                valid = dict(base, kind=kind)
                if kind == "rollback":
                    valid["rollback_proven"] = True
                cases = [(None, None), ("kind", "executor"), ("rc_run_id", 122), ("rc_run_id", "123"),
                         ("rc_run_attempt", 2), ("manifest_sha256", "0" * 64),
                         ("source_schema_sha256", "0" * 64), ("target_schema_sha256", "0" * 64),
                         ("target_id", "other-target")]
                for field, value in cases:
                    with self.subTest(kind=kind, field=field, value=value):
                        evidence = deepcopy(valid)
                        if field:
                            evidence[field] = value
                        env = dict(os.environ, CLEARAPI_REHEARSAL_TARGET_ID="unit-test-target")
                        env[f"CLEARAPI_REHEARSAL_{kind.upper()}_EVIDENCE"] = json.dumps(evidence)
                        result = subprocess.run([sys.executable, str(VERIFIER), "--kind", kind,
                                                 "--manifest", str(path)], env=env, capture_output=True, text=True)
                        self.assertEqual(result.returncode == 0, field is None, result.stderr)

            # The historical operator generator described two candidate
            # startups but explicitly disclaimed rollback. A matching RC and
            # success conclusion must not let that produce a success receipt.
            for proven in (None, False, "true", 1):
                with self.subTest(rollback_proven=proven):
                    evidence = dict(base, kind="rollback", startup_passes=2,
                                    pass1_schema_sha256="2" * 64,
                                    pass2_schema_sha256="2" * 64,
                                    production_restore_executed=False)
                    if proven is not None:
                        evidence["rollback_proven"] = proven
                    env = dict(os.environ, CLEARAPI_REHEARSAL_TARGET_ID="unit-test-target",
                               CLEARAPI_REHEARSAL_ROLLBACK_EVIDENCE=json.dumps(evidence),
                               REHEARSAL_SOURCE_COMMIT=manifest["source_commit"],
                               REHEARSAL_GATEWAY_DIGEST=manifest["image_digest"],
                               REHEARSAL_RC_RUN_ID="123", REHEARSAL_RC_RUN_ATTEMPT="1")
                    output = Path(temp) / "safe_rollback.json"
                    result = subprocess.run([sys.executable, str(PRODUCER), "--gate", "safe_rollback",
                                             "--output", str(output), "--", sys.executable,
                                             str(VERIFIER), "--kind", "rollback", "--manifest", str(path)],
                                            env=env, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    receipt = json.loads(output.read_text())
                    self.assertEqual(receipt["conclusion"], "failure")
                    self.assertIn("does not prove rollback recovery", receipt["stderr_tail"])

    def test_executor_must_match_existing_release_allowlist(self):
        tree = ast.parse((ROOT / "scripts/pg03-release-control.py").read_text())
        approved = {n.targets[0].id: ast.literal_eval(n.value) for n in tree.body
                    if isinstance(n, ast.Assign) and isinstance(n.targets[0], ast.Name)
                    and n.targets[0].id in {"EXECUTOR", "EXECUTOR_REVISION", "EXECUTOR_BINARY"}}
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "manifest.json"
            path.write_text(json.dumps({"source_commit": "a" * 40, "image_digest": "sha256:" + "b" * 64}))
            repo, digest = approved["EXECUTOR"].split("@")
            env = dict(os.environ, CLEARAPI_EXECUTOR_IMAGE_REPOSITORY=repo,
                       CLEARAPI_EXECUTOR_SOURCE_COMMIT=approved["EXECUTOR_REVISION"],
                       CLEARAPI_EXECUTOR_IMAGE_DIGEST=digest, CLEARAPI_EXECUTOR_BINARY_SHA256=approved["EXECUTOR_BINARY"],
                       CLEARAPI_EXECUTOR_PAIRED_GATEWAY_COMMIT="a" * 40)
            command = [sys.executable, str(VERIFIER), "--kind", "executor", "--manifest", str(path)]
            self.assertEqual(subprocess.run(command, env=env, capture_output=True).returncode, 0)
            env["CLEARAPI_EXECUTOR_IMAGE_DIGEST"] = "sha256:" + "f" * 64
            result = subprocess.run(command, env=env, capture_output=True, text=True)
            self.assertIn("approved release contract", result.stderr)


if __name__ == "__main__":
    unittest.main()
