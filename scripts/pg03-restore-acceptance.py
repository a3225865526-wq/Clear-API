#!/usr/bin/env python3
"""Offline restore evidence audit. No network, decrypt, SQL or Docker execution.

PASS means internally consistent material, never authenticated production proof.
The caller must authenticate the producer independently; this tool deliberately
does not emit a successor-restore-acceptance receipt or grant execution rights.
Run only in the separately approved offline backup environment, not production.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys
import tarfile

sys.dont_write_bytecode = True
STATE_SPEC = importlib.util.spec_from_file_location('pg03_state_format', Path(__file__).with_name('pg03-backup-state.py'))
STATE = importlib.util.module_from_spec(STATE_SPEC)
STATE_SPEC.loader.exec_module(STATE)

MAX_JSON = 16 * 1024 * 1024
MAX_PAYLOAD = 16 * 1024 * 1024 * 1024
REQUIRED_ARTIFACTS = {'database_dump', 'globals', 'config_archive', 'legal_archive',
                      'edge_archive', 'secrets_archive', 'deploy_control_archive',
                      'pg03_state_archive', 'pg03_state_snapshot'}
REQUIRED_ARCHIVE_FILES = {
    'config_archive': {'opt/clearapi/app/docker-compose.prod.yml', 'opt/clearapi/app/compose-candidate.yaml'},
    'edge_archive': {'opt/clearapi/edge/Caddyfile', 'opt/clearapi/edge/docker-compose.yml'},
    'deploy_control_archive': {'opt/clearapi/app/release-manifest.json'},
}
REQUIRED_ARCHIVE_ROOTS = {
    'legal_archive': ('opt/clearapi/legal/',), 'secrets_archive': ('opt/clearapi/secrets/',),
    'deploy_control_archive': ('opt/clearapi/bin/', 'opt/clearapi/config/'),
}
PG03_DIRECTORIES = {'opt/clearapi/config/pg03', 'data/clearapi/pool-executor',
                    *('data/clearapi/pool-executor/'+name for name in ('auths', 'receipts', 'pool'))}
PG03_GUARDS = {'data/clearapi/pool-executor/pool/'+name
               for name in ('checkpoint.journal', 'admission.json')}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def digest(value):
    return hashlib.sha256(value).hexdigest()


def valid_hash(value):
    return type(value) is str and re.fullmatch('[a-f0-9]{64}', value) is not None


def utc(value):
    require(type(value) is str and value.endswith('Z'), 'UTC timestamp required')
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(parsed.utcoffset().total_seconds() == 0, 'UTC timestamp required')
    return parsed


def safe_name(name):
    require(type(name) is str and name and '\\' not in name and ':' not in name
            and not PurePosixPath(name).is_absolute() and '..' not in PurePosixPath(name).parts
            and str(PurePosixPath(name)) == name, 'unsafe archive or evidence path')
    return name


def local_file(root, name):
    path = root / safe_name(name)
    for parent in (path, *path.parents):
        require(not parent.is_symlink(), 'symlink evidence path')
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'regular single-link evidence required')
    return path


def file_hash(root, name):
    path = local_file(root, name)
    fd = os.open(path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_NONBLOCK', 0))
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1
                and before.st_size <= MAX_PAYLOAD, 'unsafe or oversized evidence')
        result = hashlib.file_digest(stream, 'sha256').hexdigest()
        after = os.fstat(stream.fileno())
    # Windows ctime is a deprecated creation-time alias with differing handle
    # and path semantics. POSIX additionally checks the metadata-change clock.
    fields = ('st_dev', 'st_ino', 'st_size', 'st_mtime_ns')
    if os.name != 'nt':
        fields += ('st_ctime_ns',)
    current = path.stat()
    changed = [key for key in fields if not getattr(before, key) == getattr(after, key) == getattr(current, key)]
    require(not changed, 'evidence changed while reading: ' + ','.join(changed))
    return result, before.st_size


def document(root, name, expected=None):
    path = local_file(root, name)
    fd = os.open(path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_NONBLOCK', 0))
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                and info.st_size <= MAX_JSON, 'unsafe or oversized JSON evidence')
        raw = stream.read(MAX_JSON + 1)
    require(len(raw) <= MAX_JSON, 'JSON evidence too large')
    require(expected is None or valid_hash(expected) and digest(raw) == expected,
            'document hash mismatch')
    value = json.loads(raw, object_pairs_hook=unique)
    require(type(value) is dict, 'JSON object required')
    return value


def archive_members(archive):
    seen, total = set(), 0
    for member in archive:
        safe_name(member.name)
        require(member.name not in seen and len(seen) < 100000
                and (member.isfile() or member.isdir()) and not member.pax_headers
                and 0 <= member.size <= MAX_PAYLOAD, 'unsafe or duplicate archive entry')
        seen.add(member.name)
        total += member.size
        require(total <= MAX_PAYLOAD, 'unpacked archive too large')
        yield member


class ArtifactReader:
    """Hash an archive payload while nested parsers consume it without staging."""
    def __init__(self, source):
        self.source = source
        self.hasher = hashlib.sha256()
        self.bytes = 0

    def read(self, size=-1):
        raw = self.source.read(size)
        self.hasher.update(raw)
        self.bytes += len(raw)
        return raw


def bundle_inventory(root, manifest):
    artifacts = manifest['artifacts']
    require(REQUIRED_ARTIFACTS <= set(artifacts), 'backup inventory incomplete')
    names = {}
    for key, item in artifacts.items():
        name = safe_name(item['name'])
        require('/' not in name and name not in names and valid_hash(item['sha256']),
                'ambiguous backup artifact')
        names[name] = key
    bundle = manifest['bundle']
    require(file_hash(root, bundle['name'])[0] == bundle['sha256'], 'plaintext bundle hash mismatch')
    found, pg03_snapshot, pg03_inventory = set(), None, None
    # Streaming only: no archive extraction or temporary plaintext secret copies.
    with tarfile.open(local_file(root, bundle['name']), 'r:gz') as archive:
        for member in archive_members(archive):
            require(member.isfile() and member.name in names, 'unexpected bundle member')
            key = names[member.name]
            payload = ArtifactReader(archive.extractfile(member))
            if key.endswith('_archive'):
                nested_entries = {}
                regular_names, guards = set(), set()
                with tarfile.open(fileobj=payload, mode='r|*') as nested:
                    for item in archive_members(nested):
                        if item.isfile():
                            regular_names.add(item.name)
                        if key == 'pg03_state_archive':
                            require(any(item.name == root or item.name.startswith(root+'/') for root in
                                ('opt/clearapi/app/compose-pg03.yaml', 'opt/clearapi/config/pg03',
                                 'data/clearapi/pool-executor')), 'PG03 archive root mismatch')
                            entry = dict(mode=item.mode, uid=item.uid, gid=item.gid,
                                         directory=item.isdir())
                            if item.isfile():
                                source = nested.extractfile(item)
                                if item.name in PG03_GUARDS:
                                    require(item.size <= MAX_JSON, 'oversized PG03 guard')
                                    raw = source.read(MAX_JSON+1)
                                    STATE.validate_guard_document(raw,
                                        journal=item.name.endswith('/checkpoint.journal'))
                                    guards.add(item.name)
                                    actual = digest(raw)
                                else:
                                    actual = hashlib.file_digest(source, 'sha256').hexdigest()
                                entry.update(bytes=item.size, sha256=actual)
                            nested_entries[item.name] = entry
                if key == 'pg03_state_archive':
                    require(guards == PG03_GUARDS and all(nested_entries.get(name, {}).get('directory') is True
                            for name in PG03_DIRECTORIES)
                            and 'opt/clearapi/app/compose-pg03.yaml' in regular_names, 'PG03 recovery roots incomplete')
                    pg03_inventory = nested_entries
                else:
                    require(REQUIRED_ARCHIVE_FILES.get(key, set()) <= regular_names
                            and all(any(name.startswith(prefix) for name in regular_names)
                                    for prefix in REQUIRED_ARCHIVE_ROOTS.get(key, ())),
                            'backup configuration recovery coverage incomplete')
            elif key == 'pg03_state_snapshot':
                require(member.size <= MAX_JSON, 'PG03 snapshot too large')
                pg03_snapshot = json.loads(payload.read(), object_pairs_hook=unique)
            elif key.endswith('dump'):
                require(payload.read(5) == b'PGDMP', 'custom PostgreSQL dump required')
            while payload.read(1024 * 1024):
                pass
            require(payload.bytes == member.size and payload.hasher.hexdigest() == artifacts[key]['sha256'],
                    'artifact hash mismatch')
            found.add(member.name)
    require(found == set(names), 'bundle inventory mismatch')
    require(type(pg03_snapshot) is dict and pg03_snapshot, 'missing PG03 snapshot')
    expected = {name: {k: v for k, v in item.items() if k != 'mtime_ns'}
                for name, item in pg03_snapshot.items()}
    require(expected == pg03_inventory, 'PG03 archive snapshot mismatch')
    # Recheck after parsing, catching path replacement after the first hash.
    require(file_hash(root, bundle['name'])[0] == bundle['sha256'], 'bundle changed during audit')
    return pg03_snapshot


def database_inventory(value, databases, *, read_only_source=False):
    require(set(value) == set(databases), 'main/log database coverage mismatch')
    for name, inventory in value.items():
        require(set(inventory) == {'catalog_tables', 'tables', 'schema_sha256', 'indexes_sha256',
                'constraints_sha256', 'sequences_sha256', 'migration_state_sha256',
                'validated_constraints', 'unvalidated_constraints', 'uniqueness_violations'},
                'incomplete database probe')
        tables = inventory['tables']
        require(type(tables) is dict and tables and type(inventory['catalog_tables']) is list
                and len(inventory['catalog_tables']) == len(set(inventory['catalog_tables']))
                and set(inventory['catalog_tables']) == set(tables), 'table inventory mismatch')
        for table, item in tables.items():
            require(type(table) is str and 0 < len(table) <= 256
                    and set(item) == {'rows', 'rows_sha256'}
                    and type(item['rows']) is int and item['rows'] >= 0
                    and valid_hash(item['rows_sha256']), 'invalid table fingerprint')
        require(all(valid_hash(inventory[key]) for key in ('schema_sha256', 'indexes_sha256',
                    'constraints_sha256', 'sequences_sha256', 'migration_state_sha256'))
                and type(inventory['validated_constraints']) is int
                and inventory['validated_constraints'] > 0
                and type(inventory['unvalidated_constraints']) is int
                and inventory['unvalidated_constraints'] == 0, 'invalid constraint or schema probe')
        uniqueness = inventory['uniqueness_violations']
        require((type(uniqueness) is int and uniqueness == 0)
                or (read_only_source and uniqueness is None),
                'uniqueness verification missing or failed')


def compare_database_inventories(source, restored, databases):
    database_inventory(source, databases, read_only_source=True)
    database_inventory(restored, databases)
    for name in databases:
        # Source collection must never REINDEX production. The recovered data
        # must independently pass PostgreSQL's real unique-index enforcement.
        source_data = {key: value for key, value in source[name].items() if key != 'uniqueness_violations'}
        restored_data = {key: value for key, value in restored[name].items() if key != 'uniqueness_violations'}
        require(source_data == restored_data,
                'restored database data/schema/index/constraint/sequence/migration drift')


def audit(root, plan_sha256, now, artifacts_root=None):
    artifacts_root = root if artifacts_root is None else artifacts_root
    require(now.tzinfo is not None and valid_hash(plan_sha256), 'pinned plan and timezone required')
    plan = document(root, 'restore-plan.json', plan_sha256)
    require(plan['schema'] == 'clearapi.offline-restore-plan/v1'
            and plan['repository'] == 'a3225865526-wq/Clear-API'
            and utc(plan['issued_at_utc']) <= now < utc(plan['expires_at_utc'])
            and 0 < (utc(plan['expires_at_utc']) - utc(plan['issued_at_utc'])).total_seconds() <= 86400
            and valid_hash(plan['challenge']), 'expired or invalid restore plan')
    candidate = plan['candidate']
    require(set(candidate) == {'source_commit', 'image_digest', 'image_ref', 'image_id',
                'binary_sha256', 'rc_run_id', 'rc_attempt'}
            and re.fullmatch('[a-f0-9]{40}', candidate['source_commit']) is not None
            and re.fullmatch('sha256:[a-f0-9]{64}', candidate['image_digest']) is not None
            and re.fullmatch('sha256:[a-f0-9]{64}', candidate['image_id']) is not None
            and candidate['image_ref'] == 'ghcr.io/a3225865526-wq/clearapi@'+candidate['image_digest']
            and valid_hash(candidate['binary_sha256'])
            and all(type(candidate[key]) is int and candidate[key] > 0 for key in ('rc_run_id', 'rc_attempt')),
            'invalid candidate identity')
    identity = plan['stopped_writer_identity']
    require(identity['identity_mode'] == 'stopped_failed_gateway'
            and re.fullmatch('[a-f0-9]{64}', identity['container_id']) is not None
            and re.fullmatch('[a-f0-9]{40}', identity['oci_revision']) is not None
            and re.fullmatch('sha256:[a-f0-9]{64}', identity['docker_image_id']) is not None
            and re.fullmatch('sha256:[a-f0-9]{64}', identity['ghcr_digest']) is not None
            and identity['ghcr_repo_digest'] == 'ghcr.io/a3225865526-wq/clearapi@'+identity['ghcr_digest']
            and all(valid_hash(identity[key]) for key in ('release_manifest_sha256',
                'compose_base_sha256', 'compose_override_sha256')), 'incomplete stopped source identity')
    pins = plan['evidence_sha256']
    required = {'backup-manifest.json', 'backup-completion.json', 'restore-run.json',
                'source-inventory.json', 'restored-inventory.json', 'docker-inspect.json',
                'pg03-restored-inventory.json'}
    require(set(pins) == required and all(valid_hash(v) for v in pins.values()), 'incomplete evidence pins')
    docs = {name: document(root, name, expected) for name, expected in pins.items()}
    manifest, completion, run = (docs[name] for name in ('backup-manifest.json',
                                                       'backup-completion.json', 'restore-run.json'))
    created = utc(manifest['created_at_utc'])
    require(manifest['environment'] == 'production-internal'
            and manifest['backup_id'] == plan['backup_id'] == completion['backup_id']
            # Restore approval follows the completed, hash-pinned backup. Its
            # issued_at must not be backdated to precede that backup's dump.
            and 0 <= (now-created).total_seconds() <= 86400
            and manifest['backup_identity_schema_version'] == 2
            and manifest['stopped_writer_identity'] == plan['stopped_writer_identity']
            and manifest['pg03_recovery']['consistency'] == 'unchanged_across_dump_and_bundle',
            'backup source, identity or recovery point mismatch')
    require(completion['schema'] == 'clearapi.successor-backup-completion/v1'
            and completion['manifest_sha256'] == pins['backup-manifest.json']
            and completion['stopped_writer_identity'] == plan['stopped_writer_identity']
            and type(completion['invocation_returncode']) is int and completion['invocation_returncode'] == 0
            and completion['plaintext_cleanup'] == 'verified_absent', 'backup completion mismatch')
    encrypted = manifest['encryption']['encrypted_artifact']
    require(manifest['encryption']['key_fingerprint'] == plan['key_fingerprint']
            and re.fullmatch('[A-F0-9]{40}', plan['key_fingerprint']) is not None
            and manifest['encryption']['method'] ==
                'GnuPG OpenPGP; ECDH Curve25519; AES256 + OCB AEAD (--force-ocb)'
            and type(encrypted['bytes']) is int and encrypted['bytes'] > 0
            and file_hash(artifacts_root, encrypted['name']) == (encrypted['sha256'], encrypted['bytes'])
            and completion['encrypted_sha256'] == encrypted['sha256']
            and completion['encrypted_bytes'] == encrypted['bytes'], 'ciphertext or encryption mismatch')
    recovery = manifest['database_recovery_point']
    require(created >= utc(recovery['dump_completed_at_utc']) >= utc(recovery['dump_started_at_utc'])
            and 0 <= (created-utc(recovery['dump_started_at_utc'])).total_seconds() <= 86400
            and recovery['dump_sha256'] == manifest['artifacts']['database_dump']['sha256'],
            'dump interval mismatch')
    pg03 = bundle_inventory(artifacts_root, manifest)
    require(docs['pg03-restored-inventory.json'] == pg03, 'restored PG03 state mismatch')
    databases = plan['databases']
    require(set(databases) in ({'main'}, {'main', 'log'})
            and plan['log_layout'] in ('shared', 'separate')
            and (plan['log_layout'] == 'shared') == (set(databases) == {'main'}), 'log database coverage missing')
    for name, item in databases.items():
        require(set(item) == {'target_database', 'dump_artifact'}
                and re.fullmatch(r'clearapi_restore_[a-z0-9_]{1,40}', item['target_database']) is not None
                and item['dump_artifact'] == ('database_dump' if name == 'main' else 'log_database_dump')
                and item['dump_artifact'] in manifest['artifacts'], 'independent target or dump missing')
    source, restored = docs['source-inventory.json'], docs['restored-inventory.json']
    compare_database_inventories(source, restored, databases)
    target = plan['target_container_id']
    require(type(target) is str and re.fullmatch('[a-f0-9]{64}', target) is not None
            and target not in plan['production_container_ids']
            and plan['production_container_ids'], 'independent target identity required')
    inspect = docs['docker-inspect.json']
    require(inspect['Id'] == target and inspect['HostConfig']['NetworkMode'] == 'none'
            and inspect['Mounts'] == [] and inspect['Config']['Image'] == plan['postgres_image_ref']
            and re.fullmatch(r'postgres:17\.6-alpine@sha256:[a-f0-9]{64}', plan['postgres_image_ref']) is not None,
            'unsafe or mismatched restore container')
    started, completed = utc(run['started_at_utc']), utc(run['completed_at_utc'])
    require(created <= started < completed <= now and completed < utc(plan['expires_at_utc'])
            and run['schema'] == 'clearapi.offline-restore-run/v1'
            and run['challenge'] == plan['challenge'] and run['candidate'] == plan['candidate']
            and run['backup_completion_sha256'] == pins['backup-completion.json']
            and run['postgres_version_num'] == '170006' and run['private_key_recorded'] is False
            and run['production_connections'] == [] and run['cleanup_remaining_containers'] == [],
            'restore invocation identity, time or containment mismatch')
    commands = run['commands']
    require(set(commands) == {'decrypt', 'cleanup', *('restore_'+name for name in databases)},
            'actual command outcomes missing')
    for name, result in commands.items():
        require(type(result['returncode']) is int and result['returncode'] == 0
                and started <= utc(result['started_at_utc']) < utc(result['completed_at_utc']) <= completed,
                'restore/decrypt/cleanup subprocess failed or unfinished')
        for stream in ('stdout', 'stderr'):
            require(file_hash(root, result[stream+'_file'])[0] == result[stream+'_sha256'], 'command output hash mismatch')
        if name.startswith('restore_'):
            logical = name.removeprefix('restore_')
            db = databases[logical]
            require(result['argv'] == ['docker', 'exec', target, 'pg_restore', '--exit-on-error',
                    '--no-owner', '--no-acl', '-U', 'postgres', '--dbname='+db['target_database'],
                    '/restore/'+manifest['artifacts'][db['dump_artifact']]['name']],
                    'unsafe or non-restore invocation')
            require(utc(commands['decrypt']['completed_at_utc']) <= utc(result['started_at_utc'])
                    and utc(result['completed_at_utc']) <= utc(commands['cleanup']['started_at_utc']),
                    'invalid decrypt/restore/cleanup order')
        elif name == 'decrypt':
            require(result['argv'] == ['gpg', '--batch', '--no-tty', '--status-fd=1', '--output',
                    manifest['bundle']['name'], '--decrypt', encrypted['name']], 'decrypt command mismatch')
            status_path = local_file(root, result['stdout_file'])
            require(status_path.stat().st_size <= 65536, 'oversized decrypt status')
            statuses = status_path.read_bytes().splitlines()
            require(b'[GNUPG:] DECRYPTION_OKAY' in statuses
                    and not any(any(marker in line for marker in (b'DECRYPTION_FAILED', b'BADMDC', b'FAILURE'))
                                for line in statuses), 'decrypt integrity failure or missing outcome')
        else:
            require(result['argv'] == ['docker', 'rm', '-f', target], 'cleanup command mismatch')
    # This audit does not turn operator-provided command records into provenance.
    # A trusted producer must bind these bytes, live command results and target
    # inspection under an independently approved identity before adapter admission.
    return dict(schema='clearapi.restore-evidence-audit/v1', content_status='PASS',
                production_readiness='BLOCKED', producer_authentication='UNKNOWN',
                production_execution_allowed=False, plan_sha256=plan_sha256,
                evidence_sha256=pins, candidate=plan['candidate'], backup_id=plan['backup_id'],
                reason='independent authenticated producer and production authorization still required')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence-dir', type=Path, required=True)
    parser.add_argument('--plan-sha256', required=True, help='out-of-band pinned plan hash; not an authorization')
    parser.add_argument('--artifact-dir', type=Path, help='private ciphertext/bundle directory, defaults to evidence directory')
    args = parser.parse_args()
    try:
        result = audit(args.evidence_dir, args.plan_sha256, datetime.now(timezone.utc), args.artifact_dir)
    except (OSError, ValueError, KeyError, TypeError, tarfile.TarError):
        # Do not print payload, SQL, archive names, arbitrary errors or key data.
        print(json.dumps(dict(schema='clearapi.restore-evidence-audit/v1', content_status='FAIL',
                              production_readiness='BLOCKED', production_execution_allowed=False)))
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == '__main__':
    sys.exit(main())
