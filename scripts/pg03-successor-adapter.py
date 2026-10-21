#!/usr/bin/env python3
"""Successor adapter contracts and guarded integration; no production CLI.

The future adapter must supply an authenticated GET-only GitHub transport and a
separately reviewed production authorization policy. These checks do not invent
that approval protocol, approve business writes, or collect production facts.
"""
from datetime import datetime, timedelta, timezone
from contextlib import contextmanager
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import re
import zipfile

SPEC = importlib.util.spec_from_file_location('adapter_transaction', Path(__file__).with_name('pg03-successor-transaction.py'))
TX = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TX)
REPO = 'a3225865526-wq/Clear-API'
OWNER = 'a3225865526-wq'
REQUIRED = frozenset(('Backend vet, build, test, migration binaries', 'Frontend typecheck and test',
                      'Secret scan and forbidden files'))


def require(value, message='successor adapter contract incomplete'):
    TX.PLAN.require(value, message)


def document(raw, maximum=1048576):
    require(type(raw) is bytes and 0 < len(raw) <= maximum)
    value = json.loads(raw, object_pairs_hook=TX.PLAN.unique)
    require(type(value) is dict)
    return value


def hash_value(value):
    return TX.matches(r'[a-f0-9]{64}', value)


def exact_binding(value, request):
    require(TX.encoded(value) == TX.encoded(request['candidate']), 'candidate binding mismatch')


def inspect_server_controls(get_json, request):
    """Live GET provenance only. Never infers production approval from owner/CI."""
    require(callable(get_json), 'authenticated GET-only transport required')
    root = '/repos/'+REPO
    repo = get_json(root)
    require(repo.get('full_name') == REPO and repo.get('owner', {}).get('login') == OWNER
            and type(repo.get('id')) is int and repo['id'] > 0)
    main = get_json(root+'/git/ref/heads/main')
    require(main.get('object', {}).get('sha') == request['control_commit'])
    protection = get_json(root+'/branches/main/protection')
    checks = protection.get('required_status_checks', {})
    require(checks.get('strict') is True and REQUIRED <= set(checks.get('contexts', []))
        and protection.get('enforce_admins', {}).get('enabled') is True
        and protection.get('allow_force_pushes', {}).get('enabled') is False
        and protection.get('allow_deletions', {}).get('enabled') is False)
    for key in ('CLEARAPI_AUTO_RC_ENABLED', 'CLEARAPI_AUTO_PRODUCTION_ENABLED'):
        require(get_json(root+'/actions/variables/'+key).get('value') == 'false')
    # A second main read prevents accepting a stale source across collection.
    require(get_json(root+'/git/ref/heads/main').get('object', {}).get('sha') == request['control_commit'])
    return dict(schema='clearapi.successor-server-controls/v1', repository_id=repo['id'],
        control_commit=request['control_commit'], execution_allowed=False,
        production_authorization='requires_separately_reviewed_server_approval_protocol')


