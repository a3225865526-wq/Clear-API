"""Verify the local, empty Sub2API stack without accepting compliance terms.

Requires the pinned upstream image to be built first. Credentials stay in the
explicit external runtime directory; only sanitized observations are exported.
"""
import argparse
import datetime
import hashlib
import http.client
import json
from pathlib import Path
import re
import secrets
import subprocess
import time
import urllib.error
import urllib.request


ROOT = Path(__file__).resolve().parent.parent
PROJECT = 'clearapi-sub2api-isolated-20261030'
BASE = 'http://127.0.0.1:18882'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--runtime', type=Path, required=True)
    args = parser.parse_args()
    runtime = args.runtime.resolve()
    if runtime == ROOT or ROOT in runtime.parents:
        raise SystemExit('Runtime secrets must be outside the worktree')
    runtime.mkdir(parents=True, exist_ok=True)
    env_file = runtime / 'test.env'
    if not env_file.exists():
        names = ['DB_PASSWORD', 'ADMIN_PASSWORD', 'JWT_SECRET', 'TOTP_KEY']
        with env_file.open('x', encoding='utf-8') as f:
            f.write(''.join(f'SUB2_TEST_{name}={secrets.token_hex(32)}\n' for name in names))
        env_file.chmod(0o600)
    env = dict(line.split('=', 1) for line in env_file.read_text().splitlines() if line and not line.startswith('#'))
    compose = ['docker', 'compose', '-p', PROJECT, '--env-file', str(env_file),
               '-f', str(ROOT / 'compose.sub2api-isolated.yml')]
    report = {'time_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'project': PROJECT, 'endpoint': BASE, 'compliance_accept_called': False,
              'production_accessed': False, 'phases': []}
    evidence = ROOT / 'evidence/sub2api-integration-20261030'
    evidence.mkdir(parents=True, exist_ok=True)

    def run(*command):
        result = subprocess.run(command, capture_output=True, text=True, encoding='utf-8')
        if result.returncode:
            # Container startup diagnostics can contain generated credentials.
            (runtime / 'last-command-error.log').write_text(result.stderr, encoding='utf-8')
            raise RuntimeError(f'{command[0]} failed ({result.returncode}); diagnostics kept outside Git')
        return result.stdout.strip()

    def request(path, payload=None, token=None):
        headers = {'Content-Type': 'application/json', 'User-Agent': 'ClearAPI-isolated-verification'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        req = urllib.request.Request(BASE + path, headers=headers,
                                     data=json.dumps(payload).encode() if payload is not None else None)
        try:
            with urllib.request.urlopen(req, timeout=15) as response:
                return response.status, response.headers, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.headers, error.read()

    def observe(phase):
        deadline = time.monotonic() + 55
        while True:
            try:
                code, _, _ = request('/health')
                if code == 200:
                    break
            except (urllib.error.URLError, ConnectionError, http.client.RemoteDisconnected, TimeoutError):
                pass  # Expected while this local instance starts.
            if time.monotonic() >= deadline:
                raise RuntimeError('Local health did not become ready within 55 seconds')
            time.sleep(1)
        code, headers, html = request('/')
        assert code == 200 and 'text/html' in headers.get('Content-Type', '')
        assets = re.findall(rb'<script[^>]+src="([^"]+)"', html)
        assert assets, 'Embedded management frontend missing'
        asset_path = assets[0].decode()
        assert asset_path.startswith('/assets/'), 'Unexpected non-local frontend asset'
        asset_code, asset_headers, asset = request(asset_path)
        assert asset_code == 200 and 'javascript' in asset_headers.get('Content-Type', '')
        code, _, body = request('/api/v1/auth/login', {
            'email': 'sub2-test@example.invalid', 'password': env['SUB2_TEST_ADMIN_PASSWORD']})
        assert code == 200, f'Local admin login returned {code}'
        data = json.loads(body)['data']
        token = data['access_token']
        code, _, body = request('/api/v1/admin/compliance', token=token)
        assert code == 200
        status = json.loads(body)['data']
        blocked = {}
        for path in ['/api/v1/admin/accounts', '/api/v1/admin/groups', '/api/v1/admin/users']:
            code, _, body = request(path, token=token)
            blocked[path] = {'status': code, 'code': json.loads(body).get('code')}
            if status['required']:
                assert code == 423 and blocked[path]['code'] == 'ADMIN_COMPLIANCE_ACK_REQUIRED'
        unauthenticated = {}
        for path in ['/v1/chat/completions', '/v1/responses', '/v1/messages']:
            code, _, _ = request(path, {})
            assert code == 401
            unauthenticated[path] = code
        db = run(*compose, 'exec', '-T', 'postgres', 'psql', '-U', 'sub2test', '-d', 'sub2test', '-Atc',
                 "SELECT current_setting('server_version'); SELECT id || ':' || email FROM users ORDER BY id;")
        files = run(*compose, 'exec', '-T', 'sub2api', 'sh', '-c',
                    'find /app/data -maxdepth 1 -type f -exec sha256sum {} +')
        observation = {'phase': phase, 'health': 200, 'frontend': 200,
                       'frontend_asset_sha256': hashlib.sha256(asset).hexdigest(),
                       'admin_login': 200, 'compliance_required': status['required'],
                       'compliance_version': status['version'],
                       'acknowledgement_present': bool(status.get('acknowledgement')),
                       'management': blocked, 'unauthenticated_protocols': unauthenticated,
                       'database_identity': db, 'app_file_hashes': files}
        report['phases'].append(observation)
        return observation

    try:
        run(*compose, 'up', '-d', '--wait', '--wait-timeout', '55')
        before = observe('initial_start')
        run(*compose, 'exec', '-T', 'redis', 'redis-cli', 'SET', 'clearapi:isolation:persistence', 'fixture-v1')
        run(*compose, 'restart')
        restarted = observe('restart')
        # Recreate containers too: persistent named volumes, not writable layers.
        run(*compose, 'up', '-d', '--force-recreate', '--wait', '--wait-timeout', '55')
        recreated = observe('container_recreation')
        for after in [restarted, recreated]:
            assert after['database_identity'] == before['database_identity']
            assert after['app_file_hashes'] == before['app_file_hashes']
            assert after['frontend_asset_sha256'] == before['frontend_asset_sha256']
        assert run(*compose, 'exec', '-T', 'redis', 'redis-cli', 'GET', 'clearapi:isolation:persistence') == 'fixture-v1'
        run(*compose, 'exec', '-T', 'redis', 'redis-cli', 'DEL', 'clearapi:isolation:persistence')
        report['persistence'] = {'postgres': 'PASS', 'redis_aof': 'PASS', 'app_volume': 'PASS'}
        report['redis_version'] = run(*compose, 'exec', '-T', 'redis', 'redis-server', '--version')
        report['backup_client_version'] = run(*compose, 'exec', '-T', 'sub2api', 'pg_dump', '--version')
        report['idle_stats'] = run('docker', 'stats', '--no-stream', '--format', '{{.Name}} CPU={{.CPUPerc}} MEM={{.MemUsage}}',
                                   *run(*compose, 'ps', '-q').splitlines())
        report['status'] = 'ISOLATION_PASS_MANAGEMENT_BLOCKED' if before['compliance_required'] else 'ISOLATION_PASS'
    finally:
        (evidence / 'isolation.json').write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding='utf-8')
    print(report['status'])


if __name__ == '__main__':
    main()
