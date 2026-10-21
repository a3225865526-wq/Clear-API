"""Deterministic local observation contracts; no transport or production facts."""
import copy
import importlib.util
import os
from pathlib import Path
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('observation', Path(__file__).resolve().parents[1] / 'pg03-observation.py')
O = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(O)


class ObservationTests(unittest.TestCase):
    def setUp(self):
        self.ticks = 0.0
        self.allowed = True
        self.reserved = set()
        self.candidate = dict(source_commit='1' * 40, image_digest='sha256:' + '2' * 64,
            image_ref='ghcr.io/a3225865526-wq/clearapi@sha256:' + '2' * 64,
            rc_run_id=123, rc_attempt=1)
        self.request_template = dict(candidate=self.candidate, control_commit='3' * 40,
                                     evidence_sha256=dict(schema_rehearsal='f' * 64))
        self.scope_template = dict(schema='clearapi.successor-startup-write-scope/v2', candidate=self.candidate,
            effective_configuration_sha256='a' * 64, operations=[], background_writers=[],
            approval_ref='fixture', approved_by='a3225865526-wq')
        self.binding = dict(schema='clearapi.continuous-observation-binding/v2', repository=O.REPOSITORY,
            candidate=self.candidate, control_commit='3' * 40, intent_sha256=O.intent_sha256(self.request_template),
            qualification_intent_sha256='5' * 64, observation_id='6' * 32, identity_sha256='7' * 64,
            permission_sha256='8' * 64, lease_id='local-fixture', executor_id='9' * 64,
            issued_at=1000, expires_at=1300, coverage={k: sorted(v) for k, v in O.FIELDS.items()})
        self.values = {key: {field: 'a' * 64 for field in fields} for key, fields in O.FIELDS.items()}
        self.values['github'].update(control_commit=self.binding['control_commit'], auto_enabled=False,
                                     unapproved_active_runs=[], unapproved_pending_deployments=[])
        self.values['host'].update(executor_epoch='b' * 32, executor_id=self.binding['executor_id'],
            gateway_id='c' * 64, gateway_running=False, ingress='isolated_stopped_restart_no',
            candidate_identity=None, public_listeners=[], unique_executor=True,
            writer_routes={key: 'frozen' for key in O.ROUTES})
        self.values['database']['unapproved_writers'] = []
        self.values['database']['startup_write_scope_intent_sha256'] = O.scope_intent_sha256(self.scope_template)
        self.values['authority'].update(identity_sha256=self.binding['identity_sha256'],
            permission_sha256=self.binding['permission_sha256'], lease_id=self.binding['lease_id'],
            executor_id=self.binding['executor_id'], revoked=False, connected=True,
            exclusive_lock_owner=self.binding['executor_id'])
        self.readers = {key: self.reader(key) for key in O.PERIOD}
        self.window = self.build()

    def reader(self, channel):
        def read(binding, challenge):
            return dict(schema='clearapi.observation-sample/v1', binding_sha256=O.sha(binding),
                challenge=challenge, channel=channel, status='PASS', complete=True, unknown=[],
                projection=copy.deepcopy(self.values[channel]))
        return read

    def reserve(self, binding):
        key = binding['observation_id']
        if key in self.reserved:
            return False
        self.reserved.add(key)
        return True

    def build(self, **changes):
        options = dict(readers=self.readers, authority=lambda _: self.allowed, reserve=self.reserve,
                       final_authority=self.final_authority,
                       clock=lambda: self.ticks, wall=lambda: 1000 + self.ticks)
        options.update(changes)
        return O.ObservationWindow(self.binding, **options)

    def observe(self, end=60):
        self.window.start()
        for tick in range(end + 1):
            self.ticks = float(tick)
            for channel, period in O.PERIOD.items():
                if tick % period == 0:
                    self.window.poll(channel)
            self.window.check(readiness=True)

    def projection(self):
        host, database = self.values['host'], self.values['database']
        return dict(gateway_container_id=host['gateway_id'], gateway_running=False,
            executor_container_id=host['executor_id'], executor_process_epoch=host['executor_epoch'],
            ingress=host['ingress'], public_web_listeners=[], database_schema_sha256=database['schema_sha256'],
            protected_state_sha256=database['protected_state_sha256'],
            startup_write_scope_sha256=None)

    def final_authority(self, binding, request, qualification_sha256, projection):
        return (self.allowed and qualification_sha256 == O.sha(self.qualification)
                and self.qualification['request_sha256'] == O.sha(request)
                and binding['intent_sha256'] == O.intent_sha256(request)
                and request['evidence_sha256']['production_observation'] == O.sha(projection))

    def seal(self, proof):
        scope = copy.deepcopy(self.scope_template)
        scope.update(observed_at_utc=proof.projection['observed_at_utc'], expires_at_utc='1970-01-01T00:21:40Z',
            baseline_state_sha256=O.sha({key: value for key, value in proof.projection.items()
                                         if key != 'startup_write_scope_sha256'}))
        proof.attach_scope(scope)
        request = copy.deepcopy(self.request_template)
        request.update(issued_at_utc=proof.projection['observed_at_utc'], expires_at_utc='1970-01-01T00:21:40Z')
        request['evidence_sha256'].update(production_observation=O.sha(proof.projection),
            startup_write_scope=O.sha(scope), candidate_gate_bundle='f' * 64)
        self.qualification = dict(request_sha256=O.sha(request), status='PASS')
        proof.bind(request, O.sha(self.qualification))
        self.request_sha = O.sha(request)
        return request

    def test_continuous_window_binds_live_projection_and_never_json_authority(self):
        self.observe()
        proof = self.window.claim(self.projection())
        request = self.seal(proof)
        self.assertEqual(proof.check(request, self.request_sha), proof.projection)
        self.assertEqual(proof.projection['observed_at_utc'], '1970-01-01T00:17:40Z')
        self.assertFalse(self.window.report()['execution_allowed'])
        self.assertEqual(self.window.report()['samples']['host'], 31)
        self.allowed = False
        with self.assertRaisesRegex(O.ObservationClosed, 'authority_denied'):
            proof.check(request, self.request_sha)

    def test_request_candidate_control_or_projection_replay_rejected(self):
        for key in ('candidate', 'control_commit', 'evidence_sha256', 'request_sha256'):
            with self.subTest(key=key):
                self.setUp()
                self.observe()
                projection = self.projection()
                proof = self.window.claim(projection)
                request = self.seal(proof)
                raw_sha = self.request_sha
                if key == 'request_sha256':
                    raw_sha = 'd' * 64
                else:
                    request[key] = None
                with self.assertRaisesRegex(O.ObservationClosed, 'request_observation_mismatch'):
                    proof.check(request, raw_sha)

    def test_preflight_proof_cannot_execute_a_production_action(self):
        self.observe()
        proof = self.window.claim(self.projection())
        request = self.seal(proof)
        invoked = []
        with self.assertRaisesRegex(O.ObservationClosed, 'production_transition_monitor_not_installed'):
            proof.perform(request, 'replace_gateway', lambda *_: invoked.append(True))
        self.assertEqual(invoked, [])

    def test_baseline_must_cover_live_database_and_scope(self):
        self.observe()
        value = self.projection()
        value['protected_state_sha256'] = 'd' * 64
        with self.assertRaisesRegex(O.ObservationClosed, 'projection_not_observed'):
            self.window.claim(value)

    def test_single_shot_cannot_establish_window(self):
        self.observe(end=0)
        with self.assertRaisesRegex(O.ObservationClosed, 'continuous_window_incomplete'):
            self.window.claim(self.projection())

    def test_sample_challenge_replay_rejected(self):
        self.window.start()
        stored = self.reader('host')(self.binding, 'old-challenge')
        self.readers['host'] = lambda *_: stored
        with self.assertRaisesRegex(O.ObservationClosed, 'untrusted_or_incomplete_sample'):
            self.window.poll('host')

    def test_unknown_or_missing_coverage_never_passes(self):
        cases = (('host', 'writer_routes', {}), ('database', 'unapproved_writers', ['unknown']),
                 ('authority', 'connected', False), ('github', 'auto_enabled', True))
        for channel, key, value in cases:
            with self.subTest(channel=channel):
                self.setUp()
                self.values[channel][key] = value
                self.window.start()
                with self.assertRaises(O.ObservationClosed):
                    self.window.poll(channel)

    def test_grant_workflow_name_and_owner_do_not_exempt_unapproved_activity(self):
        self.values['github']['unapproved_active_runs'] = [dict(
            name='PG03 successor independent grant', owner='a3225865526-wq')]
        self.window.start()
        with self.assertRaisesRegex(O.ObservationClosed, 'github_not_frozen'):
            self.window.poll('github')

    def test_fingerprint_drift_is_sticky(self):
        self.observe(end=0)
        self.ticks = 2
        self.values['database']['schema_sha256'] = 'd' * 64
        with self.assertRaisesRegex(O.ObservationClosed, 'state_drift'):
            self.window.poll('database')
        self.values['database']['schema_sha256'] = 'a' * 64
        with self.assertRaisesRegex(O.ObservationClosed, 'state_drift'):
            self.window.check(readiness=True)

    def test_poll_cadence_gap_and_reader_timeout_fail(self):
        self.observe(end=0)
        self.ticks = 3
        with self.assertRaisesRegex(O.ObservationClosed, 'sample_replay_or_gap'):
            self.window.poll('host')
        self.setUp()
        read = self.readers['host']
        def slow(*args):
            self.ticks += 6
            return read(*args)
        self.readers['host'] = slow
        self.window.start()
        with self.assertRaisesRegex(O.ObservationClosed, 'collection_timeout'):
            self.window.poll('host')

    def test_disconnect_expiry_clock_or_mutated_binding_fail(self):
        for fault in ('disconnect', 'expiry', 'clock', 'binding'):
            with self.subTest(fault=fault):
                self.setUp()
                self.observe(end=0)
                if fault == 'disconnect': self.ticks = 6
                elif fault == 'expiry': self.ticks = 301
                elif fault == 'clock': self.ticks = -1
                else: self.window.binding['candidate']['rc_attempt'] = 2
                with self.assertRaises(O.ObservationClosed):
                    self.window.check(readiness=True)

    def test_claim_and_cross_process_reservation_replay_fail(self):
        self.observe()
        self.window.claim(self.projection())
        with self.assertRaisesRegex(O.ObservationClosed, 'observation_claim_replay'):
            self.window.claim(self.projection())
        second = self.build()
        with self.assertRaisesRegex(O.ObservationClosed, 'reservation_denied'):
            second.start()

    def test_default_or_oneshot_dependencies_fail_closed(self):
        with self.assertRaises(O.ObservationClosed):
            O.ObservationWindow(self.binding)
        self.binding['coverage'] = {'semantics': 'A'}
        with self.assertRaises(O.ObservationClosed):
            self.build()

    def test_denied_exception_and_unknown_sources_are_closed(self):
        for source in ('reader', 'authority', 'reserve'):
            with self.subTest(source=source):
                self.setUp()
                def unavailable(*_):
                    raise PermissionError('fixture denial')
                if source == 'reader':
                    self.readers['host'] = unavailable
                else:
                    setattr(self.window, source, unavailable)
                with self.assertRaises(O.ObservationClosed):
                    self.window.start()
                    self.window.poll('host')

    def execution(self):
        self.observe()
        proof = self.window.claim(self.projection())
        request = self.seal(proof)
        before = {key: self.values['host'][key] for key in O.ExecutionWindow.MUTABLE}
        stages = []
        for operation in O.ExecutionWindow.OPERATIONS:
            after = dict(before)
            if operation == 'replace_gateway':
                after.update(gateway_id='$successor_gateway_id', runtime_sha256='$candidate_gateway_runtime')
            if operation == 'verify_startup':
                after['gateway_running'] = True
            stages.append(dict(operation=operation, before=before, after=after, during=[before, after]))
            before = after
        policy = dict(schema='clearapi.execution-window-policy/v1', binding_sha256=O.sha(self.binding),
            request_sha256=self.request_sha, operation_nonce='f' * 64,
            owner=self.binding['executor_id'], stages=stages)
        execution = O.ExecutionWindow(proof, policy)
        # A deterministic local clock advances for each actual read; background
        # readers are started/stopped normally without sleeps or timing assertions.
        def now():
            self.ticks += .001
            return self.ticks
        self.window.clock = now
        return execution, request

    def host_action(self, name, *_, guard=None):
        if guard is not None:
            guard()
        if name == 'replace_gateway':
            self.values['host'].update(gateway_id='d' * 64, runtime_sha256='e' * 64,
                                       candidate_identity=self.candidate)
        elif name == 'verify_startup':
            self.values['host']['gateway_running'] = True
        return dict(schema='clearapi.successor-host-result/v1', operation=name,
            request_sha256=self.request_sha, status='PASS',
            value='d' * 64 if name == 'replace_gateway' else None)

    def test_execution_all_stages_dynamic_gateway_and_frozen_baseline(self):
        execution, request = self.execution()
        for name in O.ExecutionWindow.OPERATIONS:
            result = execution.perform(request, name, self.host_action)
            self.assertEqual(result['operation'], name)
            execution.check(request)
        with self.assertRaisesRegex(O.ObservationClosed, 'execution_order_or_replay'):
            execution.perform(request, 'prepare_runtime', self.host_action)

    def test_execution_protected_or_unapproved_runtime_drift_closes(self):
        for fault in ('protected_runtime_sha256', 'executor_epoch', 'writer_routes', 'candidate_identity'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                execution.perform(request, 'prepare_runtime', self.host_action)
                def changed(name, *_, guard):
                    result = self.host_action(name, guard=guard)
                    self.values['host'][fault] = 'unapproved'
                    execution.poll('host')
                    return result
                with self.assertRaises(O.ObservationClosed):
                    execution.perform(request, 'replace_gateway', changed)

    def test_execution_unknown_order_and_capability_replay_fail(self):
        for fault in ('unknown', 'exception', 'order', 'replay', 'policy'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                def unknown(name, *_, guard):
                    result = self.host_action(name, guard=guard)
                    result['status'] = 'UNKNOWN'
                    return result
                def unavailable(*_):
                    raise PermissionError('fixture')
                with self.assertRaises(O.ObservationClosed):
                    if fault == 'unknown': execution.perform(request, 'prepare_runtime', unknown)
                    elif fault == 'exception': execution.perform(request, 'prepare_runtime', unavailable)
                    elif fault == 'order': execution.perform(request, 'verify_startup', self.host_action)
                    elif fault == 'replay': O.ExecutionWindow(execution.proof, execution.policy)
                    else:
                        execution.policy['owner'] = 'unapproved'
                        execution.check(request)

    def test_execution_gateway_result_does_not_match_observed_identity(self):
        execution, request = self.execution()
        execution.perform(request, 'prepare_runtime', self.host_action)
        def wrong_result(name, *_, guard):
            result = self.host_action(name, guard=guard)
            result['value'] = 'f' * 64
            return result
        with self.assertRaisesRegex(O.ObservationClosed, 'state_drift'):
            execution.perform(request, 'replace_gateway', wrong_result)

    def test_two_phase_request_is_constructed_after_latest_actual_observation(self):
        self.assertNotIn('request_sha256', self.binding)
        self.assertNotIn('qualification_sha256', self.binding)
        self.observe()
        supplied = self.projection()
        supplied['observed_at_utc'] = '1900-01-01T00:00:00Z'
        proof = self.window.claim(supplied)
        self.assertEqual(proof.projection['observed_at_utc'], '1970-01-01T00:17:40Z')
        request = self.seal(proof)
        self.assertEqual(O.intent_sha256(request), self.binding['intent_sha256'])
        self.assertEqual(proof.check(request), proof.projection)
        with self.assertRaisesRegex(O.ObservationClosed, 'final_request_replay'):
            proof.bind(request, O.sha(self.qualification))

    def test_two_phase_unknown_qualification_or_changed_scope_cannot_seal(self):
        for fault in ('scope', 'authority', 'qualification', 'intent'):
            with self.subTest(fault=fault):
                self.setUp()
                self.observe()
                proof = self.window.claim(self.projection())
                if fault == 'scope':
                    self.scope_template['approval_ref'] = 'different'
                elif fault == 'authority':
                    self.window.final_authority = None
                elif fault == 'qualification':
                    self.window.final_authority = lambda *_: False
                else:
                    self.request_template['operations'] = ['unapproved']
                with self.assertRaises(O.ObservationClosed):
                    self.seal(proof)

    def test_final_lease_and_evidence_changes_cannot_replay_execution(self):
        execution, request = self.execution()
        request['expires_at_utc'] = '1970-01-01T00:21:41Z'
        self.assertEqual(O.intent_sha256(request), self.binding['intent_sha256'])
        with self.assertRaisesRegex(O.ObservationClosed, 'request_observation_mismatch'):
            execution.check(request)

    def test_final_authority_revocation_and_unapproved_database_writer_stop_execution(self):
        for fault in ('final_authority', 'database_writer'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                if fault == 'final_authority':
                    self.window.final_authority = lambda *_: False
                else:
                    self.values['database']['unapproved_writers'] = ['unattributed-connection']
                invoked = []
                with self.assertRaises(O.ObservationClosed):
                    execution.perform(request, 'prepare_runtime', lambda *_: invoked.append(True))
                self.assertEqual(invoked, [])

    def test_unconverged_monitor_is_unknown_and_operation_is_never_retried(self):
        execution, request = self.execution()
        class UnconvergedMonitor:
            ident = None
            def __init__(self, **_): pass
            def start(self): pass
            def join(self, timeout): pass
            def is_alive(self): return True
        calls = []
        def action(name, *_):
            calls.append(name)
            return self.host_action(name)
        with patch.object(O.threading, 'Thread', UnconvergedMonitor), \
                patch.object(O.time, 'monotonic', side_effect=range(0, 1000, 100)):
            with self.assertRaisesRegex(O.ObservationClosed, 'execution_monitor_unknown'):
                execution.perform(request, 'prepare_runtime', action)
        with self.assertRaises(O.ObservationClosed):
            execution.perform(request, 'prepare_runtime', action)
        self.assertEqual(calls, [])

    def test_monitor_launch_changes_deny_action_and_cannot_be_retried(self):
        for fault in ('revoked', 'unknown', 'reader_unknown', 'permission', 'protected_state', 'premature_transition'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                # The change occurs after check() but before monitors start.
                real_thread = threading.Thread
                fixture = self
                class ChangedAtLaunch(real_thread):
                    def start(self):
                        if fault == 'revoked': fixture.allowed = False
                        elif fault == 'unknown': fixture.values['authority']['connected'] = False
                        elif fault == 'reader_unknown':
                            fixture.readers['database'] = lambda *_: None
                        elif fault == 'permission': fixture.values['authority']['permission_sha256'] = '0' * 64
                        elif fault == 'protected_state': fixture.values['host']['executor_epoch'] = '0' * 32
                        else:
                            fixture.values['host']['runtime_sha256'] = '0' * 64
                        super().start()
                calls = []
                with patch.object(O.threading, 'Thread', ChangedAtLaunch):
                    with self.assertRaises(O.ObservationClosed):
                        execution.perform(request, 'prepare_runtime', lambda *_: calls.append(True))
                # Restoring the source must not revive this failed capability.
                self.allowed = True
                with self.assertRaises(O.ObservationClosed):
                    execution.perform(request, 'prepare_runtime', lambda *_: calls.append(True))
                self.assertEqual(calls, [])

    def test_change_after_initial_monitor_reads_is_checked_again_before_dispatch(self):
        for fault in ('reader_unknown', 'revoked', 'protected_state'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                # The last monitor's successful final authority response changes
                # a source that an earlier monitor has already read successfully.
                authority = self.window.final_authority
                monitors = set()
                def change_after_initial_reads(*args):
                    result = authority(*args)
                    if threading.current_thread() is not threading.main_thread():
                        monitors.add(threading.get_ident())
                        if len(monitors) == len(O.PERIOD):
                            if fault == 'reader_unknown': self.readers['database'] = lambda *_: None
                            elif fault == 'revoked': self.values['authority']['revoked'] = True
                            else: self.values['host']['protected_runtime_sha256'] = '0' * 64
                    return result
                self.window.final_authority = change_after_initial_reads
                calls = []
                with self.assertRaises(O.ObservationClosed):
                    execution.perform(request, 'prepare_runtime', lambda *_: calls.append(True))
                self.assertEqual(len(monitors), len(O.PERIOD))
                self.assertEqual(calls, [])

    def test_partial_monitor_start_failure_joins_started_workers_and_latches_failure(self):
        execution, request = self.execution()
        real_thread = threading.Thread
        workers, calls = [], []
        class PartialStart(real_thread):
            def start(self):
                workers.append(self)
                if len(workers) == 2:
                    raise RuntimeError('fixture: monitor start failed')
                super().start()
            def join(self, timeout=None):
                self.joined = True
                super().join(timeout)
        with patch.object(O.threading, 'Thread', PartialStart):
            with self.assertRaises(O.ObservationClosed):
                execution.perform(request, 'prepare_runtime', lambda *_: calls.append(True))
        self.assertTrue(getattr(workers[0], 'joined', False))
        self.assertFalse(workers[0].is_alive())
        self.assertIsNotNone(self.window.failed)
        self.assertFalse(execution.active)
        with self.assertRaises(O.ObservationClosed):
            execution.perform(request, 'prepare_runtime', lambda *_: calls.append(True))
        self.assertEqual(calls, [])

    def test_failure_during_monitor_initial_read_never_invokes_adapter_host(self):
        spec = importlib.util.spec_from_file_location('race_adapter', Path(O.__file__).with_name('pg03-successor-adapter.py'))
        adapter_module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(adapter_module)
        execution, request = self.execution()
        entered, release = threading.Event(), threading.Event()
        reader = self.readers['host']
        def blocked_read(binding, challenge):
            if threading.current_thread() is not threading.main_thread():
                entered.set()
                if not release.wait(2):
                    raise TimeoutError('fixture coordinator missing')
            return reader(binding, challenge)
        self.readers['host'] = blocked_read
        def invalidate():
            if entered.wait(2):
                self.values['authority']['revoked'] = True
            release.set()
        coordinator = threading.Thread(target=invalidate)
        coordinator.start()
        self.addCleanup(coordinator.join, 2)
        calls = []
        adapter = adapter_module.ProductionAdapter(host=SimpleNamespace(invoke=lambda *_: calls.append(True)),
                                                  window=execution)
        adapter.dependencies = {'fixture': True}
        # Isolate the window->adapter handoff; no real qualification or grant.
        with patch.object(adapter, 'authenticate'), self.assertRaises(adapter_module.TX.PLAN.Blocked):
            adapter.operation('prepare_runtime', request, {})
        self.assertTrue(entered.is_set())
        self.assertTrue(adapter.failed)
        self.assertEqual(calls, [])

    @unittest.skipUnless(os.name == 'posix', 'Linux host entry contract requires fcntl')
    def test_host_entry_guard_rejects_failure_published_after_outer_admission(self):
        spec = importlib.util.spec_from_file_location('race_host', Path(O.__file__).with_name('pg03-successor-host.py'))
        host_module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(host_module)
        for fault in ('revoked', 'unknown'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                protected = []
                host = SimpleNamespace(failed=False,
                    authorize=lambda *_: protected.append('authorize'),
                    _phase=lambda *_: protected.append('journal'),
                    _run=lambda *_: protected.append('command'))
                failures = []
                def publish_failure():
                    if fault == 'revoked': self.allowed = False
                    else: self.readers['host'] = lambda *_: None
                    try:
                        execution.poll('host')
                    except O.ObservationClosed as error:
                        failures.append(str(error))
                def scheduled_entry(name, *_, guard):
                    # Force an already-published monitor failure into the gap
                    # after the outer admission and before actual host entry.
                    worker = threading.Thread(target=publish_failure)
                    worker.start()
                    worker.join(2)
                    self.assertFalse(worker.is_alive())
                    self.assertEqual(len(failures), 1)
                    self.assertIsNotNone(self.window.failed)
                    return host_module.ProductionHost.invoke(host, name, request, {}, None, guard=guard)
                with self.assertRaises(O.ObservationClosed):
                    execution.perform(request, 'prepare_runtime', scheduled_entry)
                self.assertTrue(host.failed)
                self.assertEqual(protected, [])

    def test_action_must_use_entry_guard_exactly_once(self):
        for fault in ('missing', 'replay'):
            with self.subTest(fault=fault):
                self.setUp()
                execution, request = self.execution()
                def action(name, *_, guard):
                    if fault == 'replay':
                        guard()
                        guard()
                    return dict(schema='clearapi.successor-host-result/v1', operation=name,
                        request_sha256=self.request_sha, status='PASS', value=None)
                with self.assertRaisesRegex(O.ObservationClosed,
                                            'execution_admission_' + ('missing' if fault == 'missing' else 'replay')):
                    execution.perform(request, 'prepare_runtime', action)


if __name__ == '__main__':
    unittest.main()
