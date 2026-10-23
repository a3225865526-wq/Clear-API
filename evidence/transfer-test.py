"""Real binary SSH transport test, restricted to the owned loopback test host."""
import hashlib
import json
from pathlib import Path
import subprocess
import tarfile

root = Path(__file__).resolve().parent
port = subprocess.check_output([
    'docker', 'port', 'clearapi-local-deploy-test-20261023', '22'
], text=True).strip()
assert port.startswith('127.0.0.1:')
port = port.rsplit(':', 1)[1]
archive = root / 'clearapi-linux-image.tar'
with tarfile.open(archive) as tar:
    manifest = json.load(tar.extractfile('manifest.json'))
    assert len(manifest) == 1
    raw = tar.extractfile(manifest[0]['Config']).read()
    config = json.loads(raw)
    config_id = 'sha256:' + hashlib.sha256(raw).hexdigest()
assert config['os'] == 'linux' and config['architecture'] == 'amd64'
assert config['config']['Labels']['org.opencontainers.image.revision'] == 'a032815cb6244a7ee5e812de82f8300c02554e99'
with archive.open('rb') as stream:
    checksum = hashlib.file_digest(stream, 'sha256').hexdigest()
ssh = ['ssh', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
       '-o', 'IdentitiesOnly=yes', '-o', 'UserKnownHostsFile=' + str(root / 'test_known_hosts'),
       '-i', str(root / 'test_ed25519'), '-p', port, 'root@127.0.0.1']
command = (
    'set -eu; umask 077; f=$(mktemp /tmp/clearapi-image.XXXXXX); '
    'trap \'rm -f "$f"\' EXIT; cat > "$f"; '
    f'echo "{checksum}  $f" | sha256sum -c -; '
    'docker load -i "$f"; '
    f'docker image inspect --format "{{{{.Id}}}}" {config_id}'
)
with archive.open('rb') as stream:
    result = subprocess.run(ssh + [command], stdin=stream, capture_output=True, text=True)
if result.returncode:
    print(result.stdout)
    print(result.stderr)
    raise RuntimeError('SSH load/identity verification failed')
assert result.stdout.splitlines()[-1] == config_id
loaded = json.loads(subprocess.check_output(ssh + [
    'docker image inspect ' + config_id
], text=True))[0]
assert loaded['RootFS']['Layers'] == config['rootfs']['diff_ids']
assert loaded['Config']['Labels']['org.opencontainers.image.revision'] == config['config']['Labels']['org.opencontainers.image.revision']
report = {'result': 'PASS', 'endpoint': '127.0.0.1:' + port,
          'archive_bytes': archive.stat().st_size, 'archive_sha256': checksum,
          'config_id': config_id, 'architecture': config['architecture'],
          'revision': config['config']['Labels']['org.opencontainers.image.revision'],
          'loaded_repo_digests': loaded.get('RepoDigests'),
          'scope': 'real save -> pinned SSH -> checksum -> load; no production apply'}
(root / 'transfer-result.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
