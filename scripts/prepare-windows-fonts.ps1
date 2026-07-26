[CmdletBinding()]
param(
    [string]$Destination
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$repoRoot = Split-Path -Parent $PSScriptRoot
$fontVersion = 'v3.4.0'
if (-not $Destination) {
    $Destination = Join-Path $repoRoot ".artifacts\fonts\jetbrains-mono-nerd-font-$fontVersion"
}

$assets = @(
    @{
        RelativePath = 'Ligatures/Regular/JetBrainsMonoNerdFontMono-Regular.ttf'
        FileName = 'JetBrainsMonoNerdFontMono-Regular.ttf'
        SHA256 = 'f01031f40e48dc29e1112e6b0b0450a2c6cd097f3f35cfff05c55cb311f8034c'
    },
    @{
        RelativePath = 'Ligatures/Bold/JetBrainsMonoNerdFontMono-Bold.ttf'
        FileName = 'JetBrainsMonoNerdFontMono-Bold.ttf'
        SHA256 = '5bdd4a873f3cd32f882d2c55545089123926e27707d5880fc9eaf84eb01b6686'
    },
    @{
        RelativePath = 'Ligatures/Italic/JetBrainsMonoNerdFontMono-Italic.ttf'
        FileName = 'JetBrainsMonoNerdFontMono-Italic.ttf'
        SHA256 = 'ccd88b36d325e6a905edc8dd3f2522718d9690d9bed3fbb4684c7e746c34f846'
    },
    @{
        RelativePath = 'Ligatures/BoldItalic/JetBrainsMonoNerdFontMono-BoldItalic.ttf'
        FileName = 'JetBrainsMonoNerdFontMono-BoldItalic.ttf'
        SHA256 = 'd931df2928b3216892d35980cddcad9edade1b9c9cd2e09a6c2937139f474742'
    },
    @{
        RelativePath = 'Ligatures/Regular/OFL.txt'
        FileName = 'OFL.txt'
        SHA256 = '30f0c136e3c88e422d0791acd97238870f9054a9729bc34cf2ff0d4ed8cac4ad'
    }
)

function Test-AssetDigest([string]$Path, [string]$Expected) {
    if (-not (Test-Path -LiteralPath $Path)) { return $false }
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash
    return [StringComparer]::OrdinalIgnoreCase.Equals($actual, $Expected)
}

# Windows PowerShell 5.1 may otherwise negotiate an obsolete protocol with GitHub.
[Net.ServicePointManager]::SecurityProtocol =
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
New-Item -ItemType Directory -Path $Destination -Force | Out-Null

foreach ($asset in $assets) {
    $target = Join-Path $Destination $asset.FileName
    if (Test-AssetDigest $target $asset.SHA256) { continue }

    $url = 'https://raw.githubusercontent.com/ryanoasis/nerd-fonts/{0}/patched-fonts/JetBrainsMono/{1}' -f $fontVersion, $asset.RelativePath
    $temporary = "$target.download"
    $downloaded = $false
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
            Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $temporary
            if (-not (Test-AssetDigest $temporary $asset.SHA256)) {
                throw "checksum mismatch for $($asset.FileName)"
            }
            Move-Item -LiteralPath $temporary -Destination $target -Force
            $downloaded = $true
            break
        } catch {
            $lastError = $_
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
            if ($attempt -lt 3) { Start-Sleep -Seconds $attempt }
        }
    }
    if (-not $downloaded) {
        throw "downloading pinned Nerd Font asset $($asset.FileName) failed: $lastError"
    }
}

Write-Host "Pinned Nerd Font assets are available in $Destination"
