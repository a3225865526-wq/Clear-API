#!/usr/bin/env python3
"""Validate operator-supplied evidence without exposing secret values."""

import argparse
import ast
import hashlib
import json
import os
import re
from pathlib import Path


SHA = re.compile(r"^[0-9a-f]{40}$")
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
HEX = re.compile(r"^[0-9a-f]{64}$")


def require(value, message):
    if not value:
        raise SystemExit(message)
    return value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kind", required=True, choices=("executor", "provider", "backup", "rollback"))
    parser.add_argument("--manifest", type=Path, required=True)
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text())
    source = require(manifest.get("source_commit"), "manifest source missing")
    digest = require(manifest.get("image_digest"), "manifest image digest missing")
    if not SHA.fullmatch(source) or not DIGEST.fullmatch(digest):
        raise SystemExit("manifest identity malformed")
    if args.kind == "executor":
        repo = require(os.environ.get("CLEARAPI_EXECUTOR_IMAGE_REPOSITORY"), "Executor repository is not configured")
        commit = require(os.environ.get("CLEARAPI_EXECUTOR_SOURCE_COMMIT"), "Executor source is not configured")
        image = require(os.environ.get("CLEARAPI_EXECUTOR_IMAGE_DIGEST"), "Executor digest is not configured")
        binary = require(os.environ.get("CLEARAPI_EXECUTOR_BINARY_SHA256"), "Executor binary identity is not configured")
        if not re.fullmatch(r"ghcr.io/a3225865526-wq/[a-z0-9._/-]+", repo) or not SHA.fullmatch(commit) or not DIGEST.fullmatch(image) or not HEX.fullmatch(binary):
            raise SystemExit("Executor identity is malformed")
        # Reuse the formal release allowlist without importing its Linux-only CLI.
        control = Path(__file__).resolve().parents[1] / "pg03-release-control.py"
        approved = {node.targets[0].id: ast.literal_eval(node.value)
                    for node in ast.parse(control.read_text()).body
                    if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name)
                    and node.targets[0].id in {"EXECUTOR", "EXECUTOR_REVISION", "EXECUTOR_BINARY"}}
        if (repo + "@" + image != approved["EXECUTOR"] or commit != approved["EXECUTOR_REVISION"]
                or binary != approved["EXECUTOR_BINARY"]):
            raise SystemExit("Executor identity differs from the approved release contract")
        if os.environ.get("CLEARAPI_EXECUTOR_PAIRED_GATEWAY_COMMIT") != source:
            raise SystemExit("Executor is not paired with this Gateway source")
    else:
        evidence = require(os.environ.get(f"CLEARAPI_REHEARSAL_{args.kind.upper()}_EVIDENCE"),
                           f"{args.kind} rehearsal evidence is not configured")
        try:
            data = json.loads(evidence)
        except json.JSONDecodeError:
            path = Path(evidence)
            if not path.is_file() or path.is_symlink():
                raise SystemExit(f"{args.kind} rehearsal evidence is not JSON or a regular file")
            data = json.loads(path.read_text())
        # This is a trusted operator input, not a cryptographically signed result.
        if data.get("schema") != "clearapi.rehearsal-evidence/v1" or data.get("conclusion") != "success":
            raise SystemExit(f"{args.kind} rehearsal evidence is not a successful operator result")
        if data.get("source_commit") != source or data.get("gateway_digest") != digest:
            raise SystemExit(f"{args.kind} rehearsal evidence is stale")
        if (data.get("kind") != args.kind or type(data.get("rc_run_id")) is not int
                or data["rc_run_id"] != int(manifest["workflow_run"])
                or type(data.get("rc_run_attempt")) is not int
                or data["rc_run_attempt"] != int(manifest["workflow_run_attempt"])
                or data.get("manifest_sha256") != hashlib.sha256(args.manifest.read_bytes()).hexdigest()):
            raise SystemExit(f"{args.kind} rehearsal evidence RC binding mismatch")
        transition = manifest.get("schema_transition", {})
        for field in ("source_schema_sha256", "target_schema_sha256"):
            expected = transition.get(field, "")
            if not HEX.fullmatch(expected) or data.get(field) != expected:
                raise SystemExit(f"{args.kind} rehearsal evidence schema binding mismatch")
        target = require(os.environ.get("CLEARAPI_REHEARSAL_TARGET_ID"), "rehearsal target is not configured")
        if data.get("target_id") != target:
            raise SystemExit(f"{args.kind} rehearsal evidence target binding mismatch")
        # Candidate startup/idempotency proves an upgrade, not recovery of the
        # previous release after failure. Never turn that narrower result into
        # a safe_rollback receipt, even when its identity and conclusion match.
        if args.kind == "rollback" and data.get("rollback_proven") is not True:
            raise SystemExit("rollback rehearsal evidence does not prove rollback recovery")
    print(f"{args.kind} rehearsal identity PASS for {source} {digest}")


if __name__ == "__main__":
    main()
