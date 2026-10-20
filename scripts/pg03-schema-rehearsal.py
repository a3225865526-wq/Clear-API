#!/usr/bin/env python3
"""RC-only schema rehearsal: no production network, volumes, rows or credentials."""
import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
import subprocess
import time
import uuid

SCHEMA = 'pg03-schema-rehearsal-v1'
POSTGRES = 'postgres@sha256:00bc86618629af00d2937fdc5a5d63db3ff8450acf52f0636ec813c7f4902929'
LEGACY_MIGRATION_SOURCE = '69a66431646b5cc8912bf97a739e08de07316443'
MIGRATION_PROOF_PATH = '/tmp/clearapi-migration-proof.json'


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


def migration_evidence(document, container_id, source, before_schema, after_schema):
    """Interpret the reviewed legacy log grammar, without inventing a success marker.

    SysLog/SysError share a frame and discard writer errors in this exact source.
    Even a complete Docker capture is therefore only negative evidence: it can
    prove a recorded failure, but cannot prove that all migrations succeeded.
    """
    evidence = dict(schema='clearapi.migration-observation/v1', container_id=container_id,
        source_commit=source, status='unknown', reason='missing_migration_completion_contract',
        capture_complete=True, error_kinds=[], parsed_records=0,
        log_sha256=hashlib.sha256(document.encode()).hexdigest())
    if source != LEGACY_MIGRATION_SOURCE:
        evidence.update(capture_complete=False, reason='unreviewed_migration_log_contract')
    elif len(document.encode()) > 2097152 or not document:
        evidence.update(capture_complete=False, reason='missing_or_oversized_log')
    else:
        failures = {
            'options_primary_key': r'failed to migrate options primary key: .+',
            'vip_seed': r'failed to seed default vip levels: .+',
            'order_backfill': r'failed to backfill offline order redemption sources: .+',
            'retired_options': r'failed to migrate retired frontend options: .+',
            'price_metadata': r'Warning: failed to query metadata for subscription_plans\.price_amount: .+',
            'price_column': r'Warning: failed to migrate subscription_plans\.price_amount to decimal: .+',
        }
        for line in document.splitlines():
            # Docker timestamps frame the captured container stream; an embedded
            # business message or quoted error is not a migration event.
            frame = re.fullmatch(r'(\S+) \[SYS\] (\d{4}/\d{2}/\d{2} - \d{2}:\d{2}:\d{2}) \| (.*?)\s*', line)
            if frame is None:
                evidence.update(capture_complete=False, reason='unparseable_legacy_log')
                continue
            try:
                stamp = datetime.fromisoformat(frame[1].replace('Z', '+00:00'))
                if stamp.tzinfo is None:
                    raise ValueError('timestamp requires timezone')
                datetime.strptime(frame[2], '%Y/%m/%d - %H:%M:%S')
            except ValueError:
                evidence.update(capture_complete=False, reason='unparseable_legacy_log')
                continue
            evidence['parsed_records'] += 1
            for kind, pattern in failures.items():
                if re.fullmatch(pattern, frame[3]) and kind not in evidence['error_kinds']:
                    evidence['error_kinds'].append(kind)
        if evidence['error_kinds']:
            evidence.update(status='failure', reason='recorded_migration_error')
    if before_schema != after_schema:
        evidence.update(status='failure', reason='schema_conflicts_with_migration_evidence')
    return evidence


def structured_migration_evidence(proof, logs, container_id, source, challenge, binary,
                                  before_schema, after_schema):
    """Bind candidate-produced completion to this fresh container and binary.

    Logs supply only veto evidence. Neither quiet logs, health, process exit nor
    matching DDL can replace the dedicated completion record.
    """
    evidence = migration_evidence(logs, container_id, LEGACY_MIGRATION_SOURCE,
                                  before_schema, after_schema)
    evidence.update(source_commit=source, status='unknown', reason='missing_structured_proof',
                    proof_sha256=None)
    if evidence['error_kinds'] or before_schema != after_schema:
        evidence.update(status='failure', reason='conflicting_migration_evidence')
        return evidence
    if not logs or len(logs.encode()) > 2097152:
        evidence['reason'] = 'missing_or_oversized_log'
        return evidence
    if source == LEGACY_MIGRATION_SOURCE or not proof or len(proof.encode()) > 16384:
        return evidence

    try:
        validate_completion_proof(proof, container_id, challenge, binary)
    except (ValueError, TypeError):
        evidence['reason'] = 'invalid_structured_proof'
        return evidence
    evidence.update(status='success', reason='candidate_completed_startup_migrations',
                    proof_sha256=hashlib.sha256(proof.encode()).hexdigest(),
                    completion_receipt_json=proof, challenge=challenge)
    return evidence


