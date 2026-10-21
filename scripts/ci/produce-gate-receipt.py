#!/usr/bin/env python3
"""Run one rehearsal gate and persist an identity-bound receipt."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from datetime import datetime, timezone

from release_proof import OFF_GATES, PG03_GATES, required_gates


GATES = PG03_GATES | OFF_GATES


def required(name):
    value = os.environ.get(name, "")
    if not value:
        raise RuntimeError(f"missing required rehearsal identity: {name}")
    return value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--pool-mode', choices=('off', 'pg03'), default='pg03')
    parser.add_argument("--gate", required=True, choices=sorted(GATES))
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.gate not in required_gates(args.pool_mode):
        parser.error('gate does not belong to the selected pool mode')
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        raise SystemExit("a gate command is required")

    started = datetime.now(timezone.utc).isoformat()
    try:
        source = required("REHEARSAL_SOURCE_COMMIT")
        digest = required("REHEARSAL_GATEWAY_DIGEST")
        rc_run = int(required("REHEARSAL_RC_RUN_ID"))
        rc_attempt = int(required("REHEARSAL_RC_RUN_ATTEMPT"))
        result = subprocess.run(command, capture_output=True, text=True, timeout=900)
        stdout = result.stdout[-4000:]
        stderr = result.stderr[-4000:]
        conclusion = "success" if result.returncode == 0 else "failure"
        error = None if result.returncode == 0 else f"command exited {result.returncode}"
    except Exception as exc:  # receipt must exist even when setup fails
        stdout, stderr = "", ""
        conclusion, error = "failure", str(exc)
        source = os.environ.get("REHEARSAL_SOURCE_COMMIT", "")
        digest = os.environ.get("REHEARSAL_GATEWAY_DIGEST", "")
        try:
            rc_run = int(os.environ.get("REHEARSAL_RC_RUN_ID", "0"))
        except ValueError:
            rc_run = 0
        try:
            rc_attempt = int(os.environ.get("REHEARSAL_RC_RUN_ATTEMPT", "0"))
        except ValueError:
            rc_attempt = 0

    receipt = {
        "schema": ("clearapi.gate-evidence/v2" if args.pool_mode == 'off'
                   else "clearapi.gate-evidence/v1"),
        "pool_mode": args.pool_mode,
        "gate": args.gate,
        "conclusion": conclusion,
        "source_commit": source,
        "gateway_digest": digest,
        "rc_run_id": rc_run,
        "rc_run_attempt": rc_attempt,
        "started_at": started,
        "finished_at": datetime.now(timezone.utc).isoformat(),
        "command": command,
        "stdout_tail": stdout,
        "stderr_tail": stderr,
    }
    if error:
        receipt["error"] = error
    args.output.parent.mkdir(parents=True, exist_ok=True)
    raw = json.dumps(receipt, sort_keys=True, indent=2).encode() + b"\n"
    args.output.write_bytes(raw)
    print(json.dumps({"gate": args.gate, "conclusion": conclusion, "sha256": hashlib.sha256(raw).hexdigest()}))
    return 0 if conclusion == "success" else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(130)
