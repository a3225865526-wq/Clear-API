"""Build/verify a portable offline package; does not connect to any VPS."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

HERE = Path(__file__).resolve().parent


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['build', 'verify'])
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    target = args.directory.resolve()
    if args.action == 'build':
        target.mkdir(parents=True, exist_ok=False)
        for source in HERE.iterdir():
            if source.is_file():
                shutil.copy2(source, target / source.name)
        lock = json.loads((HERE / 'images.lock.json').read_text())
        for tag, expected in lock.items():
            actual = json.loads(subprocess.check_output(['docker', 'image', 'inspect', tag]))[0]
            if actual['Id'] != expected['engine_id']:
                raise SystemExit('image identity mismatch: ' + tag)
        subprocess.run(['docker', 'save', '-o', str(target / 'images.tar'), *lock], check=True)
        repo = HERE.parents[1]
        subprocess.run(['git', '-C', str(repo), 'archive', '--format=tar',
                        '-o', str(target / 'gateway-source.tar'),
                        '92edfc345d9b9526379ed25363bb79abf9839874'], check=True)
        manifest = {p.name: sha(p) for p in target.iterdir() if p.is_file()}
        (target / 'SHA256.json').write_text(json.dumps(manifest, indent=2) + '\n')
    manifest = json.loads((target / 'SHA256.json').read_text())
    for name, expected in manifest.items():
        path = (target / name).resolve()
        if path.parent != target or sha(path) != expected:
            raise SystemExit('package checksum mismatch: ' + name)
    print('PASS: ' + str(len(manifest)) + ' package files verified')


if __name__ == '__main__':
    main()
