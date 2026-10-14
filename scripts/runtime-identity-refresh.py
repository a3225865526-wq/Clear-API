#!/usr/bin/env python3
"""Publish Docker-observed identity; never start containers or contact providers."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile


class IdentityError(Exception):
    pass


class StaleIdentityError(IdentityError):
    pass


def docker_json(*args):
    result = subprocess.run(["docker", *args], check=True, capture_output=True,
                            text=True, timeout=15)
    return json.loads(result.stdout)


def observe(container, image_ref, revision, directory):
    if not re.fullmatch(r"[^\s@]+@sha256:[a-f0-9]{64}", image_ref):
        raise IdentityError("EXACT_DIGEST_REQUIRED")
    if not re.fullmatch(r"[a-f0-9]{40}", revision):
        raise IdentityError("REVISION_REQUIRED")
    # Request only identity evidence; never retrieve Config.Env or credentials.
    current = docker_json("inspect", "--format",
                          '[{"Id":{{json .Id}},"Image":{{json .Image}},'
                          '"State":{"Running":{{json .State.Running}},'
                          '"Restarting":{{json .State.Restarting}},"StartedAt":{{json .State.StartedAt}}},'
                          '"Config":{"Image":{{json .Config.Image}}},"Mounts":{{json .Mounts}}}]',
                          container)[0]
    mounts = [m for m in current.get("Mounts", []) if m.get("Destination") == "/run/clearapi"]
    if (len(mounts) != 1 or mounts[0].get("Type") != "bind" or mounts[0].get("RW") is not False
            or mounts[0].get("Source") != str(directory)):
        raise IdentityError("READ_ONLY_MOUNT_REQUIRED")
    state = current["State"]
    if not state.get("Running") or state.get("Restarting"):
        raise IdentityError("CONTAINER_NOT_RUNNING")
    if not re.fullmatch(r"[a-f0-9]{64}", current["Id"]):
        raise IdentityError("INVALID_CONTAINER_ID")
    image = docker_json("image", "inspect", "--format",
                        '[{"Id":{{json .Id}},"RepoDigests":{{json .RepoDigests}},'
                        '"Config":{"Labels":{"org.opencontainers.image.revision":'
                        '{{with (index .Config "Labels")}}'
                        '{{json (index . "org.opencontainers.image.revision")}}'
                        '{{else}}null{{end}}}}}]',
                        current["Image"])[0]
    if image["Id"] != current["Image"] or current["Config"]["Image"] != image_ref:
        raise IdentityError("IMAGE_MISMATCH")
    if image_ref not in image.get("RepoDigests", []):
        raise IdentityError("DIGEST_MISMATCH")
    if (image.get("Config", {}).get("Labels") or {}).get("org.opencontainers.image.revision") != revision:
        raise IdentityError("REVISION_MISMATCH")
    started = dt.datetime.fromisoformat(state["StartedAt"].replace("Z", "+00:00"))
    now = dt.datetime.now(dt.timezone.utc)
    if started.tzinfo is None or not dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc) < started <= now:
        raise IdentityError("INVALID_STARTED_AT")
    return {"container_id": current["Id"], "started_at": state["StartedAt"],
            "image_digest": image_ref.rsplit("@", 1)[1], "oci_revision": revision}


def secure_directory(directory, prepare=False):
    directory = Path(directory)
    if not directory.is_absolute():
        raise IdentityError("ABSOLUTE_DIRECTORY_REQUIRED")
    for parent in reversed((directory, *directory.parents)):
        if prepare and not parent.exists() and not parent.is_symlink():
            parent.mkdir(mode=0o750)
        info = parent.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or (info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX)):
            raise IdentityError("UNSAFE_DIRECTORY")
    return directory


def refresh(container, image_ref, revision, directory, check=False):
    if os.geteuid() != 0:
        raise IdentityError("ROOT_REQUIRED")
    directory = secure_directory(directory)
    path = directory / "runtime-identity.json"
    expected = observe(container, image_ref, revision, directory)
    if path.exists() or path.is_symlink():
        info = path.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0
                or stat.S_IMODE(info.st_mode) != 0o400 or info.st_nlink != 1):
            raise IdentityError("UNSAFE_IDENTITY_FILE")
    if check:
        if not path.exists():
            raise StaleIdentityError("STALE_IDENTITY: missing file")
        with path.open() as f:
            stored = json.load(f)
        if not isinstance(stored, dict) or not isinstance(stored.get("generated_at"), str):
            raise IdentityError("INVALID_IDENTITY_FILE")
        mismatches = [key for key, value in expected.items() if stored.get(key) != value]
        if mismatches:
            raise StaleIdentityError("STALE_IDENTITY: " + ",".join(mismatches))
        generated = dt.datetime.fromisoformat(stored["generated_at"].replace("Z", "+00:00"))
        started = dt.datetime.fromisoformat(expected["started_at"].replace("Z", "+00:00"))
        if generated.tzinfo is None or not started <= generated <= dt.datetime.now(dt.timezone.utc):
            raise StaleIdentityError("STALE_IDENTITY: generated_at")
    else:
        expected["generated_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
        fd, temporary = tempfile.mkstemp(prefix=".identity-", dir=directory)
        try:
            with os.fdopen(fd, "w") as f:
                os.fchown(f.fileno(), 0, 0)
                os.fchmod(f.fileno(), 0o400)
                json.dump(expected, f, sort_keys=True)
                f.write("\n")
                f.flush()
                os.fsync(f.fileno())
            if observe(container, image_ref, revision, directory) != {k: v for k, v in expected.items() if k != "generated_at"}:
                raise IdentityError("CONTAINER_CHANGED")
            os.replace(temporary, path)
            dir_fd = os.open(directory, os.O_DIRECTORY)
            try:
                os.fsync(dir_fd)
            finally:
                os.close(dir_fd)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    if observe(container, image_ref, revision, directory) != {k: v for k, v in expected.items() if k != "generated_at"}:
        raise IdentityError("CONTAINER_CHANGED")


def reconcile(container, image_ref, revision, directory):
    # Only stale identity is repairable. Live image drift, unsafe permissions,
    # missing mounts and observation errors remain blocked without publication.
    try:
        refresh(container, image_ref, revision, directory, check=True)
    except StaleIdentityError:
        refresh(container, image_ref, revision, directory)
        refresh(container, image_ref, revision, directory, check=True)
        return "repaired"
    return "unchanged"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container")
    parser.add_argument("--image-ref")
    parser.add_argument("--expected-revision")
    parser.add_argument("--directory", default="/opt/clearapi/runtime/new-api")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--prepare-only", action="store_true")
    mode.add_argument("--reconcile", action="store_true")
    args = parser.parse_args()
    try:
        if args.prepare_only:
            if os.geteuid() != 0 or args.check:
                raise IdentityError("ROOT_REQUIRED")
            secure_directory(args.directory, prepare=True)
        else:
            if not all((args.container, args.image_ref, args.expected_revision)):
                raise IdentityError("IDENTITY_ARGUMENTS_REQUIRED")
            if args.reconcile:
                result = reconcile(args.container, args.image_ref, args.expected_revision, args.directory)
                print("Runtime identity reconcile: " + result)
            else:
                refresh(args.container, args.image_ref, args.expected_revision, args.directory, args.check)
    except (IdentityError, OSError, ValueError, KeyError, TypeError, IndexError, subprocess.SubprocessError):
        parser.exit(1, "Runtime identity BLOCKED; no container operation performed.\n")
    print("Runtime identity PASS")


if __name__ == "__main__":
    main()
