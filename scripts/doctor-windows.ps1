[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$required = @('git', 'go', 'node', 'npm.cmd', 'protoc')
foreach ($name in $required) {
    if (-not (Get-Command $name -ErrorAction SilentlyContinue)) {
        throw "required command is missing: $name"
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $repoRoot 'node_modules\.bin\protoc-gen-es.cmd'))) {
    throw 'npm workspace dependencies are missing; run npm.cmd ci'
}

$goVersion = (& go env GOVERSION).Trim()
if ($goVersion -notmatch '^go1\.26\.') {
    throw "Go 1.26.x is required; found $goVersion"
}
$nodeVersion = (& node --version).Trim()
if ($nodeVersion -notmatch '^v24\.') {
    throw "Node.js 24.x is required; found $nodeVersion"
}

& node scripts/client-workspace-guard.mjs
if ($LASTEXITCODE -ne 0) { throw 'client workspace guard failed' }
& npm.cmd ls --all --json | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'npm workspace dependency validation failed' }

Write-Host "git: $((& git --version).Trim())"
Write-Host "go: $goVersion"
Write-Host "node: $nodeVersion"
Write-Host "npm: $((& npm.cmd --version).Trim())"
Write-Host "protoc: $((& protoc --version).Trim())"
Write-Host 'Windows repository doctor passed'
