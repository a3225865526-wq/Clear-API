#!/usr/bin/env python3
"""RC-only schema rehearsal: no production network, volumes, rows or credentials."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import time
import uuid

SCHEMA = 'pg03-schema-rehearsal-v1'
POSTGRES = 'postgres@sha256:00bc86618629af00d2937fdc5a5d63db3ff8450acf52f0636ec813c7f4902929'


def schema_digest(document):
    # psql nonces and pg_dump package labels do not describe database objects.
    lines = (line for line in document.strip().splitlines() if not line.startswith(
        ('\\restrict ', '\\unrestrict ', '-- Dumped from ', '-- Dumped by ')))
    return hashlib.sha256('\n'.join(lines).strip().encode()).hexdigest()


def validate_receipt(receipt, candidate, source_schema):
    if (receipt.get('schema_version') != SCHEMA or receipt.get('postgres_image_ref') != POSTGRES
            or receipt.get('postgres_version_num') != '170006' or receipt.get('startup_passes') != 2
            or receipt.get('profile') != 'isolated_schema_only_master_single_postgres'
            or receipt.get('source_schema_sha256') != source_schema
            or receipt.get('candidate_image_id') != candidate['image_id']
            or receipt.get('candidate_binary_sha256') != candidate['binary_sha256']
            or receipt.get('source_commit') != candidate['commit']
            or not re.fullmatch(r'[a-f0-9]{64}', receipt.get('target_schema_sha256', ''))):
        raise ValueError('RC schema rehearsal does not bind this candidate and source schema')


def rehearse(candidate, commit, schema, output, command=None):
    if command is None:
        def command(args, raw=None):
            return subprocess.run(['docker', *args], input=raw, check=True,
                capture_output=True, timeout=90).stdout.decode().strip()
    if not re.fullmatch(r'[a-f0-9]{40}', commit):
        raise ValueError('full source commit required')
    image = json.loads(command(['image', 'inspect', candidate]))[0]
    if (not re.fullmatch(r'sha256:[a-f0-9]{64}', image['Id']) or image['Os'] != 'linux'
            or image['Architecture'] != 'amd64'
            or image['Config']['Labels'].get('org.opencontainers.image.revision') != commit):
        raise ValueError('candidate platform or source mismatch')
    binary = command(['run', '--rm', '--network', 'none', '--read-only', '--entrypoint',
        'sha256sum', image['Id'], '/new-api']).split()[0]
    if not re.fullmatch(r'[a-f0-9]{64}', binary):
        raise ValueError('candidate binary identity missing')
    document = schema.read_bytes()
    if len(document) > 2097152 or not document.startswith(b'--'):
        raise ValueError('bounded schema-only fixture required')
    if any(re.match(rb'^(INSERT |COPY |SELECT pg_catalog.setval)', line) for line in document.splitlines()):
        raise ValueError('production rows must never enter rehearsal')
    nonce = uuid.uuid4().hex
    owned = []
    try:
        pg = command(['create', '--label', 'clearapi.schema-rehearsal='+nonce,
            '--network', 'none', '--read-only', '--memory', '512m', '--pids-limit', '128',
            '--tmpfs', '/var/lib/postgresql/data:rw,nosuid,nodev,size=512m',
            '--tmpfs', '/var/run/postgresql:rw,nosuid,nodev,size=16m', '--tmpfs', '/tmp:rw,nosuid,nodev,size=16m',
            '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', '-e', 'POSTGRES_DB=rehearsal', POSTGRES])
        if not re.fullmatch(r'[a-f0-9]{64}', pg):
            raise ValueError('invalid isolated database identity')
        owned.append(pg)
        command(['start', pg])
        for _ in range(60):
            try:
                if command(['exec', pg, 'pg_isready', '-U', 'postgres', '-d', 'rehearsal']):
                    break
            except subprocess.CalledProcessError:
                pass
            time.sleep(1)
        else:
            raise ValueError('isolated database not ready')
        command(['exec', '-i', pg, 'psql', '-U', 'postgres', '-d', 'rehearsal', '-Xq',
            '-v', 'ON_ERROR_STOP=1'], document)
        version = command(['exec', pg, 'psql', '-U', 'postgres', '-d', 'rehearsal', '-XAtqc',
            'SHOW server_version_num'])
        if version != '170006':
            raise ValueError('rehearsal engine version mismatch')
        before = command(['exec', pg, 'pg_dump', '-U', 'postgres', '-d', 'rehearsal',
            '--schema-only', '--no-owner', '--no-privileges'])
        if schema_digest(before) != schema_digest(document.decode()):
            raise ValueError('schema-only restoration changed structure')
        target = None
        for _ in range(2):
            gateway = command(['create', '--label', 'clearapi.schema-rehearsal='+nonce,
                '--network', 'container:'+pg, '--read-only', '--cap-drop', 'ALL',
                '--security-opt', 'no-new-privileges', '--memory', '1g', '--pids-limit', '256',
                '--tmpfs', '/data:rw,nosuid,nodev,size=128m', '--tmpfs', '/tmp:rw,nosuid,nodev,size=64m',
                '-e', 'SQL_DSN=postgres://postgres@127.0.0.1:5432/rehearsal?sslmode=disable',
                '-e', 'LOG_SQL_DSN=', '-e', 'SESSION_SECRET=synthetic-schema-rehearsal-'+nonce,
                '-e', 'NODE_TYPE=master', '-e', 'CLEARAPI_PRODUCTION=false',
                '-e', 'CLEARAPI_CLOSED_LAUNCH=true', '-e', 'CLEARAPI_POOL_ENABLED=false',
                '-e', 'BATCH_UPDATE_ENABLED=false', '-e', 'PORT=3000',
                '--entrypoint', '/new-api', image['Id']])
            if not re.fullmatch(r'[a-f0-9]{64}', gateway):
                raise ValueError('invalid isolated Gateway identity')
            owned.append(gateway)
            command(['start', gateway])
            for _ in range(60):
                try:
                    body = json.loads(command(['exec', gateway, 'wget', '-q', '-O', '-',
                        'http://127.0.0.1:3000/api/status']))
                    if body.get('success') is True:
                        break
                except (subprocess.CalledProcessError, ValueError):
                    pass
                if command(['inspect', '--format', '{{.State.Running}}', gateway]) != 'true':
                    raise ValueError('isolated candidate startup failed')
                time.sleep(1)
            else:
                raise ValueError('isolated candidate health deadline exceeded')
            command(['stop', '--time', '30', gateway])
            after = command(['exec', pg, 'pg_dump', '-U', 'postgres', '-d', 'rehearsal',
                '--schema-only', '--no-owner', '--no-privileges'])
            current = schema_digest(after)
            if target is not None and target != current:
                raise ValueError('second full startup changed schema again')
            target = current
        receipt = {'schema_version': SCHEMA, 'profile': 'isolated_schema_only_master_single_postgres',
            'source_commit': commit, 'candidate_image_id': image['Id'], 'candidate_binary_sha256': binary,
            'postgres_image_ref': POSTGRES, 'postgres_version_num': version, 'startup_passes': 2,
            'source_schema_sha256': schema_digest(before), 'target_schema_sha256': target}
    finally:
        # Discard only containers created by this invocation, with matching label.
        for cid in reversed(owned):
            label = command(['inspect', '--format', '{{index .Config.Labels "clearapi.schema-rehearsal"}}', cid])
            if label != nonce:
                raise ValueError('isolated cleanup identity changed; manual containment required')
            command(['rm', '-f', cid])
    # Never replace a prior rehearsal receipt or publish before cleanup succeeds.
    with output.with_suffix('.target.sql').open('x', encoding='utf-8') as f:
        f.write(after+'\n')
    with output.open('x', encoding='utf-8') as f:
        json.dump(receipt, f, indent=2)
        f.write('\n')
    return receipt


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--candidate', required=True)
    p.add_argument('--commit', required=True)
    p.add_argument('--schema', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    rehearse(a.candidate, a.commit, a.schema, a.output)
    print('Isolated exact-candidate schema rehearsal passed twice; not a production Runtime PASS')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # Container logs may contain generated synthetic setup credentials; do
        # not emit them or raw command errors in CI/production evidence.
        raise SystemExit('SCHEMA_REHEARSAL_BLOCKED: no receipt issued; no production operation performed')