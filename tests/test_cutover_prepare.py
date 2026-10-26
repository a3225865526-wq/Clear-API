"""Run on a generated throwaway Git checkout layout only; no GitHub calls."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
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

class WindowsClassification(unittest.TestCase):
    def test_operator_only_changes_do_not_block_app_updates_or_weaken_migration_checks(self):
        runtimes=[p for p in (shutil.which('powershell.exe'),shutil.which('pwsh')) if p]
        if not runtimes:self.skipTest('PowerShell runtime unavailable')
        entry=SRC.parent/'deploy-local.ps1'
        script=r'''
$ErrorActionPreference='Stop'
$entry='ENTRY_PATH'
$tokens=$null;$errors=$null
[void][Management.Automation.Language.Parser]::ParseFile($entry,[ref]$tokens,[ref]$errors)
if ($errors.Count) { throw 'Entry AST failed' }
$source=Get-Content -LiteralPath $entry -Raw -Encoding UTF8
$start=$source.IndexOf('    $operatorOnlyPaths =')
$end=$source.IndexOf('    $summary =',$start)
if ($start -lt 0 -or $end -le $start) { throw 'Classifier not found' }
$classification=$source.Substring($start,$end-$start)
function Check-Classification([string[]]$paths,[string]$expected) {
    $changes=$paths;$TestTarget=$false;$dirty=''
    $actual=''
    try { Invoke-Expression $classification; $actual=$scope }
    catch {
        if ($_.Exception.Message -like 'Only metadata/tooling changed*') { $actual='no-app-change' }
        elseif ($_.Exception.Message -like 'Migration/PG03-sensitive paths changed*') { $actual='blocked' }
        else { throw }
    }
    if ($actual -cne $expected) { throw "Expected $expected got $actual" }
}
$operators=@('scripts/pg03-failed-recovery.py','scripts/pg03-release-control.py')
Check-Classification $operators 'no-app-change'
Check-Classification ($operators+@('web/src/routes/index.tsx')) 'frontend'
Check-Classification ($operators+@('controller/public.go')) 'backend'
Check-Classification ($operators+@('model/migrations.go')) 'blocked'
Check-Classification ($operators+@('migrations/change.sql')) 'blocked'
Check-Classification ($operators+@('scripts/pg03-schema-rehearsal.py')) 'blocked'
Check-Classification ($operators+@('cmd/production-bootstrap/main.go')) 'blocked'
Write-Output 'AST and seven deployment-classification contracts PASS; no network/build/deployment'
'''.replace('ENTRY_PATH',str(entry).replace("'","''"))
        for runtime in runtimes:
            with self.subTest(runtime=runtime):
                result=subprocess.run([runtime,'-NoProfile','-NonInteractive','-Command',script],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)


if __name__=='__main__':unittest.main()