def verified_artifact(get_json, get_bytes, binding):
    """Bind original archive bytes to successful same-repo main workflow attempt.

    The trusted policy chooses each expected workflow/source/run/artifact/hash;
    no embedded archive claim can select its own producer or confer authority.
    """
    TX.exact(binding, ('run_id', 'attempt', 'source_commit', 'workflow_path', 'event',
                       'artifact_id', 'artifact_name', 'archive_sha256'))
    require(all(type(binding[k]) is int and binding[k] > 0 for k in ('run_id', 'attempt', 'artifact_id'))
        and TX.matches(r'[a-f0-9]{40}', binding['source_commit']) and hash_value(binding['archive_sha256'])
        and TX.matches(r'\.github/workflows/[a-z0-9-]+\.yml', binding['workflow_path'])
        and binding['event'] in ('workflow_dispatch', 'workflow_run', 'push')
        and TX.matches(r'[A-Za-z0-9_.-]{1,128}', binding['artifact_name']))
    root = '/repos/'+REPO
    run = get_json(root+'/actions/runs/'+str(binding['run_id']))
    require(type(run.get('id')) is int and run['id'] == binding['run_id']
        and type(run.get('run_attempt')) is int and run['run_attempt'] == binding['attempt']
        and run.get('head_sha') == binding['source_commit'] and run.get('head_branch') == 'main'
        and run.get('path') == binding['workflow_path'] and run.get('event') == binding['event']
        and run.get('status') == 'completed' and run.get('conclusion') == 'success'
        and all(run.get(key, {}).get('full_name') == REPO for key in ('repository', 'head_repository')))
    if binding['event'] == 'workflow_dispatch':
        require(run.get('actor', {}).get('login') == OWNER and run.get('triggering_actor', {}).get('login') == OWNER)
    artifact = get_json(root+'/actions/artifacts/'+str(binding['artifact_id']))
    require(type(artifact.get('id')) is int and artifact['id'] == binding['artifact_id']
        and artifact.get('expired') is False and artifact.get('name') == binding['artifact_name']
        and type(artifact.get('size_in_bytes')) is int and 0 < artifact['size_in_bytes'] <= 10485760
        and artifact.get('digest') == 'sha256:'+binding['archive_sha256']
        and artifact.get('workflow_run', {}).get('id') == binding['run_id']
        and artifact['workflow_run'].get('head_sha') == binding['source_commit'])
    raw = get_bytes(root+'/actions/artifacts/'+str(binding['artifact_id'])+'/zip', 10485760)
    require(type(raw) is bytes and len(raw) == artifact['size_in_bytes']
        and hashlib.sha256(raw).hexdigest() == binding['archive_sha256'])
    # Recheck the producer after download, rejecting a later rerun attempt.
    require(get_json(root+'/actions/runs/'+str(binding['run_id'])) == run)
    return raw


def validate_write_scope(raw, request, now, observation):
    require(hashlib.sha256(raw).hexdigest() == request['evidence_sha256']['startup_write_scope'])
    value = document(raw)
    TX.exact(value, ('schema', 'candidate', 'observed_at_utc', 'expires_at_utc',
        'effective_configuration_sha256', 'baseline_state_sha256', 'operations',
        'background_writers', 'approval_ref', 'approved_by'))
    exact_binding(value['candidate'], request)
    # Break the scope <-> observation digest cycle: scope binds the observed
    # state without its scope reference; observation then binds exact scope.
    baseline = {key: item for key, item in observation.items() if key != 'startup_write_scope_sha256'}
    require(TX.sha(TX.encoded(observation)) == request['evidence_sha256']['production_observation']
        and observation.get('startup_write_scope_sha256') == request['evidence_sha256']['startup_write_scope'])
    require(value['schema'] == 'clearapi.successor-startup-write-scope/v2'
        and 0 <= (now-TX.PLAN.utc(value['observed_at_utc'])).total_seconds() <= 60
        and now < TX.PLAN.utc(value['expires_at_utc']) <= TX.PLAN.utc(request['expires_at_utc'])
        and hash_value(value['effective_configuration_sha256'])
        and value['baseline_state_sha256'] == TX.sha(TX.encoded(baseline))
        and value['approved_by'] == OWNER and type(value['approval_ref']) is str and 1 <= len(value['approval_ref']) <= 256)
    operations, writers = value['operations'], value['background_writers']
    require(type(operations) is list and 0 < len(operations) <= 512 and type(writers) is list and 0 < len(writers) <= 128)
    seen = set()
    for item in operations:
        TX.exact(item, ('database', 'table', 'operation', 'columns', 'selection_sha256', 'max_rows',
                        'before_rows_sha256', 'expected_after_rows_sha256'))
        require(item['database'] in ('main', 'log') and TX.matches(r'[a-z][a-z0-9_]{0,62}', item['table'])
            and item['operation'] in ('insert', 'update', 'delete', 'schema')
            and type(item['columns']) is list and 0 < len(item['columns']) <= 256
            and all(TX.matches(r'[a-z][a-z0-9_]{0,62}', column) for column in item['columns'])
            and len(set(item['columns'])) == len(item['columns'])
            and type(item['max_rows']) is int and 0 <= item['max_rows'] <= 1000000
            and all(hash_value(item[key]) for key in ('selection_sha256', 'before_rows_sha256', 'expected_after_rows_sha256')))
        key = (item['database'], item['table'], item['operation'])
        require(key not in seen)
        seen.add(key)
    seen = set()
    for item in writers:
        TX.exact(item, ('name', 'enabled', 'pending_count', 'approved_effect_sha256'))
        require(TX.matches(r'[a-z][a-z0-9_]{0,95}', item['name']) and item['name'] not in seen
            and type(item['enabled']) is bool and type(item['pending_count']) is int and item['pending_count'] >= 0
            and hash_value(item['approved_effect_sha256']))
        seen.add(item['name'])
    # The caller still must authenticate the approval and compare this complete
    # content to the actual candidate/configuration/all-writer inventory.
    return value


