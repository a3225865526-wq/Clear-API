#!/usr/bin/env python3
"""Fail-closed release policy. Only GitHub-verified main artifacts can authorize CD."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

REPOSITORY = "a3225865526-wq/Clear-API"
IMAGE = "ghcr.io/a3225865526-wq/clearapi"
SHA = r"[0-9a-f]{40}"
DIGEST = r"sha256:[0-9a-f]{64}"
REQUIRED_CHECKS = {
    "Backend vet, build, test, migration binaries",
    "Frontend typecheck and test",
    "Secret scan and forbidden files",
}
PROOF_CHECKS = {
    "runtime_integrity", "billing", "backup_restore", "executor_pairing",
    "sqlite_upgrade_idempotency", "mysql_upgrade_idempotency",
    "postgres_upgrade_idempotency", "gateway_executor_provider_usage_billing",
    "replay_and_duplicate_charge", "state_isolation", "safe_rollback",
}
RC_ARTIFACT_FILES = {
    "gateway-image-release.json", "binary-build-info.txt",
    "schema-rehearsal.json", "schema-rehearsal.target.sql",
}
RECOVERY_RECORD = Path(__file__).resolve().parents[2] / ".github/release-circuit-recovery.json"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def gh(*args):
    result = subprocess.run(["gh", *args], capture_output=True, text=True, encoding="utf-8")
    require(result.returncode == 0, "GitHub request failed: " + " ".join(args[:2]))
    return result.stdout


def api(path):
    return json.loads(gh("api", f"repos/{REPOSITORY}/{path}"))


def pages(path):
    return json.loads(gh("api", "--paginate", "--slurp", f"repos/{REPOSITORY}/{path}"))


def verified_run(run, source, workflow, event):
    require(re.fullmatch(SHA, source) is not None, "invalid full source SHA")
    require(run["repository"]["full_name"] == REPOSITORY, "foreign run repository")
    require(run["head_repository"]["full_name"] == REPOSITORY, "foreign head repository")
    require(run["path"] == f".github/workflows/{workflow}", "unexpected workflow path")
    require(run["head_sha"] == source and run["head_branch"] == "main", "run source mismatch")
    allowed = (event,) if isinstance(event, str) else tuple(event)
    require(run["event"] in allowed, "unexpected run event")
    require(run["status"] == "completed" and run["conclusion"] == "success", "run not successful")


def successful_jobs(run):
    jobs = [job for page in pages(
        f"actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs?per_page=100"
    ) for job in page["jobs"]]
    require(bool(jobs), "missing run jobs")
    require(all(j["conclusion"] == "success" for j in jobs), "failed or skipped required job")
    return jobs


def latest_main_ci(source):
    runs = api(f"actions/workflows/ci.yml/runs?branch=main&event=push&head_sha={source}&per_page=1")["workflow_runs"]
    require(bool(runs), "no push CI for source")
    run = api(f"actions/runs/{runs[0]['id']}")
    verified_run(run, source, "ci.yml", "push")
    jobs = successful_jobs(run)
    names = {j["name"] for j in jobs}
    require(REQUIRED_CHECKS <= names, "missing required CI checks")
    require(any("DB matrix" in n for n in names), "missing release DB qualification")
    return run


def low_risk_paths(paths):
    """Unknown and sensitive paths require a separately reviewed safety policy."""
    blocked = []
    for path in paths:
        p = path.lower()
        sensitive = any(word in p for word in (
            "agents.md", "billing", "finance", "quota", "migration", "auth",
            "permission", "bootstrap", "runtime", "deploy", "release", "pricing",
            "account", "token", "session", "payment", "store", "referral",
            "login", "register", "password", "webauthn", "totp", "oauth",
            "credential", "wallet", "credit", "redeem", "balance", "usage",
            "reservation", "route", "pool", "executor", "legal", "privacy", "agreement",
        ))
        allowed = (p.startswith(("web/src/components/", "web/src/features/documentation/", "web/src/i18n/locales/"))
                   and p.endswith((".ts", ".tsx", ".css", ".json")))
        allowed |= p.startswith("docs/") and p.endswith(".md")
        if sensitive or not allowed:
            blocked.append(path)
    return blocked


def authorize_pr(event):
    run = event["workflow_run"]
    require(run["event"] == "pull_request" and run["conclusion"] == "success", "not a passing PR CI")
    require(run["repository"]["full_name"] == REPOSITORY, "foreign CI repository")
    require(run["head_repository"]["full_name"] == REPOSITORY, "fork CI")
    require(run["path"] == ".github/workflows/ci.yml", "wrong PR workflow")
    matches = api(f"commits/{run['head_sha']}/pulls?per_page=100")
    matches = [pr for pr in matches if pr["state"] == "open" and pr["base"]["ref"] == "main"]
    require(len(matches) == 1, "expected one current PR")
    pr = api(f"pulls/{matches[0]['number']}")
    require(pr["head"]["repo"]["full_name"] == REPOSITORY, "fork PR")
    require(pr["user"]["login"] == "a3225865526-wq", "untrusted PR author")
    require(pr["head"]["ref"].startswith("codex/"), "untrusted branch namespace")
    require(not pr["draft"] and pr["head"]["sha"] == run["head_sha"], "draft or stale head")
    require(pr["mergeable"] is True and pr["mergeable_state"] == "clean", "conflict, stale base or unmet protection")
    main_sha = api("git/ref/heads/main")["object"]["sha"]
    ancestry = api(f"compare/{main_sha}...{pr['head']['sha']}")
    require(ancestry["merge_base_commit"]["sha"] == main_sha, "head must include current main")
    files = [f for page in pages(f"pulls/{pr['number']}/files?per_page=100") for f in page]
    require(len(files) == pr["changed_files"] and bool(files), "incomplete PR file list")
    paths = [f["filename"] for f in files] + [f["previous_filename"] for f in files if "previous_filename" in f]
    blocked = low_risk_paths(paths)
    require(not blocked, "sensitive/unknown change requires stronger policy: " + ", ".join(blocked[:10]))
    protection = api("branches/main/protection")
    require(protection["enforce_admins"]["enabled"], "admin protection must remain enabled")
    require(protection["required_status_checks"]["strict"], "strict required checks missing")
    require(not protection["allow_force_pushes"]["enabled"], "force push allowed")
    require(not protection["allow_deletions"]["enabled"], "branch deletion allowed")
    expected = {c["context"] for c in protection["required_status_checks"]["checks"]}
    require(REQUIRED_CHECKS <= expected, "required checks weakened")
    current = api(f"actions/runs/{run['id']}")
    require(current["run_attempt"] == run["run_attempt"] and current["conclusion"] == "success", "stale CI attempt")
    jobs = successful_jobs(current)
    require(REQUIRED_CHECKS <= {j["name"] for j in jobs}, "missing PR checks")
    return {"number": pr["number"], "head": pr["head"]["sha"]}


def validate_proof(manifest, proof, evidence_dir, source, rc_run, executor_image):
    """The caller must authenticate the artifact's producing workflow through GitHub."""
    require(proof["schema"] == "clearapi.automatic-release/v1", "unsupported proof contract")
    require("approval" not in proof, "human approval cannot be replaced by a PASS constant")
    require(proof["source_commit"] == source == manifest["source_commit"], "proof/source mismatch")
    require(re.fullmatch(SHA, source) is not None, "invalid source")
    require(proof["rc_run_id"] == rc_run, "proof RC run mismatch")
    require(manifest["image_repository"] == IMAGE, "foreign image")
    require(re.fullmatch(DIGEST, manifest["image_digest"]) is not None, "mutable or malformed digest")
    require(manifest["image_ref"] == IMAGE + "@" + manifest["image_digest"], "image reference mismatch")
    require(manifest["oci_revision"] == source and manifest["platform"] == "linux/amd64", "OCI identity mismatch")
    require(proof["gateway_digest"] == manifest["image_digest"], "proof image mismatch")
    require(proof["gateway_binary_sha256"] == manifest["binary_sha256"], "gateway binary mismatch")
    require(re.fullmatch(r"[0-9a-f]{64}", manifest["binary_sha256"]) is not None, "invalid binary hash")
    executor = proof["executor"]
    require(re.fullmatch(r"ghcr.io/a3225865526-wq/[a-z0-9._/-]+", executor_image) is not None,
            "approved immutable Executor image repository must be configured")
    require(executor["image_repository"] == executor_image, "unapproved Executor image repository")
    require(executor["source_repository"] == "https://github.com/a3225865526-wq/CLIProxyAPI",
            "unapproved Executor source repository")
    require(re.fullmatch(SHA, executor["source_commit"]) is not None, "invalid executor source")
    require(re.fullmatch(DIGEST, executor["image_digest"]) is not None, "invalid executor digest")
    require(re.fullmatch(r"[0-9a-f]{64}", executor["binary_sha256"]) is not None, "invalid executor binary")
    require(executor["paired_gateway_commit"] == source, "executor not paired with gateway")
    # The proof must state the migration mode that the RC actually rehearsed.
    # PG03 forbids external migration flags, so an upgrading candidate proves
    # itself only through two idempotent startup passes on the same database.
    transition = manifest.get("schema_transition", {})
    source_schema = transition.get("source_schema_sha256")
    target_schema = transition.get("target_schema_sha256")
    require(type(source_schema) is str and re.fullmatch(r"[0-9a-f]{64}", source_schema) is not None
            and type(target_schema) is str and re.fullmatch(r"[0-9a-f]{64}", target_schema) is not None,
            "RC schema transition is missing or malformed")
    migration = proof["migration"]
    if source_schema == target_schema:
        require(migration.get("mode") == "no_schema_change",
                "no_schema_change contradicts the RC schema transition")
    else:
        require(migration.get("mode") == "in_place_startup_migration",
                "RC schema transition changed but the proof claims no migration")
        require(migration.get("source_schema_sha256") == source_schema
                and migration.get("target_schema_sha256") == target_schema,
                "migration evidence contradicts the RC schema transition")
        require(transition.get("schema_version") == "pg03-schema-rehearsal-v1"
                and transition.get("startup_passes") == 2,
                "in-place migration requires two idempotent startup passes")
        require(transition.get("source_commit") == source,
                "in-place migration rehearsal does not bind this candidate")
    require(re.fullmatch(SHA, proof["previous_source_commit"]) is not None, "missing predecessor identity")
    require(set(proof["checks"]) == PROOF_CHECKS, "missing/unknown release evidence checks")
    for name, check in proof["checks"].items():
        require(check["conclusion"] == "success", "gate failed: " + name)
        filename = check["file"]
        require(filename == name + ".json", "invalid evidence filename")
        path = evidence_dir / filename
        require(not path.is_symlink() and path.is_file(), "missing evidence: " + name)
        data = path.read_bytes()
        require(hashlib.sha256(data).hexdigest() == check["sha256"], "evidence hash mismatch: " + name)
        receipt = json.loads(data)
        require(receipt["schema"] == "clearapi.gate-evidence/v1" and receipt["gate"] == name,
                "wrong gate evidence contract: " + name)
        require(receipt["conclusion"] == "success", "raw evidence did not pass: " + name)
        require(receipt["source_commit"] == source and receipt["gateway_digest"] == manifest["image_digest"], "stale evidence: " + name)
        require(type(receipt.get("rc_run_id")) is int and receipt["rc_run_id"] == rc_run,
                "stale evidence RC run: " + name)
        require(type(receipt.get("rc_run_attempt")) is int
                and receipt["rc_run_attempt"] == proof["rc_run_attempt"],
                "stale evidence RC attempt: " + name)
    return {"image_digest": manifest["image_digest"], "expected_commit": source,
            "rc_run_id": rc_run,
            "executor_image_repository": executor["image_repository"],
            "executor_image_digest": executor["image_digest"],
            "executor_binary_sha256": executor["binary_sha256"],
            "executor_source_commit": executor["source_commit"],
            # Legacy Deploy V2 does not classify rollback-safe failures versus
            # runtime/billing identity failures. Automatic callers must stop on
            # every failure until that distinction is independently verified.
            "previous_source_commit": proof["previous_source_commit"], "migration_compatible": "false"}


