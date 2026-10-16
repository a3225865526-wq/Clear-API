#!/usr/bin/env python3
"""One-use, reviewed legacy retirement transition; never an ordinary Runtime PASS.

The root review entry point accepts a fresh official login response and GitHub
read credential on private stdin. It writes no database data. Only Deploy V2's
explicit maintenance mode may consume the subsequent real production approval.
"""
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


CONTROL = load('pg03-release-control')
PROBE = load('runtime-probe-session')
SCHEMA = 'legacy-retirement-transition-v1'
ACTOR = 'a3225865526-wq'


class Blocked(ValueError):
    pass


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


class Paths:
    # No command-line or environment override can select an alternative root.
    # Tests construct this object explicitly against their own isolated trees.
    def __init__(self, root=Path('/')):
        self.root = root
        self.app = root / 'opt/clearapi/app'
        self.request = self.app / 'legacy-transition-request.json'
        self.authorization = self.app / 'legacy-transition-authorization.json'
        self.state = self.app / 'legacy-transition-state.json'
        self.backup = self.app / 'legacy-transition-backup.json'
        self.plan = self.app / 'legacy-transition-plan.json'
        self.session = root / 'opt/clearapi/secrets/legacy-transition-session'
        self.audit = root / 'opt/clearapi/logs/legacy-transition.jsonl'
        self.manifest = self.app / 'release-manifest.json'
        self.backups = root / 'opt/clearapi/backups'
        self.dsn = root / 'opt/clearapi/secrets/sql_dsn'
        self.pool = root / 'data/clearapi/pool-executor/pool'


