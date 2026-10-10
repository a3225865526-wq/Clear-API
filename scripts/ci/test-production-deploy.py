#!/usr/bin/env python3
"""Exercise wrapper modes with isolated files and fail-closed Docker probes."""

import fcntl
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import textwrap
import unittest


SOURCE = Path(__file__).resolve().parents[1] / "production-deploy.sh"
DIGEST = "sha256:" + "a" * 64
COMMIT = "b" * 40
INSTALLER_COMMIT = "f1797fbb3752100c665ddf85d9dfae89b7765ad7"


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
        self.config.chmod(0o600)
        # Redirect only the fixed operator config path in the test copy.
        self.wrapper = self.root / "wrapper.sh"
        self.wrapper.write_text(SOURCE.read_text().replace(
            "production_path = str(p) == '/opt/clearapi/config/deploy.env'",
            "production_path = False # isolated non-production fixture"
        ).replace(
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
elif args[0:2] == ["docker", "compose"] and "config" in args and args[-2:] == ["--format", "json"]:
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
elif a[:4] == ["docker", "image", "inspect", "--format"] and a[4] == "{{.Id}}":
    print("sha256:running-local-id")
elif a[-2:] == ["printenv", "CLEARAPI_CLOSED_LAUNCH"]:
    print(f.get("closed", "false"))
elif a[-3:] == ["config", "--format", "json"]:
    print(json.dumps({"services":{"new-api":{
        "image":"registry/example@sha256:" + "a" * 64,
        "stop_signal":f.get("signal", "SIGTERM"),
        "stop_grace_period":f.get("grace", "2m30s"),
        "environment":{"SHUTDOWN_TIMEOUT_SECONDS":f.get("timeout", "120")}}}}))
elif "psql" in a:
    if f.get("sql_error"): sys.exit(1)
    q = a[-1]
    if "EmailVerificationEnabled" in q:
        print(f.get("db_email", "true" if f.get("closed", "false") == "false" else "false"))
    elif "id=1 AND status=1" in q: print(f.get("provider", "1"))
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
if f.get("health_error"): raise SystemExit(1)
if f.get("health_malformed"): print("not-json"); raise SystemExit(0)
print(json.dumps({"success":True,"data":{
    "register_enabled":f.get("register", True),
    "password_login_enabled":f.get("password_login", True),
    "password_register_enabled":f.get("password", True),
    "email_verification":f.get("email", f.get("closed", "false") == "false")}}))
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

    def test_email_verification_matches_trusted_launch_baseline(self):
        cases = [
            ("commercial", True, True),
            ("commercial", False, False),
            ("closed", False, True),
            ("closed", True, False),
        ]
        for baseline, email, accepted in cases:
            with self.subTest(baseline=baseline, email=email):
                fixtures = {
                    "closed": "true" if baseline == "closed" else "false",
                    "register": baseline == "commercial",
                    "password": baseline == "commercial",
                    "email": email,
                }
                r = self.run_gate("verify_launch_baseline", f"DEPLOY_LAUNCH_BASELINE={baseline}", **fixtures)
                self.assertEqual(r.returncode == 0, accepted, r.stderr)

    def test_email_verification_database_and_status_must_match(self):
        r = self.run_gate("verify_launch_baseline", "DEPLOY_LAUNCH_BASELINE=commercial",
                          email=True, db_email="false")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("database EmailVerificationEnabled", r.stderr)

        r = self.run_gate("verify_launch_baseline", "DEPLOY_LAUNCH_BASELINE=commercial",
                          email=True, db_email="")
        self.assertEqual(r.returncode, 1, r.stderr)

        r = self.run_gate("verify_launch_baseline", "DEPLOY_LAUNCH_BASELINE=commercial",
                          email=True, health_error=True)
        self.assertEqual(r.returncode, 1, r.stderr)

        r = self.run_gate("verify_launch_baseline", "DEPLOY_LAUNCH_BASELINE=commercial",
                          email=True, health_malformed=True)
        self.assertEqual(r.returncode, 1, r.stderr)

    def test_commercial_rejects_drift_and_unsafe_stop(self):
        for fixture in ({"closed":"true"}, {"register":False}, {"password":False},
                        {"email":False}, {"password_login":False}, {"provider":"0"}, {"ability":"0"},
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
        rollback_manifest = self.root / "release-manifest.previous.json"
        rollback_manifest.write_text(self.manifest.read_text())
        previous = json.loads(rollback_manifest.read_text())
        self.manifest.write_text(json.dumps({"releases": {"current": {
            "new_api_image_id": "sha256:failed-candidate", "candidate_commit": COMMIT
        }}}))
        r = self.run_gate('PRE_DEPLOY_MANIFEST="' + str(rollback_manifest) + '"; '
                          'ROLLBACK_REF=registry/previous; PREV_IMAGE_ID=sha256:running-local-id; '
                          'health_retry() { return 0; }; rollback')
        self.assertEqual(r.returncode, 0, r.stderr)
        restored = json.loads(self.manifest.read_text())
        self.assertEqual(restored["releases"], previous["releases"])
        self.assertEqual(restored["failed_deployments"][0]["candidate"]["candidate_commit"], COMMIT)
        commands = [json.loads(line) for line in self.trace.read_text().splitlines()]
        self.assertEqual(len(commands), 2)
        for command in commands:
            self.assertEqual(command[-5:], ["up", "-d", "--timeout", "150", "new-api"])

    def test_config_permissions_and_symlink_rejected_under_python_optimize(self):
        self.env["PYTHONOPTIMIZE"] = "2"
        self.config.chmod(0o666)
        result = self.run_wrapper("--verify")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())
        self.config.chmod(0o600)
        original = self.config.with_suffix(".original")
        self.config.rename(original)
        self.config.symlink_to(original)
        result = self.run_wrapper("--verify")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.trace.exists())

    def test_audit_json_escaping_and_write_failure(self):
        detail = 'quoted "reason"\\path\nsecond line'
        result = self.run_gate("audit regression failed " + shlex.quote(detail))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(self.audit.read_text())["detail"], detail)
        self.audit.unlink()
        self.audit.mkdir()
        result = self.run_gate("audit regression failed reason")
        self.assertNotEqual(result.returncode, 0)


