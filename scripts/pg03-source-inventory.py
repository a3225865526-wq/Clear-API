#!/usr/bin/env python3
"""Collect hash-only source evidence over pinned SSH; never authorize recovery."""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import queue
import re
import shlex
import subprocess
import threading
import time
import uuid

SPEC = importlib.util.spec_from_file_location('offline_restore', Path(__file__).with_name('pg03-offline-restore.py'))
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)
AU = RUNNER.AU


class SourceInventory(RUNNER.OfflineRestore):
    def __init__(self, ssh, cid, database, challenge):
        self.ssh, self.cid, self.database = ssh, cid, database
        self.spec = dict(challenge=challenge)
        self.session = None
        self.deadline = time.monotonic()+600
        self.events = queue.Queue(maxsize=32)
        self.snapshot = None

    def remote(self, argv):
        return [*self.ssh, shlex.join(['sudo', '-n', '/usr/bin/docker', *argv])]

    def inspect_source(self):
        result = subprocess.run(self.remote(['inspect', self.cid]), capture_output=True, timeout=20, check=False)
        AU.require(result.returncode == 0, 'source container inspection failed')
        items = json.loads(result.stdout)
        AU.require(len(items) == 1 and items[0]['Id'] == self.cid
                   and items[0]['Config']['Image'] == RUNNER.PG_IMAGE
                   and items[0]['State']['Running'] is True
                   and items[0]['State']['Health']['Status'] == 'healthy', 'source container identity drift')
        return dict(container_id=self.cid, image_id=items[0]['Image'], image_ref=items[0]['Config']['Image'])

    def open(self):
        self.session = subprocess.Popen(self.remote(['exec', '-i', self.cid, 'psql', '-X', '-qAt',
            '-v', 'ON_ERROR_STOP=1', '-P', 'pager=off', '-U', 'clearapi_admin', '-d', self.database]),
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        for name in ('stdout', 'stderr'):
            threading.Thread(target=self.read_stream, args=(name, getattr(self.session, name)), daemon=True).start()
        self.sql(self.database, 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; '+RUNNER.SESSION+
            "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='120s'; SET LOCAL idle_in_transaction_session_timeout='120s'; SET LOCAL row_security=off;")
        AU.require(self.sql(self.database, 'SHOW server_version_num;').strip() == b'170006', 'source version drift')
        AU.require(self.sql(self.database, 'SELECT current_database();').strip().decode() == self.database,
                   'source database drift')
        AU.require(self.sql(self.database,
            'SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user;').strip() == b't',
            'source role cannot prove full row visibility')
        self.snapshot = self.sql(self.database, 'SELECT pg_export_snapshot();').strip().decode()
        AU.require(re.fullmatch('[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', self.snapshot), 'source snapshot missing')

    def read_stream(self, name, stream):
        try:
            while raw := stream.read1(65536):
                self.events.put((name, raw))
        finally:
            self.events.put((name, None))

    def sql(self, database, query, hash_only=False):
        AU.require(database == self.database and self.session is not None
                   and self.session.poll() is None, 'source session lost')
        marker = ('clearapi_'+uuid.uuid4().hex).encode()
        self.session.stdin.write(query.encode()+b'\n\\echo '+marker+b'\n')
        self.session.stdin.flush()
        digest, pending, retained = hashlib.sha256(), b'', bytearray()
        end = b'\n'+marker+b'\n'
        while True:
            remaining = min(125, self.deadline-time.monotonic())
            AU.require(remaining > 0, 'source collection deadline expired')
            try: name, raw = self.events.get(timeout=remaining)
            except queue.Empty: raise ValueError('source query timed out') from None
            AU.require(raw is not None and name == 'stdout', 'source query failed; diagnostics withheld')
            pending += raw
            if pending == marker+b'\n' or pending.endswith(end):
                payload = b'' if pending == marker+b'\n' else pending[:-len(marker)-1]
                digest.update(payload)
                if not hash_only: retained.extend(payload)
                AU.require(len(retained) <= 16*1024*1024, 'source metadata limit exceeded')
                return digest.hexdigest() if hash_only else bytes(retained)
            # Retain only enough suffix to recognize the framing marker. COPY
            # rows stream into the hash; neither rows nor errors reach reports.
            keep = len(end)
            if len(pending) > keep:
                payload, pending = pending[:-keep], pending[-keep:]
                digest.update(payload)
                if not hash_only: retained.extend(payload)
                AU.require(len(retained) <= 16*1024*1024, 'source metadata limit exceeded')

    def call(self, args, **kwargs):
        expected = ['exec', self.cid, 'pg_dump', '-U', 'postgres', '-d', self.database, '--schema-only',
                    '--no-owner', '--no-acl', '--restrict-key='+self.spec['challenge']]
        AU.require(args == expected and self.snapshot is not None and not kwargs
                   and self.session.poll() is None, 'only snapshot schema dump is allowed')
        # No fallback without the exported snapshot. Keep exporter alive until
        # pg_dump and every source query have completed.
        argv = ['exec', self.cid, 'pg_dump', '-U', 'clearapi_admin', '-d', self.database,
                *args[7:], '--snapshot='+self.snapshot]
        result = subprocess.run(self.remote(argv), capture_output=True, timeout=125, check=False)
        AU.require(result.returncode == 0 and self.session.poll() is None, 'source snapshot dump failed')
        return dict(stdout=result.stdout)

    def sequence_state(self):
        names = json.loads(self.sql(self.database, "SELECT coalesce(json_agg(format('%I.%I',schemaname,sequencename) ORDER BY schemaname,sequencename),'[]') FROM pg_sequences WHERE schemaname NOT LIKE 'pg_%';"))
        return {name:self.sql(self.database, 'SELECT last_value,is_called FROM '+name+';', hash_only=True)
                for name in names}

    def collect(self):
        identity = self.inspect_source()
        started = datetime.now(timezone.utc).isoformat()
        try:
            self.open()
            sequences_before = self.sequence_state()
            inventory = self.inventory(self.database, read_only_source=True)
            AU.require(self.sequence_state() == sequences_before, 'source sequences changed during collection')
            self.sql(self.database, 'COMMIT;')
            AU.require(self.inspect_source() == identity, 'source identity changed during collection')
            AU.database_inventory(dict(main=inventory), {'main':{}}, read_only_source=True)
            return dict(schema='clearapi.source-inventory-observation/v1', source=identity,
                database=self.database, started_at_utc=started,
                completed_at_utc=datetime.now(timezone.utc).isoformat(), inventory=dict(main=inventory),
                exported_snapshot=True, transaction_read_only=True, sequence_endpoints_equal=True,
                continuous_writer_exclusion='UNVERIFIED', backup_snapshot_binding='UNVERIFIED',
                production_execution_allowed=False)
        finally:
            self.close()

    def close(self):
        if self.session is not None:
            self.session.stdin.close()  # disconnect rolls back an unfinished read-only transaction
            try: self.session.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.session.kill()
                self.session.wait(timeout=10)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ssh-key', required=True)
    parser.add_argument('--known-hosts', required=True)
    parser.add_argument('--container-id', required=True)
    parser.add_argument('--challenge', required=True)
    args = parser.parse_args()
    try:
        AU.require(re.fullmatch('[a-f0-9]{64}', args.container_id) and AU.valid_hash(args.challenge),
                   'invalid fixed identity')
        ssh = ['ssh', '-F', 'none', '-i', args.ssh_key, '-o', 'IdentitiesOnly=yes', '-o', 'IdentityAgent=none',
               '-o', 'StrictHostKeyChecking=yes', '-o', 'UserKnownHostsFile='+args.known_hosts,
               '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', '-T', 'clearadmin@103.42.181.219']
        print(json.dumps(SourceInventory(ssh, args.container_id, 'clearapi', args.challenge).collect(), sort_keys=True))
        return 0
    except Exception:
        print(json.dumps(dict(status='BLOCKED', reason='source read-only collection rejected',
                              production_execution_allowed=False)))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
