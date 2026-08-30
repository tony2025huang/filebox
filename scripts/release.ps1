# release.ps1 — Windows 版单文件交付构建（与 Makefile release 目标同等标准）。
# release.ps1 — Windows single-binary release build, equivalent to the Makefile release target.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
& "$env:LOCALAPPDATA\Programs\nodejs\npm.cmd" --prefix web run build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go run ./scripts/sync-web.go
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/filebox-windows-amd64.exe ./cmd/filebox
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist/filebox-linux-amd64 ./cmd/filebox
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$env:GOOS = ""; $env:GOARCH = ""; Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
$lines = foreach ($file in Get-ChildItem dist -File | Where-Object { $_.Name -ne "SHA256SUMS.txt" }) {
    $hash = ((certutil -hashfile $file.FullName SHA256)[1] -replace " ", "").ToLowerInvariant()
    "$hash  $($file.Name)"
}
Set-Content -Path dist/SHA256SUMS.txt -Value $lines -Encoding ascii
Write-Output "release artifacts written to dist/:"
Get-ChildItem dist | Select-Object Name, Length | Format-Table -AutoSize