def validate_restore_acceptance(raw, request, backup_sha256):
    require(hashlib.sha256(raw).hexdigest() == request['evidence_sha256']['restore_acceptance']
        and backup_sha256 == request['evidence_sha256']['backup_completion'])
    value = document(raw)
    TX.exact(value, ('schema', 'candidate', 'backup_completion_sha256', 'encrypted_archive_sha256',
        'plaintext_archive_sha256', 'started_at_utc', 'completed_at_utc', 'profile', 'postgres_version_num',
        'production_connections', 'network', 'host_mounts', 'private_key_recorded', 'checks', 'producer_exit_code'))
    exact_binding(value['candidate'], request)
    require(value['schema'] == 'clearapi.successor-restore-acceptance/v1'
        and hash_value(backup_sha256) and value['backup_completion_sha256'] == backup_sha256
        and all(hash_value(value[k]) for k in ('encrypted_archive_sha256', 'plaintext_archive_sha256'))
        and TX.PLAN.utc(value['started_at_utc']) < TX.PLAN.utc(value['completed_at_utc'])
        and value['profile'] == 'independent_postgres_restore_no_production_access'
        and value['postgres_version_num'] == '170006' and value['production_connections'] == []
        and value['network'] == 'none' and value['host_mounts'] == [] and value['private_key_recorded'] is False
        and type(value['producer_exit_code']) is int and value['producer_exit_code'] == 0)
    expected = {'decrypt', 'archive_inventory', 'archive_payload_hashes', 'real_pg_restore', 'data',
                'indexes', 'constraints', 'uniqueness', 'pg03_state', 'separate_log_database', 'cleanup'}
    require(type(value['checks']) is dict and set(value['checks']) == expected
            and all(value['checks'][key] == 'pass' for key in expected))
    # Semantic validation is not provenance: no "pass" flag by itself proves a
    # real restore. Only a separately authenticated producer can supply this.
    return value


def evidence_documents(get_json, get_bytes, bindings, request):
    """Original ZIP provenance plus exact document bytes for all six dependencies."""
    names = dict(production_observation='production-observation.json', schema_rehearsal='schema-rehearsal.json',
        candidate_gate_bundle='candidate-gate-bundle.json', backup_completion='backup-completion.json',
        restore_acceptance='restore-acceptance.json', startup_write_scope='startup-write-scope.json')
    TX.exact(bindings, names)
    result = {}
    for key, name in names.items():
        archive = verified_artifact(get_json, get_bytes, bindings[key])
        with zipfile.ZipFile(io.BytesIO(archive)) as saved:
            entries = saved.infolist()
            require(0 < len(entries) <= 8 and len({item.filename for item in entries}) == len(entries)
                and sum(item.file_size for item in entries) <= 4194304
                and all(not item.is_dir() and '/' not in item.filename and '\\' not in item.filename
                    and not item.flag_bits & 1 and item.file_size <= 2097152 for item in entries))
            require(name in {item.filename for item in entries})
            with saved.open(name) as source:
                raw = source.read(2097153)
            require(len(raw) <= 2097152 and hashlib.sha256(raw).hexdigest() == request['evidence_sha256'][key])
            result[key] = document(raw, 2097152)
    # These are inspected documents, not authorization or completed production
    # acceptance. Actual producer contracts/installations still need review.
    return result


