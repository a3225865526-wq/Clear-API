"""Focused continuation of the existing isolated acceptance; no provider calls.

Requires the private runtime from the previous acceptance and its two local
test containers. Never initializes, migrates, or directly edits a database.
Phases are explicit so restarts can be observed between immutable snapshots.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import datetime
import http.client
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('phase', choices=['exercise', 'review', 'snapshot', 'ordinary', 'close'])
parser.add_argument('--runtime', type=Path, required=True)
args = parser.parse_args()
repo = Path(__file__).resolve().parents[1]
out = repo / 'evidence/sub2api-billing-closeout-20261030'
out.mkdir(exist_ok=True)
runtime = args.runtime
state = json.loads((runtime / 'acceptance-state.json').read_text(encoding='utf-8'))
env = dict(line.split('=', 1) for line in (runtime / 'test.env').read_text().splitlines() if '=' in line)
gw, sub = 'http://127.0.0.1:18883', 'http://127.0.0.1:18882/api/v1'
container = 'clearapi-sub2api-isolated-20261030-acceptance-gateway-1'


def save(name, data):
    (out / name).write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def call(base, method, path, body=None, token=None, expect=200):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(base + path, json.dumps(body).encode() if body is not None else None, headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=25) as response:
            code, raw = response.status, response.read()
    except urllib.error.HTTPError as error:
        code, raw = error.code, error.read()
    result = json.loads(raw)
    assert code == expect, (method, path, code, result.get('code'))
    if expect == 200:
        assert result.get('success') is not False
    return result.get('data', result)


def snapshot():
    script = """const {DatabaseSync}=require('node:sqlite');
