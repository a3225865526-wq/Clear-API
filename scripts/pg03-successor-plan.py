#!/usr/bin/env python3
"""Offline admission review for an incident successor; never authorize execution.

Inputs are evidence to review, not trusted production observations. In particular,
passing this contract neither authenticates their producer nor grants permission
to install assets, replace a container, write a manifest, or open the edge.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys
import tempfile
import zipfile

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location(
    'successor_schema', Path(__file__).with_name('pg03-schema-rehearsal.py'))
SCHEMA = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SCHEMA)
FAILED_RUN = '39347012653'
FAILED_COMMIT = 'c50a53c1b89e121bcade7dac41d3f11e53a9832a'
FAILED_DIGEST = 'sha256:22ecaa468c784cd41ee2604dd8f3ac189960744516a508fc8c9cee3ee1724e02'
PREVIOUS_COMMIT = 'eb67c5f21639716eed7489d32ca322c249e5fdf3'
PREVIOUS_DIGEST = 'sha256:51ddf6df27129df0fe7ab7205c1e8bef0da6e2ca9b7bb9ede93ab77c863d24a5'
EXECUTOR_CONTAINER = '1081a1b576ad95e521c1d390b3cfe4d4f9c0eba880779a0391a7e9aef673e57f'
FROZEN = {
    'release-manifest.json': '6c308146551dfab395e7202f775b23121ec460757e6b56b7f107decb5cae63d3',
    'pg03-target-manifest.json': '521e60ce3edbe4cd073decaedd70134daf3d22d8039d0fed4de427b2318cb39e',
    'controlled-launch-state.json': '57a7152a312eba02e2c81b2973981894640c2088782827e6d2d112b2235dff4d',
    'compose-candidate.yaml': '077407c6ebe7d69f501f6c758fc449db4084061e6e73319f5d7e44b6cf8a2b6e',
    'compose-pg03.yaml': '5b237a189639a044d4a883fc258140ac67c462402a855b691593c9ada9f661c0',
}
# Reuse the release proof's gate inventory. Production checks remain separate.
PROOF_SPEC = importlib.util.spec_from_file_location(
    'successor_release_proof', Path(__file__).parent / 'ci' / 'assemble-release-proof.py')
PROOF = importlib.util.module_from_spec(PROOF_SPEC)
PROOF_SPEC.loader.exec_module(PROOF)
GATES = frozenset(PROOF.GATES)


class Blocked(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Blocked(message)


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def utc(value):
    require(isinstance(value, str) and value.endswith('Z'), 'explicit UTC timestamp required')
    return datetime.fromisoformat(value[:-1] + '+00:00')


def review(candidate, observation, schema_receipt, receipts, now=None):
    """Validate evidence bindings and return a proposal, never an execution permit.

    The separate production executor is intentionally not implemented here: a
    stopped-writer backup and approved startup-write projection are still missing.
    A future executor must independently authenticate artifacts/observations and
    recheck every production condition under its deployment lock.
    """
    now = now or datetime.now(timezone.utc)
    require(now.tzinfo is not None, 'timezone-aware review time required')
    source = candidate.get('source_commit')
    digest = candidate.get('image_digest')
    require(isinstance(source, str) and re.fullmatch(r'[a-f0-9]{40}', source)
            and source != FAILED_COMMIT, 'a distinct successor source is required')
    require(isinstance(digest, str) and re.fullmatch(r'sha256:[a-f0-9]{64}', digest)
            and digest not in (FAILED_DIGEST, PREVIOUS_DIGEST), 'a distinct successor image is required')
    require(all(candidate.get(key) == source for key in
                ('candidate_commit', 'oci_revision', 'workflow_commit')), 'candidate source identity mismatch')
    require(candidate.get('image_ref') == 'ghcr.io/a3225865526-wq/clearapi@' + digest,
            'candidate exact image reference mismatch')
    require(candidate.get('platform') == 'linux/amd64'
            and candidate.get('deployment_compatible') is True, 'candidate deployment platform mismatch')
    require(type(candidate.get('workflow_run')) is int and candidate['workflow_run'] > 0
            and type(candidate.get('workflow_run_attempt')) is int
            and candidate['workflow_run_attempt'] > 0, 'exact RC run and attempt required')
    require(re.fullmatch(r'sha256:[a-f0-9]{64}', candidate.get('new_api_image_id', ''))
            and re.fullmatch(r'[a-f0-9]{64}', candidate.get('binary_sha256', '')),
            'candidate image and binary identity required')
    require(candidate.get('runtime_identity') == {
        'image_id': candidate['new_api_image_id'], 'binary_path': '/new-api',
        'binary_sha256': candidate['binary_sha256']}, 'candidate runtime identity mismatch')

    age = (now - utc(observation.get('observed_at_utc'))).total_seconds()
    require(0 <= age <= 300, 'fresh observation required; historical snapshots cannot be renewed')
    require(observation.get('incident_run') == FAILED_RUN
            and observation.get('frozen_sha256') == FROZEN, 'frozen failed predecessor mismatch')
    require(observation.get('predecessor') == {
        'source_commit': FAILED_COMMIT, 'image_digest': FAILED_DIGEST,
        'status': 'failed', 'deployment_attempt_status': 'failed', 'executor_start': 'forbidden'},
        'failed predecessor must remain failed; Executor start remains forbidden')
    require(observation.get('gateway_state') == 'exited_127_missing_runtime_directory',
            'only the diagnosed stopped Gateway is eligible')
    require(observation.get('runtime_identity_dir') == '/run/clearapi'
            and observation.get('identity_mount') == {
                'source': '/run/clearapi', 'target': '/run/clearapi',
                'read_only': True, 'create_host_path': False}, 'runtime identity mount contract mismatch')
    mount = observation['identity_mount']
    require(mount['read_only'] is True and mount['create_host_path'] is False,
            'identity mount flags must be JSON booleans')
    require(observation.get('executor_container_id') == EXECUTOR_CONTAINER
            and observation.get('executor_state') == 'running_healthy',
            'prepared incident Executor required; no start operation is permitted')
    require(observation.get('ingress') == 'isolated_stopped_restart_no'
            and observation.get('public_web_listeners') == [], 'public ingress isolation required')
    require(observation.get('previous_success') == {
        'source_commit': PREVIOUS_COMMIT, 'image_digest': PREVIOUS_DIGEST},
        'retain the previous successful reference independently of the failed predecessor')

    actual_schema = observation.get('database_schema_sha256', '')
    require(isinstance(actual_schema, str) and re.fullmatch(r'[a-f0-9]{64}', actual_schema),
            'observed database schema required; preflight-only is not evidence')
    SCHEMA.validate_receipt(schema_receipt, {
        'image_id': candidate['new_api_image_id'], 'binary_sha256': candidate['binary_sha256'],
        'commit': source}, actual_schema)
    require(type(schema_receipt.get('startup_passes')) is int,
            'schema rehearsal startup count must be an integer')
    # The narrow first successor cannot introduce a further schema transition.
    require(schema_receipt['target_schema_sha256'] == actual_schema,
            'additional schema migration needs a separately reviewed successor design')

    require(set(receipts) == GATES, 'complete candidate gate inventory required')
    receipt_hashes = {}
    for gate in sorted(GATES):
        raw = receipts[gate]
        require(isinstance(raw, bytes) and len(raw) <= 1048576, 'bounded receipt bytes required')
        receipt = json.loads(raw, object_pairs_hook=unique)
        require(receipt.get('schema') == 'clearapi.gate-evidence/v1'
                and receipt.get('gate') == gate and receipt.get('conclusion') == 'success',
                'candidate gate failed: ' + gate)
        require(receipt.get('source_commit') == source and receipt.get('gateway_digest') == digest
                and type(receipt.get('rc_run_id')) is int
                and receipt['rc_run_id'] == candidate['workflow_run']
                and type(receipt.get('rc_run_attempt')) is int
                and receipt['rc_run_attempt'] == candidate['workflow_run_attempt'],
                'candidate receipt identity mismatch: ' + gate)
        receipt_hashes[gate] = hashlib.sha256(raw).hexdigest()

    # The distinction is intentional: syntactically matching receipts alone do
    # not establish their provenance, live qualification, or production authority.
    return {
        'schema': 'clearapi.incident-successor-plan/v1',
        'status': 'OFFLINE CONTRACT VALIDATED — NOT PRODUCTION ACCEPTANCE',
        'execution_allowed': False,
        'failed_run': FAILED_RUN,
        'candidate': {'source_commit': source, 'image_digest': digest,
                      'rc_run': candidate['workflow_run'], 'rc_attempt': candidate['workflow_run_attempt']},
        'candidate_receipt_sha256': receipt_hashes,
        'executor_start': 'forbidden', 'public_traffic': 'blocked',
        'previous_success': observation['previous_success'].copy(),
        'remaining_gates': [
            'authenticate current-main CI/RC/rehearsal and all receipt producers',
            'obtain fresh authorized production observations and exclusive execution window',
            'prove a fresh stopped-writer backup, restore rehearsal and startup-write scope',
            'review and implement the production successor executor and its one-use authorization',
            'approve exact installation, manifest/override writes and Gateway replacement',
            'verify new process identity, local health, Runtime and authenticated Executor connection',
            'verify restart recovery with the candidate while ingress stays isolated',
            'approve Caddy opening; verify HTTPS, login, model calls and accounting online',
        ],
    }


def successor_baseline(archive_path, archive_sha256, observed_schema_sha256):
    """Bind an RC's row-free target schema to the asserted successor baseline."""
    require(re.fullmatch(r'[a-f0-9]{64}', archive_sha256 or '')
            and re.fullmatch(r'[a-f0-9]{64}', observed_schema_sha256 or ''),
            'exact archive and observed schema hashes required')
    require(archive_path.stat().st_size <= 10485760, 'oversized RC archive')
    raw = archive_path.read_bytes()
    require(hashlib.sha256(raw).hexdigest() == archive_sha256, 'RC archive digest mismatch')
    payloads = {'gateway-image-release.json', 'schema-rehearsal.json',
                'schema-rehearsal.target.sql', 'binary-build-info.txt'}
    with zipfile.ZipFile(archive_path) as archive:
        require(len(archive.namelist()) == 5
                and set(archive.namelist()) == payloads | {'artifact-hashes.json'},
                'RC archive inventory mismatch')
        require(all(info.file_size <= 2097152 for info in archive.infolist()),
                'oversized RC archive entry')
        inventory = json.loads(archive.read('artifact-hashes.json'), object_pairs_hook=unique)
        require(isinstance(inventory, dict) and set(inventory) == payloads,
                'RC hash inventory mismatch')
        files = {name: archive.read(name) for name in payloads}
    require(all(hashlib.sha256(files[name]).hexdigest() == inventory[name] for name in payloads),
            'RC payload digest mismatch')
    candidate = json.loads(files['gateway-image-release.json'], object_pairs_hook=unique)
    prior = json.loads(files['schema-rehearsal.json'], object_pairs_hook=unique)
    require(candidate.get('schema_transition') == prior, 'RC schema receipt mismatch')
    require(prior.get('target_schema_sha256') == observed_schema_sha256
            and SCHEMA.schema_digest(files['schema-rehearsal.target.sql'].decode()) == observed_schema_sha256,
            'RC target is not the supplied database schema; obtain the actual schema separately')
    source = candidate.get('source_commit', '')
    digest = candidate.get('image_digest', '')
    require(re.fullmatch(r'[a-f0-9]{40}', source) and source != FAILED_COMMIT
            and re.fullmatch(r'sha256:[a-f0-9]{64}', digest)
            and digest not in (FAILED_DIGEST, PREVIOUS_DIGEST), 'distinct successor candidate required')
    image_ref = 'ghcr.io/a3225865526-wq/clearapi@' + digest
    require(candidate.get('image_ref') == image_ref
            and all(candidate.get(key) == source for key in
                    ('candidate_commit', 'workflow_commit', 'oci_revision')),
            'RC candidate identity mismatch')
    candidate_identity = {'image_id': candidate['new_api_image_id'],
                          'binary_sha256': candidate['binary_sha256'], 'commit': source}
    SCHEMA.validate_receipt(prior, candidate_identity, prior.get('source_schema_sha256'))
    require(prior.get('source_commit') == source and candidate.get('platform') == 'linux/amd64',
            'RC schema candidate mismatch')
    return candidate, files['schema-rehearsal.target.sql']


