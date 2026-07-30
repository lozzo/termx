#!/usr/bin/env node

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const mobileRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(mobileRoot, '..', '..')
const androidRoot = join(mobileRoot, 'android')
const sourceRoot = join(androidRoot, 'app', 'src', 'main', 'java', 'com', 'anytty', 'app')

function fail(message) {
  throw new Error(`Android source integrity failed: ${message}`)
}

function requireFile(path, label) {
  if (!existsSync(path) || !statSync(path).isFile() || statSync(path).size === 0) {
    fail(`required file is missing or empty: ${label}`)
  }
}

function collectProductionFiles(root, extensions) {
  if (!existsSync(root) || !statSync(root).isDirectory()) {
    fail(`required production source directory is missing: ${relative(repoRoot, root)}`)
  }
  const files = []
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name)
    if (entry.isDirectory()) {
      files.push(...collectProductionFiles(path, extensions))
    } else if (entry.isFile() && extensions.some((extension) => entry.name.endsWith(extension))) {
      files.push(path)
    }
  }
  return files
}

if (existsSync(join(mobileRoot, 'native', 'android'))) {
  fail('duplicate Android source mirror must not exist: native/android')
}

for (const path of [
  'MainActivity.java',
  'NativeConnectionPlugin.kt',
  'NativeFilePickerPlugin.kt',
  'NativeHapticPlugin.java',
  'AnyTTYDebugLog.kt',
  'AnyTTYWebChromeClient.java',
  join('goclient', 'AndroidClientPlatform.kt'),
  join('goclient', 'GoClientBridgeServer.kt'),
  join('goclient', 'GoClientNative.kt'),
  join('util', 'HttpHelper.java'),
  join('util', 'StorageHelper.java'),
]) {
  requireFile(join(sourceRoot, path), path)
}

for (const path of [
  join('connection', 'BridgeRouter.kt'),
  join('connection', 'ConnectionStore.kt'),
  join('connection', 'ConnectionStoreManager.kt'),
  join('connectors', 'ManagedWebRTCConnector.kt'),
  join('network', 'BridgeServer.kt'),
  join('network', 'NetworkStateManager.kt'),
  join('transfer', 'FileTransferManager.kt'),
  join('transfer', 'FileTransferProtocol.kt'),
  join('transfer', 'TransferTaskStore.kt'),
  join('transport', 'ChannelManager.kt'),
  join('transport', 'Heartbeat.kt'),
  join('transport', 'WebRTCTransport.kt'),
]) {
  if (existsSync(join(sourceRoot, path))) fail(`obsolete network owner returned: ${path}`)
}

const manifest = join(androidRoot, 'app', 'src', 'main', 'AndroidManifest.xml')
const buildGradle = join(androidRoot, 'app', 'build.gradle')
const capacitorConfig = join(mobileRoot, 'capacitor.config.ts')
for (const path of [manifest, buildGradle, capacitorConfig, join(androidRoot, 'app', 'src', 'main', 'res', 'xml', 'network_security_config.xml')]) {
  requireFile(path, path)
}
const manifestText = readFileSync(manifest, 'utf8')
for (const fragment of ['android.permission.ACCESS_NETWORK_STATE', 'android.permission.VIBRATE', 'android.permission.CAMERA', 'android:networkSecurityConfig']) {
  if (!manifestText.includes(fragment)) fail(`manifest lost required fragment: ${fragment}`)
}
const gradleText = readFileSync(buildGradle, 'utf8')
for (const fragment of [
  "apply plugin: 'kotlin-android'",
  '// anytty NativeConnection dependencies',
  'shrinkResources true',
  "main.proto.srcDir '../../../../proto'",
  "'cloud.anytty.com:443'",
  "'cloud.anytty.com'",
]) {
  if (!gradleText.includes(fragment)) fail(`Gradle configuration lost required fragment: ${fragment}`)
}
const capacitorConfigText = readFileSync(capacitorConfig, 'utf8')
if (!capacitorConfigText.includes("loggingBehavior: 'none'")) {
  fail('Capacitor framework logging could expose native bridge bearer tokens')
}

const androidBuildFiles = [
  buildGradle,
  join(androidRoot, 'build.gradle'),
  join(androidRoot, 'settings.gradle'),
  join(androidRoot, 'capacitor.settings.gradle'),
  join(androidRoot, 'gradle.properties'),
  join(androidRoot, 'app', 'capacitor.build.gradle'),
  join(repoRoot, 'scripts', 'build-android-client.sh'),
  join(repoRoot, 'scripts', 'build-android-client-windows.ps1'),
]
for (const path of androidBuildFiles) requireFile(path, relative(repoRoot, path))

const productionBoundaryFiles = [
  ...androidBuildFiles,
  ...collectProductionFiles(join(repoRoot, 'client', 'binding', 'cabi', 'androidlib'), ['.go']),
  ...collectProductionFiles(join(androidRoot, 'app', 'src', 'main', 'cpp'), ['.c', '.h']),
  ...collectProductionFiles(join(androidRoot, 'app', 'src', 'main', 'java'), ['.java', '.kt']),
]
const forbiddenProductionMarkers = [
  'ANYTTY_ANDROID_GO_TAGS',
  'anytty_android_spike',
  'createSpike',
  'android-spike-daemon',
  'android-managed-1',
  'anytty-go-client-%d',
]
for (const path of productionBoundaryFiles) {
  const source = readFileSync(path, 'utf8')
  for (const marker of forbiddenProductionMarkers) {
    if (source.includes(marker)) {
      fail(`forbidden Android dev/spike marker ${JSON.stringify(marker)} found in ${relative(repoRoot, path)}`)
    }
  }
}

console.log('Android Gradle source integrity passed')