def validate_completion_proof(proof, container_id, challenge, binary):
    """Validate a bounded candidate receipt without inventing log evidence."""
    if (type(proof) is not str or not 0 < len(proof.encode()) <= 16384
            or not re.fullmatch(r'[a-f0-9]{64}', container_id)
            or not re.fullmatch(r'[a-f0-9]{64}', binary)):
        raise ValueError('invalid completion receipt binding')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate proof key')
            result[key] = value
        return result

    receipt = json.loads(proof, object_pairs_hook=unique)
    if (not isinstance(receipt, dict) or set(receipt) != {
            'schema', 'status', 'challenge', 'hostname', 'pid', 'binary_sha256',
            'phases', 'main_database', 'log_database'}
            or receipt['schema'] != 'clearapi.startup-migration-proof/v1'
            or receipt['status'] != 'success'
            or receipt['challenge'] != challenge
            or not re.fullmatch(r'[a-f0-9]{64}', challenge)
            or receipt['hostname'] != container_id[:12]
            or type(receipt['pid']) is not int or receipt['pid'] != 1
            or receipt['binary_sha256'] != binary
            or receipt['phases'] != ['main_database', 'retired_options', 'log_database']
            or receipt['main_database'] != 'postgres'
            or receipt['log_database'] != 'postgres'):
        raise ValueError('proof contract mismatch')
    return receipt


