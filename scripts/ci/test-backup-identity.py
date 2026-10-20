#!/usr/bin/env python3
"""Offline backup regression: fake Docker accepts read/dump probes only.

Full backup tests use real tar, SHA256 and GPG with an ephemeral test-only key.
No production socket, credentials, database or container is accessed.
"""
import hashlib
from datetime import datetime, timezone
import io
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import time
import unittest
from unittest.mock import patch


SCRIPTS = Path(__file__).resolve().parents[1]
REVISION = "fb0d30f59d8c6cc17de3f323eeef71029ca8f26e"
IMAGE_ID = "sha256:" + "1" * 64
DIGEST = "sha256:" + "2" * 64  # Intentionally different from Docker Image ID.
REF = "ghcr.io/a3225865526-wq/clearapi@" + DIGEST

FAKE_DOCKER = '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
p = Path(os.environ["TEST_STATE"])
s = json.loads(p.read_text())
a = sys.argv[1:]
with open(os.environ["TEST_TRACE"], "a") as f: f.write(json.dumps(a) + "\\n")
if a[0] == "compose" and (a[-3:] == ["ps", "-q", "new-api"] or a[-4:] == ["ps", "-a", "-q", "new-api"]):
    print("container")
elif a[:2] == ["ps", "-q"]:
    print(s.get("other_writer", ""))
elif a[0] == "compose" and a[-3:] == ["config", "--format", "json"]:
    print(json.dumps({"services": {"new-api": {"image": s["reference"]}}}))
elif a[0] == "compose" and a[-4:] == ["config", "--no-consistency", "--format", "json"]:
    print(json.dumps({"services": {"new-api": {"image": s.get("override_reference", s["reference"])}}}))
elif a == ["inspect", "container"]:
    print(json.dumps([s["container"]]))
elif a[:2] == ["image", "inspect"]:
    if a[2] == s["reference"]: print(json.dumps([s["effective_image"]]))
    elif a[2] == s["container"]["Image"]: print(json.dumps([s["image"]]))
    else: sys.exit("unexpected image")
elif a[:3] == ["exec", "clearapi-postgres-1", "pg_dump"]:
    print("isolated dump fixture")
    if s.get("pg03_drift"):
        Path(s["pg03_drift"]).write_text('{"guard_version":3,"revision":9}')
    if s.get("drift_on_dump"):
        s["container"]["Id"] = "replacement-container"
        p.write_text(json.dumps(s))
elif a[:3] == ["exec", "clearapi-postgres-1", "pg_dumpall"]:
    print("isolated globals fixture")
elif a[:3] == ["exec", "clearapi-postgres-1", "psql"]:
    print("isolated marker hash")
elif a[0] == "run" and "pg_restore" in a and a[-3] == "pg_restore" and a[-2] == "-l":
    print("isolated TOC fixture")
