#!/usr/bin/env python3
"""Durable public ingress isolation for an authenticated controlled release.

Called only by Deploy V2 while holding the production deployment lock. There is
deliberately no public-release entry point: owner acceptance and a separately
reviewed opening procedure are required before restoring the recorded edge.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys


spec = importlib.util.spec_from_file_location('lifecycle', Path(__file__).with_name('pg03-legacy-transition.py'))
lifecycle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lifecycle)


class TrafficHold:
    def __init__(self, app=Path('/opt/clearapi/app'), command=None):
        self.target = app / 'pg03-target-manifest.json'
        self.state = app / 'controlled-launch-state.json'
        self.command = command or self.production_command

    @staticmethod
    def production_command(args):
        result = subprocess.run(args, capture_output=True, text=True, timeout=180)
        if result.returncode:
            raise ValueError('traffic isolation command failed')
        return result.stdout.strip()

    def edge(self):
        containers = json.loads(self.command(['docker', 'inspect', 'clearapi-edge-caddy']))
        if len(containers) != 1:
            raise ValueError('one exact edge is required')
        c = containers[0]
        return {'id': c['Id'], 'image': c['Image'], 'restart_policy': c['HostConfig']['RestartPolicy'],
                'running': c['State']['Running'], 'paused': c['State']['Paused']}

    def binding(self, run, digest, commit):
        target = lifecycle.read_json(self.target, private=False)
        gateway = target['components']['gateway']
        authorization = target['validation']['controlled_authorization']
        if (target.get('launch_stage') != 'controlled' or target.get('public_traffic') != 'blocked'
                or target.get('pool_mode') != 'pg03' or target.get('legacy_transition')
                or authorization.get('mode') != 'controlled-proof-v1'
                or authorization.get('run') != run or target['validation'].get('approval_run') != run
                or not re.fullmatch(r'[1-9][0-9]*', run)
                or not re.fullmatch(r'[1-9][0-9]*', authorization.get('rehearsal_run', ''))
                or gateway['image_digest'] != digest or gateway['oci_revision'] != commit):
            raise ValueError('traffic isolation lacks exact controlled release authority')
        return {'run': run, 'digest': digest, 'commit': commit,
                'target_sha256': hashlib.sha256(lifecycle.read_file(self.target, private=False)).hexdigest()}

    def verify(self, binding):
        state = lifecycle.read_json(self.state)
        if (state.get('schema') != 'clearapi.controlled-traffic/v1' or state.get('binding') != binding
                or state.get('stage') != 'isolated'):
            raise ValueError('traffic isolation journal mismatch')
        edge = self.edge()
        if (edge != dict(state['edge'], running=False, paused=False,
                         restart_policy={'Name': 'no', 'MaximumRetryCount': 0})):
            raise ValueError('public edge is not durably stopped')
        # Prove the application's only host endpoint remains on loopback.
        if self.command(['docker', 'port', 'clearapi-new-api-1', '3000/tcp']) != '127.0.0.1:3000':
            raise ValueError('Gateway is exposed outside loopback')
        for line in self.command(['ss', '-lntuH']).splitlines():
            fields = line.split()
            if len(fields) < 6:
                raise ValueError('cannot establish public listener isolation')
            if re.search(r':(?:80|443)$', fields[4]):
                raise ValueError('a public web listener remains active')
        return state

    def hold(self, binding):
        if self.state.exists() or self.state.is_symlink():
            state = lifecycle.read_json(self.state)
            if state.get('binding') != binding or state.get('stage') not in ('isolating', 'isolated'):
                raise ValueError('another controlled release requires operator review')
            if state['stage'] == 'isolated':
                return self.verify(binding)
            edge = self.edge()
            if edge['id'] != state['edge']['id'] or edge['image'] != state['edge']['image']:
                raise ValueError('edge changed during containment')
        else:
            edge = self.edge()
            if not edge['running'] or edge['paused'] or edge['restart_policy'].get('Name') != 'unless-stopped':
                raise ValueError('unexpected initial edge state')
            state = {'schema': 'clearapi.controlled-traffic/v1', 'binding': binding,
                     'stage': 'isolating', 'edge': edge}
            # The fsynced latch precedes every mutation. An interrupted hold can
            # only continue isolating the same edge; it can never reopen it.
            lifecycle.publish(self.state, state)
        self.command(['docker', 'update', '--restart=no', state['edge']['id']])
        self.command(['docker', 'stop', '--time', '150', state['edge']['id']])
        state['stage'] = 'isolated'
        lifecycle.publish(self.state, state, exclusive=False)
        return self.verify(binding)


def main():
    if os.geteuid() != 0 or len(sys.argv) != 5 or sys.argv[1] not in ('hold', 'check'):
        raise ValueError('root hold/check entry point required')
    mode, run, digest, commit = sys.argv[1:]
    hold = TrafficHold()
    binding = hold.binding(run, digest, commit)
    if mode == 'hold':
        hold.hold(binding)
    else:
        hold.verify(binding)
    print('Public ingress isolated; Provider acceptance remains deferred')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # Never echo subprocess output or arbitrary file data.
        sys.exit('CONTROLLED TRAFFIC BLOCKED: preserve journal and stopped edge; operator review required')
