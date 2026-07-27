[CmdletBinding()]
param(
    [string]$Binary,
    [string]$InstallDirectory = (Join-Path $env:LOCALAPPDATA 'Programs\AnyTTY'),
    [switch]$NoStart,
    [switch]$NoAutoStart,
    [switch]$NoFont,
    [switch]$NoTerminalProfile,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$installedBinary = Join-Path $InstallDirectory 'anytty.exe'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$runName = 'AnyTTYDaemon'
$fontFamily = 'JetBrainsMono NFM'
$fontKey = 'HKCU:\Software\Microsoft\Windows NT\CurrentVersion\Fonts'
$userFontDirectory = Join-Path $env:LOCALAPPDATA 'Microsoft\Windows\Fonts'
$terminalFragmentDirectory = Join-Path $env:LOCALAPPDATA 'Microsoft\Windows Terminal\Fragments\AnyTTY'
$terminalFragment = Join-Path $terminalFragmentDirectory 'anytty.json'
$fontDefinitions = @(
    @{ FileName = 'JetBrainsMonoNerdFontMono-Regular.ttf'; RegistryName = 'AnyTTY JetBrainsMono NFM Regular (TrueType)' },
    @{ FileName = 'JetBrainsMonoNerdFontMono-Bold.ttf'; RegistryName = 'AnyTTY JetBrainsMono NFM Bold (TrueType)' },
    @{ FileName = 'JetBrainsMonoNerdFontMono-Italic.ttf'; RegistryName = 'AnyTTY JetBrainsMono NFM Italic (TrueType)' },
    @{ FileName = 'JetBrainsMonoNerdFontMono-BoldItalic.ttf'; RegistryName = 'AnyTTY JetBrainsMono NFM Bold Italic (TrueType)' }
)

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

function Initialize-FontInterop {
    if ('AnyTTY.WindowsFontInterop' -as [type]) { return }
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

namespace AnyTTY {
    public static class WindowsFontInterop {
        [DllImport("gdi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern int AddFontResourceEx(string fileName, uint flags, IntPtr reserved);

        [DllImport("gdi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern bool RemoveFontResourceEx(string fileName, uint flags, IntPtr reserved);

        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern IntPtr SendMessageTimeout(
            IntPtr window, uint message, UIntPtr wParam, IntPtr lParam,
            uint flags, uint timeout, out UIntPtr result);
    }
}
'@
}

function Publish-FontChange {
    Initialize-FontInterop
    $result = [UIntPtr]::Zero
    [void][AnyTTY.WindowsFontInterop]::SendMessageTimeout(
        [IntPtr]0xffff, 0x001d, [UIntPtr]::Zero, [IntPtr]::Zero, 0x0002, 1000, [ref]$result)
}

function Resolve-FontSourceDirectory {
    $packaged = Join-Path $PSScriptRoot 'fonts'
    if (Test-Path -LiteralPath (Join-Path $packaged $fontDefinitions[0].FileName)) { return $packaged }

    $prepare = Join-Path $PSScriptRoot 'prepare-windows-fonts.ps1'
    if (-not (Test-Path -LiteralPath $prepare)) {
        throw 'the Windows package does not contain the pinned Nerd Font assets'
    }
    $prepared = Join-Path $repoRoot '.artifacts\fonts\jetbrains-mono-nerd-font-v3.4.0'
    & $prepare -Destination $prepared
    return $prepared
}

function Install-AnyTTYFonts([string]$SourceDirectory) {
    Initialize-FontInterop
    New-Item -ItemType Directory -Path $userFontDirectory -Force | Out-Null
    New-Item -Path $fontKey -Force | Out-Null

    foreach ($font in $fontDefinitions) {
        $source = Join-Path $SourceDirectory $font.FileName
        if (-not (Test-Path -LiteralPath $source)) { throw "font asset is missing: $source" }
        $destination = Join-Path $userFontDirectory ("AnyTTY-$($font.FileName)")
        $registeredPath = $null
        $fontProperties = Get-ItemProperty -Path $fontKey -ErrorAction SilentlyContinue
        if ($fontProperties) {
            $registeredProperty = $fontProperties.PSObject.Properties[$font.RegistryName]
            if ($registeredProperty) { $registeredPath = $registeredProperty.Value }
        }
        $needsRegistration = -not [StringComparer]::OrdinalIgnoreCase.Equals($registeredPath, $destination)
        $needsCopy = -not (Test-Path -LiteralPath $destination)
        if (-not $needsCopy) {
            $sourceDigest = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
            $destinationDigest = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash
            $needsCopy = -not [StringComparer]::OrdinalIgnoreCase.Equals($sourceDigest, $destinationDigest)
        }
        if ($needsCopy) {
            $temporary = "$destination.new"
            Copy-Item -LiteralPath $source -Destination $temporary -Force
            Move-Item -LiteralPath $temporary -Destination $destination -Force
        }

        New-ItemProperty -Path $fontKey -Name $font.RegistryName -Value $destination -PropertyType String -Force | Out-Null
        if (($needsCopy -or $needsRegistration) -and
            [AnyTTY.WindowsFontInterop]::AddFontResourceEx($destination, 0, [IntPtr]::Zero) -eq 0) {
            throw "registering font failed: $destination"
        }
    }

    $licenseDirectory = Join-Path $InstallDirectory 'licenses'
    New-Item -ItemType Directory -Path $licenseDirectory -Force | Out-Null
    foreach ($license in @('OFL.txt', 'NERD_FONTS_LICENSE.txt')) {
        $source = Join-Path $SourceDirectory $license
        if (Test-Path -LiteralPath $source) {
            Copy-Item -LiteralPath $source -Destination (Join-Path $licenseDirectory $license) -Force
        }
    }
    Publish-FontChange
}

function Remove-AnyTTYFonts {
    Initialize-FontInterop
    foreach ($font in $fontDefinitions) {
        $destination = Join-Path $userFontDirectory ("AnyTTY-$($font.FileName)")
        Remove-ItemProperty -Path $fontKey -Name $font.RegistryName -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $destination) {
            [void][AnyTTY.WindowsFontInterop]::RemoveFontResourceEx($destination, 0, [IntPtr]::Zero)
            Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
            if (Test-Path -LiteralPath $destination) {
                Write-Warning "font file is still in use and could not be removed: $destination"
            }
        }
    }
    Publish-FontChange
}

function Install-WindowsTerminalProfile {
    New-Item -ItemType Directory -Path $terminalFragmentDirectory -Force | Out-Null
    $profile = [ordered]@{
        profiles = @(
            [ordered]@{
                name = 'AnyTTY'
                guid = '{e7ebe7aa-1104-462b-8a95-d598c3ec65d6}'
                commandline = ('"{0}"' -f $installedBinary)
                startingDirectory = '%USERPROFILE%'
                font = [ordered]@{ face = $fontFamily }
            }
        )
    }
    $temporary = "$terminalFragment.new"
    $profile | ConvertTo-Json -Depth 5 | Out-File -LiteralPath $temporary -Encoding utf8
    Move-Item -LiteralPath $temporary -Destination $terminalFragment -Force
}

function Remove-WindowsTerminalProfile {
    Remove-Item -LiteralPath $terminalFragment -Force -ErrorAction SilentlyContinue
    if ((Test-Path -LiteralPath $terminalFragmentDirectory) -and
        -not (Get-ChildItem -LiteralPath $terminalFragmentDirectory -Force | Select-Object -First 1)) {
        Remove-Item -LiteralPath $terminalFragmentDirectory -Force
    }
}

if ($Uninstall) {
    Stop-InstalledDaemon
    Remove-ItemProperty -Path $runKey -Name $runName -ErrorAction SilentlyContinue
    Update-UserPath $InstallDirectory $false
    Remove-WindowsTerminalProfile
    Remove-AnyTTYFonts
    Remove-InstallDirectory
    Write-Host 'AnyTTY was removed from the current user account'
    return
}

if (-not $Binary) {
    $packagedBinary = Join-Path $PSScriptRoot 'anytty.exe'
    if (Test-Path -LiteralPath $packagedBinary) {
        $Binary = $packagedBinary
    } else {
        & (Join-Path $PSScriptRoot 'build-windows.ps1')
        if ($LASTEXITCODE -ne 0) { throw 'building the Windows binary failed' }
        $Binary = Join-Path $repoRoot '.artifacts\bin\anytty.exe'
    }
}
$Binary = (Resolve-Path -LiteralPath $Binary).Path

Stop-InstalledDaemon
New-Item -ItemType Directory -Path $InstallDirectory -Force | Out-Null
$temporaryBinary = Join-Path $InstallDirectory 'anytty.exe.new'
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
if (-not $NoFont) {
    Install-AnyTTYFonts (Resolve-FontSourceDirectory)
}
if (-not $NoTerminalProfile) {
    Install-WindowsTerminalProfile
}
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

Write-Host "AnyTTY is installed at $installedBinary"
if (-not $NoTerminalProfile) {
    Write-Host "Open the AnyTTY profile in Windows Terminal to use $fontFamily"
}
