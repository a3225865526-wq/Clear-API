param([string]$Repository, [string]$Connection, [string]$Command, [string]$Archive = '')
$ErrorActionPreference = 'Stop'
# Load the shipping transport functions without invoking its build/deploy main.
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $Repository 'scripts/deploy-local.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
foreach ($name in @('Quote-ProcessArgument', 'Send-ArchiveOverSsh')) {
    $definition = $ast.Find({ param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name
    }, $true)
    . ([scriptblock]::Create($definition.Extent.Text))
}
$cfg = Get-Content -LiteralPath $Connection -Raw | ConvertFrom-Json
if ($cfg.host -cne '127.0.0.1' -or $cfg.testTarget -ne $true) { throw 'Loopback test only' }
$arguments = @('-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','IdentitiesOnly=yes',
    '-o',"UserKnownHostsFile=$($cfg.knownHostsFile)",'-i',$cfg.identityFile,
    '-p',[string]$cfg.port,'root@127.0.0.1',$Command)
try {
    if ($Archive) { Send-ArchiveOverSsh $arguments $Archive }
    else { & ssh @arguments; if ($LASTEXITCODE) { exit $LASTEXITCODE } }
} catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }
