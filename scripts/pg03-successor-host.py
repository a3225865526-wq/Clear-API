#!/usr/bin/env python3
"""Explicitly bound Linux successor actions; no default transport or activation.

Trusted ports enforce actual source authentication, bounded cancellation and
production permissions. This file supplies fixed local actions, not those rights.
"""
from contextlib import contextmanager
from datetime import datetime, timezone
import importlib.util
import os
from pathlib import Path
import stat
import sys

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('host_adapter', Path(__file__).with_name('pg03-successor-adapter.py'))
AD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AD)
TX, PLAN = AD.TX, AD.TX.PLAN
SPEC = importlib.util.spec_from_file_location('host_lifecycle', Path(__file__).with_name('pg03-legacy-transition.py'))
STORE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STORE)
ASSETS = frozenset(('/opt/clearapi/bin/runtime-identity-refresh.py',
    '/opt/clearapi/bin/production-migration-entrypoint.sh', '/opt/clearapi/bin/pg03-successor-host.py',
    '/opt/clearapi/bin/pg03-successor-adapter.py', '/opt/clearapi/bin/pg03-successor-transaction.py',
    '/opt/clearapi/bin/pg03-successor-plan.py', '/opt/clearapi/bin/pg03-schema-rehearsal.py',
    '/opt/clearapi/bin/pg03-legacy-transition.py', '/opt/clearapi/bin/pg03-release-control.py',
    '/opt/clearapi/bin/runtime-probe-session.py', '/opt/clearapi/bin/pg03-candidate-qualification.py',
    '/opt/clearapi/bin/pg03-observation.py', '/opt/clearapi/bin/ci/assemble-release-proof.py',
    '/opt/clearapi/bin/ci/pg03-isolated-rehearsal.py'))
MAX_OUTPUT = 1048576


def require(value, message='successor host contract incomplete'):
    PLAN.require(value, message)


