"""Build/verify a portable offline package; does not connect to any VPS."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile

HERE = Path(__file__).resolve().parent
PACKAGE_FILES = {
    '.gitattributes', '.gitignore', 'compose.yaml', 'nginx.conf', 'images.lock.json',
    'production.env.example', 'channel.disabled.plan.json', 'provider-plan.historical.json',
    'package.py', 'backup.sh', 'logrotate.conf', 'OPERATIONS.md', 'MIGRATION.md',
    'target.server.example.json', 'target.windows.example.json', 'stack.sudoers.example',
    'install-stack-target.sh',
    'SIX_SERVICE_RUNBOOK.md',
}
HELPER_FILES = {'deploy-local.ps1', 'local-deploy-stack.ps1', 'local-deploy-vps.py',
                'local-deploy-stack.py', 'local-deploy-state.py'}


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['build', 'verify', 'bundle'])
    parser.add_argument('directory', type=Path)
    parser.add_argument('--image-archive', type=Path)
    parser.add_argument('--image-archive-sha256')
    args = parser.parse_args()
    target = args.directory.resolve()
    if args.action == 'build':
        repo = HERE.parents[1]
        inputs = [str((HERE / name).relative_to(repo)) for name in sorted(PACKAGE_FILES)]
        inputs += ['scripts/' + name for name in sorted(HELPER_FILES)]
        subprocess.run(['git', '-C', str(repo), 'ls-files', '--error-unmatch', '--', *inputs],
                       stdout=subprocess.DEVNULL, check=True)
        if subprocess.run(['git', '-C', str(repo), 'diff', '--quiet', 'HEAD', '--', *inputs]).returncode:
            raise SystemExit('Commit the exact operator inputs before building a release package')
        target.mkdir(parents=True, exist_ok=False)
        # Never enumerate/copy ignored production.env, keys, test DBs or dumps.
        for name in sorted(PACKAGE_FILES):
            shutil.copy2(HERE / name, target / name)
        lock = json.loads((HERE / 'images.lock.json').read_text())
        if args.image_archive:
            if not args.image_archive_sha256 or sha(args.image_archive) != args.image_archive_sha256:
                raise SystemExit('Previously approved offline image archive checksum mismatch')
            shutil.copy2(args.image_archive, target / 'images.tar')
        else:
            for tag, expected in lock.items():
                actual = json.loads(subprocess.check_output(['docker', 'image', 'inspect', tag]))[0]
                if actual['Id'] not in {expected['engine_id'], expected['config_id']}:
                    raise SystemExit('image identity mismatch: ' + tag)
            subprocess.run(['docker', 'save', '-o', str(target / 'images.tar'), *lock], check=True)
        for name in sorted(HELPER_FILES):
            shutil.copy2(repo / 'scripts' / name, target / name)
        with tarfile.open(target / 'images.tar') as images:
            found = set()
            for item in json.load(images.extractfile('manifest.json')):
                raw = images.extractfile(item['Config']).read()
                config = json.loads(raw)
                config_id = 'sha256:' + hashlib.sha256(raw).hexdigest()
                if config['os'] != 'linux' or config['architecture'] != 'amd64':
                    raise SystemExit('Wrong image platform in offline archive')
                for tag in item['RepoTags']:
                    if tag not in lock or lock[tag]['config_id'] != config_id:
                        raise SystemExit('Offline image inventory/config identity mismatch')
                    found.add(tag)
                    lock[tag]['config_id'] = config_id
            if found != set(lock):
                raise SystemExit('Missing offline image')
        (target / 'images.lock.json').write_text(json.dumps(lock, indent=2) + '\n')
        subprocess.run(['git', '-C', str(repo), 'archive', '--format=tar',
                        '-o', str(target / 'gateway-source.tar'),
                        '92edfc345d9b9526379ed25363bb79abf9839874'], check=True)
        operator_commit = subprocess.check_output(['git', '-C', str(repo), 'rev-parse', 'HEAD'], text=True).strip()
        (target / 'operator-source.json').write_text(json.dumps({
            'operator_commit': operator_commit,
            'gateway_commit': '92edfc345d9b9526379ed25363bb79abf9839874',
            'sub2api_commit': 'd851a38477597157e78450d30af2d8504764d085',
            'helper_sha256': {name: sha(target / name) for name in sorted(HELPER_FILES)},
        }, indent=2) + '\n')
        manifest = {p.name: sha(p) for p in target.iterdir() if p.is_file()}
        (target / 'SHA256.json').write_text(json.dumps(manifest, indent=2) + '\n')
    manifest = json.loads((target / 'SHA256.json').read_text())
    required = PACKAGE_FILES | HELPER_FILES | {'images.tar', 'gateway-source.tar', 'operator-source.json'}
    if set(manifest) != required or set(p.name for p in target.iterdir()) != required | {'SHA256.json'}:
        raise SystemExit('Unexpected/missing package content; runtime credentials are never release inputs')
    for name, expected in manifest.items():
        path = (target / name).resolve()
        if path.parent != target or sha(path) != expected:
            raise SystemExit('package checksum mismatch: ' + name)
    print('PASS: ' + str(len(manifest)) + ' package files verified')
    if args.action == 'bundle':
        bundle = target.with_suffix('.tar')
        if bundle.exists():
            raise SystemExit('Refusing to overwrite an existing immutable release archive')
        with tarfile.open(bundle, 'w:') as archive:
            for name in sorted(list(manifest) + ['SHA256.json']):
                archive.add(target / name, arcname=name, recursive=False)
        print(json.dumps({'path': str(bundle), 'sha256': sha(bundle), 'bytes': bundle.stat().st_size}))


if __name__ == '__main__':
    main()
