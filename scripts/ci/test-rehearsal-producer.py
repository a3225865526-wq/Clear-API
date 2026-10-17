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

    def test_assembler_rejects_changed_schema_and_receipt_from_another_rc(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest = {"source_commit": "a" * 40, "image_digest": "sha256:" + "b" * 64,
                        "workflow_run": 123, "workflow_run_attempt": 1, "binary_sha256": "c" * 64,
                        "schema_transition": {"source_schema_sha256": "1" * 64,
                                              "target_schema_sha256": "1" * 64}}
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
            self.assertIn("RC schema transition", result.stderr)
            self.assertFalse((root / "proof.json").exists())
            manifest["schema_transition"]["target_schema_sha256"] = "1" * 64
            (root / "manifest.json").write_text(json.dumps(manifest))
            receipt = json.loads((root / "backup_restore.json").read_text())
            receipt["rc_run_id"] = 122
            (root / "backup_restore.json").write_text(json.dumps(receipt))
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
