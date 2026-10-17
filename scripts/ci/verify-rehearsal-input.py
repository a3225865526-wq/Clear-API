#!/usr/bin/env python3
"""Validate operator-supplied evidence without exposing secret values."""

import argparse
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
        if data.get("schema") != "clearapi.rehearsal-evidence/v1" or data.get("conclusion") != "success":
            raise SystemExit(f"{args.kind} rehearsal evidence is not a successful signed result")
        if data.get("source_commit") != source or data.get("gateway_digest") != digest:
            raise SystemExit(f"{args.kind} rehearsal evidence is stale")
    print(f"{args.kind} rehearsal identity PASS for {source} {digest}")


if __name__ == "__main__":
    main()
