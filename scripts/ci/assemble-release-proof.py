#!/usr/bin/env python3
"""Assemble a release proof only when every receipt really passed."""

import argparse
import hashlib
import json
from pathlib import Path


GATES = {
    "runtime_integrity", "billing", "backup_restore", "executor_pairing",
    "sqlite_upgrade_idempotency", "mysql_upgrade_idempotency",
    "postgres_upgrade_idempotency", "gateway_executor_provider_usage_billing",
    "replay_and_duplicate_charge", "state_isolation", "safe_rollback",
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--receipts", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--executor-repository", required=True)
    parser.add_argument("--executor-source-commit", required=True)
    parser.add_argument("--executor-digest", required=True)
    parser.add_argument("--executor-binary-sha256", required=True)
    parser.add_argument("--previous-source-commit", required=True)
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text())
    checks = {}
    for gate in sorted(GATES):
        path = args.receipts / f"{gate}.json"
        if not path.is_file() or path.is_symlink():
            raise SystemExit(f"missing receipt: {gate}")
        raw = path.read_bytes()
        receipt = json.loads(raw)
        if receipt.get("schema") != "clearapi.gate-evidence/v1" or receipt.get("gate") != gate:
            raise SystemExit(f"invalid receipt contract: {gate}")
        if receipt.get("conclusion") != "success":
            raise SystemExit(f"gate failed: {gate}")
        if receipt.get("source_commit") != manifest.get("source_commit"):
            raise SystemExit(f"stale receipt source: {gate}")
        if receipt.get("gateway_digest") != manifest.get("image_digest"):
            raise SystemExit(f"stale receipt digest: {gate}")
        checks[gate] = {"conclusion": "success", "file": path.name,
                        "sha256": hashlib.sha256(raw).hexdigest()}
    proof = {
        "schema": "clearapi.automatic-release/v1",
        "source_commit": manifest["source_commit"],
        "rc_run_id": int(manifest["workflow_run"]),
        "rc_run_attempt": int(manifest["workflow_run_attempt"]),
        "gateway_digest": manifest["image_digest"],
        "gateway_binary_sha256": manifest["binary_sha256"],
        "previous_source_commit": args.previous_source_commit,
        "migration": {"mode": "no_schema_change"},
        "executor": {
            "image_repository": args.executor_repository,
            "source_repository": "https://github.com/a3225865526-wq/CLIProxyAPI",
            "source_commit": args.executor_source_commit,
            "image_digest": args.executor_digest,
            "binary_sha256": args.executor_binary_sha256,
            "paired_gateway_commit": manifest["source_commit"],
        },
        "checks": checks,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(proof, sort_keys=True, indent=2) + "\n")


if __name__ == "__main__":
    main()
