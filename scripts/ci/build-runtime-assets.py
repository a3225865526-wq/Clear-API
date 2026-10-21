#!/usr/bin/env python3
"""Bind a new boot bundle to exact Git tree bytes and a completed RC manifest."""
import argparse
import ast
import hashlib
import json
from pathlib import Path
import re
import subprocess


REPOSITORY = 'a3225865526-wq/Clear-API'
RUNTIME = '/opt/clearapi/runtime/new-api'
SCRIPTS = ('runtime-boot-recovery.py', 'runtime-identity-refresh.py')
TEMPLATE_SHA = '6bf1c77e6ce1c162e22328239badbaf67b77bde21f100fbbcf3a5747812d01d5'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encode(value):
    return (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()


def git(repository, *args):
    return subprocess.run(['git', '-C', str(repository), *args], check=True,
                          capture_output=True).stdout


def build(repository, tree, manifest_path, output):
    require(re.fullmatch('[0-9a-f]{40}', tree), 'exact source tree required')
    require(git(repository, 'cat-file', '-t', tree).strip() == b'tree', 'source is not a Git tree')
    manifest_raw = manifest_path.read_bytes()
    manifest = json.loads(manifest_raw)
    require(manifest.get('pool_mode') == 'off' and manifest.get('platform') == 'linux/amd64',
            'off linux/amd64 RC required')
    require(re.fullmatch('sha256:[0-9a-f]{64}', manifest.get('image_digest', '')),
            'exact image digest required')
    require(manifest.get('source_tree') == tree, 'RC source tree mismatch')
    require(manifest.get('production_ready') is False, 'preproduction RC required')
    commit = manifest.get('source_commit')
    if commit is not None:
        require(re.fullmatch('[0-9a-f]{40}', commit), 'invalid source commit')
        require(git(repository, 'rev-parse', commit + '^{tree}').decode().strip() == tree,
                'source commit/tree mismatch')
        require(manifest.get('source_kind') == 'git-commit', 'source kind mismatch')
    else:
        require(manifest.get('source_kind') == 'git-tree-snapshot', 'uncommitted source must be explicit')
    run = str(manifest.get('workflow_run', ''))
    attempt = manifest.get('workflow_run_attempt')
    require(re.fullmatch('[A-Za-z0-9-]{1,80}', run) and type(attempt) is int and attempt >= 1,
            'RC run identity required')
    require(commit is not None or run.startswith('local-'), 'snapshot cannot claim a CI RC run')
    template = git(repository, 'show', tree + ':scripts/boot-recovery-install.py')
    require(sha(template) == TEMPLATE_SHA, 'unreviewed installer template')
    sources = {name: git(repository, 'show', tree + ':scripts/' + name) for name in SCRIPTS}
    parsed = ast.parse(sources[SCRIPTS[0]])
    definitions = {node.targets[0].id: ast.literal_eval(node.value)
                   for node in parsed.body if isinstance(node, ast.Assign)
                   and isinstance(node.targets[0], ast.Name)
                   and node.targets[0].id in ('ASSETS', 'HELPER_SHA')}
    directory = next(node.value.args[0].value for node in parsed.body
                     if isinstance(node, ast.Assign) and isinstance(node.targets[0], ast.Name)
                     and node.targets[0].id == 'DIRECTORY')
    require(directory == RUNTIME and definitions['HELPER_SHA'] == sha(sources[SCRIPTS[1]]),
            'runtime directory or helper hash mismatch')
    compose = git(repository, 'show', tree + ':docker-compose.production.yml').decode()
    require('source: ' + RUNTIME in compose and 'target: /run/clearapi' in compose
            and 'create_host_path: false' in compose, 'Compose runtime contract mismatch')
    require(definitions['ASSETS']['/etc/tmpfiles.d/clearapi-runtime.conf'] ==
            'd /opt/clearapi/runtime 0755 root root -\n'
            'd ' + RUNTIME + ' 0750 root root -\n', 'tmpfiles runtime contract mismatch')
    assets = []
    for name, raw in sources.items():
        assets.append(dict(kind='source_file', source_path='scripts/' + name,
                           destination_path='/opt/clearapi/bin/' + name, sha256=sha(raw),
                           size_bytes=len(raw)))
    for name, text in definitions['ASSETS'].items():
        raw = text.encode()
        assets.append(dict(kind='embedded_generated_file', source_path='scripts/' + SCRIPTS[0],
                           destination_path=name, sha256=sha(raw), size_bytes=len(raw)))
    source = dict(repository=REPOSITORY, commit=commit, root_tree=tree)
    rc = dict(run_id=run, attempt=attempt, original_rc_contains_this_inventory=False,
              gateway_manifest_sha256=sha(manifest_raw), image_digest=manifest['image_digest'])
    inventory = dict(schema='clearapi.runtime-assets/v2', source=source, rc=rc, pool_mode='off',
                     runtime_directory=RUNTIME, installer_template_sha256=TEMPLATE_SHA, assets=assets)
    raw = encode(inventory)
    rendered = template.decode()
    rendered = rendered.replace("INVENTORY_SHA = 'b25e5a9c3deee05b51c7aaf3d0c74167c0be223929e2f1191d01ebc5492917d2'",
                                'INVENTORY_SHA = ' + repr(sha(raw)))
    rendered = rendered.replace("SOURCE = '69a66431646b5cc8912bf97a739e08de07316443'", 'SOURCE = ' + repr(commit))
    rendered = rendered.replace("TREE = '7231fa53f2a1bb8cebb7ab314f9f038442ac3de6'", 'TREE = ' + repr(tree))
    rendered = rendered.replace('39357150402', run)
    rendered = rendered.replace("'runtime-boot-recovery.py', 'runtime-identity-refresh.py', 'pg03-failed-recovery.py'",
                                "'runtime-boot-recovery.py', 'runtime-identity-refresh.py'")
    rendered = rendered.replace("inventory['rc']['attempt'] == 1", "inventory['rc']['attempt'] == " + str(attempt))
    rendered = rendered.replace("rc_attempt=1", "rc_attempt=" + str(attempt))
    rendered = rendered.replace("baseline['rc_attempt'] == 1", "baseline['rc_attempt'] == " + str(attempt))
    rendered = rendered.replace('_' + run + '_1', '_' + run + '_' + str(attempt))
    rendered = rendered.replace('_RC' + run + '_1', '_RC' + run + '_' + str(attempt))
    rendered = rendered.replace('clearapi.boot-preinstall/v1', 'clearapi.boot-preinstall/v2')
    anchor = "    inventory = decode(raw)\n"
    require(rendered.count(anchor) == 1, 'installer validation anchor changed')
    rendered = rendered.replace(anchor, anchor +
        "    require(inventory['schema'] == 'clearapi.runtime-assets/v2' and inventory['rc'] == " + repr(rc) +
        ", 'RC manifest binding mismatch')\n" +
        "    require(sha((source_dir / 'gateway-image-release.json').read_bytes()) == " + repr(sha(manifest_raw)) +
        ", 'RC manifest identity mismatch')\n")
    compile(rendered, 'candidate-boot-install.py', 'exec')
    output.mkdir(parents=True, exist_ok=False)
    files = {'inventory.json': raw, 'gateway-image-release.json': manifest_raw,
             'boot-recovery-install.py': rendered.encode()}
    files.update({'scripts/' + name: data for name, data in sources.items()})
    for name, data in files.items():
        path = output / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    (output / 'artifact-hashes.json').write_bytes(encode({name: sha(data) for name, data in files.items()}))
    return inventory


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repository', type=Path, default=Path('.'))
    parser.add_argument('--source-tree', required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    build(args.repository, args.source_tree, args.manifest, args.output)
    print('Exact runtime bundle prepared; no production operation authorized')


if __name__ == '__main__':
    main()
