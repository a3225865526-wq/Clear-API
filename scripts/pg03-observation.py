"""PG03 continuous read-only observation contract; no production transports.

Trusted readers must authenticate sources and enforce bounded cancellation. Input
JSON, hashes, and a PASS string do not establish that trust. A serialized report
never authorizes execution; adapters must retain and recheck the live window.
"""
import copy
from datetime import datetime, timezone
import hashlib
import json
import math
import re
import threading
import time

REPOSITORY = 'a3225865526-wq/Clear-API'
PERIOD = {'github': 15.0, 'host': 2.0, 'database': 2.0, 'authority': 2.0}
TTL = {'github': 30.0, 'host': 5.0, 'database': 5.0, 'authority': 5.0}
ROUTES = frozenset(('actions_all_refs', 'dispatch_rerun_approval', 'upstream_events',
    'ghcr_verify', 'manual_clearadmin', 'manual_clearapi_deploy', 'console_root',
    'external_credentials', 'systemd_cron_at', 'other_agents'))
FIELDS = {
    'github': frozenset(('workflow_inventory_sha256', 'entry_freeze_sha256',
                         'control_commit', 'auto_enabled', 'unapproved_active_runs', 'unapproved_pending_deployments')),
    'host': frozenset(('runtime_sha256', 'protected_runtime_sha256', 'candidate_identity',
                       'effective_config_sha256', 'tool_inventory_sha256',
                       'executor_epoch', 'executor_id', 'gateway_id', 'gateway_running',
                       'ingress', 'public_listeners', 'writer_routes', 'unique_executor')),
    'database': frozenset(('schema_sha256', 'protected_state_sha256', 'pending_work_sha256',
                           'startup_write_scope_intent_sha256', 'database_identity_sha256', 'unapproved_writers')),
    'authority': frozenset(('identity_sha256', 'permission_sha256', 'lease_id', 'executor_id',
                            'revoked', 'connected', 'exclusive_lock_owner')),
}


class ObservationClosed(ValueError):
    pass


def sha(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':'),
                                    ensure_ascii=False, allow_nan=False).encode()).hexdigest()


def matches(pattern, value):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def intent_sha256(request):
    """Stable approval scope, excluding only future-produced evidence and lease.

    Historical backup/restore/schema references, transaction/incident identities,
    candidate and operations stay bound. Final evidence and final short lease
    must be independently authenticated once, after continuous observation.
    """
    value = copy.deepcopy(request)
    for key in ('issued_at_utc', 'expires_at_utc'):
        value.pop(key, None)
    evidence = value.get('evidence_sha256')
    if type(evidence) is dict:
        for key in ('production_observation', 'startup_write_scope', 'candidate_gate_bundle'):
            evidence.pop(key, None)
    return sha(value)


def scope_intent_sha256(scope):
    return sha({key: value for key, value in scope.items()
                if key not in ('observed_at_utc', 'expires_at_utc', 'baseline_state_sha256')})


