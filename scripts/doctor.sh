#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

required_commands=(git go node npm java javac protoc protoc-gen-go perl rg apkanalyzer)
for command_name in "${required_commands[@]}"; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command is missing: $command_name" >&2
    exit 1
  fi
done
if [[ ! -x clients/mobile/android/gradlew ]]; then
  echo "Android Gradle wrapper is missing or not executable" >&2
  exit 1
fi
if [[ ! -x node_modules/.bin/protoc-gen-es ]]; then
  echo "npm workspace dependencies are missing; run npm ci" >&2
  exit 1
fi

android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "$android_sdk" && -f clients/mobile/android/local.properties ]]; then
  android_sdk="$(sed -n 's/^sdk\.dir=//p' clients/mobile/android/local.properties | tail -1 | sed 's#\\:#:#g; s#\\\\#\\#g')"
fi
if [[ -z "$android_sdk" || ! -d "$android_sdk" ]]; then
  echo "Android SDK was not found via ANDROID_HOME, ANDROID_SDK_ROOT, or android/local.properties" >&2
  exit 1
fi

# doctor 只读取源码、锁文件和临时生成物，不修复或重写工作树。
scripts/repository-layout-guard.sh
npm ls --all --json >/dev/null
scripts/check-generated-code.sh
clients/mobile/scripts/verify-android-source.sh
clients/mobile/android/gradlew --version >/dev/null

printf '%s\n' \
  "go: $(go env GOVERSION)" \
  "node: $(node --version)" \
  "npm: $(npm --version)" \
  "protoc: $(protoc --version)" \
  "android-sdk: $android_sdk" \
  'repository doctor passed'
