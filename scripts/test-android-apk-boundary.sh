#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/verify-android-apk-boundary.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/anytty-apk-boundary-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  printf '%s\n' "Android APK boundary test failed: $*" >&2
  exit 1
}

for tool in node zip unzip rg; do
  command -v "$tool" >/dev/null 2>&1 || fail "required test tool is unavailable: $tool"
done

fake_apkanalyzer="$tmp_dir/apkanalyzer"
fake_aapt2="$tmp_dir/aapt2"
cat >"$fake_apkanalyzer" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
subject="$1"
verb="$2"
apk="${@: -1}"
case "$subject:$verb" in
  manifest:print) unzip -p "$apk" fixture/AndroidManifest.xml ;;
  resources:xml)
    resource=''
    shift 2
    while (( $# > 1 )); do
      if [[ "$1" == '--file' ]]; then resource="$2"; shift 2; else shift; fi
    done
    unzip -p "$apk" "$resource"
    ;;
  dex:packages) unzip -p "$apk" fixture/dex-packages.txt ;;
  *) exit 2 ;;
esac
SH
cat >"$fake_aapt2" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$1:$2" in
  dump:resources) unzip -p "$3" fixture/resources.txt ;;
  dump:xmltree)
    [[ "$4" == '--file' ]] || exit 2
    unzip -p "$3" "$5"
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$fake_apkanalyzer" "$fake_aapt2"

gate_env=(
  env
  ANYTTY_APKANALYZER="$fake_apkanalyzer"
  ANYTTY_AAPT2="$fake_aapt2"
  ANYTTY_ANDROID_EXPECTED_ABIS='arm64-v8a x86_64'
)

create_complete_root() {
  local root="$1"
  mkdir -p "$root/fixture" "$root/res/xml" "$root/assets/public" "$root/META-INF/services"
  for abi in arm64-v8a x86_64; do
    mkdir -p "$root/lib/$abi"
    printf '%s\n' production-native >"$root/lib/$abi/libanytty_client.so"
    printf '%s\n' production-native >"$root/lib/$abi/libanytty_client_jni.so"
  done
  cp "$repo_root/clients/mobile/android/app/src/main/res/xml/network_security_config.xml" "$root/res/xml/network_security_config.xml"
  cp "$repo_root/clients/mobile/android/app/src/main/res/xml/backup_rules.xml" "$root/res/xml/backup_rules.xml"
  cp "$repo_root/clients/mobile/android/app/src/main/res/xml/data_extraction_rules.xml" "$root/res/xml/data_extraction_rules.xml"
  cp "$repo_root/clients/mobile/index.html" "$root/assets/public/index.html"
  printf '%s\n' 'P d 1 1 1 org.slf4j.nop' >"$root/fixture/dex-packages.txt"
  printf '%s\n' 'org.slf4j.nop.NOPServiceProvider' >"$root/META-INF/services/org.slf4j.spi.SLF4JServiceProvider"
  cat >"$root/fixture/resources.txt" <<'TEXT'
    resource 0x7f100000 xml/backup_rules
      () (file) res/xml/backup_rules.xml type=XML
    resource 0x7f100001 xml/config
      () (file) res/xml/config.xml type=XML
    resource 0x7f100002 xml/data_extraction_rules
      () (file) res/xml/data_extraction_rules.xml type=XML
    resource 0x7f100003 xml/network_security_config
      () (file) res/xml/network_security_config.xml type=XML
TEXT
  cat >"$root/fixture/AndroidManifest.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.anytty.app">
  <application android:allowBackup="false" android:fullBackupContent="@ref/0x7f100000" android:dataExtractionRules="@ref/0x7f100002" android:usesCleartextTraffic="false" android:networkSecurityConfig="@ref/0x7f100003">
    <activity android:name="com.anytty.app.MainActivity" android:exported="true">
      <intent-filter>
        <action android:name="android.intent.action.MAIN" />
        <category android:name="android.intent.category.LAUNCHER" />
      </intent-filter>
    </activity>
    <provider android:name="androidx.startup.InitializationProvider" android:authorities="com.anytty.app.androidx-startup" android:exported="false">
      <meta-data android:name="androidx.lifecycle.ProcessLifecycleInitializer" android:value="androidx.startup" />
    </provider>
    <receiver android:name="androidx.profileinstaller.ProfileInstallReceiver" android:directBootAware="false" android:enabled="true" android:exported="true" android:permission="android.permission.DUMP">
      <intent-filter>
        <action android:name="androidx.profileinstaller.action.INSTALL_PROFILE" />
      </intent-filter>
    </receiver>
  </application>
</manifest>
XML
}

pack_apk() {
  local root="$1"
  local apk="$2"
  (cd "$root" && zip -q -r "$apk" .)
}

replace_file() {
  local path="$1"
  local before="$2"
  local after="$3"
  node --input-type=commonjs - "$path" "$before" "$after" <<'NODE'
const { readFileSync, writeFileSync } = require('node:fs')
const [path, before, after] = process.argv.slice(2)
const source = readFileSync(path, 'utf8')
if (!source.includes(before)) throw new Error(`fixture marker missing: ${before}`)
writeFileSync(path, source.replace(before, after))
NODE
}

