#!/usr/bin/env python3
"""Read-only candidate qualification, separate from production authorization.

Only an independently authenticated, revocable policy provider may supply policy.
No CLI, network transport, approval producer, deployment or mutation is provided.
Legacy release-control current-main admission is intentionally unchanged.
"""
import base64
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
from pathlib import Path
import re
import sys
import zipfile

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('qualification_adapter', Path(__file__).with_name('pg03-successor-adapter.py'))
AD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AD)
TX, PLAN = AD.TX, AD.TX.PLAN
ROOT = '/repos/' + AD.REPO
RUN_FIELDS = ('run_id', 'attempt', 'source_commit', 'workflow_path', 'event')
REQUIRED_CONTROL_FILES = frozenset(('scripts/pg03-candidate-qualification.py',
    'scripts/pg03-successor-adapter.py', 'scripts/pg03-successor-transaction.py',
    'scripts/pg03-observation.py', 'scripts/pg03-successor-host.py',
    'scripts/pg03-restore-acceptance.py', 'scripts/pg03-offline-restore.py',
    'scripts/pg03-backup-state.py', 'scripts/backup-identity.py', 'scripts/production-backup.sh',
    'scripts/production-migration-entrypoint.sh',
    'scripts/ci/pg03-successor-grant.py',
    'scripts/pg03-successor-plan.py', 'scripts/pg03-schema-rehearsal.py',
    'scripts/pg03-release-control.py', '.github/workflows/ci.yml',
    '.github/workflows/release-pg03-gateway.yml', '.github/workflows/pg03-isolated-rehearsal.yml',
    '.github/workflows/pg03-successor-approval.yml'))
ISOLATION_FILES = frozenset(('scripts/ci/pg03-isolated-rehearsal.py',
    'scripts/pg03-successor-plan.py', 'scripts/pg03-schema-rehearsal.py',
    'scripts/ci/assemble-release-proof.py', 'scripts/ci/test-pg03-successor-plan.py',
    '.github/workflows/pg03-isolated-rehearsal.yml'))
SOURCE_FILES = frozenset(('.github/workflows/ci.yml', '.github/workflows/release-pg03-gateway.yml'))


def require(condition, message='candidate qualification incomplete'):
    PLAN.require(condition, message)


def control_path(path):
    return (type(path) is str and '..' not in path.split('/')
        and re.fullmatch(r'(scripts/(ci/)?[a-z0-9-]+\.(py|sh)|\.github/workflows/[a-z0-9-]+\.yml|evidence/[A-Za-z0-9_./-]+|CLEARAPI_MASTER_STATUS\.md)', path))