def gateway_command_plan(compose_files):
    """Compile fixed local argv for review; never invokes them or selects image.

    A production adapter must independently qualify these exact files (including
    the candidate image overlay), their effective config and all writers under
    lock before using the bounded executor. No defaults can silently select old
    V2 recovery or start a dependency/Executor/edge service.
    """
    require(type(compose_files) is tuple and 2 <= len(compose_files) <= 4
        and len(set(compose_files)) == len(compose_files)
        and all(TX.matches(r'/opt/clearapi/app/[a-zA-Z0-9_.-]+\.(yaml|yml)', path) for path in compose_files))
    prefix = ['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'compose',
              '--project-directory', '/opt/clearapi/app']
    for path in compose_files:
        prefix += ['-f', path]
    return dict(execution_allowed=False,
        prepare_runtime=['/usr/bin/python3', '-IB', '/opt/clearapi/bin/runtime-identity-refresh.py',
            '--prepare-only', '--directory', '/run/clearapi'],
        replace_gateway=prefix+['up', '--no-start', '--no-deps', '--no-build', '--pull', 'never', '--timeout', '150', 'new-api'],
        start_gateway=prefix+['start', '--wait', '--wait-timeout', '150', 'new-api'],
        restart_gateway=prefix+['restart', '--no-deps', '--timeout', '150', 'new-api'],
        contain_gateway=prefix+['stop', '--timeout', '150', 'new-api'])


def production_grant(get_json, get_bytes, policy, request, qualification, now):
    """Authenticate a separate, exact, environment-reviewed authorization.

    policy is obtained from the administrator's trusted policy source, never
    request JSON. No environment-policy fallback or owner-dispatch shortcut is
    accepted. The producer workflow must be separately installed and reviewed;
    this repository does not register an executable production entry.
    """
    TX.exact(policy, ('schema', 'request_sha256', 'qualification_policy_sha256',
        'environment_id', 'production_approvers', 'approval_artifact', 'revoked', 'consumed'))
    require(policy['schema'] == 'clearapi.successor-production-policy/v1'
        and policy['revoked'] is False and policy['consumed'] is False
        and type(policy['environment_id']) is int and policy['environment_id'] > 0
        and policy['request_sha256'] == TX.sha(TX.encoded(request))
        and policy['qualification_policy_sha256'] == qualification['policy_sha256'])
    approvers = policy['production_approvers']
    require(type(approvers) is list and 0 < len(approvers) <= 16
        and len(set(approvers)) == len(approvers)
        and all(TX.matches(r'[A-Za-z0-9-]{1,39}', name) and name != OWNER for name in approvers),
        'independent reviewed approver identities required')
    binding = policy['approval_artifact']
    require(binding['source_commit'] == request['control_commit']
        and binding['workflow_path'] == '.github/workflows/pg03-successor-approval.yml'
        and binding['event'] == 'workflow_dispatch')
    root = '/repos/'+REPO
    environment = get_json(root+'/environments/production')
    require(environment.get('id') == policy['environment_id'] and environment.get('name') == 'production'
        and environment.get('deployment_branch_policy') == dict(protected_branches=False, custom_branch_policies=True))
    rules = environment.get('protection_rules', [])
    reviewers = [rule for rule in rules if rule.get('type') == 'required_reviewers']
    require(len(reviewers) == 1 and reviewers[0].get('prevent_self_review') is True
        and all(any(row.get('type') == 'User' and row.get('reviewer', {}).get('login') == name
                    for row in reviewers[0].get('reviewers', [])) for name in approvers),
        'independent production review required')
    branches = get_json(root+'/environments/production/deployment-branch-policies')
    require(branches.get('total_count') == 1 and branches.get('branch_policies')
        and len(branches['branch_policies']) == 1
        and branches['branch_policies'][0].get('name') == 'main'
        and branches['branch_policies'][0].get('type') == 'branch')
    reviews_path = root+'/actions/runs/'+str(binding['run_id'])+'/approvals'
    reviews = get_json(reviews_path)
    require(type(reviews) is list and not any(row.get('state') == 'rejected' for row in reviews)
        and any(row.get('state') == 'approved' and row.get('user', {}).get('login') in approvers
            and any(env.get('id') == policy['environment_id'] and env.get('name') == 'production'
                    for env in row.get('environments', [])) for row in reviews), 'production review absent or rejected')
    require(get_json(root+'/actions/runs/'+str(binding['run_id'])).get('display_title')
        == 'PG03 grant '+policy['request_sha256'], 'approval activity request mismatch')
    archive = verified_artifact(get_json, get_bytes, binding)
    with zipfile.ZipFile(io.BytesIO(archive)) as saved:
        entries = saved.infolist()
        require(len(entries) == 1 and entries[0].filename == 'production-grant.json'
            and not entries[0].flag_bits & 1 and entries[0].file_size <= 32768)
        grant = document(saved.read(entries[0]), 32768)
    TX.exact(grant, ('schema', 'repository', 'request_sha256', 'transaction_id', 'candidate',
        'control_commit', 'evidence_sha256', 'qualification_policy_sha256', 'issued_at_utc',
        'expires_at_utc', 'nonce', 'operations', 'public_traffic', 'executor_start', 'database_restore'))
    exact_binding(grant['candidate'], request)
    require(grant['schema'] == 'clearapi.successor-production-grant/v1' and grant['repository'] == REPO
        and grant['request_sha256'] == policy['request_sha256']
        and grant['transaction_id'] == request['transaction_id']
        and grant['control_commit'] == request['control_commit']
        and grant['evidence_sha256'] == request['evidence_sha256']
        and grant['qualification_policy_sha256'] == qualification['policy_sha256']
        and grant['operations'] == list(TX.OPERATIONS) and TX.matches(r'[a-f0-9]{64}', grant['nonce'])
        and TX.PLAN.utc(request['issued_at_utc']) <= TX.PLAN.utc(grant['issued_at_utc']) <= now
        < TX.PLAN.utc(grant['expires_at_utc']) <= TX.PLAN.utc(request['expires_at_utc'])
        and grant['public_traffic'] == 'blocked' and grant['executor_start'] == 'forbidden'
        and grant['database_restore'] == 'forbidden', 'production grant mismatch or expired')
    require(get_json(reviews_path) == reviews
        and get_json(root+'/environments/production') == environment
        and get_json(root+'/environments/production/deployment-branch-policies') == branches,
        'production authority changed during verification')
    return grant


