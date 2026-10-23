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
              'release-pg03-gateway.yml':'on:\n  workflow_dispatch:\n  workflow_run:\n    workflows: [CI]\n    branches: [main]\n    types: [completed]\n',
              'deployment-rehearsal.yml':'on:\n  workflow_run:\n    workflows: [PG03 reviewed main Gateway RC]\n    branches: [main]\n    types: [completed]\n  workflow_dispatch:\n',
              'deploy-production.yml':'on:\n  workflow_dispatch:\njobs:\n  authorize:\n    name: check\n'
            }
            for name, content in originals.items(): (folder/name).write_text(content)
            with patch.object(m,'ROOT',root):
                result=m.changes()
            self.assertEqual(set(result),set('.github/workflows/'+p for p in originals))
            self.assertIn('if: ${{ false }}',result['.github/workflows/automatic-deploy.yml'][1])
            self.assertNotIn('workflow_run:',result['.github/workflows/deployment-rehearsal.yml'][1])
            self.assertNotIn('workflow_run:',result['.github/workflows/release-pg03-gateway.yml'][1])
            for name,content in originals.items():
                self.assertEqual((folder/name).read_text(),content)  # preview made no writes

    def test_unknown_workflow_layout_is_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)
            (root/'.github/workflows').mkdir(parents=True)
            for name in ('automatic-deploy.yml','release-pg03-gateway.yml',
                         'deployment-rehearsal.yml','deploy-production.yml'):
                (root/'.github/workflows'/name).write_text('new layout\n')
            with patch.object(m,'ROOT',root):
                with self.assertRaises(ValueError): m.changes()

if __name__=='__main__':unittest.main()
