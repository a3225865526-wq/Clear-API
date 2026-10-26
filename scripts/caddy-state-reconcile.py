#!/usr/bin/env python3
"""Read-only Caddy state comparison with append-only local audit output."""
import argparse
import hashlib
import importlib.util
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys


CONTAINER = 'clearapi-edge-caddy'
PRODUCTION_PATHS = (Path('/opt/clearapi').resolve(), Path('/run/clearapi').resolve())
LOCAL_AUDIT_ROOT = Path(__file__).resolve().parents[1] / 'evidence'
INSPECT_FORMAT = ('[{"Id":{{json .Id}},"Image":{{json .Image}},'
                  '"State":{"Running":{{json .State.Running}},"Paused":{{json .State.Paused}}},'
                  '"HostConfig":{"RestartPolicy":{{json .HostConfig.RestartPolicy}}}}]')
sys.dont_write_bytecode = True


class ProbeError(Exception):
    pass


def utc_now():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ProbeError('duplicate JSON field')
        result[key] = value
    return result


def normalize_edge(edge, reason='persistent_edge_invalid'):
    if not isinstance(edge, dict):
        raise ProbeError(reason)
    restart = edge.get('restart_policy') or edge.get('RestartPolicy') or {}
    if not isinstance(restart, dict):
        raise ProbeError(reason)
    normalized = {
        'id': edge.get('id', edge.get('Id')),
        'image': edge.get('image', edge.get('Image')),
        'running': edge.get('running', edge.get('Running')),
        'paused': edge.get('paused', edge.get('Paused')),
        'restart_policy': {
            'Name': restart.get('Name'),
            'MaximumRetryCount': restart.get('MaximumRetryCount'),
        },
    }
    if (not isinstance(normalized['id'], str) or not re.fullmatch(r'[a-f0-9]{64}', normalized['id'])
            or not isinstance(normalized['image'], str)
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', normalized['image'])
            or not isinstance(normalized['running'], bool)
            or not isinstance(normalized['paused'], bool)
            or normalized['restart_policy']['Name'] not in ('no', 'always', 'on-failure', 'unless-stopped')):
        raise ProbeError(reason)
    retries = normalized['restart_policy']['MaximumRetryCount']
    if type(retries) is not int or retries < 0:
        raise ProbeError(reason)
    return normalized