else: sys.exit("Forbidden Docker operation: " + repr(a))
'''


class BackupIdentityTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.app = self.root / "opt/clearapi/app"
        self.app.mkdir(parents=True)
        self.override = self.app / "compose-candidate.yaml"
        self.override.write_text("services:\n  new-api:\n    image: " + REF + "\n")
        (self.app / "docker-compose.prod.yml").write_text("services: {}\n")
        self.manifest = self.app / "release-manifest.json"
        self.current = dict(candidate_commit=REVISION, new_api_image_id=IMAGE_ID,
                            image_digest=DIGEST, image_ref=REF)
        self.write_release()
        self.state = dict(reference=REF,
                          container={"Id": "container", "Image": IMAGE_ID,
                                     "State": {"Running": True}, "Created": "2026-10-08T22:06:32Z"},
                          image={"Id": IMAGE_ID, "RepoDigests": [REF], "Config": {
                              "Labels": {"org.opencontainers.image.revision": REVISION}}},
                          effective_image={"Id": IMAGE_ID})
        self.state_path = self.root / "state.json"
        self.trace = self.root / "trace.jsonl"
        fake = self.root / "docker"
        fake.write_text(FAKE_DOCKER)
        fake.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                        TEST_STATE=str(self.state_path), TEST_TRACE=str(self.trace))

    def write_release(self):
        self.manifest.write_text(json.dumps({"releases": {"current": self.current}}))

    def tool(self, mode="capture", *extra):
        self.state_path.write_text(json.dumps(self.state))
        return subprocess.run(["python3", str(SCRIPTS / "backup-identity.py"), mode,
                               "--app-dir", str(self.app), *extra], env=self.env,
                              capture_output=True, text=True, timeout=10)

    def test_actual_old_release_and_distinct_identity_semantics(self):
        result = self.tool()
        self.assertEqual(result.returncode, 0, result.stderr)
        identity = json.loads(result.stdout)
        self.assertEqual(identity["oci_revision"], REVISION)
        self.assertEqual(identity["docker_image_id"], IMAGE_ID)
        self.assertEqual(identity["ghcr_digest"], DIGEST)
        self.assertNotEqual(identity["docker_image_id"], identity["ghcr_digest"])

    def test_new_running_release_is_automatically_captured(self):
        revision = "a" * 40
        image_id = "sha256:" + "3" * 64
        digest = "sha256:" + "4" * 64
        reference = "ghcr.io/a3225865526-wq/clearapi@" + digest
        self.state["container"]["Image"] = image_id
        self.state["image"].update(Id=image_id, RepoDigests=[reference])
        self.state["effective_image"]["Id"] = image_id
        self.state["reference"] = reference
        self.override.write_text("services:\n  new-api:\n    image: " + reference + "\n")
        self.state["image"]["Config"]["Labels"]["org.opencontainers.image.revision"] = revision
        self.current.update(candidate_commit=revision, new_api_image_id=image_id,
                            image_digest=digest, image_ref=reference)
        self.write_release()
        identity = json.loads(self.tool().stdout)
        self.assertEqual(identity["oci_revision"], revision)
        self.assertEqual(identity["docker_image_id"], image_id)
        self.assertEqual(identity["ghcr_digest"], digest)
        self.full_backup()

    def test_missing_revision_rejected(self):
        self.state["image"]["Config"]["Labels"] = {}
        self.assertNotEqual(self.tool().returncode, 0)

    def test_missing_override_rejected(self):
        self.override.unlink()
        self.assertNotEqual(self.tool().returncode, 0)

    def test_empty_override_rejected(self):
        self.override.write_text("")
        self.assertNotEqual(self.tool().returncode, 0)

    def test_release_commit_mismatch_rejected(self):
        self.current["candidate_commit"] = "a" * 40
        self.write_release()
        self.assertNotEqual(self.tool().returncode, 0)

    def test_release_image_mismatch_rejected(self):
        self.current["new_api_image_id"] = DIGEST
        self.write_release()
        self.assertNotEqual(self.tool().returncode, 0)

    def test_release_digest_mismatch_rejected(self):
        self.current["image_digest"] = IMAGE_ID
        self.write_release()
        self.assertNotEqual(self.tool().returncode, 0)

    def test_override_does_not_pin_effective_image_rejected(self):
        self.state["override_reference"] = None
        self.assertNotEqual(self.tool().returncode, 0)

    def test_compose_image_mismatch_rejected(self):
        self.state["effective_image"]["Id"] = DIGEST
        self.assertNotEqual(self.tool().returncode, 0)

    def test_unverified_repo_digest_rejected(self):
        self.state["image"]["RepoDigests"] = []
        self.assertNotEqual(self.tool().returncode, 0)

    def test_floating_compose_reference_rejected(self):
        self.state["reference"] = "ghcr.io/a3225865526-wq/clearapi:latest"
        self.assertNotEqual(self.tool().returncode, 0)

    def test_container_change_during_backup_rejected(self):
        snapshot = self.root / "snapshot.json"
        snapshot.write_text(self.tool().stdout)
        self.state["container"]["Id"] = "replacement"
        result = self.tool("check", "--snapshot", str(snapshot))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("changed during backup", result.stderr)

    def stopped_gateway(self):
        cid = "f" * 64
        self.state['container'].update(Id=cid,
            State=dict(Status='exited', Running=False, Paused=False, Restarting=False,
                       ExitCode=127, StartedAt='2026-10-20T00:00:00Z', FinishedAt='2026-10-20T00:00:01Z'),
            HostConfig=dict(RestartPolicy=dict(Name='no', MaximumRetryCount=0)))
        self.current.update(status='failed', deployment_attempt=dict(status='failed'))
        self.write_release()
        return cid

    def test_stopped_backup_is_explicit_and_rechecks_writer_isolation(self):
        cid = self.stopped_gateway()
        self.assertNotEqual(self.tool().returncode, 0, 'default backup must still require a running Gateway')
        result = self.tool('capture', '--stopped-gateway', cid)
        self.assertEqual(result.returncode, 0, result.stderr)
        frozen = self.root / 'stopped.json'
        frozen.write_text(result.stdout)
        identity = json.loads(result.stdout)
        self.assertEqual(identity['identity_mode'], 'stopped_failed_gateway')
        self.assertEqual(identity['container_id'], cid)
        self.state['other_writer'] = 'unapproved-running-writer'
        self.assertNotEqual(self.tool('capture', '--stopped-gateway', cid).returncode, 0)
        del self.state['other_writer']
        self.state['container']['State']['FinishedAt'] = '2026-10-21T00:00:00Z'
        self.assertNotEqual(self.tool('check', '--stopped-gateway', cid,
                                     '--snapshot', str(frozen)).returncode, 0)

    def test_stopped_backup_rejects_restart_or_incident_drift(self):
        cid = self.stopped_gateway()
        self.assertNotEqual(self.tool('capture', '--stopped-gateway', 'e' * 64).returncode, 0)
        self.state['container']['HostConfig']['RestartPolicy']['Name'] = 'always'
        self.assertNotEqual(self.tool('capture', '--stopped-gateway', cid).returncode, 0)
        self.state['container']['HostConfig']['RestartPolicy']['Name'] = 'no'
        self.current['status'] = 'success'
        self.write_release()
        self.assertNotEqual(self.tool('capture', '--stopped-gateway', cid).returncode, 0)

    def full_backup(self, drift=False, pg03=False, pg03_drift=False, legacy=False, preserve=False,
                    stopped=False, cleanup_failure=False):
        base = self.root / "opt/clearapi/backups"
        (base / "bin").mkdir(parents=True)
        (base / "manifests").mkdir()
        for part in ("legal", "secrets", "edge", "bin", "config"):
            directory = self.root / "opt/clearapi" / part
            directory.mkdir(exist_ok=True)
            (directory / "fixture").write_text("isolated fixture")
        transition_files = {}
        if legacy:
            for name in ('request', 'authorization', 'plan', 'backup', 'state'):
                relative = 'opt/clearapi/app/legacy-transition-'+name+'.json'
                path = self.root/relative
                path.write_text(json.dumps({'fixture': name}))
                path.chmod(0o600)
                transition_files[relative] = path.read_bytes()
            relative = 'opt/clearapi/logs/legacy-transition.jsonl'
            path = self.root/relative
            path.parent.mkdir()
            path.write_text('{"phase":"passed"}\n')
            path.chmod(0o600)
            transition_files[relative] = path.read_bytes()
        old_id = "20261008T000000Z"
        old_manifest = base / "manifests" / (old_id + ".json")
        old_manifest.write_text('{"candidate_commit":"603b217"}\n')
        old_bundle = base / "encrypted" / old_id / "historical.gpg"
        old_bundle.parent.mkdir(parents=True)
        old_bundle.write_bytes(b"historical encrypted fixture")
        old_bytes = old_manifest.read_bytes(), old_bundle.read_bytes()
        historical = {}
        if preserve:
            self.env['CLEARAPI_BACKUP_PRESERVE_EXISTING'] = 'true'
            for day in range(2, 20):
                stamp = f'202609{day:02d}T000000Z'
                path = base / 'encrypted' / stamp / 'historical.gpg'
                path.parent.mkdir(parents=True)
                path.write_bytes(b'existing encrypted backup')
                historical[path] = path.read_bytes()
        if stopped:
            self.env['CLEARAPI_BACKUP_STOPPED_GATEWAY'] = self.stopped_gateway()
        (self.root / "log").mkdir()
        helper = base / "bin/backup-identity.py"
        helper.write_text((SCRIPTS / "backup-identity.py").read_text().replace(
            "/opt/clearapi/app", str(self.app)))
        original_state = {}
        if pg03:
            state_tool = base / 'bin/pg03-backup-state.py'
            state_tool.write_text((SCRIPTS / 'pg03-backup-state.py').read_text())
            pg03_files = {
                'opt/clearapi/app/compose-pg03.yaml': 'services: {}\n',
                'opt/clearapi/config/pg03/pg03.env': 'synthetic test-only environment\n',
                'data/clearapi/pool-executor/pool/checkpoint.journal': '{"guard_version":3,"revision":8,"checkpoint":"signed-fixture-bytes"}',
                'data/clearapi/pool-executor/pool/admission.json': '{"guard_version":3,"revision":12,"grants":["synthetic-grant"],"capabilities":["synthetic-capability"]}',
                'data/clearapi/pool-executor/receipts/receipt.json': '{"state":"STARTED","id":"synthetic-receipt"}',
                'data/clearapi/pool-executor/auths/account.json': '{"synthetic_credential":true}',
            }
            for name, value in pg03_files.items():
                p = self.root / name
                p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                for parent in p.parents:
                    if parent == self.root: break
                    if 'pool-executor' in str(parent): parent.chmod(0o700)
                p.write_text(value)
                p.chmod(0o644 if name.endswith('yaml') else 0o600)
                original_state[name] = p.read_bytes()
            if pg03_drift:
                self.state['pg03_drift'] = str(self.root / 'data/clearapi/pool-executor/pool/checkpoint.journal')
        gnupg = self.root / "gnupg"
        gnupg.mkdir(mode=0o700)
        env = dict(self.env, GNUPGHOME=str(gnupg))
        def gpg(*args):
            return subprocess.run(["gpg", "--batch", "--pinentry-mode", "loopback",
                                   "--passphrase", "", *args], env=env,
                                  capture_output=True, check=True, timeout=30)
        gpg("--quick-generate-key", "Isolated Backup Test", "ed25519", "cert", "0")
        fingerprint = next(line.split(":")[9] for line in gpg("--with-colons", "--list-keys").stdout.decode().splitlines() if line.startswith("fpr:"))
        gpg("--quick-add-key", fingerprint, "cv25519", "encr", "0")
        source = Path(os.environ.get("BACKUP_SOURCE", SCRIPTS / "production-backup.sh")).read_text()
        source = source.replace("/opt/clearapi", str(self.root / "opt/clearapi"))
        source = source.replace("/data/clearapi", str(self.root / "data/clearapi"))
        source = source.replace('--root / ', '--root ' + str(self.root) + ' ')
        source = source.replace("/var/log/clearapi", str(self.root / "log"))
        source = source.replace("/run/lock/clearapi-backup.lock", str(self.root / "backup.lock"))
        source = source.replace(" -C / ", " -C " + str(self.root) + " ")
        source = source.replace("D675187C7097EF9D8C3BFF966B1439F896043FFD", fingerprint)
        if cleanup_failure:
            source = source.replace('if ! find "$STAGE" -type f -exec shred -u {} +; then rc=1; fi',
                                    'if ! false; then rc=1; fi')
        script = self.root / "backup.sh"
        script.write_text(source)
        self.state["drift_on_dump"] = drift
        self.state_path.write_text(json.dumps(self.state))
        invocation_started = datetime.now(timezone.utc)
        result = subprocess.run(["bash", str(script)], env=env, capture_output=True,
                                text=True, timeout=45)
        self.assertEqual(old_bytes, (old_manifest.read_bytes(), old_bundle.read_bytes()))
        manifests = [p for p in (base / "manifests").glob("*.json") if p != old_manifest]
        if drift or pg03_drift:
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(manifests, [])
            return
        if cleanup_failure:
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(len(manifests), 1, 'legacy manifest can exist despite EXIT cleanup failure')
            spec = importlib.util.spec_from_file_location('backup_identity', SCRIPTS / 'backup-identity.py')
            identity_tool = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(identity_tool)
            frozen = json.loads(manifests[0].read_text())['stopped_writer_identity']
            with self.assertRaisesRegex(ValueError, 'invocation'):
                identity_tool.successor_completion(base, manifests[0], frozen, invocation_started, result.returncode)
            return
        self.assertEqual(result.returncode, 0, result.stderr)
        for path, expected in historical.items():
            self.assertEqual(path.read_bytes(), expected, 'release backup must not prune existing backups')
        self.assertEqual(len(manifests), 1)
        manifest = json.loads(manifests[0].read_text())
        if stopped:
            self.assertEqual(manifest['backup_identity_schema_version'], 2)
            self.assertNotIn('running_identity', manifest)
            self.assertEqual(manifest['stopped_writer_identity']['container_id'], 'f' * 64)
            spec = importlib.util.spec_from_file_location('backup_identity', SCRIPTS / 'backup-identity.py')
            identity_tool = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(identity_tool)
            frozen = manifest['stopped_writer_identity']
            verified = identity_tool.successor_completion(base, manifests[0], frozen,
                                                           invocation_started, result.returncode)
            self.assertEqual(verified['plaintext_cleanup'], 'verified_absent')
            self.assertEqual(verified['restore_acceptance'], 'not_run')
            self.assertIs(verified['production_execution_allowed'], False)
            for code in (None, False, 1, -15):
                with self.assertRaisesRegex(ValueError, 'invocation'):
                    identity_tool.successor_completion(base, manifests[0], frozen, invocation_started, code)
            leftover = base / 'staging' / manifest['backup_id']
            leftover.mkdir()
            with self.assertRaisesRegex(ValueError, 'cleanup'):
                identity_tool.successor_completion(base, manifests[0], frozen, invocation_started, 0)
            leftover.rmdir()
            cipher = base / 'encrypted' / manifest['backup_id'] / manifest['encryption']['encrypted_artifact']['name']
            original_cipher = cipher.read_bytes()
            cipher.write_bytes(original_cipher + b'tamper')
            with self.assertRaisesRegex(ValueError, 'artifact'):
                identity_tool.successor_completion(base, manifests[0], frozen, invocation_started, 0)
            cipher.write_bytes(original_cipher)
            # Hashing an already-open descriptor must not authorize a replaced
            # pathname, or a manifest modified in-place during that hash.
            original_manifest = manifests[0].read_bytes()
            digest_file = identity_tool.hashlib.file_digest
            for target in (manifests[0], cipher):
                def tamper_during_hash(stream, algorithm):
                    value = digest_file(stream, algorithm)
                    if target == manifests[0]:
                        target.write_bytes(original_manifest + b' ')
                    else:
                        replacement = target.with_suffix('.replacement')
                        replacement.write_bytes(original_cipher)
                        replacement.chmod(0o600)
                        replacement.replace(target)
                    return value
                with patch.object(identity_tool.hashlib, 'file_digest', side_effect=tamper_during_hash):
                    with self.assertRaisesRegex(ValueError, 'changed'):
                        identity_tool.successor_completion(base, manifests[0], frozen, invocation_started, 0)
                manifests[0].write_bytes(original_manifest)
                cipher.write_bytes(original_cipher)
            staging = base / 'staging'
            moved_staging = base / 'staging-history'
            staging.rename(moved_staging)
            staging.symlink_to(moved_staging, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, 'staging directory'):
                identity_tool.successor_completion(base, manifests[0], frozen, invocation_started, 0)
            staging.unlink()
            moved_staging.rename(staging)
        self.assertEqual(manifest["candidate_commit"], self.current["candidate_commit"])
        self.assertEqual(manifest["new_api_image_id"], self.current["new_api_image_id"])
        self.assertEqual(manifest["ghcr_repo_digest"], self.current["image_ref"])
        artifact = manifest["encryption"]["encrypted_artifact"]
        encrypted = base / "encrypted" / manifest["backup_id"] / artifact["name"]
        offsite = self.root / "offsite.gpg"
        offsite.write_bytes(encrypted.read_bytes())
        self.assertEqual(hashlib.sha256(offsite.read_bytes()).hexdigest(), artifact["sha256"])
        self.assertEqual(offsite.stat().st_size, artifact["bytes"])
        # Existing restore reader: encryption path/name, decrypt, tar, artifact hashes.
        bundle = gpg("--decrypt", str(offsite)).stdout
        self.assertEqual(hashlib.sha256(bundle).hexdigest(), manifest["bundle"]["sha256"])
        with tarfile.open(fileobj=io.BytesIO(bundle), mode="r:gz") as archive:
            for item in manifest["artifacts"].values():
                self.assertEqual(hashlib.sha256(archive.extractfile(item["name"]).read()).hexdigest(), item["sha256"])
            if legacy:
                deployctl = archive.extractfile(manifest['artifacts']['deploy_control_archive']['name']).read()
                with tarfile.open(fileobj=io.BytesIO(deployctl), mode='r:gz') as control:
                    for name, raw in transition_files.items():
                        self.assertEqual(control.extractfile(name).read(), raw)
            config = archive.extractfile(manifest["artifacts"]["config_archive"]["name"]).read()
            if pg03:
                state_archive = self.root / 'pg03.tar.gz'
                state_archive.write_bytes(archive.extractfile(manifest['artifacts']['pg03_state_archive']['name']).read())
                snapshot = self.root / 'pg03.json'
                snapshot.write_bytes(archive.extractfile(manifest['artifacts']['pg03_state_snapshot']['name']).read())
                state_archive.chmod(0o600)
                snapshot.chmod(0o600)
                restored = self.root / 'isolated-restore'
                result = subprocess.run(['python3', str(state_tool), 'restore',
                    '--archive', str(state_archive), '--snapshot', str(snapshot),
                    '--destination', str(restored)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                for name, expected in original_state.items():
                    self.assertEqual((restored / name).read_bytes(), expected)
                self.assertEqual((restored / 'data/clearapi/pool-executor/pool/admission.json').stat().st_mode & 0o777, 0o600)
                self.assertEqual(manifest['pg03_recovery']['consistency'], 'unchanged_across_dump_and_bundle')
        with tarfile.open(fileobj=io.BytesIO(config), mode="r:gz") as archive:
            self.assertIn("opt/clearapi/app/compose-candidate.yaml", archive.getnames())
        point = manifest["database_recovery_point"]
        self.assertEqual(point["dump_sha256"], manifest["artifacts"]["database_dump"]["sha256"])
        self.assertLessEqual(point["dump_started_at_utc"], point["dump_completed_at_utc"])

    def test_lock_contention_never_publishes_a_backup(self):
        """A skipped run must be a failure, never a silent success."""
        base = self.root / "opt/clearapi/backups"
        (base / "manifests").mkdir(parents=True)
        (base / "bin").mkdir()
        (self.root / "log").mkdir()
        lock = self.root / "backup.lock"
        source = (SCRIPTS / "production-backup.sh").read_text()
        source = source.replace("/opt/clearapi", str(self.root / "opt/clearapi"))
        source = source.replace("/data/clearapi", str(self.root / "data/clearapi"))
        source = source.replace("/var/log/clearapi", str(self.root / "log"))
        source = source.replace("/run/lock/clearapi-backup.lock", str(lock))
        script = self.root / "backup.sh"
        script.write_text(source)
        holder = subprocess.Popen(
            ["flock", str(lock), "sleep", "20"], stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL)
        self.addCleanup(holder.kill)
        for _ in range(100):
            if lock.exists():
                break
            time.sleep(0.05)
        result = subprocess.run(["bash", str(script)], env=self.env, capture_output=True,
                                text=True, timeout=30)
        holder.kill()
        self.assertEqual(result.returncode, 75,
                         "lock contention must exit EX_TEMPFAIL, not 0: " + result.stderr)
        # backup.sh logs to the operator log, not stderr.
        self.assertIn("no backup was produced",
                      (self.root / "log/backup.log").read_text())
        self.assertEqual(list((base / "manifests").glob("*.json")), [],
                         "a skipped run must never publish a completion marker")

    def test_two_concurrent_runs_publish_at_most_one_backup(self):
        """Exactly one exclusive holder; the loser produces no marker."""
        base = self.root / "opt/clearapi/backups"
        (base / "manifests").mkdir(parents=True)
        (base / "bin").mkdir()
        (self.root / "log").mkdir()
        lock = self.root / "backup.lock"
        source = (SCRIPTS / "production-backup.sh").read_text()
        source = source.replace("/opt/clearapi", str(self.root / "opt/clearapi"))
        source = source.replace("/data/clearapi", str(self.root / "data/clearapi"))
        source = source.replace("/var/log/clearapi", str(self.root / "log"))
        source = source.replace("/run/lock/clearapi-backup.lock", str(lock))
        script = self.root / "backup.sh"
        script.write_text(source)
        # Both racers fail later on the absent fixtures; the only observable
        # difference is that neither reports a skipped run as success.
        racers = [subprocess.Popen(["bash", str(script)], env=self.env,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                  for _ in range(2)]
        results = [racer.communicate(timeout=60) for racer in racers]
        codes = sorted(racer.returncode for racer in racers)
        self.assertNotIn(0, codes, "no racer may report a completed backup")
        self.assertEqual(list((base / "manifests").glob("*.json")), [])

    def test_full_encrypted_backup_offsite_and_legacy_restore_reader(self):
        self.full_backup()

    def test_release_backup_preserves_history_beyond_the_retention_limit(self):
        self.full_backup(preserve=True)

    def test_stopped_incident_encrypted_backup_preserves_failed_identity_and_history(self):
        self.full_backup(pg03=True, preserve=True, stopped=True)

    def test_published_legacy_manifest_cannot_hide_backup_exit_cleanup_failure(self):
        self.full_backup(pg03=True, preserve=True, stopped=True, cleanup_failure=True)

    def test_backup_drift_fails_without_publishing_manifest(self):
        self.full_backup(drift=True)

    def test_pg03_encrypted_backup_and_isolated_restore_preserve_all_state(self):
        self.full_backup(pg03=True)

    def test_encrypted_backup_preserves_consumed_maintenance_proof_and_audit(self):
        self.full_backup(pg03=True, legacy=True)

    def test_pg03_change_during_dump_does_not_publish_recovery_point(self):
        self.full_backup(pg03=True, pg03_drift=True)

    def test_restore_rejects_traversal_links_and_existing_runtime(self):
        spec = importlib.util.spec_from_file_location('pg03_state', SCRIPTS / 'pg03-backup-state.py')
        tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(tool)
        for name, link in [('../escape', False), ('data/clearapi/pool-executor/pool/checkpoint.journal', True)]:
            with self.subTest(name=name):
                path=self.root/'unsafe.tar.gz'
                with tarfile.open(path,'w:gz') as archive:
                    member=tarfile.TarInfo(name)
                    if link:
                        member.type=tarfile.SYMTYPE;member.linkname='/etc/passwd'
                    archive.addfile(member)
                snapshot=self.root/'snapshot.json'
                snapshot.write_text(json.dumps({name:{'mode':0o600,'uid':0,'gid':0,'directory':False,'bytes':0,'sha256':hashlib.sha256(b'').hexdigest()}}))
                destination=self.root/'not-created'
                with self.assertRaises(ValueError):tool.restore(path,snapshot,destination)
                self.assertFalse(destination.exists())
        with self.assertRaises(ValueError):tool.restore(path,snapshot,self.root)


if __name__ == "__main__":
    unittest.main(verbosity=2)