def rehearse_successor_schema(archive_path, archive_sha256, observed_schema_sha256, output,
                             observation=None, nonce=None, command=None):
    """Run two isolated starts from the RC's target schema, preserving the RC.

    The externally supplied schema hash is an assertion to test, not a fresh
    production observation. Images must already be available on the local daemon;
    this function never obtains credentials, pulls images or contacts production.
    """
    candidate, schema = successor_baseline(archive_path, archive_sha256, observed_schema_sha256)
    image_ref, source = candidate['image_ref'], candidate['source_commit']
    candidate_identity = {'image_id': candidate['new_api_image_id'],
                          'binary_sha256': candidate['binary_sha256'], 'commit': source}
    outputs = (output.with_suffix('.target.sql'), output)
    require(output.suffix == '.json' and output.parent.is_dir()
            and all(not path.exists() and not path.is_symlink() for path in outputs),
            'new local receipt paths required; preserve existing evidence')
    with tempfile.TemporaryDirectory(prefix='clearapi-successor-schema-') as directory:
        root = Path(directory)
        schema_path = root / 'current-schema.sql'
        schema_path.write_bytes(schema)
        staged = root / 'rehearsal.json'
        options = dict(
            observation=observation, nonce=nonce, expected_identity=candidate_identity,
            expected_schema=observed_schema_sha256, require_migration_evidence=True)
        if command is not None:
            options['command'] = command
        receipt = SCHEMA.rehearse(image_ref, source, schema_path, staged, **options)
        SCHEMA.validate_receipt(receipt, candidate_identity, observed_schema_sha256)
        require(type(receipt.get('startup_passes')) is int
                and receipt.get('target_schema_sha256') == observed_schema_sha256,
                'successor startup changes schema; no successor receipt published')
        receipt['successor_baseline'] = {
            'schema': 'clearapi.successor-schema-baseline/v1',
            'rc_archive_sha256': archive_sha256, 'schema_source': 'verified_rc_target',
            'observed_schema_sha256': observed_schema_sha256,
            'production_observation_verified': False, 'production_acceptance': 'not_run',
            'producer_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            'schema_helper_sha256': hashlib.sha256(Path(SCHEMA.__file__).read_bytes()).hexdigest(),
        }
        # Publish receipt last. Partial publication is retained, never retried or
        # mistaken for acceptance; callers must use new paths after a failure.
        with outputs[0].open('xb') as stream:
            stream.write(staged.with_suffix('.target.sql').read_bytes())
        with output.open('x', encoding='utf-8') as stream:
            json.dump(receipt, stream, indent=2)
            stream.write('\n')
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--bundle', type=Path,
                      help='local evidence directory; no network or production command is available')
    mode.add_argument('--rehearse-schema', action='store_true',
                      help='two isolated local Docker starts; requires existing candidate/PostgreSQL images')
    mode.add_argument('--inspect-baseline', action='store_true',
                      help='validate the RC/schema input without contacting Docker')
    parser.add_argument('--rc-archive', type=Path)
    parser.add_argument('--rc-archive-sha256')
    parser.add_argument('--observed-schema-sha256')
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.inspect_baseline:
        require(args.rc_archive is not None and args.output is None,
                'local RC archive required; baseline inspection never writes output files')
        candidate, _ = successor_baseline(args.rc_archive, args.rc_archive_sha256,
                                        args.observed_schema_sha256)
        print(json.dumps({
            'schema': 'clearapi.successor-baseline-inspection/v1',
            'source_commit': candidate['source_commit'], 'image_digest': candidate['image_digest'],
            'rc_archive_sha256': args.rc_archive_sha256,
            'baseline_schema_sha256': args.observed_schema_sha256,
            'rehearsal': 'not_run', 'production_observation_verified': False,
            'execution_allowed': False,
        }, indent=2))
        return
    if args.rehearse_schema:
        require(args.rc_archive is not None and args.output is not None,
                'local RC archive and new output required')
        rehearse_successor_schema(args.rc_archive, args.rc_archive_sha256,
                                 args.observed_schema_sha256, args.output)
        print('Isolated successor schema startup passed twice; production acceptance NOT RUN')
        return
    require(not any((args.rc_archive, args.rc_archive_sha256, args.observed_schema_sha256, args.output)),
            'schema rehearsal arguments cannot be used for an offline bundle review')
    root = args.bundle.resolve(strict=True)
    require(root.is_dir(), 'local evidence directory required')
    # This CLI reads only a fixed, flat inventory in the explicitly selected
    # directory. Refuse links and special files rather than following evidence.
    files = {}
    for name in ('candidate.json', 'observation.json', 'schema-rehearsal.json',
                 *(gate + '.json' for gate in sorted(GATES))):
        path = root / name
        require(not path.is_symlink() and path.is_file() and path.stat().st_size <= 1048576,
                'missing or unsafe local evidence file')
        files[name] = path.read_bytes()
    result = review(*(json.loads(files[name], object_pairs_hook=unique) for name in
                     ('candidate.json', 'observation.json', 'schema-rehearsal.json')),
                    {gate: files[gate + '.json'] for gate in GATES})
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # Never reflect arbitrary evidence, command output or credentials.
        sys.exit('SUCCESSOR PLAN BLOCKED: incomplete or inconsistent evidence; no production action performed')
