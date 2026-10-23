#!/usr/bin/env python3
"""Freeze verified running identity for backups; never pull or change containers."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile


def docker(*args):
    result = subprocess.run(["docker", *args], check=True, capture_output=True, text=True)
    return result.stdout.strip()


def capture(app_dir):
    app = Path(app_dir)
    override = app / "compose-candidate.yaml"
    manifest = app / "release-manifest.json"
    if not override.is_file() or not override.stat().st_size:
        raise ValueError("missing effective Compose override")
    manifest_bytes = manifest.read_bytes()
    current = json.loads(manifest_bytes)["releases"]["current"]
    compose = ("compose", "--project-directory", str(app), "-f",
               str(app / "docker-compose.prod.yml"), "-f", str(override))
    containers = docker(*compose, "ps", "-q", "new-api").splitlines()
    if len(containers) != 1:
        raise ValueError("expected exactly one running new-api container")
    container = json.loads(docker("inspect", containers[0]))[0]
    if not container["State"]["Running"]:
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
    if archive_ref:
        if (current.get('deployment_method') != 'local-build-ssh-archive-v1'
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
    if archive_ref:
        identity.update(image_ref=effective, archive_sha256=current['archive_sha256'],
                        deployment_method=current['deployment_method'], docker_engine_image_id=image_id)
    return identity


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("capture", "check", "finalize"))
    parser.add_argument("--app-dir", default="/opt/clearapi/app")
    parser.add_argument("--snapshot")
    parser.add_argument("--manifest")
    parser.add_argument("--dump-started")
    parser.add_argument("--dump-completed")
    args = parser.parse_args()
    live = capture(args.app_dir)
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