class LiveObservationProof:
    """In-memory preflight proof; cannot be recreated from a JSON PASS report."""
    def __init__(self, window, projection):
        self._window = window
        self._projection = copy.deepcopy(projection)
        self._execution_claimed = False
        self._request_sha256 = None
        self._qualification_sha256 = None
        self._scope = None

    @property
    def projection(self):
        return copy.deepcopy(self._projection)

    def attach_scope(self, scope):
        window = self._window
        window.check()
        window.require(self._scope is None and self._request_sha256 is None, 'scope_replay')
        baseline = {key: value for key, value in self._projection.items() if key != 'startup_write_scope_sha256'}
        window.require(type(scope) is dict
            and scope.get('schema') == 'clearapi.successor-startup-write-scope/v2'
            and scope.get('candidate') == window.binding['candidate']
            and scope.get('baseline_state_sha256') == sha(baseline)
            and scope.get('effective_configuration_sha256') == window.projections['host']['effective_config_sha256']
            and scope_intent_sha256(scope) == window.projections['database']['startup_write_scope_intent_sha256'],
            'scope_intent_or_baseline_mismatch')
        self._scope = copy.deepcopy(scope)
        self._projection['startup_write_scope_sha256'] = sha(scope)

    def bind(self, request, qualification_sha256):
        """Seal final request after fresh facts, scope and qualification exist."""
        window = self._window
        window.check()
        window.require(type(request) is dict and type(request.get('evidence_sha256')) is dict,
                       'final_intent_or_observation_mismatch')
        with window._claim_lock:
            window.require(self._request_sha256 is None, 'final_request_replay')
            # Consume before the external final authority call. UNKNOWN is final.
            self._request_sha256 = sha(request)
        window.require(self._scope is not None
            and intent_sha256(request) == window.binding['intent_sha256']
            and request.get('candidate') == window.binding['candidate']
            and request.get('control_commit') == window.binding['control_commit']
            and type(request.get('evidence_sha256')) is dict
            and request['evidence_sha256'].get('production_observation') == sha(self._projection)
            and request['evidence_sha256'].get('startup_write_scope') == sha(self._scope)
            and matches(r'[a-f0-9]{64}', qualification_sha256), 'final_intent_or_observation_mismatch')
        try:
            window.require(callable(window.final_authority)
                and window.final_authority(copy.deepcopy(window.binding), copy.deepcopy(request),
                    qualification_sha256, self.projection) is True, 'final_authority_denied')
        except ObservationClosed:
            raise
        except BaseException:
            window.fail('final_authority_unknown')
        self._qualification_sha256 = qualification_sha256
        window.trace.append(dict(request_sha256=self._request_sha256,
                                 qualification_sha256=qualification_sha256))
        self.check(request)

    def check(self, request, request_sha256=None):
        self._window.require(self._window.claimed, 'unclaimed_observation')
        self._window.check()
        binding = self._window.binding
        self._window.require(type(request) is dict and type(request.get('evidence_sha256')) is dict,
                             'request_observation_mismatch')
        if request_sha256 is None:
            request_sha256 = sha(request)
        self._window.require(self._qualification_sha256 is not None
            and request_sha256 == self._request_sha256 and sha(request) == self._request_sha256
            and request.get('candidate') == binding['candidate']
            and request.get('control_commit') == binding['control_commit']
            and request.get('evidence_sha256', {}).get('production_observation') == sha(self._projection),
            'request_observation_mismatch')
        try:
            issued, expires = (datetime.fromisoformat(request[key].replace('Z', '+00:00'))
                               for key in ('issued_at_utc', 'expires_at_utc'))
            self._window.require(issued.tzinfo == timezone.utc and expires.tzinfo == timezone.utc
                and 0 < (expires - issued).total_seconds() <= 300
                and issued.timestamp() <= self._window.wall() < expires.timestamp()
                and expires.timestamp() <= binding['expires_at'], 'final_request_expired')
            self._window.require(callable(self._window.final_authority)
                and self._window.final_authority(copy.deepcopy(binding), copy.deepcopy(request),
                    self._qualification_sha256, self.projection) is True, 'final_authority_denied')
        except ObservationClosed:
            raise
        except BaseException:
            self._window.fail('final_authority_unknown')
        return copy.deepcopy(self._projection)

    def perform(self, request, name, action, *args):
        """Preflight baseline never approves its own future runtime transitions.

        An installed, independently reviewed host window must implement the
        stage-specific transition monitor. No caller flag or supplied callable
        turns this local preflight proof into that production executor.
        """
        self.check(request)
        self._window.fail('production_transition_monitor_not_installed')


