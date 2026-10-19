#!/usr/bin/env python3
"""Use the existing pinned SSH identity for bounded authorization inspection.

No installation, login, secret reads, Docker commands or production replacement.
The only root executable, apart from hash probes, is an exact-source V2 --verify.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[2]
ASSETS = {
    "scripts/production-deploy-v2.sh": "/opt/clearapi/bin/production-deploy-v2.sh",
    "scripts/pg03-release-control.py": "/opt/clearapi/bin/pg03-release-control.py",
    "scripts/runtime-identity-refresh.py": "/opt/clearapi/bin/runtime-identity-refresh.py",
    "scripts/runtime-probe-session.py": "/opt/clearapi/bin/runtime-probe-session.py",
    "scripts/production-backup.sh": "/opt/clearapi/backups/bin/clearapi-backup.sh",
    "scripts/pg03-backup-state.py": "/opt/clearapi/backups/bin/pg03-backup-state.py",
}


def ssh(command):
    alias = os.environ.get("PROD_SSH_ALIAS", "clearapi-vps")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]*", alias):
        raise ValueError("invalid configured SSH alias")
    result = subprocess.run(["ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
                             "-o", "ConnectTimeout=15", "-T", alias, command],
                            capture_output=True, text=True, timeout=180)
    return {"exit_code": result.returncode, "stdout": result.stdout[-16000:],
            "stderr": result.stderr[-4000:]}


def inspect():
    report = {"schema": "clearapi.vps-authorization-inspection/v1",
              "workflow_run": os.environ.get("GITHUB_RUN_ID"),
              "workflow_attempt": os.environ.get("GITHUB_RUN_ATTEMPT"),
              "workflow_source": os.environ.get("GITHUB_SHA"),
              "observed_at": datetime.now(timezone.utc).isoformat(),
              "mode": "inspection_only", "production_deployed": False,
              "installation_attempted": False, "session_creation_attempted": False,
              "runtime_gate_passed": False, "status": "BLOCKED", "assets": []}
    identity = ssh("/usr/bin/id -un")
    report["identity"] = identity
    if identity["exit_code"] != 0 or not re.fullmatch(r"[a-z_][a-z0-9_-]*\n?", identity["stdout"]):
        report["reason"] = "SSH identity unavailable"
        return report
    # -n never requests a password. Listing permission is not using that permission.
    report["sudo_authorization"] = ssh("/usr/bin/sudo -n -l")
    for source, target in ASSETS.items():
        expected = hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
        remote = ssh("/usr/bin/sudo -n /usr/bin/sha256sum " + target)
        fields = remote["stdout"].strip().split()
        actual = fields[0] if (len(fields) == 2 and fields[1] == target
                              and re.fullmatch(r"[a-f0-9]{64}", fields[0])) else None
        matched = remote["exit_code"] == 0 and actual == expected
        report["assets"].append({"target": target, "expected_sha256": expected,
                                  "actual_sha256": actual, "matched": matched,
                                  "exit_code": remote["exit_code"]})
    if not all(asset["matched"] for asset in report["assets"]):
        report["reason"] = "Installed assets mismatch or hash permission unavailable; Runtime not invoked"
        return report
    # The audited --verify path performs identity/Runtime checks and writes its
    # existing audit event. It never installs, replaces, restarts or deploys.
    runtime = ssh("/usr/bin/sudo -n /opt/clearapi/bin/production-deploy-v2.sh --verify")
    report["runtime_verification"] = runtime
    report["runtime_gate_passed"] = runtime["exit_code"] == 0
    report["status"] = "VERIFIED" if report["runtime_gate_passed"] else "BLOCKED"
    report["reason"] = "Actual wrapper --verify completed" if report["runtime_gate_passed"] else "Runtime verification blocked"
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        report = inspect()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        report = {"schema": "clearapi.vps-authorization-inspection/v1", "status": "BLOCKED",
                  "reason": type(error).__name__, "production_deployed": False,
                  "installation_attempted": False, "session_creation_attempted": False}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return 0 if report["status"] == "VERIFIED" else 1


if __name__ == "__main__":
    raise SystemExit(main())
