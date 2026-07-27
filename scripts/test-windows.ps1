[CmdletBinding()]
param(
    [switch]$GoOnly
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

# Tests own their ANYTTY configuration and must not inherit a developer session.
Get-ChildItem Env: | Where-Object Name -Like 'ANYTTY*' | ForEach-Object {
    Remove-Item "Env:$($_.Name)"
}
$env:GOWORK = 'off'

& go test ./... -count=1
if ($LASTEXITCODE -ne 0) { throw 'Go test gate failed' }

if (-not $GoOnly) {
    & node scripts/client-workspace-guard.mjs
    if ($LASTEXITCODE -ne 0) { throw 'client workspace guard failed' }
    & npm.cmd run proto
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript Proto generation failed' }
    & git diff --exit-code -- clients/ui/src/generated
    if ($LASTEXITCODE -ne 0) { throw 'generated TypeScript Proto files are stale' }
    & npm.cmd run test:i18n
    if ($LASTEXITCODE -ne 0) { throw 'i18n gate failed' }
    & npm.cmd test
    if ($LASTEXITCODE -ne 0) { throw 'client test gate failed' }
    & npm.cmd run typecheck
    if ($LASTEXITCODE -ne 0) { throw 'client typecheck gate failed' }
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'client build gate failed' }
}

Write-Host 'Windows test gate passed'