def rehearse(candidate, commit, schema, output, command=None, observation=None,
             nonce=None, expected_identity=None, expected_schema=None,
             require_migration_evidence=False):
    if command is None:
        def command(args, raw=None):
            result = subprocess.run(['docker', *args], input=raw, check=True,
                capture_output=True, timeout=90)
            raw_output = result.stdout + result.stderr if args[0] == 'logs' else result.stdout
            return raw_output.decode().strip()
    if not re.fullmatch(r'[a-f0-9]{40}', commit):
        raise ValueError('full source commit required')
    image = json.loads(command(['image', 'inspect', candidate]))[0]
    if (not re.fullmatch(r'sha256:[a-f0-9]{64}', image['Id']) or image['Os'] != 'linux'
            or image['Architecture'] != 'amd64'
            or image['Config']['Labels'].get('org.opencontainers.image.revision') != commit):
        raise ValueError('candidate platform or source mismatch')
    if expected_identity is not None and (
            candidate not in image.get('RepoDigests', [])
            or image['Id'] != expected_identity['image_id']
            or image['Config']['Labels'].get('org.opencontainers.image.source')
                != 'https://github.com/a3225865526-wq/Clear-API'):
        raise ValueError('exact registry candidate identity mismatch')
    document = schema.read_bytes()
    if len(document) > 2097152 or not document.startswith(b'--'):
        raise ValueError('bounded schema-only fixture required')
    if any(re.match(rb'^(INSERT |COPY |SELECT pg_catalog.setval)', line) for line in document.splitlines()):
        raise ValueError('production rows must never enter rehearsal')
    nonce = nonce or uuid.uuid4().hex
    if not re.fullmatch(r'[a-f0-9]{32}', nonce):
        raise ValueError('invalid isolation nonce')
    observation = observation if observation is not None else {}
    observation.update(startups=[], cleanup=[], cleanup_complete=False)
    owned = []
    try:
        observation['phase'] = 'binary_identity'
        identity = command(['create', '--pull', 'never', '--label', 'clearapi.schema-rehearsal='+nonce,
            '--network', 'none', '--read-only', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges', '--memory', '128m', '--pids-limit', '32',
            '--entrypoint', 'sha256sum', image['Id'], '/new-api'])
        if not re.fullmatch(r'[a-f0-9]{64}', identity):
            raise ValueError('invalid binary probe identity')
        owned.append(identity)
        binary = command(['start', '-a', identity]).split()[0]
        if (not re.fullmatch(r'[a-f0-9]{64}', binary)
                or expected_identity is not None and binary != expected_identity['binary_sha256']):
            raise ValueError('candidate binary identity mismatch')
        observation['candidate'] = dict(image_id=image['Id'], binary_sha256=binary,
                                         image_ref=candidate, source_commit=commit)
        observation['phase'] = 'restore_test_database'
        pg = command(['create', '--label', 'clearapi.schema-rehearsal='+nonce,
            '--pull', 'never',
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
        observation['postgres'] = dict(container_id=pg, image_ref=POSTGRES, version_num=version)
        before = command(['exec', pg, 'pg_dump', '-U', 'postgres', '-d', 'rehearsal',
            '--schema-only', '--no-owner', '--no-privileges'])
        if schema_digest(before) != schema_digest(document.decode()):
            raise ValueError('schema-only restoration changed structure')
        target = None
        for index in range(2):
            observation['phase'] = 'before_schema_pass_'+str(index+1)
            # Capture the actual database before EACH start, including pass two.
            before_pass = command(['exec', pg, 'pg_dump', '-U', 'postgres', '-d', 'rehearsal',
                '--schema-only', '--no-owner', '--no-privileges'])
            startup = dict(pass_number=index+1, before_schema_sha256=schema_digest(before_pass),
                           create_exit_code=None, start_exit_code=None, healthy=False,
                           stop_exit_code=None, after_schema_sha256=None, process=None)
            observation['startups'].append(startup)
            if expected_schema is not None and startup['before_schema_sha256'] != expected_schema:
                raise ValueError('startup baseline changed')
            observation['phase'] = 'create_pass_'+str(index+1)
            log_options = ['--log-driver', 'json-file', '--log-opt', 'max-size=-1',
                           '--log-opt', 'mode=blocking'] if require_migration_evidence else []
            challenge = uuid.uuid4().hex + uuid.uuid4().hex
            proof_options = ['-e', 'CLEARAPI_MIGRATION_PROOF_CHALLENGE='+challenge] if require_migration_evidence else []
            gateway = command(['create', '--label', 'clearapi.schema-rehearsal='+nonce,
                '--pull', 'never',
                '--network', 'container:'+pg, '--read-only', '--cap-drop', 'ALL',
                '--security-opt', 'no-new-privileges', '--memory', '1g', '--pids-limit', '256',
                '--tmpfs', '/data:rw,nosuid,nodev,size=128m', '--tmpfs', '/tmp:rw,nosuid,nodev,size=64m',
                *log_options,
                *proof_options,
                '-e', 'SQL_DSN=postgres://postgres@127.0.0.1:5432/rehearsal?sslmode=disable',
                '-e', 'LOG_SQL_DSN=', '-e', 'SESSION_SECRET=synthetic-schema-rehearsal-'+nonce,
                '-e', 'NODE_TYPE=master', '-e', 'CLEARAPI_PRODUCTION=false',
                '-e', 'CLEARAPI_CLOSED_LAUNCH=true', '-e', 'CLEARAPI_POOL_ENABLED=false',
                '-e', 'BATCH_UPDATE_ENABLED=false', '-e', 'PORT=3000',
                '--entrypoint', '/new-api', image['Id']])
            if not re.fullmatch(r'[a-f0-9]{64}', gateway):
                raise ValueError('invalid isolated Gateway identity')
            owned.append(gateway)
            startup.update(container_id=gateway, create_exit_code=0)
            observation['phase'] = 'start_pass_'+str(index+1)
            command(['start', gateway])
            startup['start_exit_code'] = 0
            observation['phase'] = 'health_pass_'+str(index+1)
            for _ in range(60):
                try:
                    body = json.loads(command(['exec', gateway, 'wget', '-T', '2', '-q', '-O', '-',
                        'http://127.0.0.1:3000/api/status']))
                    if body.get('success') is True:
                        startup['healthy'] = True
                        break
                except (subprocess.CalledProcessError, ValueError):
                    pass
                if command(['inspect', '--format', '{{.State.Running}}', gateway]) != 'true':
                    raise ValueError('isolated candidate startup failed')
                time.sleep(1)
            else:
                raise ValueError('isolated candidate health deadline exceeded')
            # Read while running: the per-container /tmp is tmpfs, so Docker
            # cannot recover its contents after stop. Bound the read at source.
            proof = None
            if require_migration_evidence and commit != LEGACY_MIGRATION_SOURCE:
                try:
                    proof = command(['exec', gateway, 'head', '-c', '16385', MIGRATION_PROOF_PATH])
                except Exception:
                    pass  # Missing proof remains UNKNOWN; still stop/collect/clean.
            observation['phase'] = 'stop_pass_'+str(index+1)
            command(['stop', '--time', '30', gateway])
            startup['stop_exit_code'] = 0
            state = json.loads(command(['inspect', '--format', '{{json .State}}', gateway]))
            startup['process'] = {key: state.get(key) for key in
                                  ('Status', 'Running', 'ExitCode', 'OOMKilled')}
            # Error strings may contain configuration: record presence only.
            startup['process']['error_present'] = bool(state.get('Error'))
            if (state.get('Status') != 'exited' or state.get('Running') is not False
                    or type(state.get('ExitCode')) is not int or state['ExitCode'] != 0
                    or state.get('OOMKilled') is not False or state.get('Error')):
                raise ValueError('candidate did not exit cleanly after SIGTERM')
            observation['phase'] = 'after_schema_pass_'+str(index+1)
            after = command(['exec', pg, 'pg_dump', '-U', 'postgres', '-d', 'rehearsal',
                '--schema-only', '--no-owner', '--no-privileges'])
            current = schema_digest(after)
            startup['after_schema_sha256'] = current
            if require_migration_evidence:
                observation['phase'] = 'migration_evidence_pass_'+str(index+1)
                startup['migration'] = dict(status='unknown', reason='log_capture_unavailable',
                                            container_id=gateway, capture_complete=False)
                try:
                    logs = command(['logs', '--timestamps', gateway])
                    startup['migration'] = migration_evidence(logs, gateway, commit,
                        startup['before_schema_sha256'], current)
                    if commit != LEGACY_MIGRATION_SOURCE:
                        startup['migration'] = structured_migration_evidence(proof, logs, gateway,
                            commit, challenge, binary, startup['before_schema_sha256'], current)
                except Exception:
                    # Preserve UNKNOWN and the normal outer cleanup path.
                    raise ValueError('migration log evidence unavailable') from None
                if startup['migration']['status'] != 'success':
                    raise ValueError('migration outcome BLOCKED: conclusive evidence required')
            if expected_schema is not None and current != expected_schema:
                raise ValueError('startup changed fixed target schema')
            if target is not None and target != current:
                raise ValueError('second full startup changed schema again')
            target = current
        receipt = {'schema_version': SCHEMA, 'profile': 'isolated_schema_only_master_single_postgres',
            'source_commit': commit, 'candidate_image_id': image['Id'], 'candidate_binary_sha256': binary,
            'postgres_image_ref': POSTGRES, 'postgres_version_num': version, 'startup_passes': 2,
            'source_schema_sha256': schema_digest(before), 'target_schema_sha256': target}
        observation['phase'] = 'startups_complete'
    except Exception as error:
        observation['failure'] = dict(phase=observation['phase'],
            command_exit_code=error.returncode if isinstance(error, subprocess.CalledProcessError) else None,
            timed_out=isinstance(error, subprocess.TimeoutExpired))
        raise
    finally:
        # Discard only containers created by this invocation, with matching label.
        for cid in reversed(owned):
            cleanup = dict(container_id=cid, removed=False)
            observation['cleanup'].append(cleanup)
            try:
                label = command(['inspect', '--format', '{{index .Config.Labels "clearapi.schema-rehearsal"}}', cid])
                if label != nonce:
                    raise ValueError('isolated cleanup identity changed')
                command(['rm', '-f', '-v', cid])
                cleanup['removed'] = True
            except Exception:
                # Still attempt the remaining owned containers; never echo errors.
                pass
        observation['cleanup_complete'] = all(item['removed'] for item in observation['cleanup'])
        if not observation['cleanup_complete']:
            raise ValueError('isolated cleanup incomplete; no success receipt')
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