def docker_edge(command=None):
    command = command or subprocess.run
    try:
        active = command(['systemctl', 'is-active', '--quiet', 'docker.service'],
                         capture_output=True, text=True, timeout=5, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise ProbeError('docker_service_state_unknown') from error
    if active.returncode != 0:
        raise ProbeError('docker_service_not_active')
    try:
        result = command(
            ['docker', '--host', 'unix:///var/run/docker.sock', 'inspect', '--format', INSPECT_FORMAT, CONTAINER],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
    except (FileNotFoundError, OSError, subprocess.TimeoutExpired) as error:
        raise ProbeError(type(error).__name__) from error
    if result.returncode != 0:
        raise ProbeError('docker_inspect_failed')
    try:
        containers = json.loads(result.stdout, object_pairs_hook=unique)
    except (TypeError, ValueError) as error:
        raise ProbeError('docker_inspect_invalid_json') from error
    if not isinstance(containers, list) or len(containers) != 1:
        raise ProbeError('docker_inspect_cardinality')
    try:
        container = containers[0]
        return normalize_edge({
            'Id': container['Id'],
            'Image': container['Image'],
            'Running': container['State']['Running'],
            'Paused': container['State']['Paused'],
            'RestartPolicy': container['HostConfig']['RestartPolicy'],
        }, reason='docker_inspect_shape')
    except (KeyError, TypeError, AttributeError) as error:
        raise ProbeError('docker_inspect_shape') from error


def read_persisted_state(path):
    try:
        with path.open('r', encoding='utf-8') as stream:
            return json.load(stream, object_pairs_hook=unique)
    except FileNotFoundError as error:
        raise ProbeError('persistent_state_missing') from error
    except (OSError, TypeError, ValueError) as error:
        raise ProbeError('persistent_state_invalid') from error


def diff_values(observed, expected):
    differences = {}
    for key in ('id', 'image', 'running', 'paused', 'restart_policy'):
        if observed.get(key) != expected.get(key):
            differences[key] = {'observed': observed.get(key), 'expected': expected.get(key)}
    return differences


def probe(state_path, command=None, observed_at=None):
    observed_at = observed_at or utc_now()
    base = {
        'schema': 'clearapi.caddy-state-reconcile/v1',
        'observed_at_utc': observed_at,
        'container': CONTAINER,
        'mode': 'read_only',
        'persistent_state_path': str(state_path),
    }
    base.update(observed=None, persisted=None, expected=None, differences={}, snapshot_differences={},
                persisted_schema=None, persisted_stage=None, status='UNKNOWN',
                scope='caddy_state_only', suggested_action='retain_isolation_and_obtain_operator_review')
    errors = []
    try:
        persisted = read_persisted_state(state_path)
        if not isinstance(persisted, dict):
            raise ProbeError('persistent_state_invalid')
        base.update(persisted_schema=persisted.get('schema'), persisted_stage=persisted.get('stage'))
        persisted_edge = normalize_edge(persisted.get('edge', {}))
        base['persisted'] = persisted_edge
        if (persisted.get('schema') != 'clearapi.controlled-traffic/v1'
                or persisted.get('stage') != 'isolated'):
            raise ProbeError('persistent_journal_not_isolated')
        if (not persisted_edge['running'] or persisted_edge['paused']
                or persisted_edge['restart_policy']['Name'] != 'unless-stopped'):
            raise ProbeError('persistent_edge_snapshot_invalid')
        base['expected'] = dict(persisted_edge, running=False, paused=False,
                                restart_policy={'Name': 'no', 'MaximumRetryCount': 0})
        result_path=state_path.parent/'website-restoration-result-39347012653.json'
        if result_path.exists() or result_path.is_symlink():
            source=Path(__file__).with_name('local-deploy-state.py')
            spec=importlib.util.spec_from_file_location('local_state',source)
            state=importlib.util.module_from_spec(spec);spec.loader.exec_module(state)
            active=state.load(state_path.parent)
            result=json.loads(state.read_file(result_path,private=True),object_pairs_hook=unique)
            identity=hashlib.sha256(json.dumps(active['adoption'],sort_keys=True).encode()).hexdigest()
            if (result.get('schema')!='clearapi.website-restoration/v1'
                    or result.get('method')!='existing-running-adoption-v1'
                    or result.get('status')!='website_restored' or result.get('public_traffic')!='open'
                    or result.get('adoption_identity_sha256')!=identity
                    or result.get('authorization')!={'source_commit':active['adoption']['source_commit'],
                                                   'scope':'existing-gateway-original-caddy-v1'}
                    or result.get('edge',{}).get('container_id')!=persisted_edge['id']
                    or result.get('edge',{}).get('image_id')!=persisted_edge['image']):
                raise ProbeError('invalid_current_instance_restoration_result')
            base['expected']=persisted_edge
            base['effective_stage']='website_restored'
            base['historical_journal_preserved']=True
    except (ProbeError,OSError,ValueError,KeyError,TypeError) as error:
        errors.append(str(error))
    try:
        base['observed'] = docker_edge(command)
    except ProbeError as error:
        errors.append(str(error))
    if base['observed'] is not None and base['persisted'] is not None:
        base['snapshot_differences'] = diff_values(base['observed'], base['persisted'])
    if errors:
        return dict(base, reason=errors[0], errors=errors)
    observed, persisted_edge, expected_isolated = base['observed'], base['persisted'], base['expected']
    differences = diff_values(observed, expected_isolated)
    base['snapshot_differences'] = diff_values(observed, persisted_edge)
    if differences:
        status = 'FAIL'
        action = 'retain_isolation_and_reconcile_after_authorized_review'
    else:
        status = 'PASS'
        action = 'no_action'
    return dict(base, status=status, observed=observed, persisted=persisted_edge,
                expected=expected_isolated,
                differences=differences, suggested_action=action)


def append_audit_record(path, record, state_path=None):
    resolved = path.resolve()
    if any(resolved == root or root in resolved.parents for root in PRODUCTION_PATHS):
        raise ValueError('production audit paths are not allowed')
    if (resolved == LOCAL_AUDIT_ROOT.resolve() or LOCAL_AUDIT_ROOT.resolve() not in resolved.parents
            or str(resolved).startswith('\\\\')):
        raise ValueError('audit path must be inside the local evidence directory')
    if state_path is not None and (resolved == state_path.resolve()
                                  or path.exists() and state_path.exists() and path.samefile(state_path)):
        raise ValueError('audit path must not alias the persistent state')
    if path.is_symlink():
        raise ValueError('audit symlinks are not allowed')
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    flags = os.O_WRONLY | os.O_CREAT | os.O_APPEND | getattr(os, 'O_NOFOLLOW', 0)
    fd = os.open(path, flags, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise ValueError('audit target must be a regular file without hard links')
        with os.fdopen(fd, 'a', encoding='utf-8') as stream:
            stream.write(json.dumps(record, sort_keys=True, separators=(',', ':')) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
    except Exception:
        try:
            os.close(fd)
        except OSError:
            pass
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state', type=Path, required=True)
    output = parser.add_mutually_exclusive_group(required=True)
    output.add_argument('--audit-log', type=Path)
    output.add_argument('--stdout-only', action='store_true',
                        help='emit the probe record without writing any audit file')
    args = parser.parse_args()
    record = probe(args.state)
    if args.audit_log is not None:
        append_audit_record(args.audit_log, record, args.state)
    print(json.dumps(record, sort_keys=True))
    return 0 if record['status'] == 'PASS' else 1


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        sys.exit('Caddy state reconcile UNKNOWN: ' + type(error).__name__)