class ProductionHost:
    """Fixed actions behind independently authenticated site/collector ports.

    No root, runner, policy or collector is inferred. site_authority must verify
    the current production approval, installation and sole-executor permissions.
    collector.authenticate must authenticate live facts, including their source
    and deadlines; collector.verify_dependencies must authenticate the actual
    backup/restore producer, not merely a stored PASS report. These ports remain
    installation gates and must never be supplied by request JSON.

    bounded_runner(argv, deadline) returns {returncode: int, stdout: bytes}, caps
    output BEFORE buffering, suppresses stderr and cancels on absolute deadline.
    It must execute argv directly without a shell, inherited Docker context or
    credential-bearing environment. No runnable default is provided.
    """
    def __init__(self, *, root=None, site_policy=None, site_authority=None,
                 bounded_runner=None, collector=None, now=None):
        self.root = Path(root) if root is not None else None
        self.site_policy, self.site_authority = site_policy, site_authority
        self.runner, self.collector = bounded_runner, collector
        self.now = now or (lambda: datetime.now(timezone.utc))
        self.policy = self.grant = self.request_sha = None
        self.commands, self.gateway, self.baseline = None, None, None
        self.last_observation, self.used = None, set()
        self.dependencies_verified = False
        self.failed, self.locked = False, False

    def path(self, name):
        require(self.root is not None and name.startswith('/') and '..' not in Path(name).parts)
        return self.root / name.lstrip('/')

    @contextmanager
    def deployment_lock(self):
        require(os.name == 'posix' and os.geteuid() == 0 and self.root is not None
            and not self.root.is_symlink() and self.root.is_dir() and not self.locked,
            'explicit root-owned Linux installation required')
        with STORE.CONTROL.deployment_lock(self.path('/run/lock/clearapi-production-deploy.lock')):
            self.locked = True
            try:
                yield
            finally:
                self.locked = False

    def _deadline(self, request, deadline):
        require(self.locked and deadline.tzinfo is not None and self.now() < deadline
            <= PLAN.utc(request['expires_at_utc']), 'host lock or deadline unavailable')

    def _policy(self, request, deadline):
        self._deadline(request, deadline)
        require(not self.failed and all(callable(port) for port in
            (self.site_policy, self.site_authority, self.runner)) and self.collector is not None,
            'production host ports unbound')
        policy = self.site_policy()
        TX.exact(policy, ('schema', 'policy_id', 'request_sha256', 'control_commit', 'candidate',
            'frozen_sha256', 'issued_at_utc', 'expires_at_utc', 'asset_sha256', 'compose_files', 'compose_files_sha256',
            'effective_nonsecret', 'effective_configuration_sha256', 'permission_sha256',
            'executor_identity_sha256', 'writer_inventory_sha256', 'startup_write_scope_sha256',
            'stopped_writer_identity'))
        require(policy['schema'] == 'clearapi.successor-site-policy/v1'
            and TX.matches(r'[a-f0-9]{32}', policy['policy_id'])
            and policy['request_sha256'] == TX.sha(TX.encoded(request))
            and policy['control_commit'] == request['control_commit']
            and TX.encoded(policy['candidate']) == TX.encoded(request['candidate'])
            and policy['frozen_sha256'] == PLAN.FROZEN
            and PLAN.utc(request['issued_at_utc']) <= PLAN.utc(policy['issued_at_utc']) <= self.now()
            < PLAN.utc(policy['expires_at_utc']) <= PLAN.utc(request['expires_at_utc'])
            and policy['startup_write_scope_sha256'] == request['evidence_sha256']['startup_write_scope'])
        require(all(AD.hash_value(policy[key]) for key in ('effective_configuration_sha256',
            'permission_sha256', 'executor_identity_sha256', 'writer_inventory_sha256')))
        assets = policy['asset_sha256']
        require(type(assets) is dict and ASSETS <= set(assets) and len(assets) <= 64
            and all(TX.matches(r'/opt/clearapi/bin/(ci/)?[a-z0-9-]+\.(py|sh)', name) and AD.hash_value(value)
                    for name, value in assets.items()), 'installed asset inventory missing')
        files = policy['compose_files']
        require(type(files) is list and type(policy['compose_files_sha256']) is dict
            and set(policy['compose_files_sha256']) == set(files)
            and all(AD.hash_value(value) for value in policy['compose_files_sha256'].values()))
        commands = AD.gateway_command_plan(tuple(files))
        projection = policy['effective_nonsecret']
        TX.exact(projection, ('candidate_image', 'identity_mount', 'migration_launcher', 'entrypoint',
            'command', 'tmpfs_paths', 'gateway_public_listeners', 'dependency_start'))
        require(projection == dict(candidate_image=request['candidate']['image_ref'],
            identity_mount=dict(source='/run/clearapi', target='/run/clearapi', read_only=True, create_host_path=False),
            migration_launcher=dict(source='/opt/clearapi/bin/production-migration-entrypoint.sh',
                target='/entrypoint-migration.sh', read_only=True, create_host_path=False),
            entrypoint=['/entrypoint-migration.sh'], command=[], tmpfs_paths=['/tmp'],
            gateway_public_listeners=[], dependency_start=False), 'launcher or effective Compose policy mismatch')
        require(policy['stopped_writer_identity'].get('identity_mode') == 'stopped_failed_gateway')
        if self.policy is not None:
            require(TX.encoded(policy) == TX.encoded(self.policy), 'site policy changed')
        return AD.document(TX.encoded(policy)), commands

    def authorize(self, request, grant, deadline):
        try:
            TX.validate_request(TX.encoded(request), self.now())
            policy, commands = self._policy(request, deadline)
            require(grant.get('schema') == 'clearapi.successor-production-grant/v1'
                and grant.get('request_sha256') == policy['request_sha256']
                and grant.get('transaction_id') == request['transaction_id']
                and TX.encoded(grant.get('candidate')) == TX.encoded(request['candidate'])
                and grant.get('operations') == list(TX.OPERATIONS)
                and self.now() < PLAN.utc(grant['expires_at_utc'])
                and grant.get('public_traffic') == 'blocked' and grant.get('executor_start') == 'forbidden'
                and grant.get('database_restore') == 'forbidden')
            require(self.site_authority(policy, request, grant, deadline) is True,
                    'independent live site authority denied')
            require(self.collector.authenticate(policy, deadline) is True, 'collector identity or permissions unavailable')
            directory = self.path('/run/clearapi')
            info = directory.lstat()
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0
                and stat.S_IMODE(info.st_mode) == 0o750, 'approved root-owned runtime directory must already exist')
            for name, expected in policy['asset_sha256'].items():
                require(TX.sha(STORE.read_file(self.path(name), private=False)) == expected, 'installed asset mismatch')
            for name, expected in PLAN.FROZEN.items():
                require(TX.sha(STORE.read_file(self.path('/opt/clearapi/app/'+name), private=False)) == expected,
                    'failed predecessor files changed')
            site = self.collector.site_projection(request, deadline)
            TX.exact(site, ('schema', 'effective_nonsecret', 'effective_configuration_sha256', 'compose_files_sha256',
                'permission_sha256', 'executor_identity_sha256', 'writer_inventory_sha256', 'all_entry_routes_frozen',
                'sole_executor', 'executor_container_id', 'lock_owned'))
            require(site['schema'] == 'clearapi.successor-site-projection/v1'
                and site['all_entry_routes_frozen'] is True and site['sole_executor'] is True
                and site['lock_owned'] is True and site['executor_container_id'] == PLAN.EXECUTOR_CONTAINER
                and all(site[key] == policy[key] for key in ('effective_nonsecret', 'effective_configuration_sha256',
                    'permission_sha256', 'executor_identity_sha256', 'writer_inventory_sha256')))
            require(type(site['compose_files_sha256']) is dict and set(site['compose_files_sha256']) == set(policy['compose_files']))
            require(site['compose_files_sha256'] == policy['compose_files_sha256'], 'approved Compose bytes changed')
            for name, expected in site['compose_files_sha256'].items():
                require(AD.hash_value(expected) and TX.sha(STORE.read_file(self.path(name), private=False)) == expected)
            require(TX.encoded(self.site_policy()) == TX.encoded(policy), 'site authority drift')
            self._deadline(request, deadline)
            self.policy, self.commands, self.grant, self.request_sha = policy, commands, AD.document(TX.encoded(grant)), policy['request_sha256']
            return True
        except BaseException:
            self.failed = True
            raise PLAN.Blocked('production host identity, installation or policy unavailable') from None

    def verify_dependencies(self, request, documents, deadline):
        self.authorize(request, self.grant, deadline)
        try:
            checked = AD.validate_dependencies(documents, request, self.now())
            require(checked['backup']['stopped_writer_identity'] == self.policy['stopped_writer_identity'])
            require(checked['scope']['effective_configuration_sha256'] == self.policy['effective_configuration_sha256'])
            require(self.collector.verify_dependencies(request, documents, self.policy, deadline) is True,
                'actual backup, restore producer, all writers or exact scope unverified')
            self._deadline(request, deadline)
            self.dependencies_verified = True
            return True
        except BaseException:
            self.failed = True
            raise PLAN.Blocked('host dependency source or backup identity mismatch') from None

    def observe(self, request, deadline):
        try:
            self.authorize(request, self.grant, deadline)
            value = self.collector.observe(request, deadline)
            running = self.gateway is not None and self.last_observation is not None
            TX.validate_observation(value, request, self.now(), self.gateway if running else TX.GATEWAY,
                running, self.baseline, restarted='verify_restart' in self.used)
            if not running:
                require(self.baseline is None or TX.encoded(value) == TX.encoded(self.baseline), 'stopped baseline changed')
                self.baseline = AD.document(TX.encoded(value))
            self._deadline(request, deadline)
            return value
        except BaseException:
            self.failed = True
            raise PLAN.Blocked('host live observation failed or unknown') from None

    def _run(self, argv, deadline):
        require(self.now() < deadline)
        result = self.runner(list(argv), deadline)
        TX.exact(result, ('returncode', 'stdout'))
        require(type(result['returncode']) is int and result['returncode'] == 0
            and type(result['stdout']) is bytes and len(result['stdout']) <= MAX_OUTPUT
            and self.now() < deadline, 'fixed host command failed, timed out or output oversized')
        return result['stdout']

    def _phase(self, request, name):
        require(self.locked and name in TX.OPERATIONS and name not in self.used and self.request_sha == TX.sha(TX.encoded(request)))
        app, prefix = self.path('/opt/clearapi/app'), 'pg03-successor-'+request['transaction_id']
        binding = dict(request_sha256=self.request_sha, transaction_id=request['transaction_id'])
        require(STORE.read_json(app/(prefix+'-attempt.json')) == dict(schema='clearapi.successor-attempt/v1',
            **binding, status='reserved_outcome_unknown', request=request, public_traffic='blocked'), 'TX durable claim missing')
        require(not any((app/(prefix+'-'+suffix+'.json')).exists() or (app/(prefix+'-'+suffix+'.json')).is_symlink()
            for suffix in ('failure', 'result')), 'TX already completed, failed or unknown')
        index = TX.OPERATIONS.index(name)
        for number, phase in enumerate(TX.OPERATIONS, 1):
            path = app/(prefix+'-'+str(number)+'-'+phase+'.json')
            if number <= index:
                require(STORE.read_json(path) == dict(schema='clearapi.successor-stage/v1', **binding, phase=phase, status='completed'))
            else:
                require(not path.exists() and not path.is_symlink(), 'phase replay or later stage exists')
        self.used.add(name)
        intent = app/(prefix+'-host-'+name+'-intent.json')
        STORE.publish(intent, dict(schema='clearapi.successor-host-intent/v1', **binding, operation=name, outcome='UNKNOWN'))
        return app/(prefix+'-host-'+name+'-result.json')

    def _challenge(self, request, context, phase):
        require(TX.matches(r'[a-f0-9]{64}', context.get('migration_challenge')))
        directory = self.path('/run/clearapi')
        info = directory.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o750)
        destination = directory/'migration-challenge'
        if destination.exists() or destination.is_symlink():
            STORE.read_file(destination)
        staged = directory/('.migration-challenge-'+request['transaction_id']+'-'+phase)
        fd = os.open(staged, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write((context['migration_challenge']+'\n').encode())
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(staged, destination)
        descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def _postflight(self, request, context, deadline, restarted):
        # The trusted collector must authenticate actual PID 1, process start
        # ticks, /proc/1/exe bytes, live/configured hostname, container/image
        # mounts, current Runtime and authenticated Executor connection. Receipt
        # shape is supplementary and never establishes those facts itself.
        require(context.get('gateway_container_id') == self.gateway and TX.matches(r'[a-f0-9]{64}', self.gateway))
        facts = self.collector.verify_postflight(request, dict(context), deadline)
        TX.exact(facts, ('verified', 'configured_hostname', 'live_hostname', 'observation'))
        require(facts['verified'] is True and type(facts['live_hostname']) is str
            and facts['configured_hostname'] == facts['live_hostname'], 'postflight source unverified')
        raw = self._run(['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'exec', self.gateway,
            'cat', '/tmp/clearapi-migration-proof.json'], deadline)
        PLAN.SCHEMA.validate_completion_proof(raw.decode(), self.gateway, context['migration_challenge'],
            request['candidate']['binary_sha256'], facts['live_hostname'])
        value = facts['observation']
        TX.validate_observation(value, request, self.now(), self.gateway, True, self.baseline, restarted=restarted)
        require(value['migration_challenge'] == context['migration_challenge'] and value['migration_proof_sha256'] == TX.sha(raw))
        if restarted:
            require(value['process_started_ticks'] != self.last_observation['process_started_ticks']
                and value['migration_proof_sha256'] != self.last_observation['migration_proof_sha256'])
        STORE.publish(self.path('/opt/clearapi/app')/('pg03-successor-'+request['transaction_id']+
            ('-restart-proof.json' if restarted else '-startup-proof.json')),
            dict(schema='clearapi.successor-host-proof/v1', request_sha256=self.request_sha,
                gateway_container_id=self.gateway, migration_challenge=context['migration_challenge'],
                raw_receipt=raw.decode(), receipt_sha256=TX.sha(raw), observation=value))
        self.last_observation = AD.document(TX.encoded(value))
        return value

    def invoke(self, name, request, context, deadline, *, guard=None):
        try:
            # ExecutionWindow supplies its live one-use guard at actual host
            # entry, before authorization, journal publication or any command.
            if guard is not None:
                guard()
            self.authorize(request, self.grant, deadline)
            require(self.dependencies_verified and self.baseline is not None, 'host dependencies or stopped baseline unverified')
            result_path = self._phase(request, name)
            value = None
            if name == 'prepare_runtime':
                self._run(self.commands['prepare_runtime'], deadline)
            elif name == 'replace_gateway':
                self._run(self.commands['replace_gateway'], deadline)
                prefix = self.commands['replace_gateway'][:self.commands['replace_gateway'].index('up')]
                cid = self._run(prefix+['ps', '-a', '-q', 'new-api'], deadline).decode().strip()
                require(TX.matches(r'[a-f0-9]{64}', cid) and cid not in (TX.GATEWAY, PLAN.EXECUTOR_CONTAINER))
                self.gateway, value = cid, cid
            elif name in ('verify_startup', 'verify_restart'):
                require(context.get('gateway_container_id') == self.gateway)
                if name == 'verify_restart':
                    saved = STORE.read_json(self.path('/opt/clearapi/app')/('pg03-successor-'+request['transaction_id']+'-startup-proof.json'))
                    require(saved['request_sha256'] == self.request_sha and saved['gateway_container_id'] == self.gateway
                        and saved['migration_challenge'] != context['migration_challenge']
                        and TX.sha(saved['raw_receipt'].encode()) == saved['receipt_sha256']
                        == self.last_observation['migration_proof_sha256']
                        and TX.encoded(saved['observation']) == TX.encoded(self.last_observation))
                    self._run(self.commands['contain_gateway'], deadline)
                self._challenge(request, context, name)
                self._run(self.commands['start_gateway'], deadline)
                value = self._postflight(request, context, deadline, name == 'verify_restart')
            else:
                require(self.last_observation is not None and 'verify_restart' in self.used)
                manifest = dict(schema='clearapi.successor-manifest/v1', request_sha256=self.request_sha,
                    transaction_id=request['transaction_id'], candidate=request['candidate'],
                    control_commit=request['control_commit'], failed_predecessor_unchanged=PLAN.FROZEN,
                    previous_success=request['previous_success'], gateway_container_id=self.gateway,
                    postflight=self.last_observation, public_traffic='blocked', executor_started=False,
                    database_restored=False, production_acceptance='requires_authenticated_producer_final_outcome')
                path = self.path('/opt/clearapi/app')/('pg03-successor-'+request['transaction_id']+'-manifest.json')
                STORE.publish(path, manifest)
                value = TX.sha(STORE.read_file(path))
            self.authorize(request, self.grant, deadline)
            result = dict(schema='clearapi.successor-host-result/v1', operation=name,
                request_sha256=self.request_sha, status='PASS', value=value)
            STORE.publish(result_path, result)
            return result
        except BaseException:
            self.failed = True
            raise PLAN.Blocked('host operation failed or unknown; preserve journals and contain without retry') from None

    def contain(self, request, context, deadline):
        """Only stop the owned successor; retain old manifest and every receipt."""
        require(self.locked and self.request_sha == TX.sha(TX.encoded(request))
            and self.commands is not None and TX.matches(r'[a-f0-9]{64}', self.gateway)
            and context.get('gateway_container_id') in (None, self.gateway)
            and self.gateway not in (TX.GATEWAY, PLAN.EXECUTOR_CONTAINER))
        self.failed = True
        intent = STORE.read_json(self.path('/opt/clearapi/app')/('pg03-successor-'+request['transaction_id']+'-host-replace_gateway-intent.json'))
        require(intent == dict(schema='clearapi.successor-host-intent/v1', request_sha256=self.request_sha,
            transaction_id=request['transaction_id'], operation='replace_gateway', outcome='UNKNOWN'))
        require(self.collector.authorize_containment(request, self.gateway, self.policy, deadline) is True,
            'owned candidate containment identity UNKNOWN')
        self._run(['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'stop', '--time', '150', self.gateway], deadline)
        value = self.collector.containment(request, self.gateway, deadline)
        expected = dict(frozen_sha256=PLAN.FROZEN, executor_container_id=PLAN.EXECUTOR_CONTAINER,
            executor_process_epoch=self.baseline['executor_process_epoch'], ingress='isolated_stopped_restart_no',
            public_web_listeners=[], successor_gateway_stopped=True, database_restored=False, executor_started=False)
        require(value == expected, 'containment UNKNOWN')
        return value


if __name__ == '__main__':
    raise SystemExit('SUCCESSOR HOST BLOCKED: reviewed site authority, bounded runner and live collector required')
