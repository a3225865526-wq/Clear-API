"""Run on a generated throwaway Git checkout layout only; no GitHub calls."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SRC=Path(__file__).resolve().parents[1]/'scripts'/'prepare-local-cutover.py'
spec=importlib.util.spec_from_file_location('prepare_local_cutover',SRC)
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class CutoverPreparation(unittest.TestCase):
    def test_exact_trigger_disabling_changes_only_selected_files(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)
            folder=root/'.github/workflows'
            folder.mkdir(parents=True)
            originals={
              'automatic-deploy.yml':'name: Automatic production release\non:\n  workflow_run:\n    workflows: [Deployment rehearsal]\n    branches: [main]\n    types: [completed]\njobs:\n  deploy:\n    if: >-\n      something &&\n      another\n    uses: ./.github/workflows/deploy-production.yml\n',
              'failed-production-recovery.yml':"on:\n  workflow_dispatch:\njobs:\n  rehearse:\n    if: inputs.mode == 'rehearse'\n  recover:\n    if: inputs.mode == 'recover' || inputs.mode == 'restore'\n",
              'ghcr-pull-rehearsal.yml':'on:\n  workflow_dispatch:\njobs:\n  release_audit:\n    name: read-only\n  rehearse:\n    if: ${{ !inputs.release_audit_only }}\n',
              'deploy-production.yml':'on:\n  workflow_dispatch:\njobs:\n  authorize:\n    name: check\n'
            }
            for name, content in originals.items(): (folder/name).write_text(content)
            with patch.object(m,'ROOT',root):
                result=m.changes()
            self.assertEqual(set(result),set('.github/workflows/'+p for p in originals))
            self.assertIn('if: ${{ false }}',result['.github/workflows/automatic-deploy.yml'][1])
            self.assertIn("if: inputs.mode == 'rehearse'",result['.github/workflows/failed-production-recovery.yml'][1])
            self.assertIn('  release_audit:\n',result['.github/workflows/ghcr-pull-rehearsal.yml'][1])
            self.assertTrue(all('if: ${{ false }}' in new for old,new in result.values()))
            for name,content in originals.items():
                self.assertEqual((folder/name).read_text(),content)  # preview made no writes

    def test_unknown_workflow_layout_is_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)
            (root/'.github/workflows').mkdir(parents=True)
            for name in ('automatic-deploy.yml','failed-production-recovery.yml',
                         'ghcr-pull-rehearsal.yml','deploy-production.yml'):
                (root/'.github/workflows'/name).write_text('new layout\n')
            with patch.object(m,'ROOT',root):
                with self.assertRaises(ValueError): m.changes()

if __name__=='__main__':unittest.main()
