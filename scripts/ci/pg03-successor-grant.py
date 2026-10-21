#!/usr/bin/env python3
"""Read-only approval artifact producer. Never executes or accesses production.

Environment review provenance is verified later through the live GitHub API.
This artifact alone does not establish that any review occurred or grant access.
"""
from datetime import datetime, timezone
import importlib.util
import os
from pathlib import Path
import secrets
import sys

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('grant_transaction', Path(__file__).parents[1]/'pg03-successor-transaction.py')
TX = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TX)
REPO = 'a3225865526-wq/Clear-API'


def build(raw, policy_sha, context, now):
    request = TX.validate_request(raw, now)
    TX.PLAN.require(context.get('GITHUB_REPOSITORY') == REPO
        and context.get('GITHUB_REF') == 'refs/heads/main'
        and context.get('GITHUB_EVENT_NAME') == 'workflow_dispatch'
        and all(context.get(key) == 'a3225865526-wq' for key in ('GITHUB_ACTOR', 'GITHUB_TRIGGERING_ACTOR'))
        and context.get('GITHUB_SHA') == request['control_commit']
        and context.get('GITHUB_WORKFLOW_REF') == REPO+'/.github/workflows/pg03-successor-approval.yml@refs/heads/main'
        and TX.matches(r'[a-f0-9]{64}', policy_sha), 'grant context mismatch')
    TX.PLAN.require(raw == TX.encoded(request), 'canonical request required')
    return dict(schema='clearapi.successor-production-grant/v1', repository=REPO,
        request_sha256=TX.sha(raw), transaction_id=request['transaction_id'], candidate=request['candidate'],
        control_commit=request['control_commit'], evidence_sha256=request['evidence_sha256'],
        qualification_policy_sha256=policy_sha, issued_at_utc=now.isoformat().replace('+00:00', 'Z'),
        expires_at_utc=request['expires_at_utc'], nonce=secrets.token_hex(32), operations=request['operations'],
        public_traffic='blocked', executor_start='forbidden', database_restore='forbidden')


def main():
    TX.PLAN.require(len(sys.argv) == 2 and sys.argv[1] in ('inspect', 'grant'), 'fixed mode required')
    raw = os.environ.get('REQUEST_JSON', '').encode()
    grant = build(raw, os.environ.get('POLICY_SHA', ''), os.environ, datetime.now(timezone.utc))
    if sys.argv[1] == 'inspect':
        # Only validated hash/hex/enum fields reach the reviewer summary.
        with Path(os.environ['GITHUB_STEP_SUMMARY']).open('a', encoding='utf-8') as saved:
            saved.write('PG03 independent grant; no production execution\n\n```json\n'
                        + TX.encoded(grant).decode() + '\n```\n')
    else:
        folder = Path(os.environ['RUNNER_TEMP'])/'pg03-successor-grant'
        folder.mkdir(mode=0o700)
        with (folder/'production-grant.json').open('xb') as saved:
            saved.write(TX.encoded(grant))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('SUCCESSOR GRANT BLOCKED: invalid, stale or mismatched request; no production action')