class InstallerFaultTest(unittest.TestCase):
    """Run a path-rewritten installer only against an isolated virtual VPS."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "opt/clearapi/bin"
        self.bin.mkdir(parents=True)
        self.bin.chmod(0o755)
        self.lockdir = self.root / "run/lock"
        self.lockdir.mkdir(parents=True)
        self.lockdir.chmod(0o755)
        self.target = self.bin / "production-deploy.sh"
        self.old = b"#!/bin/bash\n# old wrapper\nexit 0\n"
        self.target.write_bytes(self.old)
        self.target.chmod(0o750)
        self.source = self.root / "source.sh"
        self.source.write_bytes(SOURCE.read_bytes())
        self.machine = self.root / "machine-id"
        self.machine.write_text("virtual-vps")
        installer = SOURCE.parent / "clearapi-install-approved-script.sh"
        text = installer.read_text().replace("/opt", str(self.root / "opt"))
        text = text.replace("/run", str(self.root / "run"))
        text = text.replace("/etc/machine-id", str(self.machine))
        # The production script still requires root. Here, only filesystem
        # ownership is virtualized so CI may run the sandbox without root.
        text = text.replace("s.st_uid != 0", "s.st_uid != os.getuid()")
        self.installer = self.root / "installer.sh"
        self.installer.write_text(text)
        self.mockbin = self.root / "mocks"
        self.mockbin.mkdir()
        host = self.mockbin / "host-command"
        host.write_text("#!/usr/bin/python3\n" + r'''
import os, pathlib, subprocess, sys
name=pathlib.Path(sys.argv[0]).name; a=sys.argv[1:]
root=pathlib.Path(os.environ['INSTALL_ROOT']); failure=os.environ['INSTALL_FAILURE']
if name=='id': print('0' if a==['-u'] else 'virtual clearapi-deploy'); sys.exit(0)
if name=='chown': sys.exit(0) # CI filesystem ownership is virtualized.
if name=='stat':
    value=subprocess.check_output(['/usr/bin/stat',*a],text=True).strip()
    if a[1]=='%u:%g:%a': value='0:0:'+value.split(':')[-1]
    elif a[1]=='%u:%a': value='0:'+value.split(':')[-1]
    elif a[1]=='%u': value='0'
    print(value); sys.exit(0)
if name=='install': os.execv('/usr/bin/install',['install','-m','0750',a[-2],a[-1]])
if name=='sudo':
    count=root/'sudo-count'; n=int(count.read_text())+1 if count.exists() else 1; count.write_text(str(n))
    if n==2 and failure in ('post','restore'): sys.exit(1)
    os.execv('/usr/bin/sha256sum',['sha256sum',a[-1]])
if name=='mv':
    if 'restore.sh' in a[-2]:
        with open(root/'restore-lock','w') as f:
            probe=subprocess.run(['/usr/bin/flock','-n',str(root/'run/lock/clearapi-production-deploy.lock'),'true'])
            f.write(str(probe.returncode))
        if failure=='restore': sys.exit(1)
    os.execv('/usr/bin/mv',['mv',*a])
if name=='python3':
    data=sys.stdin.read()
    if failure=='audit' and len(a)>3 and a[2]=='INSTALLED':
        data='import os\ndef broken_fsync(fd): raise OSError("injected persistence failure")\nos.fsync=broken_fsync\n'+data
    sys.argv=['-',*a[1:]]
    exec(compile(data,'<fixture-python>','exec')); sys.exit(0)
sys.exit('unexpected mock command')
''')
        host.chmod(0o755)
        for name in ("id", "chown", "stat", "install", "sudo", "mv", "python3"):
            (self.mockbin / name).symlink_to(host)
        self.env = dict(os.environ, PATH=str(self.mockbin)+os.pathsep+os.environ["PATH"],
                        INSTALL_ROOT=str(self.root), INSTALL_FAILURE="", PYTHONOPTIMIZE="2")

    def run_install(self, failure=""):
        env = dict(self.env, INSTALL_FAILURE=failure)
        return subprocess.run(["bash", str(self.installer), str(self.source), INSTALLER_COMMIT],
                              env=env, capture_output=True, text=True, timeout=10)

    def test_normal_install_hash_owner_and_permissions(self):
        result = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("INSTALL_PASS", result.stdout)
        self.assertEqual(self.target.read_bytes(), self.source.read_bytes())
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o750)
        self.assertEqual(self.target.stat().st_uid, os.getuid())
        self.assertEqual(self.target.stat().st_gid, os.getgid())
        self.assertEqual(hashlib.sha256(self.target.read_bytes()).hexdigest(), hashlib.sha256(SOURCE.read_bytes()).hexdigest())

    def test_post_replace_failure_restores_inside_lock(self):
        result = self.run_install("post")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("INSTALL_PASS", result.stdout)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o750)
        self.assertNotEqual((self.root / "restore-lock").read_text(), "0")
        audit = next(self.bin.glob(".script-install.*/audit.jsonl"))
        self.assertEqual(json.loads(audit.read_text().splitlines()[-1])["status"], "RESTORED")

    def test_restore_failure_retains_backup_and_audit(self):
        result = self.run_install("restore")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RESTORE_FAILED", result.stderr)
        self.assertNotIn("INSTALL_PASS", result.stdout)
        backup = next(self.bin.glob(".script-install.*/production-deploy.old.sh"))
        self.assertEqual(backup.read_bytes(), self.old)
        self.assertEqual(json.loads((backup.parent / "audit.jsonl").read_text().splitlines()[-1])["status"], "RESTORE_FAILED")

    def test_persistence_failure_never_reports_pass(self):
        result = self.run_install("audit")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("INSTALL_PASS", result.stdout)
        self.assertIn("injected persistence failure", result.stderr)
        self.assertEqual(self.target.read_bytes(), self.old)

    def test_lock_contention_rejects_without_replace(self):
        lock = self.lockdir / "clearapi-production-deploy.lock"
        with lock.open("w") as f:
            lock.chmod(0o600)
            fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = self.run_install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("lock is held", result.stderr)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertFalse(list(self.bin.glob(".script-install.*")))


class WorkflowStdinTest(unittest.TestCase):
    """Run the real workflow shell through SSH/sudo into all wrapper steps.

    Only host commands are substituted. Every non-login command attempts to
    drain stdin, so this detects the production regression rather than merely
    asserting that a helper contains a redirection.
    """

    setUp = WrapperModesTest.setUp

    def run_chain(self, failure="", payload="non-secret-ci-credential", framed=True,
                  legacy=False, compatible=False, migration=False):
        workflow = SOURCE.parents[1] / ".github/workflows/deploy-production.yml"
        block = workflow.read_text().split("      - name: Run controlled deploy on VPS\n")[1]
        block = textwrap.dedent(block.split("        run: |\n", 1)[1].split(
            "\n      - name:", 1)[0])
        for expression, value in {"inputs.image_digest": DIGEST,
                                  "inputs.expected_commit": COMMIT,
                                  "inputs.migration_compatible": "true" if compatible else "false",
                                  "github.run_id": "123"}.items():
            block = block.replace("${{ " + expression + " }}", value)
        if not framed:
            block = block.replace("printf '%s\\n'", "printf '%s'")
        source = self.wrapper.read_text().replace("/run/lock", str(self.root / "lock"))
        if legacy:
            # Mutation control: remove only the stream isolation. Full-chain
            # probes then consume the marker and the login rejects empty stdin.
            source = source.replace("exec 3<&0\nexec </dev/null", ": # deliberately unsafe stdin")
            start = source.index("# The workflow sends one newline-terminated record.")
            end = source.index("# Helpers (all defined before any step executes)", start)
            source = source[:start] + source[end:]
            source = source.replace("printf '%s' \"$GHCR_PULL_TOKEN\" | ", "")
        self.wrapper.write_text(source)
        self.wrapper.chmod(0o755)
        self.manifest.write_text(json.dumps({"releases": {"current": {
            "new_api_image_id": "sha256:running-local-id", "image_digest": DIGEST
        }}}))
        backup = self.root / "backup"
        backup.mkdir()
        caddy = self.root / "Caddyfile"
        caddy.write_text("fixture")
        with self.config.open("a") as f:
            if migration:
                f.write('\nDEPLOY_MIGRATION_FLAGS=--approved-fixture\n')
            f.write("\nDEPLOY_LAUNCH_BASELINE=commercial\nDEPLOY_EXPECT_CHANNELS=2\n"
                    "DEPLOY_EXPECT_ENV_KEYS='CLEARAPI_CLOSED_LAUNCH=false'\n"
                    "DEPLOY_GHCR_TOKEN_FILE=" + str(self.root / "absent-token") + "\n"
                    "DEPLOY_BACKUP_DIR=" + str(backup) + "\nDEPLOY_CADDYFILE=" + str(caddy) +
                    "\nDEPLOY_CADDYFILE_MD5=" + hashlib.md5(b"fixture").hexdigest() + "\n")
            if not legacy:
                f.write('if IFS= read -r unexpected; then echo "configuration consumed stdin" >&2; exit 1; fi\n')
        fake = self.root / "host-command"
        fake.write_text("#!/usr/bin/env python3\n" + r'''
import hashlib, json, os, pathlib, subprocess, sys
name=pathlib.Path(sys.argv[0]).name; a=sys.argv[1:]; root=pathlib.Path(os.environ['TEST_ROOT'])
def record(**more):
    with open(os.environ['TEST_TRACE'],'a') as f: f.write(json.dumps(dict(name=name,args=a,**more))+'\n')
if name=='ssh':
    record()
    # SSH preserves stdin but does not forward the runner's token environment.
    os.environ.pop('GH_PULL_TOKEN',None)
    command=a[-1].replace('/opt/clearapi/bin/production-deploy.sh',str(root/'wrapper.sh'))
    os.execvp('bash',['bash','-c',command])
if name=='sudo': os.execvp(a[0],a)
data=sys.stdin.buffer.read()
assert 'GHCR_PULL_TOKEN' not in os.environ
if name=='docker' and a[:1]==['login']:
    record(stdin_length=len(data),stdin_sha256=hashlib.sha256(data).hexdigest(),
           docker_config=os.environ.get('DOCKER_CONFIG'))
    if data!=b'non-secret-ci-credential' or os.environ['TEST_FAILURE']=='login': sys.exit(1)
    pathlib.Path(os.environ['DOCKER_CONFIG'],'config.json').write_text('non-secret fixture')
    sys.exit(0)
record(stdin_length=len(data))
if data: sys.exit('Non-login command consumed shared stdin')
fail=os.environ['TEST_FAILURE']
if name=='systemctl':
    if a[0]=='start':
        if fail=='backup': sys.exit(1)
        (root/'backup'/'encrypted-fixture').write_text('non-secret artifact')
    elif a[0]=='show': print('0')
    sys.exit(0)
if name=='curl':
    body=json.dumps({'success':True,'data':{'register_enabled':True,'password_login_enabled':True,'password_register_enabled':True,'email_verification':True}})
    if '-D' in a: pathlib.Path(a[a.index('-D')+1]).write_text('CF-Ray: fixture\n')
    if '-o' in a: pathlib.Path(a[a.index('-o')+1]).write_text(body)
    else: print(body)
    sys.exit(0)
if name!='docker': sys.exit('Forbidden host command')
if a[0]=='pull': (root/'logged-in').touch(); sys.exit(0)
if a[0]=='logout': sys.exit(0)
if a[:2]==['image','inspect']:
    fmt=a[3]
    if fmt=='{{.Id}}': print('sha256:running-local-id' if (root/'rolled-back').exists() else ('sha256:candidate-local-id' if (root/'logged-in').exists() else 'sha256:running-local-id'))
    elif 'RepoDigests' in fmt: print('registry/example@sha256:'+('c' if fail=='digest' else 'a')*64)
    elif 'revision' in fmt: print(('c' if fail=='revision' else 'b')*40)
    else: sys.exit('Forbidden image probe')
elif a[0]=='run':
    if '--entrypoint' in a and a[a.index('--entrypoint')+1]=='/billing-migrate': sys.exit(1)
elif a[0]=='inspect':
    if 'NetworkSettings' in a[2]: print('clearapi_internal')
    else: print('sha256:candidate-local-id' if (root/'replaced').exists() and not (root/'rolled-back').exists() else 'sha256:running-local-id')
elif a[0]=='port':
    if '3000/tcp' in a: print('127.0.0.1:3000')
elif a[:2]==['ps','-a']: pass
elif a[0]=='compose':
    if 'ps' in a: print('postgres-container' if a[-1]=='postgres' else 'running-container')
    elif 'config' in a:
        candidate = any('compose-jobs.yaml' in x for x in a)
        image = 'ghcr.io/a3225865526-wq/clearapi@sha256:'+'a'*64 if candidate else 'registry/example@sha256:'+'a'*64
        if candidate and fail=='job-image': image='registry/wrong:latest'
        service = {'image': image, 'stop_signal':'SIGTERM','stop_grace_period':'2m30s','environment':{'SHUTDOWN_TIMEOUT_SECONDS':'120'}}
        print(json.dumps({'services':{'new-api':service, 'production-bootstrap':{'image':image}}}))
    elif 'printenv' in a: print('false')
    elif 'psql' in a:
        q=a[-1]
        if 'EmailVerificationEnabled' in q: print('true')
        elif 'id=1 AND status=1' in q: print('0' if fail=='provider' else '1')
        elif 'FROM abilities' in q: print('0' if 'NOT' in q else '1')
        elif 'DisableOnlinePayments' in q: print('2')
        elif 'count(*)' in q: print('0')
        elif 'SELECT status' in q: print('2')
        elif 'needs_review' in q: print('4/10/4\n41/93/41')
        elif 'SELECT value' in q: print('enforce')
        else: sys.exit('Forbidden query')
    elif 'up' in a:
        if (root/'replaced').exists(): (root/'rolled-back').touch()
        else:
            (root/'replaced').touch()
            if fail=='partial': sys.exit(1)
    elif 'run' in a:
        if not any('compose-jobs.yaml' in x for x in a): sys.exit('job missing candidate override')
        if (root/'replaced').exists(): sys.exit('job ran after replacement')
    else: sys.exit('Forbidden compose operation')
else: sys.exit('Forbidden docker operation')
''')
        fake.chmod(0o755)
        for name in ("ssh", "sudo", "docker", "systemctl", "curl"):
            (self.root / name).unlink(missing_ok=True)
            (self.root / name).symlink_to(fake)
        env = dict(self.env, TEST_ROOT=str(self.root), TEST_FAILURE=failure,
                   GH_PULL_TOKEN=payload, PROD_SSH_ALIAS="fixture-vps",
                   GHCR_PULL_TOKEN="must-not-be-exported",
                   TMPDIR=str(self.root))
        result = subprocess.run(["bash", "-c", block], env=env, capture_output=True,
                                text=True, timeout=20)
        self.assertTrue(self.trace.exists(),
                        f"mock trace absent: exit={result.returncode}\n{result.stderr}")
        trace = [json.loads(line) for line in self.trace.read_text().splitlines()]
        output = result.stdout + result.stderr
        if self.audit.exists():
            output += self.audit.read_text()
        if payload:
            self.assertNotIn(payload, output)
            self.assertNotIn(payload, self.trace.read_text())
        logins = [x for x in trace if x["name"] == "docker" and x["args"][0] == "login"]
        for login in logins:
            self.assertFalse(Path(login["docker_config"]).exists(), "ephemeral credential config retained")
        return result, trace, logins

    def test_full_workflow_credential_ownership_and_commercial_deploy(self):
        result, trace, logins = self.run_chain()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("DEPLOYMENT SUCCESS", result.stderr)
        self.assertEqual(len(logins), 1)
        self.assertEqual(logins[0]["stdin_sha256"], hashlib.sha256(b"non-secret-ci-credential").hexdigest())
        self.assertTrue(all(x.get("stdin_length", 0) == 0 for x in trace if x not in logins))
        queries = [x["args"][-1] for x in trace if "psql" in x["args"]]
        self.assertTrue(any("id=1 AND status=1" in q for q in queries))
        self.assertTrue(any("SELECT status FROM channels WHERE id=2" in q for q in queries))
        self.assertFalse(any("SELECT status FROM channels WHERE id=1;" in q for q in queries))
        self.assertTrue(any(x["args"][-5:] == ["up","-d","--timeout","150","new-api"] for x in trace))

    def test_empty_and_truncated_frames_rejected_before_probes(self):
        result, trace, logins = self.run_chain(payload="")
        self.assertEqual(result.returncode, 2)
        self.assertFalse(logins)
        self.assertEqual([x["name"] for x in trace], ["ssh"])

    def test_truncated_frame_rejected(self):
        result, trace, logins = self.run_chain(framed=False)
        self.assertEqual(result.returncode, 2)
        self.assertIn("incomplete", result.stderr)
        self.assertFalse(logins)

    def test_wrong_credential_rejected_without_logging(self):
        result, trace, logins = self.run_chain(payload="truncated-non-secret")
        self.assertEqual(result.returncode, 1)
        self.assertFalse((self.root / "replaced").exists())

    def test_baseline_failure_prevents_login(self):
        result, trace, logins = self.run_chain(failure="provider")
        self.assertEqual(result.returncode, 1)
        self.assertFalse(logins)
        self.assertIn("Channel 1", result.stderr)

    def test_backup_failure_prevents_login(self):
        result, trace, logins = self.run_chain(failure="backup")
        self.assertEqual(result.returncode, 1)
        self.assertFalse(logins)

    def test_login_failure_cleans_ephemeral_config(self):
        result, trace, logins = self.run_chain(failure="login")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(len(logins), 1)
        self.assertFalse((self.root / "replaced").exists())

    def test_digest_mismatch_prevents_replace(self):
        result, trace, logins = self.run_chain(failure="digest")
        self.assertEqual(result.returncode, 1)
        self.assertIn("RepoDigests", result.stderr)
        self.assertFalse((self.root / "replaced").exists())

    def test_revision_mismatch_prevents_replace(self):
        result, trace, logins = self.run_chain(failure="revision")
        self.assertEqual(result.returncode, 1)
        self.assertIn("revision label", result.stderr)
        self.assertFalse((self.root / "replaced").exists())

    def test_regression_control_detects_stdin_consumer(self):
        result, trace, logins = self.run_chain(legacy=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(any(x.get("stdin_length", 0)>0 for x in trace if x["name"]!='ssh'))

    def test_partial_replace_rolls_back_manifest_and_identity(self):
        result, trace, _ = self.run_chain(failure="partial", compatible=True)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertTrue((self.root / "rolled-back").exists())
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest["releases"]["current"]["new_api_image_id"], "sha256:running-local-id")
        self.assertEqual(manifest["failed_deployments"][0]["candidate"]["candidate_commit"], COMMIT)
        self.assertIn("rollback complete", result.stderr)
        compose_configs = [x for x in trace if x["name"] == "docker" and
                           x["args"][0] == "compose" and "config" in x["args"]]
        image_probes = [x for x in trace if x["name"] == "docker" and
                        x["args"][0:3] == ["image", "inspect", "--format"]]
        self.assertGreaterEqual(len(compose_configs), 2)
        self.assertGreaterEqual(len(image_probes), 2)

    def test_wrong_job_image_prevents_migration(self):
        result, trace, _ = self.run_chain(failure="job-image", migration=True)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("candidate job image mismatch", result.stderr)
        self.assertFalse(any(x["name"] == "docker" and "compose" in x["args"] and "run" in x["args"] for x in trace))

    def test_migration_and_bootstrap_use_same_candidate(self):
        result, trace, _ = self.run_chain(migration=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        jobs = [x for x in trace if x["name"] == "docker" and "compose" in x["args"] and "run" in x["args"]]
        self.assertEqual(len(jobs), 2)
        overrides = [next(a for a in x["args"] if a.endswith("compose-jobs.yaml")) for x in jobs]
        self.assertEqual(overrides[0], overrides[1])
        self.assertTrue(all("--no-deps" in x["args"] and "never" in x["args"] for x in jobs))


if __name__ == "__main__":
    unittest.main()
