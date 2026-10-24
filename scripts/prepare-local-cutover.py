#!/usr/bin/env python3
"""Prepare exact local-only Actions retirement changes; NEVER pushes to GitHub.

This must NOT be run with --apply until the user separately authorizes cutover,
and GitHub Actions has been disabled through the GitHub UI first.
"""
import argparse
import difflib
from pathlib import Path
import re
import sys

ROOT=Path(__file__).resolve().parent.parent


def single_replace(data, old, new, path):
    if data.count(old) != 1:
        raise ValueError(f'{path}: expected one exact legacy trigger, found {data.count(old)}')
    return data.replace(old,new,1)


def changes():
    out={}
    path='.github/workflows/automatic-deploy.yml'
    orig=(ROOT/path).read_text(encoding='utf-8')
    next_=single_replace(orig, 'on:\n  workflow_run:\n    workflows: [Deployment rehearsal]\n    branches: [main]\n    types: [completed]\n', 'on:\n  workflow_dispatch:\n',path)
    next_, num=re.subn(r'(?ms)(^  deploy:\n)    if: >-\n.*?(?=    uses:)',
                       lambda m:m.group(1)+"    if: ${{ false }} # retired local cutover\n",next_)
    if num != 1: raise ValueError(f'{path}: cannot find deploy job predicate')
    out[path]=(orig,next_)

    path='.github/workflows/deploy-production.yml'
    orig=(ROOT/path).read_text(encoding='utf-8')
    next_=single_replace(orig,'jobs:\n  authorize:\n',
                         'jobs:\n  authorize:\n    if: ${{ false }} # retired local cutover\n',path)
    out[path]=(orig,next_)

    path='.github/workflows/failed-production-recovery.yml'
    orig=(ROOT/path).read_text(encoding='utf-8')
    next_=single_replace(orig,"    if: inputs.mode == 'recover' || inputs.mode == 'restore'\n",
                         '    if: ${{ false }} # retired local cutover\n',path)
    out[path]=(orig,next_)

    path='.github/workflows/ghcr-pull-rehearsal.yml'
    orig=(ROOT/path).read_text(encoding='utf-8')
    next_=single_replace(orig,'    if: ${{ !inputs.release_audit_only }}\n',
                         '    if: ${{ false }} # retired local cutover\n',path)
    out[path]=(orig,next_)
    return out


def main():
    p=argparse.ArgumentParser()
    p.add_argument('--apply', action='store_true',help='local worktree edits only, still no push')
    args=p.parse_args()
    result=changes() # any mismatch blocks ALL changes before writing
    for name,(old,new) in result.items():
        print(''.join(difflib.unified_diff(old.splitlines(True),new.splitlines(True),
                                           fromfile='a/'+name,tofile='b/'+name)),end='')
    if args.apply:
        for name,(old,new) in result.items():
            (ROOT/name).write_text(new,encoding='utf-8',newline='')
        print('LOCAL CUTOVER WORKTREE UPDATED; NO COMMIT OR PUSH EXECUTED')
    else:
        print('PREVIEW ONLY. No local files changed, no workflow or production writes.')

if __name__=='__main__':
    try: main()
    except (ValueError,OSError) as ex:
        print('BLOCKED: '+str(ex),file=sys.stderr)
        sys.exit(2)
