#!/usr/bin/env python3
"""Assemble a release proof only when every receipt really passed."""

import argparse
import hashlib
import json
from pathlib import Path
import re

from release_proof import (PG03_GATES, required_gates, validate_receipt_mode,
                           validate_schema_transition)


GATES = PG03_GATES


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--schema-rehearsal", type=Path)
    parser.add_argument("--receipts", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--pool-mode", choices=('off', 'pg03'), default='pg03')
    parser.add_argument("--executor-repository")
    parser.add_argument("--executor-source-commit")
    parser.add_argument("--executor-digest")
    parser.add_argument("--executor-binary-sha256")
    parser.add_argument("--previous-source-commit", required=True)
    parser.add_argument("--release-stage", choices=("commercial", "controlled"), default="commercial")
    args = parser.parse_args()
    gates = required_gates(args.pool_mode, args.release_stage)
    executor_args = (args.executor_repository, args.executor_source_commit,
                     args.executor_digest, args.executor_binary_sha256)
    if args.pool_mode == 'pg03' and not all(executor_args):
        parser.error('PG03 requires all four Executor identity arguments')
    if args.pool_mode == 'off' and any(value is not None for value in executor_args):
        parser.error('off proof must not claim an Executor dependency')
    manifest = json.loads(args.manifest.read_text())
    checks = {}
    for gate in sorted(gates):
        path = args.receipts / f"{gate}.json"
        if not path.is_file() or path.is_symlink():
            raise SystemExit(f"missing receipt: {gate}")
        raw = path.read_bytes()
        receipt = json.loads(raw)
        validate_receipt_mode(receipt, args.pool_mode)
        if receipt.get("gate") != gate:
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
    validate_schema_transition(transition, manifest, args.pool_mode)
    if args.pool_mode == 'off':
        if args.schema_rehearsal is None or args.schema_rehearsal.is_symlink() or not args.schema_rehearsal.is_file():
            raise SystemExit("OFF proof requires the exact RC schema rehearsal artifact")
        rehearsal = json.loads(args.schema_rehearsal.read_bytes())
        if rehearsal != transition:
            raise SystemExit("OFF schema rehearsal artifact does not match the RC manifest")
        schema_rehearsal_sha256 = hashlib.sha256(args.schema_rehearsal.read_bytes()).hexdigest()
        if manifest.get('schema_rehearsal_sha256') != schema_rehearsal_sha256:
            raise SystemExit("OFF schema rehearsal digest does not match the RC manifest")
    source_schema = transition.get("source_schema_sha256", "")
    target_schema = transition.get("target_schema_sha256", "")
    if not re.fullmatch(r"[0-9a-f]{64}", source_schema) or not re.fullmatch(r"[0-9a-f]{64}", target_schema):
        raise SystemExit("RC schema transition is missing or malformed")
    if args.pool_mode == 'off':
        migration = {
            "mode": "off_isolated_startup_migration",
            "source_schema_sha256": source_schema,
            "target_schema_sha256": target_schema,
            "startup_passes": 2,
            "production_schema": "unverified",
        }
    elif source_schema == target_schema:
        migration = {"mode": "no_schema_change"}
    else:
        # PG03 forbids external migration flags: the candidate image applies its
        # own additive migrations during startup. A real upgrade is therefore only
        # provable when the RC rehearsal ran that startup twice on the same
        # database and observed an idempotent, converged schema.
        if transition.get("schema_version") != "pg03-schema-rehearsal-v1":
            raise SystemExit("RC schema transition is not a PG03 schema rehearsal")
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
        "schema": ("clearapi.controlled-release/v1" if args.release_stage == "controlled"
                   else ("clearapi.automatic-release/v2" if args.pool_mode == 'off'
                         else "clearapi.automatic-release/v1")),
        "pool_mode": args.pool_mode,
        "source_commit": manifest["source_commit"],
        "rc_run_id": int(manifest["workflow_run"]),
        "rc_run_attempt": int(manifest["workflow_run_attempt"]),
        "gateway_digest": manifest["image_digest"],
        "gateway_binary_sha256": manifest["binary_sha256"],
        "previous_source_commit": args.previous_source_commit,
        "migration": migration,
        "checks": checks,
    }
    if args.pool_mode == 'pg03':
        proof['executor'] = {
            "image_repository": args.executor_repository,
            "source_repository": "https://github.com/a3225865526-wq/CLIProxyAPI",
            "source_commit": args.executor_source_commit,
            "image_digest": args.executor_digest,
            "binary_sha256": args.executor_binary_sha256,
            "paired_gateway_commit": manifest["source_commit"],
        }
    else:
        proof.update(repository='a3225865526-wq/Clear-API',
                     rc_artifact=f"clearapi-off-rc-{manifest['workflow_run']}-{manifest['workflow_run_attempt']}",
                     manifest_sha256=hashlib.sha256(args.manifest.read_bytes()).hexdigest(),
                     schema_rehearsal_sha256=schema_rehearsal_sha256)
    if args.release_stage == "controlled":
        proof["public_traffic"] = "blocked"
        proof["provider_acceptance"] = "deferred_to_owner_after_deployment"
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(proof, sort_keys=True, indent=2) + "\n")


if __name__ == "__main__":
    main()