def validate_policy(policy, request, now):
    """Validate content only; caller must authenticate the provider independently.

    The provider must read current owner approval and revocation/consumption state
    server-side on EVERY call, with bounded GET-only transport, never from request
    JSON, a historical local file, environment switches or a request's callback.
    """
    TX.exact(policy, ('schema', 'policy_id', 'state', 'scope', 'source_control_policy',
        'approved_by', 'approval_ref', 'repository', 'repository_id', 'issued_at_utc',
        'expires_at_utc', 'request_sha256', 'transaction_id', 'candidate', 'control_commit',
        'max_evidence_age_seconds', 'revoked_policy_ids', 'revoked_source_commits',
        'revoked_image_digests', 'consumed_transaction_ids', 'control_files_sha256', 'source_workflow_files_sha256',
        'isolation_control_files_sha256', 'allowed_control_changes',
        'source_control_diff_sha256', 'bindings'))
    require(policy['schema'] == 'clearapi.candidate-qualification-policy/v1'
        and TX.matches(r'[a-f0-9]{32}', policy['policy_id']) and policy['state'] == 'active'
        and policy['scope'] == 'candidate_qualification_only'
        and policy['source_control_policy'] == 'independently_reviewed_source_control_split'
        and policy['approved_by'] == AD.OWNER and TX.matches(r'[A-Za-z0-9:/_.#-]{1,256}', policy['approval_ref'])
        and policy['repository'] == AD.REPO and type(policy['repository_id']) is int and policy['repository_id'] > 0
        and policy['request_sha256'] == TX.sha(TX.encoded(request))
        and policy['transaction_id'] == request['transaction_id']
        and TX.encoded(policy['candidate']) == TX.encoded(request['candidate'])
        and policy['control_commit'] == request['control_commit'], 'unapproved policy or request binding')
    issued, expires = PLAN.utc(policy['issued_at_utc']), PLAN.utc(policy['expires_at_utc'])
    require(issued <= now < expires <= PLAN.utc(request['expires_at_utc'])
        and 0 < (expires-issued).total_seconds() <= 300, 'qualification policy expired')
    for key, pattern in (('revoked_policy_ids', r'[a-f0-9]{32}'),
                         ('revoked_source_commits', r'[a-f0-9]{40}'),
                         ('revoked_image_digests', r'sha256:[a-f0-9]{64}'),
                         ('consumed_transaction_ids', r'[a-f0-9]{32}')):
        values = policy[key]
        require(type(values) is list and len(values) <= 10000
            and all(TX.matches(pattern, value) for value in values) and len(set(values)) == len(values))
    require(policy['policy_id'] not in policy['revoked_policy_ids']
        and request['candidate']['source_commit'] not in policy['revoked_source_commits']
        and request['candidate']['image_digest'] not in policy['revoked_image_digests']
        and request['transaction_id'] not in policy['consumed_transaction_ids'], 'candidate revoked or transaction consumed')
    require(type(policy['max_evidence_age_seconds']) is int and 0 < policy['max_evidence_age_seconds'] <= 604800)
    for key, required in (('control_files_sha256', REQUIRED_CONTROL_FILES),
                          ('source_workflow_files_sha256', SOURCE_FILES),
                          ('isolation_control_files_sha256', ISOLATION_FILES)):
        values = policy[key]
        require(type(values) is dict and required <= set(values) and len(values) <= 100
            and all(control_path(path) and AD.hash_value(value) for path, value in values.items()))
    changes = policy['allowed_control_changes']
    require(type(changes) is list and len(changes) <= 300 and len(set(changes)) == len(changes)
        and all(control_path(path) for path in changes) and AD.hash_value(policy['source_control_diff_sha256']))
    TX.exact(policy['bindings'], ('rc', 'source_ci', 'control_ci', 'isolation'))
    expected = dict(rc=(request['candidate']['source_commit'], '.github/workflows/release-pg03-gateway.yml'),
        source_ci=(request['candidate']['source_commit'], '.github/workflows/ci.yml'),
        control_ci=(request['control_commit'], '.github/workflows/ci.yml'),
        isolation=(None, '.github/workflows/pg03-isolated-rehearsal.yml'))
    for name, (source, workflow) in expected.items():
        binding = policy['bindings'][name]
        TX.exact(binding, RUN_FIELDS + (('artifact_id', 'artifact_name', 'archive_sha256') if name in ('rc', 'isolation') else ()))
        require(all(type(binding[key]) is int and binding[key] > 0 for key in ('run_id', 'attempt'))
            and TX.matches(r'[a-f0-9]{40}', binding['source_commit'])
            and (source is None or binding['source_commit'] == source) and binding['workflow_path'] == workflow
            and (binding['event'] == 'push' if name.endswith('ci') else binding['event'] in ('workflow_dispatch', 'workflow_run')))
    rc = policy['bindings']['rc']
    require(rc['run_id'] == request['candidate']['rc_run_id'] and rc['attempt'] == request['candidate']['rc_attempt'])


def inspect_run(get_json, binding, policy, now):
    """No successful historical run may hide a newer failing run or rerun."""
    run = get_json(ROOT+'/actions/runs/'+str(binding['run_id']))
    workflow_name = binding['workflow_path'].rsplit('/', 1)[1]
    workflow = get_json(ROOT+'/actions/workflows/'+workflow_name)
    require(type(run.get('id')) is int and run['id'] == binding['run_id']
        and type(run.get('run_attempt')) is int and run['run_attempt'] == binding['attempt']
        and run.get('head_sha') == binding['source_commit'] and run.get('head_branch') == 'main'
        and run.get('event') == binding['event'] and run.get('path') == binding['workflow_path']
        and run.get('status') == 'completed' and run.get('conclusion') == 'success'
        and workflow.get('state') == 'active' and workflow.get('path') == binding['workflow_path']
        and type(workflow.get('id')) is int and run.get('workflow_id') == workflow['id']
        and all(run.get(key, {}).get('full_name') == AD.REPO
                and run[key].get('id') == policy['repository_id'] for key in ('repository', 'head_repository')),
        'run/workflow provenance mismatch')
    require(0 <= (now-PLAN.utc(run.get('updated_at'))).total_seconds() <= policy['max_evidence_age_seconds'], 'evidence expired')
    if binding['event'] == 'workflow_dispatch':
        require(run.get('actor', {}).get('login') == AD.OWNER and run.get('triggering_actor', {}).get('login') == AD.OWNER)
    query = '?branch=main&head_sha='+binding['source_commit']
    if binding['event'] == 'push':
        query += '&event=push'
    query += '&per_page=1'
    latest = get_json(ROOT+'/actions/workflows/'+workflow_name+'/runs'+query).get('workflow_runs')
    require(type(latest) is list and len(latest) == 1 and latest[0].get('id') == binding['run_id']
        and type(latest[0].get('run_attempt')) is int and latest[0]['run_attempt'] == binding['attempt'], 'superseded run or attempt')
    jobs = get_json(ROOT+'/actions/runs/'+str(binding['run_id'])+'/attempts/'+str(binding['attempt'])+'/jobs?per_page=100')
    require(type(jobs.get('total_count')) is int and type(jobs.get('jobs')) is list
        and 0 < jobs['total_count'] == len(jobs['jobs']) <= 100
        and all(job.get('status') == 'completed' and job.get('conclusion') == 'success' for job in jobs['jobs']),
        'missing, failed, skipped or unknown jobs')
    return run


