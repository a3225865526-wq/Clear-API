$ErrorActionPreference="Stop"
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
Get-ImageConfigId "clearapi-local-isolated:a032815-20261023"