def validate_dependencies(documents, request, now):
    """Content checks supplement authenticated producer provenance, never replace it."""
    TX.exact(documents, TX.EVIDENCE)
    require(all(TX.sha(TX.encoded(value)) == request['evidence_sha256'][key]
                for key, value in documents.items()), 'canonical dependency digest mismatch')
    observation = documents['production_observation']
    TX.validate_observation(observation, request, now, TX.GATEWAY, False)
    receipt = documents['schema_rehearsal']
    candidate = request['candidate']
    TX.PLAN.SCHEMA.validate_receipt(receipt, dict(image_id=candidate['image_id'],
        binary_sha256=candidate['binary_sha256'], commit=candidate['source_commit']), request['baseline_schema_sha256'])
    require(type(receipt['startup_passes']) is int
        and receipt['target_schema_sha256'] == request['baseline_schema_sha256'])
    bundle = documents['candidate_gate_bundle']
    TX.exact(bundle, TX.PLAN.GATES)
    for name, gate in bundle.items():
        require(gate.get('schema') == 'clearapi.gate-evidence/v1' and gate.get('gate') == name
            and gate.get('conclusion') == 'success' and gate.get('source_commit') == candidate['source_commit']
            and gate.get('gateway_digest') == candidate['image_digest']
            and type(gate.get('rc_run_id')) is int and gate['rc_run_id'] == candidate['rc_run_id']
            and type(gate.get('rc_run_attempt')) is int and gate['rc_run_attempt'] == candidate['rc_attempt'])
    backup = documents['backup_completion']
    TX.exact(backup, ('schema', 'backup_id', 'manifest_sha256', 'encrypted_sha256', 'encrypted_bytes',
        'stopped_writer_identity', 'invocation_returncode', 'plaintext_cleanup', 'restore_acceptance',
        'production_execution_allowed'))
    require(backup['schema'] == 'clearapi.successor-backup-completion/v1'
        and TX.matches(r'\d{8}T\d{6}Z', backup['backup_id'])
        and all(hash_value(backup[key]) for key in ('manifest_sha256', 'encrypted_sha256'))
        and type(backup['encrypted_bytes']) is int and backup['encrypted_bytes'] > 0
        and type(backup['invocation_returncode']) is int and backup['invocation_returncode'] == 0
        and backup['plaintext_cleanup'] == 'verified_absent'
        and backup['restore_acceptance'] == 'not_run' and backup['production_execution_allowed'] is False)
    identity = backup['stopped_writer_identity']
    # The exact raw identity is independently checked by the host against the
    # current stopped container and manifest bytes under the deployment lock.
    require(type(identity) is dict and identity.get('identity_mode') == 'stopped_failed_gateway')
    restore = validate_restore_acceptance(TX.encoded(documents['restore_acceptance']), request,
                                          request['evidence_sha256']['backup_completion'])
    require(restore['encrypted_archive_sha256'] == backup['encrypted_sha256']
        and TX.PLAN.utc(restore['completed_at_utc']) <= now, 'restore archive or completion mismatch')
    scope = validate_write_scope(TX.encoded(documents['startup_write_scope']), request, now, observation)
    return dict(observation=observation, backup=backup, restore=restore, scope=scope)