def inspect_files(get_json, commit, expected):
    for path, digest in expected.items():
        value = get_json(ROOT+'/contents/'+path+'?ref='+commit)
        require(value.get('type') == 'file' and value.get('path') == path and value.get('encoding') == 'base64'
            and type(value.get('size')) is int and 0 < value['size'] <= 2097152)
        require(type(value.get('content')) is str and len(value['content']) <= 3000000)
        raw = base64.b64decode(value['content'].replace('\n', ''), validate=True)
        require(len(raw) == value['size'] and TX.sha(raw) == digest, 'reviewed control file drift')


def inspect_source_split(get_json, request, policy):
    source, control = request['candidate']['source_commit'], request['control_commit']
    require(source != control, 'split protocol requires distinct source and control; legacy main admission remains available')
    value = get_json(ROOT+'/compare/'+source+'...'+control)
    files, commits = value.get('files'), value.get('commits')
    require(value.get('status') == 'ahead' and value.get('merge_base_commit', {}).get('sha') == source
        and value.get('base_commit', {}).get('sha') == source
        and type(commits) is list and type(value.get('total_commits')) is int
        and 0 < len(commits) == value['total_commits'] <= 250
        and all(TX.matches(r'[a-f0-9]{40}', item.get('sha')) for item in commits)
        and commits[-1]['sha'] == control and type(files) is list and len(files) < 300,
        'incomplete or divergent source/control comparison')
    projection = []
    for item in files:
        require(control_path(item.get('filename')) and item.get('status') in ('added', 'modified')
            and TX.matches(r'[a-f0-9]{40}', item.get('sha')) and not item.get('previous_filename'),
            'application source changed; rebuild RC and affected acceptance')
        projection.append({key: item[key] for key in ('filename', 'status', 'sha')})
    paths = [item['filename'] for item in projection]
    require(len(set(paths)) == len(paths) and set(paths) == set(policy['allowed_control_changes'])
        and TX.sha(TX.encoded(dict(commits=[item['sha'] for item in commits], files=projection))) == policy['source_control_diff_sha256'],
        'unreviewed source/control difference')


def zip_documents(raw, names):
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        entries = archive.infolist()
        require(len(entries) == len(names) and {item.filename for item in entries} == set(names)
            and all(not item.is_dir() and not item.flag_bits & 1 and item.file_size <= 2097152 for item in entries)
            and sum(item.file_size for item in entries) <= 4194304, 'unsafe or incomplete artifact inventory')
        return {name: archive.read(name) for name in names}


