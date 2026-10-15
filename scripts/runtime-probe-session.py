#!/usr/bin/env python3
"""Install a newly issued official root-login response from a private stdin pipe.

No login bypass, token signing, browser extraction or refresh-token persistence.
The operator completes normal login/MFA first. Never paste the response in chat.
"""
import base64
import getpass
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time
import urllib.request

TARGET = Path('/opt/clearapi/secrets/runtime-probe-session')


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def encrypt_password(password, public_key):
    # Use the server-supported RSA-OAEP/SHA256 contract through OpenSSL; do not
    # implement cryptography or read signing keys. Only a public key hits disk.
    with tempfile.TemporaryDirectory(prefix='runtime-probe-public-key-') as directory:
        path = Path(directory) / 'public.pem'
        path.write_text(public_key, encoding='ascii')
        result = subprocess.run(['openssl', 'pkeyutl', '-encrypt', '-pubin', '-inkey', str(path),
            '-pkeyopt', 'rsa_padding_mode:oaep', '-pkeyopt', 'rsa_oaep_md:sha256',
            '-pkeyopt', 'rsa_mgf1_md:sha256'], input=password.encode(),
            capture_output=True, timeout=15, check=True)
    return base64.b64encode(result.stdout).decode('ascii')


def interactive_login(opener, user_input=input, secret_input=getpass.getpass, encrypt=encrypt_password):
    def request(path, payload=None):
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request('http://127.0.0.1:3000/api/user/' + path, data=data,
            headers={'Content-Type': 'application/json', 'Cache-Control': 'no-store',
                     'User-Agent': 'ClearAPI-Runtime-Probe-Operator'})
        with opener.open(req, timeout=15) as response:
            raw = response.read(65537)
            if response.status != 200 or len(raw) > 65536:
                raise ValueError('official login unavailable')
        body = json.loads(raw)
        if body.get('success') is not True:
            raise ValueError('official login rejected; no automatic retry')
        return body
    key = request('login/encryption-key')['data']
    username = user_input('Root dashboard username: ')
    password = secret_input('Password (not echoed): ')
    if key.get('enabled') is True:
        payload = {'username': username, 'password_encrypted': encrypt(password, key['public_key']),
                   'encryption_key_id': key['kid']}
    elif key.get('enabled') is False:
        # Only the independently inspected local Gateway is reachable here.
        payload = {'username': username, 'password': password}
    else:
        raise ValueError('unknown official password transport policy')
    body = request('login', payload)
    data = body.get('data', {})
    if data.get('require_verification') is True:
        if not any(m.get('method') == '2fa' and m.get('available') is True for m in data.get('methods', [])):
            raise ValueError('required login proof needs an existing interactive client; no bypass')
        body = request('login/verify', {'flow_token': data['flow_token'], 'method': '2fa',
            'code': secret_input('Required 2FA code (not echoed): ')})
    return json.dumps(body).encode()


def live_report(token, container, binary, opener):
    request = urllib.request.Request('http://127.0.0.1:3000/api/clearapi/runtime-integrity',
        headers={'Authorization': 'Bearer ' + token, 'Cache-Control': 'no-store'})
    with opener.open(request, timeout=10) as response:
        if response.status != 200:
            raise ValueError('root probe rejected')
        body = json.loads(response.read(65537))
    report = body.get('data', {})
    if (body.get('success') is not True or report.get('status') != 'PASS'
            or report.get('schema_version') != '1' or report.get('report_version') != '1'
            or report.get('container_id') != container['Id']
            or report.get('started_at') != container['State']['StartedAt']
            or report.get('build_identity') != 'sha256:' + binary):
        raise ValueError('probe identity not bound to current Gateway')
    return report


def login_access(raw, now):
    body = json.loads(raw)
    data = body.get('data', {})
    token = data.get('access_token', '')
    if (body.get('success') is not True or data.get('token_type', '').lower() != 'bearer'
            or not re.fullmatch(r'[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', token)
            or len(token) > 16384 or type(data.get('access_expires_at')) is not int
            or data['access_expires_at'] <= now + 60):
        raise ValueError('fresh completed login required')
    payload = token.split('.')[1]
    claims = json.loads(base64.urlsafe_b64decode(payload + '=' * (-len(payload) % 4)))
    # This is an expiry precheck, never signature validation. The official live
    # endpoint below verifies the signature, live session, auth version and root.
    if claims.get('exp') != data['access_expires_at']:
        raise ValueError('login expiry mismatch')
    return token


def install(path, token):
    for p in path.parents:
        s = p.lstat()
        if (not stat.S_ISDIR(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                or (s.st_mode & 0o022 and not s.st_mode & stat.S_ISVTX)):
            raise ValueError('unsafe session directory')
    if path.exists() or path.is_symlink():
        s = path.lstat()
        if (not stat.S_ISREG(s.st_mode) or s.st_uid != 0 or s.st_gid != 0
                or stat.S_IMODE(s.st_mode) != 0o600 or s.st_nlink != 1):
            raise ValueError('unsafe existing session')
    fd, temporary = tempfile.mkstemp(prefix='.runtime-probe-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as f:
            f.write(token.encode('ascii') + b'\n')
            f.flush()
            os.fsync(f.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    if os.geteuid() != 0 or sys.argv[1:] not in (['--login-response-stdin'], ['--login']):
        raise ValueError('root and private official login-response stdin required')
    def docker(*args):
        return subprocess.run(['docker', *args], check=True, capture_output=True,
                              text=True, timeout=15).stdout.strip()
    c = json.loads(docker('inspect', 'clearapi-new-api-1'))[0]
    if (not c['State']['Running'] or c['State'].get('Restarting')
            or docker('port', c['Id'], '3000/tcp') != '127.0.0.1:3000'):
        raise ValueError('local probe not bound to Gateway')
    binary = docker('exec', c['Id'], 'sha256sum', '/proc/1/exe').split()[0]
    if not re.fullmatch(r'[a-f0-9]{64}', binary):
        raise ValueError('invalid running binary identity')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    if sys.argv[1:] == ['--login']:
        if not sys.stdin.isatty():
            raise ValueError('operator login requires a private terminal')
        raw = interactive_login(opener)
    else:
        raw = sys.stdin.buffer.read(65537)
    if len(raw) > 65536:
        raise ValueError('oversized login response')
    token = login_access(raw, time.time())
    live_report(token, c, binary, opener)
    after = json.loads(docker('inspect', c['Id']))[0]
    if (c['Id'] != after['Id'] or c['Image'] != after['Image']
            or c['State']['StartedAt'] != after['State']['StartedAt']
            or not after['State']['Running'] or after['State'].get('Restarting')):
        raise ValueError('Gateway changed during session validation')
    install(TARGET, token)
    print('Root Runtime probe session installed; access expires normally; run V2 --verify')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        print('OPERATOR_ACTION_REQUIRED: complete official root login/MFA and supply its fresh response through private stdin; session not replaced', file=sys.stderr)
        sys.exit(1)