def verify_rc_artifact(directory):
    manifest = json.loads((directory / "gateway-image-release.json").read_text())
    hashes = json.loads((directory / "artifact-hashes.json").read_text())
    require(set(hashes) == RC_ARTIFACT_FILES, "RC artifact inventory mismatch")
    for filename, expected_hash in hashes.items():
        require(re.fullmatch(r"[0-9a-f]{64}", expected_hash) is not None, "invalid RC artifact hash")
        path = directory / filename
        require(path.is_file() and not path.is_symlink(), "missing or unsafe RC artifact file")
        require(hashlib.sha256(path.read_bytes()).hexdigest() == expected_hash, "RC artifact hash mismatch")
    rehearsal = json.loads((directory / "schema-rehearsal.json").read_text())
    require(rehearsal == manifest["schema_transition"], "schema rehearsal and manifest mismatch")
    return manifest


def verify_circuit_recovery(previous, source):
    """A merged investigation may recover only the exact authorization-only failures.

    GitHub history remains the circuit's state. This never rewrites a failed run,
    permits a retry, or recovers a failure that reached production credentials.
    """
    require(RECOVERY_RECORD.is_file() and not RECOVERY_RECORD.is_symlink(),
            "release circuit open; missing reviewed recovery investigation")
    raw = RECOVERY_RECORD.read_bytes()
    record = json.loads(raw)
    require(record["schema"] == "clearapi.authorization-circuit-recovery/v1",
            "invalid circuit recovery contract")
    issued = datetime.fromisoformat(record["investigated_at"].replace("Z", "+00:00"))
    expires = datetime.fromisoformat(record["expires_at"].replace("Z", "+00:00"))
    require(issued.tzinfo is not None and expires.tzinfo is not None
            and issued <= datetime.now(timezone.utc) < expires
            and 0 < (expires - issued).total_seconds() <= 86400,
            "release circuit recovery expired or invalid")
    require(re.fullmatch(SHA, record["baseline_commit"]) is not None,
            "invalid recovery baseline")
    comparison = api(f"compare/{record['baseline_commit']}...{source}")
    require(comparison["status"] == "ahead"
            and comparison["merge_base_commit"]["sha"] == record["baseline_commit"],
            "recovery is not descended from investigated main")
    failures = []
    for run in previous:
        if run["conclusion"] == "success":
            break
        require(run.get("status") == "completed" and run["conclusion"] == "failure",
                "release circuit open; unfinished or unclassified production run")
        failures.append(run)
    entries = record["failures"]
    require(bool(entries) and len(entries) == len(failures)
            and len({e["run_id"] for e in entries}) == len(entries)
            and {e["run_id"] for e in entries} == {r["id"] for r in failures},
            "release circuit open; investigation does not cover exact failure history")
    evidence = []
    for entry in entries:
        run = api(f"actions/runs/{entry['run_id']}")
        require(run["path"] == ".github/workflows/automatic-deploy.yml"
                and run["event"] == "workflow_run" and run["head_branch"] == "main"
                and run["repository"]["full_name"] == REPOSITORY
                and run["head_repository"]["full_name"] == REPOSITORY
                and run["head_sha"] == entry["source_commit"]
                and run["created_at"] == entry["created_at"]
                and run["run_attempt"] == entry["run_attempt"] == 1
                and run["status"] == "completed" and run["conclusion"] == "failure",
                "investigated run identity changed")
        jobs = [j for page in pages(
            f"actions/runs/{run['id']}/attempts/1/jobs?per_page=100") for j in page["jobs"]]
        require(len(jobs) == 2 and {j["id"] for j in jobs}
                == {entry["authorization_job_id"], entry["deployment_job_id"]},
                "investigated job inventory changed")
        auth = next(j for j in jobs if j["id"] == entry["authorization_job_id"])
        deploy = next(j for j in jobs if j["id"] == entry["deployment_job_id"])
        require(auth["name"] == "deploy / Validate release authorization before production credentials"
                and auth["status"] == "completed" and auth["conclusion"] == "failure"
                and deploy["name"] == "deploy / Controlled production deploy (exact digest)"
                and deploy["status"] == "completed" and deploy["conclusion"] == "skipped"
                and not deploy.get("steps"), "investigated failure reached production")
        failed = [s for s in auth["steps"] if s["conclusion"] == "failure"]
        require(len(failed) == 1 and failed[0]["name"]
                == "Authenticate automatic proof before production credentials",
                "investigated failure is not authorization-only")
        log = gh("api", f"repos/{REPOSITORY}/actions/jobs/{auth['id']}/logs")
        log_hash = hashlib.sha256("\n".join(log.lstrip("\ufeff").splitlines()).encode()).hexdigest()
        require(re.fullmatch(r"[a-f0-9]{64}", entry["authorization_log_sha256"]) is not None
                and log_hash == entry["authorization_log_sha256"]
                and entry["reason"] in (
                    "unexpected run event",
                    "release circuit open; investigate prior deployment before resuming")
                and any(line.endswith("Z RELEASE BLOCKED: " + entry["reason"])
                        for line in log.splitlines()), "investigated authorization log changed")
        evidence.append({"run_id": run["id"], "authorization_log_sha256": log_hash,
                         "production_job": "skipped"})
    cutoff = min(e["created_at"] for e in entries)
    # Fail closed if another formal path operated on production during the incident.
    manual = [r for page in pages("actions/workflows/deploy-production.yml/runs?per_page=100")
              for r in page["workflow_runs"]]
    require(not any(r["status"] != "completed" or r["created_at"] >= cutoff for r in manual),
            "release circuit open; concurrent or intervening manual deployment")
    deployments = [d for page in pages("deployments?environment=production&per_page=100") for d in page]
    require(not any(d["created_at"] >= cutoff for d in deployments),
            "release circuit open; production deployment record requires investigation")
    return {"schema": record["schema"], "investigation_sha256": hashlib.sha256(raw).hexdigest(),
            "source_commit": source, "deployment_run_id": os.environ["GITHUB_RUN_ID"],
            "verified_at": datetime.now(timezone.utc).isoformat(), "failures": evidence,
            "production_deployments_since_incident": 0, "intervening_manual_runs": 0}