def inspect_isolation(files, policy, request):
    """Reuse the reviewed fixed rehearsal validators; v1 supports its exact RC.

    A new candidate needs its own real isolation evidence and reviewed validator
    identity, not a policy field that can reinterpret the fixed historical proof.
    """
    spec = importlib.util.spec_from_file_location('qualified_isolation', Path(__file__).parent/'ci/pg03-isolated-rehearsal.py')
    action = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(action)
    candidate, rc, isolated = request['candidate'], policy['bindings']['rc'], policy['bindings']['isolation']
    require(candidate['source_commit'] == action.SOURCE and candidate['image_ref'] == action.IMAGE
        and candidate['binary_sha256'] == action.BINARY and candidate['rc_run_id'] == int(action.RC)
        and rc['artifact_id'] == int(action.ARTIFACT) and rc['archive_sha256'] == action.ARCHIVE_SHA
        and request['baseline_schema_sha256'] == action.BASELINE, 'unsupported fixed isolation identity')
    result, receipt = AD.document(files['result.json'], 2097152), AD.document(files['success.json'])
    require(result.get('schema') == 'clearapi.isolated-actions-rehearsal/v1'
        and result.get('control_commit') == isolated['source_commit']
        and result.get('workflow_ref') == AD.REPO+'/'+isolated['workflow_path']+'@refs/heads/main'
        and result.get('run_id') == str(isolated['run_id']) and result.get('run_attempt') == str(isolated['attempt'])
        and result.get('candidate_source') == candidate['source_commit'] and result.get('candidate_image') == candidate['image_ref']
        and result.get('rc_run') == str(rc['run_id']) and type(result.get('rc_attempt')) is int and result['rc_attempt'] == rc['attempt']
        and result.get('artifact_id') == str(rc['artifact_id']) and result.get('archive_sha256') == rc['archive_sha256']
        and result.get('baseline_schema_sha256') == request['baseline_schema_sha256']
        and result.get('control_sha256') == policy['isolation_control_files_sha256']
        and result.get('production_observation_verified') is False and result.get('production_acceptance') == 'not_run'
        and result.get('conclusion') == 'success' and result.get('prior_job_status') == 'success'
        and result.get('rehearsal') == 'passed' and result.get('success_receipt_present') is True
        and result.get('auth_removed') is True and result.get('final_cleanup', {}).get('complete') is True
        and result.get('steps') == dict.fromkeys(('prepare', 'baseline', 'pull', 'rehearse', 'finalize'), 'passed'),
        'isolation identity or finalization mismatch')
    observation = result.get('observation', {})
    require(observation.get('candidate') == dict(image_id=candidate['image_id'], binary_sha256=candidate['binary_sha256'],
        image_ref=candidate['image_ref'], source_commit=candidate['source_commit']) and observation.get('cleanup_complete') is True)
    action.validate_negative_probe(result.get('negative_migration', {}))
    action.validate_persisted_migrations(observation)
    for index, startup in enumerate(observation['startups'], 1):
        require(startup.get('pass_number') == index and startup.get('healthy') is True
            and startup.get('before_schema_sha256') == action.BASELINE and startup.get('after_schema_sha256') == action.BASELINE
            and all(type(startup.get(key)) is int and startup[key] == 0 for key in ('create_exit_code', 'start_exit_code', 'stop_exit_code'))
            and startup.get('process') == dict(Status='exited', Running=False, ExitCode=0, OOMKilled=False, error_present=False))
    cleanup = observation.get('cleanup', []) + result['negative_migration']['cleanup']
    require(len(cleanup) == 6 and len({item.get('container_id') for item in cleanup}) == 6
        and all(TX.matches(r'[a-f0-9]{64}', item.get('container_id')) and item.get('removed') is True for item in cleanup))
    identity = dict(image_id=candidate['image_id'], binary_sha256=candidate['binary_sha256'], commit=candidate['source_commit'])
    PLAN.SCHEMA.validate_receipt(receipt, identity, request['baseline_schema_sha256'])
    require(receipt['target_schema_sha256'] == request['baseline_schema_sha256']
        and receipt.get('successor_baseline') == dict(schema='clearapi.successor-schema-baseline/v1',
            rc_archive_sha256=rc['archive_sha256'], schema_source='verified_rc_target',
            observed_schema_sha256=request['baseline_schema_sha256'], production_observation_verified=False,
            production_acceptance='not_run', producer_sha256=policy['isolation_control_files_sha256']['scripts/pg03-successor-plan.py'],
            schema_helper_sha256=policy['isolation_control_files_sha256']['scripts/pg03-schema-rehearsal.py']))


