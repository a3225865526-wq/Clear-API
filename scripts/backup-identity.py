#!/usr/bin/env python3
"""Freeze verified running identity for backups; never pull or change containers."""

import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import os
import stat
import subprocess
import sys
import tarfile
import tempfile


def docker(*args):
    env = None
    if args[0] == 'compose' and '--env-file' in args:
        env = {key: value for key, value in os.environ.items()
               if key in ('PATH', 'HOME', 'LANG') or key.startswith('LC_')}
    result = subprocess.run(["docker", *args], check=True, capture_output=True, text=True, env=env)
    return result.stdout.strip()


def capture(app_dir, stopped_gateway=None):
    app = Path(app_dir)
    override = app / "compose-candidate.yaml"
    manifest = app / "release-manifest.json"
    adopted = app / "local-deploy-state.json"
    state = None
    if adopted.exists() or adopted.is_symlink():
        source = Path(__file__).with_name('local-deploy-state.py')
        if not source.is_file():
            source = Path('/opt/clearapi/bin/local-deploy-state.py')
        spec = importlib.util.spec_from_file_location('local_state', source)
        state = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(state)
        manifest = state.manifest_path(app)
        override = app / 'compose-local.yaml'
    if not override.is_file() or not override.stat().st_size:
        raise ValueError("missing effective Compose override")
    manifest_bytes = manifest.read_bytes()
    current = json.loads(manifest_bytes)["releases"]["current"]
    compose = ("compose", "--project-directory", str(app), "-f",
               str(app / "docker-compose.prod.yml"), "-f", str(override))
    if state:
        if stopped_gateway:
            raise ValueError('legacy stopped-writer mode cannot consume adopted metadata')
        compose = tuple(state.compose_args(app)[1:])
    selection = ("ps", "-a", "-q", "new-api") if stopped_gateway else ("ps", "-q", "new-api")
    containers = docker(*compose, *selection).splitlines()
    if len(containers) != 1:
        raise ValueError("expected exactly one running new-api container")
    container = json.loads(docker("inspect", containers[0]))[0]
    if stopped_gateway:
        if (not re.fullmatch(r"[a-f0-9]{64}", stopped_gateway)
                or container["Id"] != stopped_gateway
                or container["State"].get("Running") is not False
                or container["State"].get("Paused") is not False
                or container["State"].get("Restarting") is not False
                or container["State"].get("Status") != "exited"
                or container["HostConfig"].get("RestartPolicy") != {"Name": "no", "MaximumRetryCount": 0}
                or current.get("status") != "failed"
                or current.get("deployment_attempt", {}).get("status") != "failed"
                or docker("ps", "-q", "--filter", "label=com.docker.compose.project=clearapi",
                          "--filter", "label=com.docker.compose.service=new-api")):
            raise ValueError("stopped failed Gateway or writer isolation mismatch")
    elif not container["State"]["Running"]:
        raise ValueError("container is not running")
    image_id = container["Image"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
        raise ValueError("invalid Docker Image ID")
    image = json.loads(docker("image", "inspect", image_id))[0]
    revision = (image.get("Config", {}).get("Labels") or {}).get(
        "org.opencontainers.image.revision", "")
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("missing or invalid OCI revision")
    effective = json.loads(docker(*compose, "config", "--format", "json"))[
        "services"]["new-api"]["image"]
    override_image = json.loads(docker("compose", "-f", str(override),
                                      "config", "--no-consistency", "--format", "json"))[
        "services"]["new-api"]["image"]
    if override_image != effective:
        raise ValueError("Compose override does not pin the effective image")
    archive_ref = effective == 'clearapi-local:' + revision
    if not archive_ref and not re.fullmatch(r"ghcr.io/a3225865526-wq/clearapi@sha256:[0-9a-f]{64}", effective):
        raise ValueError("Compose image must be an exact approved GHCR reference")
    effective_image = json.loads(docker("image", "inspect", effective))[0]
    if image["Id"] != image_id or effective_image["Id"] != image_id:
        raise ValueError("Compose / running Docker Image ID mismatch")
    if not archive_ref and effective not in image.get("RepoDigests", []):
        raise ValueError("GHCR RepoDigest missing from running image")
    digest = None if archive_ref else effective.split("@", 1)[1]
    canonical_id = image_id
    if archive_ref or state:
        if archive_ref and (current.get('deployment_method') != 'local-build-ssh-archive-v1'
                or not re.fullmatch(r'[0-9a-f]{64}', str(current.get('archive_sha256', '')))
                or current.get('status') != 'success'):
            raise ValueError('archive deployment provenance missing')
        # Docker containerd and classic stores expose different .Id semantics.
        # Freeze the config digest used by the SSH archive deployment manifest.
        with tempfile.TemporaryDirectory(prefix='backup-image-id-') as folder:
            archive = Path(folder) / 'image.tar'
            docker('save', '-o', str(archive), image_id)
            with tarfile.open(archive) as bundle:
                entries = json.load(bundle.extractfile('manifest.json'))
                if len(entries) != 1:
                    raise ValueError('expected one saved image config')
                canonical_id = 'sha256:' + hashlib.sha256(bundle.extractfile(entries[0]['Config']).read()).hexdigest()
    expected = {"new_api_image_id": canonical_id, "candidate_commit": revision,
                "image_ref": effective}
    if not archive_ref:
        expected['image_digest'] = digest
    if any(current.get(key) != value for key, value in expected.items()):
        raise ValueError("release manifest / running identity mismatch")
    # Include the files and container incarnation: a redeploy during a dump
    # invalidates the backup even if it uses the same revision and image.
    identity = {"oci_revision": revision, "docker_image_id": canonical_id,
            "ghcr_repo_digest": None if archive_ref else effective, "ghcr_digest": digest,
            "container_id": container["Id"], "container_created": container["Created"],
            "release_manifest_sha256": hashlib.sha256(manifest_bytes).hexdigest(),
            "compose_base_sha256": hashlib.sha256((app / "docker-compose.prod.yml").read_bytes()).hexdigest(),
            "compose_override_sha256": hashlib.sha256(override.read_bytes()).hexdigest()}
    if state:
        adopted_state = state.load(app)
        identity.update(deployment_state_file=state.NAME,
                        original_incident=adopted_state['adoption']['original_incident'],
                        compose_inputs_sha256=adopted_state['adoption']['compose_sha256'],
                        compose_environment_sha256=adopted_state['adoption']['environment_sha256'])
        identity['docker_engine_image_id']=image_id
    if stopped_gateway:
        identity.update(identity_mode="stopped_failed_gateway",
                        gateway_state={key: container["State"].get(key) for key in
                                       ("Status", "Running", "Paused", "Restarting", "ExitCode", "StartedAt", "FinishedAt")},
                        restart_policy=container["HostConfig"]["RestartPolicy"])
    if archive_ref:
        identity.update(image_ref=effective, archive_sha256=current['archive_sha256'],
                        deployment_method=current['deployment_method'], docker_engine_image_id=image_id)
    return identity


def successor_completion(base, manifest_path, stopped_identity, invocation_started, returncode):
    """Check a finished invocation, never infer completion from a legacy marker.

    Called by the future authorized successor under its deployment lock, using
    its own subprocess result and start time. This is evidence validation only;
    it neither authenticates the caller nor grants production permission.
    """
    if type(returncode) is not int or returncode != 0:
        raise ValueError("backup invocation did not complete successfully")
    if (invocation_started.tzinfo is None
            or stopped_identity.get('identity_mode') != 'stopped_failed_gateway'):
        raise ValueError("exact stopped-writer invocation required")
    path = Path(manifest_path)
    stamp = path.stem
    base = Path(base)
    if (not re.fullmatch(r'\d{8}T\d{6}Z', stamp)
            or path != base / 'manifests' / (stamp + '.json')):
        raise ValueError("backup manifest path mismatch")
    # Reuse the deployment subsystem's secure root-owned reader/publication
    # contract without importing it into this otherwise standalone backup tool.
    for parent in path.parents:
        info = parent.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid())
                or info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX):
            raise ValueError("unsafe backup directory")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        info_manifest = info
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_nlink != 1 or info.st_mode & 0o027):
            raise ValueError("unsafe backup manifest")
        raw = stream.read(1048577)
        read_complete = os.fstat(stream.fileno())
    attributes = ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_nlink', 'st_size', 'st_mtime_ns', 'st_ctime_ns')
    if any(getattr(info_manifest, key) != getattr(read_complete, key) for key in attributes):
        raise ValueError("backup manifest changed while reading")
    if len(raw) > 1048576:
        raise ValueError("oversized backup manifest")
    manifest = json.loads(raw)
    created = datetime.fromisoformat(manifest['created_at_utc'].replace('Z', '+00:00'))
    now = datetime.now(timezone.utc)
    if (created.tzinfo is None or not invocation_started.replace(microsecond=0) <= created <= now
            or manifest.get('backup_id') != stamp
            or manifest.get('backup_identity_schema_version') != 2
            or manifest.get('stopped_writer_identity') != stopped_identity
            or 'running_identity' in manifest
            or manifest.get('pg03_recovery', {}).get('consistency') != 'unchanged_across_dump_and_bundle'
            or any(not re.fullmatch(r'[a-f0-9]{64}', manifest.get('artifacts', {}).get(key, {}).get('sha256', ''))
                   for key in ('database_dump', 'globals', 'config_archive', 'legal_archive', 'edge_archive', 'secrets_archive',
                               'deploy_control_archive', 'pg03_state_archive', 'pg03_state_snapshot'))):
        raise ValueError("backup identity, freshness or PG03 inventory mismatch")
    stage = base / 'staging' / stamp
    staging_parent = stage.parent.lstat()
    if (not stat.S_ISDIR(staging_parent.st_mode) or staging_parent.st_uid != os.geteuid()
            or staging_parent.st_mode & 0o022):
        raise ValueError("unsafe backup staging directory")
    if stage.exists() or stage.is_symlink():
        raise ValueError("backup plaintext staging cleanup incomplete")
    artifact = manifest['encryption']['encrypted_artifact']
    if (artifact.get('name') != 'clearapi-backup-' + stamp + '.tar.gz.gpg'
            or type(artifact.get('bytes')) is not int or artifact['bytes'] <= 0
            or not re.fullmatch(r'[a-f0-9]{64}', artifact.get('sha256', ''))):
        raise ValueError("encrypted artifact identity missing")
    encrypted = base / 'encrypted' / stamp / artifact['name']
    for parent in (encrypted.parent, encrypted.parent.parent):
        info = parent.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_mode & 0o022):
            raise ValueError("unsafe encrypted backup directory")
    fd = os.open(encrypted, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != os.geteuid()
                or before.st_nlink != 1 or stat.S_IMODE(before.st_mode) != 0o600
                or before.st_size != artifact['bytes']):
            raise ValueError("unsafe encrypted artifact")
        actual = hashlib.file_digest(stream, 'sha256').hexdigest()
        after = os.fstat(stream.fileno())
    if (actual != artifact['sha256']
            or any(getattr(before, key) != getattr(after, key) for key in attributes)):
        raise ValueError("backup bytes changed during verification")
    for verified_path, frozen in ((path, info_manifest), (encrypted, before)):
        current = verified_path.lstat()
        if any(getattr(current, key) != getattr(frozen, key) for key in attributes):
            raise ValueError("backup path changed during verification")
    return dict(schema='clearapi.successor-backup-completion/v1', backup_id=stamp,
                manifest_sha256=hashlib.sha256(raw).hexdigest(), encrypted_sha256=actual,
                encrypted_bytes=artifact['bytes'], stopped_writer_identity=stopped_identity,
                invocation_returncode=0, plaintext_cleanup='verified_absent',
                restore_acceptance='not_run', production_execution_allowed=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("capture", "check", "finalize", "archive-paths"))
    parser.add_argument("--app-dir", default="/opt/clearapi/app")
    parser.add_argument("--snapshot")
    parser.add_argument("--manifest")
    parser.add_argument("--dump-started")
    parser.add_argument("--dump-completed")
    parser.add_argument("--stopped-gateway", help="exact failed, exited, restart=no Gateway; separate incident mode")
    parser.add_argument("--archive-root", default="/")
    args = parser.parse_args()
    if args.mode == 'archive-paths':
        app = Path(args.app_dir)
        names = ['docker-compose.prod.yml', 'compose-candidate.yaml']
        if (app / 'local-deploy-state.json').exists():
            # capture validates the state, all original layers and environment first.
            capture(app)
            names += ['compose-pg03.yaml', 'compose-website-recovery-39957948359.json',
                      'compose-local.yaml', 'local-deploy-state.json', 'release-manifest.json',
                      'pg03-target-manifest.json', 'controlled-launch-state.json']
        for name in names:
            print(str((app / name).relative_to(args.archive_root)))
        return
    live = capture(args.app_dir, args.stopped_gateway)
    if args.mode == "capture":
        print(json.dumps(live, sort_keys=True))
        return
    frozen = json.loads(Path(args.snapshot).read_text())
    if frozen != live:
        raise ValueError("running identity changed during backup")
    if args.mode == "finalize":
        path = Path(args.manifest)
        manifest = json.loads(path.read_text())
        manifest.update(candidate_commit=frozen["oci_revision"],
                        new_api_image_id=frozen["docker_image_id"],
                        ghcr_repo_digest=frozen["ghcr_repo_digest"],
                        backup_identity_schema_version=1, running_identity=frozen,
                        legacy_baseline_metadata_fields=["postgres_version", "finance_schema_version",
                                                         "terms_hash", "privacy_hash"],
                        database_recovery_point={
                            "database": manifest["database_name"],
                            "dump_started_at_utc": args.dump_started,
                            "dump_completed_at_utc": args.dump_completed,
                            "dump_sha256": manifest["artifacts"]["database_dump"]["sha256"],
                            "semantics": "pg_dump consistent MVCC snapshot within this interval; not a PITR WAL position"})
        if args.stopped_gateway:
            manifest["backup_identity_schema_version"] = 2
            manifest["stopped_writer_identity"] = manifest.pop("running_identity")
        path.write_text(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        # Do not echo Docker Compose output (it may contain secrets).
        print("backup identity verification failed: " + (
            str(error) if isinstance(error, (ValueError, KeyError)) else type(error).__name__),
            file=sys.stderr)
        sys.exit(1)
