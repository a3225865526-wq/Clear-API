#!/usr/bin/env python3
"""Successor transaction kernel; no production adapter or mutation CLI exists.

The injected trusted adapter must authenticate GitHub authority, verify receipt
provenance, collect live facts and enforce the approved column-level write scope.
JSON claims never implement those duties. Until that adapter is implemented and
reviewed, this module can only validate local contracts and run isolated fixtures.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import secrets
import sys

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('successor_plan', Path(__file__).with_name('pg03-successor-plan.py'))
PLAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PLAN)
GATEWAY = 'f8f963a7df5f5114a02a7660e18121411c6aa5f284924d60b514a638dc10d24f'
OPERATIONS = ('prepare_runtime', 'replace_gateway', 'verify_startup', 'verify_restart', 'commit_successor')
EVIDENCE = frozenset(('production_observation', 'schema_rehearsal', 'candidate_gate_bundle',
                     'backup_completion', 'restore_acceptance', 'startup_write_scope'))


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


def sha(value):
    return hashlib.sha256(value).hexdigest()


def exact(value, keys):
    PLAN.require(type(value) is dict and set(value) == set(keys), 'unexpected transaction fields')


def matches(pattern, value):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def validate_request(raw, now):
    PLAN.require(type(raw) is bytes and len(raw) <= 32768, 'bounded request bytes required')
    request = json.loads(raw, object_pairs_hook=PLAN.unique)
    exact(request, ('schema', 'transaction_id', 'issued_at_utc', 'expires_at_utc', 'control_commit',
                    'candidate', 'evidence_sha256', 'baseline_schema_sha256', 'operations',
                    'failed_run', 'frozen_sha256', 'previous_success'))
    PLAN.require(request['schema'] == 'clearapi.successor-transaction-request/v1'
                 and matches(r'[a-f0-9]{32}', request['transaction_id'])
                 and matches(r'[a-f0-9]{40}', request['control_commit']), 'invalid transaction identity')
    issued, expires = PLAN.utc(request['issued_at_utc']), PLAN.utc(request['expires_at_utc'])
    PLAN.require(now.tzinfo is not None and issued <= now < expires
                 and 0 < (expires - issued).total_seconds() <= 300, 'transaction lease expired or invalid')
    candidate = request['candidate']
    exact(candidate, ('source_commit', 'image_digest', 'image_ref', 'image_id', 'binary_sha256',
                      'rc_run_id', 'rc_attempt'))
    PLAN.require(matches(r'[a-f0-9]{40}', candidate['source_commit'])
                 and candidate['source_commit'] != PLAN.FAILED_COMMIT
                 and matches(r'sha256:[a-f0-9]{64}', candidate['image_digest'])
                 and candidate['image_digest'] not in (PLAN.FAILED_DIGEST, PLAN.PREVIOUS_DIGEST)
                 and candidate['image_ref'] == 'ghcr.io/a3225865526-wq/clearapi@' + candidate['image_digest']
                 and matches(r'sha256:[a-f0-9]{64}', candidate['image_id'])
                 and matches(r'[a-f0-9]{64}', candidate['binary_sha256'])
                 and all(type(candidate[key]) is int and candidate[key] > 0
                         for key in ('rc_run_id', 'rc_attempt')), 'invalid candidate binding')
    exact(request['evidence_sha256'], EVIDENCE)
    PLAN.require(all(matches(r'[a-f0-9]{64}', value) for value in request['evidence_sha256'].values())
                 and matches(r'[a-f0-9]{64}', request['baseline_schema_sha256']), 'missing evidence binding')
    PLAN.require(request['operations'] == list(OPERATIONS) and request['failed_run'] == PLAN.FAILED_RUN
                 and request['frozen_sha256'] == PLAN.FROZEN
                 and request['previous_success'] == dict(source_commit=PLAN.PREVIOUS_COMMIT,
                                                         image_digest=PLAN.PREVIOUS_DIGEST),
                 'incident or operation scope mismatch')
    return request


def validate_binding(request, raw, now):
    PLAN.require(encoded(request) == encoded(validate_request(raw, now)),
                 'approved transaction data changed')


def validate_observation(value, request, now, gateway, running, baseline=None, restarted=False):
    """Strict non-secret projection; collection and provenance belong to adapter."""
    exact(value, ('schema', 'observed_at_utc', 'frozen_sha256', 'gateway_container_id', 'gateway_running',
                  'executor_container_id', 'executor_process_epoch', 'ingress', 'public_web_listeners',
                  'database_schema_sha256', 'protected_state_sha256', 'startup_write_scope_sha256',
                  'candidate_identity', 'process_started_ticks', 'migration_challenge', 'migration_proof_sha256',
                  'runtime_evidence_sha256', 'restart_evidence_sha256'))
    age = (now - PLAN.utc(value['observed_at_utc'])).total_seconds()
    PLAN.require(value['schema'] == 'clearapi.successor-live-projection/v1' and 0 <= age <= 60
                 and value['frozen_sha256'] == PLAN.FROZEN and value['gateway_container_id'] == gateway
                 and value['gateway_running'] is running
                 and value['executor_container_id'] == PLAN.EXECUTOR_CONTAINER
                 and matches(r'[a-f0-9]{32}', value['executor_process_epoch'])
                 and value['ingress'] == 'isolated_stopped_restart_no'
                 and value['public_web_listeners'] == []
                 and value['database_schema_sha256'] == request['baseline_schema_sha256']
                 and value['startup_write_scope_sha256'] == request['evidence_sha256']['startup_write_scope']
                 and matches(r'[a-f0-9]{64}', value['protected_state_sha256']), 'live projection mismatch')
    if baseline is not None:
        PLAN.require(all(value[key] == baseline[key] for key in
                         ('protected_state_sha256', 'executor_process_epoch')), 'protected state changed')
    if running:
        PLAN.require(value['candidate_identity'] == request['candidate']
                     and all(type(value['candidate_identity'][key]) is int for key in ('rc_run_id', 'rc_attempt'))
                     and matches(r'[1-9][0-9]*', value['process_started_ticks'])
                     and all(matches(r'[a-f0-9]{64}', value[key]) for key in
                             ('migration_challenge', 'migration_proof_sha256', 'runtime_evidence_sha256'))
                     and (matches(r'[a-f0-9]{64}', value['restart_evidence_sha256']) if restarted
                          else value['restart_evidence_sha256'] is None), 'postflight evidence incomplete')
    else:
        PLAN.require(all(value[key] is None for key in ('candidate_identity', 'process_started_ticks',
                         'migration_challenge', 'migration_proof_sha256', 'runtime_evidence_sha256',
                         'restart_evidence_sha256')), 'stopped predecessor cannot claim postflight')


class Transaction:
    """One-use state engine with real durable records and an explicit trusted IO.

    No default production IO is supplied. authenticate() must perform current
    server-side checks and verify_evidence() must verify producers and restore,
    not merely accept the matching hashes in this request. The production adapter
    and its end-to-end authorization path remain a separate implementation gate.
    Each adapter call must enforce an absolute deadline and bounded cancellation;
    this kernel rechecks time but cannot interrupt an unbounded trusted callback.
    """
    def __init__(self, app, io=None, now=None):
        self.app = Path(app)
        self.io = io
        self.now = now or (lambda: datetime.now(timezone.utc))

    def execute(self, raw):
        request = validate_request(raw, self.now())
        PLAN.require(self.io is not None, 'production adapter not implemented')
        spec = importlib.util.spec_from_file_location('successor_lifecycle', Path(__file__).with_name('pg03-legacy-transition.py'))
        lifecycle = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(lifecycle)
        prefix = 'pg03-successor-' + request['transaction_id']
        attempt = self.app / (prefix + '-attempt.json')
        failure = self.app / (prefix + '-failure.json')
        result = self.app / (prefix + '-result.json')
        stages = [self.app / (prefix + '-' + str(number) + '-' + phase + '.json')
                  for number, phase in enumerate(OPERATIONS, start=1)]
        binding = dict(request_sha256=sha(raw), transaction_id=request['transaction_id'])
        with self.io.deployment_lock():
            PLAN.require(not any(path.exists() or path.is_symlink() for path in (attempt, failure, result, *stages)),
                         'transaction already consumed; never retry unknown outcomes')
            try:
                self.io.authenticate(request, binding['request_sha256'])
                validate_binding(request, raw, self.now())
                self.io.verify_evidence(request)
                validate_binding(request, raw, self.now())
                before = self.io.observe(request)
                validate_binding(request, raw, self.now())
            except Exception:
                raise PLAN.Blocked('successor preflight authority or evidence unavailable') from None
            validate_observation(before, request, self.now(), GATEWAY, False)
            before = json.loads(encoded(before))
            PLAN.require(sha(encoded(before)) == request['evidence_sha256']['production_observation'],
                         'approved observation mismatch')
            validate_binding(request, raw, self.now())
            # No adapter mutation can occur until this exclusive claim is durable.
            lifecycle.publish(attempt, dict(schema='clearapi.successor-attempt/v1', **binding,
                status='reserved_outcome_unknown', request=request, public_traffic='blocked'))
            phase, mutated, gateway, previous = 'reserved', False, None, None
            context = dict(request_sha256=binding['request_sha256'],
                           migration_challenge=secrets.token_hex(32), gateway_container_id=None)
            try:
                for number, phase in enumerate(OPERATIONS, start=1):
                    validate_binding(request, raw, self.now())
                    self.io.authenticate(request, binding['request_sha256'])
                    validate_binding(request, raw, self.now())
                    # Adapter methods are a fixed internal interface, never input
                    # commands or method names selected by request JSON.
                    if phase == 'prepare_runtime':
                        mutated = True  # Failure/timeout may already have changed state.
                        self.io.prepare_runtime(request, context)
                    elif phase == 'replace_gateway':
                        gateway = self.io.replace_gateway(request, context)
                        PLAN.require(matches(r'[a-f0-9]{64}', gateway)
                                     and gateway not in (GATEWAY, PLAN.EXECUTOR_CONTAINER), 'new Gateway identity required')
                        context['gateway_container_id'] = gateway
                    elif phase in ('verify_startup', 'verify_restart'):
                        if phase == 'verify_restart':
                            context['migration_challenge'] = secrets.token_hex(32)
                        observed = (self.io.verify_startup(request, context) if phase == 'verify_startup'
                                    else self.io.verify_restart(request, context))
                        validate_binding(request, raw, self.now())
                        validate_observation(observed, request, self.now(), gateway, True, before,
                                             restarted=phase == 'verify_restart')
                        PLAN.require(observed['migration_challenge'] == context['migration_challenge'],
                                     'migration proof replay')
                        if previous is not None:
                            PLAN.require(observed['process_started_ticks'] != previous['process_started_ticks']
                                         and observed['migration_proof_sha256'] != previous['migration_proof_sha256'],
                                         'restart must prove a distinct process and completion')
                        previous = json.loads(encoded(observed))
                    else:
                        context['successor_manifest_sha256'] = self.io.commit_successor(request, context)
                        validate_binding(request, raw, self.now())
                        PLAN.require(matches(r'[a-f0-9]{64}', context['successor_manifest_sha256']),
                                     'successor manifest persistence unproved')
                        final = self.io.observe(request)
                        validate_binding(request, raw, self.now())
                        validate_observation(final, request, self.now(), gateway, True, before, restarted=True)
                        PLAN.require(all(final[key] == previous[key] for key in
                                         ('process_started_ticks', 'migration_challenge', 'migration_proof_sha256')),
                                     'postflight process changed during commit')
                    validate_binding(request, raw, self.now())
                    lifecycle.publish(self.app / (prefix + '-' + str(number) + '-' + phase + '.json'),
                        dict(schema='clearapi.successor-stage/v1', **binding, phase=phase, status='completed'))
                    validate_binding(request, raw, self.now())
                receipt = dict(schema='clearapi.successor-result/v1', **binding,
                    status='controlled_successor_verified_ingress_blocked', candidate=request['candidate'],
                    completed_at_utc=self.now().isoformat().replace('+00:00', 'Z'),
                    successor_manifest_sha256=context['successor_manifest_sha256'],
                    gateway_container_id=gateway, process_started_ticks=previous['process_started_ticks'],
                    migration_proof_sha256=previous['migration_proof_sha256'],
                    runtime_evidence_sha256=previous['runtime_evidence_sha256'],
                    restart_evidence_sha256=previous['restart_evidence_sha256'],
                    original_deployment_status='failed', previous_success=request['previous_success'],
                    public_traffic='blocked', database_restored=False, executor_started=False,
                    production_adapter_acceptance='not_established_by_kernel_tests')
                validate_binding(request, raw, self.now())
                lifecycle.publish(result, receipt)
                validate_binding(request, raw, self.now())
                return receipt
            except BaseException:
                # Containment must use the original approved request even if an
                # adapter accidentally mutated a nested Python input object.
                request = json.loads(raw, object_pairs_hook=PLAN.unique)
                contained = False
                if mutated:
                    try:
                        containment = self.io.contain(request, context)
                        expected = dict(frozen_sha256=PLAN.FROZEN,
                            executor_container_id=PLAN.EXECUTOR_CONTAINER,
                            executor_process_epoch=before['executor_process_epoch'],
                            ingress='isolated_stopped_restart_no', public_web_listeners=[],
                            successor_gateway_stopped=True, database_restored=False, executor_started=False)
                        contained = (type(containment) is dict and containment == expected
                                     and containment['successor_gateway_stopped'] is True
                                     and containment['database_restored'] is False
                                     and containment['executor_started'] is False)
                    except BaseException:
                        pass
                # If this publication also fails, the durable attempt still
                # consumes the request. A result file alone never proves success:
                # its consumer must authenticate the producer's final success.
                lifecycle.publish(failure, dict(schema='clearapi.successor-failure/v1', **binding,
                    phase=phase, outcome='unknown_after_possible_mutation', containment_verified=contained,
                    mutation_may_have_occurred=mutated,
                    public_traffic='blocked' if contained else 'UNKNOWN', automatic_retry=False))
                raise PLAN.Blocked('successor failed; preserve consumed attempt and isolation') from None

    def read_result(self, raw, producer_returncode):
        """Inspect a completed producer; no file by itself grants authority.

        The caller must separately authenticate that producer and its actual exit
        outcome. This read-only contract check always returns execution_allowed
        false, including when every local fixture record matches.
        """
        PLAN.require(type(producer_returncode) is int and producer_returncode == 0,
                     'actual successful producer outcome required')
        PLAN.require(self.io is not None and type(raw) is bytes and len(raw) <= 32768,
                     'trusted storage adapter and bounded request required')
        supplied = json.loads(raw, object_pairs_hook=PLAN.unique)
        PLAN.require(type(supplied) is dict and matches(r'[a-f0-9]{32}', supplied.get('transaction_id')),
                     'invalid transaction identity')
        prefix = 'pg03-successor-' + supplied['transaction_id']
        spec = importlib.util.spec_from_file_location('successor_result_storage', Path(__file__).with_name('pg03-legacy-transition.py'))
        storage = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(storage)
        with self.io.deployment_lock():
            failure = self.app / (prefix + '-failure.json')
            PLAN.require(not failure.exists() and not failure.is_symlink(), 'conflicting failure evidence')
            raw_result = storage.read_file(self.app / (prefix + '-result.json'))
            result = json.loads(raw_result, object_pairs_hook=PLAN.unique)
            exact(result, ('schema', 'request_sha256', 'transaction_id', 'status', 'candidate', 'completed_at_utc',
                'successor_manifest_sha256', 'gateway_container_id', 'process_started_ticks', 'migration_proof_sha256',
                'runtime_evidence_sha256', 'restart_evidence_sha256', 'original_deployment_status', 'previous_success',
                'public_traffic', 'database_restored', 'executor_started', 'production_adapter_acceptance'))
            completed = PLAN.utc(result['completed_at_utc'])
            request = validate_request(raw, completed)
            PLAN.require(completed <= self.now(), 'future completion evidence')
            binding = dict(request_sha256=sha(raw), transaction_id=request['transaction_id'])
            attempt = storage.read_json(self.app / (prefix + '-attempt.json'))
            PLAN.require(attempt == dict(schema='clearapi.successor-attempt/v1', **binding,
                status='reserved_outcome_unknown', request=request, public_traffic='blocked')
                and encoded(attempt['request']) == encoded(request), 'attempt binding mismatch')
            PLAN.require(result['schema'] == 'clearapi.successor-result/v1'
                and all(result[key] == value for key, value in binding.items())
                and result['status'] == 'controlled_successor_verified_ingress_blocked'
                and encoded(result['candidate']) == encoded(request['candidate'])
                and result['original_deployment_status'] == 'failed'
                and result['previous_success'] == request['previous_success']
                and result['public_traffic'] == 'blocked' and result['database_restored'] is False
                and result['executor_started'] is False
                and result['production_adapter_acceptance'] == 'not_established_by_kernel_tests'
                and matches(r'[a-f0-9]{64}', result['gateway_container_id'])
                and result['gateway_container_id'] not in (GATEWAY, PLAN.EXECUTOR_CONTAINER)
                and matches(r'[1-9][0-9]*', result['process_started_ticks'])
                and all(matches(r'[a-f0-9]{64}', result[key]) for key in
                        ('successor_manifest_sha256', 'migration_proof_sha256', 'runtime_evidence_sha256',
                         'restart_evidence_sha256')), 'result binding mismatch')
            for number, phase in enumerate(OPERATIONS, start=1):
                stage = storage.read_json(self.app / (prefix + '-' + str(number) + '-' + phase + '.json'))
                PLAN.require(stage == dict(schema='clearapi.successor-stage/v1', **binding,
                                          phase=phase, status='completed'), 'missing or conflicting stage evidence')
            return dict(schema='clearapi.successor-result-inspection/v1', **binding,
                        result_sha256=sha(raw_result), execution_allowed=False,
                        production_acceptance='not_established_by_contract_validation')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check-request', type=Path, required=True)
    args = parser.parse_args()
    with args.check_request.open('rb') as stream:
        raw = stream.read(32769)
    validate_request(raw, datetime.now(timezone.utc))
    print(json.dumps(dict(schema='clearapi.successor-request-inspection/v1', request_sha256=sha(raw),
                          production_adapter='not_implemented', execution_allowed=False)))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('SUCCESSOR TRANSACTION BLOCKED: no production entry or execution permission')
