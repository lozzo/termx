[CmdletBinding()]
param(
    [string]$Binary,
    [string]$InstallDirectory = (Join-Path $env:LOCALAPPDATA 'Programs\Muxvia'),
    [switch]$NoStart,
    [switch]$NoAutoStart,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$installedBinary = Join-Path $InstallDirectory 'muxvia.exe'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$runName = 'MuxviaDaemon'

function Update-UserPath([string]$Directory, [bool]$Add) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @($current -split ';' | Where-Object { $_.Trim() })
    $filtered = @($parts | Where-Object { -not [StringComparer]::OrdinalIgnoreCase.Equals($_.TrimEnd('\'), $Directory.TrimEnd('\')) })
    if ($Add) { $filtered += $Directory }
    [Environment]::SetEnvironmentVariable('Path', ($filtered -join ';'), 'User')
    $processParts = @($env:Path -split ';' | Where-Object { -not [StringComparer]::OrdinalIgnoreCase.Equals($_.TrimEnd('\'), $Directory.TrimEnd('\')) })
    if ($Add) { $processParts += $Directory }
    $env:Path = $processParts -join ';'
}

function Stop-InstalledDaemon {
    if (Test-Path -LiteralPath $installedBinary) {
        $previousPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            & $installedBinary daemon stop *> $null
            $stopExitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousPreference
        }
        if ($stopExitCode -notin @(0, 3)) {
            throw "stopping the installed daemon failed with exit code $stopExitCode"
        }
    }
}

function Remove-InstallDirectory {
    if (-not (Test-Path -LiteralPath $InstallDirectory)) { return }
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    do {
        try {
            Remove-Item -LiteralPath $InstallDirectory -Recurse -Force -ErrorAction Stop
            return
        } catch {
            $lastError = $_
            Start-Sleep -Milliseconds 100
        }
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "removing the install directory failed: $lastError"
}

if ($Uninstall) {
    Stop-InstalledDaemon
    Remove-ItemProperty -Path $runKey -Name $runName -ErrorAction SilentlyContinue
    Update-UserPath $InstallDirectory $false
    Remove-InstallDirectory
    Write-Host 'Muxvia was removed from the current user account'
    return
}

if (-not $Binary) {
    $packagedBinary = Join-Path $PSScriptRoot 'muxvia.exe'
    if (Test-Path -LiteralPath $packagedBinary) {
        $Binary = $packagedBinary
    } else {
        & (Join-Path $PSScriptRoot 'build-windows.ps1')
        if ($LASTEXITCODE -ne 0) { throw 'building the Windows binary failed' }
        $Binary = Join-Path $repoRoot '.artifacts\bin\muxvia.exe'
    }
}
$Binary = (Resolve-Path -LiteralPath $Binary).Path

Stop-InstalledDaemon
New-Item -ItemType Directory -Path $InstallDirectory -Force | Out-Null
$temporaryBinary = Join-Path $InstallDirectory 'muxvia.exe.new'
Copy-Item -LiteralPath $Binary -Destination $temporaryBinary -Force
Move-Item -LiteralPath $temporaryBinary -Destination $installedBinary -Force

foreach ($notice in @('LICENSE', 'THIRD_PARTY_NOTICES.txt')) {
    $source = Join-Path $PSScriptRoot $notice
    if (-not (Test-Path -LiteralPath $source)) { $source = Join-Path $repoRoot $notice }
    if (Test-Path -LiteralPath $source) {
        Copy-Item -LiteralPath $source -Destination (Join-Path $InstallDirectory $notice) -Force
    }
}

Update-UserPath $InstallDirectory $true
if ($NoAutoStart) {
    Remove-ItemProperty -Path $runKey -Name $runName -ErrorAction SilentlyContinue
} else {
    New-Item -Path $runKey -Force | Out-Null
    $escapedBinary = $installedBinary.Replace("'", "''")
    $startup = 'powershell.exe -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -Command "& ''{0}'' daemon start"' -f $escapedBinary
    Set-ItemProperty -Path $runKey -Name $runName -Value $startup
}
if (-not $NoStart) {
    & $installedBinary daemon start
    if ($LASTEXITCODE -ne 0) { throw 'starting the installed daemon failed' }
}

Write-Host "Muxvia is installed at $installedBinary"
