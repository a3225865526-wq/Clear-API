"""Shared release-mode contract; evidence validation never grants host access."""

import re
import hashlib
import json
from pathlib import Path

PG03_GATES = frozenset({
    'runtime_integrity', 'billing', 'backup_restore', 'executor_pairing',
    'sqlite_upgrade_idempotency', 'mysql_upgrade_idempotency',
    'postgres_upgrade_idempotency', 'gateway_executor_provider_usage_billing',
    'replay_and_duplicate_charge', 'state_isolation', 'safe_rollback',
})
OFF_GATES = (PG03_GATES - {'executor_pairing', 'gateway_executor_provider_usage_billing'}
             | {'pool_disabled', 'ordinary_provider_usage_billing'})


def required_gates(pool_mode, release_stage='commercial'):
    if pool_mode not in ('off', 'pg03') or release_stage not in ('commercial', 'controlled'):
        raise ValueError('unsupported release mode or stage')
    if pool_mode == 'off':
        if release_stage != 'commercial':
            raise ValueError('off release cannot defer ordinary Provider acceptance')
        return OFF_GATES
    return (PG03_GATES - {'gateway_executor_provider_usage_billing'}
            if release_stage == 'controlled' else PG03_GATES)


def proof_mode(proof, controlled=False):
    legacy = 'clearapi.controlled-release/v1' if controlled else 'clearapi.automatic-release/v1'
    if proof.get('schema') == legacy:
        # Existing v1 artifacts describe PG03 only; never reinterpret them as off.
        if proof.get('pool_mode', 'pg03') != 'pg03':
            raise ValueError('legacy proof cannot authorize off')
        return 'pg03'
    if not controlled and proof.get('schema') == 'clearapi.automatic-release/v2':
        mode = proof.get('pool_mode')
        required_gates(mode)
        return mode
    raise ValueError('unsupported proof contract')


def validate_receipt_mode(receipt, pool_mode):
    if pool_mode == 'off':
        if receipt.get('schema') != 'clearapi.gate-evidence/v2' or receipt.get('pool_mode') != 'off':
            raise ValueError('off receipt mode/schema mismatch')
    elif (receipt.get('schema') != 'clearapi.gate-evidence/v1'
          or receipt.get('pool_mode', 'pg03') != 'pg03'):
        raise ValueError('PG03 receipt mode/schema mismatch')


def validate_schema_transition(transition, manifest, pool_mode):
    if pool_mode == 'pg03':
        return
    if (not isinstance(transition, dict)
            or transition.get('source_commit') != manifest.get('source_commit')
            or transition.get('startup_passes') != 2
            or transition.get('schema_version') != 'pg03-schema-rehearsal-v1'):
        raise ValueError('RC schema rehearsal mode or source binding mismatch')
    if (transition.get('pool_mode') != 'off'
            or transition.get('candidate_binary_sha256') != manifest.get('binary_sha256')
            or transition.get('candidate_image_digest') != manifest.get('image_digest')):
        raise ValueError('OFF schema rehearsal identity mismatch')
    for field in ('source_schema_sha256', 'target_schema_sha256'):
        if not isinstance(transition.get(field), str) or not re.fullmatch(r'[0-9a-f]{64}', transition[field]):
            raise ValueError('RC schema transition is missing or malformed')


def validate_off_rc_artifact(directory, source, source_tree, run_id, attempt):
    directory = Path(directory)
    manifest = json.loads((directory / 'gateway-image-release.json').read_bytes())
    hashes = json.loads((directory / 'artifact-hashes.json').read_bytes())
    required = {'new-api', 'gateway-image-release.json', 'schema-rehearsal.json',
                'schema-rehearsal.target.sql', 'runtime-assets/artifact-hashes.json',
                'runtime-assets/gateway-image-release.json', 'runtime-assets/inventory.json',
                'runtime-assets/boot-recovery-install.py',
                'runtime-assets/scripts/runtime-boot-recovery.py',
                'runtime-assets/scripts/runtime-identity-refresh.py'}
    if set(hashes) != required:
        raise ValueError('OFF RC artifact inventory mismatch')
    for name, digest in hashes.items():
        path = directory / name
        if (not isinstance(digest, str) or not path.is_file() or path.is_symlink()
                or not re.fullmatch(r'[0-9a-f]{64}', digest)
                or hashlib.sha256(path.read_bytes()).hexdigest() != digest):
            raise ValueError('OFF RC artifact content hash mismatch: ' + name)
    if (manifest.get('schema') != 'clearapi.release-candidate/v1'
            or manifest.get('source_kind') != 'git-commit' or manifest.get('pool_mode') != 'off'
            or manifest.get('source_commit') != source or manifest.get('source_tree') != source_tree
            or manifest.get('workflow_run') != str(run_id)
            or type(manifest.get('workflow_run_attempt')) is not int
            or manifest['workflow_run_attempt'] != attempt
            or manifest.get('platform') != 'linux/amd64'
            or manifest.get('image') != 'ghcr.io/a3225865526-wq/clearapi'
            or not re.fullmatch(r'sha256:[0-9a-f]{64}', manifest.get('image_digest', ''))
            or manifest.get('binary_sha256') != hashes['new-api']
            or manifest.get('production_ready') is not False
            or manifest.get('production_acceptance') != 'not_run'):
        raise ValueError('OFF RC manifest identity mismatch')
    if ((directory / 'runtime-assets/gateway-image-release.json').read_bytes()
            != (directory / 'gateway-image-release.json').read_bytes()):
        raise ValueError('OFF runtime manifest mismatch')
    transition = json.loads((directory / 'schema-rehearsal.json').read_bytes())
    if transition != manifest.get('schema_transition'):
        raise ValueError('OFF schema rehearsal artifact mismatch')
    validate_schema_transition(transition, manifest, 'off')
    if (transition.get('profile') != 'isolated_schema_only_master_single_postgres'
            or transition.get('production_schema') != 'unverified'
            or not re.fullmatch(r'sha256:[0-9a-f]{64}', transition.get('candidate_image_id', ''))):
        raise ValueError('OFF schema rehearsal receipt is incomplete')
    return manifest
