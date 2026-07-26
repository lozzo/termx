[CmdletBinding()]
param(
    [switch]$Cloud,
    [switch]$Clean
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$artifactDir = Join-Path $repoRoot '.artifacts\bin'

if ($Clean -and (Test-Path -LiteralPath $artifactDir)) {
    Remove-Item -LiteralPath $artifactDir -Recurse -Force
}
New-Item -ItemType Directory -Path $artifactDir -Force | Out-Null

$go = Get-Command go -ErrorAction Stop
$previousGoWork = $env:GOWORK
try {
    $env:GOWORK = 'off'
    & $go.Source build -trimpath -o (Join-Path $artifactDir 'muxvia.exe') ./cmd/muxvia
    if ($LASTEXITCODE -ne 0) { throw 'building muxvia.exe failed' }

    if ($Cloud) {
        & $go.Source build -trimpath -o (Join-Path $artifactDir 'muxvia-cloud-controller.exe') ./cmd/muxvia-cloud-controller
        if ($LASTEXITCODE -ne 0) { throw 'building muxvia-cloud-controller.exe failed' }
        & $go.Source build -trimpath -o (Join-Path $artifactDir 'muxvia-cloud-edge.exe') ./cmd/muxvia-cloud-edge
        if ($LASTEXITCODE -ne 0) { throw 'building muxvia-cloud-edge.exe failed' }
    }
} finally {
    $env:GOWORK = $previousGoWork
}

Write-Host "Windows binaries are available in $artifactDir"
