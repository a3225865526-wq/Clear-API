#!/usr/bin/env python3
"""Assemble a release proof only when every receipt really passed."""

import argparse
import hashlib
import json
from pathlib import Path
import re


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
        if type(receipt.get("rc_run_id")) is not int or receipt["rc_run_id"] != int(manifest["workflow_run"]):
            raise SystemExit(f"stale receipt RC run: {gate}")
        if type(receipt.get("rc_run_attempt")) is not int or receipt["rc_run_attempt"] != int(manifest["workflow_run_attempt"]):
            raise SystemExit(f"stale receipt RC attempt: {gate}")
        checks[gate] = {"conclusion": "success", "file": path.name,
                        "sha256": hashlib.sha256(raw).hexdigest()}
    transition = manifest.get("schema_transition", {})
    source_schema = transition.get("source_schema_sha256", "")
    target_schema = transition.get("target_schema_sha256", "")
    if not re.fullmatch(r"[0-9a-f]{64}", source_schema) or not re.fullmatch(r"[0-9a-f]{64}", target_schema):
        raise SystemExit("RC schema transition is missing or malformed")
    if source_schema == target_schema:
        migration = {"mode": "no_schema_change"}
    else:
        # PG03 forbids external migration flags: the candidate image applies its
        # own additive migrations during startup. A real upgrade is therefore only
        # provable when the RC rehearsal ran that startup twice on the same
        # database and observed an idempotent, converged schema.
        if transition.get("schema_version") != "pg03-schema-rehearsal-v1":
            raise SystemExit("RC schema transition changed but is not a PG03 schema rehearsal")
        if transition.get("startup_passes") != 2:
            raise SystemExit("in-place migration requires two idempotent startup passes")
        if transition.get("source_commit") != manifest.get("source_commit"):
            raise SystemExit("in-place migration rehearsal does not bind this candidate")
        migration = {
            "mode": "in_place_startup_migration",
            "source_schema_sha256": source_schema,
            "target_schema_sha256": target_schema,
            "startup_passes": 2,
        }
    proof = {
        "schema": "clearapi.automatic-release/v1",
        "source_commit": manifest["source_commit"],
        "rc_run_id": int(manifest["workflow_run"]),
        "rc_run_attempt": int(manifest["workflow_run_attempt"]),
        "gateway_digest": manifest["image_digest"],
        "gateway_binary_sha256": manifest["binary_sha256"],
        "previous_source_commit": args.previous_source_commit,
        "migration": migration,
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
