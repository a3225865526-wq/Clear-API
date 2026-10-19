#!/usr/bin/env python3
"""Exercise wrapper modes with isolated files and fail-closed Docker probes."""

import fcntl
import hashlib
import json
import os
import re
from pathlib import Path
import shlex
import subprocess
import tempfile
import textwrap
import unittest
import importlib.util
import sys
from unittest.mock import patch

CONTAINER_ID = "c" * 64
STARTED_AT = "2026-10-13T00:00:00Z"
BINARY_SHA = "d" * 64


def runtime_report():
    return {"success": True, "data": {
        "schema_version": "1", "report_version": "1", "status": "PASS",
        "instance_id": "6f6ebdbe-9e74-4d25-b608-64eecbe0c0b1",
        "container_id": CONTAINER_ID, "started_at": STARTED_AT,
        "build_identity": "sha256:" + BINARY_SHA, "config_generation": "sha256:" + "e" * 64,
        "config_digest": "sha256:" + hashlib.sha256(b'{"online_payment_enabled":false,"store_enabled":false,"billing_mode":"enforce","referral_enabled":false,"pool_enabled":false}').hexdigest(),
        "approval_view_digest": "sha256:" + "f" * 64,
        "online_payment_enabled": False, "store_enabled": False, "billing_mode": "enforce",
        "referral_enabled": False, "pool_enabled": False, "approval_schema": 1,
        "catalog_version": 1, "effective_product_count": 0,
        "product_view_digest": "sha256:" + "f" * 64,
        "generated_at": "fresh",
    }}


SOURCE = Path(os.environ.get("DEPLOY_SOURCE", Path(__file__).resolve().parents[1] / "production-deploy.sh"))
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
        self.probe_token = self.root / "probe-session"
        self.probe_token.write_text("fixture.header.signature")
        self.probe_token.chmod(0o600)
        self.probe_owner = self.root / "probe-owner.json"
        self.probe_owner.write_text(json.dumps({"uid": 0, "gid": 0}))
        with self.config.open("a") as f:
            f.write("\nDEPLOY_RUNTIME_PROBE_TOKEN_FILE=" + shlex.quote(str(self.probe_token)) + "\n")
        # Stub only HTTP transport. The actual shell, Python contract validator,
        # secure credential reader and Docker identity checks still execute.
        (self.root / "sitecustomize.py").write_text(r'''
import datetime, json, os, urllib.request
class Response:
    status = 200
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def read(self, limit):
        body = json.loads(os.environ['RUNTIME_REPORT'])
        data = body.get('data', {})
        if data.get('generated_at') == 'fresh':
            data['generated_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        failure = os.environ.get('TEST_FAILURE', '')
        if failure == 'probe-missing': data.pop('schema_version', None)
        if failure == 'probe-stale': data['generated_at'] = '2000-01-01T00:00:00Z'
        if failure == 'probe-build': data['build_identity'] = 'sha256:' + '0'*64
        if failure == 'probe-config': data['store_enabled'] = True
        if failure == 'probe-approval': body = {'success':False, 'code':'APPROVAL_VIEW_INVALID'}
        root = os.environ.get('TEST_ROOT')
        if root and os.path.exists(root+'/replaced') and not os.path.exists(root+'/rolled-back'):
            data['instance_id'] = '7f6ebdbe-9e74-4d25-b608-64eecbe0c0b2'
            data['container_id'] = 'a'*64
            if failure == 'probe-post-config': data['config_generation'] = 'sha256:'+'0'*64
            if failure == 'probe-post-approval': data['approval_view_digest'] = 'sha256:'+'0'*64
            if failure == 'probe-post-products': data['product_view_digest'] = 'sha256:'+'0'*64
        if root and os.path.exists(root+'/restarted'):
            data['instance_id'] = '7f6ebdbe-9e74-4d25-b608-64eecbe0c0b2'
            data['started_at'] = '2026-10-13T01:00:00Z'
            if failure == 'restart-config': data['config_generation'] = 'sha256:'+'0'*64
        return json.dumps(body).encode()[:limit]
class Opener:
    def open(self, request, timeout):
        if os.environ.get('TEST_FAILURE') == 'probe-unavailable': raise OSError('fixture unavailable')
        if request.full_url != 'http://127.0.0.1:3000/api/clearapi/runtime-integrity': raise OSError('forbidden URL')
        if request.get_header('Authorization') != 'Bearer fixture.header.signature': raise OSError('bad fixture credential')
        return Response()
urllib.request.build_opener = lambda *args: Opener()
''')
        # Production uses Python isolated mode for credential handling. Inject
        # the fixture HTTP transport only through a disposable interpreter shim,
        # rather than weakening -I or relying on inherited PYTHONPATH in the gate.
        python = self.root / "python3"
        python.write_text("#!/usr/bin/python3\n" + r'''
import json, os, pathlib, sys
args = sys.argv[1:]
if '-I' in args and '-' in args:
    code = sys.stdin.read()
    fixture = pathlib.Path(__file__).with_name('sitecustomize.py').read_text()
    sys.argv = ['-', *args[args.index('-')+1:]]
    # Only this disposable test interpreter simulates a root-owned credential.
    # Keep real type, mode, link-count and O_NOFOLLOW checks. The shipped gate
    # is unmodified and never reads this fixture or installs these hooks.
    token = pathlib.Path(__file__).with_name('probe-session')
    owner = json.loads(token.with_name('probe-owner.json').read_text())
    trusted_dirs = set(token.parents)
    real_lstat, real_fstat = os.lstat, os.fstat
    def fixture_owner(value):
        fields = list(value)
        fields[4], fields[5] = owner['uid'], owner['gid']
        return os.stat_result(fields)
    def fixture_lstat(path):
        value = real_lstat(path)
        return fixture_owner(value) if pathlib.Path(path) in trusted_dirs else value
    def fixture_fstat(fd):
        value = real_fstat(fd)
        return fixture_owner(value) if pathlib.Path(os.readlink('/proc/self/fd/'+str(fd))) == token else value
    os.lstat, os.fstat = fixture_lstat, fixture_fstat
    exec(compile(fixture, '<fixture-http>', 'exec'))
    exec(compile(code, '<actual-gate>', 'exec'))
else:
    os.execv('/usr/bin/python3', ['/usr/bin/python3', *args])
''')
        python.chmod(0o755)
        self.identity_tool = self.root / "runtime-identity-refresh.py"
        self.identity_tool.write_text("import json,os,sys\nif 'TEST_ROOT' in os.environ:\n with open(os.environ['TEST_TRACE'],'a') as f: f.write(json.dumps({'name':'identity-refresh','args':sys.argv[1:]})+'\\n')\nif '--prepare-only' not in sys.argv and (os.environ.get('TEST_FAILURE') == 'identity-refresh' or os.environ.get('TEST_FAILURE') == 'rollback-identity' and os.path.exists(os.environ['TEST_ROOT']+'/rolled-back')): sys.exit(1)\n")
        # Redirect only the fixed operator config path in the test copy.
        self.wrapper = self.root / "wrapper.sh"
        self.wrapper.write_text(SOURCE.read_text().replace(
            "production_path = str(p) == '/opt/clearapi/config/deploy.env'",
            "production_path = False # isolated non-production fixture"
        ).replace(
            "/opt/clearapi/config/deploy.env", str(self.config)
        ).replace("/opt/clearapi/bin/runtime-identity-refresh.py", str(self.identity_tool)).replace("/run/lock", str(self.root / "lock")))
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
elif args[:3] == ["docker", "inspect", "--format"] and args[3] == '{{.Id}} {{.State.StartedAt}} {{.Image}}':
    print('c'*64+' 2026-10-13T00:00:00Z sha256:running-local-id')
