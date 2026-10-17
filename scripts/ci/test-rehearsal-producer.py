#!/usr/bin/env python3
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PRODUCER = ROOT / "scripts/ci/produce-gate-receipt.py"
ASSEMBLER = ROOT / "scripts/ci/assemble-release-proof.py"
WORKFLOW = ROOT / ".github/workflows/deployment-rehearsal.yml"


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
                       REHEARSAL_RC_RUN_ID="123")
            result = subprocess.run([sys.executable, str(PRODUCER), "--gate", "runtime_integrity",
                                     "--output", str(output), "--", sys.executable, "-c", "raise SystemExit(7)"],
                                    env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            receipt = json.loads(output.read_text())
            self.assertEqual(receipt["conclusion"], "failure")
            self.assertEqual(receipt["source_commit"], "a" * 40)

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


if __name__ == "__main__":
    unittest.main()
