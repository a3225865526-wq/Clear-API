#!/usr/bin/env python3
"""Read-only release investigation; emits allowlisted metadata, never raw Secrets."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from datetime import datetime, timezone

REPO = 'a3225865526-wq/Clear-API'
KINDS = ('backup', 'provider', 'rollback')
IDENTITIES = {'source_commit': r'[a-f0-9]{40}', 'gateway_digest': r'sha256:[a-f0-9]{64}',
              'manifest_sha256': r'[a-f0-9]{64}', 'source_schema_sha256': r'[a-f0-9]{64}',
              'target_schema_sha256': r'[a-f0-9]{64}'}
PUBLIC_HISTORY = {
    'archived_9a3e_RC39193373708': ('9a3e6585737d823896137346faf94c2e1f4d1b04',
        'sha256:95f7e50f1894171aef3eaf5c94b9e123491430c8f17d4caab3c107aa0ae79b6c', 39193373708),
    'passed_cd6f_RC39197505434': ('cd6f719e79de432de6219561b04502784cb58930',
        'sha256:bdbb535b8826aa243eee7922a7a5e2d0fb8646f69a34653b4251ca60c488fdc6', 39197505434),
    'prior_8b4b_RC39200419037': ('8b4bc07cf02c8fc23884d64278b728eafa23a6eb',
        'sha256:31b178271d01aeddbcee9a510a880ea5f40ed9221c7d10ecdc010164a7001ecf', 39200419037),
    'current_d0c2_RC39205228315': ('d0c20d6c46d86845805e4a9d35b067b0f6b20be4',
        'sha256:98e615c08fd0697506340c2150c6e2a7e5aa53c543741ce61ebe4a190424ca65', 39205228315)}


class AuditBlocked(ValueError):
    """Only fixed, non-secret messages from this module may be reflected."""


def gh(*args):
    result = subprocess.run(['gh', *args], capture_output=True, text=True, timeout=120)
    if result.returncode:
        # Do not reflect arbitrary API bodies, subprocess stderr or credentials.
        raise AuditBlocked('GitHub read unavailable; check Actions read / Deployments read and endpoint policy')
    return result.stdout


def pages(path):
    return json.loads(gh('api', '--paginate', '--slurp', f'repos/{REPO}/{path}'))


def evidence_metadata(raw, kind, manifest, manifest_hash, target):
    report = {'configured': bool(raw), 'raw_evidence_printed': False,
              'original_acceptance_verified': False}
    try:
        data = json.loads(raw)
        if not isinstance(data, dict):
            raise ValueError()
    except (ValueError, TypeError):
        report['error'] = 'Evidence unavailable or not a JSON object; no referenced file was opened'
        return report
    report['evidence_sha256'] = hashlib.sha256(raw.encode()).hexdigest()
    identity = {}
    for field, pattern in IDENTITIES.items():
        value = data.get(field)
        identity[field] = value if isinstance(value, str) and re.fullmatch(pattern, value) else None
    for field in ('rc_run_id', 'rc_run_attempt'):
        value = data.get(field)
        identity[field] = value if type(value) is int and value > 0 else None
    report['declared_release_identity'] = identity
    report['public_history_matches'] = {}
    for label, values in PUBLIC_HISTORY.items():
        fields = [field for field, value in zip(('source_commit', 'gateway_digest', 'rc_run_id'), values)
                  if identity[field] == value]
        if fields:
            report['public_history_matches'][label] = fields
    expected = {'source_commit': manifest['source_commit'], 'gateway_digest': manifest['image_digest'],
                'rc_run_id': int(manifest['workflow_run']),
                'rc_run_attempt': int(manifest['workflow_run_attempt']), 'manifest_sha256': manifest_hash,
                **{field: manifest['schema_transition'][field]
                   for field in ('source_schema_sha256', 'target_schema_sha256')}}
    report['binding_matches'] = {field: identity[field] == value for field, value in expected.items()}
    report['binding_matches'].update(
        kind=data.get('kind') == kind, target_id=bool(target) and data.get('target_id') == target,
        schema=data.get('schema') == 'clearapi.rehearsal-evidence/v1',
        operator_declared_success=data.get('conclusion') == 'success')
    # Arbitrary provenance text, URLs and payloads might contain credentials.
    # Report presence only; a separate investigation must verify the originals.
    report['source_reference_present'] = any(data.get(k) for k in
        ('source_reference', 'source_artifact', 'source_run_id', 'provenance', 'measurement'))
    report['rollback_proven'] = data.get('rollback_proven') is True
    report['production_restore_executed'] = data.get('production_restore_executed') is True
    return report


def deployment_metadata(record, statuses):
    sha = record.get('sha', '')
    if (type(record.get('id')) is not int or not isinstance(sha, str)
            or not re.fullmatch(r'[a-f0-9]{40}', sha)):
        raise AuditBlocked('Malformed deployment record; investigation remains blocked')
    created = datetime.fromisoformat(record['created_at'].replace('Z', '+00:00'))
    if created.tzinfo is None:
        raise AuditBlocked('Deployment timestamp is not timezone aware')
    observed = []
    for status in statuses:
        state = status.get('state')
        if state not in ('error', 'failure', 'inactive', 'pending', 'success', 'queued', 'in_progress'):
            raise AuditBlocked('Unknown deployment status; investigation remains blocked')
        log = status.get('log_url', '')
        match = re.fullmatch(r'https://github\.com/' + re.escape(REPO)
                             + r'/actions/runs/([1-9][0-9]*)(?:/job/[1-9][0-9]*)?', log or '')
        observed.append({'id': status['id'], 'state': state,
                         'actions_run_id': int(match[1]) if match else None})
    return {'id': record['id'], 'source_commit': sha, 'statuses': observed,
            'created_at': created.isoformat(),
            'requires_investigation': True}


def main():
    report = {'schema': 'clearapi.release-state-audit/v1',
              'status': 'BLOCKED', 'audit_complete': False,
              'stage': 'rc_identity', 'not_release_evidence': True,
              'observed_at': datetime.now(timezone.utc).isoformat(),
              'production_deployed': False, 'circuit_recovered': False,
              'vps_pending_operations': 'UNKNOWN', 'errors': []}
    try:
        run = os.environ.get('AUDIT_RC_RUN_ID', '')
        if not re.fullmatch(r'[1-9][0-9]*', run):
            raise AuditBlocked('RC id must identify an existing successful run')
        rc = json.loads(gh('api', f'repos/{REPO}/actions/runs/{run}'))
        current = json.loads(gh('api', f'repos/{REPO}/git/ref/heads/main'))['object']['sha']
        if (rc['status'] != 'completed' or rc['conclusion'] != 'success'
                or rc['head_branch'] != 'main' or rc['head_sha'] != current
                or rc['event'] not in ('workflow_run', 'workflow_dispatch')
                or rc['path'] != '.github/workflows/release-pg03-gateway.yml'
                or rc['repository']['full_name'] != REPO
                or rc['head_repository']['full_name'] != REPO):
            raise AuditBlocked('RC does not identify the successful current-main release')
        with tempfile.TemporaryDirectory(prefix='clearapi-audit-') as directory:
            gh('run', 'download', run, '--repo', REPO, '--name',
               f"pg03-gateway-release-{run}-{rc['run_attempt']}", '--dir', directory)
            spec = importlib.util.spec_from_file_location('release_policy', Path(__file__).with_name('automatic-release.py'))
            policy = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(policy)
            manifest = policy.verify_rc_artifact(Path(directory))
            if (manifest['source_commit'] != current or int(manifest['workflow_run']) != int(run)
                    or int(manifest['workflow_run_attempt']) != rc['run_attempt']
                    or not re.fullmatch(r'sha256:[a-f0-9]{64}', manifest['image_digest'])
                    or any(not re.fullmatch(r'[a-f0-9]{64}', manifest['schema_transition'][k])
                           for k in ('source_schema_sha256', 'target_schema_sha256'))):
                raise AuditBlocked('RC artifact identity mismatch')
            manifest_hash = hashlib.sha256((Path(directory) / 'gateway-image-release.json').read_bytes()).hexdigest()
        report['candidate'] = {'source_commit': current, 'rc_run_id': int(run),
            'rc_run_attempt': rc['run_attempt'], 'manifest_sha256': manifest_hash,
            'gateway_digest': manifest['image_digest'], 'schema_transition': {
                k: manifest['schema_transition'][k] for k in ('source_schema_sha256', 'target_schema_sha256')}}
        report['operator_evidence'] = {kind: evidence_metadata(
            os.environ.get(f'CLEARAPI_REHEARSAL_{kind.upper()}_EVIDENCE', ''), kind,
            manifest, manifest_hash, os.environ.get('CLEARAPI_REHEARSAL_TARGET_ID', '')) for kind in KINDS}
        report['stage'] = 'production_deployment_records'
        deployments = [d for page in pages('deployments?environment=production&per_page=100') for d in page]
        report['production_deployments'] = []
        for record in deployments:
            statuses = [s for page in pages(f"deployments/{record['id']}/statuses?per_page=100") for s in page]
            report['production_deployments'].append(deployment_metadata(record, statuses))
        report['stage'] = 'formal_production_runs'
        report['formal_production_runs'] = []
        for workflow in ('automatic-deploy.yml', 'deploy-production.yml'):
            runs = [r for page in pages(f'actions/workflows/{workflow}/runs?per_page=100')
                    for r in page['workflow_runs']]
            for run_record in runs:
                if (run_record['status'] == 'completed'
                        and (run_record['conclusion'] == 'skipped'
                             or run_record['created_at'] < '2026-10-18T23:25:57Z')):
                    continue
                report['formal_production_runs'].append({
                    'workflow': workflow, 'id': run_record['id'],
                    'source_commit': run_record['head_sha'], 'status': run_record['status'],
                    'conclusion': run_record['conclusion'], 'created_at': run_record['created_at']})
        report['stage'] = 'read_only_audit_complete'
        report['audit_complete'] = True
    except Exception as error:
        # Fixed errors from this module only; no API responses or Secret values.
        report['errors'].append(str(error) if type(error) is AuditBlocked else type(error).__name__)
    Path('release-state-audit.json').write_text(json.dumps(report, sort_keys=True, indent=2) + '\n')
    # Logs contain comparisons to already-public history, not values extracted
    # from Secrets. Full allowlisted metadata is confined to the audit artifact.
    print('AUDIT_RESULT complete=' + str(int(report['audit_complete'])) + ' stage=' + report['stage'])
    for kind, evidence in report.get('operator_evidence', {}).items():
        mismatches = ','.join(k for k, v in evidence.get('binding_matches', {}).items() if not v) or 'none'
        history = ';'.join(k + ':' + ','.join(v) for k, v in evidence.get('public_history_matches', {}).items()) or 'none'
        print(f'AUDIT_EVIDENCE kind={kind} configured={int(evidence["configured"])} mismatches={mismatches} public_history={history}')
    for record in report.get('production_deployments', []):
        states = ','.join(s['state'] for s in record['statuses']) or 'UNKNOWN'
        runs = ','.join(str(s['actions_run_id']) for s in record['statuses'] if s['actions_run_id']) or 'UNKNOWN'
        print(f'AUDIT_DEPLOYMENT id={record["id"]} sha={record["source_commit"]} created={record["created_at"]} states={states} actions_runs={runs}')
    for run in report.get('formal_production_runs', []):
        print(f'AUDIT_FORMAL_RUN workflow={run["workflow"]} id={run["id"]} sha={run["source_commit"]} status={run["status"]} conclusion={run["conclusion"]}')
    for error in report['errors']:
        print('AUDIT_BLOCKED ' + error)
    return 0 if report['audit_complete'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