expect_failure() {
  local label="$1"
  local expected="$2"
  shift 2
  local log="$tmp_dir/$label.log"
  if "$@" >"$log" 2>&1; then fail "$label unexpectedly passed"; fi
  if ! rg -F -q "$expected" "$log"; then
    sed -n '1,100p' "$log" >&2
    fail "$label did not report the expected failure"
  fi
}

complete_root="$tmp_dir/complete"
create_complete_root "$complete_root"
complete_apk="$tmp_dir/complete.apk"
pack_apk "$complete_root" "$complete_apk"
"${gate_env[@]}" "$gate" "$complete_apk"

missing_root="$tmp_dir/missing"
cp -R "$complete_root" "$missing_root"
rm "$missing_root/lib/x86_64/libanytty_client.so"
missing_apk="$tmp_dir/missing.apk"
pack_apk "$missing_root" "$missing_apk"
expect_failure missing 'missing required native library' "${gate_env[@]}" "$gate" "$missing_apk"

provider_root="$tmp_dir/provider"
cp -R "$complete_root" "$provider_root"
replace_file "$provider_root/fixture/AndroidManifest.xml" '</application>' '<provider android:name="androidx.core.content.FileProvider" /></application>'
provider_apk="$tmp_dir/provider.apk"
pack_apk "$provider_root" "$provider_apk"
expect_failure provider 'release manifest contains forbidden FileProvider' "${gate_env[@]}" "$gate" "$provider_apk"

lifecycle_root="$tmp_dir/lifecycle"
cp -R "$complete_root" "$lifecycle_root"
replace_file "$lifecycle_root/fixture/AndroidManifest.xml" '<meta-data android:name="androidx.lifecycle.ProcessLifecycleInitializer" android:value="androidx.startup" />' ''
lifecycle_apk="$tmp_dir/lifecycle.apk"
pack_apk "$lifecycle_root" "$lifecycle_apk"
expect_failure lifecycle 'ProcessLifecycleInitializer' "${gate_env[@]}" "$gate" "$lifecycle_apk"

backup_root="$tmp_dir/backup"
cp -R "$complete_root" "$backup_root"
replace_file "$backup_root/fixture/AndroidManifest.xml" 'android:allowBackup="false"' 'android:allowBackup="true"'
backup_apk="$tmp_dir/backup.apk"
pack_apk "$backup_root" "$backup_apk"
expect_failure backup 'android:allowBackup must be "false"' "${gate_env[@]}" "$gate" "$backup_apk"

reference_root="$tmp_dir/reference"
cp -R "$complete_root" "$reference_root"
replace_file "$reference_root/fixture/AndroidManifest.xml" 'android:fullBackupContent="@ref/0x7f100000"' 'android:fullBackupContent="@ref/0x7f100002"'
reference_apk="$tmp_dir/reference.apk"
pack_apk "$reference_root" "$reference_apk"
expect_failure reference 'android:fullBackupContent must be "@ref/0x7f100000"' "${gate_env[@]}" "$gate" "$reference_apk"

network_root="$tmp_dir/network"
cp -R "$complete_root" "$network_root"
replace_file "$network_root/res/xml/network_security_config.xml" '<base-config cleartextTrafficPermitted="false"' '<base-config cleartextTrafficPermitted="true"'
network_apk="$tmp_dir/network.apk"
pack_apk "$network_root" "$network_apk"
expect_failure network 'network base-config must deny cleartext' "${gate_env[@]}" "$gate" "$network_apk"

csp_root="$tmp_dir/csp"
cp -R "$complete_root" "$csp_root"
replace_file "$csp_root/assets/public/index.html" "object-src 'none'" "object-src 'self'"
csp_apk="$tmp_dir/csp.apk"
pack_apk "$csp_root" "$csp_apk"
expect_failure csp 'CSP value is not the exact Android contract' "${gate_env[@]}" "$gate" "$csp_apk"

dependency_root="$tmp_dir/dependency"
cp -R "$complete_root" "$dependency_root"
printf '%s\n' 'P d 1 1 1 org.slf4j.simple' >>"$dependency_root/fixture/dex-packages.txt"
dependency_apk="$tmp_dir/dependency.apk"
pack_apk "$dependency_root" "$dependency_apk"
expect_failure dependency 'non-NOP SLF4J provider' "${gate_env[@]}" "$gate" "$dependency_apk"

marker_root="$tmp_dir/marker"
cp -R "$complete_root" "$marker_root"
printf '%s\n' android-spike-daemon >>"$marker_root/lib/arm64-v8a/libanytty_client.so"
marker_apk="$tmp_dir/marker.apk"
pack_apk "$marker_root" "$marker_apk"
expect_failure marker 'forbidden Android dev/spike marker' "${gate_env[@]}" "$gate" "$marker_apk"

corrupt_apk="$tmp_dir/corrupt.apk"
printf '%s\n' 'not-an-apk' >"$corrupt_apk"
expect_failure corrupt 'APK archive integrity check failed' "${gate_env[@]}" "$gate" "$corrupt_apk"

printf '%s\n' 'Android APK boundary fixture tests passed'