class ObservationWindow:
    """Single-use live capability, distinct from the successor mutation executor.

    Readers: (binding, challenge) -> non-secret envelope. authority(binding) must
    authenticate current approval/identity and reserve(binding) must durably claim
    the observation_id across processes/reboots before the first read. Callbacks
    are trusted host code, never selectable from request JSON. This module does
    not supply their production implementation or accept stored snapshot inputs.
    """
    def __init__(self, binding, *, readers=None, authority=None, reserve=None, final_authority=None,
                 clock=time.monotonic, wall=time.time):
        self.binding = copy.deepcopy(binding)
        self._binding_hash = sha(binding)
        self.readers, self.authority, self.reserve = readers, authority, reserve
        self.final_authority = final_authority
        self.clock, self.wall = clock, wall
        self.origin, self.wall_origin = clock(), wall()
        self.failed, self.started, self.claimed = None, False, False
        self.samples, self.baselines, self.first, self.counts = {}, {}, {}, {}
        self.projections = {}
        self.trace = []
        self._claim_lock = threading.Lock()
        self._authority_lock = threading.RLock()
        self._last_clock = self.origin
        try:
            required = {'schema', 'repository', 'candidate', 'control_commit', 'intent_sha256',
                        'qualification_intent_sha256', 'observation_id', 'identity_sha256', 'permission_sha256',
                        'lease_id', 'executor_id', 'issued_at', 'expires_at', 'coverage'}
            self.require(type(binding) is dict and set(binding) == required, 'binding_fields')
            self.require(binding['schema'] == 'clearapi.continuous-observation-binding/v2'
                and binding['repository'] == REPOSITORY
                and matches(r'[a-f0-9]{40}', binding['control_commit'])
                and matches(r'[a-f0-9]{32}', binding['observation_id'])
                and all(matches(r'[a-f0-9]{64}', binding[k]) for k in
                    ('intent_sha256', 'qualification_intent_sha256', 'identity_sha256', 'permission_sha256'))
                and all(matches(r'[A-Za-z0-9_.:-]{1,128}', binding[k]) for k in
                    ('lease_id', 'executor_id'))
                and binding['coverage'] == {k: sorted(v) for k, v in FIELDS.items()}, 'binding_identity')
            candidate = binding['candidate']
            self.require(type(candidate) is dict
                and matches(r'[a-f0-9]{40}', candidate.get('source_commit'))
                and matches(r'sha256:[a-f0-9]{64}', candidate.get('image_digest'))
                and candidate.get('image_ref') == 'ghcr.io/a3225865526-wq/clearapi@' + candidate['image_digest']
                and all(type(candidate.get(k)) is int and candidate[k] > 0 for k in ('rc_run_id', 'rc_attempt')),
                'candidate_identity')
            self.require(all(type(binding[k]) in (int, float) and math.isfinite(binding[k])
                             for k in ('issued_at', 'expires_at'))
                and binding['issued_at'] <= self.wall_origin < binding['expires_at']
                and 60 < binding['expires_at'] - binding['issued_at'] <= 900, 'lease_bounds')
            self.require(type(readers) is dict and set(readers) == set(PERIOD)
                and all(callable(v) for v in readers.values()) and callable(authority)
                and callable(reserve), 'trusted_live_dependencies_missing')
        except ObservationClosed:
            raise
        except Exception:
            self.fail('invalid_binding')

    def fail(self, reason):
        self.failed = self.failed or reason
        raise ObservationClosed(self.failed)

    def require(self, condition, reason):
        if not condition:
            self.fail(reason)

    def check_authority(self):
        with self._authority_lock:
            self.require(self.failed is None, self.failed)
            now, wall_now = self.clock(), self.wall()
            self.require(math.isfinite(now) and now >= self._last_clock and math.isfinite(wall_now)
                and abs(wall_now - (self.wall_origin + now - self.origin)) <= 2, 'clock_discontinuity')
            self._last_clock = now
            self.require(sha(self.binding) == self._binding_hash, 'binding_changed')
            self.require(self.binding['issued_at'] <= wall_now < self.binding['expires_at'], 'lease_expired')
            try:
                self.require(self.authority(copy.deepcopy(self.binding)) is True, 'authority_denied')
            except ObservationClosed:
                raise
            except BaseException:
                self.fail('authority_unknown')

    def start(self):
        self.check_authority()
        with self._claim_lock:
            self.require(not self.started, 'observation_replay')
            # Consume in memory before external reservation: an interrupted or
            # failed reservation is UNKNOWN and cannot be retried on this object.
            self.started = True
            try:
                self.require(self.reserve(copy.deepcopy(self.binding)) is True, 'reservation_denied')
            except ObservationClosed:
                raise
            except BaseException:
                self.fail('reservation_unknown')
        self.check_authority()

    def poll(self, channel):
        return self._collect_projection(channel)

    def _collect_projection(self, channel, allowed=None):
        """One bounded trusted read. Cadence is measured from read start, never JSON."""
        self.check_authority()
        self.require(self.started and channel in PERIOD, 'invalid_poll')
        started = self.clock()
        prior = self.samples.get(channel)
        if prior:
            self.require(started > prior['started'] and started - prior['started'] <= PERIOD[channel] + 0.25,
                         'sample_replay_or_gap')
        # An unpredictable challenge prevents a trusted producer reusing a stored response.
        import secrets
        challenge = secrets.token_hex(32)
        try:
            row = self.readers[channel](copy.deepcopy(self.binding), challenge)
            finished = self.clock()
            self.check_authority()
            self.require(0 <= finished - started <= TTL[channel], 'collection_timeout')
            self.require(type(row) is dict and set(row) == {'schema', 'binding_sha256', 'challenge',
                'channel', 'status', 'complete', 'unknown', 'projection'}
                and row['schema'] == 'clearapi.observation-sample/v1'
                and row['binding_sha256'] == self._binding_hash and row['challenge'] == challenge
                and row['channel'] == channel and row['status'] == 'PASS'
                and row['complete'] is True and row['unknown'] == [], 'untrusted_or_incomplete_sample')
            value = row['projection']
            self.require(type(value) is dict and set(value) == FIELDS[channel], 'coverage_incomplete')
            for key, item in value.items():
                if key.endswith('_sha256'):
                    self.require(matches(r'[a-f0-9]{64}', item), 'missing_fingerprint')
            if channel == 'github':
                self.require(value['control_commit'] == self.binding['control_commit']
                    and value['auto_enabled'] is False and value['unapproved_active_runs'] == []
                    and value['unapproved_pending_deployments'] == [], 'github_not_frozen')
            elif channel == 'host':
                self.require(matches(r'[a-f0-9]{32}', value['executor_epoch'])
                    and value['executor_id'] == self.binding['executor_id']
                    and matches(r'[a-f0-9]{64}', value['gateway_id'])
                    and (value['gateway_running'] is False if allowed is None
                         else type(value['gateway_running']) is bool) and value['public_listeners'] == []
                    and value['ingress'] == 'isolated_stopped_restart_no'
                    and value['unique_executor'] is True
                    and value['writer_routes'] == {key: 'frozen' for key in ROUTES}, 'host_not_exclusive')
            elif channel == 'database':
                # During startup a writer is approved only after the trusted
                # reader attributes its connection to the exact candidate and
                # enforces the approved row/column/operation scope. Unknown
                # connections and out-of-scope writes belong in this collection.
                self.require(value['unapproved_writers'] == [], 'database_writers_present')
            else:
                self.require(all(value[key] == self.binding[key] for key in
                    ('identity_sha256', 'permission_sha256', 'lease_id', 'executor_id'))
                    and value['revoked'] is False and value['connected'] is True
                    and value['exclusive_lock_owner'] == self.binding['executor_id'], 'identity_or_lock_lost')
            digest = sha(value)
            self.require((channel not in self.baselines or self.baselines[channel] == digest) if allowed is None
                         else (allowed(value) if callable(allowed)
                               else any(value == expected for expected in allowed)), 'state_drift')
            self.baselines.setdefault(channel, digest)
            self.projections[channel] = copy.deepcopy(value)
            self.first.setdefault(channel, finished)
            self.samples[channel] = dict(started=started, finished=finished, sha256=digest)
            self.counts[channel] = self.counts.get(channel, 0) + 1
            self.trace.append(dict(channel=channel, started=started, finished=finished,
                                   sample_sha256=sha(row), challenge=challenge))
        except ObservationClosed:
            raise
        except BaseException:
            self.fail('reader_unknown')

    def check(self, *, readiness=False):
        self.check_authority()
        now = self.clock()
        self.require(self.started, 'not_started')
        for channel in PERIOD:
            row = self.samples.get(channel)
            if row is None:
                self.require(readiness and now - self.origin <= TTL[channel], 'missing_channel')
            else:
                self.require(0 <= now - row['started'] <= TTL[channel], 'disconnected_or_stale')
        ready = (len(self.samples) == len(PERIOD)
                 and now - max(self.first.values(), default=now) >= 60
                 and all(self.counts.get(k, 0) >= math.floor(60 / PERIOD[k]) + 1 for k in PERIOD))
        self.require(ready or readiness, 'continuous_window_incomplete')
        return ready

    def claim(self, projection):
        """Claim once; executor must continue poll/check until observation closes.

        Does not invoke deployment. Durable cross-process replay protection is
        supplied by reserve(), and successor transaction still needs its own claim.
        """
        self.check()
        with self._claim_lock:
            self.require(not self.claimed, 'observation_claim_replay')
            self.claimed = True
        host, database = self.projections['host'], self.projections['database']
        self.require(type(projection) is dict
            and projection.get('gateway_container_id') == host['gateway_id']
            and projection.get('gateway_running') is False
            and projection.get('executor_container_id') == self.binding['executor_id']
            and projection.get('executor_process_epoch') == host['executor_epoch']
            and projection.get('ingress') == host['ingress']
            and projection.get('public_web_listeners') == host['public_listeners']
            and projection.get('database_schema_sha256') == database['schema_sha256']
            and projection.get('protected_state_sha256') == database['protected_state_sha256']
            and projection.get('startup_write_scope_sha256') is None,
            'projection_not_observed')
        # The final timestamp comes from completed trusted reads, never a caller
        # timestamp prepared before the 60-second observation interval.
        measured = min(self.samples[channel]['finished'] for channel in ('host', 'database'))
        projection = copy.deepcopy(projection)
        projection['observed_at_utc'] = datetime.fromtimestamp(
            self.wall_origin + measured - self.origin, timezone.utc).isoformat().replace('+00:00', 'Z')
        return LiveObservationProof(self, projection)

    def report(self):
        return dict(schema='clearapi.continuous-observation-report/v1',
            binding_sha256=self._binding_hash, status='FAIL' if self.failed else 'LOCAL_CONTRACT_ONLY',
            observation_id=self.binding['observation_id'], wall_origin=self.wall_origin,
            baseline_sha256=sha(self.baselines), channel_baselines=copy.deepcopy(self.baselines),
            samples=copy.deepcopy(self.counts),
            trace_sha256=sha(self.trace), execution_allowed=False,
            production_acceptance='not_established_by_local_contract')