def qualify(get_json, get_bytes, policy_provider, request, now=None):
    """Read-only, short-lived qualification; never an execution permit.

    Revalidation of the SAME unconsumed request is intentional for preflight.
    Cross-request replay is rejected by hash/nonce, while the separately approved
    executor must durably consume its grant under lock before any production IO.
    """
    require(all(callable(value) for value in (get_json, get_bytes, policy_provider)), 'trusted providers required')
    clock = now or (lambda: datetime.now(timezone.utc))
    raw_request = TX.encoded(request)
    request = TX.validate_request(raw_request, clock())
    policy = policy_provider()
    validate_policy(policy, request, clock())
    frozen = TX.encoded(policy)
    # Policy cannot mutate while transport callbacks run.
    policy = AD.document(frozen)
    controls = AD.inspect_server_controls(get_json, request)
    require(controls['repository_id'] == policy['repository_id'], 'repository identity changed')
    inspect_source_split(get_json, request, policy)
    inspect_files(get_json, request['control_commit'], policy['control_files_sha256'])
    inspect_files(get_json, request['candidate']['source_commit'], policy['source_workflow_files_sha256'])
    inspect_files(get_json, policy['bindings']['isolation']['source_commit'], policy['isolation_control_files_sha256'])
    runs = {key: inspect_run(get_json, binding, policy, clock()) for key, binding in policy['bindings'].items()}
    rc = policy['bindings']['rc']
    rc_raw = AD.verified_artifact(get_json, get_bytes, rc)
    files = zip_documents(rc_raw, ('gateway-image-release.json', 'binary-build-info.txt', 'schema-rehearsal.json',
        'schema-rehearsal.target.sql', 'artifact-hashes.json'))
    inventory = AD.document(files['artifact-hashes.json'])
    require(set(inventory) == set(files)-{'artifact-hashes.json'}
        and all(TX.sha(files[key]) == value for key, value in inventory.items()), 'RC payload hash mismatch')
    # Reuse the existing production manifest semantic checks without invoking its
    # current-main admission function or any production entry point.
    spec = importlib.util.spec_from_file_location('qualification_release', Path(__file__).with_name('pg03-release-control.py'))
    release = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(release)
    rc_run = dict(runs['rc'], verified_source_ci=runs['source_ci'])
    artifact = get_json(ROOT+'/actions/artifacts/'+str(rc['artifact_id']))
    manifest, manifest_hash = release.release_manifest(dict(commit=request['candidate']['source_commit'],
        digest=request['candidate']['image_digest'], rc_run=rc['run_id']), rc_run,
        dict(artifacts=[artifact]), lambda identity: rc_raw)
    require(manifest['new_api_image_id'] == request['candidate']['image_id']
        and manifest['binary_sha256'] == request['candidate']['binary_sha256']
        and manifest.get('production_ready') is False and manifest.get('production_acceptance') == 'not_run')
    schema = AD.document(files['schema-rehearsal.json'])
    require(schema == manifest['schema_transition']
        and PLAN.SCHEMA.schema_digest(files['schema-rehearsal.target.sql'].decode()) == request['baseline_schema_sha256']
        and schema.get('target_schema_sha256') == request['baseline_schema_sha256'])
    PLAN.SCHEMA.validate_receipt(schema, dict(image_id=request['candidate']['image_id'],
        binary_sha256=request['candidate']['binary_sha256'], commit=request['candidate']['source_commit']), schema.get('source_schema_sha256'))
    isolation = policy['bindings']['isolation']
    isolated_raw = AD.verified_artifact(get_json, get_bytes, isolation)
    inspect_isolation(zip_documents(isolated_raw, ('result.json', 'success.json')), policy, request)
    # Source/control may diverge intentionally, but selected versions, latest run
    # attempts, main, active policy and revocation state may never drift mid-read.
    for key, binding in policy['bindings'].items():
        require(inspect_run(get_json, binding, policy, clock()) == runs[key], 'run changed during qualification')
        if key in ('rc', 'isolation'):
            current = get_json(ROOT+'/actions/artifacts/'+str(binding['artifact_id']))
            require(current.get('expired') is False and current.get('digest') == 'sha256:'+binding['archive_sha256'])
    require(AD.inspect_server_controls(get_json, request) == controls, 'server controls drift')
    current_policy = policy_provider()
    validate_policy(current_policy, request, clock())
    require(TX.encoded(current_policy) == frozen, 'policy changed during qualification')
    TX.validate_request(raw_request, clock())
    return dict(schema='clearapi.candidate-qualification/v1', status='PASS', execution_allowed=False,
        production_authorization='BLOCKED', policy_id=policy['policy_id'], policy_sha256=TX.sha(frozen),
        request_sha256=TX.sha(raw_request), transaction_id=request['transaction_id'], repository=AD.REPO,
        repository_id=policy['repository_id'], candidate=request['candidate'], control_commit=request['control_commit'],
        issued_at_utc=clock().isoformat().replace('+00:00', 'Z'), expires_at_utc=policy['expires_at_utc'],
        manifest_sha256=manifest_hash, evidence=policy['bindings'],
        control_files_sha256=policy['control_files_sha256'], production_acceptance='NOT RUN')


if __name__ == '__main__':
    raise SystemExit('CANDIDATE QUALIFICATION BLOCKED: authenticated policy and GET providers required; no production entry')
