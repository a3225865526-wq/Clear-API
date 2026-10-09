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
        self.config = self.root / "deploy.env"
        self.config.write_text("\n".join(
            key + "=" + shlex.quote(str(value)) for key, value in {
                "DEPLOY_APP_DIR": self.root, "DEPLOY_OVERRIDE_FILE": self.override,
                "DEPLOY_MANIFEST": self.manifest, "DEPLOY_AUDIT_LOG": self.audit,
            }.items()
        ))
        # Redirect only the fixed operator config path in the test copy.
        self.wrapper = self.root / "wrapper.sh"
        self.wrapper.write_text(SOURCE.read_text().replace(
            "/opt/clearapi/config/deploy.env", str(self.config)
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

    def run_gate(self, body, config="", **fixtures):
        with self.config.open("a") as f:
            f.write("\n" + config + "\n")
        # Execute the actual configuration and helper definitions in Bash;
        # stop before the deployment steps, and allow only fixture probes.
        source = self.wrapper.read_text().split("# Identity-only --verify mode:")[0]
        gate = self.root / "gate.sh"
        gate.write_text(source + "\n" + body + "\n")
        probe = self.root / "sudo"
        probe.write_text("#!/usr/bin/env python3\n" + '''
import json, os, sys
a = sys.argv[1:]
f = json.loads(os.environ["GATE_FIXTURES"])
if a[-5:] == ["up", "-d", "--timeout", "150", "new-api"]:
    with open(os.environ["TEST_TRACE"], "a") as out: out.write(json.dumps(a) + "\\n")
elif a[-3:] == ["ps", "-q", "new-api"]:
    print("running-container")
elif a == ["docker", "inspect", "--format", "{{.Image}}", "running-container"]:
    print("sha256:running-local-id")
elif a[-2:] == ["printenv", "CLEARAPI_CLOSED_LAUNCH"]:
    print(f.get("closed", "false"))
elif a[-3:] == ["config", "--format", "json"]:
    print(json.dumps({"services":{"new-api":{
        "stop_signal":f.get("signal", "SIGTERM"),
        "stop_grace_period":f.get("grace", "2m30s"),
        "environment":{"SHUTDOWN_TIMEOUT_SECONDS":f.get("timeout", "120")}}}}))
elif "psql" in a:
    if f.get("sql_error"): sys.exit(1)
    q = a[-1]
    if "id=1 AND status=1" in q: print(f.get("provider", "1"))
    elif "FROM abilities" in q and "NOT" not in q: print(f.get("ability", "1"))
    elif "FROM abilities" in q: print(f.get("extra_ability", "0"))
    elif "count(*) FROM channels" in q: print(f.get("extra_channel", "0"))
    elif "SELECT status FROM channels" in q: print(f.get("disabled", "2"))
    elif "DisableOnlinePayments" in q: print(f.get("off", "2"))
    else: sys.exit("Forbidden query")
else: sys.exit("Forbidden operation")
''')
        probe.chmod(0o755)
        curl = self.root / "curl"
        curl.write_text("#!/usr/bin/env python3\n" + '''
import json, os
f=json.loads(os.environ["GATE_FIXTURES"])
print(json.dumps({"success":True,"data":{
    "register_enabled":f.get("register", True),
    "password_register_enabled":f.get("password", True),
    "email_verification":f.get("email", False)}}))
''')
        curl.chmod(0o755)
        env = dict(self.env, GATE_FIXTURES=json.dumps(fixtures))
        return subprocess.run(["bash", str(gate)], env=env, capture_output=True,
                              text=True, timeout=10)

    def test_config_loaded_before_aliases_and_operator_wins(self):
        self.env["DEPLOY_EXPECT_CHANNELS"] = "1,2"
        r = self.run_gate('printf "%s/%s/%s" "$EXPECT_CHANNELS" "$LAUNCH_BASELINE" "$APP_DIR"',
                          "DEPLOY_LAUNCH_BASELINE=commercial\nDEPLOY_EXPECT_CHANNELS=2")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, "2/commercial/" + str(self.root))

    def test_commercial_and_closed_launch_acceptance(self):
        r = self.run_gate("verify_launch_baseline; verify_stop_policy", "DEPLOY_LAUNCH_BASELINE=commercial")
        self.assertEqual(r.returncode, 0, r.stderr)
        r = self.run_gate("verify_launch_baseline; verify_stop_policy", "DEPLOY_LAUNCH_BASELINE=closed",
                          closed="true", register=False, password=False)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_commercial_rejects_drift_and_unsafe_stop(self):
        for fixture in ({"closed":"true"}, {"register":False}, {"password":False},
                        {"email":True}, {"provider":"0"}, {"ability":"0"},
                        {"extra_ability":"1"}, {"extra_channel":"1"},
                        {"disabled":"1"}, {"off":"1"}, {"grace":"10s"},
                        {"signal":"SIGKILL"}, {"timeout":"180"}, {"sql_error":True}):
            with self.subTest(fixture=fixture):
                r = self.run_gate("verify_launch_baseline; verify_stop_policy",
                                  "DEPLOY_LAUNCH_BASELINE=commercial", **fixture)
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("FAIL:", r.stderr)

    def test_closed_baseline_rejects_open_state(self):
        r = self.run_gate("verify_launch_baseline", closed="false")
        self.assertEqual(r.returncode, 1, r.stderr)

    def test_commercial_rejects_old_channel_expectations(self):
        r = self.run_gate("true", "DEPLOY_LAUNCH_BASELINE=commercial\nDEPLOY_EXPECT_CHANNELS=1,2")
        self.assertEqual(r.returncode, 2, r.stderr)

    def test_unknown_baseline_rejected(self):
        r = self.run_gate("true", "DEPLOY_LAUNCH_BASELINE=typo")
        self.assertEqual(r.returncode, 2, r.stderr)

    def test_replace_and_rollback_use_explicit_grace_for_old_container(self):
        step = SOURCE.read_text().split("# 14. controlled replace")[1].split("# 15. localhost health")[0]
        setup = 'IMAGE_REF=registry/example; CANDIDATE_IMAGE_ID=sha256:running-local-id; '
        r = self.run_gate(setup + step)
        self.assertEqual(r.returncode, 0, r.stderr)
        r = self.run_gate('ROLLBACK_REF=registry/previous; PREV_IMAGE_ID=sha256:running-local-id; '
                          'health_retry() { return 0; }; rollback')
        self.assertEqual(r.returncode, 0, r.stderr)
        commands = [json.loads(line) for line in self.trace.read_text().splitlines()]
        self.assertEqual(len(commands), 2)
        for command in commands:
            self.assertEqual(command[-5:], ["up", "-d", "--timeout", "150", "new-api"])


if __name__ == "__main__":
    unittest.main()
