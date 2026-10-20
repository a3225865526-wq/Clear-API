#!/usr/bin/env python3
"""Fixed PG03 Actions transport and evidence adapter; never a production entry."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import uuid

sys.dont_write_bytecode = True
SCRIPTS = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('isolated_successor', SCRIPTS / 'pg03-successor-plan.py')
PLAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PLAN)
REPO = 'a3225865526-wq/Clear-API'
SOURCE = '69a66431646b5cc8912bf97a739e08de07316443'
IMAGE = 'ghcr.io/a3225865526-wq/clearapi@sha256:07fd58272b4fc62bcc52a3ad1d20e0dac357ba93de9e91a6a797d5e8fe3e8e7a'
RC = '39357150402'
ARTIFACT = '12235966400'
ARCHIVE_SHA = 'ac7c42fc5a3549773f2c59080b503d6d5201f7442c9c20a955eb542f97ffb09b'
BASELINE = '3c1c77f355a2f9376f7cd4133267c99856cd4905b6116ad2ba615dbf84305035'
BINARY = '40ebc70665bf7f45c7a201c4fdd82a90913a1873b2c7ca0debda1829983eee4b'


def execute(args, raw=None, extra_env=None, include_stderr=False):
    # Do not inherit DOCKER_HOST/CONTEXT, GH_DEBUG, production DSNs or tokens.
    env = {key: os.environ[key] for key in ('PATH', 'HOME', 'LANG', 'SYSTEMROOT') if key in os.environ}
    env.update(extra_env or {})
    result = subprocess.run(args, input=raw, env=env, check=True, capture_output=True,
                            timeout=180)
    return result.stdout + result.stderr if include_stderr else result.stdout


def docker(root, args, raw=None, config=None):
    return execute(['docker', '--host', 'unix:///var/run/docker.sock', *args], raw,
                   {'DOCKER_CONFIG': str(config or root / 'docker-empty')},
                   include_stderr=args[0] == 'logs').decode().strip()


def api(path):
    # gh follows the artifact redirect without forwarding GitHub authorization
    # to the blob host. Neither headers, signed URLs nor response errors are logged.
    return execute(['gh', 'api', '--hostname', 'github.com', 'repos/'+REPO+'/'+path],
                   extra_env={'GH_TOKEN': os.environ['GH_TOKEN']})


def validate_rc(run, artifact):
    PLAN.require(run.get('id') == int(RC) and type(run.get('run_attempt')) is int
                 and run['run_attempt'] == 1 and run.get('head_sha') == SOURCE
                 and run.get('head_branch') == 'main' and run.get('status') == 'completed'
                 and run.get('conclusion') == 'success'
                 and run.get('path') == '.github/workflows/release-pg03-gateway.yml'
                 and run.get('event') in ('workflow_dispatch', 'workflow_run')
                 and run.get('repository', {}).get('full_name') == REPO
                 and run.get('head_repository', {}).get('full_name') == REPO,
                 'fixed RC identity mismatch')
    PLAN.require(artifact.get('id') == int(ARTIFACT) and artifact.get('expired') is False
                 and artifact.get('name') == 'pg03-gateway-release-'+RC+'-1'
                 and type(artifact.get('size_in_bytes')) is int
                 and 0 < artifact['size_in_bytes'] <= 10485760
                 and artifact.get('workflow_run', {}).get('id') == int(RC)
                 and artifact['workflow_run'].get('head_sha') == SOURCE,
                 'fixed RC archive unavailable or mismatched')


def baseline(root):
    validate_rc(json.loads(api('actions/runs/'+RC)),
                json.loads(api('actions/artifacts/'+ARTIFACT)))
    archive = root / 'rc.zip'
    with archive.open('xb') as stream:
        stream.write(api('actions/artifacts/'+ARTIFACT+'/zip'))
    candidate, _ = PLAN.successor_baseline(archive, ARCHIVE_SHA, BASELINE)
    PLAN.require(candidate.get('source_commit') == SOURCE and candidate.get('image_ref') == IMAGE
                 and candidate.get('workflow_run') == RC and candidate.get('workflow_run_attempt') == '1'
                 and candidate.get('binary_sha256') == BINARY
                 and candidate.get('production_ready') is False
                 and candidate.get('production_acceptance') == 'not_run', 'fixed candidate mismatch')
    return candidate


def erase_auth(root):
    # Exact files created here only; no global Docker logout, prune or home cleanup.
    config = root / 'registry-auth'
    if config.exists():
        PLAN.require(not config.is_symlink() and config.is_dir(), 'unsafe temporary auth directory')
        (config / 'config.json').unlink(missing_ok=True)
        config.rmdir()
    return not config.exists()


def pull(root):
    config = root / 'registry-auth'
    config.mkdir(mode=0o700)
    (config / 'config.json').write_text('{}\n', encoding='utf-8')
    try:
        docker(root, ['login', 'ghcr.io', '--username', 'a3225865526-wq', '--password-stdin'],
               (os.environ['GH_TOKEN']+'\n').encode(), config)
        docker(root, ['pull', '--platform', 'linux/amd64', IMAGE], config=config)
        docker(root, ['pull', '--platform', 'linux/amd64', PLAN.SCHEMA.POSTGRES], config=config)
    finally:
        erase_auth(root)


def cleanup(root, nonce):
    PLAN.require(re.fullmatch(r'[a-f0-9]{32}', nonce or ''), 'invalid cleanup ownership')
    ids = docker(root, ['ps', '-aq', '--no-trunc', '--filter', 'label=clearapi.schema-rehearsal='+nonce]).split()
    results = []
    for cid in ids:
        item = dict(container_id=cid, removed=False)
        results.append(item)
        try:
            PLAN.require(re.fullmatch(r'[a-f0-9]{64}', cid), 'unsafe cleanup id')
            label = docker(root, ['inspect', '--format', '{{index .Config.Labels "clearapi.schema-rehearsal"}}', cid])
            PLAN.require(label == nonce, 'cleanup owner mismatch')
            docker(root, ['rm', '-f', '-v', cid])
            item['removed'] = True
        except Exception:
            pass
    remaining = docker(root, ['ps', '-aq', '--no-trunc', '--filter', 'label=clearapi.schema-rehearsal='+nonce])
    return dict(containers=results, complete=not remaining and all(item['removed'] for item in results))


def sync_directory(path):
    # The execution target is Linux. Unsupported directory fsync must fail,
    # never silently downgrade the evidence persistence contract.
    PLAN.require(os.name == 'posix', 'Linux directory persistence required')
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def persist_json(path, value):
    with path.open('x', encoding='utf-8', newline='\n') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def quarantine_public(root):
    public = root / 'public'
    if not public.exists() and not public.is_symlink():
        return False
    PLAN.require(not public.is_symlink() and public.is_dir(), 'unsafe public evidence')
    destination = root / ('retained-public-'+uuid.uuid4().hex)
    # Both resolved rename targets must remain direct children of this run.
    PLAN.require(public.resolve().parent == root.resolve()
                 and destination.resolve().parent == root.resolve()
                 and not destination.exists(), 'unsafe evidence retention path')
    public.rename(destination)
    sync_directory(root)
    return True


def publish_evidence(root, state, receipt):
    """Persist private observation, receipt, then summary; publish as one directory."""
    PLAN.require(not (root / 'public').exists(), 'fresh public evidence required')
    staged = Path(tempfile.mkdtemp(prefix='evidence-', dir=root))
    public = staged / 'public'
    public.mkdir(mode=0o700)
    # A private intent is not an authoritative success result.
    persist_json(staged / 'state.json', dict(state, conclusion='pending_publication'))
    if receipt is not None:
        persist_json(public / 'success.json', receipt)
    persist_json(public / 'result.json', state)
    sync_directory(public)
    sync_directory(staged)
    public.rename(root / 'public')
    sync_directory(root)


def verify_public(root):
    """The uploader requires this read-back step to finish successfully."""
    public = root / 'public'
    result_path = public / 'result.json'
    PLAN.require(public.is_dir() and not public.is_symlink()
                 and result_path.is_file() and not result_path.is_symlink()
                 and result_path.stat().st_size <= 2097152, 'unsafe public result')
    result = json.loads(result_path.read_text(encoding='utf-8'), object_pairs_hook=PLAN.unique)
    success_path = public / 'success.json'
    if os.environ.get('FINALIZE_OUTCOME') == 'success':
        PLAN.require(result.get('conclusion') == 'success' and result.get('success_receipt_present') is True
                     and success_path.is_file() and not success_path.is_symlink(), 'incomplete success bundle')
        receipt = json.loads(success_path.read_text(encoding='utf-8'), object_pairs_hook=PLAN.unique)
        PLAN.SCHEMA.validate_receipt(receipt, dict(image_id=result['observation']['candidate']['image_id'],
            binary_sha256=BINARY, commit=SOURCE), BASELINE)
    else:
        # A failed producer cannot authorize an old success, even if the file
        # system refused to move/remove it. Do not reflect the old payload.
        PLAN.require(result.get('conclusion') == 'failure_or_unknown'
                     and result.get('success_receipt_present') is False
                     and not success_path.exists(), 'failed finalizer has no eligible failure bundle')
    with Path(os.environ['GITHUB_OUTPUT']).open('a', encoding='utf-8') as stream:
        stream.write('evidence_ready=true\n')
        stream.flush()
        os.fsync(stream.fileno())


def finalize(root, state):
    # Each cleanup is independent. Unknown cleanup never becomes success.
    state['final_cleanup'] = dict(complete=False)
    state['auth_removed'] = False
    try:
        state['auth_removed'] = erase_auth(root)
    except Exception:
        pass
    try:
        state['final_cleanup'] = cleanup(root, state.get('nonce'))
    except Exception:
        pass
    success = (os.environ.get('PRIOR_STATUS') == 'success' and state.get('rehearsal') == 'passed'
               and state['auth_removed'] and state['final_cleanup']['complete'])
    # Preserve old evidence outside the upload allowlist; never reuse it.
    if quarantine_public(root):
        state['publication_error'] = 'preexisting_public_evidence'
        success = False
    receipt = None
    if success:
        try:
            observation = state['observation']
            PLAN.require(observation.get('cleanup_complete') is True
                         and len(observation['startups']) == 2, 'incomplete startup evidence')
            for index, startup in enumerate(observation['startups'], start=1):
                PLAN.require(startup.get('pass_number') == index and startup.get('healthy') is True
                             and startup.get('before_schema_sha256') == BASELINE
                             and startup.get('after_schema_sha256') == BASELINE
                             and all(type(startup.get(key)) is int and startup[key] == 0 for key in
                                     ('create_exit_code', 'start_exit_code', 'stop_exit_code'))
                             and startup.get('process') == dict(Status='exited', Running=False,
                                 ExitCode=0, OOMKilled=False, error_present=False),
                             'incomplete startup evidence')
                PLAN.require(startup.get('migration', {}).get('status') == 'success',
                             'migration evidence is not conclusive')
            receipt = json.loads((root / 'schema.json').read_text(encoding='utf-8'))
            PLAN.SCHEMA.validate_receipt(receipt, dict(image_id=state['observation']['candidate']['image_id'],
                binary_sha256=BINARY, commit=SOURCE), BASELINE)
            PLAN.require(receipt['target_schema_sha256'] == BASELINE, 'final schema mismatch')
        except Exception:
            success = False
    state.update(conclusion='success' if success else 'failure_or_unknown',
                 prior_job_status=os.environ.get('PRIOR_STATUS', 'unknown'),
                 success_receipt_present=success)
    state.setdefault('steps', {})['finalize'] = 'passed' if success else 'failed'
    try:
        publish_evidence(root, state, receipt if success else None)
    except Exception:
        # Withdraw a directory exposed before a directory-sync/output failure.
        # If withdrawal or failure publication is impossible, propagate nonzero;
        # the separate read-back step cannot authorize an old success.
        quarantine_public(root)
        success = False
        state.update(conclusion='failure_or_unknown', success_receipt_present=False,
                     publication_error='required_evidence_write_failed')
        state['steps']['finalize'] = 'failed'
        publish_evidence(root, state, None)
    PLAN.require(success, 'isolated rehearsal incomplete; failure evidence only')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('prepare', 'baseline', 'pull', 'rehearse', 'finalize', 'verify-public'))
    args = parser.parse_args()
    e = os.environ
    for key in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'):
        PLAN.require(re.fullmatch(r'[1-9][0-9]*', e.get(key, '')), 'exact run identity required')
    expected = Path(e['RUNNER_TEMP']).resolve() / ('pg03-isolated-'+e['GITHUB_RUN_ID']+'-'+e['GITHUB_RUN_ATTEMPT'])
    root = Path(e['REHEARSAL_ROOT'])
    PLAN.require(root.resolve() == expected and not root.is_symlink(), 'unsafe run directory')
    os.umask(0o077)
    if args.mode == 'verify-public':
        verify_public(root)
        return
    state_path = root / 'state.json'
    if args.mode == 'prepare':
        PLAN.require(e.get('GITHUB_ACTIONS') == 'true' and e.get('GITHUB_REPOSITORY') == REPO
                     and e.get('GITHUB_EVENT_NAME') == 'workflow_dispatch'
                     and e.get('GITHUB_REF') == 'refs/heads/main'
                     and e.get('GITHUB_ACTOR') == 'a3225865526-wq'
                     and e.get('GITHUB_TRIGGERING_ACTOR') == 'a3225865526-wq'
                     and re.fullmatch(r'[a-f0-9]{40}', e.get('CONTROL_SHA', ''))
                     and e['CONTROL_SHA'] == e.get('GITHUB_SHA')
                     and execute(['git', 'rev-parse', 'HEAD']).decode().strip() == e['CONTROL_SHA']
                     and not execute(['git', 'status', '--porcelain']).strip(),
                     'reviewed main control identity required')
        root.mkdir(mode=0o700)
        (root / 'docker-empty').mkdir(mode=0o700)
        state = dict(schema='clearapi.isolated-actions-rehearsal/v1', nonce=uuid.uuid4().hex,
            control_commit=e['CONTROL_SHA'], workflow_ref=e['GITHUB_WORKFLOW_REF'],
            run_id=e['GITHUB_RUN_ID'], run_attempt=e['GITHUB_RUN_ATTEMPT'],
            candidate_source=SOURCE, candidate_image=IMAGE, rc_run=RC, rc_attempt=1,
            artifact_id=ARTIFACT, archive_sha256=ARCHIVE_SHA, baseline_schema_sha256=BASELINE,
            production_observation_verified=False, production_acceptance='not_run',
            rehearsal='not_run', control_sha256={
                str(path.relative_to(SCRIPTS.parent)): hashlib.sha256(path.read_bytes()).hexdigest()
                for path in (Path(__file__).resolve(), SCRIPTS / 'pg03-successor-plan.py',
                             SCRIPTS / 'pg03-schema-rehearsal.py', SCRIPTS / 'ci/assemble-release-proof.py',
                             SCRIPTS / 'ci/test-pg03-successor-plan.py',
                             SCRIPTS.parent / '.github/workflows/pg03-isolated-rehearsal.yml')})
    elif args.mode == 'finalize' and not state_path.exists():
        # Prepare/checkout can fail before initialization. Retain UNKNOWN rather
        # than claim that an unobserved invocation cleaned up successfully.
        root.mkdir(mode=0o700, exist_ok=True)
        state = dict(rehearsal='not_run', production_acceptance='not_run', nonce=None)
    else:
        state = json.loads(state_path.read_text(encoding='utf-8'))
    state.setdefault('steps', {})[args.mode] = 'started'
    if args.mode == 'finalize':
        # No fallible state write is permitted after the final publication.
        finalize(root, state)
        return
    try:
        if args.mode == 'baseline':
            baseline(root)
            state['baseline_verified'] = True
        elif args.mode == 'pull':
            PLAN.require(state.get('baseline_verified') is True, 'validate RC before credentials')
            store = json.loads(docker(root, ['info', '--format', '{{json .DriverStatus}}']))
            PLAN.require(['driver-type', 'io.containerd.snapshotter.v1'] in store,
                         'containerd image store required')
            state['image_store'] = 'containerd'
            pull(root)
            state.update(images_pulled=True, auth_removed=True)
        elif args.mode == 'rehearse':
            PLAN.require(state.get('images_pulled') is True and erase_auth(root), 'erase credentials before startup')
            for ref in (IMAGE, PLAN.SCHEMA.POSTGRES):
                image = json.loads(docker(root, ['image', 'inspect', ref]))[0]
                PLAN.require(ref in image.get('RepoDigests', []) and image.get('Os') == 'linux'
                             and image.get('Architecture') == 'amd64', 'fixed image unavailable')
            observation = {}
            state['observation'] = observation
            # Injection only binds commands to this local daemon and an empty auth config.
            # The existing successor remains the single schema/startup orchestrator.
            PLAN.rehearse_successor_schema(root / 'rc.zip', ARCHIVE_SHA, BASELINE,
                root / 'schema.json', observation, state['nonce'],
                command=lambda args, raw=None: docker(root, args, raw))
            state['rehearsal'] = 'passed'
        state['steps'][args.mode] = 'passed'
    except Exception:
        state['steps'][args.mode] = 'failed'
        raise
    finally:
        state_path.write_text(json.dumps(state, indent=2)+'\n', encoding='utf-8')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('PG03 ISOLATED BLOCKED: see allowlisted evidence; raw errors and logs suppressed')
