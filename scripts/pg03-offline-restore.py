#!/usr/bin/env python3
"""Default-deny offline PostgreSQL restore runner, never a production executor.

Default mode prints a command plan. Execution requires a local root-owned policy,
its out-of-band hash, a signed expiring admin authorization, a pinned execution
spec, and a one-use durable claim on a dedicated empty offline Docker daemon.
An approved administrator supplies already decrypted bytes and a separately
authenticated decryption receipt. No private key, SSH, DSN or production loader.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import selectors
import signal
import shutil
import stat
import subprocess
import sys
import tarfile
import time

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('restore_audit', Path(__file__).with_name('pg03-restore-acceptance.py'))
AU = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AU)
SPEC = importlib.util.spec_from_file_location('pg03_state', Path(__file__).with_name('pg03-backup-state.py'))
STATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STATE)
PG_IMAGE = 'postgres:17.6-alpine@sha256:ef257d85f76e48da1c64832459b59fcaba1a4dac97bf5d7450c77753542eee94'
POLICY_PATH = Path('/etc/clearapi/offline-restore-policy.json')
PUBLIC_KEY = Path('/etc/clearapi/offline-restore.pub')
INPUT_ROOT = Path('/var/tmp/clearapi-offline-inputs')
OUTPUT_ROOT = Path('/var/tmp/clearapi-offline-results')
CLAIM_ROOT = Path('/var/lib/clearapi-offline-restore/claims')
DOCKER = '/usr/bin/docker'
OPENSSL = '/usr/bin/openssl'
SESSION = "SET timezone='UTC'; SET datestyle='ISO, YMD'; SET intervalstyle='postgres'; SET extra_float_digits=3; SET bytea_output='hex'; "


def now():
    return datetime.now(timezone.utc)


def stamp():
    return now().isoformat().replace('+00:00', 'Z')


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def private_file(path, expected=None):
    for parent in path.parents:
        info = parent.lstat()
        AU.require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0
                   and not info.st_mode & 0o022 or stat.S_ISDIR(info.st_mode)
                   and info.st_uid == 0 and info.st_mode & stat.S_ISVTX,
                   'unsafe administrator evidence directory')
    info = path.lstat()
    AU.require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
               and not info.st_mode & 0o077, 'private root-owned authorization required')
    raw = AU.document(path.parent, path.name, expected)
    return raw


def write_file(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def validate_spec(value, instant):
    AU.require(value['schema'] == 'clearapi.offline-restore-execute-spec/v1'
               and value['repository'] == 'a3225865526-wq/Clear-API'
               and re.fullmatch('[a-f0-9]{32}', value['authorization_id']) is not None
               and AU.valid_hash(value['challenge']) and value['postgres_image_ref'] == PG_IMAGE
               and AU.utc(value['issued_at_utc']) <= instant < AU.utc(value['expires_at_utc'])
               and 0 < (AU.utc(value['expires_at_utc'])-AU.utc(value['issued_at_utc'])).total_seconds() <= 7200,
               'invalid or expired execution spec')
    candidate = value['candidate']
    AU.require(set(candidate) == {'source_commit', 'image_digest', 'image_ref', 'image_id',
                'binary_sha256', 'rc_run_id', 'rc_attempt'}
               and re.fullmatch('[a-f0-9]{40}', candidate['source_commit']) is not None
               and re.fullmatch('sha256:[a-f0-9]{64}', candidate['image_digest']) is not None
               and re.fullmatch('sha256:[a-f0-9]{64}', candidate['image_id']) is not None
               and candidate['image_ref'] == 'ghcr.io/a3225865526-wq/clearapi@'+candidate['image_digest']
               and AU.valid_hash(candidate['binary_sha256'])
               and all(type(candidate[key]) is int and candidate[key] > 0 for key in ('rc_run_id', 'rc_attempt')),
               'invalid candidate identity')
    AU.require(set(value['input_sha256']) == {'backup-manifest.json', 'backup-completion.json',
                'source-inventory.json', 'predecryption-run.json'}
               and all(AU.valid_hash(v) for v in value['input_sha256'].values()), 'input evidence pins missing')
    AU.require(set(value['databases']) in ({'main'}, {'main', 'log'})
               and value['log_layout'] in ('shared', 'separate')
               and (value['log_layout'] == 'shared') == (set(value['databases']) == {'main'}),
               'log coverage mismatch')
    for name, database in value['databases'].items():
        AU.require(set(database) == {'target_database', 'dump_artifact'}
                   and re.fullmatch('clearapi_restore_[a-z0-9_]{1,40}', database['target_database']) is not None
                   and database['dump_artifact'] == ('database_dump' if name == 'main' else 'log_database_dump'),
                   'independent target required')
    AU.require(value['production_container_ids'] and all(re.fullmatch('[a-f0-9]{64}', v) is not None
               for v in value['production_container_ids']), 'known production identities required')
    return value


def command_plan(spec):
    name = 'clearapi-offline-restore-'+spec['authorization_id']
    return dict(schema='clearapi.offline-restore-command-plan/v1', mode='PLAN', production_execution_allowed=False,
        required_policy=str(POLICY_PATH), required_signature='separately approved administrator signature',
        image=PG_IMAGE, container_name=name, network='none', host_mounts=[],
        databases=spec['databases'], steps=['verify_signed_inputs', 'claim_once', 'require_empty_offline_daemon',
            'verify_ciphertext_and_predecrypted_bundle', 'validate_all_archive_members',
            'create_network_none_tmpfs_container', 'restore_exact_database_dumps',
            'collect_full_data_and_catalog_inventory', 'restore_pg03_into_independent_tmpfs_directory',
            'compare_source_inventory', 'remove_only_owned_container', 'audit_and_seal_result'])


class LocalCommands:
    """Bounded no-shell local processes; raw row data is hashed, never persisted."""
    def __init__(self, lease, daemon_id=None):
        self.lease = lease
        self.daemon_id = daemon_id

    def run(self, argv, stdin=None, hash_only=False, stdin_file=None, expected_sha256=None):
        self.lease()
        AU.require(argv[0] in (DOCKER, OPENSSL), 'unsupported executable')
        AU.require((stdin_file is None and expected_sha256 is None)
                   or (stdin is None and stdin_file is not None and AU.valid_hash(expected_sha256)),
                   'ambiguous or unpinned input stream')
        if self.daemon_id is not None and argv[0] == DOCKER and argv[1:] != ['info', '--format', '{{.ID}}']:
            daemon = self.run([DOCKER, 'info', '--format', '{{.ID}}'])
            AU.require(daemon['returncode'] == 0 and daemon['stdout'].decode().strip() == self.daemon_id,
                       'offline daemon identity changed')
        started, deadline = stamp(), time.monotonic()+600
        environment = {'PATH': '/usr/bin:/bin', 'HOME': '/nonexistent', 'LC_ALL': 'C',
                       'DOCKER_HOST': 'unix:///var/run/docker.sock', 'DOCKER_CONFIG': '/nonexistent'}
        input_stream, input_info = None, None
        try:
            if stdin_file is not None:
                path = Path(stdin_file)
                AU.local_file(path.parent, path.name)
                descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
                input_stream = os.fdopen(descriptor, 'rb')
                input_info = os.fstat(descriptor)
                AU.require(stat.S_ISREG(input_info.st_mode) and input_info.st_nlink == 1
                           and 0 < input_info.st_size <= AU.MAX_PAYLOAD, 'unsafe input stream')
                AU.require(hashlib.file_digest(input_stream, 'sha256').hexdigest() == expected_sha256,
                           'input stream hash mismatch')
                input_stream.seek(0)
                self.lease()
            AU.require(time.monotonic() < deadline, 'offline command timeout before start')
            process = subprocess.Popen(argv, stdin=input_stream if input_stream is not None else
                (subprocess.PIPE if stdin is not None else subprocess.DEVNULL),
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, close_fds=True,
                start_new_session=True)
        except BaseException:
            if input_stream is not None:
                input_stream.close()
            raise
        hashes = {'stdout': hashlib.sha256(), 'stderr': hashlib.sha256()}
        contents, sizes = {'stdout': bytearray(), 'stderr': bytearray()}, {'stdout': 0, 'stderr': 0}
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ, 'stdout')
        selector.register(process.stderr, selectors.EVENT_READ, 'stderr')
        offset = 0
        try:
            if stdin is not None:
                AU.require(len(stdin) <= 1024*1024, 'SQL input too large')
                if stdin:
                    os.set_blocking(process.stdin.fileno(), False)
                    selector.register(process.stdin, selectors.EVENT_WRITE, 'stdin')
                else:
                    process.stdin.close()
            while selector.get_map():
                self.lease()
                AU.require(time.monotonic() < deadline, 'offline command timeout')
                for key, _ in selector.select(timeout=0.2):
                    if key.data == 'stdin':
                        try:
                            written = os.write(key.fileobj.fileno(), stdin[offset:offset+4096])
                            offset += written
                        except BlockingIOError:
                            continue
                        except BrokenPipeError:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                            continue
                        if offset == len(stdin):
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                        continue
                    raw = os.read(key.fileobj.fileno(), 65536)
                    stream = key.data
                    if not raw:
                        selector.unregister(key.fileobj)
                        continue
                    hashes[stream].update(raw)
                    sizes[stream] += len(raw)
                    AU.require(sizes[stream] <= (AU.MAX_PAYLOAD if hash_only and stream == 'stdout'
                                               else AU.MAX_JSON), 'command output too large')
                    if stream == 'stderr' or not hash_only:
                        contents[stream].extend(raw)
            returncode = process.wait(timeout=1)
            AU.require(stdin is None or offset == len(stdin), 'offline command did not consume its input')
            if input_stream is not None:
                current = os.fstat(input_stream.fileno())
                linked = Path(stdin_file).stat(follow_symlinks=False)
                AU.require(input_stream.tell() == input_info.st_size
                    and all(getattr(input_info, key) == getattr(current, key) == getattr(linked, key)
                        for key in ('st_dev', 'st_ino', 'st_mode', 'st_nlink', 'st_size', 'st_mtime_ns', 'st_ctime_ns')),
                    'input stream changed or was not fully consumed')
        except BaseException:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait(timeout=2)
            raise
        finally:
            selector.close()
            process.stdout.close()
            process.stderr.close()
            if process.stdin is not None and not process.stdin.closed:
                process.stdin.close()
            if input_stream is not None:
                input_stream.close()
        record = dict(argv=argv, returncode=returncode, started_at_utc=started, completed_at_utc=stamp(),
                    stdout=bytes(contents['stdout']), stderr=bytes(contents['stderr']),
                    stdout_sha256=hashes['stdout'].hexdigest(), stderr_sha256=hashes['stderr'].hexdigest(),
                    stdout_bytes=sizes['stdout'], stderr_bytes=sizes['stderr'])
        if input_info is not None:
            record.update(stdin_sha256=expected_sha256, stdin_bytes=input_info.st_size)
        return record


class OfflineRestore:
    def __init__(self, spec, transport, inputs, output, lease):
        self.spec, self.transport, self.inputs, self.output, self.lease = spec, transport, inputs, output, lease
        self.cid = None
        self.manifest = None
        self.commands = {}
        self.journal = []

    def call(self, args, stdin=None, hash_only=False, allow_failure=False, stdin_file=None, expected_sha256=None):
        self.lease()
        result = self.transport.run([DOCKER, *args], stdin=stdin, hash_only=hash_only,
                                    stdin_file=stdin_file, expected_sha256=expected_sha256)
        self.journal.append({k:v for k, v in result.items() if k not in ('stdout', 'stderr')})
        write_file(self.output / ('command-%04d.json' % len(self.journal)), encoded(self.journal[-1]))
        AU.require(type(result['returncode']) is int and (allow_failure or result['returncode'] == 0),
                   'offline subprocess failed')
        return result

    def command_record(self, name, result):
        record = {k:v for k,v in result.items() if k not in ('stdout', 'stderr', 'stdout_bytes', 'stderr_bytes')}
        record['argv'] = ['docker', *record['argv'][1:]]
        for stream in ('stdout', 'stderr'):
            filename = name+'-'+stream+'.private'
            write_file(self.output / filename, result[stream])
            record[stream+'_file'] = filename
        return record

    def inspect_target(self):
        result = self.call(['inspect', self.cid])
        values = json.loads(result['stdout'])
        AU.require(type(values) is list and len(values) == 1, 'target inspect incomplete')
        value = values[0]
        host = value['HostConfig']
        AU.require(value['Id'] == self.cid and self.cid not in self.spec['production_container_ids']
                   and value['Config']['Image'] == PG_IMAGE and host['NetworkMode'] == 'none'
                   and value['Config']['Labels'].get('clearapi.offline-restore') == self.spec['authorization_id']
                   and value['Mounts'] == [] and host.get('Binds') in (None, [])
                   and host.get('PortBindings') in (None, {}) and host.get('Privileged') is False
                   and host.get('PidMode', '') == '' and host.get('Devices') in (None, [])
                   and set(host['Tmpfs']) == {'/var/lib/postgresql/data', '/restore'}
                   and all(v == 'rw,noexec,nosuid,nodev,mode=0700' for v in host['Tmpfs'].values()),
                   'target isolation drift')
        return value

    def sql(self, database, query, hash_only=False):
        self.inspect_target()
        result = self.call(['exec', '-i', self.cid, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                            '-U', 'postgres', '-d', database], stdin=(SESSION+query).encode(), hash_only=hash_only)
        return result['stdout_sha256'] if hash_only else result['stdout']

    def copy_dump(self, path, dumpname, expected_sha256):
        """Stream into the live tmpfs; Docker archive copy may bypass that mount."""
        AU.require(AU.safe_name(dumpname) == dumpname and '/' not in dumpname, 'unsafe dump name')
        self.inspect_target()
        target = '/restore/'+dumpname
        self.call(['exec', '-i', self.cid, 'sh', '-c', 'umask 077; cat > "$1"',
                   'clearapi-restore-copy', target], stdin_file=path, expected_sha256=expected_sha256)
        actual = self.call(['exec', self.cid, 'sha256sum', target])['stdout']
        AU.require(actual == (expected_sha256+'  '+target+'\n').encode(), 'live restore payload hash mismatch')

    def inventory(self, database):
        table_query = """SELECT coalesce(json_agg(format('%I.%I',n.nspname,c.relname)
            ORDER BY n.nspname,c.relname),'[]') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
            WHERE c.relkind IN ('r','p','m') AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema';"""
        tables = json.loads(self.sql(database, table_query))
        AU.require(type(tables) is list and tables and len(tables) <= 10000, 'table catalog missing')
        foreign = int(self.sql(database, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind='f' AND n.nspname NOT LIKE 'pg_%';").strip())
        AU.require(foreign == 0, 'foreign tables require separately reviewed recovery coverage')
        # Let PostgreSQL enforce NULLS/partial/expression uniqueness semantics.
        # Rebuild before fingerprinting, so expression functions cannot mutate
        # rows/sequences after the accepted inventory has been measured.
        unique_indexes = json.loads(self.sql(database, "SELECT coalesce(json_agg(format('%I.%I',n.nspname,c.relname) ORDER BY n.nspname,c.relname),'[]') FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE i.indisunique AND n.nspname NOT LIKE 'pg_%';"))
        for index in unique_indexes:
            AU.require(type(index) is str and 0 < len(index) <= 256, 'invalid unique index identifier')
            self.sql(database, 'REINDEX INDEX '+index+';')
        result = dict(catalog_tables=tables, tables={})
        for table in tables:
            # Names originate from PostgreSQL format('%I.%I'), never caller SQL.
            AU.require(type(table) is str and 0 < len(table) <= 256, 'invalid catalog identifier')
            rows = int(self.sql(database, 'SELECT count(*) FROM '+table+';').strip())
            fingerprint = self.sql(database, 'COPY (SELECT to_jsonb(t)::text FROM '+table+
                ' t ORDER BY to_jsonb(t)::text COLLATE "C") TO STDOUT;', hash_only=True)
            result['tables'][table] = dict(rows=rows, rows_sha256=fingerprint)
        # pg_dump emits version-dependent restrict nonce; pin it explicitly so
        # source and restored schema bytes use the same reviewed normalization.
        dump = self.call(['exec', self.cid, 'pg_dump', '-U', 'postgres', '-d', database, '--schema-only',
                          '--no-owner', '--no-acl', '--restrict-key='+self.spec['challenge']])
        result['schema_sha256'] = AU.digest(dump['stdout'])
        catalog_queries = {
            'indexes_sha256': "SELECT n.nspname,c.relname,pg_get_indexdef(i.indexrelid),i.indisvalid,i.indisready FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' ORDER BY n.nspname,c.relname;",
            'constraints_sha256': "SELECT n.nspname,c.relname,k.conname,k.contype,k.convalidated,pg_get_constraintdef(k.oid,true) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' ORDER BY n.nspname,c.relname,k.conname;",
        }
        for key, query in catalog_queries.items():
            result[key] = self.sql(database, query, hash_only=True)
        sequences = json.loads(self.sql(database, "SELECT coalesce(json_agg(json_build_object('name',format('%I.%I',schemaname,sequencename),'type',data_type,'start',start_value,'min',min_value,'max',max_value,'increment',increment_by,'cycle',cycle,'cache',cache_size) ORDER BY schemaname,sequencename),'[]') FROM pg_sequences WHERE schemaname NOT LIKE 'pg_%';"))
        for sequence in sequences:
            AU.require(type(sequence['name']) is str and 0 < len(sequence['name']) <= 256, 'invalid sequence identifier')
            sequence['state_sha256'] = self.sql(database, 'SELECT last_value,is_called FROM '+sequence['name']+';', hash_only=True)
        result['sequences_sha256'] = AU.digest(encoded(sequences))
        has_options = self.sql(database, "SELECT to_regclass('public.options') IS NOT NULL;").strip()
        AU.require(has_options in (b't', b'f'), 'options catalog probe failed')
        result['migration_state_sha256'] = (self.sql(database, "SELECT to_jsonb(t)::text FROM public.options t WHERE key LIKE 'bootstrap.%' OR lower(key) LIKE '%migration%' OR lower(key) LIKE '%schema%' ORDER BY to_jsonb(t)::text COLLATE \"C\";", hash_only=True)
            if has_options == b't' else AU.digest(b'options_table_absent'))
        counts = json.loads(self.sql(database, "SELECT json_build_object('validated',count(*) FILTER (WHERE convalidated),'unvalidated',count(*) FILTER (WHERE NOT convalidated)) FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace WHERE n.nspname NOT LIKE 'pg_%';"))
        result.update(validated_constraints=counts['validated'], unvalidated_constraints=counts['unvalidated'])
        invalid = int(self.sql(database, "SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND (NOT i.indisvalid OR NOT i.indisready);").strip())
        AU.require(invalid == 0, 'invalid or unready restored indexes')
        result['uniqueness_violations'] = 0
        return result

    def run(self):
        documents = {name: AU.document(self.inputs, name, pin) for name,pin in self.spec['input_sha256'].items()}
        self.manifest = documents['backup-manifest.json']
        completion = documents['backup-completion.json']
        predecrypt = documents['predecryption-run.json']
        encrypted = self.manifest['encryption']['encrypted_artifact']
        AU.require(AU.file_hash(self.inputs, encrypted['name']) == (encrypted['sha256'], encrypted['bytes'])
                   and completion['encrypted_sha256'] == encrypted['sha256']
                   and completion['manifest_sha256'] == self.spec['input_sha256']['backup-manifest.json']
                   and self.manifest['backup_id'] == self.spec['backup_id'] == completion['backup_id']
                   and self.manifest['stopped_writer_identity'] == self.spec['stopped_writer_identity']
                   and self.manifest['encryption']['key_fingerprint'] == self.spec['key_fingerprint'],
                   'backup identity mismatch')
        AU.database_inventory(documents['source-inventory.json'], self.spec['databases'])
        AU.require(predecrypt['schema'] == 'clearapi.offline-predecryption/v1'
                   and predecrypt['challenge'] == self.spec['challenge']
                   and predecrypt['candidate'] == self.spec['candidate']
                   and predecrypt['backup_completion_sha256'] == self.spec['input_sha256']['backup-completion.json'],
                   'predecryption provenance binding mismatch')
        self.commands['decrypt'] = predecrypt['command']
        for stream in ('stdout', 'stderr'):
            item = self.commands['decrypt']
            AU.require(AU.file_hash(self.inputs, item[stream+'_file'])[0] == item[stream+'_sha256'],
                       'predecryption output mismatch')
            write_file(self.output / item[stream+'_file'], AU.local_file(self.inputs, item[stream+'_file']).read_bytes())
        AU.bundle_inventory(self.inputs, self.manifest)
        expected = {'database_dump' if name == 'main' else 'log_database_dump' for name in self.spec['databases']}
        expected |= {'pg03_state_archive', 'pg03_state_snapshot'}
        artifacts = self.manifest['artifacts']
        AU.require(expected <= set(artifacts), 'restore payload missing')
        payload_dir = self.output / 'private-payload'
        payload_dir.mkdir(mode=0o700)
        wanted = {artifacts[key]['name'] for key in expected}
        with tarfile.open(AU.local_file(self.inputs, self.manifest['bundle']['name']), 'r:gz') as bundle:
            for member in AU.archive_members(bundle):
                if member.name in wanted:
                    with bundle.extractfile(member) as source:
                        fd = os.open(payload_dir / member.name, os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW, 0o600)
                        with os.fdopen(fd, 'wb') as target:
                            shutil.copyfileobj(source, target, 1024*1024)
        for key in expected:
            AU.require(AU.file_hash(payload_dir, artifacts[key]['name'])[0] == artifacts[key]['sha256'],
                       'copied payload drift')
        AU.require(self.call(['ps', '-aq', '--no-trunc'])['stdout'].strip() == b'', 'offline daemon must be empty')
        self.call(['image', 'inspect', PG_IMAGE])  # local only; no pull
        create = self.call(['create', '--pull=never', '--name', 'clearapi-offline-restore-'+self.spec['authorization_id'],
            '--label', 'clearapi.offline-restore='+self.spec['authorization_id'], '--network', 'none',
            '--tmpfs', '/var/lib/postgresql/data:rw,noexec,nosuid,nodev,mode=0700',
            '--tmpfs', '/restore:rw,noexec,nosuid,nodev,mode=0700', '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', PG_IMAGE])
        cid = create['stdout'].decode().strip()
        AU.require(re.fullmatch('[a-f0-9]{64}', cid) is not None
                   and cid not in self.spec['production_container_ids'], 'unsafe created target')
        self.cid = cid
        inspection = self.inspect_target()
        self.call(['start', self.cid])
        for attempt in range(30):
            # The image starts a temporary socket-only server during initdb.
            # It can answer pg_isready and then shut down before PID1 execs the
            # final PostgreSQL process. Do not admit that temporary server.
            process = self.call(['exec', self.cid, 'cat', '/proc/1/comm'], allow_failure=True)
            ready = self.call(['exec', self.cid, 'pg_isready', '-U', 'postgres'], allow_failure=True)
            if process['returncode'] == 0 and process['stdout'].strip() == b'postgres' and ready['returncode'] == 0:
                break
            AU.require(attempt < 29, 'isolated database not ready')
            time.sleep(0.2)
        version = self.sql('postgres', 'SHOW server_version_num;').strip()
        AU.require(version == b'170006', 'PostgreSQL version mismatch')
        restored = {}
        for name, database in self.spec['databases'].items():
            dumpname = artifacts[database['dump_artifact']]['name']
            self.inspect_target()
            self.copy_dump(payload_dir/dumpname, dumpname, artifacts[database['dump_artifact']]['sha256'])
            self.call(['exec', self.cid, 'createdb', '-U', 'postgres', database['target_database']])
            result = self.call(['exec', self.cid, 'pg_restore', '--exit-on-error', '--no-owner', '--no-acl',
                '-U', 'postgres', '--dbname='+database['target_database'], '/restore/'+dumpname], allow_failure=True)
            self.commands['restore_'+name] = self.command_record('restore_'+name, result)
            AU.require(result['returncode'] == 0, 'offline subprocess failed')
            restored[name] = self.inventory(database['target_database'])
        AU.database_inventory(restored, self.spec['databases'])
        AU.require(restored == documents['source-inventory.json'], 'restored/source inventory mismatch')
        destination = self.output / 'pg03-recovered'
        STATE.restore(payload_dir/artifacts['pg03_state_archive']['name'],
                      payload_dir/artifacts['pg03_state_snapshot']['name'], destination)
        pg03 = STATE.inventory(destination)
        inspection = self.inspect_target()
        self.commands['cleanup'] = self.command_record('cleanup', self.call(['rm', '-f', self.cid]))
        self.cid = None
        AU.require(self.call(['ps', '-aq', '--no-trunc'])['stdout'].strip() == b'', 'cleanup incomplete')
        for path in (destination, payload_dir):
            AU.require(path.parent == self.output and not path.is_symlink(), 'cleanup path mismatch')
            shutil.rmtree(path)
        for name in ('backup-manifest.json', 'backup-completion.json', 'source-inventory.json'):
            raw = AU.local_file(self.inputs,name).read_bytes()
            AU.require(AU.digest(raw) == self.spec['input_sha256'][name], 'source evidence changed during restore')
            write_file(self.output/name, raw)
        write_file(self.output/'docker-inspect.json', encoded(inspection))
        write_file(self.output/'restored-inventory.json', encoded(restored))
        write_file(self.output/'pg03-restored-inventory.json', encoded(pg03))
        run = dict(schema='clearapi.offline-restore-run/v1', started_at_utc=self.commands['decrypt']['started_at_utc'],
            completed_at_utc=stamp(), challenge=self.spec['challenge'], candidate=self.spec['candidate'],
            backup_completion_sha256=self.spec['input_sha256']['backup-completion.json'], postgres_version_num='170006',
            private_key_recorded=False, production_connections=[], cleanup_remaining_containers=[], commands=self.commands)
        write_file(self.output/'restore-run.json', encoded(run))
        plan = {key: value for key,value in self.spec.items() if key not in ('authorization_id', 'input_sha256')}
        plan.update(schema='clearapi.offline-restore-plan/v1', target_container_id=cid,
            evidence_sha256={name: AU.file_hash(self.output,name)[0] for name in ('backup-manifest.json',
                'backup-completion.json', 'source-inventory.json', 'docker-inspect.json', 'restored-inventory.json',
                'pg03-restored-inventory.json', 'restore-run.json')})
        write_file(self.output/'restore-plan.json', encoded(plan))
        result = AU.audit(self.output, AU.digest(encoded(plan)), now(), self.inputs)
        result.update(execute_spec_sha256=AU.digest(encoded(self.spec)), offline_restore_process='completed',
                      authorization_id=self.spec['authorization_id'])
        write_file(self.output/'audit-result.json', encoded(result))
        return result


def execute(args):
    AU.require(os.name == 'posix' and os.geteuid() == 0, 'offline execution requires Linux root')
    AU.require(not Path('/opt/clearapi/app/release-manifest.json').exists()
               and not Path('/data/clearapi').exists(), 'production host refused')
    AU.require(Path(args.policy) == POLICY_PATH, 'fixed offline policy required')
    policy = private_file(POLICY_PATH, args.policy_sha256)
    spec = validate_spec(private_file(Path(args.spec), args.spec_sha256), now())
    authorization = private_file(Path(args.authorization), args.authorization_sha256)
    AU.require(policy['schema'] == 'clearapi.offline-restore-policy/v1'
               and policy['environment'] == 'isolated_offline_restore'
               and policy['public_key_file'] == str(PUBLIC_KEY)
               and policy['machine_id_sha256'] == AU.digest(Path('/etc/machine-id').read_bytes())
               and authorization['schema'] == 'clearapi.offline-restore-authorization/v1'
               and authorization['approved_by'] == 'a3225865526-wq'
               and authorization['action'] == 'independent_offline_restore_only'
               and authorization['spec_sha256'] == args.spec_sha256
               and authorization['authorization_id'] == spec['authorization_id']
               and AU.utc(authorization['issued_at_utc']) <= now() < AU.utc(authorization['expires_at_utc'])
               <= AU.utc(spec['expires_at_utc']), 'offline authorization mismatch')
    for path, key in ((PUBLIC_KEY,'public_key_sha256'), (Path(DOCKER),'docker_binary_sha256'),
                      (Path(OPENSSL),'openssl_binary_sha256'), (Path(__file__),'runner_sha256')):
        info = path.lstat()
        AU.require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022
                   and AU.file_hash(path.parent,path.name)[0] == policy[key], 'offline executable or trust anchor drift')
    AU.require(set(policy['module_sha256']) == {'pg03-restore-acceptance.py', 'pg03-backup-state.py'},
               'reviewed restore dependencies required')
    for name, expected in policy['module_sha256'].items():
        path = Path(__file__).with_name(name)
        info = path.lstat()
        AU.require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022
                   and AU.file_hash(path.parent,name)[0] == expected, 'restore module drift')
    signature = Path(args.signature)
    info = signature.lstat()
    AU.require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o077
               and info.st_size <= 8192, 'unsafe detached signature')
    def lease():
        validate_spec(spec, now())
        AU.require(now() < AU.utc(authorization['expires_at_utc'])
                   and AU.file_hash(POLICY_PATH.parent,POLICY_PATH.name)[0] == args.policy_sha256
                   and AU.file_hash(Path(args.authorization).parent,Path(args.authorization).name)[0]
                       == args.authorization_sha256
                   and not (CLAIM_ROOT/(spec['authorization_id']+'.revoked')).exists(), 'authorization expired or revoked')
    transport = LocalCommands(lease, policy['offline_daemon_id'])
    verified = transport.run([OPENSSL, 'pkeyutl', '-verify', '-pubin', '-rawin', '-inkey', str(PUBLIC_KEY),
                              '-sigfile', str(signature), '-in', str(args.authorization)])
    AU.require(verified['returncode'] == 0, 'administrator signature rejected')
    inputs = INPUT_ROOT/spec['authorization_id']
    output = OUTPUT_ROOT/spec['authorization_id']
    for directory in (inputs, OUTPUT_ROOT, CLAIM_ROOT):
        info = directory.lstat()
        AU.require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o077,
                   'private offline directories required')
    # Both plaintext input and temporary recovery live on tmpfs; evidence results
    # need an approved seal/export before host shutdown. Claims remain durable.
    mounts = [line.split() for line in Path('/proc/self/mountinfo').read_text().splitlines()]
    for directory in (inputs, OUTPUT_ROOT):
        matches = [fields for fields in mounts if str(directory) == fields[4] or str(directory).startswith(fields[4].rstrip('/')+'/')]
        selected = max(matches, key=lambda fields: len(fields[4]))
        AU.require(selected[selected.index('-')+1] == 'tmpfs', 'plaintext requires approved tmpfs')
    matches = [fields for fields in mounts if str(CLAIM_ROOT) == fields[4]
               or str(CLAIM_ROOT).startswith(fields[4].rstrip('/')+'/')]
    selected = max(matches, key=lambda fields: len(fields[4]))
    AU.require(selected[selected.index('-')+1] in ('ext4','xfs','btrfs','zfs'),
               'one-use claims require an approved durable filesystem')
    daemon = transport.run([DOCKER, 'info', '--format', '{{.ID}}'])
    AU.require(daemon['returncode'] == 0 and daemon['stdout'].decode().strip() == policy['offline_daemon_id'],
               'offline daemon identity mismatch')
    claim = CLAIM_ROOT/(spec['authorization_id']+'.json')
    write_file(claim, encoded(dict(authorization_sha256=args.authorization_sha256,
        spec_sha256=args.spec_sha256, claimed_at_utc=stamp(), state='claimed_no_reuse')))
    output.mkdir(mode=0o700)  # never reuse an old attempt, even after failure
    runner = OfflineRestore(spec, transport, inputs, output, lease)
    try:
        return runner.run()
    except BaseException:
        # Cleanup is limited to this proven owned CID while lease is valid. An
        # expired/unknown identity never triggers additional unapproved mutation.
        cleanup = 'NOT_RUN'
        if runner.cid is not None:
            try:
                runner.inspect_target()
                runner.call(['rm', '-f', runner.cid])
                cleanup = 'PASS'
            except Exception:
                cleanup = 'BLOCKED'
        write_file(output/'failure.json', encoded(dict(status='FAIL', cleanup=cleanup,
            authorization_id=spec['authorization_id'], owned_container_id=runner.cid,
            owned_label='clearapi.offline-restore='+spec['authorization_id'],
            private_plaintext_retained=any((output/name).exists() for name in ('private-payload','pg03-recovered')),
            production_execution_allowed=False)))
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('plan', 'execute'), nargs='?', default='plan')
    parser.add_argument('--spec', type=Path, required=True)
    parser.add_argument('--spec-sha256', required=True)
    parser.add_argument('--policy', default=str(POLICY_PATH))
    parser.add_argument('--policy-sha256')
    parser.add_argument('--authorization', type=Path)
    parser.add_argument('--authorization-sha256')
    parser.add_argument('--signature', type=Path)
    args = parser.parse_args()
    try:
        spec = validate_spec(AU.document(args.spec.parent, args.spec.name, args.spec_sha256), now())
        if args.mode == 'plan':
            print(json.dumps(command_plan(spec), sort_keys=True))
            return 0
        AU.require(args.policy_sha256 and args.authorization and args.authorization_sha256 and args.signature,
                   'signed independent authorization required')
        result = execute(args)
        print(json.dumps(result, sort_keys=True))
        return 0
    except Exception:
        print(json.dumps(dict(schema='clearapi.offline-restore-result/v1', status='BLOCKED',
            production_execution_allowed=False, reason='offline evidence, identity, command or authorization rejected')))
        return 1


if __name__ == '__main__':
    sys.exit(main())
