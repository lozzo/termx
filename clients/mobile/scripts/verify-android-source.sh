#!/usr/bin/env bash

set -euo pipefail

mobile_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
android_root="$mobile_root/android"
source_root="$android_root/app/src/main/java/com/termx/app"
manifest="$android_root/app/src/main/AndroidManifest.xml"
build_gradle="$android_root/app/build.gradle"

# Gradle source tree 是 Android 自定义源码唯一真值；cap sync 后缺失即失败，不做复制或回填。
if [[ -d "$mobile_root/native/android" ]]; then
  echo "duplicate Android source mirror must not exist: $mobile_root/native/android" >&2
  exit 1
fi

required_sources=(
  MainActivity.java
  NativeConnectionPlugin.kt
  NativeFilePickerPlugin.kt
  NativeHapticPlugin.java
  TermxDebugLog.kt
  TermxWebChromeClient.java
  connection/BridgeRouter.kt
  connection/ConnectionStore.kt
  connection/ConnectionStoreManager.kt
  connectors/ManagedWebRTCConnector.kt
  managed/AndroidGrantCredentialStore.kt
  managed/ManagedCloudAdapter.kt
  managed/ManagedCloudAssembly.kt
  managed/ManagedEndpointContract.kt
  managed/ManagedPathQuality.kt
  managed/ManagedPathQualityReporter.kt
  managed/ManagedRoutePlan.kt
  network/BridgeServer.kt
  network/NetworkStateManager.kt
  transfer/FileTransferManager.kt
  transfer/TransferTaskStore.kt
  transport/ChannelManager.kt
  transport/Heartbeat.kt
  transport/WebRTCTransport.kt
  util/HttpHelper.java
  util/StorageHelper.java
)
for relative_path in "${required_sources[@]}"; do
  if [[ ! -s "$source_root/$relative_path" ]]; then
    echo "cap sync removed required Android source: $relative_path" >&2
    exit 1
  fi
done

required_files=(
  "$manifest"
  "$build_gradle"
  "$android_root/app/src/main/res/xml/network_security_config.xml"
)
for required_file in "${required_files[@]}"; do
  if [[ ! -s "$required_file" ]]; then
    echo "cap sync removed required Android configuration: $required_file" >&2
    exit 1
  fi
done

required_manifest_fragments=(
  android.permission.ACCESS_NETWORK_STATE
  android.permission.WAKE_LOCK
  android.permission.VIBRATE
  android.permission.CAMERA
  android:networkSecurityConfig
)
for fragment in "${required_manifest_fragments[@]}"; do
  if ! rg -Fq "$fragment" "$manifest"; then
    echo "Android manifest lost required TermX configuration: $fragment" >&2
    exit 1
  fi
done

required_gradle_fragments=(
  "apply plugin: 'kotlin-android'"
  '// termx NativeConnection dependencies'
  'shrinkResources true'
  "test.resources.srcDir '../../../../shared/cloudcompanion/testdata'"
)
for fragment in "${required_gradle_fragments[@]}"; do
  if ! rg -Fq "$fragment" "$build_gradle"; then
    echo "Android Gradle configuration lost required TermX setting: $fragment" >&2
    exit 1
  fi
done

echo "Android Gradle source integrity passed"
