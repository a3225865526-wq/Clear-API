"""Offline policy regression tests. Synthetic receipts are never production evidence."""

from copy import deepcopy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("automatic_release", Path(__file__).with_name("automatic-release.py"))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)
SOURCE = "a" * 40
DIGEST = "sha256:" + "b" * 64
EXECUTOR_IMAGE = "ghcr.io/a3225865526-wq/executor-unit-fixture"


class ReleaseProofTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="clearapi-auto-cicd-")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.manifest = {
            "source_commit": SOURCE, "image_repository": policy.IMAGE,
            "image_digest": DIGEST, "image_ref": policy.IMAGE + "@" + DIGEST,
            "oci_revision": SOURCE, "platform": "linux/amd64", "binary_sha256": "c" * 64,
            "schema_transition": {"schema_version": "pg03-schema-rehearsal-v1",
                                  "source_commit": SOURCE, "startup_passes": 2},
            "source_ci_runs": [{"databaseId": 96, "headSha": SOURCE,
                                 "status": "completed", "conclusion": "success"}],
        }
        self.proof = {
            "schema": "clearapi.automatic-release/v1", "source_commit": SOURCE,
            "rc_run_id": 123, "rc_run_attempt": 1, "gateway_digest": DIGEST,
            "gateway_binary_sha256": "c" * 64, "previous_source_commit": "d" * 40,
            "migration": {"mode": "no_schema_change"},
            "executor": {"source_commit": "e" * 40, "image_digest": "sha256:" + "f" * 64,
                         "image_repository": EXECUTOR_IMAGE,
                         "source_repository": "https://github.com/a3225865526-wq/CLIProxyAPI",
                         "binary_sha256": "1" * 64, "paired_gateway_commit": SOURCE},
            "checks": {},
        }
        for name in policy.PROOF_CHECKS:
            raw = json.dumps({"schema": "clearapi.gate-evidence/v1", "gate": name,
                              "conclusion": "success", "source_commit": SOURCE, "gateway_digest": DIGEST,
                              "fixture": "unit-test-only"}).encode()
            (self.root / (name + ".json")).write_bytes(raw)
            self.proof["checks"][name] = {"conclusion": "success", "file": name + ".json",
                                          "sha256": hashlib.sha256(raw).hexdigest()}

    def validate(self, manifest=None, proof=None):
        return policy.validate_proof(manifest or self.manifest, proof or self.proof, self.root, SOURCE, 123, EXECUTOR_IMAGE)

    def test_matching_authenticated_contract_returns_exact_identity(self):
        result = self.validate()
        self.assertEqual(result["image_digest"], DIGEST)
        self.assertEqual(result["expected_commit"], SOURCE)
        self.assertEqual(result["rc_run_id"], 123)
        self.assertEqual(result["executor_image_digest"], self.proof["executor"]["image_digest"])
        self.assertEqual(result["migration_compatible"], "false", "unclassified runtime failures must stop")

    def test_rc_artifact_inventory_and_schema_receipt_are_bound_to_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = dict(self.manifest)
            (root / "gateway-image-release.json").write_text(json.dumps(manifest))
            (root / "binary-build-info.txt").write_text("build identity")
            (root / "schema-rehearsal.json").write_text(json.dumps(manifest["schema_transition"]))
            (root / "schema-rehearsal.target.sql").write_text("-- target schema")
            files = [root / name for name in policy.RC_ARTIFACT_FILES]
            inventory = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in files}
            (root / "artifact-hashes.json").write_text(json.dumps(inventory))
            self.assertEqual(policy.verify_rc_artifact(root), manifest)

            (root / "schema-rehearsal.target.sql").unlink()
            with self.assertRaisesRegex(ValueError, "missing or unsafe"):
                policy.verify_rc_artifact(root)
            (root / "schema-rehearsal.target.sql").write_text("-- target schema")

            rehearsal = dict(manifest["schema_transition"], source_schema_sha256="0" * 64)
            (root / "schema-rehearsal.json").write_text(json.dumps(rehearsal))
            inventory["schema-rehearsal.json"] = hashlib.sha256(
                (root / "schema-rehearsal.json").read_bytes()).hexdigest()
            (root / "artifact-hashes.json").write_text(json.dumps(inventory))
            with self.assertRaisesRegex(ValueError, "schema rehearsal and manifest mismatch"):
                policy.verify_rc_artifact(root)

    def test_identity_contract_and_migration_fail_closed(self):
        cases = [
            ("source_commit", "0" * 40), ("gateway_digest", "latest"),
            ("rc_run_id", 124), ("schema", "v0"),
            ("gateway_binary_sha256", "0" * 64),
            ("previous_source_commit", ""), ("approval", "passed"),
            ("migration", {"mode": "irreversible"}),
        ]
        for field, value in cases:
            with self.subTest(field=field):
                proof = deepcopy(self.proof)
                proof[field] = value
                with self.assertRaises(ValueError):
                    self.validate(proof=proof)

    def test_each_gate_is_mandatory_and_cannot_be_skipped(self):
        for name in policy.PROOF_CHECKS:
            for status in ("failure", "skipped", "not_run", "PASS", True):
                with self.subTest(gate=name, status=status):
                    proof = deepcopy(self.proof)
                    proof["checks"][name]["conclusion"] = status
                    with self.assertRaises(ValueError):
                        self.validate(proof=proof)
            proof = deepcopy(self.proof)
            del proof["checks"][name]
            with self.assertRaises(ValueError):
                self.validate(proof=proof)

    def test_mutable_or_foreign_image_is_rejected(self):
        for field, value in (("image_repository", "attacker/image"),
                             ("image_digest", "latest"), ("image_ref", "evil@" + DIGEST),
                             ("oci_revision", "0" * 40), ("platform", "linux/arm64")):
            with self.subTest(field=field):
                manifest = deepcopy(self.manifest)
                manifest[field] = value
                with self.assertRaises(ValueError):
                    self.validate(manifest=manifest)

    def test_executor_pairing_cannot_be_claimed_by_gateway_alone(self):
        self.proof["executor"]["paired_gateway_commit"] = "0" * 40
        with self.assertRaises(ValueError):
            self.validate()

    def test_tampered_and_stale_evidence_is_rejected(self):
        name = "billing"
        path = self.root / (name + ".json")
        path.write_text("{}")
        with self.assertRaisesRegex(ValueError, "hash mismatch"):
            self.validate()
        raw = json.dumps({"schema": "clearapi.gate-evidence/v1", "gate": name,
                          "conclusion": "success", "source_commit": "0" * 40, "gateway_digest": DIGEST}).encode()
        path.write_bytes(raw)
        self.proof["checks"][name]["sha256"] = hashlib.sha256(raw).hexdigest()
        with self.assertRaisesRegex(ValueError, "stale evidence"):
            self.validate()

    def test_missing_and_path_traversal_evidence_is_rejected(self):
        self.proof["checks"]["billing"]["file"] = "../billing.json"
        with self.assertRaisesRegex(ValueError, "filename"):
            self.validate()
        self.proof["checks"]["billing"]["file"] = "billing.json"
        (self.root / "billing.json").unlink()
        with self.assertRaisesRegex(ValueError, "missing evidence"):
            self.validate()

    def test_authenticated_workflow_chain_and_persistent_failure_circuit(self):
        run = {"repository": {"full_name": policy.REPOSITORY},
               "head_repository": {"full_name": policy.REPOSITORY},
               "head_sha": SOURCE, "head_branch": "main", "event": "workflow_run",
               "status": "completed", "conclusion": "success", "run_attempt": 1}
        rehearsal = dict(run, id=100, path=".github/workflows/deployment-rehearsal.yml")
        rc = dict(run, id=123, path=".github/workflows/release-pg03-gateway.yml")
        responses = {"actions/runs/100": rehearsal, "actions/runs/123": rc,
                     "git/ref/heads/main": {"object": {"sha": SOURCE}},
                     f"actions/workflows/release-pg03-gateway.yml/runs?branch=main&head_sha={SOURCE}&per_page=1": {"workflow_runs": [rc]},
                     f"actions/workflows/deployment-rehearsal.yml/runs?branch=main&head_sha={SOURCE}&per_page=1": {"workflow_runs": [rehearsal]},
                     f"compare/{'d' * 40}...{SOURCE}": {"status": "ahead", "files": [{"filename": "scripts/production-deploy-v2.sh"}]}}

        def download(*args):
            directory = Path(args[args.index("--dir") + 1])
            directory.mkdir(parents=True)
            if args[2] == "100":
                (directory / "release-proof.json").write_text(json.dumps(self.proof))
                for name in policy.PROOF_CHECKS:
                    (directory / (name + ".json")).write_bytes((self.root / (name + ".json")).read_bytes())
            else:
                manifest = dict(self.manifest, workflow_run="123", workflow_run_attempt="1")
                (directory / "gateway-image-release.json").write_text(json.dumps(manifest))
                (directory / "binary-build-info.txt").write_text("unit-test-only")
                (directory / "schema-rehearsal.json").write_text(json.dumps(manifest["schema_transition"]))
                (directory / "schema-rehearsal.target.sql").write_text("-- synthetic schema")
                hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.iterdir()}
                (directory / "artifact-hashes.json").write_text(json.dumps(hashes))
            return ""

        with patch.dict(os.environ, {"GITHUB_RUN_ID": "900", "GITHUB_RUN_ATTEMPT": "1", "CLEARAPI_EXECUTOR_IMAGE_REPOSITORY": EXECUTOR_IMAGE}), \
                patch.object(policy, "api", side_effect=lambda path: deepcopy(responses[path])), \
                patch.object(policy, "latest_main_ci", return_value={"id": 96}), patch.object(policy, "successful_jobs"), \
                patch.object(policy, "pages", return_value=[{"workflow_runs": []}]) as history, \
                patch.object(policy, "gh", side_effect=download) as downloads:
            result = policy.authorize_release({"workflow_run": rehearsal}, self.root / "accepted")
            self.assertEqual(result["image_digest"], DIGEST)
            self.assertEqual(downloads.call_count, 2)
            self.assertEqual(result["rehearsal_run_id"], "100")
            downloads.reset_mock()
            history.return_value = [{"workflow_runs": [{"id": 899, "conclusion": "failure"}]}]
            with self.assertRaisesRegex(ValueError, "circuit open"):
                policy.authorize_release({"workflow_run": rehearsal}, self.root / "rejected")
            downloads.assert_not_called()
            with patch.dict(os.environ, {"GITHUB_RUN_ATTEMPT": "2"}):
                with self.assertRaisesRegex(ValueError, "reruns"):
                    policy.authorize_release({"workflow_run": rehearsal}, self.root / "retry")
            responses[f"compare/{'d' * 40}...{SOURCE}"]["files"] = []
            with self.assertRaisesRegex(ValueError, "empty"):
                policy.authorize_release({"workflow_run": rehearsal}, self.root / "empty-diff")


