#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sdk_root="${ANDROID_SDK_ROOT:-${HOME}/Library/Android/sdk}"
ndk_version="${TERMX_ANDROID_NDK_VERSION:-27.2.12479018}"
ndk_root="${ANDROID_NDK_ROOT:-${sdk_root}/ndk/${ndk_version}}"
output_root="${1:-${repo_root}/clients/mobile/android/app/build/generated/termxJniLibs}"
api="${TERMX_ANDROID_API:-24}"

if [[ ! -d "${ndk_root}" ]]; then
  echo "Android NDK ${ndk_version} is not installed at ${ndk_root}" >&2
  exit 1
fi

case "$(uname -s)" in
  Darwin) host_tag="darwin-x86_64" ;;
  Linux) host_tag="linux-x86_64" ;;
  *) echo "unsupported Android build host: $(uname -s)" >&2; exit 1 ;;
esac

toolchain="${ndk_root}/toolchains/llvm/prebuilt/${host_tag}/bin"
include_dir="${repo_root}/client/binding/cabi"
jni_source="${repo_root}/clients/mobile/android/app/src/main/cpp/termx_client_jni.c"

build_abi() {
  local abi="$1" goarch="$2" triple="$3"
  local destination="${output_root}/${abi}"
  mkdir -p "${destination}"
  (
    cd "${repo_root}"
    # Pion 的 Android interface adapter 仍使用受控 linkname；Go 1.23+ 需要显式允许该上游实现。
    GOOS=android GOARCH="${goarch}" CGO_ENABLED=1 CC="${toolchain}/${triple}${api}-clang" \
      go build -trimpath -buildmode=c-shared -ldflags='-checklinkname=0' \
      -o "${destination}/libtermx_client.so" ./client/binding/cabi/androidlib
  )
  "${toolchain}/${triple}${api}-clang" -shared -fPIC \
    -I"${include_dir}" "${jni_source}" \
    -L"${destination}" -ltermx_client \
    -Wl,-soname,libtermx_client_jni.so \
    -o "${destination}/libtermx_client_jni.so"
  rm -f "${destination}/libtermx_client.h"
}

build_abi arm64-v8a arm64 aarch64-linux-android
build_abi x86_64 amd64 x86_64-linux-android
