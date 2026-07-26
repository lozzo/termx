[CmdletBinding()]
param(
    [string]$Version,
    [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

if (-not $Version) {
    $Version = (& git describe --tags --always --dirty).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $Version) { $Version = 'development' }
}
if (-not $SkipBuild) {
    & (Join-Path $PSScriptRoot 'build-windows.ps1')
}

$binary = Join-Path $repoRoot '.artifacts\bin\muxvia.exe'
if (-not (Test-Path -LiteralPath $binary)) { throw "Windows binary is missing: $binary" }
$packageRoot = Join-Path $repoRoot ".artifacts\packages\muxvia-$Version-windows-amd64"
$archive = "$packageRoot.zip"
if (Test-Path -LiteralPath $packageRoot) { Remove-Item -LiteralPath $packageRoot -Recurse -Force }
if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null

Copy-Item -LiteralPath $binary -Destination (Join-Path $packageRoot 'muxvia.exe')
Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE') -Destination $packageRoot
Copy-Item -LiteralPath (Join-Path $repoRoot 'cmd\muxvia\THIRD_PARTY_NOTICES.txt') -Destination (Join-Path $packageRoot 'THIRD_PARTY_NOTICES.txt')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'install-windows.ps1') -Destination (Join-Path $packageRoot 'install.ps1')
Compress-Archive -LiteralPath $packageRoot -DestinationPath $archive -CompressionLevel Optimal
Write-Host "Windows package created: $archive"
