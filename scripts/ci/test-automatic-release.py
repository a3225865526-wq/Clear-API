"""Offline policy regression tests. Synthetic receipts are never production evidence."""

from copy import deepcopy
from datetime import datetime, timedelta, timezone
import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("automatic_release", Path(__file__).with_name("automatic-release.py"))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)
audit_spec = importlib.util.spec_from_file_location('release_audit', Path(__file__).with_name('audit-release-state.py'))
audit = importlib.util.module_from_spec(audit_spec)
audit_spec.loader.exec_module(audit)
SOURCE = "a" * 40
DIGEST = "sha256:" + "b" * 64
EXECUTOR_IMAGE = "ghcr.io/a3225865526-wq/executor-unit-fixture"


class ReadOnlyReleaseAuditTest(unittest.TestCase):
    def test_unexpected_library_error_cannot_print_credentials_or_mark_audit_complete(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = io.StringIO()
            with patch.dict(os.environ, {'AUDIT_RC_RUN_ID': '123'}), \
                 patch.object(audit, 'gh', side_effect=ValueError('never-echo-credential')), \
                 patch.object(audit, 'Path', side_effect=lambda p: root / p), \
                 patch('sys.stdout', output):
                self.assertEqual(audit.main(), 1)
            report = json.loads((root / 'release-state-audit.json').read_text())
            self.assertFalse(report['audit_complete'])
            self.assertFalse(report['circuit_recovered'])
            self.assertEqual(report['status'], 'BLOCKED')
            self.assertNotIn('never-echo-credential', output.getvalue() + json.dumps(report))

    def test_evidence_reports_binding_without_echoing_secret_payloads_or_granting_pass(self):
        manifest = {'source_commit': SOURCE, 'image_digest': DIGEST, 'workflow_run': '123',
                    'workflow_run_attempt': '1', 'schema_transition': {
                        'source_schema_sha256': '1' * 64, 'target_schema_sha256': '2' * 64}}
        evidence = {'schema': 'clearapi.rehearsal-evidence/v1', 'kind': 'backup',
                    'conclusion': 'success', 'source_commit': 'c' * 40, 'gateway_digest': DIGEST,
                    'manifest_sha256': '3' * 64, 'source_schema_sha256': '1' * 64,
                    'target_schema_sha256': '2' * 64, 'rc_run_id': 123, 'rc_run_attempt': 1,
                    'target_id': 'private-target-value', 'token': 'never-echo-token',
                    'provenance': 'https://example.invalid/?token=never-echo-provenance'}
        report = audit.evidence_metadata(json.dumps(evidence), 'backup', manifest, '3' * 64,
                                         'private-target-value')
        self.assertFalse(report['binding_matches']['source_commit'])
        self.assertTrue(report['binding_matches']['gateway_digest'])
        self.assertTrue(report['binding_matches']['target_id'])
        self.assertFalse(report['original_acceptance_verified'])
        for secret in ('never-echo-token', 'never-echo-provenance', 'private-target-value'):
            self.assertNotIn(secret, json.dumps(report))
        evidence['source_commit'] = 'credential-that-is-not-a-sha'
        report = audit.evidence_metadata(json.dumps(evidence), 'backup', manifest, '3' * 64, '')
        self.assertIsNone(report['declared_release_identity']['source_commit'])
        self.assertNotIn('credential-that-is-not-a-sha', json.dumps(report))
        self.assertIn('error', audit.evidence_metadata('/root/private-file', 'backup', manifest, '', ''))

    def test_deployment_records_preserve_unresolved_history_without_payload_or_recovery(self):
        record = {'id': 9, 'sha': SOURCE, 'created_at': '2026-10-19T05:14:53Z',
                  'payload': {'token': 'never-echo-payload'}}
        statuses = [{'id': 10, 'state': 'in_progress',
                     'log_url': 'https://github.com/' + audit.REPO + '/actions/runs/39203542451/job/11'}]
        result = audit.deployment_metadata(record, statuses)
        self.assertTrue(result['requires_investigation'])
        self.assertEqual(result['statuses'][0]['actions_run_id'], 39203542451)
        self.assertNotIn('never-echo-payload', json.dumps(result))
        unknown = audit.deployment_metadata(record, [{'id': 10, 'state': 'unclassified'}])
        self.assertTrue(unknown['requires_investigation'])
        self.assertEqual(unknown['statuses'][0]['state'], 'unclassified')
        self.assertFalse(unknown['statuses'][0]['known_state'])
        unsafe = audit.deployment_metadata(record, [{'id': 10, 'state': 'arbitrary sensitive text'}])
        self.assertEqual(unsafe['statuses'][0]['state'], 'UNKNOWN')

    def test_audit_job_has_no_production_environment_or_ssh_and_excludes_vps_job(self):
        workflow = (Path(__file__).parents[2] / '.github/workflows/ghcr-pull-rehearsal.yml').read_text()
        job = workflow.split('  release_audit:\n', 1)[1].split('  rehearse:\n', 1)[0]
        self.assertIn('github.actor == github.repository_owner', job)
        self.assertIn('github.triggering_actor == github.repository_owner', job)
        self.assertIn('      deployments: read', job)
        for forbidden in ('environment: production', 'PROD_SSH', 'sudo ', 'secrets: inherit', ': write'):
            self.assertNotIn(forbidden, job)
        self.assertIn("    if: ${{ !inputs.release_audit_only }}", workflow.split('  rehearse:\n', 1)[1])
        standalone = (Path(__file__).parents[2] / '.github/workflows/release-state-audit.yml').read_text()
        self.assertIn('branches: [codex/pg03-predecessor-compat-20261019]', standalone)
        self.assertIn('github.actor == github.repository_owner', standalone)
        self.assertIn('github.triggering_actor == github.repository_owner', standalone)
        for forbidden in ('environment: production', 'PROD_SSH', 'sudo ', ': write', 'workflow_run:'):
            self.assertNotIn(forbidden, standalone)


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
                                  "source_commit": SOURCE, "startup_passes": 2,
                                  "source_schema_sha256": "2" * 64,
                                  "target_schema_sha256": "2" * 64},
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
                              "rc_run_id": 123, "rc_run_attempt": 1,
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

    def test_changed_or_missing_schema_is_not_no_schema_change(self):
        for value in (None, "", "3" * 64):
            with self.subTest(schema=value):
                manifest = deepcopy(self.manifest)
                manifest["schema_transition"]["target_schema_sha256"] = value
                with self.assertRaisesRegex(ValueError, "RC schema transition"):
                    self.validate(manifest=manifest)

    def test_authentic_in_place_migration_is_accepted(self):
        # PG03 forbids migration flags, so an upgrading candidate proves itself
        # only through two idempotent startup passes on one database.
        manifest = deepcopy(self.manifest)
        manifest["schema_transition"].update(
            source_schema_sha256="2" * 64, target_schema_sha256="3" * 64, startup_passes=2)
        proof = deepcopy(self.proof)
        proof["migration"] = {"mode": "in_place_startup_migration",
                              "source_schema_sha256": "2" * 64,
                              "target_schema_sha256": "3" * 64, "startup_passes": 2}
        self.assertEqual(self.validate(manifest=manifest, proof=proof)["expected_commit"], SOURCE)

    def test_in_place_migration_requires_two_passes_and_exact_binding(self):
        manifest = deepcopy(self.manifest)
        manifest["schema_transition"].update(
            source_schema_sha256="2" * 64, target_schema_sha256="3" * 64, startup_passes=2)
        good = deepcopy(self.proof)
        good["migration"] = {"mode": "in_place_startup_migration",
                             "source_schema_sha256": "2" * 64,
                             "target_schema_sha256": "3" * 64, "startup_passes": 2}
        self.validate(manifest=manifest, proof=good)
        cases = [
            ("passes", lambda m, p: m["schema_transition"].update(startup_passes=1)),
            ("version", lambda m, p: m["schema_transition"].update(schema_version="other")),
            ("binding", lambda m, p: m["schema_transition"].update(source_commit="9" * 40)),
            ("mode", lambda m, p: p.update(migration={"mode": "no_schema_change"})),
            ("src", lambda m, p: p["migration"].update(source_schema_sha256="1" * 64)),
            ("tgt", lambda m, p: p["migration"].update(target_schema_sha256="1" * 64)),
            ("omit", lambda m, p: p.update(migration={})),
        ]
        for label, mutate in cases:
            with self.subTest(case=label):
                m, p = deepcopy(manifest), deepcopy(good)
                mutate(m, p)
                with self.assertRaises(ValueError):
                    self.validate(manifest=m, proof=p)

    def test_receipt_must_match_exact_rc_run(self):
        path = self.root / "backup_restore.json"
        for field, value in (("rc_run_id", None), ("rc_run_id", "123"), ("rc_run_id", 122),
                             ("rc_run_attempt", None), ("rc_run_attempt", "1"), ("rc_run_attempt", 2)):
            with self.subTest(field=field, value=value):
                receipt = json.loads(path.read_text())
                receipt.update(rc_run_id=123, rc_run_attempt=1)
                receipt[field] = value
                raw = json.dumps(receipt).encode()
                path.write_bytes(raw)
                self.proof["checks"]["backup_restore"]["sha256"] = hashlib.sha256(raw).hexdigest()
                with self.assertRaisesRegex(ValueError, "RC (run|attempt)"):
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
        rc = dict(run, id=123, path=".github/workflows/release-pg03-gateway.yml", event="workflow_dispatch")
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
            with patch.object(policy, "RECOVERY_RECORD", self.root / "missing-investigation.json"), \
                    self.assertRaisesRegex(ValueError, "circuit open"):
                policy.authorize_release({"workflow_run": rehearsal}, self.root / "rejected")
            downloads.assert_not_called()
            with patch.object(policy, "verify_circuit_recovery", return_value={"fixture": "unit-only"}):
                policy.authorize_release({"workflow_run": rehearsal}, self.root / "recovered")
                audit = json.loads((self.root / "recovered/recovery-audit.json").read_text())
                self.assertEqual(audit["rc_run_id"], 123)
                self.assertEqual(audit["rehearsal_run_id"], 100)
                self.assertEqual(audit["gateway_digest"], DIGEST)
            with patch.dict(os.environ, {"GITHUB_RUN_ATTEMPT": "2"}):
                with self.assertRaisesRegex(ValueError, "reruns"):
                    policy.authorize_release({"workflow_run": rehearsal}, self.root / "retry")
            history.return_value = [{"workflow_runs": []}]
            responses[f"compare/{'d' * 40}...{SOURCE}"]["files"] = []
            with self.assertRaisesRegex(ValueError, "empty"):
                policy.authorize_release({"workflow_run": rehearsal}, self.root / "empty-diff")


class CircuitRecoveryTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.record_path = Path(directory.name) / "investigation.json"
        now = datetime.now(timezone.utc)
        self.log = "2026-10-19T00:00:00.0000000Z RELEASE BLOCKED: unexpected run event\n"
        self.record = {
            "schema": "clearapi.authorization-circuit-recovery/v1", "baseline_commit": "d" * 40,
            "investigated_at": (now - timedelta(minutes=1)).isoformat(),
            "expires_at": (now + timedelta(hours=1)).isoformat(),
            "failures": [{"run_id": 899, "source_commit": "e" * 40, "run_attempt": 1,
                          "created_at": "2026-10-19T00:00:00Z", "authorization_job_id": 10,
                          "deployment_job_id": 11, "reason": "unexpected run event",
                          "authorization_log_sha256": hashlib.sha256(self.log.rstrip("\n").encode()).hexdigest()}],
        }
        self.run = {"id": 899, "path": ".github/workflows/automatic-deploy.yml", "event": "workflow_run",
                    "head_branch": "main", "repository": {"full_name": policy.REPOSITORY},
                    "head_repository": {"full_name": policy.REPOSITORY}, "head_sha": "e" * 40,
                    "created_at": "2026-10-19T00:00:00Z", "run_attempt": 1,
                    "status": "completed", "conclusion": "failure"}
        self.jobs = [
            {"id": 10, "name": "deploy / Validate release authorization before production credentials",
             "status": "completed", "conclusion": "failure", "steps": [
                 {"name": "Authenticate automatic proof before production credentials", "conclusion": "failure"}]},
            {"id": 11, "name": "deploy / Controlled production deploy (exact digest)",
             "status": "completed", "conclusion": "skipped", "steps": None},
        ]
        self.manual = []
        self.deployments = []
        self.previous = [self.run]
        self.comparison = {"status": "ahead", "merge_base_commit": {"sha": "d" * 40}}
        self.inspection = None

    def configure_inspection(self):
        self.inspection_log = "unit-test-only inspection stopped before Runtime\n"
        self.inspection_sources = {path: b"unit-test-only immutable source" for path in (
            ".github/workflows/ghcr-pull-rehearsal.yml", "scripts/ci/inspect-vps-authorization.py")}
        deployment = {"id": 700, "sha": "f" * 40, "ref": "main", "environment": "production",
                      "created_at": "2026-10-19T00:10:00Z", "task": "deploy",
                      "production_environment": False, "transient_environment": False}
        self.deployments = [deepcopy(deployment)]
        self.inspection = dict(self.run, id=800, path=".github/workflows/ghcr-pull-rehearsal.yml",
                               event="workflow_dispatch", head_sha=deployment["sha"],
                               created_at=deployment["created_at"],
                               actor={"login": "a3225865526-wq"},
                               triggering_actor={"login": "a3225865526-wq"})
        self.inspection_steps = [
            {"name": "Inspect existing VPS authorization without deployment", "conclusion": "failure"},
            {"name": "Verify installed rehearsal wrapper matches this commit", "conclusion": "skipped"},
            {"name": "Run pull rehearsal on VPS", "conclusion": "skipped"}]
        self.inspection_jobs = [{"id": 801, "name": "VPS authorization inspection or exact-digest pull rehearsal",
                                 "status": "completed", "conclusion": "failure", "steps": self.inspection_steps}]
        self.inspection_statuses = [{"id": 701, "state": "failure", "environment": "production",
                                     "created_at": "2026-10-19T00:11:00Z",
                                     "log_url": f"https://github.com/{policy.REPOSITORY}/actions/runs/800/job/801"}]
        self.record["read_only_inspections"] = [{"deployment": deployment, "run_id": 800,
            "source_commit": deployment["sha"], "run_attempt": 1, "job_id": 801,
            "log_sha256": hashlib.sha256(self.inspection_log.rstrip("\n").encode()).hexdigest(),
            "source_sha256": {p: hashlib.sha256(raw).hexdigest() for p, raw in self.inspection_sources.items()},
            "statuses": deepcopy(self.inspection_statuses)}]

    def verify(self):
        self.record_path.write_text(json.dumps(self.record))

        def api(path):
            if path == "actions/runs/899":
                return self.run
            if path == f"compare/{'d' * 40}...{SOURCE}":
                return self.comparison
            if self.inspection and path == "actions/runs/800":
                return self.inspection
            if self.inspection and path.startswith("contents/"):
                filename = path.removeprefix("contents/").split("?ref=", 1)[0]
                return {"encoding": "base64", "content": base64.b64encode(self.inspection_sources[filename]).decode()}
            raise AssertionError(path)

        def pages(path):
            if path == "actions/runs/899/attempts/1/jobs?per_page=100":
                return [{"jobs": self.jobs}]
            if path == "actions/workflows/deploy-production.yml/runs?per_page=100":
                return [{"workflow_runs": self.manual}]
            if path == "deployments?environment=production&per_page=100":
                return [self.deployments]
            if self.inspection and path == "actions/runs/800/attempts/1/jobs?per_page=100":
                return [{"jobs": self.inspection_jobs}]
            if self.inspection and path == "deployments/700/statuses?per_page=100":
                return [self.inspection_statuses]
            raise AssertionError(path)

        with patch.object(policy, "RECOVERY_RECORD", self.record_path), \
                patch.object(policy, "api", side_effect=api), patch.object(policy, "pages", side_effect=pages), \
                patch.object(policy, "gh", side_effect=lambda *args: self.inspection_log
                             if args[-1].endswith("/801/logs") else self.log), \
                patch.dict(os.environ, {"GITHUB_RUN_ID": "900"}):
            return policy.verify_circuit_recovery(self.previous, SOURCE)

    def test_exact_investigation_is_revalidated_and_produces_only_recovery_audit(self):
        result = self.verify()
        self.assertEqual(result["failures"][0]["run_id"], 899)
        self.assertEqual(result["deployment_run_id"], "900")
        self.assertEqual(result["production_deployments_since_incident"], 0)
        self.assertNotIn("success", result)
        self.assertEqual(result["investigation_sha256"], hashlib.sha256(self.record_path.read_bytes()).hexdigest())

    def test_reviewed_read_only_record_preserves_real_count_and_failed_history(self):
        self.configure_inspection()
        result = self.verify()
        self.assertEqual(result["production_deployments_since_incident"], 1)
        self.assertEqual(result["read_only_inspections"][0]["deployment_id"], 700)
        self.assertEqual(self.inspection_statuses[0]["state"], "failure")
        self.deployments.append(dict(self.deployments[0], id=702))
        with self.assertRaisesRegex(ValueError, "requires investigation"):
            self.verify()
        self.deployments = []
        with self.assertRaisesRegex(ValueError, "requires investigation"):
            self.verify()

    def test_inspection_changes_and_any_pull_operation_fail_closed(self):
        for field, value in (("head_sha", "0" * 40), ("run_attempt", 2),
                             ("event", "workflow_run"), ("conclusion", "success"),
                             ("triggering_actor", {"login": "outsider"})):
            with self.subTest(field=field):
                self.configure_inspection()
                self.inspection[field] = value
                with self.assertRaises(ValueError):
                    self.verify()
        for position in (1, 2):
            self.configure_inspection()
            self.inspection_steps[position]["conclusion"] = "success"
            with self.assertRaises(ValueError):
                self.verify()
        self.configure_inspection()
        self.inspection_jobs.append(dict(self.inspection_jobs[0], id=802))
        with self.assertRaises(ValueError):
            self.verify()

    def test_inspection_log_source_status_and_deployment_tampering_fail_closed(self):
        for change in ("log", "source", "source_inventory", "status", "status_added", "deployment"):
            with self.subTest(change=change):
                self.configure_inspection()
                if change == "log": self.inspection_log += "changed"
                elif change == "source": self.inspection_sources["scripts/ci/inspect-vps-authorization.py"] += b"changed"
                elif change == "source_inventory": self.record["read_only_inspections"][0]["source_sha256"].pop("scripts/ci/inspect-vps-authorization.py")
                elif change == "status": self.inspection_statuses[0]["log_url"] += "changed"
                elif change == "status_added": self.inspection_statuses.insert(0, dict(self.inspection_statuses[0], id=702))
                else: self.deployments[0]["sha"] = "0" * 40
                with self.assertRaises(ValueError):
                    self.verify()

    def test_any_production_entry_or_unknown_job_is_unrecoverable(self):
        original = deepcopy(self.jobs)
        for change in (
            {"conclusion": "failure"}, {"status": "in_progress"},
            {"steps": [{"name": "Set up SSH", "conclusion": "success"}]},
            {"name": "unclassified production job"},
        ):
            with self.subTest(change=change):
                self.jobs = deepcopy(original)
                self.jobs[1].update(change)
                with self.assertRaises(ValueError):
                    self.verify()
        self.jobs = original + [{"id": 12}]
        with self.assertRaises(ValueError):
            self.verify()

    def test_new_failed_active_cancelled_or_retried_run_is_not_covered(self):
        for status, conclusion in (("completed", "failure"), ("in_progress", None),
                                   ("completed", "cancelled")):
            with self.subTest(conclusion=conclusion):
                self.previous = [{"id": 901, "status": status, "conclusion": conclusion}, self.run]
                with self.assertRaises(ValueError):
                    self.verify()
        self.previous = [self.run]
        self.run["run_attempt"] = 2
        with self.assertRaises(ValueError):
            self.verify()

    def test_changed_identity_log_authorization_step_and_ancestry_fail_closed(self):
        for field, value in (("head_sha", "0" * 40), ("event", "workflow_dispatch"),
                             ("head_repository", {"full_name": "attacker/fork"}),
                             ("created_at", "2026-10-19T01:00:00Z")):
            with self.subTest(field=field):
                original = self.run[field]
                self.run[field] = value
                with self.assertRaises(ValueError):
                    self.verify()
                self.run[field] = original
        self.log += "changed log\n"
        with self.assertRaisesRegex(ValueError, "log changed"):
            self.verify()
        self.log = self.log.removesuffix("changed log\n")
        self.jobs[0]["steps"][0]["name"] = "Checkout"
        with self.assertRaises(ValueError):
            self.verify()
        self.jobs[0]["steps"][0]["name"] = "Authenticate automatic proof before production credentials"
        self.comparison["merge_base_commit"]["sha"] = "0" * 40
        with self.assertRaises(ValueError):
            self.verify()

    def test_manual_runs_and_production_records_require_separate_investigation(self):
        for run in ({"status": "in_progress", "created_at": "2026-10-18T00:00:00Z"},
                    {"status": "completed", "created_at": "2026-10-19T00:00:00Z"}):
            with self.subTest(run=run):
                self.manual = [run]
                with self.assertRaises(ValueError):
                    self.verify()
        self.manual = []
        self.deployments = [{"created_at": "2026-10-19T00:00:00Z"}]
        with self.assertRaises(ValueError):
            self.verify()

    def test_expired_future_overlong_and_missing_investigation_fail_closed(self):
        original = deepcopy(self.record)
        now = datetime.now(timezone.utc)
        for issued, expires in ((now - timedelta(days=2), now - timedelta(days=1)),
                               (now + timedelta(minutes=1), now + timedelta(hours=1)),
                               (now - timedelta(minutes=1), now + timedelta(days=2))):
            self.record = deepcopy(original)
            self.record.update(investigated_at=issued.isoformat(), expires_at=expires.isoformat())
            with self.assertRaises(ValueError):
                self.verify()
        with patch.object(policy, "RECOVERY_RECORD", self.record_path.parent / "missing.json"):
            with self.assertRaisesRegex(ValueError, "missing reviewed"):
                policy.verify_circuit_recovery(self.previous, SOURCE)


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