def authorize_release(event, destination):
    require(os.environ.get("GITHUB_RUN_ATTEMPT") == "1", "automatic deployment reruns require incident recovery")
    rehearsal = api(f"actions/runs/{event['workflow_run']['id']}")
    source = rehearsal["head_sha"]
    # A dispatched rehearsal is legitimate: operator evidence is bound to an RC
    # manifest that only exists after the RC finishes, which the automatic
    # trigger cannot win. Both entries run the same gates and assembler.
    verified_run(rehearsal, source, "deployment-rehearsal.yml",
                 ("workflow_run", "workflow_dispatch"))
    require(rehearsal["run_attempt"] == event["workflow_run"]["run_attempt"], "stale rehearsal attempt")
    require(api("git/ref/heads/main")["object"]["sha"] == source, "rehearsal source is no longer main")
    successful_jobs(rehearsal)
    source_ci = latest_main_ci(source)
    # A failed or cancelled automatic deployment is a durable circuit breaker.
    previous = [run for page in pages("actions/workflows/automatic-deploy.yml/runs?branch=main&per_page=100")
                for run in page["workflow_runs"]]
    previous = [r for r in previous if str(r["id"]) != os.environ["GITHUB_RUN_ID"]
                and r["conclusion"] != "skipped"]
    recovery = None
    if previous and previous[0]["conclusion"] != "success":
        recovery = verify_circuit_recovery(previous, source)
    destination.mkdir(parents=True, exist_ok=False)
    gh("run", "download", str(rehearsal["id"]), "--repo", REPOSITORY, "--name",
       f"deployment-proof-{rehearsal['id']}-{rehearsal['run_attempt']}", "--dir", str(destination / "proof"))
    proof = json.loads((destination / "proof/release-proof.json").read_text())
    require(type(proof["rc_run_id"]) is int and proof["rc_run_id"] > 0, "invalid proof RC run ID")
    rc = api(f"actions/runs/{proof['rc_run_id']}")
    verified_run(rc, source, "release-pg03-gateway.yml", ("workflow_run", "workflow_dispatch"))
    require(proof["rc_run_attempt"] == rc["run_attempt"], "stale RC attempt")
    latest_rc = api(f"actions/workflows/release-pg03-gateway.yml/runs?branch=main&head_sha={source}&per_page=1")["workflow_runs"]
    require(bool(latest_rc) and latest_rc[0]["id"] == rc["id"], "superseded RC run")
    latest_rehearsal = api(f"actions/workflows/deployment-rehearsal.yml/runs?branch=main&head_sha={source}&per_page=1")["workflow_runs"]
    require(bool(latest_rehearsal) and latest_rehearsal[0]["id"] == rehearsal["id"], "superseded rehearsal run")
    successful_jobs(rc)
    gh("run", "download", str(rc["id"]), "--repo", REPOSITORY, "--name",
       f"pg03-gateway-release-{rc['id']}-{rc['run_attempt']}", "--dir", str(destination / "rc"))
    manifest = verify_rc_artifact(destination / "rc")
    require(str(manifest["workflow_run"]) == str(rc["id"]), "manifest producer mismatch")
    require(str(manifest["workflow_run_attempt"]) == str(rc["run_attempt"]), "manifest attempt mismatch")
    require(any(run["databaseId"] == source_ci["id"] and run["headSha"] == source
                and run["status"] == "completed" and run["conclusion"] == "success"
                for run in manifest.get("source_ci_runs", [])), "manifest source CI mismatch")
    result = validate_proof(manifest, proof, destination / "proof", source, rc["id"],
                            os.environ.get("CLEARAPI_EXECUTOR_IMAGE_REPOSITORY", ""))
    comparison = api(f"compare/{proof['previous_source_commit']}...{source}")
    require(comparison["status"] == "ahead", "deployment source must advance the production predecessor")
    # GitHub caps compare files at 300. Never treat a truncated diff as safe.
    files = comparison.get("files", [])
    require(len(files) < 300, "deployment diff may be truncated")
    paths = [f["filename"] for f in files] + [f["previous_filename"] for f in files if "previous_filename" in f]
    require(bool(paths), "deployment diff is empty")
    result["rehearsal_run_id"] = str(rehearsal["id"])
    if recovery is not None:
        # Only a fully authenticated current proof may emit the recovery receipt.
        recovery.update(rc_run_id=rc["id"], rehearsal_run_id=rehearsal["id"],
                        gateway_digest=result["image_digest"])
        (destination / "recovery-audit.json").write_text(json.dumps(recovery, indent=2) + "\n")
        print("Verified authorization-only circuit recovery: " + json.dumps(recovery, sort_keys=True))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("pr", "source", "authorize"))
    parser.add_argument("--event", type=Path, default=os.environ.get("GITHUB_EVENT_PATH"))
    parser.add_argument("--destination", type=Path)
    args = parser.parse_args()
    require(os.environ.get("GITHUB_REPOSITORY") == REPOSITORY, "foreign execution repository")
    event = json.loads(Path(args.event).read_text())
    if args.mode == "pr":
        result = authorize_pr(event)
    elif args.mode == "source":
        run = event["workflow_run"]
        require(api("git/ref/heads/main")["object"]["sha"] == run["head_sha"], "stale source")
        current = latest_main_ci(run["head_sha"])
        require(current["id"] == run["id"] and current["run_attempt"] == run["run_attempt"], "stale triggering CI")
        result = {"source_commit": run["head_sha"], "ci_run_id": current["id"]}
    else:
        require(args.destination is not None, "evidence destination required")
        result = authorize_release(event, args.destination)
    output = json.dumps(result, indent=2)
    print(output)
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
            for key, value in result.items():
                stream.write(f"{key}={value}\n")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError) as error:
        raise SystemExit("RELEASE BLOCKED: " + str(error)) from None