class ExecutionWindow:
    """Approved exact runtime transitions, continuously frozen protected facts.

    trusted_policy is obtained from an independently authenticated host policy
    authority, never the request. It lists every admissible runtime projection,
    including intermediate states, before any action starts. Unknown intermediate
    states are rejected rather than incorporated as new baselines. No transport,
    command, privilege or production authorization is supplied by this class.
    """
    OPERATIONS = ('prepare_runtime', 'replace_gateway', 'verify_startup', 'verify_restart', 'commit_successor')
    MUTABLE = frozenset(('runtime_sha256', 'gateway_id', 'gateway_running'))

    def __init__(self, proof, trusted_policy):
        if type(proof) is not LiveObservationProof:
            raise ObservationClosed('live_preflight_capability_required')
        self.proof, self.window = proof, proof._window
        self.window.require(proof._qualification_sha256 is not None, 'final_request_unsealed')
        with self.window._claim_lock:
            self.window.require(not proof._execution_claimed, 'execution_capability_replay')
            proof._execution_claimed = True
        self.policy = copy.deepcopy(trusted_policy)
        self.index, self.active = 0, False
        self._lock = threading.RLock()
        self._operation_lock = threading.Lock()
        self._policy_hash = sha(self.policy)
        self._baseline = copy.deepcopy(self.window.projections)
        self._successor_id = None
        policy = self.policy
        self.window.require(type(policy) is dict and set(policy) ==
            {'schema', 'binding_sha256', 'request_sha256', 'operation_nonce', 'owner', 'stages'}
            and policy['schema'] == 'clearapi.execution-window-policy/v1'
            and policy['binding_sha256'] == self.window._binding_hash
            and policy['request_sha256'] == proof._request_sha256
            and policy['owner'] == self.window.binding['executor_id']
            and matches(r'[a-f0-9]{64}', policy['operation_nonce'])
            and type(policy['stages']) is list and len(policy['stages']) == len(self.OPERATIONS),
            'execution_policy_binding')
        previous = {key: self._baseline['host'][key] for key in self.MUTABLE}
        for operation, stage in zip(self.OPERATIONS, policy['stages']):
            self.window.require(type(stage) is dict and set(stage) == {'operation', 'before', 'after', 'during'}
                and stage['operation'] == operation and stage['before'] == previous
                and type(stage['during']) is list and 1 <= len(stage['during']) <= 8,
                'execution_policy_sequence')
            for state in (stage['before'], stage['after'], *stage['during']):
                self.window.require(type(state) is dict and set(state) == self.MUTABLE
                    and (matches(r'[a-f0-9]{64}', state['runtime_sha256'])
                         or state['runtime_sha256'] == '$candidate_gateway_runtime')
                    and (matches(r'[a-f0-9]{64}', state['gateway_id'])
                         or state['gateway_id'] == '$successor_gateway_id')
                    and type(state['gateway_running']) is bool, 'execution_policy_runtime')
            self.window.require(stage['before'] in stage['during'] and stage['after'] in stage['during'],
                                'execution_policy_transition_coverage')
            if operation in ('prepare_runtime', 'replace_gateway'):
                self.window.require(stage['after']['gateway_running'] is False
                    and all(state['gateway_running'] is False for state in stage['during']),
                    'premature_gateway_start')
            if operation == 'replace_gateway':
                self.window.require(stage['after']['gateway_id'] != stage['before']['gateway_id'],
                                    'successor_gateway_identity_required')
            if operation == 'prepare_runtime':
                self.window.require(all(state['gateway_id'] == previous['gateway_id']
                    and state['runtime_sha256'] != '$candidate_gateway_runtime' for state in stage['during']),
                    'preparation_cannot_replace_gateway')
            if operation in ('verify_startup', 'verify_restart', 'commit_successor'):
                self.window.require(stage['after']['gateway_running'] is True
                    and stage['after']['gateway_id'] == stage['before']['gateway_id'],
                    'unexpected_gateway_identity_or_state')
            previous = stage['after']

    def _expected(self, state):
        value = copy.deepcopy(self._baseline['host'])
        value.update(state)
        if state['gateway_id'] != self._baseline['host']['gateway_id']:
            value['candidate_identity'] = self.window.binding['candidate']
        return value

    def _host_matches(self, value, state):
        expected = self._expected(state)
        gateway = expected['gateway_id']
        if gateway == '$successor_gateway_id':
            if self._successor_id is None:
                if value['gateway_id'] in (self._baseline['host']['gateway_id'],
                                            self.window.binding['executor_id']):
                    return False
                expected['gateway_id'] = value['gateway_id']
            else:
                expected['gateway_id'] = self._successor_id
        if expected['runtime_sha256'] == '$candidate_gateway_runtime':
            # protected_runtime_sha256 independently covers every runtime field
            # outside the approved Gateway delta; only exact candidate state is
            # admissible here. The authenticated reader must establish identity.
            expected['runtime_sha256'] = value['runtime_sha256']
        return value == expected

    def _collect(self, channel, states):
        with self._lock:
            allowed = ((lambda value: any(self._host_matches(value, state) for state in states)) if channel == 'host'
                       else [self._baseline[channel]])
            self.window._collect_projection(channel, allowed)

    def _check_binding(self, request):
        self.window.require(sha(self.policy) == self._policy_hash, 'execution_policy_changed')
        # LiveObservationProof is a preflight capability. During execution its
        # immutable approved projection stays request-bound, while this policy
        # verifies the explicitly allowed successor runtime separately.
        self.proof.check(request)

    def poll(self, channel):
        """Poll an already-approved transition; no caller-selected state allowed."""
        self.window.require(self.active and self.index < len(self.OPERATIONS), 'poll_outside_operation')
        self._collect(channel, self.policy['stages'][self.index]['during'])

    def check(self, request):
        self._check_binding(request)
        self.window.require(not self.active, 'concurrent_execution_check')
        state = (self.policy['stages'][self.index]['before'] if self.index < len(self.OPERATIONS)
                 else self.policy['stages'][-1]['after'])
        for channel in PERIOD:
            self._collect(channel, [state])
        self._check_binding(request)
        return self.window.report()

    def perform(self, request, name, action, *args):
        """Run one fixed host operation with live monitoring; never retries it."""
        self.window.require(self._operation_lock.acquire(blocking=False), 'concurrent_operation')
        stop, threads, errors = threading.Event(), [], []
        try:
            self.window.require(self.index < len(self.OPERATIONS)
                and name == self.OPERATIONS[self.index] and callable(action), 'execution_order_or_replay')
            self.check(request)
            stage = self.policy['stages'][self.index]
            self.active = True

            def monitor(channel):
                try:
                    while not stop.wait(PERIOD[channel]):
                        self.poll(channel)
                        self._check_binding(request)
                except BaseException:
                    errors.append(channel)
                    stop.set()

            for channel in PERIOD:
                worker = threading.Thread(target=monitor, args=(channel,), daemon=True)
                threads.append(worker)
                worker.start()
            try:
                result = action(name, *args)
            except BaseException:
                self.window.fail('operation_outcome_unknown')
            finally:
                stop.set()
                join_deadline = time.monotonic() + 5
                for worker in threads:
                    worker.join(timeout=max(0, join_deadline - time.monotonic()))
            self.window.require(not errors and not any(worker.is_alive() for worker in threads),
                                'execution_monitor_unknown')
            self.window.require(type(result) is dict and set(result) ==
                {'schema', 'operation', 'request_sha256', 'status', 'value'}
                and result['schema'] == 'clearapi.successor-host-result/v1'
                and result['operation'] == name
                and result['request_sha256'] == self.proof._request_sha256
                and result['status'] == 'PASS', 'operation_result_unknown')
            if name == 'replace_gateway':
                self.window.require(matches(r'[a-f0-9]{64}', result.get('value'))
                    and result['value'] not in (self._baseline['host']['gateway_id'],
                                                 self.window.binding['executor_id']), 'successor_result_unknown')
                self._successor_id = result['value']
            self._check_binding(request)
            for channel in PERIOD:
                self._collect(channel, [stage['after']])
            self._check_binding(request)
            self.window.trace.append(dict(operation=name, policy_sha256=self._policy_hash,
                operation_nonce=self.policy['operation_nonce'], completed=self.window.clock()))
            self.index += 1
            return result
        except ObservationClosed:
            raise
        except BaseException:
            self.window.fail('execution_unknown')
        finally:
            self.active = False
            stop.set()
            self._operation_lock.release()
