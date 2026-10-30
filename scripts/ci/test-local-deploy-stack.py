"""Focused regressions for native readiness and binary SSH upload preservation."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

SOURCE = Path(__file__).resolve().parents[1] / 'local-deploy-stack.py'
spec = importlib.util.spec_from_file_location('stack_target', SOURCE)
stack = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stack)


class StackTransportContracts(unittest.TestCase):
    def test_native_cli_zero_exit_is_not_readiness(self):
        target = stack.StackTarget.__new__(stack.StackTarget)
        target.h = SimpleNamespace(DeployError=RuntimeError)
        for status in ['not_bootstrapped', 'input_error', 'readiness_failed']:
            with self.subTest(status=status):
                target.call = Mock(return_value=json.dumps({'status': status}))
                with self.assertRaisesRegex(RuntimeError, 'not verified'):
                    target.verify_readiness('/unused')
        target.call = Mock(return_value='diagnostic preamble\n{"status":"verified"}')
        target.verify_readiness('/unused')

    def test_readonly_child_does_not_consume_pending_upload(self):
        script = '''
import importlib.util, os, sys
from types import SimpleNamespace
s=importlib.util.spec_from_file_location('stack',sys.argv[1])
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
t=m.StackTarget.__new__(m.StackTarget)
t.h=SimpleNamespace(STATE=SimpleNamespace(compose_environment=lambda:dict(os.environ)))
t.dc=lambda *a,**kw:[sys.executable,'-c','import sys; print(len(sys.stdin.buffer.read()))']
print(t.call('/unused','config'))
print(len(sys.stdin.buffer.read()))
'''
        payload = b'pending-binary-archive\x00\xff' * 100
        result = subprocess.run([sys.executable, '-c', script, str(SOURCE)], input=payload,
                                capture_output=True, check=True)
        self.assertEqual(result.stdout.decode().splitlines(), ['0', str(len(payload))])


if __name__ == '__main__':
    unittest.main()