class TrustPolicyTest(unittest.TestCase):
    def test_pr_policy_binds_trusted_author_current_head_base_and_protection(self):
        run = {"id": 42, "run_attempt": 1, "head_sha": SOURCE, "event": "pull_request",
               "conclusion": "success", "path": ".github/workflows/ci.yml",
               "repository": {"full_name": policy.REPOSITORY},
               "head_repository": {"full_name": policy.REPOSITORY}}
        pr = {"number": 7, "state": "open", "base": {"ref": "main"},
              "head": {"repo": {"full_name": policy.REPOSITORY}, "ref": "codex/help", "sha": SOURCE},
              "user": {"login": "a3225865526-wq"}, "draft": False, "mergeable": True,
              "mergeable_state": "clean", "changed_files": 1}
        protection = {"enforce_admins": {"enabled": True}, "allow_force_pushes": {"enabled": False},
                      "allow_deletions": {"enabled": False}, "required_status_checks": {
                          "strict": True, "checks": [{"context": n, "app_id": 15368}
                                                     for n in policy.REQUIRED_CHECKS]}}
        responses = {f"commits/{SOURCE}/pulls?per_page=100": [pr], "pulls/7": pr,
                     "git/ref/heads/main": {"object": {"sha": "d" * 40}},
                     f"compare/{'d' * 40}...{SOURCE}": {"merge_base_commit": {"sha": "d" * 40}},
                     "branches/main/protection": protection, "actions/runs/42": run}

        def paginated(path):
            if path.startswith("pulls/"):
                return [[{"filename": "web/src/components/help.tsx"}]]
            return [{"jobs": [{"name": name, "conclusion": "success"} for name in policy.REQUIRED_CHECKS]}]

        with patch.object(policy, "api", side_effect=lambda path: deepcopy(responses[path])), patch.object(policy, "pages", side_effect=paginated):
            self.assertEqual(policy.authorize_pr({"workflow_run": run}), {"number": 7, "head": SOURCE})
            for field, value in (("user", {"login": "outsider"}), ("draft", True),
                                 ("mergeable", False), ("head", {"repo": {"full_name": policy.REPOSITORY}, "ref": "codex/help", "sha": "0" * 40})):
                with self.subTest(field=field):
                    original = pr[field]
                    pr[field] = value
                    with self.assertRaises(ValueError):
                        policy.authorize_pr({"workflow_run": run})
                    pr[field] = original
            protection["required_status_checks"]["strict"] = False
            with self.assertRaisesRegex(ValueError, "strict"):
                policy.authorize_pr({"workflow_run": run})

    def test_sensitive_and_unknown_changes_are_not_low_risk(self):
        paths = ["AGENTS.md", ".github/workflows/ci.yml", "model/schema.go", "go.mod",
                 "scripts/production-deploy.sh", "web/src/features/billing/page.tsx",
                 "web/src/features/auth/login.tsx", ".agents/rules/billing.md",
                 "web/package.json", "web/src/features/runtime/page.tsx"]
        self.assertEqual(policy.low_risk_paths(paths), paths)
        self.assertEqual(policy.low_risk_paths(["docs/help.md", "web/src/components/help.tsx"]), [])

    def test_run_source_event_and_repository_are_bound(self):
        run = {"repository": {"full_name": policy.REPOSITORY},
               "head_repository": {"full_name": policy.REPOSITORY},
               "path": ".github/workflows/ci.yml", "head_sha": SOURCE,
               "head_branch": "main", "event": "push", "status": "completed",
               "conclusion": "success"}
        policy.verified_run(run, SOURCE, "ci.yml", "push")
        for field, value in (("head_repository", {"full_name": "attacker/fork"}),
                             ("head_sha", "0" * 40), ("event", "pull_request"),
                             ("path", ".github/workflows/fake.yml"),
                             ("conclusion", "skipped"), ("head_branch", "codex/dev")):
            with self.subTest(field=field):
                bad = deepcopy(run)
                bad[field] = value
                with self.assertRaises(ValueError):
                    policy.verified_run(bad, SOURCE, "ci.yml", "push")

    def test_failed_or_skipped_job_blocks_entire_run(self):
        for status in ("failure", "skipped", "cancelled", None):
            with self.subTest(status=status), patch.object(policy, "pages", return_value=[{"jobs": [{"conclusion": status}]}]):
                with self.assertRaises(ValueError):
                    policy.successful_jobs({"id": 1, "run_attempt": 2})

    def test_fork_pr_does_not_reach_github_queries(self):
        event = {"workflow_run": {"event": "pull_request", "conclusion": "success",
                 "repository": {"full_name": policy.REPOSITORY},
                 "head_repository": {"full_name": "attacker/fork"}}}
        with patch.object(policy, "api") as mocked:
            with self.assertRaisesRegex(ValueError, "fork"):
                policy.authorize_pr(event)
            mocked.assert_not_called()


if __name__ == "__main__":
    unittest.main()
