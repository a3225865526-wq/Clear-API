#!/usr/bin/env python3
"""Exercise wrapper modes with isolated files and fail-closed Docker probes."""

import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parents[1] / "production-deploy.sh"
DIGEST = "sha256:" + "a" * 64
COMMIT = "b" * 40


class WrapperModesTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.audit = self.root / "audit.jsonl"
        self.trace = self.root / "commands.jsonl"
        self.manifest = self.root / "release-manifest.json"
        self.manifest.write_text(json.dumps({"releases": {"current": {
            "new_api_image_id": "sha256:running-local-id"
        }}}))
        self.override = self.root / "compose-candidate.yaml"
        self.override.write_text("services:\n  new-api:\n    image: registry/example@" + DIGEST + "\n")
        config = self.root / "deploy.env"
        config.write_text("\n".join(
            key + "=" + shlex.quote(str(value)) for key, value in {
                "APP_DIR": self.root, "OVERRIDE_FILE": self.override,
                "MANIFEST": self.manifest, "AUDIT_LOG": self.audit,
            }.items()
        ))
        # Redirect only the fixed operator config path in the test copy.
        self.wrapper = self.root / "wrapper.sh"
        self.wrapper.write_text(SOURCE.read_text().replace(
            "/opt/clearapi/config/deploy.env", str(config)
        ))
        fake_sudo = self.root / "sudo"
        fake_sudo.write_text("#!/usr/bin/env python3\n" + '''
import json, os, sys
args = sys.argv[1:]
with open(os.environ["TEST_TRACE"], "a") as f:
    f.write(json.dumps(args) + "\\n")
if args[0:2] == ["docker", "compose"] and args[-3:] == ["ps", "-q", "new-api"]:
    print("running-container")
elif args[0:2] == ["docker", "compose"] and args[-3:] == ["config", "--format", "json"]:
    print(json.dumps({"services": {"new-api": {"image": "registry/example@sha256:" + "a" * 64}}}))
elif args == ["docker", "inspect", "--format", "{{.Image}}", "running-container"]:
    print("sha256:running-local-id")
elif args[0:4] == ["docker", "image", "inspect", "--format"] and args[4] == "{{.Id}}":
    print(os.environ.get("TEST_EFFECTIVE_ID", "sha256:running-local-id"))
else:
    sys.exit("Forbidden operation: " + repr(args))
''')
        fake_sudo.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                        TEST_TRACE=str(self.trace))

    def run_wrapper(self, *args):
        before = {p.name: p.read_bytes() for p in self.root.iterdir() if p.is_file()}
        result = subprocess.run(["bash", str(self.wrapper), *args], env=self.env,
                                capture_output=True, text=True, timeout=10)
        # Only the documented audit append and read-probe trace may change.
        after = {p.name: p.read_bytes() for p in self.root.iterdir()
                 if p.is_file() and p not in (self.audit, self.trace)}
        self.assertEqual(before, after)
        return result

    def test_verify_without_candidate_metadata_succeeds_and_audits(self):
        result = self.run_wrapper("--verify")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("identity OK", result.stderr)
        self.assertEqual(json.loads(self.audit.read_text())["result"], "ok")
        self.assertEqual(len(self.trace.read_text().splitlines()), 4)

    def test_verify_missing_override_fails(self):
        self.override.unlink()
        result = self.run_wrapper("--verify")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("override", result.stderr)
        self.assertEqual(json.loads(self.audit.read_text())["result"], "drift")

    def test_verify_manifest_mismatch_fails(self):
        self.manifest.write_text(json.dumps({"releases": {"current": {
            "new_api_image_id": "sha256:different-local-id"
        }}}))
        result = self.run_wrapper("--verify")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("manifest current", result.stderr)

    def test_verify_compose_mismatch_fails(self):
        self.env["TEST_EFFECTIVE_ID"] = "sha256:different-local-id"
        result = self.run_wrapper("--verify")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("compose effective image", result.stderr)

    def test_deploy_metadata_stays_strict_without_side_effects(self):
        cases = [
            (["--commit", COMMIT], "--digest and --commit are required"),
            (["--digest", DIGEST], "--digest and --commit are required"),
            (["--digest", "latest", "--commit", COMMIT], "exact 'sha256"),
            (["--digest", DIGEST, "--commit", "short"], "40-hex"),
            (["--digest", DIGEST, "--commit", COMMIT,
              "--migration-compatible", "invalid"], "true or false"),
        ]
        for args, message in cases:
            with self.subTest(args=args):
                result = self.run_wrapper(*args)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn(message, result.stderr)
                self.assertFalse(self.audit.exists())
                self.assertFalse(self.trace.exists())


if __name__ == "__main__":
    unittest.main()
