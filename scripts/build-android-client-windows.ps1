[CmdletBinding()]
param(
    [string]$OutputRoot
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$sdkRoot = if ($env:ANDROID_SDK_ROOT) { $env:ANDROID_SDK_ROOT } elseif ($env:ANDROID_HOME) { $env:ANDROID_HOME } else { Join-Path $env:LOCALAPPDATA 'Android\Sdk' }
$ndkVersion = if ($env:ANYTTY_ANDROID_NDK_VERSION) { $env:ANYTTY_ANDROID_NDK_VERSION } else { '27.2.12479018' }
$ndkRoot = if ($env:ANDROID_NDK_ROOT) { $env:ANDROID_NDK_ROOT } else { Join-Path $sdkRoot "ndk\$ndkVersion" }
$api = if ($env:ANYTTY_ANDROID_API) { $env:ANYTTY_ANDROID_API } else { '24' }
if (-not $OutputRoot) { $OutputRoot = Join-Path $repoRoot 'clients\mobile\android\app\build\generated\anyttyJniLibs' }
if (-not (Test-Path -LiteralPath $ndkRoot)) { throw "Android NDK $ndkVersion is not installed at $ndkRoot" }

$toolchain = Join-Path $ndkRoot 'toolchains\llvm\prebuilt\windows-x86_64\bin'
$includeDir = Join-Path $repoRoot 'client\binding\cabi'
$jniSource = Join-Path $repoRoot 'clients\mobile\android\app\src\main\cpp\anytty_client_jni.c'

function Build-Abi([string]$Abi, [string]$GoArch, [string]$Triple) {
    $destination = Join-Path $OutputRoot $Abi
    New-Item -ItemType Directory -Path $destination -Force | Out-Null
    $compiler = Join-Path $toolchain "$Triple$api-clang.cmd"
    if (-not (Test-Path -LiteralPath $compiler)) { throw "Android compiler is missing: $compiler" }

    $previous = @($env:GOOS, $env:GOARCH, $env:CGO_ENABLED, $env:CC)
    try {
        $env:GOOS = 'android'
        $env:GOARCH = $GoArch
        $env:CGO_ENABLED = '1'
        $env:CC = '"' + $compiler + '"'
        $arguments = @('build', '-trimpath', '-buildmode=c-shared', '-ldflags=-checklinkname=0')
        $arguments += @('-o', (Join-Path $destination 'libanytty_client.so'), './client/binding/cabi/androidlib')
        Push-Location $repoRoot
        try { & go @arguments } finally { Pop-Location }
        if ($LASTEXITCODE -ne 0) { throw "building Go Android library for $Abi failed" }
    } finally {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED, $env:CC = $previous
    }

    & $compiler -shared -fPIC "-I$includeDir" $jniSource "-L$destination" -lanytty_client '-Wl,-soname,libanytty_client_jni.so' -o (Join-Path $destination 'libanytty_client_jni.so')
    if ($LASTEXITCODE -ne 0) { throw "building JNI bridge for $Abi failed" }
    Remove-Item -LiteralPath (Join-Path $destination 'libanytty_client.h') -ErrorAction SilentlyContinue
}

Build-Abi 'arm64-v8a' 'arm64' 'aarch64-linux-android'
Build-Abi 'x86_64' 'amd64' 'x86_64-linux-android'
