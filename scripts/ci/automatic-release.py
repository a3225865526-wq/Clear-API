#!/usr/bin/env python3
"""Fail-closed release policy. Only GitHub-verified main artifacts can authorize CD."""

import argparse
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
    require(run["event"] == event, "unexpected run event")
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
    require(proof["migration"]["mode"] == "no_schema_change", "migration has no automated safety proof")
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
    return {"image_digest": manifest["image_digest"], "expected_commit": source,
            # Legacy Deploy V2 does not classify rollback-safe failures versus
            # runtime/billing identity failures. Automatic callers must stop on
            # every failure until that distinction is independently verified.
            "previous_source_commit": proof["previous_source_commit"], "migration_compatible": "false"}


def authorize_release(event, destination):
    require(os.environ.get("GITHUB_RUN_ATTEMPT") == "1", "automatic deployment reruns require incident recovery")
    rehearsal = api(f"actions/runs/{event['workflow_run']['id']}")
    source = rehearsal["head_sha"]
    verified_run(rehearsal, source, "deployment-rehearsal.yml", "workflow_run")
    require(rehearsal["run_attempt"] == event["workflow_run"]["run_attempt"], "stale rehearsal attempt")
    require(api("git/ref/heads/main")["object"]["sha"] == source, "rehearsal source is no longer main")
    successful_jobs(rehearsal)
    latest_main_ci(source)
    # A failed or cancelled automatic deployment is a durable circuit breaker.
    previous = [run for page in pages("actions/workflows/automatic-deploy.yml/runs?branch=main&per_page=100")
                for run in page["workflow_runs"]]
    previous = [r for r in previous if str(r["id"]) != os.environ["GITHUB_RUN_ID"]
                and r["conclusion"] != "skipped"]
    if previous:
        require(previous[0]["conclusion"] == "success", "release circuit open; investigate prior deployment before resuming")
    destination.mkdir(parents=True, exist_ok=False)
    gh("run", "download", str(rehearsal["id"]), "--repo", REPOSITORY, "--name",
       f"deployment-proof-{rehearsal['id']}-{rehearsal['run_attempt']}", "--dir", str(destination / "proof"))
    proof = json.loads((destination / "proof/release-proof.json").read_text())
    require(type(proof["rc_run_id"]) is int and proof["rc_run_id"] > 0, "invalid proof RC run ID")
    rc = api(f"actions/runs/{proof['rc_run_id']}")
    verified_run(rc, source, "release-pg03-gateway.yml", "workflow_run")
    require(proof["rc_run_attempt"] == rc["run_attempt"], "stale RC attempt")
    latest_rc = api(f"actions/workflows/release-pg03-gateway.yml/runs?branch=main&head_sha={source}&per_page=1")["workflow_runs"]
    require(bool(latest_rc) and latest_rc[0]["id"] == rc["id"], "superseded RC run")
    latest_rehearsal = api(f"actions/workflows/deployment-rehearsal.yml/runs?branch=main&head_sha={source}&per_page=1")["workflow_runs"]
    require(bool(latest_rehearsal) and latest_rehearsal[0]["id"] == rehearsal["id"], "superseded rehearsal run")
    successful_jobs(rc)
    gh("run", "download", str(rc["id"]), "--repo", REPOSITORY, "--name",
       f"pg03-gateway-release-{rc['id']}-{rc['run_attempt']}", "--dir", str(destination / "rc"))
    manifest = json.loads((destination / "rc/gateway-image-release.json").read_text())
    hashes = json.loads((destination / "rc/artifact-hashes.json").read_text())
    for filename in ("gateway-image-release.json", "binary-build-info.txt"):
        require(hashlib.sha256((destination / "rc" / filename).read_bytes()).hexdigest() == hashes[filename], "RC artifact hash mismatch")
    require(str(manifest["workflow_run"]) == str(rc["id"]), "manifest producer mismatch")
    require(str(manifest["workflow_run_attempt"]) == str(rc["run_attempt"]), "manifest attempt mismatch")
    result = validate_proof(manifest, proof, destination / "proof", source, rc["id"],
                            os.environ.get("CLEARAPI_EXECUTOR_IMAGE_REPOSITORY", ""))
    comparison = api(f"compare/{proof['previous_source_commit']}...{source}")
    require(comparison["status"] in ("ahead", "identical"), "invalid deployment ancestry")
    # GitHub caps compare files at 300. Never treat a truncated diff as safe.
    files = comparison.get("files", [])
    require(len(files) < 300, "deployment diff may be truncated")
    paths = [f["filename"] for f in files] + [f["previous_filename"] for f in files if "previous_filename" in f]
    require(not low_risk_paths(paths), "release contains sensitive changes without stronger policy")
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