const d=new DatabaseSync('/ledger/gateway.db',{readOnly:true});d.exec('PRAGMA busy_timeout=5000');
const q=s=>d.prepare(s).all();console.log(JSON.stringify({
user:q('SELECT id,quota,used_quota FROM users WHERE id=1')[0],
token:q('SELECT id,remain_quota,used_quota FROM tokens WHERE id=1')[0],
reservations:q('SELECT * FROM billing_reservations ORDER BY id'),
logs:q('SELECT id,request_id,quota,type,prompt_tokens,completion_tokens FROM logs ORDER BY id'),
outbox:q('SELECT id,request_id,state FROM billing_completion_outboxes ORDER BY id'),
receipts:q('SELECT * FROM billing_completion_receipts ORDER BY id'),
markers:q('SELECT * FROM billing_review_markers ORDER BY id'),
decisions:q('SELECT * FROM billing_reservation_decisions ORDER BY id'),
audit:q("SELECT * FROM finance_audits WHERE action='billing.decide' ORDER BY id"),
refund_events:q("SELECT event_key,request_id,payload FROM finance_request_events WHERE event_key LIKE 'billing-review-refund-%' ORDER BY id")
}));d.close();"""
    assert state['gateway_user_id'] == state['gateway_token_id'] == 1
    return json.loads(subprocess.check_output(['docker', 'exec', container, 'node', '--no-warnings', '-e', script], text=True))


def sub_usage():
    sql = "SELECT coalesce(json_agg(x),'[]') FROM (SELECT id,request_id,model,api_key_id,account_id,input_tokens,output_tokens,cache_read_tokens FROM usage_logs ORDER BY id) x;"
    return json.loads(subprocess.check_output(['docker', 'exec', 'clearapi-sub2api-isolated-20261030-postgres-1', 'psql', '-U', 'sub2test', '-d', 'sub2test', '-Atc', sql], text=True))


admin = call(sub, 'POST', '/auth/login', {'email': 'sub2-test@example.invalid', 'password': env['SUB2_TEST_ADMIN_PASSWORD']})['access_token']
owner = call(gw, 'POST', '/api/user/login', {'username': 'acceptroot', 'password': state['gateway_password']})['access_token']
finance = '/api/owner/finance/billing-reservations'

if args.phase == 'exercise':
    if not (out / 'before.json').exists():
        save('before.json', snapshot())
    capability = call(gw, 'GET', '/api/owner/finance/capability', token=owner)
    assert capability['billing_reconciliation_ready']
    save('capability.json', capability)
    user = call(sub, 'POST', '/auth/login', {'email': 'clearapi-executor@example.invalid', 'password': state['sub_user_password']})['access_token']
    call(sub, 'PUT', '/keys/' + str(state['keys']['openai']['id']), {'status': 'active'}, user)
    aid = state['accounts']['openai']
    account = call(sub, 'GET', '/admin/accounts/' + str(aid), token=admin)
    additions = {'accept-timeout': 'mock-timeout', 'accept-completed-drop': 'mock-completed-drop', 'accept-completed-drop-small': 'mock-completed-drop-small'}
    credentials = account['credentials']
    assert credentials['base_url'] == 'http://mock-upstream:8090/openai'
    private_env = dict(line.split('=', 1) for line in (runtime / 'acceptance.env').read_text().splitlines() if '=' in line)
    # Admin GET intentionally redacts secrets and model_mapping. Restore only
    # this known synthetic fixture's credentials, never a provider account.
    assert account['name'] == 'clearapi-acceptance-mock-openai'
    credentials.update(api_key=private_env['SUB2_ACCEPTANCE_MOCK_KEY'], pool_mode=False,
                       model_mapping={m: m for m in [*state['gateway_models'].values(), 'mock-retry', *additions.values()]})
    extra = account.get('extra') or {}
    extra.update(openai_passthrough=True)
    call(sub, 'PUT', '/admin/accounts/' + str(aid), {'credentials': credentials, 'extra': extra}, admin)
    call(sub, 'POST', '/admin/accounts/' + str(aid) + '/schedulable', {'schedulable': True}, admin)
    cid = state['gateway_channels']['openai']
    channel = call(gw, 'GET', '/api/channel/' + str(cid), token=owner)
    mapping = json.loads(channel['model_mapping'])
    mapping.update(additions)
    call(gw, 'PUT', '/api/channel/', {'id': cid, 'models': ','.join(mapping), 'model_mapping': json.dumps(mapping)}, owner)
    pricing = call(gw, 'GET', '/api/option/model_pricing?' + urllib.parse.urlencode([('model', m) for m in additions]), token=owner)
    entries = {e['model_name']: e for e in pricing['entries']}
    call(gw, 'PATCH', '/api/option/model_pricing', {'changes': [{'model_name': m, 'expected_version': entries.get(m, {}).get('version', pricing['empty_version']), 'pricing': {'billing_setting.billing_mode': 'tiered_expr', 'billing_setting.billing_expr': 'tier("mock", p*2 + cr*0.5 + c*4)'}} for m in additions]}, owner)
    call(gw, 'POST', '/api/channel/' + str(cid) + '/status', {'status': 1}, owner)
    cases = json.loads((out / 'new-cases.json').read_text()) if (out / 'new-cases.json').exists() else []
    for public, upstream in additions.items():
        if any(c['model'] == public for c in cases):
            continue  # Never redispatch a case after an observer-only failure.
        before = snapshot()
        stream = public == 'accept-timeout'
        if stream:
            urllib.request.urlopen('http://127.0.0.1:18884/control/reset').close()
        connection = http.client.HTTPConnection('127.0.0.1', 18883, timeout=20)
        start = time.monotonic()
        connection.request('POST', '/v1/responses', json.dumps({'model': public, 'input': 'isolated billing boundary', 'max_output_tokens': 32, 'stream': stream}), {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state['gateway_relay_key']})
        response = connection.getresponse()
        rid = response.getheader('X-Oneapi-Request-Id')
        raw = response.read()
        connection.close()
        elapsed = time.monotonic() - start
        if stream:
            urllib.request.urlopen('http://127.0.0.1:18884/control/release').close()
        case = {'model': public, 'upstream_model': upstream, 'request_id': rid, 'http_status': response.status, 'seconds': elapsed, 'response': raw.decode(errors='replace')}
        cases.append(case)
        save('new-cases.json', cases)
        deadline = time.monotonic() + 90
        while True:
            after = snapshot()
            reservation = next(r for r in after['reservations'] if r['request_id'] == rid)
            usage = sub_usage()
            if reservation['state'] != 'active' and all(r['state'] == 'delivered' for r in after['outbox']) and (not stream or any(r['model'] == upstream for r in usage)):
                break
            assert time.monotonic() < deadline, 'Completion/late usage did not arrive'
            time.sleep(1)
        markers = [m for m in after['markers'] if m['reservation_id'] == reservation['id']]
        assert markers and len([r for r in after['logs'] if r['type'] == 2 and r['request_id'] == rid]) == 1
        assert before['user']['quota'] - after['user']['quota'] == reservation['applied']
        case.update(reservation=reservation, markers=markers, sub2_usage=[r for r in usage if r['model'] == upstream])
        save('new-cases.json', cases)
        print(public, response.status, reservation['state'], reservation['applied'], flush=True)
    executions = [json.loads(line) for line in (runtime / 'mock-executions.jsonl').read_text().splitlines()]
    save('controlled-provider-executions.json', executions)
    save('after-new-cases.json', snapshot())

elif args.phase == 'review':
    before = snapshot()
    pending = call(gw, 'GET', finance + '?scope=pending&page_size=100', token=owner)
    save('pending-before.json', pending)
    old = json.loads((repo / 'evidence/sub2api-final-acceptance-20261030/mock-e2e.json').read_text(encoding='utf-8'))
    rejected = next(c for c in old['cases'] if c['name'] == 'upstream-rejection')
    small = next(c for c in json.loads((out / 'new-cases.json').read_text()) if c['model'] == 'accept-completed-drop-small')
    results = []
    # One proven-zero pre-execution rejection, and one provider execution whose
    # verified 2 uncached input tokens cost exactly 2 synthetic quota units.
    for rid, final, reason in [(rejected['request_id'], 0, 'Isolated mock-reject code proves rejection before execution; old mock E2E request and fixture verified; final quota zero.'), (small['request_id'], 2, 'Controlled provider execution receipt: input=2 output=0 cache=0; frozen synthetic expression tier mock gives quota=2; execution ended; no inferred zero.')]:
        row = next(r for r in pending['items'] if r['request_id'] == rid)
        assert not row['executing'] and row['applied'] >= final
        payload = {'expected_state': row['state'], 'expected_applied': row['applied'], 'expected_target': row['target'], 'refund_quota': row['applied'] - final, 'reason': reason, 'execution_ended': True}
        path = finance + '/' + str(row['id']) + '/decision'
        call(gw, 'POST', path, {**payload, 'expected_applied': row['applied'] + 1}, owner, expect=409)
        call(gw, 'POST', path, {**payload, 'execution_ended': False}, owner, expect=400)
        call(gw, 'POST', path, {**payload, 'refund_quota': row['applied'] + 1}, owner, expect=409)
        with ThreadPoolExecutor(max_workers=2) as executor:
            decisions = list(executor.map(lambda _: call(gw, 'POST', path, payload, owner), range(2)))
        assert decisions[0]['id'] == decisions[1]['id'] and decisions[0]['final_quota'] == final
        call(gw, 'POST', path, payload, owner)
        call(gw, 'POST', path, {**payload, 'reason': reason + ' changed'}, owner, expect=409)
        results.append({'request_id': rid, 'before': row, 'decision': decisions[0], 'parallel_duplicate_same_id': True, 'third_duplicate_ok': True, 'stale_conflict': 409, 'missing_execution_confirmation': 400, 'over_refund_conflict': 409, 'changed_decision_conflict': 409})
    after = snapshot()
    refund = sum(r['decision']['refund_quota'] for r in results)
    assert after['user']['quota'] - before['user']['quota'] == after['token']['remain_quota'] - before['token']['remain_quota'] == refund
    assert len(after['decisions']) - len(before['decisions']) == len(after['audit']) - len(before['audit']) == 2
    assert after['logs'] == before['logs'] and after['outbox'] == before['outbox'] and after['receipts'] == before['receipts']
    reviewed = call(gw, 'GET', finance + '?scope=reviewed&page_size=100', token=owner)
    save('review-results.json', {'results': results, 'refund_total': refund, 'reviewed': reviewed, 'gross_logs_immutable': True, 'wallet_token_single_refund': True})
    save('after-review.json', after)
    print('Review/refund/concurrent duplicate assertions passed; refund', refund)

elif args.phase == 'snapshot':
    current = snapshot()
    reference = json.loads((out / 'after-review.json').read_text())
    for key in ['user', 'token', 'reservations', 'logs', 'outbox', 'receipts', 'markers', 'decisions', 'audit', 'refund_events']:
        assert current[key] == reference[key], key
    rounds = list(out.glob('recovery-round-*.json'))
    save('recovery-round-' + str(len(rounds) + 1) + '.json', {'time_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'unchanged_vs_after_review': True, 'snapshot': current})
    print('Recovery round', len(rounds) + 1, 'all financial identities/receipts unchanged')

elif args.phase == 'ordinary':
    assert not (out / 'ordinary-with-review-enabled.json').exists()
    before = snapshot()
    for cid in [1, 2, 4, 5]:
        call(gw, 'POST', '/api/channel/' + str(cid) + '/status', {'status': 2}, owner)
    call(gw, 'POST', '/api/channel/3/status', {'status': 1}, owner)
    connection = http.client.HTTPConnection('127.0.0.1', 18883, timeout=20)
    connection.request('POST', '/v1/chat/completions', json.dumps({'model': 'ordinary-model', 'messages': [{'role': 'user', 'content': 'isolated ordinary-channel regression'}], 'max_tokens': 32}), {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state['gateway_relay_key']})
    response = connection.getresponse()
    rid = response.getheader('X-Oneapi-Request-Id')
    response.read()
    connection.close()
    assert response.status == 200
    deadline = time.monotonic() + 90
    while True:
        after = snapshot()
        logs = [r for r in after['logs'] if r['type'] == 2 and r['request_id'] == rid]
        if logs:
            break
        assert time.monotonic() < deadline
        time.sleep(1)
    assert len(logs) == 1 and logs[0]['quota'] == 890
    assert before['user']['quota'] - after['user']['quota'] == before['token']['remain_quota'] - after['token']['remain_quota'] == 890
    assert after['decisions'] == before['decisions']
    save('ordinary-with-review-enabled.json', {'request_id': rid, 'status': 200, 'wallet_delta': 890, 'token_delta': 890, 'unique_log': logs[0], 'all_pool_channels_disabled': True, 'review_decisions_unchanged': True})
    print('Ordinary API still settles once with pool channels disabled and finance review enabled')

elif args.phase == 'close':
    user = call(sub, 'POST', '/auth/login', {'email': 'clearapi-executor@example.invalid', 'password': state['sub_user_password']})['access_token']
    for item in state['keys'].values():
        call(sub, 'PUT', '/keys/' + str(item['id']), {'status': 'inactive'}, user)
    for aid in state['accounts'].values():
        call(sub, 'POST', '/admin/accounts/' + str(aid) + '/schedulable', {'schedulable': False}, admin)
    for cid in range(1, 6):
        call(gw, 'POST', '/api/channel/' + str(cid) + '/status', {'status': 2}, owner)
    final = snapshot()
    net = 100000000 - final['user']['quota']
    gross = sum(r['quota'] for r in final['logs'] if r['type'] == 2)
    adjustments = sum(r['refund_quota'] for r in final['decisions'])
    assert net == final['token']['used_quota'] == sum(r['applied'] for r in final['reservations']) == gross - adjustments
    assert len({r['request_id'] for r in final['logs'] if r['type'] == 2}) == len([r for r in final['logs'] if r['type'] == 2])
    assert all(r['state'] == 'delivered' for r in final['outbox'])
    pending = call(gw, 'GET', finance + '?scope=pending&page_size=100', token=owner)
    save('pending-final.json', pending)
    save('final.json', {'net_wallet': net, 'gross_consume_logs': gross, 'decision_refunds': adjustments, 'all_amounts_reconcile': True, 'final_snapshot': final, 'sub2_usage': sub_usage(), 'closing_state': {'all_13_keys_inactive': True, 'both_mock_accounts_unschedulable': True, 'all_five_test_channels_disabled': True}})
    print('Final net', net, '= gross', gross, '- decision refunds', adjustments)