elif args[:2] == ['docker', 'run'] and '--entrypoint' in args and args[args.index('--entrypoint')+1] == 'sha256sum':
    print('d'*64+'  /new-api')
elif args[:2] == ['docker', 'port']: print('127.0.0.1:3000')
else:
    sys.exit("Forbidden operation: " + repr(args))
''')
        fake_sudo.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                        TEST_TRACE=str(self.trace),
                        RUNTIME_REPORT=json.dumps(runtime_report()))

    def run_wrapper(self, *args):
        before = {p.name: p.read_bytes() for p in self.root.iterdir()
                  if p.is_file() and p not in (self.audit, self.trace)}
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
        self.assertIn("Runtime Gate PASS", result.stderr)

    def test_wrapper_reconcile_uses_approved_release_and_runtime_gate(self):
        self.manifest.write_text(json.dumps({"releases":{"current":{
            "new_api_image_id":"sha256:running-local-id", "image_ref":"registry/example@"+DIGEST,
            "image_digest":DIGEST, "candidate_commit":COMMIT, "status":"success"}}}))
        result = self.run_wrapper("--reconcile")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = [json.loads(line) for line in self.trace.read_text().splitlines()]
        self.assertFalse(any("up" in x or "restart" in x or "pull" in x for x in commands))
        self.assertTrue(any("--entrypoint" in x and "sha256sum" in x for x in commands))
        self.assertEqual(json.loads(self.audit.read_text())["event"], "reconcile")

    def test_wrapper_reconcile_rejects_unapproved_digest(self):
        self.manifest.write_text(json.dumps({"releases":{"current":{
            "new_api_image_id":"sha256:running-local-id", "image_ref":"registry/example@sha256:"+"f"*64,
            "image_digest":"sha256:"+"f"*64, "candidate_commit":COMMIT, "status":"success"}}}))
        result = self.run_wrapper("--reconcile")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("approved digest drift", result.stderr)

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
        source = self.wrapper.read_text().split("# Verification mode:")[0]
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
elif a[:4] == ['docker','image','inspect','--format'] and 'revision' in a[4]: print('b'*40)
elif a[:3] == ['docker','inspect','--format'] and a[3] == '{{.Id}} {{.State.StartedAt}} {{.Image}}':
    print('c'*64+' 2026-10-13T00:00:00Z sha256:running-local-id')
elif a[:2] == ['docker','run'] and '--entrypoint' in a and a[a.index('--entrypoint')+1] == 'sha256sum':
    print('d'*64+'  /new-api')
elif a[:2] == ['docker','port']: print('127.0.0.1:3000')
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
    elif "clearapi-commercial-billing" in q: print(f.get("invalid_commercial_billing", "0"))
    elif "status NOT IN" in q:
        print(f.get("invalid_channel_status", "0"))
    elif "count(*) FROM channels" in q: print(f.get("extra_channel", "0"))
    elif "clearapi-commercial-config" in q:
        print(f.get("commercial_config", '1\\t1\\tmodel-a,model-b\\tdefault\\tmodel-a\\tdefault\\ttrue\\n1\\t1\\tmodel-a,model-b\\tdefault\\tmodel-b\\tdefault\\ttrue').encode().decode('unicode_escape'))
    elif "clearapi-commercial-pricing" in q:
        print(f.get("commercial_pricing", 'ModelRatio\\t{"model-a":1,"model-b":2}\\nGroupRatio\\t{"default":1}\\nSelfUseModeEnabled\\tfalse'))
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
                        {"email":False}, {"password_login":False},
                        {"commercial_config":'1\t1\tmodel-a\tdefault\tmodel-b\tdefault\ttrue'},
                        {"commercial_config":'1\t2\tmodel-a\tdefault\tmodel-a\tdefault\ttrue'},
                        {"commercial_pricing":'ModelRatio\t{"model-a":0}\nGroupRatio\t{"default":1}'},
                        {"commercial_pricing":'ModelRatio\t{"other-model":1}\nGroupRatio\t{"default":1}'},
                        {"commercial_pricing":'ModelRatio\tbroken-json'},
                        {"invalid_channel_status":"1"}, {"grace":"10s"},
                        {"signal":"SIGKILL"}, {"timeout":"180"}, {"sql_error":True}):
            with self.subTest(fixture=fixture):
                r = self.run_gate("verify_launch_baseline; verify_stop_policy",
                                  "DEPLOY_LAUNCH_BASELINE=commercial", **fixture)
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("FAIL:", r.stderr)

    def test_commercial_accepts_multiple_dynamic_channels_models_and_groups(self):
        config = (
            "1\t1\tmodel-a,shared-model\tdefault,vip\tmodel-a\tdefault\ttrue\n"
            "1\t1\tmodel-a,shared-model\tdefault,vip\tshared-model\tvip\ttrue\n"
            "7\t1\tshared-model,model-c\tpro\tshared-model\tpro\ttrue\n"
            "7\t1\tshared-model,model-c\tpro\tmodel-c\tpro\ttrue"
        )
        pricing = (
            "ModelRatio\t{\"model-a\":1,\"shared-model\":2,\"model-c\":3}\n"
            "GroupRatio\t{\"default\":1,\"vip\":1.2,\"pro\":0.8}\n"
            "SelfUseModeEnabled\tfalse"
        )
        r = self.run_gate("verify_launch_baseline", "DEPLOY_LAUNCH_BASELINE=commercial\nDEPLOY_EXPECT_CHANNELS=1,2",
                          commercial_config=config, commercial_pricing=pricing)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("dynamic commercial configuration PASS", r.stderr)

    def test_commercial_accounting_accepts_orders_and_blocks_invalid_balances(self):
        r = self.run_gate("verify_commercial_billing", "DEPLOY_LAUNCH_BASELINE=commercial")
        self.assertEqual(r.returncode, 0, r.stderr)
        r = self.run_gate("verify_commercial_billing", "DEPLOY_LAUNCH_BASELINE=commercial", invalid_commercial_billing="1")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("invalid wallet, token or offline-order accounting", r.stderr)

    def test_closed_baseline_rejects_open_state(self):
        r = self.run_gate("verify_launch_baseline", closed="false")
        self.assertEqual(r.returncode, 1, r.stderr)

    def test_commercial_does_not_require_fixed_channel_expectations(self):
        r = self.run_gate("verify_launch_baseline", "DEPLOY_LAUNCH_BASELINE=commercial\nDEPLOY_EXPECT_CHANNELS=1,2")
        self.assertEqual(r.returncode, 0, r.stderr)

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


class RuntimeGateContractTest(unittest.TestCase):
    setUp = WrapperModesTest.setUp
    run_wrapper = WrapperModesTest.run_wrapper

    def test_commercial_catalog_requires_explicit_operator_policy_and_approved_report(self):
        report = runtime_report()
        report['data'].update(store_enabled=True, effective_product_count=3)
        config = {k: report['data'][k] for k in ('online_payment_enabled', 'store_enabled',
                  'billing_mode', 'referral_enabled', 'pool_enabled')}
        report['data']['config_digest'] = 'sha256:' + hashlib.sha256(
            json.dumps(config, separators=(',', ':')).encode()).hexdigest()
        self.env['RUNTIME_REPORT'] = json.dumps(report)
        # Inherited caller values do not select a production policy.
        self.env['DEPLOY_STORE_POLICY'] = 'commercial_catalog'
        result = self.run_wrapper('--verify')
        self.assertNotEqual(result.returncode, 0)
        with self.config.open('a') as f:
            f.write('\nDEPLOY_LAUNCH_BASELINE=commercial\nDEPLOY_STORE_POLICY=commercial_catalog\n')
        result = self.run_wrapper('--verify')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('Runtime Gate PASS', result.stderr)
        for field, value, code in (
            ('online_payment_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('pool_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('billing_mode', 'monitor', 'RUNTIME_CONFIG_MISMATCH'),
            ('referral_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('store_enabled', False, 'RUNTIME_CONFIG_MISMATCH'),
            ('approval_view_digest', 'invalid', 'APPROVAL_VIEW_INVALID'),
            ('effective_product_count', -1, 'PRODUCT_VIEW_INVALID'),
        ):
            with self.subTest(field=field):
                changed = json.loads(json.dumps(report))
                changed['data'][field] = value
                config = {k: changed['data'][k] for k in config}
                changed['data']['config_digest'] = 'sha256:' + hashlib.sha256(
                    json.dumps(config, separators=(',', ':')).encode()).hexdigest()
                self.reject_report(changed, code)

    def test_closed_baseline_and_unknown_operator_store_policy_remain_blocked(self):
        for policy in ('commercial_catalog', 'unknown'):
            with self.subTest(policy=policy):
                original = self.config.read_text()
                with self.config.open('a') as f:
                    f.write('\nDEPLOY_STORE_POLICY=' + policy + '\n')
                result = self.run_wrapper('--verify')
                self.assertEqual(result.returncode, 2, result.stderr)
                self.config.write_text(original)

    def reject_report(self, report, code):
        self.env['RUNTIME_REPORT'] = json.dumps(report)
        result = self.run_wrapper('--verify')
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn('"status": "BLOCKED"', result.stderr)
        self.assertIn('"code": "'+code+'"', result.stderr)
        self.assertEqual(json.loads(self.audit.read_text().splitlines()[-1])['result'], 'blocked')
        self.assertNotIn('fixture.header.signature', result.stdout+result.stderr+self.trace.read_text())

    def test_every_required_field_is_mandatory_and_unknown_versions_block(self):
        for field in runtime_report()['data']:
            with self.subTest(field=field):
                report = runtime_report()
                report['data'].pop(field)
                self.reject_report(report, 'PROBE_UNAVAILABLE')
        for field in ('schema_version', 'report_version'):
            for value in (0, 1, 2, True, '0', '2', None):
                with self.subTest(field=field, value=value):
                    report = runtime_report()
                    report['data'][field] = value
                    self.reject_report(report, 'PROBE_UNAVAILABLE')

    def test_real_producer_contract_and_cross_instance_binding(self):
        if os.environ.get('RUNTIME_PROBE_EXPORT') != '1':
            self.skipTest('real producer artifact not requested')
        report = json.loads(Path('/tmp/clearapi-runtime-v1-report.json').read_text())
        data = report['data']
        self.env['RUNTIME_REPORT'] = json.dumps(report)
        # The test Docker transport independently supplies the deterministic
        # identity contract. Report fields are not used as expectations.
        fake = self.root / 'sudo'
        text = fake.read_text().replace("'c'*64+' 2026-10-13T00:00:00Z sha256:running-local-id'",
            repr(CONTAINER_ID+' '+STARTED_AT+' sha256:running-local-id'))
        text = text.replace("'d'*64+'  /new-api'",repr(BINARY_SHA+'  /new-api'))
        fake.write_text(text)
        data['generated_at'] = 'fresh'
        self.env['RUNTIME_REPORT'] = json.dumps(report)
        result = self.run_wrapper('--verify')
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('Runtime Gate PASS',result.stderr)
        # The producer's fixture A report cannot satisfy independently inspected B.
        fake.write_text(text.replace(CONTAINER_ID,'0'*64))
        self.reject_report(report,'PROBE_UNAVAILABLE')

    def test_stage2b1_response_is_explicitly_incompatible(self):
        report = runtime_report()
        for field in ('schema_version', 'report_version', 'status', 'container_id', 'started_at', 'config_digest', 'approval_view_digest', 'effective_product_count'):
            report['data'].pop(field)
        report['data']['effective_product_digest'] = report['data'].pop('product_view_digest')
        report['data']['probe_timestamp'] = report['data'].pop('generated_at')
        self.reject_report(report, 'PROBE_UNAVAILABLE')

    def test_identity_time_policy_and_product_failures(self):
        cases = [
            ('instance_id', '0'*64, 'PROBE_UNAVAILABLE'),
            ('container_id', '0'*64, 'PROBE_UNAVAILABLE'),
            ('started_at', '2000-01-01T00:00:00Z', 'PROBE_UNAVAILABLE'),
            ('status', 'unknown', 'PROBE_UNAVAILABLE'),
            ('build_identity', 'sha256:'+'0'*64, 'PROBE_UNAVAILABLE'),
            ('generated_at', '2000-01-01T00:00:00Z', 'PROBE_UNAVAILABLE'),
            ('generated_at', '2999-01-01T00:00:00Z', 'PROBE_UNAVAILABLE'),
            ('generated_at', '2026-10-13T00:00:00', 'PROBE_UNAVAILABLE'),
            ('config_generation', 'unknown', 'RUNTIME_CONFIG_MISMATCH'),
            ('config_digest', 'unknown', 'RUNTIME_CONFIG_MISMATCH'),
            ('config_digest', 'sha256:'+'0'*64, 'RUNTIME_CONFIG_MISMATCH'),
            ('approval_view_digest', 'unknown', 'APPROVAL_VIEW_INVALID'),
            ('online_payment_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('store_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('referral_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('pool_enabled', True, 'RUNTIME_CONFIG_MISMATCH'),
            ('billing_mode', 'monitor', 'RUNTIME_CONFIG_MISMATCH'),
            ('approval_schema', 2, 'APPROVAL_VIEW_INVALID'),
            ('catalog_version', 0, 'PRODUCT_VIEW_INVALID'),
            ('product_view_digest', 'unknown', 'PRODUCT_VIEW_INVALID'),
            ('effective_product_count', 1, 'PRODUCT_VIEW_INVALID'),
        ]
        for field, value, code in cases:
            with self.subTest(field=field, value=value):
                report = runtime_report()
                report['data'][field] = value
                self.reject_report(report, code)
        self.reject_report({'success':False, 'code':'APPROVAL_VIEW_INVALID'}, 'APPROVAL_VIEW_INVALID')
        self.reject_report({'success':False, 'code':'unknown-private-fixture-detail'}, 'PROBE_UNAVAILABLE')

    def test_operator_policy_and_credential_security(self):
        self.env['CLEARAPI_STORE_POLICY'] = 'approved'
        result = self.run_wrapper('--verify')
        self.assertEqual(result.returncode, 0, result.stderr)
        with self.config.open('a') as f:
            f.write('\nCLEARAPI_STORE_POLICY=approved\n')
        result = self.run_wrapper('--verify')
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn('REFUSING: DEPLOY_STORE_POLICY', result.stderr)

    def test_unsafe_or_missing_credentials_and_transport_never_fall_back(self):
        for mode in (0o644, 0o666):
            with self.subTest(mode=mode):
                self.probe_token.chmod(mode)
                self.reject_report(runtime_report(), 'PROBE_UNAVAILABLE')
        self.probe_token.chmod(0o600)
        for owner in ({'uid':1001,'gid':0}, {'uid':0,'gid':1001}):
            with self.subTest(owner=owner):
                self.probe_owner.write_text(json.dumps(owner))
                self.reject_report(runtime_report(),'PROBE_UNAVAILABLE')
        self.probe_owner.write_text(json.dumps({'uid':0,'gid':0}))
        self.env['TEST_FAILURE'] = 'probe-unavailable'
        self.reject_report(runtime_report(), 'PROBE_UNAVAILABLE')
        self.env.pop('TEST_FAILURE')
        self.probe_token.unlink()
        self.reject_report(runtime_report(), 'PROBE_UNAVAILABLE')


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
        self.bundle = self.root / "bundle"
        self.bundle.mkdir()
        self.source = self.bundle / "production-deploy.sh"
        self.source.write_bytes(SOURCE.read_bytes())
        self.identity_source = self.bundle / "runtime-identity-refresh.py"
        self.identity_source.write_bytes((SOURCE.parent / "runtime-identity-refresh.py").read_bytes())
        self.artifact_list = self.bundle / "runtime-identity-artifacts.sha256"
        self.artifact_list.write_bytes((SOURCE.parent / "runtime-identity-artifacts.sha256").read_bytes())
        self.identity_target = self.bin / "runtime-identity-refresh.py"
        self.machine = self.root / "machine-id"
        self.machine.write_text("virtual-vps")
        installer = SOURCE.parent / "clearapi-install-approved-script.sh"
        text = installer.read_text().replace("/opt", str(self.root / "opt"))
        text = text.replace("/run/", str(self.root / "run")+"/").replace("/run ", str(self.root / "run")+" ")
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
    elif a[1]=='%u' and str(root/'run/lock') == a[2] and os.environ.get('LOCK_OWNER'):
        value=os.environ['LOCK_OWNER']
    elif a[1]=='%u': value='0'
    print(value); sys.exit(0)
if name=='install': os.execv('/usr/bin/install',['install','-m','0750',a[-2],a[-1]])
if name=='sudo':
    count=root/'sudo-count'; n=int(count.read_text())+1 if count.exists() else 1; count.write_text(str(n))
    if n==2 and failure in ('post','restore'): sys.exit(1)
    os.execv('/usr/bin/sha256sum',['sha256sum',a[-1]])
if name=='mv':
    if failure=='helper-move' and a[-2].endswith('runtime-identity.new.py'): sys.exit(1)
    if failure=='wrapper-move' and a[-2].endswith('production-deploy.new.sh'): sys.exit(1)
    if 'restore.sh' in a[-2]:
        with open(root/'restore-lock','w') as f:
            probe=subprocess.run(['/usr/bin/flock','-n',str(root/'run/lock/clearapi-production-deploy.lock'),'true'])
            f.write(str(probe.returncode))
        if failure=='restore': sys.exit(1)
    os.execv('/usr/bin/mv',['mv',*a])
if name=='python3':
    data=sys.stdin.read()
    if os.environ.get('LOCK_FILE_OWNER') and a[1] == str(root/'run/lock/clearapi-production-deploy.lock'):
        data='import os\n_original_lstat=os.lstat\ndef wrong_owner(path):\n s=list(_original_lstat(path)); s[4]=int(os.environ["LOCK_FILE_OWNER"]); return os.stat_result(s)\nos.lstat=wrong_owner\n'+data
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
                        INSTALL_ROOT=str(self.root), INSTALL_FAILURE="", PYTHONOPTIMIZE="2",
                        LOCK_OWNER="0")

    def run_install(self, failure=""):
        env = dict(self.env, INSTALL_FAILURE=failure)
        return subprocess.run(["bash", str(self.installer), str(self.bundle), INSTALLER_COMMIT],
                              env=env, capture_output=True, text=True, timeout=10)

    def test_lock_directory_accepts_root_0755_and_sticky_1777(self):
        for mode in ("755", "1777"):
            with self.subTest(mode=mode):
                self.target.write_bytes(self.old)
                self.lockdir.chmod(int(mode, 8))
                result = subprocess.run(
                    ["bash", str(self.installer), str(self.bundle), INSTALLER_COMMIT],
                    env=dict(self.env, LOCK_MODE=mode), capture_output=True, text=True, timeout=10,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("INSTALL_PASS", result.stdout)

    def test_lock_directory_rejects_insecure_modes_and_ownership(self):
        for env in (
            {"LOCK_MODE": "777"},
            {"LOCK_MODE": "1775"},
            {"LOCK_OWNER": "1000"},
            {"LOCK_SYMLINK": "1"},
            {"LOCK_TYPE": "file"},
        ):
            with self.subTest(env=env):
                self.target.write_bytes(self.old)
                if "LOCK_MODE" in env:
                    self.lockdir.chmod(int(env["LOCK_MODE"], 8))
                else:
                    self.lockdir.chmod(0o755)
                if "LOCK_SYMLINK" in env:
                    self.lockdir.rename(self.root / "real-lock")
                    self.lockdir.symlink_to(self.root / "real-lock", target_is_directory=True)
                if "LOCK_TYPE" in env:
                    self.lockdir.unlink()
                    self.lockdir.touch()
                result = subprocess.run(
                    ["bash", str(self.installer), str(self.bundle), INSTALLER_COMMIT],
                    env=dict(self.env, **env), capture_output=True, text=True, timeout=10,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.target.read_bytes(), self.old)

    def test_lock_file_must_be_root_owned_regular_single_link_and_private(self):
        for env in (
            {"LOCK_FILE_MODE": "644"},
            {"LOCK_FILE_TYPE": "symlink"},
            {"LOCK_LINKS": "2"},
            {"LOCK_FILE_OWNER": str(os.getuid() + 1)},
        ):
            with self.subTest(env=env):
                lock = self.lockdir / "clearapi-production-deploy.lock"
                if lock.exists() or lock.is_symlink():
                    lock.unlink()
                lock.touch(mode=0o600)
                if "LOCK_FILE_MODE" in env:
                    lock.chmod(0o644)
                if "LOCK_FILE_TYPE" in env:
                    lock.unlink()
                    lock.symlink_to(self.source)
                if "LOCK_LINKS" in env:
                    os.link(lock, self.lockdir / "second-link")
                result = subprocess.run(
                    ["bash", str(self.installer), str(self.bundle), INSTALLER_COMMIT],
                    env=dict(self.env, **env), capture_output=True, text=True, timeout=10,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.target.read_bytes(), self.old)

    def test_normal_install_hash_owner_and_permissions(self):
        result = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("INSTALL_PASS", result.stdout)
        self.assertEqual(self.target.read_bytes(), self.source.read_bytes())
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o750)
        self.assertEqual(self.identity_target.read_bytes(), self.identity_source.read_bytes())
        self.assertEqual(self.identity_target.stat().st_mode & 0o777, 0o750)
        self.assertEqual(self.identity_target.stat().st_uid, os.getuid())
        self.assertEqual(self.identity_target.stat().st_gid, os.getgid())
        self.assertEqual(self.target.stat().st_uid, os.getuid())
        self.assertEqual(self.target.stat().st_gid, os.getgid())
        self.assertEqual(hashlib.sha256(self.target.read_bytes()).hexdigest(), hashlib.sha256(SOURCE.read_bytes()).hexdigest())

    def test_missing_helper_blocks_before_target_changes(self):
        self.identity_source.rename(self.bundle / "missing-helper.py")
        result = self.run_install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing or unsafe required artifact", result.stderr)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertFalse(self.identity_target.exists())
        self.assertFalse(list(self.bin.glob(".script-install.*")))

    def test_artifact_hash_mismatch_blocks_both_targets(self):
        for source in (self.source, self.identity_source):
            with self.subTest(source=source.name):
                original = source.read_bytes()
                source.write_bytes(original+b"\n# tampered fixture\n")
                result = self.run_install()
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.target.read_bytes(), self.old)
                self.assertFalse(self.identity_target.exists())
                self.assertNotIn("INSTALL_PASS", result.stdout)
                source.write_bytes(original)

    def test_caller_cannot_self_approve_tampered_artifact_list(self):
        self.identity_source.write_bytes(b"print('tampered')\n")
        entries = "".join(hashlib.sha256(p.read_bytes()).hexdigest()+"  "+p.name+"\n"
                          for p in (self.source,self.identity_source))
        self.artifact_list.write_text(entries)
        result = self.run_install()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertFalse(self.identity_target.exists())

    def test_existing_helper_replaced_and_restored_as_pair(self):
        old_helper = b"# old reviewed helper\n"
        self.identity_target.write_bytes(old_helper)
        self.identity_target.chmod(0o750)
        result = self.run_install("post")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertEqual(self.identity_target.read_bytes(), old_helper)
        self.assertEqual(self.identity_target.stat().st_mode & 0o777, 0o750)

    def test_helper_move_failure_keeps_original_pair(self):
        result = self.run_install("helper-move")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertFalse(self.identity_target.exists())
        self.assertNotIn("RESTORE_FAILED", result.stderr)
        self.assertNotIn("INSTALL_PASS", result.stdout)

    def test_wrapper_move_failure_restores_helper(self):
        old_helper = b"# existing helper\n"
        self.identity_target.write_bytes(old_helper)
        self.identity_target.chmod(0o750)
        result = self.run_install("wrapper-move")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertEqual(self.identity_target.read_bytes(), old_helper)
        self.assertNotIn("INSTALL_PASS", result.stdout)

    def test_post_replace_failure_restores_inside_lock(self):
        result = self.run_install("post")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("INSTALL_PASS", result.stdout)
        self.assertEqual(self.target.read_bytes(), self.old)
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o750)
        self.assertFalse(self.identity_target.exists())
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
                  legacy=False, compatible=False, migration=False, restart=False):
        workflow = SOURCE.parents[1] / ".github/workflows/deploy-production.yml"
        block = workflow.read_text().split("      - name: Run controlled deploy on VPS\n")[1]
        block = textwrap.dedent(block.split("        run: |\n", 1)[1].split(
            "\n      - name:", 1)[0])
        for expression, value in {"inputs.image_digest": DIGEST,
                                  "inputs.expected_commit": COMMIT,
                                  "inputs.migration_compatible": "true" if compatible else "false",
                                  "github.run_id": "123"}.items():
            block = block.replace("${{ " + expression + " }}", value)
        if restart:
            block = block.replace("--ghcr-token-stdin", "--ghcr-token-stdin --restart")
            with self.config.open("a") as f:
                f.write("\nDEPLOY_IMAGE_BASE=registry/example\n")
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
def publish_backup(base, fail):
    """Mirror production-backup.sh: artifact first, manifest last."""
    import datetime
    base=pathlib.Path(base); (base/'manifests').mkdir(parents=True,exist_ok=True)
    ident=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    blob=base/'encrypted'/ident/('clearapi-backup-'+ident+'.tar.gz.gpg')
    blob.parent.mkdir(parents=True,exist_ok=True)
    raw=b'non-secret encrypted fixture'
    if fail=='backup-incomplete':
        # Artifact exists but is truncated relative to the manifest digest.
        blob.write_bytes(raw+b'')
    else:
        blob.write_bytes(raw)
    manifest={'backup_schema_version':2,'backup_id':ident,
              'created_at_utc':datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
              'backup_identity_schema_version':1,
              'running_identity':{'oci_revision':'b'*40,'docker_image_id':'sha256:'+'d'*64,
                                  'ghcr_repo_digest':'registry/example@sha256:'+'a'*64,'container_id':'c'*64},
              'artifacts':{'database_dump':{'name':'dump','sha256':'e'*64}},
              'encryption':{'encrypted_artifact':{
                  'name':'clearapi-backup-'+ident+'.tar.gz.gpg',
                  'bytes':len(raw) if fail!='backup-incomplete' else len(raw)+4096,
                  'sha256':hashlib.sha256(raw).hexdigest() if fail!='backup-incomplete' else 'f'*64}}}
    if fail=='backup-manifest-corrupt':
        manifest['artifacts']={}          # no dump digest -> incomplete record
    target=base/'manifests'/(ident+'.json')
    target.write_text(json.dumps(manifest)); target.chmod(0o640)
    if fail=='backup-staging-only':
        # Only staging debris; the published manifest must not exist.
        (base/'staging'/ident).mkdir(parents=True,exist_ok=True)
        (base/'staging'/ident/('clearapi-pg-dump-'+ident+'.dump')).write_bytes(b'partial dump')
        target.unlink()
        for leftover in list((base/'encrypted'/ident).glob('*')): leftover.unlink()
        (base/'encrypted'/ident).rmdir()
    if fail=='backup-old-only':        # A stale, older recovery point exists but this deploy produced nothing.
        target.unlink()
        for leftover in list((base/'encrypted'/ident).glob('*')): leftover.unlink()
        (base/'encrypted'/ident).rmdir()
        old=base/'manifests'/'20260101T000000Z.json'
        old.write_text(json.dumps({'backup_schema_version':2,'backup_id':'20260101T000000Z',
            'created_at_utc':'2026-01-01T00:00:00Z','backup_identity_schema_version':1,
            'running_identity':{'oci_revision':'b'*40,'docker_image_id':'sha256:'+'d'*64,
                                'ghcr_repo_digest':'registry/example@sha256:'+'a'*64,'container_id':'c'*64},
            'artifacts':{'database_dump':{'name':'dump','sha256':'e'*64}},
            'encryption':{'encrypted_artifact':{'name':'clearapi-backup-20260101T000000Z.tar.gz.gpg',
                                                'bytes':1,'sha256':'e'*64}}})); old.chmod(0o640)
        oldblob=base/'encrypted'/'20260101T000000Z'/'clearapi-backup-20260101T000000Z.tar.gz.gpg'
        oldblob.parent.mkdir(parents=True,exist_ok=True); oldblob.write_bytes(b'x')
    return ident
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
    # The deploy gate reads ActiveState (pre-flight) and Result/ExecMainStatus
    # (post-run) and only accepts a completed unit plus a published manifest.
    state=os.environ.get('BACKUP_UNIT_STATE','inactive')
    if a[0]=='is-active':
        # The gate asks is-active about the TIMER (must be healthy) and then
        # reads ActiveState about the SERVICE (must be idle) before starting it.
        sys.exit(0 if 'timer' in ' '.join(a) else (0 if state=='active' else 1))
    if a[0]=='start':
        if fail=='backup': sys.exit(1)
        if fail=='backup-contended':
            # Lock contention: systemd reports a clean start but EX_TEMPFAIL.
            (root/'backup-status').write_text('75'); (root/'backup-result').write_text('exit-code')
            sys.exit(0)
        if fail=='backup-unknown':
            # Unit never really ran: no manifest, no artifact, result unknown.
            (root/'backup-status').write_text('0'); (root/'backup-result').write_text('')
            sys.exit(0)
        publish_backup(root/'backup', fail)
        (root/'backup-status').write_text('0'); (root/'backup-result').write_text('success')
    elif a[0]=='show':
        prop=a[a.index('-p')+1]
        name2={'ExecMainStatus':'backup-status','Result':'backup-result','ActiveState':'backup-state'}.get(prop)
        path2=root/name2 if name2 else None
        print(path2.read_text() if path2 and path2.exists() else (state if prop=='ActiveState' else ''))
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
    if '--entrypoint' in a and a[a.index('--entrypoint')+1]=='sha256sum':
        print('d'*64+'  /new-api'); sys.exit(0)
    if '--entrypoint' in a and a[a.index('--entrypoint')+1]=='/billing-migrate': sys.exit(1)
elif a[0]=='inspect':
    if a[2]=='{{.Id}} {{.State.StartedAt}} {{.Image}}':
        replaced=(root/'replaced').exists() and not (root/'rolled-back').exists()
        print(('a' if replaced else 'c')*64+(' 2026-10-13T01:00:00Z ' if (root/'restarted').exists() else ' 2026-10-13T00:00:00Z ')+('sha256:candidate-local-id' if replaced else 'sha256:running-local-id'))
    elif 'NetworkSettings' in a[2]: print('clearapi_internal')
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
        elif 'clearapi-commercial-config' in q: print(('1\\t2\\tmodel-a\\tdefault\\tmodel-a\\tdefault\\ttrue' if fail=='provider' else '1\\t1\\tmodel-a,model-b\\tdefault\\tmodel-a\\tdefault\\ttrue\\n1\\t1\\tmodel-a,model-b\\tdefault\\tmodel-b\\tdefault\\ttrue').encode().decode('unicode_escape'))
        elif 'clearapi-commercial-pricing' in q: print('ModelRatio\\t{"model-a":1,"model-b":2}\\nGroupRatio\\t{"default":1}\\nSelfUseModeEnabled\\tfalse'.encode().decode('unicode_escape'))
        elif 'status NOT IN' in q: print('0')
        elif 'id=1 AND status=1' in q: print('0' if fail=='provider' else '1')
        elif 'FROM abilities' in q: print('0' if 'NOT' in q else '1')
        elif 'DisableOnlinePayments' in q: print('2')
        elif 'count(*)' in q: print('0')
        elif 'SELECT status' in q: print('2')
        elif 'needs_review' in q: print('4/10/4\n41/93/41')
        elif 'SELECT value' in q: print('enforce')
        else: sys.exit('Forbidden query')
    elif 'restart' in a: (root/'restarted').touch()
    elif 'up' in a:
        if (root/'replaced').exists(): (root/'rolled-back').touch()
        else:
            (root/'replaced').touch()
            if fail in ('partial', 'rollback-identity'): sys.exit(1)
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
                   POOL_MODE="off", DEPLOY_WRAPPER="/opt/clearapi/bin/production-deploy.sh",
                   IMAGE_DIGEST=DIGEST, EXPECTED_COMMIT=COMMIT, GITHUB_RUN_ID="123",
                   MIGRATION_COMPATIBLE="true" if compatible else "false",
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
        self.assertTrue(any("clearapi-commercial-config" in q for q in queries))
        self.assertTrue(any("SELECT status FROM channels WHERE id=2" in q for q in queries))
        self.assertFalse(any("SELECT status FROM channels WHERE id=1;" in q for q in queries))
        self.assertTrue(any(x["args"][-5:] == ["up","-d","--timeout","150","new-api"] for x in trace))

    def test_runtime_blocks_prevent_migration_replace_and_success(self):
        cases = [('probe-unavailable','PROBE_UNAVAILABLE'),
                 ('probe-missing','PROBE_UNAVAILABLE'), ('probe-stale','PROBE_UNAVAILABLE'),
                 ('probe-build','PROBE_UNAVAILABLE'), ('probe-config','RUNTIME_CONFIG_MISMATCH'),
                 ('probe-approval','APPROVAL_VIEW_INVALID')]
        for index, (failure, code) in enumerate(cases):
            with self.subTest(failure=failure):
                if index: self.setUp()
                result, trace, logins = self.run_chain(failure=failure, migration=True)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn('"code": "'+code+'"', result.stderr)
                self.assertFalse(logins)
                self.assertFalse((self.root/'replaced').exists())
                self.assertFalse(any(x['name']=='docker' and x['args'][0]=='compose' and 'run' in x['args'] for x in trace))
                self.assertNotIn('DEPLOYMENT SUCCESS', result.stderr)
                manifest = json.loads(self.manifest.read_text())
                self.assertEqual(manifest['releases']['current']['new_api_image_id'], 'sha256:running-local-id')
                self.assertNotEqual(manifest['releases']['current'].get('status'), 'success')

    def test_post_replace_drift_never_marks_success(self):
        for index, (failure, code) in enumerate([
                ('probe-post-config','RUNTIME_CONFIG_MISMATCH'),
                ('probe-post-approval','APPROVAL_VIEW_INVALID'),
                ('probe-post-products','PRODUCT_VIEW_INVALID')]):
            with self.subTest(failure=failure):
                if index: self.setUp()
                result, trace, _ = self.run_chain(failure=failure)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn('"code": "'+code+'"', result.stderr)
                self.assertTrue((self.root/'replaced').exists())
                self.assertNotEqual(json.loads(self.manifest.read_text())['releases']['current'].get('status'), 'success')
                self.assertNotIn('DEPLOYMENT SUCCESS', result.stderr)
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
        self.assertIn("invalid commercial channel/model/group configuration", result.stderr)

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
        self.assertTrue(any(x["name"] == "identity-refresh" and "--container" in x["args"] for x in trace))
        compose_configs = [x for x in trace if x["name"] == "docker" and
                           x["args"][0] == "compose" and "config" in x["args"]]
        image_probes = [x for x in trace if x["name"] == "docker" and
                        x["args"][0:3] == ["image", "inspect", "--format"]]
        self.assertGreaterEqual(len(compose_configs), 2)
        self.assertGreaterEqual(len(image_probes), 2)

    def test_deploy_refreshes_after_replace(self):
        result, trace, _ = self.run_chain()
        self.assertEqual(result.returncode, 0, result.stderr)
        replace = next(i for i, x in enumerate(trace) if x["name"] == "docker" and "up" in x["args"])
        refresh = next(i for i, x in enumerate(trace) if x["name"] == "identity-refresh" and "--container" in x["args"])
        self.assertLess(replace, refresh)

    def test_refresh_failure_blocks_success(self):
        result, trace, _ = self.run_chain(failure="identity-refresh")
        self.assertEqual(result.returncode, 1)
        self.assertNotEqual(json.loads(self.manifest.read_text())["releases"]["current"]["status"], "success")

    def test_controlled_restart_refreshes_without_pull_or_migration(self):
        result, trace, _ = self.run_chain(restart=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.root / "restarted").exists())
        self.assertTrue(any(x["name"] == "identity-refresh" and "--container" in x["args"] for x in trace))
        self.assertFalse(any(x["name"] == "docker" and ("pull" in x["args"] or "run" in x["args"] and "/billing-migrate" in x["args"]) for x in trace))

    def test_restart_keeps_runtime_config_gate(self):
        result, trace, _ = self.run_chain(restart=True, failure="restart-config")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RUNTIME_CONFIG_MISMATCH", result.stderr)

    def test_rollback_refresh_failure_never_reports_recovery(self):
        result, trace, _ = self.run_chain(failure="rollback-identity", compatible=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.root / "rolled-back").exists())
        self.assertIn("ROLLBACK_FAILED", result.stderr)
        self.assertNotIn("rollback complete", result.stderr)
        self.assertNotIn("PRODUCTION DEPLOY COMPLETED", result.stderr)
        self.assertEqual(json.loads(self.manifest.read_text())["releases"]["current"]["status"], "pending")

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

    def test_completed_backup_is_required_before_any_pull_or_replace(self):
        result, trace, logins = self.run_chain()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("step 4: pre-deploy backup command completed", result.stderr)
        self.assertIn("step 5: backup", result.stderr)
        names = [x["args"][0] for x in trace if x["name"] == "docker"]
        self.assertIn("pull", names)
        self.assertNotIn("up", [x["args"][0] for x in trace if x["name"] == "docker" and "up" in x["args"]])
        self.assertTrue((self.root / "replaced").exists())

    def test_only_staging_files_do_not_satisfy_the_backup_gate(self):
        result, trace, logins = self.run_chain(failure="backup-staging-only")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("no verified completed backup", result.stderr)
        self.assertFalse(logins)
        self.assertFalse((self.root / "replaced").exists())

    def test_incomplete_encrypted_artifact_blocks_the_deploy(self):
        result, trace, logins = self.run_chain(failure="backup-incomplete")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("no verified completed backup", result.stderr)
        self.assertFalse(logins)
        self.assertFalse((self.root / "replaced").exists())

    def test_manifest_without_integrity_fields_blocks_the_deploy(self):
        result, trace, logins = self.run_chain(failure="backup-manifest-corrupt")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("no verified completed backup", result.stderr)
        self.assertFalse(logins)

    def test_older_completed_backup_does_not_authorize_this_deploy(self):
        result, trace, logins = self.run_chain(failure="backup-old-only")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("no verified completed backup", result.stderr)
        self.assertFalse(logins)
        self.assertFalse((self.root / "replaced").exists())

    def test_lock_contention_is_not_reported_as_success(self):
        result, trace, logins = self.run_chain(failure="backup-contended")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("ExecMainStatus=75", result.stderr)
        self.assertIn("lock contention", result.stderr)
        self.assertFalse(logins)
        self.assertFalse((self.root / "replaced").exists())

    def test_unknown_backup_state_blocks_before_the_deploy_runs(self):
        result, trace, logins = self.run_chain(failure="backup-unknown")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("is not a completed backup", result.stderr)
        self.assertFalse(logins)
        self.assertFalse((self.root / "replaced").exists())

    def test_in_flight_backup_unit_blocks_the_deploy(self):
        self.env["BACKUP_UNIT_STATE"] = "activating"
        result, trace, logins = self.run_chain()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("backup result is UNKNOWN", result.stderr)
        self.assertFalse(logins)
        self.assertFalse((self.root / "replaced").exists())



class RuntimeIdentityTest(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("identity_refresh", SOURCE.parent / "runtime-identity-refresh.py")
        self.tool = importlib.util.module_from_spec(spec)
        sys.dont_write_bytecode = True
        spec.loader.exec_module(self.tool)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.ref = "registry/example@" + DIGEST
        self.current = {"Id": "c"*64, "Image": "sha256:"+"d"*64,
                        "State": {"Running": True, "Restarting": False, "StartedAt": "2026-01-01T00:00:00Z"},
                        "Config": {"Image": self.ref},
                        "Mounts": [{"Type": "bind", "Source": str(self.directory), "Destination": "/run/clearapi", "RW": False}]}
        self.image = {"Id": self.current["Image"], "RepoDigests": [self.ref],
                      "Config": {"Labels": {"org.opencontainers.image.revision": COMMIT}}}
        def docker(*args):
            return [self.image if args[0] == "image" else self.current]
        self.addCleanup(patch.stopall)
        patch.object(self.tool, "docker_json", side_effect=docker).start()
        patch.object(self.tool.os, "geteuid", return_value=0).start()
        patch.object(self.tool, "secure_directory", return_value=self.directory).start()
        patch.object(self.tool.os, "fchown").start()
        self.identity_owner = (0, 0)
        # Mock the consumer's metadata boundary, not pathlib's version-dependent
        # os.stat/os.lstat implementation. Preserve real mode/type/link counts.
        original_lstat = Path.lstat
        def root_fixture(path, *args, **kwargs):
            info = original_lstat(path, *args, **kwargs)
            if Path(path) == self.directory / "runtime-identity.json":
                values = list(info)
                values[4], values[5] = self.identity_owner
                return os.stat_result(values)
            return info
        patch.object(Path, "lstat", new=root_fixture).start()

    def refresh(self, check=False):
        self.tool.refresh("app", self.ref, COMMIT, self.directory, check)

    def test_publish_permissions_and_check(self):
        self.refresh()
        p = self.directory / "runtime-identity.json"
        self.assertEqual(p.stat().st_mode & 0o777, 0o400)
        identity = json.loads(p.read_text())
        self.assertEqual(identity["container_id"], self.current["Id"])
        self.assertEqual(identity["image_digest"], DIGEST)
        self.assertEqual(identity["oci_revision"], COMMIT)
        self.refresh(check=True)

    def test_unsafe_owner_rejected_without_repair(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        before = path.read_bytes()
        for owner in ((1001, 0), (0, 1001), (1001, 1001)):
            with self.subTest(owner=owner):
                self.identity_owner = owner
                with self.assertRaisesRegex(self.tool.IdentityError, "UNSAFE_IDENTITY_FILE"):
                    self.refresh(check=True)
                with self.assertRaisesRegex(self.tool.IdentityError, "UNSAFE_IDENTITY_FILE"):
                    self.tool.reconcile("app", self.ref, COMMIT, self.directory)
                self.assertEqual(path.read_bytes(), before)

    def test_unsafe_mode_rejected(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        path.chmod(0o600)
        with self.assertRaisesRegex(self.tool.IdentityError, "UNSAFE_IDENTITY_FILE"):
            self.tool.reconcile("app", self.ref, COMMIT, self.directory)
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_hardlinked_identity_rejected(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        os.link(path, self.directory / "second-identity-link")
        with self.assertRaisesRegex(self.tool.IdentityError, "UNSAFE_IDENTITY_FILE"):
            self.tool.reconcile("app", self.ref, COMMIT, self.directory)
        self.assertEqual(path.stat().st_nlink, 2)

    def test_nonregular_identity_rejected(self):
        (self.directory / "runtime-identity.json").mkdir()
        with self.assertRaisesRegex(self.tool.IdentityError, "UNSAFE_IDENTITY_FILE"):
            self.tool.reconcile("app", self.ref, COMMIT, self.directory)

    def test_restart_stale_then_refresh(self):
        self.refresh()
        self.current["State"]["StartedAt"] = "2026-01-02T00:00:00Z"
        with self.assertRaisesRegex(self.tool.IdentityError, "STALE_IDENTITY"):
            self.refresh(check=True)
        self.refresh()
        self.refresh(check=True)

    def test_recreate_and_rollback_stale(self):
        self.refresh()
        self.current["Id"] = "e"*64
        with self.assertRaisesRegex(self.tool.IdentityError, "STALE_IDENTITY"):
            self.refresh(check=True)
        self.refresh()
        self.current["Id"] = "f"*64
        self.current["State"]["StartedAt"] = "2026-01-03T00:00:00Z"
        self.refresh()
        self.refresh(check=True)

    def test_automatic_restart_blocks_until_host_refresh(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        before = path.read_bytes()
        self.current["State"]["Running"] = False
        with self.assertRaisesRegex(self.tool.IdentityError, "CONTAINER_NOT_RUNNING"):
            self.refresh()
        self.assertEqual(path.read_bytes(), before)
        self.current["State"]["Running"] = True
        self.current["State"]["StartedAt"] = "2026-01-04T00:00:00Z"
        with self.assertRaisesRegex(self.tool.IdentityError, "STALE_IDENTITY"):
            self.refresh(check=True)
        self.assertEqual(path.read_bytes(), before)
        # There is deliberately no automatic restart watcher in this implementation.
        self.refresh()
        self.refresh(check=True)

    def test_rollback_uses_restored_image_and_new_lifecycle(self):
        old_ref, old_image, old_revision = self.ref, self.current["Image"], COMMIT
        self.refresh()
        self.ref = "registry/example@sha256:" + "e"*64
        self.current["Id"] = "e"*64
        self.current["Image"] = "sha256:" + "f"*64
        self.current["Config"]["Image"] = self.ref
        self.current["State"]["StartedAt"] = "2026-01-02T00:00:00Z"
        self.image = {"Id": self.current["Image"], "RepoDigests": [self.ref],
                      "Config": {"Labels": {"org.opencontainers.image.revision": "e"*40}}}
        self.tool.refresh("app", self.ref, "e"*40, self.directory)
        candidate_identity = (self.directory / "runtime-identity.json").read_bytes()
        candidate_ref = self.ref
        self.ref = old_ref
        self.current["Id"] = "f"*64
        self.current["Image"] = old_image
        self.current["Config"]["Image"] = old_ref
        self.current["State"]["StartedAt"] = "2026-01-03T00:00:00Z"
        self.image = {"Id": old_image, "RepoDigests": [old_ref],
                      "Config": {"Labels": {"org.opencontainers.image.revision": old_revision}}}
        with self.assertRaisesRegex(self.tool.IdentityError, "IMAGE_MISMATCH"):
            self.tool.reconcile("app", candidate_ref, "e"*40, self.directory)
        self.assertEqual((self.directory / "runtime-identity.json").read_bytes(), candidate_identity)
        with self.assertRaisesRegex(self.tool.IdentityError, "STALE_IDENTITY"):
            self.refresh(check=True)
        self.assertEqual(self.tool.reconcile("app", self.ref, COMMIT, self.directory), "repaired")
        identity = json.loads((self.directory / "runtime-identity.json").read_text())
        self.assertEqual(identity["image_digest"], DIGEST)
        self.assertEqual(identity["oci_revision"], old_revision)
        self.assertEqual(identity["container_id"], "f"*64)
        self.assertEqual(identity["started_at"], "2026-01-03T00:00:00Z")
        self.refresh(check=True)

    def test_reconcile_after_docker_restart(self):
        self.refresh()
        self.current["State"]["StartedAt"] = "2026-01-05T00:00:00Z"
        result = self.tool.reconcile("app", self.ref, COMMIT, self.directory)
        self.assertEqual(result, "repaired")
        self.refresh(check=True)
        path = self.directory / "runtime-identity.json"
        before = path.read_bytes()
        before_stat = path.stat()
        self.assertEqual(self.tool.reconcile("app", self.ref, COMMIT, self.directory), "unchanged")
        self.assertEqual(path.read_bytes(), before)
        self.assertEqual(path.stat().st_ino, before_stat.st_ino)

    def test_reconcile_repairs_each_stale_identity_field(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        for field, stale in (("container_id","f"*64), ("started_at","2025-01-01T00:00:00Z"),
                             ("image_digest","sha256:"+"f"*64)):
            with self.subTest(field=field):
                stored = json.loads(path.read_text())
                stored[field] = stale
                path.chmod(0o600)
                path.write_text(json.dumps(stored))
                path.chmod(0o400)
                with self.assertRaisesRegex(self.tool.StaleIdentityError, field):
                    self.refresh(check=True)
                self.assertEqual(self.tool.reconcile("app", self.ref, COMMIT, self.directory), "repaired")
                self.refresh(check=True)

    def test_reconcile_repair_failure_blocks_and_preserves_identity(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        before = path.read_bytes()
        self.current["State"]["StartedAt"] = "2026-01-05T00:00:00Z"
        with patch.object(self.tool.os, "replace", side_effect=PermissionError("fixture")):
            with self.assertRaises(PermissionError):
                self.tool.reconcile("app", self.ref, COMMIT, self.directory)
        self.assertEqual(path.read_bytes(), before)
        with self.assertRaises(self.tool.StaleIdentityError):
            self.refresh(check=True)

    def test_reconcile_verification_failure_blocks(self):
        self.refresh()
        self.current["State"]["StartedAt"] = "2026-01-05T00:00:00Z"
        original = self.tool.refresh
        def verify_failure(*args, **kwargs):
            if kwargs.get("check") and verify_failure.repaired:
                raise self.tool.IdentityError("CONTAINER_CHANGED")
            result = original(*args, **kwargs)
            if not kwargs.get("check"):
                verify_failure.repaired = True
            return result
        verify_failure.repaired = False
        with patch.object(self.tool, "refresh", side_effect=verify_failure):
            with self.assertRaisesRegex(self.tool.IdentityError, "CONTAINER_CHANGED"):
                self.tool.reconcile("app", self.ref, COMMIT, self.directory)

    def test_reconcile_never_accepts_live_digest_drift(self):
        self.refresh()
        path = self.directory / "runtime-identity.json"
        before = path.read_bytes()
        self.image["RepoDigests"] = ["registry/example@sha256:"+"f"*64]
        with self.assertRaisesRegex(self.tool.IdentityError, "DIGEST_MISMATCH"):
            self.tool.reconcile("app", self.ref, COMMIT, self.directory)
        self.assertEqual(path.read_bytes(), before)

    def test_reconcile_creates_missing_identity(self):
        self.assertEqual(self.tool.reconcile("app", self.ref, COMMIT, self.directory), "repaired")
        self.refresh(check=True)

    def test_digest_mismatch_does_not_overwrite(self):
        self.refresh()
        p = self.directory / "runtime-identity.json"
        before = p.read_bytes()
        self.image["RepoDigests"] = ["registry/example@sha256:"+"f"*64]
        with self.assertRaisesRegex(self.tool.IdentityError, "DIGEST_MISMATCH"):
            self.refresh()
        self.assertEqual(p.read_bytes(), before)

    def test_revision_mismatch(self):
        self.image["Config"]["Labels"]["org.opencontainers.image.revision"] = "f"*40
        with self.assertRaisesRegex(self.tool.IdentityError, "REVISION_MISMATCH"):
            self.refresh()
        self.assertFalse((self.directory / "runtime-identity.json").exists())

    def test_writable_or_missing_mount_rejected(self):
        self.current["Mounts"][0]["RW"] = True
        with self.assertRaisesRegex(self.tool.IdentityError, "READ_ONLY_MOUNT_REQUIRED"):
            self.refresh()
        self.current["Mounts"] = []
        with self.assertRaisesRegex(self.tool.IdentityError, "READ_ONLY_MOUNT_REQUIRED"):
            self.refresh()

    def test_symlink_not_followed(self):
        target = self.directory / "other"
        target.write_text("untouched")
        (self.directory / "runtime-identity.json").symlink_to(target)
        with self.assertRaisesRegex(self.tool.IdentityError, "UNSAFE_IDENTITY_FILE"):
            self.refresh()
        self.assertEqual(target.read_text(), "untouched")

    def test_changed_container_not_published(self):
        first = self.tool.observe("app", self.ref, COMMIT, self.directory)
        changed = dict(first, started_at="2026-01-02T00:00:00Z")
        with patch.object(self.tool, "observe", side_effect=[first, changed]):
            with self.assertRaisesRegex(self.tool.IdentityError, "CONTAINER_CHANGED"):
                self.refresh()
        self.assertFalse((self.directory / "runtime-identity.json").exists())

    @unittest.skipUnless(os.getuid() == 0, "real permission integration requires Linux root")
    def test_cli_filesystem_integration(self):
        # Actual CLI, Docker subprocess protocol, permissions and atomic publication.
        state = self.directory / "fixture.json"
        state.write_text(json.dumps({"container": self.current, "image": self.image}))
        daemon = self.directory / "docker"
        daemon.write_text("#!/usr/bin/python3\nimport json,os,sys\ns=json.load(open(os.environ['IDENTITY_FIXTURE']))\nprint(json.dumps([s['image' if sys.argv[1]=='image' else 'container']]))\n")
        daemon.chmod(0o755)
        env = dict(os.environ, PATH=str(self.directory)+os.pathsep+os.environ["PATH"], IDENTITY_FIXTURE=str(state))
        cmd = ["/usr/bin/python3", str(SOURCE.parent / "runtime-identity-refresh.py"),
               "--container", "app", "--image-ref", self.ref, "--expected-revision", COMMIT,
               "--directory", str(self.directory)]
        for extra in ([], ["--check"]):
            result = subprocess.run(cmd+extra, env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
        self.current["State"]["StartedAt"] = "2026-01-02T00:00:00Z"
        state.write_text(json.dumps({"container": self.current, "image": self.image}))
        result = subprocess.run(cmd+["--check"], env=env, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        for extra in (["--reconcile"], ["--check"]):
            result = subprocess.run(cmd+extra, env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
        p = self.directory / "runtime-identity.json"
        self.assertEqual((p.stat().st_uid, p.stat().st_gid, p.stat().st_mode & 0o777), (0, 0, 0o400))


if __name__ == "__main__":
    unittest.main()
