#requires -Version 5.1
<#
ClearAPI Windows local build + SSH archive transport + single-service update.
NO GitHub push/RC/Actions. Requires an explicitly cut-over, installed VPS helper.
DryRun NEVER pushes images, changes containers, or writes to the VPS.
#>
[CmdletBinding()]
param(
    [switch]$DryRun,
    [string]$ConfigPath = (Join-Path $PSScriptRoot 'local-deploy.config.json'),
    [string]$FixturePath = '',
    [switch]$TestTarget
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$phase = 'precheck'

function Run-Native {
    param([string]$Command, [string[]]$Arguments)
    $result = & $Command @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed in ${phase}: $Command (exit $LASTEXITCODE)"
    }
    return (($result | Out-String).Trim())
}
function Require-Command([string]$name) {
    if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "Missing dependency: $name" }
}
function Is-Sha([string]$v) { return ($v -cmatch '^sha256:[a-f0-9]{64}$') }
function Is-Commit([string]$v) { return ($v -cmatch '^[a-f0-9]{40}$') }
function Quote-ProcessArgument([string]$value) {
    if ($value -notmatch '[\s"]') { return $value }
    $builder = New-Object System.Text.StringBuilder
    [void]$builder.Append('"')
    $slashes = 0
    foreach ($character in $value.ToCharArray()) {
        if ($character -eq '\') { $slashes++; continue }
        if ($character -eq '"') {
            [void]$builder.Append(('\' * (2 * $slashes + 1)) + '"')
            $slashes = 0
            continue
        }
        if ($slashes) { [void]$builder.Append('\' * $slashes); $slashes = 0 }
        [void]$builder.Append($character)
    }
    if ($slashes) { [void]$builder.Append('\' * (2 * $slashes)) }
    [void]$builder.Append('"')
    return $builder.ToString()
}
function Send-ArchiveOverSsh([string[]]$Arguments, [string]$ArchivePath) {
    $sshPath = (Get-Command ssh -ErrorAction Stop).Source
    $start = New-Object System.Diagnostics.ProcessStartInfo
    $start.FileName = $sshPath
    $start.Arguments = (($Arguments | ForEach-Object { Quote-ProcessArgument $_ }) -join ' ')
    $start.UseShellExecute = $false
    $start.RedirectStandardInput = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $start
    if (-not $process.Start()) { throw 'Unable to start SSH process' }
    $stdout = $process.StandardOutput.ReadToEndAsync()
    $stderr = $process.StandardError.ReadToEndAsync()
    $input = [IO.File]::OpenRead($ArchivePath)
    $inputError = $null
    try { $input.CopyTo($process.StandardInput.BaseStream) }
    catch { $inputError = $_.Exception }
    finally { $input.Dispose(); $process.StandardInput.Close() }
    $process.WaitForExit()
    $output = $stdout.GetAwaiter().GetResult()
    $errorOutput = $stderr.GetAwaiter().GetResult()
    if ($process.ExitCode -ne 0) { throw "SSH failed (exit $($process.ExitCode)): $errorOutput" }
    if ($inputError) { throw "SSH archive input failed: $($inputError.Message)" }
    return $output.Trim()
}

function Get-ImageConfigId([string]$Image) {
    # Desktop's containerd store can report a manifest digest as .Id.
    # Hash the saved config bytes to compare with a classic VPS engine.
    Require-Command 'tar'
    $temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    $folder = Join-Path $temporaryRoot ('clearapi-image-id-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $folder | Out-Null
    try {
        $archive = Join-Path $folder 'image.tar'
        Run-Native 'docker' @('save','--output',$archive,$Image) | Out-Null
        Run-Native 'tar' @('-xf',$archive,'-C',$folder,'manifest.json') | Out-Null
        $manifest = @(Get-Content -LiteralPath (Join-Path $folder 'manifest.json') -Raw | ConvertFrom-Json)
        if ($manifest.Count -ne 1 -or $manifest[0].Config -cnotmatch '^(blobs/sha256/[a-f0-9]{64}|[a-f0-9]{64}\.json)$') {
            throw 'Saved image must contain exactly one valid image configuration'
        }
        $configEntry = [string]$manifest[0].Config
        Run-Native 'tar' @('-xf',$archive,'-C',$folder,$configEntry) | Out-Null
        return 'sha256:' + (Get-FileHash -LiteralPath (Join-Path $folder $configEntry) -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    finally {
        $resolvedFolder = [IO.Path]::GetFullPath($folder)
        if (-not $resolvedFolder.StartsWith($temporaryRoot,[StringComparison]::OrdinalIgnoreCase) -or
            (Split-Path $resolvedFolder -Leaf) -cnotmatch '^clearapi-image-id-[a-f0-9]{32}$') {
            throw 'Refusing cleanup outside the image verification temporary directory'
        }
        Remove-Item -LiteralPath $resolvedFolder -Recurse -Force
    }
}

try {
    Require-Command 'git'
    $cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($TestTarget) {
        if (-not $cfg.testTarget -or $cfg.host -cne '127.0.0.1' -or $cfg.remoteConfigPath -cnotmatch '^/opt/clearapi-test/[A-Za-z0-9._/-]+$') { throw 'TestTarget requires explicit loopback configuration' }
    } else {
        if ($cfg.testTarget) { throw 'Test configuration requires -TestTarget' }
        if ($cfg.host -cne '103.42.181.219' -or $cfg.user -cne 'clearadmin' -or [int]$cfg.port -ne 22) {
            throw 'Unapproved production server identity or port in configuration'
        }
    }
    $configArgument = if ($TestTarget) { ' --config-path ' + [string]$cfg.remoteConfigPath } else { '' }
    if (-not $FixturePath) {
        foreach ($p in @($cfg.identityFile, $cfg.knownHostsFile)) {
            if (-not (Test-Path -LiteralPath $p -PathType Leaf)) { throw "Missing SSH identity/known-hosts file: $p" }
        }
    }
    $gitRoot = Run-Native 'git' @('-C', $repo, 'rev-parse', '--show-toplevel')
    if (([IO.Path]::GetFullPath($gitRoot).TrimEnd('\','/')) -cne ([IO.Path]::GetFullPath($repo).TrimEnd('\','/'))) {
        throw 'Not at the intended repository root'
    }
    $remoteUrls = Run-Native 'git' @('-C',$repo,'config','--get-regexp','^remote\..*\.url$')
    if (-not ($remoteUrls -split "`n" | Where-Object {
        $_ -match '(?i)\s(?:https://github\.com/|git@github\.com:)a3225865526-wq/Clear-API(?:\.git)?$'
    })) {
        throw 'No verified Clear-API Git remote (origin or production)'
    }
    $commit = Run-Native 'git' @('-C',$repo,'rev-parse','HEAD')
    if (-not (Is-Commit $commit)) { throw 'Invalid source commit' }
    $dirty = Run-Native 'git' @('-C',$repo,'status','--porcelain','--untracked-files=normal')
    if ($dirty -and -not $TestTarget) { throw 'Git worktree is dirty; commit or move local changes before deployment' }
    if ($FixturePath -and -not $DryRun) { throw 'FixturePath is only valid with -DryRun' }

    if ($FixturePath) {
        $remote = Get-Content -LiteralPath $FixturePath -Raw -Encoding UTF8 | ConvertFrom-Json
    } else {
        Require-Command 'ssh'
        $sshOptions = @('-o','BatchMode=yes','-o','StrictHostKeyChecking=yes',
            '-o','IdentitiesOnly=yes','-o',"UserKnownHostsFile=$($cfg.knownHostsFile)",
            '-i', [string]$cfg.identityFile, '-p', [string]$cfg.port)
        $phase = 'read-only VPS preflight'
        $preflightTarget = if ($TestTarget) { "$($cfg.user)@$($cfg.host)" } else { 'clearadmin@103.42.181.219' }
        $raw = Run-Native 'ssh' ($sshOptions + @($preflightTarget,
            ('sudo -n /opt/clearapi/bin/clearapi-local-deploy.py' + $configArgument + ' preflight')))
        $remote = $raw | ConvertFrom-Json
    }
    if ($remote.healthy -ne $true -or $remote.service -cne 'new-api' -or
        -not (Is-Sha ([string]$remote.image_id)) -or -not (Is-Commit ([string]$remote.commit))) {
        throw 'Remote predecessor identity or health has not been verified'
    }
    $arch = [string]$remote.arch
    if ($arch -cnotin @('amd64','arm64')) { throw 'Unsupported or unknown VPS CPU architecture' }
    if ($remote.commit -ceq $commit -and -not $TestTarget) { throw 'This exact source commit is already deployed' }
    $phase = 'source diff classification'
    Run-Native 'git' @('-C',$repo,'cat-file','-e',"$($remote.commit)^{commit}") | Out-Null
    $changedRaw = Run-Native 'git' @('-C',$repo,'diff','--name-only',"$($remote.commit)..$commit",'--')
    $changes = @($changedRaw -split "`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    if ($changes.Count -eq 0) { throw 'No source changes to deploy' }
    $risky = @($changes | Where-Object {
        $_ -match '(^|/)(migrations?|schema|pg03|accountpool|routechain)(/|\.|$)' -or
        $_ -match '^cmd/(finance-migrate|billing-migrate|legacy-store-retire|production-bootstrap)/' -or
        $_ -match '^scripts/(production-entrypoint|production-migration-entrypoint|pg03-)' -or
        $_ -match '\.sql$'
    })
    if ($risky.Count -gt 0 -and -not $TestTarget) { throw "Migration/PG03-sensitive paths changed. Separate reviewed deployment required: $($risky -join ', ')" }
    $appChanges = @($changes | Where-Object {
        $_ -match '^web/' -or $_ -match '^docs/clearapi/' -or $_ -ceq 'VERSION' -or
        $_ -match '\.go$' -or $_ -match '^(go\.mod|go\.sum|Dockerfile|relaykit/)' -or
        $_ -match '^scripts/production-entrypoint\.sh$'
    })
    if ($appChanges.Count -eq 0 -and $TestTarget -and $dirty) { $appChanges = @('isolated-test-working-tree') }
    if ($appChanges.Count -eq 0) { throw 'Only metadata/tooling changed; no Gateway build needed' }
    $backendChanges = @($appChanges | Where-Object {
        $_ -notmatch '^web/' -and $_ -notmatch '^docs/clearapi/' -and $_ -cne 'VERSION'
    })
    $scope = if ($backendChanges.Count -eq 0) { 'frontend' } else { 'backend' }
    $summary = [ordered]@{ commit=$commit; previous_commit=[string]$remote.commit
        architecture=$arch; service='new-api'; scope=$scope; changed_files=$changes.Count
        estimated_backup=($scope -eq 'backend') }
    if ($DryRun) {
        $summary['result']='DRY_RUN_NO_PRODUCTION_WRITES'
        $summary['note']='No image built, no SSH writes, no registry access and no production writes.'
        $summary | ConvertTo-Json -Depth 5
        exit 0
    }

    Require-Command 'docker'
    $phase = 'Docker Buildx prerequisites'
    Run-Native 'docker' @('info','--format','{{.OSType}}') | ForEach-Object {
        if ($_ -cne 'linux') { throw 'Docker Desktop must run Linux containers' }
    }
    Run-Native 'docker' @('buildx','version') | Out-Null
    $tag = "clearapi-local:$commit"
    $phase = 'local Linux image build'
    Run-Native 'docker' @('buildx','build','--platform',"linux/$arch",'--load',
        '--tag',$tag,'--file',(Join-Path $repo 'Dockerfile'),
        '--build-arg',"SOURCE_COMMIT=$commit",
        '--build-arg','SOURCE_REPO=https://github.com/a3225865526-wq/Clear-API',
        '--build-arg',"BUILD_TIMESTAMP=$([DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ'))",
        $repo) | Out-Null
    $imageId = Get-ImageConfigId $tag
    $actualArch = Run-Native 'docker' @('image','inspect','--format','{{.Architecture}}',$tag)
    $actualRevision = Run-Native 'docker' @('image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',$tag)
    if (-not (Is-Sha $imageId) -or $actualArch -cne $arch -or $actualRevision -cne $commit) {
        throw 'Built image architecture, ID, or revision does not match expected release'
    }
    $phase = 'save and checksum local image archive'
    $archive = Join-Path ([IO.Path]::GetTempPath()) ("clearapi-$commit-" + [guid]::NewGuid().ToString('N') + '.tar')
    Run-Native 'docker' @('save','--output',$archive,$tag) | Out-Null
    $archiveHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    $archiveBytes = (Get-Item -LiteralPath $archive).Length
    $phase = 'stream image archive to controlled VPS helper'
    $remoteCommand = 'sudo -n /opt/clearapi/bin/clearapi-local-deploy.py' + $configArgument + ' receive' +
        " --commit $commit --image-id $imageId --archive-sha256 $archiveHash --archive-bytes $archiveBytes" +
        " --previous-image-id $($remote.image_id) --scope $scope"
    $sshTarget = if ($TestTarget) { "$($cfg.user)@$($cfg.host)" } else { 'clearadmin@103.42.181.219' }
    $out = Send-ArchiveOverSsh ($sshOptions + @($sshTarget,$remoteCommand)) $archive
    Remove-Item -LiteralPath $archive -Force
    $result = $out | ConvertFrom-Json
    if ($result.result -cne 'success' -or $result.image_id -cne $imageId -or
        $result.commit -cne $commit -or $result.archive_sha256 -cne $archiveHash) {
        throw 'Remote success result does not match exact candidate identity'
    }
    ([ordered]@{ result='SUCCESS'; commit=$commit; archive_sha256=$archiveHash
        image_id=$imageId; updated_service='new-api'; scope=$scope
        backup_id=$result.backup_id; health=$result.health; rollback=$result.rollback
        target_mode=$(if ($TestTarget) { 'isolated-test-v1' } else { 'production' })
        public_url=$(if ($TestTarget) { $null } else { 'https://www.clear999.com' }) }) | ConvertTo-Json -Depth 4
}
catch {
    Write-Error "BLOCKED at [$phase]: $($_.Exception.Message)"
    exit 1
}
finally {
    # No production-side cleanup is attempted from Windows.
}
