[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateScript({ Test-Path -LiteralPath $_ -PathType Leaf })]
    [string]$Executable,

    [string]$DataDirectory = 'C:\ProgramData\FileBox\data',
    [string]$LogDirectory = 'C:\ProgramData\FileBox\logs',
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$JwtSecret,
    [string]$ServiceName = 'FileBox'
)

$ErrorActionPreference = 'Stop'
$resolvedExecutable = (Resolve-Path -LiteralPath $Executable).Path
$resolvedData = [IO.Path]::GetFullPath($DataDirectory)
$resolvedLogs = [IO.Path]::GetFullPath($LogDirectory)
New-Item -ItemType Directory -Force -Path $resolvedData, $resolvedLogs | Out-Null

$nssm = Get-Command nssm -ErrorAction SilentlyContinue
if ($null -ne $nssm) {
    & $nssm.Source install $ServiceName $resolvedExecutable "--data=$resolvedData" '--log-enabled=true' "--log-dir=$resolvedLogs"
    if ($LASTEXITCODE -ne 0) { throw "nssm install failed with exit code $LASTEXITCODE" }
    & $nssm.Source set $ServiceName AppEnvironmentExtra "FILEBOX_JWT_SECRET=$JwtSecret"
    & $nssm.Source set $ServiceName AppStdout (Join-Path $resolvedLogs 'service-stdout.log')
    & $nssm.Source set $ServiceName AppStderr (Join-Path $resolvedLogs 'service-stderr.log')
    & $nssm.Source set $ServiceName Start SERVICE_AUTO_START
    if ($LASTEXITCODE -ne 0) { throw "nssm service configuration failed with exit code $LASTEXITCODE" }
    Write-Host "Installed $ServiceName with NSSM. Start it with: nssm start $ServiceName"
    exit 0
}

$quotedExecutable = '"' + $resolvedExecutable + '"'
$binPath = "$quotedExecutable --data=`"$resolvedData`" --log-enabled=true --log-dir=`"$resolvedLogs`""
Write-Warning 'nssm was not found; installing the basic Windows Service Control Manager entry.'
Write-Warning 'The sc.exe fallback cannot set FILEBOX_JWT_SECRET. Set it as a machine-level environment variable before starting the service.'
& sc.exe create $ServiceName binPath= $binPath start= auto DisplayName= 'FileBox'
if ($LASTEXITCODE -ne 0) { throw "sc create failed with exit code $LASTEXITCODE" }
& sc.exe description $ServiceName 'FileBox file service'
Write-Host "Installed $ServiceName with sc.exe. Start/stop/delete with: sc.exe start $ServiceName; sc.exe stop $ServiceName; sc.exe delete $ServiceName"
