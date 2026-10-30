# Loaded only by deploy-local.ps1 -StackTarget; reuse original SSH transport.
function Invoke-StackDeployment($cfg, [string]$configPath, [bool]$dryRun) {
    foreach ($name in @('ssh','git')) { Require-Command $name }
    if ($cfg.targetMode -cne 'six-service-v1' -or $cfg.targetId -cnotmatch '^[a-z][a-z0-9-]{2,40}$' -or
        $cfg.host -cnotmatch '^[A-Za-z0-9][A-Za-z0-9.-]+$' -or
        $cfg.user -cnotmatch '^[a-z_][a-z0-9_-]*$' -or [int]$cfg.port -lt 1 -or [int]$cfg.port -gt 65535) {
        throw 'Invalid explicit six-service SSH target identity'
    }
    $owner = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    foreach ($path in @($configPath, $cfg.identityFile, $cfg.knownHostsFile)) {
        $item = Get-Item -LiteralPath $path
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Protected input cannot be a reparse point' }
        $acl = Get-Acl -LiteralPath $path
        foreach ($rule in $acl.Access) {
            $sid = $rule.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value
            if ($rule.AccessControlType -eq 'Allow' -and $sid -notin @($owner,'S-1-5-18','S-1-5-32-544') -and
                (($rule.FileSystemRights -band [Security.AccessControl.FileSystemRights]::Write) -or
                 ($path -ceq $cfg.identityFile -and ($rule.FileSystemRights -band [Security.AccessControl.FileSystemRights]::ReadData)))) {
                throw 'SSH target config/identity is writable by another principal'
            }
        }
    }
    foreach ($hash in @($cfg.bundleSha256, $cfg.remoteConfigSha256)) {
        if ($hash -cnotmatch '^[a-f0-9]{64}$') { throw 'Missing exact approved bundle/config SHA256' }
    }
    $bundle = Get-Item -LiteralPath $cfg.bundlePath
    if ((Get-FileHash -LiteralPath $bundle.FullName -Algorithm SHA256).Hash.ToLowerInvariant() -cne $cfg.bundleSha256) {
        throw 'Local offline bundle checksum mismatch'
    }
    if (-not (Is-Commit $cfg.gatewayCommit)) { throw 'Invalid application source commit' }
    Run-Native 'git' @('-C',$repo,'cat-file','-e',"$($cfg.gatewayCommit)^{commit}") | Out-Null
    $options = @('-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','IdentitiesOnly=yes',
        '-o','ServerAliveInterval=10','-o','ServerAliveCountMax=3',
        '-o',"UserKnownHostsFile=$($cfg.knownHostsFile)",'-i',[string]$cfg.identityFile,'-p',[string]$cfg.port)
    $target = "$($cfg.user)@$($cfg.host)"
    $helper = 'sudo -n /opt/clearapi/bin/clearapi-local-deploy.py'
    $preflight = (Run-Native 'ssh' ($options + @($target, "$helper stack-preflight"))) | ConvertFrom-Json
    if ($preflight.target_mode -cne 'six-service-v1' -or $preflight.target_id -cne $cfg.targetId -or
        $preflight.config_sha256 -cne $cfg.remoteConfigSha256 -or $preflight.isolated -ne $cfg.isolated -or $preflight.pending -or
        $preflight.generation -cnotmatch '^(EMPTY|[a-f0-9]{64})$') {
        throw 'Root-approved target identity/configuration or transaction state differs'
    }
    if ($dryRun) {
        [ordered]@{result='DRY_RUN_NO_REMOTE_WRITES';target_id=$cfg.targetId;bundle_sha256=$cfg.bundleSha256;
            generation=$preflight.generation;services=$preflight.services} | ConvertTo-Json -Depth 6
        return
    }
    $command = "$helper stack-receive --bundle-sha256 $($cfg.bundleSha256) --bundle-bytes $($bundle.Length) --expected-generation $($preflight.generation)"
    $result = (Send-ArchiveOverSsh ($options + @($target,$command)) $bundle.FullName) | ConvertFrom-Json
    if ($result.result -cnotin @('success','already_applied') -or $result.target_id -cne $cfg.targetId -or
        $result.bundle_sha256 -cne $cfg.bundleSha256 -or $result.config_sha256 -cne $cfg.remoteConfigSha256) {
        throw 'Remote receipt does not match approved target and package'
    }
    $result | ConvertTo-Json -Depth 8
}