def read_file(path, private=True, maximum=1048576):
    for parent in path.parents:
        s = parent.lstat()
        if (not stat.S_ISDIR(s.st_mode) or s.st_uid != os.geteuid()
                or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
            raise Blocked('unsafe transition directory')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as f:
        s = os.fstat(f.fileno())
        if (not stat.S_ISREG(s.st_mode) or s.st_uid != os.geteuid()
                or s.st_gid != os.getegid() or s.st_nlink != 1 or s.st_mode & 0o022
                or (private and stat.S_IMODE(s.st_mode) != 0o600)):
            raise Blocked('unsafe transition file')
        raw = f.read(maximum + 1)
    if len(raw) > maximum:
        raise Blocked('oversized transition evidence')
    return raw


def read_json(path, private=True):
    return json.loads(read_file(path, private), object_pairs_hook=CONTROL.unique)


def publish(path, value, exclusive=True):
    # Validate the complete parent chain, including the target directory.
    for parent in path.parents:
        s = parent.lstat()
        if (not stat.S_ISDIR(s.st_mode) or s.st_uid != os.geteuid()
                or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
            raise Blocked('unsafe publication directory')
    raw = encoded(value) + b'\n'
    if not exclusive:
        read_file(path)
    fd, temporary = tempfile.mkstemp(prefix='.legacy-transition-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as f:
            f.write(raw)
            f.flush()
            os.fsync(f.fileno())
        if exclusive:
            os.link(temporary, path, follow_symlinks=False)
        else:
            os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


class Production:
    def __init__(self, paths):
        self.paths = paths

    def command(self, args):
        result = subprocess.run(args, capture_output=True, timeout=90, check=True)
        return result.stdout.decode().strip()

    def gateway(self):
        # Reuse the backup subsystem's exact running/Compose/manifest binding.
        source = json.loads(self.command(['/usr/bin/python3',
            '/opt/clearapi/backups/bin/backup-identity.py', 'capture', '--app-dir', str(self.paths.app)]))
        cid = source['container_id']
        source['started_at'] = self.command(['docker', 'inspect', '--format', '{{.State.StartedAt}}', cid])
        source['restart_policy'] = json.loads(self.command(['docker', 'inspect', '--format', '{{json .HostConfig.RestartPolicy}}', cid]))
        mounts = json.loads(self.command(['docker', 'inspect', '--format', '{{json .Mounts}}', cid]))
        dsns = [m for m in mounts if m['Destination'] == '/run/secrets/sql_dsn']
        s = self.paths.dsn.lstat()
        if (len(dsns) != 1 or dsns[0]['Source'] != str(self.paths.dsn) or dsns[0]['RW'] is not False
                or not stat.S_ISREG(s.st_mode) or s.st_uid != os.geteuid() or s.st_gid != os.getegid()
                or s.st_nlink != 1 or stat.S_IMODE(s.st_mode) != 0o600):
            raise Blocked('database credential mount is not the approved private source')
        source['dsn_file'] = {'inode': s.st_ino, 'mtime_ns': s.st_mtime_ns, 'bytes': s.st_size}
        source['binary_sha256'] = self.command(['docker', 'exec', cid, 'sha256sum', '/proc/1/exe']).split()[0]
        expected = self.command(['docker', 'run', '--rm', '--network', 'none', '--read-only',
            '--entrypoint', 'sha256sum', source['docker_image_id'], '/new-api']).split()[0]
        if source['binary_sha256'] != expected or self.command(['docker', 'port', cid, '3000/tcp']) != '127.0.0.1:3000':
            raise Blocked('running Gateway binary or endpoint mismatch')
        process = self.command(['docker', 'exec', cid, 'cat', '/proc/1/stat'])
        source['process_started_ticks'] = process.rsplit(')', 1)[1].split()[19]
        return source

    def edge(self):
        value = json.loads(self.command(['docker', 'inspect', '--format',
            '{"id":{{json .Id}},"image":{{json .Image}},"started_at":{{json .State.StartedAt}},"running":{{json .State.Running}},"paused":{{json .State.Paused}}}',
            'clearapi-edge-caddy']))
        if value['running'] is not True:
            raise Blocked('maintenance edge is not running')
        return value

    def edge_paused(self, expected, paused):
        live = self.edge()
        if any(live.get(k) != expected.get(k) for k in ('id', 'image', 'started_at', 'running')):
            raise Blocked('maintenance edge identity changed')
        if live['paused'] != paused:
            self.command(['docker', 'pause' if paused else 'unpause', expected['id']])
        if self.edge() != dict(expected, paused=paused):
            raise Blocked('maintenance edge isolation not verified')

    def runtime(self, token):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), PROBE.NoRedirect())
        request = urllib.request.Request('http://127.0.0.1:3000/api/clearapi/runtime-integrity',
            headers={'Authorization': 'Bearer ' + token, 'Cache-Control': 'no-store'})
        try:
            response = opener.open(request, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            status, raw = response.status, response.read(65537)
        if len(raw) > 65536:
            raise Blocked('oversized Runtime response')
        return status, json.loads(raw, object_pairs_hook=CONTROL.unique)

    def candidate(self, candidate):
        ref = candidate['image_ref']
        info = json.loads(self.command(['docker', 'image', 'inspect', '--format',
            '{"id":{{json .Id}},"digests":{{json .RepoDigests}},"os":{{json .Os}},"arch":{{json .Architecture}},"revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}}}', ref]))
        if (info['id'] != candidate['image_id'] or ref not in info['digests']
                or info['os'] != 'linux' or info['arch'] != 'amd64'
                or info['revision'] != candidate['commit']):
            raise Blocked('candidate source identity mismatch')
        actual = self.command(['docker', 'run', '--rm', '--network', 'none', '--read-only',
            '--entrypoint', 'sha256sum', ref, '/new-api', '/legacy-store-retire']).splitlines()
        if actual != [candidate['binary_sha256'] + '  /new-api', candidate['retire_binary_sha256'] + '  /legacy-store-retire']:
            raise Blocked('candidate binary identity mismatch')

    def retirement(self, candidate, actor=None, evidence=None, apply=False):
        # The DSN is mounted only into the verified image; it never enters argv,
        # host environment, HTTP evidence or an audit record.
        args = ['docker', 'run', '--rm', '--network', 'clearapi_internal', '--read-only',
            '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '--mount',
            'type=bind,src=' + str(self.paths.dsn) + ',dst=/run/secrets/sql_dsn,readonly']
        if apply:
            args += ['--mount', 'type=bind,src=' + str(self.paths.plan) + ',dst=/transition-plan.json,readonly']
        args += ['--entrypoint', '/bin/sh', candidate['image_ref'], '-ec',
            'export CLEARAPI_LEGACY_RETIREMENT_DSN="$(cat /run/secrets/sql_dsn)"; exec /legacy-store-retire "$@"',
            'legacy-store-retire', '--dialect', 'postgres']
        if apply:
            plan = read_json(self.paths.plan)
            args += ['--apply-approved-plan', '/transition-plan.json', '--confirmation', plan['confirmation']]
            self.command(args)
            return None
        args += ['--actor-id', str(actor), '--evidence-ref', evidence]
        return json.loads(self.command(args), object_pairs_hook=CONTROL.unique)

    def snapshot(self):
        # Deliberately selected non-secret accounting fields. Hash aggregation
        # occurs in PostgreSQL; no account details or redemption keys leave it.
        projections = {
            'users': 'id,role,status,quota,used_quota,total_recharge_cents',
            'tokens': 'id,user_id,status,remain_quota,used_quota',
            'channels': 'id,status',
            'billing_reservations': 'id,user_id,token_id,reserved,target,applied,state,updated_at',
            'balance_transactions': 'id,user_id,created_at,type,amount_quota,balance_before,balance_after,recharge_cents,reference_type,reference_id',
            'redemptions': 'id,user_id,status,quota,redeemed_time,used_user_id,expired_time,face_value_cents,source,type,batch_id',
            'redemption_batches': 'id,status,count,quota,face_value_cents,source,type,product_id,product_revision,approval_id',
            'top_ups': 'id,user_id,amount,money,status,complete_time',
            'offline_orders': 'id', 'subscription_orders': 'id',
            'credit_approval_events': 'id,product_id,revision,digest,policy,actor_id,decision,created_time,reference_approval_id,evidence_batch_id',
        }
        fields = []
        for table, columns in projections.items():
            where = " WHERE decision <> 'retire_legacy'" if table == 'credit_approval_events' else ''
            fields += ["'%s', (SELECT encode(sha256(convert_to(COALESCE(string_agg(row_to_json(t)::text,'|' ORDER BY id),''),'UTF8')),'hex') FROM (SELECT %s FROM %s%s) t)" % (table, columns, table, where)]
        fields += ["'catalog', (SELECT value FROM options WHERE key='clearapi.credit_catalog')",
            "'store_count', (SELECT count(*) FROM store_products)",
            "'config', (SELECT json_object_agg(key,value) FROM options WHERE key IN ('StoreRechargeEnabled','DisableOnlinePayments','billing_reserve_policy.mode','ClearAPIPoolMasterEnabled','clearapi.referral'))"]
        sql = 'BEGIN READ ONLY; SELECT json_build_object(' + ','.join(fields) + '); COMMIT;'
        result = json.loads(self.command(['docker', 'exec', 'clearapi-postgres-1', 'psql',
            '-U', 'clearapi_app', '-d', 'clearapi', '-X', '-Atq', '-v', 'ON_ERROR_STOP=1', '-c', sql]))
        raw_catalog = result.pop('catalog')
        catalog = json.loads(raw_catalog, object_pairs_hook=CONTROL.unique)
        catalog['_raw_digest'] = digest(raw_catalog.encode())
        business = dict(result)
        schema = self.command(['docker', 'exec', 'clearapi-postgres-1', 'pg_dump',
            '-U', 'clearapi_app', '-d', 'clearapi', '--schema-only', '--no-owner', '--no-privileges'])
        # PostgreSQL 17 emits random psql restrict nonces; they are transport
        # framing, not schema. Preserve every other schema byte in the binding.
        canonical = '\n'.join(line for line in schema.splitlines()
            if not line.startswith(('\\restrict ', '\\unrestrict ')))
        business['schema_sha256'] = digest(canonical.encode())
        config = business['config']
        referral = json.loads(config.get('clearapi.referral', '{}'))
        if (config.get('StoreRechargeEnabled') != 'false' or config.get('DisableOnlinePayments') != 'true'
                or config.get('billing_reserve_policy.mode') != 'enforce'
                or config.get('ClearAPIPoolMasterEnabled', 'false') != 'false'
                or referral.get('rewards_enabled') is not False or result['store_count'] != 0):
            raise Blocked('maintenance safety configuration mismatch')
        for name in ('checkpoint.journal', 'admission.json'):
            raw = read_file(self.paths.pool / name)
            state = json.loads(raw, object_pairs_hook=CONTROL.unique)
            if state['guard_version'] != 3 or type(state['revision']) is not int or state['revision'] < 0:
                raise Blocked('invalid Pool high-water mark')
            business[name] = digest(raw)
        business['frozen'] = digest(encoded({k: catalog.get(k) for k in
            ('legacy_products', 'legacy_catalog', 'legacy_evidence_batch_ids', 'approval_products')}))
        return business, catalog

    def stopped(self, source):
        if self.command(['docker', 'inspect', '--format', '{{.State.Running}}', source['container_id']]) != 'false':
            raise Blocked('old Gateway is still running')
        running = self.command(['docker', 'ps', '-q', '--filter', 'label=com.docker.compose.project=clearapi',
            '--filter', 'label=com.docker.compose.service=new-api'])
        if running:
            raise Blocked('another Gateway writer is running')

    def disable_old_restart(self, source):
        self.stopped(source)
        self.command(['docker', 'update', '--restart=no', source['container_id']])
        if self.command(['docker', 'inspect', '--format', '{{.HostConfig.RestartPolicy.Name}}', source['container_id']]) != 'no':
            raise Blocked('old Gateway automatic restart is not disabled')

    def stop_gateway(self):
        # Failure containment uses only the audited project/service, never the
        # Executor or a guessed container selected from untrusted input.
        self.command(['docker', 'compose', '--project-directory', str(self.paths.app),
            '-f', str(self.paths.app / 'docker-compose.prod.yml'), '-f', str(self.paths.app / 'compose-pg03.yaml'),
            '-f', str(self.paths.app / 'compose-candidate.yaml'), 'stop', '--timeout', '150', 'new-api'])


class Transition:
    def __init__(self, paths=None, io=None, now=time.time):
        self.paths = paths or Paths()
        self.io = io or Production(self.paths)
        self.now = now

    def audit(self, phase, request, result):
        # No raw HTTP body, credentials, exception text or business rows.
        row = {'schema_version': SCHEMA, 'transition_id': request['transition_id'],
            'ts': int(self.now()), 'phase': phase, 'result': result,
            'request_sha256': digest(read_file(self.paths.request))}
        for parent in self.paths.audit.parents:
            s = parent.lstat()
            if (not stat.S_ISDIR(s.st_mode) or s.st_uid != os.geteuid()
                    or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
                raise Blocked('unsafe audit directory')
        fd = os.open(self.paths.audit, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'ab') as f:
            s = os.fstat(f.fileno())
            if not stat.S_ISREG(s.st_mode) or s.st_uid != os.geteuid() or s.st_nlink != 1 or s.st_mode & 0o077:
                raise Blocked('unsafe transition audit')
            f.write(encoded(row) + b'\n')
            f.flush()
            os.fsync(f.fileno())

    def review(self, data):
        # A successful RC and same-main CI establish executable provenance; they
        # do not authorize production retirement or claim an old Runtime PASS.
        if any(p.exists() or p.is_symlink() for p in (self.paths.request, self.paths.authorization,
                self.paths.state, self.paths.plan, self.paths.session, self.paths.backup)):
            raise Blocked('preserve prior transition evidence before another review')
        token = data.pop('token')
        get = lambda path: CONTROL.api(path, token)
        rc = CONTROL.approved_rc(data, get)
        manifest, manifest_sha = CONTROL.release_manifest(data, rc,
            get('actions/runs/' + str(data['rc_run']) + '/artifacts?per_page=100'),
            lambda ident: CONTROL.api('actions/artifacts/' + str(ident) + '/zip', token, archive=True))
        if manifest.get('verification', {}).get('legacy_retirement_source_tests') != 'passed':
            raise Blocked('same-main RC must include the retirement Runtime regressions')
        candidate = {'image_ref': manifest['image_ref'], 'image_digest': manifest['image_digest'],
            'image_id': manifest['new_api_image_id'], 'commit': manifest['source_commit'],
            'binary_sha256': manifest['binary_sha256'], 'retire_binary_sha256': manifest['legacy_retire_binary_sha256'],
            'rc_run': str(data['rc_run']), 'rc_manifest_sha256': manifest_sha}
        self.io.candidate(candidate)
        access = PROBE.login_access(encoded(data['login_response']), self.now())
        claims = json.loads(base64.urlsafe_b64decode(access.split('.')[1] + '=' * (-len(access.split('.')[1]) % 4)))
        if (type(claims.get('iat')) is not int or not -5 <= self.now() - claims['iat'] <= 120
                or not re.fullmatch(r'[1-9][0-9]*', claims.get('sub', ''))
                or claims['exp'] < self.now() + 600):
            raise Blocked('fresh official root login required')
        source = self.io.gateway()
        edge = self.io.edge()
        if edge['paused'] is not False:
            raise Blocked('edge is already in a maintenance state')
        status, body = self.io.runtime(access)
        if status != 503 or body != {'success': False, 'status': 'BLOCKED', 'code': 'APPROVAL_VIEW_INVALID'}:
            raise Blocked('expected real authenticated legacy Runtime rejection required')
        # The server has now verified signature, live session, root role/status
        # and versions. Subject decoding above is not offline authentication.
        actor = int(claims['sub'])
        review = self.io.retirement(candidate, actor=actor, evidence=data['evidence_ref'])
        plan = review['plan']
        if plan['actor_id'] != actor or plan['evidence_ref'] != data['evidence_ref']:
            raise Blocked('retirement plan operator or evidence mismatch')
        business, catalog = self.io.snapshot()
        self.validate_scope(plan, catalog)
        if source != self.io.gateway():
            raise Blocked('old Gateway changed during review')
        request = {'schema_version': SCHEMA, 'transition_id': str(uuid.uuid4()),
            'created_at': int(self.now()), 'expires_at': min(int(self.now()) + 600, claims['exp'] - 60),
            'source': source, 'edge': edge, 'candidate': candidate, 'review': review, 'invariants': business,
            'old_runtime': {'http_status': 503, 'status': 'BLOCKED', 'code': 'APPROVAL_VIEW_INVALID'}}
        # Each publication is exclusive. Partial preparation must be retained
        # for operator review; another request cannot silently replace it.
        publish(self.paths.plan, review)
        PROBE.install(self.paths.session, access)
        publish(self.paths.request, request)
        self.audit('review', request, 'OLD_RUNTIME_BLOCKED_NOT_PASS')
        return digest(read_file(self.paths.request))

    def validate_scope(self, plan, catalog):
        legacy = catalog.get('legacy_products', [])
        published = [p['id'] for p in catalog.get('legacy_catalog', []) if p.get('published')]
        approvals = catalog.get('approval_products', [])
        if (plan['catalog_version'] != 5 or plan['prepare_event_id'] != 1 or plan['missing_store_ids'] != [1]
                or plan['catalog_digest'] != catalog['_raw_digest']
                or catalog.get('version') != 5 or catalog.get('approval_schema') != 1 or catalog.get('unified') is not False
                or catalog.get('legacy_retirement') is not None or len(legacy) != 1
                or legacy[0]['id'] != 1 or legacy[0]['enabled'] is not True or published != ['credit-1']
                or len(approvals) != 8 or any(p['state'] != 'draft' or p['published'] or p['approval_id'] != 0 for p in approvals)):
            raise Blocked('outside the approved v5 legacy retirement scope')

    def authorization(self, run, image_digest, commit, allow_expired=False):
        raw = read_file(self.paths.request)
        request = json.loads(raw, object_pairs_hook=CONTROL.unique)
        authority = read_json(self.paths.authorization)
        if (request['schema_version'] != SCHEMA or authority['schema_version'] != SCHEMA
                or authority['request_sha256'] != digest(raw) or authority['transition_id'] != request['transition_id']
                or authority['approval_run'] != str(run) or authority['reviewers'] != [ACTOR]
                or request['candidate']['image_digest'] != image_digest or request['candidate']['commit'] != commit
                or authority['candidate'] != request['candidate']
                or request['old_runtime'] != {'http_status': 503, 'status': 'BLOCKED', 'code': 'APPROVAL_VIEW_INVALID'}
                or not 0 < request['expires_at'] - request['created_at'] <= 600
                or request['created_at'] > self.now() + 5
                or (not allow_expired and self.now() >= request['expires_at'])):
            raise Blocked('missing, expired or mismatched maintenance authorization')
        uuid.UUID(request['transition_id'])
        if read_json(self.paths.plan) != request['review']:
            raise Blocked('retirement plan file changed')
        return request

    def check_before(self, request):
        if self.io.gateway() != request['source']:
            raise Blocked('old Gateway identity changed')
        if self.io.edge() != request['edge']:
            raise Blocked('public edge changed before maintenance')
        access = read_file(self.paths.session).decode().strip()
        status, body = self.io.runtime(access)
        if status != 503 or body != {'success': False, 'status': 'BLOCKED', 'code': 'APPROVAL_VIEW_INVALID'}:
            raise Blocked('old Runtime is not the approved authenticated failure')
        business, catalog = self.io.snapshot()
        self.validate_scope(request['review']['plan'], catalog)
        if business != request['invariants'] or self.io.gateway() != request['source']:
            raise Blocked('pre-transition business or process state changed')

    def checkpoint(self, stage, request, **values):
        state = {'schema_version': SCHEMA, 'transition_id': request['transition_id'],
            'request_sha256': digest(read_file(self.paths.request)), 'stage': stage, **values}
        publish(self.paths.state, state, exclusive=not self.paths.state.exists())
        self.audit(stage, request, 'RECORDED')

    def candidate_identity(self, request):
        source = self.io.gateway()
        candidate = request['candidate']
        if (source['container_id'] == request['source']['container_id']
                or source['docker_image_id'] != candidate['image_id']
                or source['oci_revision'] != candidate['commit'] or source['binary_sha256'] != candidate['binary_sha256']
                or source.get('dsn_file') != request['source'].get('dsn_file')):
            raise Blocked('new Runtime is not the approved candidate process and database')
        return {k: source[k] for k in ('container_id', 'docker_image_id', 'oci_revision',
            'started_at', 'binary_sha256', 'process_started_ticks')}

    def phase(self, phase, run, image_digest, commit, backup_started=None):
        request = self.authorization(run, image_digest, commit, allow_expired=phase in ('status', 'fail'))
        state = read_json(self.paths.state) if self.paths.state.exists() else None
        if state and (state['transition_id'] != request['transition_id'] or state['request_sha256'] != digest(read_file(self.paths.request))):
            raise Blocked('transition journal belongs to another authorization')
        if phase == 'status':
            if state is None:
                return 'new'
            if state['stage'] == 'passed':
                if self.candidate_identity(request) != state['candidate_identity']:
                    raise Blocked('completed transition identity changed')
                return 'completed'
            raise Blocked('consumed transition requires manual recovery review')
        if phase == 'fail':
            if state is not None:
                # Includes an uncertain/failed retirement command: commit may
                # have succeeded before a transport or process failure.
                self.checkpoint('failed', request, previous_stage=state['stage'], automatic_rollback=False)
                self.io.edge_paused(request['edge'], True)
                self.io.stop_gateway()
            return 'failed_closed'
        if phase == 'preflight':
            if state is not None:
                raise Blocked('maintenance authorization already consumed')
            self.check_before(request)
            self.audit('preflight', request, 'OLD_RUNTIME_BLOCKED_NOT_PASS')
        elif phase == 'backup':
            if state is not None:
                raise Blocked('backup cannot replace consumed evidence')
            self.check_before(request)
            receipt = self.verify_backup(request, backup_started)
            publish(self.paths.backup, receipt)
        elif phase == 'begin':
            if state is not None or request['expires_at'] - self.now() < 300:
                raise Blocked('consumed authorization or insufficient maintenance time')
            self.check_before(request)
            self.io.candidate(request['candidate'])
            receipt = read_json(self.paths.backup)
            if self.verify_backup(request, receipt['started_at']) != receipt:
                raise Blocked('backup evidence changed')
            self.checkpoint('entered', request, backup=receipt)
        elif phase == 'isolate':
            if state is None or state['stage'] != 'entered':
                raise Blocked('edge isolation requires consumed maintenance entry')
            self.checkpoint('isolating', request, backup=state['backup'])
            self.io.edge_paused(request['edge'], True)
            self.checkpoint('isolated', request, backup=state['backup'])
        elif phase == 'retire':
            if state is None or state['stage'] != 'isolated':
                raise Blocked('retirement requires a fresh consumed maintenance entry')
            if self.io.edge() != dict(request['edge'], paused=True):
                raise Blocked('public writes are not isolated')
            self.io.stopped(request['source'])
            business, catalog = self.io.snapshot()
            self.validate_scope(request['review']['plan'], catalog)
            if business != request['invariants']:
                raise Blocked('state changed while quiescing the Gateway')
            self.io.disable_old_restart(request['source'])
            # Durable latch BEFORE the transaction: an ambiguous CLI failure
            # never permits restarting the old image or resetting the journal.
            self.checkpoint('retiring', request, backup=state['backup'])
            self.io.retirement(request['candidate'], apply=True)
            business, catalog = self.io.snapshot()
            if business != request['invariants'] or catalog.get('legacy_retirement', {}).get('plan') != request['review']['plan']:
                raise Blocked('retirement postcondition or accounting mismatch')
            self.checkpoint('retired', request, backup=state['backup'])
        elif phase == 'postflight':
            if state is None or state['stage'] != 'retired':
                raise Blocked('new Runtime verification requires committed retirement')
            if self.io.edge() != dict(request['edge'], paused=True):
                raise Blocked('public edge reopened before candidate verification')
            source = self.candidate_identity(request)
            status, body = self.io.runtime(read_file(self.paths.session).decode().strip())
            report = body.get('data', {})
            if (status != 200 or body.get('success') is not True or report.get('status') != 'PASS'
                    or report.get('container_id') != source['container_id'] or report.get('started_at') != source['started_at']
                    or report.get('build_identity') != 'sha256:' + source['binary_sha256']
                    or report.get('effective_product_count') != 0 or report.get('store_enabled') is not False
                    or report.get('online_payment_enabled') is not False or report.get('pool_enabled') is not False
                    or report.get('referral_enabled') is not False or report.get('billing_mode') != 'enforce'):
                raise Blocked('new Gateway did not produce its own safe Runtime PASS')
            business, catalog = self.io.snapshot()
            if (business != request['invariants'] or catalog.get('legacy_retirement', {}).get('plan') != request['review']['plan']
                    or self.candidate_identity(request) != source):
                raise Blocked('postflight accounting or history changed')
            self.checkpoint('candidate_verified', request, candidate_container=source['container_id'], candidate_identity=source, backup=state['backup'])
        elif phase == 'release-edge':
            if state is None or state['stage'] != 'candidate_verified':
                raise Blocked('public edge requires new Runtime and all V2 invariant gates')
            business, _ = self.io.snapshot()
            if business != request['invariants'] or self.candidate_identity(request) != state['candidate_identity']:
                raise Blocked('state changed before opening public edge')
            self.checkpoint('edge_releasing', request, candidate_container=state['candidate_container'], candidate_identity=state['candidate_identity'], backup=state['backup'])
            self.io.edge_paused(request['edge'], False)
            self.checkpoint('edge_released', request, candidate_container=state['candidate_container'], candidate_identity=state['candidate_identity'], backup=state['backup'])
        elif phase == 'complete':
            if state is None or state['stage'] != 'edge_released':
                raise Blocked('completion requires all V2 gates first')
            manifest = read_json(self.paths.manifest, private=False)['releases']['current']
            if manifest.get('status') != 'success' or manifest.get('image_digest') != image_digest:
                raise Blocked('V2 release has not completed')
            business, _ = self.io.snapshot()
            if (business != request['invariants'] or self.candidate_identity(request) != state['candidate_identity']
                    or self.io.edge() != request['edge']):
                raise Blocked('final invariants changed')
            self.checkpoint('passed', request, candidate_container=state['candidate_container'], candidate_identity=state['candidate_identity'], backup=state['backup'])
        else:
            raise Blocked('unsupported transition phase')
        return phase

    def verify_backup(self, request, started):
        # Backup identity capture requires the OLD Gateway to stay running while
        # the official encrypted backup is made. No stopped-source substitution.
        from datetime import datetime, timezone
        began = datetime.fromisoformat(started.replace('Z', '+00:00'))
        # V2 uses date -u without an offset; interpret it as UTC on every host.
        stamp = began.replace(tzinfo=timezone.utc).timestamp() if began.tzinfo is None else began.timestamp()
        matches = []
        for path in self.paths.backups.joinpath('manifests').glob('*.json'):
            manifest = read_json(path, private=False)
            created = datetime.fromisoformat(manifest['created_at_utc'].replace('Z', '+00:00')).timestamp()
            if stamp <= created <= self.now() + 5:
                matches.append((path, manifest))
        if len(matches) != 1:
            raise Blocked('one fresh official backup is required')
        path, manifest = matches[0]
        recovery = manifest.get('pg03_recovery', {})
        if (manifest.get('backup_identity_schema_version') != 1
                or recovery.get('consistency') != 'unchanged_across_dump_and_bundle'
                or recovery.get('scope') != ['compose-pg03.yaml', 'config/pg03', 'pool-executor/auths', 'pool-executor/pool', 'pool-executor/receipts']
                or any(not re.fullmatch(r'[a-f0-9]{64}', manifest.get('artifacts', {}).get(k, {}).get('sha256', ''))
                       for k in ('database_dump', 'pg03_state_archive', 'pg03_state_snapshot'))):
            raise Blocked('backup identity or PG03 recovery scope missing')
        running = manifest['running_identity']
        if any(running.get(key) != value for key, value in request['source'].items()
               if key not in ('started_at', 'binary_sha256', 'process_started_ticks', 'restart_policy', 'dsn_file')):
            raise Blocked('backup does not bind the reviewed OLD source')
        artifact = manifest['encryption']['encrypted_artifact']
        ident = manifest['backup_id']
        if not re.fullmatch(r'\d{8}T\d{6}Z', ident) or artifact['name'] != 'clearapi-backup-' + ident + '.tar.gz.gpg':
            raise Blocked('invalid encrypted backup identity')
        raw = read_file(self.paths.backups / 'encrypted' / ident / artifact['name'], maximum=1024 * 1024 * 1024)
        if len(raw) != artifact['bytes'] or digest(raw) != artifact['sha256']:
            raise Blocked('encrypted backup bytes do not match the official manifest')
        return {'started_at': started, 'backup_id': ident, 'manifest_sha256': digest(read_file(path, private=False)),
            'encrypted_sha256': artifact['sha256'], 'request_sha256': digest(read_file(self.paths.request))}


def main():
    if os.geteuid() != 0:
        raise Blocked('root entry point required')
    transition = Transition()
    if sys.argv[1:] == ['review', '--login']:
        if not sys.stdin.isatty():
            raise Blocked('private root terminal required')
        import getpass
        transition.io.gateway()
        data = {k: input(label).strip() for k, label in (('commit', 'Reviewed main commit: '),
            ('digest', 'Exact candidate digest: '), ('rc_run', 'Successful same-main RC run: '),
            ('evidence_ref', 'Non-secret review evidence reference: '))}
        data['token'] = getpass.getpass('GitHub read credential (not echoed): ')
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), PROBE.NoRedirect())
        data['login_response'] = json.loads(PROBE.interactive_login(opener), object_pairs_hook=CONTROL.unique)
        print(transition.review(data))
        return
    if sys.argv[1:] == ['review']:
        raw = sys.stdin.buffer.read(131073)
        if len(raw) > 131072:
            raise Blocked('oversized private review input')
        data = json.loads(raw, object_pairs_hook=CONTROL.unique)
        print(transition.review(data))
        return
    if len(sys.argv) not in (6, 7) or sys.argv[1] != 'phase':
        raise Blocked('explicit review or V2 phase entry point required')
    phase, run, image_digest, commit = sys.argv[2:6]
    if not re.fullmatch(r'[1-9][0-9]*', run) or not re.fullmatch(r'sha256:[a-f0-9]{64}', image_digest) or not re.fullmatch(r'[a-f0-9]{40}', commit):
        raise Blocked('invalid release identity')
    print(transition.phase(phase, run, image_digest, commit, sys.argv[6] if len(sys.argv) == 7 else None))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # No raw exception, request body, token, DSN or Docker output in logs.
        print('LEGACY_TRANSITION_BLOCKED: preserve evidence and maintenance state; no automatic recovery', file=sys.stderr)
        sys.exit(1)
