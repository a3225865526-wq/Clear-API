"""Six-service target support for local-deploy-vps.py; no independent entry point."""
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time

SERVICES = {'new-api', 'gateway-postgres', 'sub2api', 'sub2api-postgres', 'sub2api-redis', 'edge'}
WRITERS = ['edge', 'new-api', 'sub2api']
DATABASES = [('gateway-postgres', 'clearapi', 'clearapi'), ('sub2api-postgres', 'sub2api', 'sub2api')]
CONFIG = Path('/opt/clearapi/config/stack-target.json')
ACTIVE = Path('/opt/clearapi/config/stack-target.enabled')


def deployment_interrupted(number, frame):
    raise InterruptedError('deployment transport interrupted: signal ' + str(number))


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


class StackTarget:
    def __init__(self, host):
        self.h = host
        if host.ACTIVATED.exists() or Path('/opt/clearapi/app/local-deploy-state.json').exists():
            raise host.DeployError('legacy target already activated; six-service target must be independent')
        raw = host.STATE.read_file(CONFIG, private=True)
        self.c = json.loads(raw, object_pairs_hook=host.STATE.unique)
        c = self.c
        if (c['schema'] != 'clearapi.six-service-target/v1'
                or not re.fullmatch(r'[a-z][a-z0-9-]{2,40}', c['target_id'])
                or host.STATE.read_file(ACTIVE, private=True).decode().strip() != 'STACK_TARGET_APPROVED_V1:' + c['target_id']
                or c['daemon_id'] != host.run('docker', 'info', '--format', '{{.ID}}')
                or c['arch'] != host.arch()):
            raise host.DeployError('stack target activation/daemon/architecture mismatch')
        self.root = Path('/opt/clearapi/stack')
        for name in ['releases', 'incoming', 'backups', 'receipts']:
            path = self.root / name
            if not path.is_dir() or path.is_symlink() or path.stat().st_uid != 0 or path.stat().st_mode & 0o022:
                raise host.DeployError('unsafe stack directory')
        self.config_sha = hashlib.sha256(raw).hexdigest()
        if not re.fullmatch(r'(?:[A-Fa-f0-9]{40}|[A-Fa-f0-9]{64})', c['backup_recipient']):
            raise host.DeployError('backup encryption requires an exact owner key fingerprint')
        protected = {'stack.env', 'secrets/token-key', 'secrets/admin-password', 'secrets/terms.txt', 'secrets/privacy.txt'}
        if set(c['protected_inputs']) != protected:
            raise host.DeployError('incomplete protected runtime input identity')
        secrets = Path('/opt/clearapi/config/secrets')
        if secrets.is_symlink() or secrets.stat().st_uid != 0 or secrets.stat().st_mode & 0o022:
            raise host.DeployError('unsafe secret directory')
        for name, expected in c['protected_inputs'].items():
            source = CONFIG.parent / name
            host.secure_file(source, 0o600)
            if digest(source) != expected:
                raise host.DeployError('protected runtime configuration or key changed')
        for name, expected in c['helper_files'].items():
            if name not in ['local-deploy-vps.py', 'local-deploy-stack.py', 'local-deploy-state.py']:
                raise host.DeployError('unsupported helper dependency')
            installed = Path(host.__file__) if name == 'local-deploy-vps.py' else Path(__file__).with_name(name)
            if hashlib.sha256(host.STATE.read_file(installed)).hexdigest() != expected:
                raise host.DeployError('installed helper identity mismatch')
        if set(c['helper_files']) != {'local-deploy-vps.py', 'local-deploy-stack.py', 'local-deploy-state.py'}:
            raise host.DeployError('incomplete helper identity')
        self.state_path = self.root / 'state.json'
        self.journal = self.root / 'pending.json'

    def read(self, path):
        return json.loads(self.h.STATE.read_file(path, private=True), object_pairs_hook=self.h.STATE.unique)

    def write(self, path, data):
        self.h.atomic_write(path, (json.dumps(data, sort_keys=True) + '\n').encode())

    def state(self):
        return self.read(self.state_path) if self.state_path.exists() else None

    def envfile(self, state=None):
        return Path((state or {}).get('env_file', '/opt/clearapi/config/stack.env'))

    def dc(self, release, *args, state=None):
        return ['docker', 'compose', '-p', self.c['target_id'], '--env-file', str(self.envfile(state)),
                '-f', str(Path(release) / 'compose.yaml'), *args]

    def call(self, release, *args, state=None):
        # sudo may allocate a PTY even when SSH itself has no terminal. A batch
        # deployment must never let Compose run infer a terminal from that FD.
        if args and args[0] == 'run':
            args = ('run', '-T', *args[1:])
        result = subprocess.run(self.dc(release, *args, state=state), capture_output=True, text=True,
                                stdin=subprocess.DEVNULL, env=self.h.STATE.compose_environment())
        if result.returncode:
            detail = result.stderr[-2000:]
            values = [line.split('=', 1)[1] for line in self.envfile(state).read_text().splitlines() if '=' in line]
            values += [p.read_text().strip() for p in Path('/opt/clearapi/config/secrets').iterdir() if p.is_file() and p.stat().st_size <= 128]
            for value in values:
                if len(value) >= 8:
                    detail = detail.replace(value, '[REDACTED]')
            raise self.h.DeployError('six-service Compose ' + args[0] + ' failed: ' + detail.strip())
        return result.stdout.strip()

    def binary(self, args, data=None):
        options = {'input': data} if data is not None else {'stdin': subprocess.DEVNULL}
        p = subprocess.run(args, capture_output=True, env=self.h.STATE.compose_environment(), **options)
        if p.returncode:
            raise self.h.DeployError('stack command failed: ' + args[0] + ' (exit ' + str(p.returncode) + ')')
        return p.stdout

    def capacity(self, archive_bytes=0):
        memory = int(self.h.run('docker', 'info', '--format', '{{.MemTotal}}'))
        # Host total and available are distinct; cgroups are verified in the lab.
        available = next(int(x.split()[1]) * 1024 for x in Path('/proc/meminfo').read_text().splitlines() if x.startswith('MemAvailable:'))
        if memory < 1900 * 1024**2 or available < self.c['minimum_available_bytes']:
            raise self.h.DeployError('insufficient host memory')
        if shutil.disk_usage(self.root).free < max(self.c['minimum_free_bytes'], archive_bytes * 4):
            raise self.h.DeployError('insufficient disk including image/backup headroom')

    def effective(self, release, state=None):
        manifest = self.read_public(Path(release) / 'SHA256.json')
        for name in ['compose.yaml', 'nginx.conf', 'images.lock.json']:
            if digest(Path(release) / name) != manifest[name]:
                raise self.h.DeployError('deployed configuration drift: ' + name)
        self.h.STATE.read_file(self.envfile(state), private=True)
        obj = json.loads(self.call(release, 'config', '--format', 'json', state=state))
        if set(obj['services']) != SERVICES:
            raise self.h.DeployError('exact six-service layout required')
        total = 0
        for name, service in obj['services'].items():
            if service.get('privileged') or service.get('network_mode') == 'host' or service.get('pull_policy') != 'never':
                raise self.h.DeployError('unsafe service privileges or online image dependency')
            limit = int(service.get('mem_limit', 0))
            if limit <= 0:
                raise self.h.DeployError('every service requires a memory limit')
            total += limit
            if not service.get('healthcheck') or service.get('logging', {}).get('driver') != 'local':
                raise self.h.DeployError('missing health/log rotation')
            for port in service.get('ports', []):
                if name not in ['edge', 'sub2api'] or port.get('host_ip') != '127.0.0.1':
                    raise self.h.DeployError('unexpected public port')
            for mount in service.get('volumes', []):
                if mount['type'] != 'bind' or not Path(mount['source']).is_relative_to('/opt/clearapi'):
                    raise self.h.DeployError('mount escapes new target')
                if not mount.get('read_only') and not any(Path(mount['source']).is_relative_to(self.root / name)
                                                         for name in ['data', 'sub2data', 'recovery']):
                    raise self.h.DeployError('persistent data escapes the new target data directories')
        if not 0 < total <= 1504 * 1024**2:
            raise self.h.DeployError('unbounded or excessive aggregate memory')
        env = obj['services']['new-api']['environment']
        secrets = [v for v in obj['services']['new-api']['volumes'] if v['target'] == '/run/clearapi-secrets']
        if len(secrets) != 1 or secrets[0]['source'] != '/opt/clearapi/config/secrets' or not secrets[0].get('read_only'):
            raise self.h.DeployError('protected secrets must be mounted read-only from the enrolled target')
        if (env.get('CLEARAPI_PRODUCTION') != 'true' or env.get('CLEARAPI_POOL_ENABLED') != 'false' or env.get('REDIS_CONN_STRING')
                or env.get('BATCH_UPDATE_ENABLED', 'false') != 'false'):
            raise self.h.DeployError('single durable wallet/PG03-off contract violated')
        return obj

    def healthy(self, release, state):
        config = self.effective(release, state)
        lock = self.read_public(Path(release) / 'images.lock.json')
        observed = {}
        for name in sorted(SERVICES):
            cid = self.call(release, 'ps', '-q', name, state=state)
            if not re.fullmatch(r'[a-f0-9]{64}', cid):
                raise self.h.DeployError('missing service: ' + name)
            info = json.loads(self.h.run('docker', 'inspect', cid))[0]
            if (not info['State']['Running'] or info['State']['OOMKilled'] or info['RestartCount'] > 0
                    or info['State'].get('Health', {}).get('Status') != 'healthy'):
                raise self.h.DeployError('unhealthy/OOM service: ' + name)
            tag = config['services'][name]['image']
            service = config['services'][name]
            # Classic config ID from image tar, independent of containerd manifest IDs.
            if info['Image'] != lock[tag]['config_id'] and self.h.image_id(tag) != lock[tag]['config_id']:
                raise self.h.DeployError('running image config mismatch: ' + name)
            if info['Image'] != self.h.run('docker', 'image', 'inspect', '--format', '{{.Id}}', tag):
                raise self.h.DeployError('running image differs from approved tag: ' + name)
            calculated = self.call(release, 'config', '--hash', name, state=state).split()
            labels = info['Config'].get('Labels', {})
            if len(calculated) != 2 or labels.get('com.docker.compose.config-hash') != calculated[1]:
                raise self.h.DeployError('running Compose configuration differs: ' + name)
            host = info['HostConfig']
            if (host['Memory'] != service['mem_limit'] or host['PidsLimit'] != service['pids_limit']
                    or host['NanoCpus'] != int(float(service['cpus']) * 1_000_000_000)
                    or 'memswap_limit' in service and host['MemorySwap'] != service['memswap_limit']):
                raise self.h.DeployError('running resource limits differ: ' + name)
            actual_env = dict(item.split('=', 1) for item in info['Config']['Env'])
            if any(actual_env.get(key) != str(value) for key, value in service.get('environment', {}).items()):
                raise self.h.DeployError('running environment differs: ' + name)
            mounts = {(v['Destination'], v['Source'], v['RW']) for v in info['Mounts']}
            expected_mounts = {(v['target'], v['source'], not v.get('read_only', False)) for v in service['volumes']}
            if mounts != expected_mounts:
                raise self.h.DeployError('running persistence/secret mounts differ: ' + name)
            networks = {config['networks'][key]['name'] for key in service['networks']}
            if set(info['NetworkSettings']['Networks']) != networks:
                raise self.h.DeployError('running network membership differs: ' + name)
            ports = {(key, item['HostIp'], item['HostPort']) for key, values in info['NetworkSettings']['Ports'].items() for item in values or []}
            expected_ports = {(str(p['target']) + '/' + p.get('protocol', 'tcp'), p['host_ip'], str(p['published'])) for p in service.get('ports', [])}
            if ports != expected_ports:
                raise self.h.DeployError('running port bindings differ: ' + name)
            observed[name] = {'container_id': cid, 'image_id': info['Image'], 'health': 'healthy',
                              'restart_count': info['RestartCount'], 'config_hash': calculated[1]}
        # Local edge probes avoid unauthenticated external requests during preparation.
        edge = observed['edge']['container_id']
        self.h.run('docker', 'exec', edge, 'curl', '-fsS', '--max-time', '8', 'http://127.0.0.1:8080/api/status')
        frontend = self.h.run('docker', 'exec', edge, 'curl', '-fsS', '--max-time', '8', 'http://127.0.0.1:8080/')
        if '<html' not in frontend.lower():
            raise self.h.DeployError('Gateway frontend missing')
        observed['new-api']['frontend_sha256'] = hashlib.sha256(frontend.encode()).hexdigest()
        sub = observed['sub2api']['container_id']
        frontend = self.h.run('docker', 'exec', sub, 'wget', '-q', '-O', '-', 'http://127.0.0.1:8080/login')
        if '<html' not in frontend.lower():
            raise self.h.DeployError('Sub2API frontend missing')
        observed['sub2api']['frontend_sha256'] = hashlib.sha256(frontend.encode()).hexdigest()
        return observed

    def read_public(self, path):
        return json.loads(self.h.STATE.read_file(path), object_pairs_hook=self.h.STATE.unique)

    def verify_readiness(self, release, state=None):
        output = self.call(release, 'exec', '-T', 'new-api', '/production-bootstrap', '--verify-only', state=state)
        # The native CLI deliberately exits 0 for "not_bootstrapped" as well.
        # A successful command alone must never become a deployment PASS.
        records = [json.loads(line) for line in output.splitlines() if line.startswith('{')]
        if not records or records[-1].get('status') != 'verified':
            raise self.h.DeployError('native production readiness is not verified')

    def preflight(self):
        self.capacity()
        self.h.STATE.read_file('/opt/clearapi/config/stack.env', private=True)
        state = self.state()
        if state:
            if digest(self.envfile(state)) != state['env_sha256']:
                raise self.h.DeployError('runtime environment changed')
            health = self.healthy(state['release'], state) if not self.journal.exists() else None
            if health:
                self.verify_readiness(state['release'], state)
        else:
            if not self.journal.exists() and self.h.run('docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + self.c['target_id']):
                raise self.h.DeployError('foreign unmanaged project')
            health = None
        return {'target_id': self.c['target_id'], 'target_mode': 'six-service-v1', 'arch': self.c['arch'],
                'isolated': self.c['isolated'], 'config_sha256': self.config_sha,
                'helper_files': self.c['helper_files'], 'generation': digest(self.state_path) if state else 'EMPTY',
                'healthy': bool(health), 'pending': self.journal.exists(), 'services': health}

    def unpack(self, a):
        if (a.bundle_sha256 != self.c['bundle_sha256'] or not self.h.HEX.fullmatch(a.bundle_sha256)
                or a.bundle_bytes != self.c['bundle_bytes'] or a.bundle_bytes <= 0):
            raise self.h.DeployError('bundle not independently approved by root')
        self.capacity(a.bundle_bytes)
        incoming = self.root / 'incoming' / (a.bundle_sha256 + '.tar')
        with incoming.open('wb') as out:
            remaining = a.bundle_bytes
            while remaining:
                chunk = sys.stdin.buffer.read(min(1024**2, remaining))
                if not chunk:
                    raise self.h.DeployError('truncated SSH upload')
                out.write(chunk)
                remaining -= len(chunk)
            if sys.stdin.buffer.read(1):
                raise self.h.DeployError('oversized upload')
        if digest(incoming) != a.bundle_sha256:
            raise self.h.DeployError('upload checksum mismatch')
        release = self.root / 'releases' / a.bundle_sha256
        # Never extract over an existing release or rewrite a mounted file inode.
        if not release.exists():
            release.mkdir(mode=0o700)
            with tarfile.open(incoming, 'r:') as archive:
                members = archive.getmembers()
                if len({m.name for m in members}) != len(members):
                    raise self.h.DeployError('duplicate archive member')
                for member in members:
                    if not member.isfile() or Path(member.name).name != member.name or member.size > a.bundle_bytes:
                        raise self.h.DeployError('unsafe archive member')
                archive.extractall(release, members=members, filter='data')
        manifest = self.read_public(release / 'SHA256.json')
        if set(p.name for p in release.iterdir()) != set(manifest) | {'SHA256.json'}:
            raise self.h.DeployError('unmanifested package content')
        for name, expected in manifest.items():
            if Path(name).name != name or digest(release / name) != expected:
                raise self.h.DeployError('package checksum mismatch')
        if manifest['gateway-source.tar'] != self.c['source_archive_sha256']:
            raise self.h.DeployError('source archive not approved')
        with tarfile.open(release / 'gateway-source.tar') as source:
            if source.pax_headers.get('comment') != self.c['gateway_commit']:
                raise self.h.DeployError('source commit mismatch')
        config = self.effective(release, self.state())
        if config['services']['new-api']['environment'].get('CLEARAPI_PRODUCTION') != 'true':
            raise self.h.DeployError('candidate must retain the actual production readiness gate')
        lock = self.read_public(release / 'images.lock.json')
        previous = self.state()
        if previous:
            old_config = self.effective(previous['release'], previous)
            old_lock = self.read_public(Path(previous['release']) / 'images.lock.json')
            for tag in set(old_lock) & set(lock):
                if old_lock[tag]['config_id'] != lock[tag]['config_id']:
                    raise self.h.DeployError('immutable image tag changed; use a distinct release tag')
            for service, _, _ in DATABASES + [('sub2api-redis', '', '')]:
                old_tag = old_config['services'][service]['image']
                new_tag = config['services'][service]['image']
                if old_lock[old_tag]['config_id'] != lock[new_tag]['config_id']:
                    raise self.h.DeployError('stateful engine version change requires separate migration review')
        if {v['image'] for v in config['services'].values()} != set(lock):
            raise self.h.DeployError('image inventory mismatch')
        with tarfile.open(release / 'images.tar') as images:
            entries = json.load(images.extractfile('manifest.json'))
            found = {}
            for item in entries:
                raw = images.extractfile(item['Config']).read()
                metadata = json.loads(raw)
                for tag in item['RepoTags']:
                    found[tag] = 'sha256:' + hashlib.sha256(raw).hexdigest()
                if metadata['architecture'] != self.c['arch'] or metadata['os'] != 'linux':
                    raise self.h.DeployError('image platform mismatch')
                if any(tag.startswith('clearapi-local:') for tag in item['RepoTags']):
                    if metadata['config']['Labels'].get('org.opencontainers.image.revision') != self.c['gateway_commit']:
                        raise self.h.DeployError('Gateway OCI revision mismatch')
            if found != {tag: value['config_id'] for tag, value in lock.items()}:
                raise self.h.DeployError('image archive config identity mismatch')
        return release

    def fence(self, release, state=None):
        self.call(release, 'stop', '-t', '30', *WRITERS, state=state)

    def query_digest(self, release, service, user, db, script, state):
        command = self.dc(release, 'exec', '-T', service, 'psql', '-U', user, '-d', db,
                          '-At', '--set', 'ON_ERROR_STOP=1', state=state)
        hashed = hashlib.sha256()
        with tempfile.TemporaryFile() as query:
            query.write(script.encode())
            query.seek(0)
            with subprocess.Popen(command, stdin=query, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                  env=self.h.STATE.compose_environment()) as process:
                for row in process.stdout:
                    hashed.update(row)
                if process.wait():
                    raise self.h.DeployError('database snapshot hashing failed')
        return hashed.hexdigest()

    def data_digest(self, release, state):
        result = {}
        for service, user, db in DATABASES:
            names = self.call(release, 'exec', '-T', service, 'psql', '-U', user, '-d', db, '-Atc',
                              "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename", state=state).splitlines()
            queries = []
            for name in names:
                if not re.fullmatch(r'[a-zA-Z0-9_]+', name):
                    raise self.h.DeployError('unexpected table identifier')
                queries.append(f"SELECT 'table:{name}'; SELECT row_to_json(t)::text FROM \"{name}\" t ORDER BY 1;")
            result[db] = self.query_digest(release, service, user, db, '\n'.join(queries), state)
        return hashlib.sha256(json.dumps(result, sort_keys=True).encode()).hexdigest()

    def financial_digest(self, release, state):
        fields = {'users': 'id,quota,used_quota,request_count', 'tokens': 'id,remain_quota,used_quota',
                  'billing_reservations': 'id,request_id,reserved,target,applied,state',
                  'logs': 'id,request_id,quota,type'}
        queries = [f"SELECT 'table:{table}'; SELECT row_to_json(t)::text FROM (SELECT {columns} FROM {table} ORDER BY id) t;" for table, columns in fields.items()]
        return self.query_digest(release, 'gateway-postgres', 'clearapi', 'clearapi', '\n'.join(queries), state)

    def schema_digest(self, release, state):
        schemas = {}
        for service, user, db in DATABASES:
            raw = self.binary(self.dc(release, 'exec', '-T', service, 'pg_dump', '-U', user, '-d', db,
                                      '--schema-only', '--no-owner', '--no-acl', state=state)).decode()
            # pg_dump 17 emits a random psql \restrict token, not schema content.
            canonical = '\n'.join(line for line in raw.splitlines() if not re.match(r'^\\(?:un)?restrict ', line))
            schemas[db] = hashlib.sha256(canonical.encode()).hexdigest()
        return schemas

    def backup(self, release, state):
        bid = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S') + '-' + os.urandom(4).hex()
        target = self.root / 'backups' / bid
        target.mkdir(mode=0o700)
        # Existing backup implementation, same project, same protected env.
        self.binary(['env', 'COMPOSE_PROJECT_NAME=' + self.c['target_id'], 'sh',
                     str(Path(release) / 'backup.sh'), str(self.envfile(state)), str(target / 'plain')])
        data_hash = self.data_digest(release, state)
        plain = target / 'snapshot.tar'
        with tarfile.open(plain, 'w:') as bundle:
            bundle.add(target / 'plain', arcname='snapshot')
            for name in ['stack.env', 'secrets', 'stack-target.json', 'stack-target.enabled']:
                bundle.add(Path('/opt/clearapi/config') / name, arcname='config/' + name)
        encrypted = target / 'snapshot.tar.gpg'
        self.h.run('gpg', '--batch', '--yes', '--trust-model', 'always', '--recipient', self.c['backup_recipient'],
                   '--output', str(encrypted), '--encrypt', str(plain))
        proof = {'backup_id': bid, 'target_id': self.c['target_id'], 'release': str(release),
                 'cipher_sha256': digest(encrypted), 'bytes': encrypted.stat().st_size,
                 'data_digest': data_hash, 'data_digest_schema': 'public-json-sha256/v1',
                 'env_sha256': digest(self.envfile(state)), 'daemon_id': self.c['daemon_id'],
                 'config_sha256': self.config_sha,
                 'image_configs': self.read_public(Path(release) / 'images.lock.json')}
        proof['schema_digest'] = self.schema_digest(release, state)
        if state:
            proof['financial_digest'] = self.financial_digest(release, state)
        self.write(target / 'proof.json', proof)
        # Plaintext exists only in a root-private staging directory; retain on failure.
        plain.unlink()
        shutil.rmtree(target / 'plain')
        return proof

    def receipt(self, result):
        result.update(target_id=self.c['target_id'], config_sha256=self.config_sha,
                      helper_files=self.c['helper_files'],
                      at=dt.datetime.now(dt.timezone.utc).isoformat())
        name = str(time.time_ns()) + '.json'
        self.write(self.root / 'receipts' / name, result)
        return result

    def apply(self, a):
        before = self.preflight()
        if before['pending']:
            raise self.h.DeployError('incomplete deployment; explicit stack-recover required')
        if before['generation'] != a.expected_generation:
            raise self.h.DeployError('predecessor generation changed')
        release = self.unpack(a)
        old = self.state()
        if old and old['bundle_sha256'] == a.bundle_sha256:
            return self.receipt({'result': 'already_applied', 'bundle_sha256': a.bundle_sha256, 'services': before['services']})
        journal = {'phase': 'received', 'previous': old, 'candidate': str(release), 'bundle_sha256': a.bundle_sha256}
        self.write(self.journal, journal)
        try:
            self.h.run('docker', 'load', '-i', str(release / 'images.tar'))
            if old:
                self.fence(old['release'], old)
                journal['backup'] = self.backup(old['release'], old)
            else:
                self.call(release, 'up', '-d', '--wait', '--wait-timeout', '150', 'gateway-postgres', 'sub2api-postgres', 'sub2api-redis')
                journal['backup'] = self.backup(release, {})
            journal['phase'] = 'backup_ready'
            self.write(self.journal, journal)
            # Persist before DB images/migration/application can touch persistent state.
            journal['phase'] = 'switching'
            self.write(self.journal, journal)
            self.call(release, 'up', '-d', '--wait', '--wait-timeout', '150', 'gateway-postgres', 'sub2api-postgres', 'sub2api-redis', state=old)
            if not old:
                bootstrap = ['--execute', '--candidate-commit', self.c['gateway_commit']]
                for key, value in self.c['bootstrap'].items():
                    if key not in ['admin-username', 'admin-password-file', 'terms-file', 'privacy-file', 'terms-version', 'privacy-version', 'approved-by']:
                        raise self.h.DeployError('unapproved bootstrap argument')
                    bootstrap += ['--' + key, value]
                self.call(release, 'run', '--rm', '--no-deps', '--entrypoint', '/production-bootstrap', 'new-api', *bootstrap)
            self.call(release, 'up', '-d', '--wait', '--wait-timeout', '180', 'new-api', 'sub2api', state=old)
            state = {'release': str(release), 'bundle_sha256': a.bundle_sha256,
                     'env_file': str(self.envfile(old)), 'env_sha256': digest(self.envfile(old)), 'backup_id': journal['backup']['backup_id']}
            # Gate traffic on the existing production-bootstrap readiness contract.
            self.verify_readiness(release, state)
            if old and self.financial_digest(release, state) != journal['backup']['financial_digest']:
                raise self.h.DeployError('FINANCIAL_INCONSISTENCY: keep ingress fenced; no automatic wallet/DB rollback')
            self.call(release, 'up', '-d', '--no-deps', '--wait', '--wait-timeout', '60', 'edge', state=old)
            health = self.healthy(release, state)
            self.write(self.state_path, state)
            self.journal.unlink()
            return self.receipt({'result': 'success', **state, 'services': health, 'rollback': False})
        except BaseException as error:
            # This also handles disconnect/SIGINT while retaining a durable phase marker.
            try:
                self.fence(release)
            except self.h.DeployError:
                pass
            self.receipt({'result': 'failed', 'phase': journal['phase'], 'bundle_sha256': a.bundle_sha256,
                          'error': str(error), 'backup_id': journal.get('backup', {}).get('backup_id'),
                          'rollback': 'manual_after_switch' if journal['phase'] == 'switching' else 'recover_before_switch'})
            raise

    def recover(self, a):
        self.capacity()
        if a.backup_id:
            return self.restore(a.backup_id)
        if not self.journal.exists():
            state = self.state()
            self.verify_readiness(state['release'], state)
            return self.receipt({'result': 'no_pending_deployment', 'services': self.healthy(state['release'], state)})
        journal = self.read(self.journal)
        old = journal['previous']
        if a.resume_previous:
            if not old:
                raise self.h.DeployError('no previous release to resume')
            approval = self.read('/opt/clearapi/config/stack-recovery.json')
            self.h.STATE.read_file('/opt/clearapi/config/stack-recovery.json', private=True)
            self.fence(journal['candidate'], old)
            self.call(old['release'], 'up', '-d', '--wait', '--wait-timeout', '120',
                      'gateway-postgres', 'sub2api-postgres', 'sub2api-redis', state=old)
            current = self.data_digest(old['release'], old)
            previous_config = self.effective(old['release'], old)
            candidate_config = self.effective(journal['candidate'], old)
            old_lock = self.read_public(Path(old['release']) / 'images.lock.json')
            candidate_lock = self.read_public(Path(journal['candidate']) / 'images.lock.json')
            previous_image = old_lock[previous_config['services']['new-api']['image']]['config_id']
            candidate_image = candidate_lock[candidate_config['services']['new-api']['image']]['config_id']
            if (approval.get('action') != 'resume_previous_images' or approval.get('target_id') != self.c['target_id']
                    or approval.get('candidate_bundle_sha256') != journal['bundle_sha256']
                    or approval.get('observed_data_digest') != current or not approval.get('approved_by')
                    or previous_image != candidate_image
                    or self.financial_digest(old['release'], old) != journal['backup']['financial_digest']):
                raise self.h.DeployError('MANUAL_RECONCILIATION_REQUIRED: restore authorization, app identity or financial data changed')
            self.call(old['release'], 'up', '-d', '--wait', '--wait-timeout', '180', state=old)
            health = self.healthy(old['release'], old)
            self.verify_readiness(old['release'], old)
            self.journal.unlink()
            return self.receipt({'result': 'previous_images_resumed_without_data_restore', 'services': health,
                                 'database_snapshot_restored': False, 'latest_data_preserved': True})
        if journal['phase'] == 'switching' and old is None:
            if journal['bundle_sha256'] != self.c['bundle_sha256']:
                raise self.h.DeployError('fresh initialization resume requires the exact original approved bundle')
            release = journal['candidate']
            self.fence(release)
            bootstrap = ['--execute', '--candidate-commit', self.c['gateway_commit']]
            for key, value in self.c['bootstrap'].items():
                if key not in ['admin-username', 'admin-password-file', 'terms-file', 'privacy-file', 'terms-version', 'privacy-version', 'approved-by']:
                    raise self.h.DeployError('unapproved bootstrap argument')
                bootstrap += ['--' + key, value]
            self.call(release, 'run', '--rm', '--no-deps', '--entrypoint', '/production-bootstrap', 'new-api', *bootstrap)
            self.call(release, 'up', '-d', '--wait', '--wait-timeout', '180')
            state = {'release': release, 'bundle_sha256': journal['bundle_sha256'],
                     'env_file': str(self.envfile()), 'env_sha256': digest(self.envfile()), 'backup_id': journal['backup']['backup_id']}
            health = self.healthy(release, state)
            self.verify_readiness(release)
            self.write(self.state_path, state)
            self.journal.unlink()
            return self.receipt({'result': 'initialized_after_interruption', **state, 'services': health})
        if journal['phase'] == 'received' and old is None:
            ids = self.call(journal['candidate'], 'ps', '-aq').splitlines()
            never_started = all(json.loads(self.h.run('docker', 'inspect', cid))[0]['State']['StartedAt'] ==
                                '0001-01-01T00:00:00Z' for cid in ids)
            if never_started:
                self.call(journal['candidate'], 'rm', '-f')
                self.journal.unlink()
                return self.receipt({'result': 'unstarted_initialization_cleared', 'data_removed': False})
        if journal['phase'] in ['received', 'backup_ready'] and old:
            self.call(old['release'], 'up', '-d', '--wait', '--wait-timeout', '180', state=old)
            health = self.healthy(old['release'], old)
            self.verify_readiness(old['release'], old)
            self.journal.unlink()
            return self.receipt({'result': 'pre_switch_restored', 'services': health})
        raise self.h.DeployError('MANUAL_RECOVERY_REQUIRED: candidate may have written DB; keep ingress fenced, preserve volumes and inspect fresh encrypted backup')

    def restore(self, bid):
        if not re.fullmatch(r'[0-9T-]+[a-f0-9]{8}', bid):
            raise self.h.DeployError('invalid backup ID')
        approval = self.read('/opt/clearapi/config/stack-recovery.json')
        backup = self.root / 'backups' / bid
        proof = self.read(backup / 'proof.json')
        material = Path('/opt/clearapi/config/stack-restore.tar')
        self.h.STATE.read_file('/opt/clearapi/config/stack-recovery.json', private=True)
        self.h.secure_file(material, 0o600)
        if (approval['target_id'] != self.c['target_id'] or approval['backup_id'] != bid
                or not approval['approved_by'] or approval['plaintext_sha256'] != digest(material)
                or proof.get('target_id') != self.c['target_id'] or proof.get('daemon_id') != self.c['daemon_id']
                or proof.get('data_digest_schema') != 'public-json-sha256/v1'
                or digest(backup / 'snapshot.tar.gpg') != proof['cipher_sha256']):
            raise self.h.DeployError('explicit restore authorization/artifact mismatch')
        journal = self.read(self.journal) if self.journal.exists() else None
        old = journal['previous'] if journal else self.state()
        if not old:
            raise self.h.DeployError('fresh failed initialization requires owner inspection; no existing data is overwritten')
        self.fence(journal['candidate'] if journal else old['release'], old)
        # Never restore a snapshot over newer wallet/order data, even with a generic approval.
        self.call(old['release'], 'up', '-d', '--wait', '--wait-timeout', '120',
                  'gateway-postgres', 'sub2api-postgres', 'sub2api-redis', state=old)
        current = self.data_digest(old['release'], old)
        if current != proof['data_digest'] or current != approval['observed_data_digest']:
            raise self.h.DeployError('NEW_WRITES_DETECTED: preserve databases; owner reconciliation required, no snapshot rollback')
        recovery = self.root / 'recovery' / (bid + '-' + os.urandom(4).hex())
        recovery.mkdir(mode=0o700, parents=True)
        with tarfile.open(material) as archive:
            archive.extractall(recovery / 'material', filter='data')
        plain = recovery / 'material/snapshot'
        expected = {}
        for line in (plain / 'SHA256SUMS').read_text().splitlines():
            sha, name = line.split(None, 1)
            expected[name.strip()] = sha
        for name, sha in expected.items():
            if Path(name).name != name or digest(plain / name) != sha:
                raise self.h.DeployError('decrypted backup checksum mismatch')
        root = recovery / 'data'
        subroot = recovery / 'sub2data'
        root.mkdir(); subroot.mkdir()
        for name, destination in [('app-state.tar', root), ('sub2api-app.tar', subroot)]:
            with tarfile.open(plain / name) as archive:
                archive.extractall(destination, filter='data')
        (subroot / 'redis').mkdir()
        shutil.copy2(plain / 'redis.rdb', subroot / 'redis/dump.rdb')
        lines = (plain / 'production.env').read_text().splitlines()
        lines = [('ROOT=' + str(root)) if line.startswith('ROOT=') else
                 ('SUB2_ROOT=' + str(subroot)) if line.startswith('SUB2_ROOT=') else line for line in lines]
        envpath = recovery / 'restored.env'
        self.h.atomic_write(envpath, ('\n'.join(lines) + '\n').encode())
        restored = {**old, 'env_file': str(envpath), 'env_sha256': digest(envpath)}
        self.call(old['release'], 'stop', '-t', '30', state=old)
        self.call(old['release'], 'up', '-d', '--wait', '--wait-timeout', '150',
                  'gateway-postgres', 'sub2api-postgres', state=restored)
        for service, user, db in DATABASES:
            filename = 'gateway.dump' if service == 'gateway-postgres' else 'sub2api.dump'
            with (plain / filename).open('rb') as data:
                result = subprocess.run(self.dc(old['release'], 'exec', '-T', service, 'pg_restore', '-U', user,
                                               '-d', db, '--no-owner', '--no-acl', state=restored), stdin=data,
                                        capture_output=True, env=self.h.STATE.compose_environment())
            if result.returncode:
                raise self.h.DeployError('restore into new PostgreSQL directory failed; original volumes preserved')
        # Import RDB with AOF disabled, then enable/rewrite AOF before ordinary startup.
        override = recovery / 'redis-import.json'
        self.write(override, {'services': {'sub2api-redis': {'command': ['sh', '-c',
            'exec redis-server --appendonly no --maxmemory 48mb --maxmemory-policy noeviction --requirepass "$$REDIS_PASSWORD"']}}})
        args = self.dc(old['release'], state=restored) + ['-f', str(override)]
        self.h.run(*(args + ['up', '-d', '--wait', '--wait-timeout', '120', 'sub2api-redis']))
        self.h.run(*(args + ['exec', '-T', 'sub2api-redis', 'redis-cli', 'CONFIG', 'SET', 'appendonly', 'yes']))
        deadline = time.monotonic() + 60
        while True:
            info = self.h.run(*(args + ['exec', '-T', 'sub2api-redis', 'redis-cli', 'INFO', 'persistence']))
            if 'aof_rewrite_in_progress:0' in info and 'aof_last_bgrewrite_status:ok' in info:
                break
            if time.monotonic() > deadline:
                raise self.h.DeployError('Redis AOF rebuild timeout')
            time.sleep(1)
        if self.data_digest(old['release'], restored) != proof['data_digest']:
            raise self.h.DeployError('restored all-table data digest mismatch; original volumes preserved')
        if self.schema_digest(old['release'], restored) != proof['schema_digest']:
            raise self.h.DeployError('restored schema/index/constraint digest mismatch; original volumes preserved')
        self.call(old['release'], 'up', '-d', '--wait', '--wait-timeout', '180', state=restored)
        health = self.healthy(old['release'], restored)
        self.verify_readiness(old['release'], restored)
        self.write(self.state_path, restored)
        if self.journal.exists():
            self.journal.unlink()
        return self.receipt({'result': 'restored_into_new_directories', 'backup_id': bid,
                             'services': health, 'original_data_preserved': True, 'data_digest': proof['data_digest']})


def dispatch(host, args):
    if args.config_path:
        raise host.DeployError('stack mode forbids alternate/test config paths')
    for number in [signal.SIGHUP, signal.SIGTERM, signal.SIGINT]:
        signal.signal(number, deployment_interrupted)
    with host.deploy_lock():
        target = StackTarget(host)
        if args.mode == 'stack-preflight':
            result = target.preflight()
        elif args.mode == 'stack-receive':
            result = target.apply(args)
        elif args.mode == 'stack-backup':
            state = target.state()
            preflight = target.preflight()
            if preflight['pending'] or not preflight['healthy']:
                raise host.DeployError('cannot use backup to bypass an incomplete deployment')
            target.fence(state['release'], state)
            proof = target.backup(state['release'], state)
            if not args.keep_fenced:
                target.call(state['release'], 'up', '-d', '--wait', '--wait-timeout', '180', state=state)
                target.healthy(state['release'], state)
            result = target.receipt({'result': 'backup_complete_fenced' if args.keep_fenced else 'backup_complete', **proof})
        else:
            result = target.recover(args)
        print(json.dumps(result, sort_keys=True))