class ProductionAdapter:
    """Coordinate the kernel with explicit trusted authority and host ports.

    Construction is inert. There is no credential loader, default host, SSH or
    enable flag. An installed host port must enforce bounded calls, actual live
    collection and write projection; a fixture is not production acceptance.
    authenticate and every operation revalidate the independent server grant,
    candidate policy and live exclusive window. Transaction owns the durable
    one-use claim and result records. No operation restores a DB or opens ingress.
    """
    def __init__(self, *, get_json=None, get_bytes=None, candidate_policy=None,
                 production_policy=None, evidence_bindings=None, host=None, window=None,
                 qualify=None, now=None):
        self.get_json, self.get_bytes = get_json, get_bytes
        self.candidate_policy, self.production_policy = candidate_policy, production_policy
        self.evidence_bindings, self.host, self.window = evidence_bindings, host, window
        self.qualify = qualify
        self.now = now or (lambda: datetime.now(timezone.utc))
        self.binding = self.grant = self.dependencies = None
        self.policies = None
        self.failed = False

    @contextmanager
    def deployment_lock(self):
        require(self.host is not None and self.window is not None, 'production host/window unbound')
        with self.host.deployment_lock():
            yield

    def authenticate(self, request, request_sha):
        try:
            require(not self.failed and all(callable(value) for value in (self.get_json, self.get_bytes,
                self.candidate_policy, self.production_policy, self.evidence_bindings, self.qualify)),
                'production authority unbound or failed')
            TX.validate_request(TX.encoded(request), self.now())
            require(request_sha == TX.sha(TX.encoded(request)))
            if self.binding is not None:
                require(self.binding == request_sha, 'adapter request replay or drift')
            candidate_policy, production_policy = self.candidate_policy(), self.production_policy()
            policies = TX.encoded([candidate_policy, production_policy])
            require(self.policies is None or self.policies == policies, 'authorization policy drift')
            qualification = self.qualify(self.get_json, self.get_bytes, self.candidate_policy, request, now=self.now)
            TX.exact(qualification, ('schema', 'status', 'execution_allowed', 'production_authorization',
                'policy_id', 'policy_sha256', 'request_sha256', 'transaction_id', 'repository', 'repository_id',
                'candidate', 'control_commit', 'issued_at_utc', 'expires_at_utc', 'manifest_sha256', 'evidence',
                'control_files_sha256', 'production_acceptance'))
            require(qualification['schema'] == 'clearapi.candidate-qualification/v1'
                and qualification['status'] == 'PASS' and qualification['execution_allowed'] is False
                and qualification['production_authorization'] == 'BLOCKED'
                and qualification['production_acceptance'] == 'NOT RUN'
                and qualification['repository'] == REPO and type(qualification['repository_id']) is int
                and qualification['repository_id'] > 0
                and qualification['policy_sha256'] == TX.sha(TX.encoded(candidate_policy))
                and qualification['policy_id'] == candidate_policy['policy_id']
                and qualification['evidence'] == candidate_policy['bindings']
                and qualification['control_files_sha256'] == candidate_policy['control_files_sha256']
                and qualification['request_sha256'] == request_sha
                and qualification['transaction_id'] == request['transaction_id']
                and TX.encoded(qualification['candidate']) == TX.encoded(request['candidate'])
                and qualification['control_commit'] == request['control_commit']
                and 0 <= (self.now()-TX.PLAN.utc(qualification['issued_at_utc'])).total_seconds() <= 60
                and self.now() < TX.PLAN.utc(qualification['expires_at_utc']) <= TX.PLAN.utc(request['expires_at_utc'])
                and hash_value(qualification['manifest_sha256']), 'candidate qualification failed or drifted')
            controls = inspect_server_controls(self.get_json, request)
            require(controls['repository_id'] == qualification['repository_id'])
            grant = production_grant(self.get_json, self.get_bytes, production_policy, request, qualification, self.now())
            require(self.grant is None or TX.encoded(self.grant) == TX.encoded(grant), 'production grant drift')
            self.window.check(request)
            # Installed host authenticates the sole executor, frozen entry routes,
            # lock ownership, effective Compose/files and exact candidate image.
            require(self.host.authorize(request, grant, self.deadline(request)) is True,
                    'production host installation or executor unverified')
            require(TX.encoded([self.candidate_policy(), self.production_policy()]) == policies)
            TX.validate_request(TX.encoded(request), self.now())
            self.binding, self.grant, self.policies = request_sha, grant, policies
        except BaseException:
            self.failed = True
            raise TX.PLAN.Blocked('production authority unavailable, changed or expired') from None

    def deadline(self, request):
        return min(TX.PLAN.utc(request['expires_at_utc']), self.now() + timedelta(seconds=30))

    def verify_evidence(self, request):
        try:
            self.authenticate(request, TX.sha(TX.encoded(request)))
            bindings = self.evidence_bindings()
            docs = evidence_documents(self.get_json, self.get_bytes, bindings, request)
            checked = validate_dependencies(docs, request, self.now())
            # GitHub provenance cannot prove a producer measured real production
            # or restored data. Host verifies those sources, current stopped
            # identity, all-writer/configuration and row/column scope under lock.
            require(self.host.verify_dependencies(request, docs, self.deadline(request)) is True,
                    'production dependency source unverified')
            require(TX.encoded(self.evidence_bindings()) == TX.encoded(bindings), 'evidence producer drift')
            self.authenticate(request, TX.sha(TX.encoded(request)))
            self.dependencies = checked
        except BaseException:
            self.failed = True
            raise TX.PLAN.Blocked('production dependencies incomplete or unknown') from None

    def observe(self, request):
        try:
            self.authenticate(request, TX.sha(TX.encoded(request)))
            value = self.host.observe(request, self.deadline(request))
            self.authenticate(request, TX.sha(TX.encoded(request)))
            return value
        except BaseException:
            self.failed = True
            raise TX.PLAN.Blocked('production observation unavailable') from None

    def operation(self, name, request, context):
        try:
            require(name in TX.OPERATIONS and self.dependencies is not None, 'preflight dependencies missing')
            self.authenticate(request, TX.sha(TX.encoded(request)))
            result = self.window.perform(request, name, self.host.invoke, request, dict(context), self.deadline(request))
            self.authenticate(request, TX.sha(TX.encoded(request)))
            TX.exact(result, ('schema', 'operation', 'request_sha256', 'status', 'value'))
            require(result['schema'] == 'clearapi.successor-host-result/v1' and result['operation'] == name
                and result['request_sha256'] == self.binding and result['status'] == 'PASS',
                'production operation failed or unknown')
            return result['value']
        except BaseException:
            self.failed = True
            raise TX.PLAN.Blocked('production operation failed or unknown; contain without retry') from None

    def prepare_runtime(self, request, context):
        require(self.operation('prepare_runtime', request, context) is None)

    def replace_gateway(self, request, context):
        return self.operation('replace_gateway', request, context)

    def verify_startup(self, request, context):
        return self.operation('verify_startup', request, context)

    def verify_restart(self, request, context):
        return self.operation('verify_restart', request, context)

    def commit_successor(self, request, context):
        return self.operation('commit_successor', request, context)

    def contain(self, request, context):
        # Recovery cannot require a still-valid lease or a live GitHub/window:
        # this one fixed method only stops the successor and preserves isolation.
        require(self.host is not None and self.binding == TX.sha(TX.encoded(request)))
        self.failed = True
        return self.host.contain(request, dict(context), self.now() + timedelta(seconds=30))


if __name__ == '__main__':
    raise SystemExit('SUCCESSOR ADAPTER BLOCKED: no production entry or approval protocol')
