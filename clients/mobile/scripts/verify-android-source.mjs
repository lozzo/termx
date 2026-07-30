#!/usr/bin/env node

import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const mobileRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = resolve(mobileRoot, '..', '..')
const androidRoot = join(mobileRoot, 'android')
const appRoot = join(androidRoot, 'app')
const mainRoot = join(appRoot, 'src', 'main')
const javaRoot = join(mainRoot, 'java', 'com', 'anytty', 'app')
const expectedCsp = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data: blob:; media-src 'self' data: blob:; worker-src 'self'; child-src 'self'; connect-src 'self' ws://127.0.0.1:*; object-src 'none'; base-uri 'none'; frame-src 'none'; form-action 'none'; manifest-src 'none'"

function fail(message) {
  throw new Error(`Android source contract failed: ${message}`)
}

function requireFile(path, label = relative(repoRoot, path)) {
  if (!existsSync(path) || !statSync(path).isFile() || statSync(path).size === 0) {
    fail(`required file is missing or empty: ${label}`)
  }
  return readFileSync(path, 'utf8')
}

function requireIncludes(text, fragments, label) {
  for (const fragment of fragments) {
    if (!text.includes(fragment)) fail(`${label} lost ${JSON.stringify(fragment)}`)
  }
}

function forbidIncludes(text, fragments, label) {
  for (const fragment of fragments) {
    if (text.includes(fragment)) fail(`${label} contains forbidden ${JSON.stringify(fragment)}`)
  }
}

function requireMatch(text, pattern, label) {
  if (!pattern.test(text)) fail(`${label} does not match ${pattern}`)
}

if (existsSync(join(mobileRoot, 'native', 'android'))) fail('duplicate native/android source mirror exists')

const requiredMainFiles = [
  'MainActivity.java',
  'AnyTTYDebugEvent.kt',
  'AnyTTYLocalUrl.java',
  'AnyTTYWebChromeClient.java',
  'AnyTTYWebViewClient.java',
  'NativeConnectionPlugin.kt',
  'NativeFilePickerPlugin.kt',
  join('goclient', 'BridgeTransport.kt'),
  join('goclient', 'GoClientBridgeServer.kt'),
  join('goclient', 'GoClientNative.kt'),
]
for (const path of requiredMainFiles) requireFile(join(javaRoot, path), path)

const deletedOwners = [
  join(javaRoot, 'AnyTTYDebugLog.kt'),
  join(mainRoot, 'res', 'xml', 'file_paths.xml'),
  join(mobileRoot, 'src', 'nativeDebugLog.ts'),
  join(androidRoot, 'gradle', 'dependency-locks', 'capacitor-cordova-android-plugins.lockfile'),
]
for (const path of deletedOwners) {
  if (existsSync(path)) fail(`deleted owner returned: ${relative(repoRoot, path)}`)
}

const appGradle = requireFile(join(appRoot, 'build.gradle'))
requireIncludes(appGradle, [
  'ndkVersion = "27.2.12479018"',
  'coreLibraryDesugaringEnabled = true',
  'buildConfig = true',
  'shrinkResources = true',
  "implementation 'org.java-websocket:Java-WebSocket:1.5.6'",
  "implementation 'org.slf4j:slf4j-nop:2.0.6'",
  "resolutionStrategy.force 'org.slf4j:slf4j-api:2.0.6', 'org.slf4j:slf4j-nop:2.0.6'",
  'tasks.register("verifyReleaseSlf4j")',
  "main.proto.srcDir '../../../../proto'",
], 'app/build.gradle')
forbidIncludes(appGradle, ['aaptOptions', 'implementation fileTree', "project(':capacitor-cordova-android-plugins')"], 'app/build.gradle')

const rootGradle = requireFile(join(androidRoot, 'build.gradle'))
const variablesGradle = requireFile(join(androidRoot, 'variables.gradle'))
requireIncludes(rootGradle, ["kotlin-gradle-plugin:2.2.20"], 'android/build.gradle')
requireIncludes(variablesGradle, ["kotlin_version = '2.2.20'"], 'android/variables.gradle')
forbidIncludes(rootGradle + variablesGradle, ['1.9.24'], 'Kotlin Gradle configuration')

const settingsGradle = requireFile(join(androidRoot, 'settings.gradle'))
const capacitorBuild = requireFile(join(appRoot, 'capacitor.build.gradle'))
forbidIncludes(settingsGradle, ['capacitor-cordova-android-plugins'], 'settings.gradle')
forbidIncludes(capacitorBuild, ['cordova.variables.gradle', 'capacitor-cordova-android-plugins'], 'app/capacitor.build.gradle')

const trackedGradleFiles = execFileSync('git', ['ls-files', 'clients/mobile/android/*.gradle', 'clients/mobile/android/**/*.gradle'], {
  cwd: repoRoot,
  encoding: 'utf8',
}).trim().split('\n').filter(Boolean)
for (const tracked of trackedGradleFiles) {
  const source = requireFile(join(repoRoot, tracked), tracked)
  if (/\bflatDir\b/.test(source)) fail(`controlled Gradle source contains flatDir: ${tracked}`)
  if (/implementation\s+fileTree\s*\(/.test(source)) fail(`controlled Gradle source contains a fileTree jar dependency: ${tracked}`)
}

const manifest = requireFile(join(mainRoot, 'AndroidManifest.xml'))
requireIncludes(manifest, [
  'android:allowBackup="false"',
  'android:fullBackupContent="@xml/backup_rules"',
  'android:dataExtractionRules="@xml/data_extraction_rules"',
  'android:usesCleartextTraffic="false"',
  'android:networkSecurityConfig="@xml/network_security_config"',
], 'main AndroidManifest.xml')
forbidIncludes(manifest, ['<provider', 'android:debuggable=', 'android:testOnly=', 'android.intent.action.VIEW', 'android.intent.category.BROWSABLE'], 'main AndroidManifest.xml')

const debugManifest = requireFile(join(appRoot, 'src', 'debug', 'AndroidManifest.xml'))
requireMatch(debugManifest, /<provider\b[\s\S]*android:name="androidx\.core\.content\.FileProvider"[\s\S]*android:authorities="\$\{applicationId\}\.debugprovider"[\s\S]*android:exported="false"[\s\S]*android:resource="@xml\/debug_file_paths"/, 'debug AndroidManifest.xml')
const debugPaths = requireFile(join(appRoot, 'src', 'debug', 'res', 'xml', 'debug_file_paths.xml'))
requireMatch(debugPaths, /^<\?xml[^>]*>\s*<paths[^>]*>\s*<cache-path name="anytty_debug_share" path="anytty-debug-share\/" \/>\s*<\/paths>\s*$/s, 'debug_file_paths.xml')

const capacitorConfig = requireFile(join(mobileRoot, 'capacitor.config.ts'))
requireIncludes(capacitorConfig, [
  "loggingBehavior: 'none'",
  "hostname: 'localhost'",
  "androidScheme: 'http'",
  'allowNavigation: []',
  'allowMixedContent: false',
], 'capacitor.config.ts')

const indexHtml = requireFile(join(mobileRoot, 'index.html'))
const cspTag = `<meta http-equiv="Content-Security-Policy" content="${expectedCsp}" />`
requireMatch(indexHtml, new RegExp(`<meta charset="UTF-8" \\/>\\s*${escapeRegExp(cspTag)}`), 'index.html CSP adjacency')

const webChromeClient = requireFile(join(javaRoot, 'AnyTTYWebChromeClient.java'))
requireIncludes(webChromeClient, [
  'resources.length == 1',
  'PermissionRequest.RESOURCE_VIDEO_CAPTURE.equals(resources[0])',
  'request.deny();',
  'callback.invoke(origin, false, false);',
  'filePathCallback.onReceiveValue(null);',
], 'AnyTTYWebChromeClient.java')
forbidIncludes(webChromeClient, ['RESOURCE_AUDIO_CAPTURE', 'Intent('], 'AnyTTYWebChromeClient.java')

const mainActivity = requireFile(join(javaRoot, 'MainActivity.java'))
requireIncludes(mainActivity, [
  'WebSettings.MIXED_CONTENT_NEVER_ALLOW',
  'setAllowFileAccess(false)',
  'setAllowContentAccess(false)',
  'setAllowFileAccessFromFileURLs(false)',
  'setAllowUniversalAccessFromFileURLs(false)',
  'setGeolocationEnabled(false)',
], 'MainActivity.java')

const bridgeTransport = requireFile(join(javaRoot, 'goclient', 'BridgeTransport.kt'))
requireIncludes(bridgeTransport, [
  'BRIDGE_PHYSICAL_LIMIT = 8',
  'BRIDGE_UPGRADE_LIMIT = 4',
  'BRIDGE_MAX_HEADER_BYTES = 16 * 1024',
  'BRIDGE_AUTH_DEADLINE_NANOS = 2_000_000_000L',
  'BRIDGE_MAX_MESSAGE_BYTES = 4 * 1024 * 1024',
  'class BridgeWebSocketFactory',
  'class BridgeWebSocketImpl',
  'class BridgeDraft6455',
  'override fun copyInstance(): Draft = BridgeDraft6455()',
], 'BridgeTransport.kt')
forbidIncludes(bridgeTransport, ['perOrigin', 'onConnect(false)'], 'BridgeTransport.kt')

const qrScanner = requireFile(join(mobileRoot, 'src', 'nativeQrScanner.ts'))
requireIncludes(qrScanner, ['formatsToSupport:', 'Html5QrcodeSupportedFormats.QR_CODE'], 'nativeQrScanner.ts')

const goBindingClient = requireFile(join(mobileRoot, 'src', 'GoBindingClient.ts'))
requireIncludes(goBindingClient, [
  'new WebSocket(`ws://127.0.0.1:${endpoint.port}`, BRIDGE_PROTOCOL)',
  'socket.protocol !== BRIDGE_PROTOCOL',
  'const auth = new Uint8Array(1 + AUTH_TOKEN_BYTES)',
], 'GoBindingClient.ts')

const releaseLog = requireFile(join(appRoot, 'src', 'release', 'java', 'com', 'anytty', 'app', 'AnyTTYDebugLog.kt'))
forbidIncludes(releaseLog, ['File(', 'Log.', 'Throwable', 'String?', 'message', 'details'], 'release AnyTTYDebugLog')
requireMatch(releaseLog, /fun init\([^)]*\) = Unit/, 'release AnyTTYDebugLog')

const debugLog = requireFile(join(appRoot, 'src', 'debug', 'java', 'com', 'anytty', 'app', 'AnyTTYDebugLog.kt'))
forbidIncludes(debugLog, ['Throwable', 'Uri', 'message:', 'tag:', 'details:', 'logcat', '<T : Enum'], 'debug AnyTTYDebugLog')

const nativeConnection = requireFile(join(javaRoot, 'NativeConnectionPlugin.kt'))
const nativeFilePicker = requireFile(join(javaRoot, 'NativeFilePickerPlugin.kt'))
requireIncludes(nativeConnection, [
  'ProcessLifecycleOwner.get().lifecycle.addObserver(this)',
  'ProcessLifecycleOwner.get().lifecycle.removeObserver(this)',
], 'NativeConnectionPlugin lifecycle ownership')
forbidIncludes(nativeConnection + nativeFilePicker, ['android.util.Log', 'exportDebugLogs', 'writeDebugLog'], 'Android plugins')
if (/\bLog\.(?:d|e|i|v|w|wtf)\s*\(/.test(nativeConnection + nativeFilePicker)) fail('Android plugins call Log.*')

const androidGoLog = requireFile(join(repoRoot, 'client', 'binding', 'cabi', 'androidlib', 'log_android.go'))
const androidGoProduction = requireFile(join(repoRoot, 'client', 'binding', 'cabi', 'androidlib', 'production.go'))
requireIncludes(androidGoLog, ['log.SetOutput(io.Discard)'], 'Android c-shared logging')
requireIncludes(androidGoProduction, ['Logger:  nil'], 'Android Pion logging')
forbidIncludes(androidGoLog + androidGoProduction, ['__android_log_write', '-llog', 'slog.', 'log.Printf', 'log.Println'], 'Android Go logging')

const mobilePackage = JSON.parse(requireFile(join(mobileRoot, 'package.json')))
if (mobilePackage.scripts?.['cap:copy'] !== 'npx cap copy android && node scripts/verify-android-source.mjs') {
  fail('mobile cap:copy must use cap copy android followed by the source gate')
}
if ('cap:sync' in (mobilePackage.scripts ?? {})) fail('legacy cap:sync script remains')
if (mobilePackage.scripts?.['cap:build'] !== 'npm run build && npm run cap:copy') fail('cap:build must use cap:copy')

execFileSync(process.execPath, [
  join(mobileRoot, 'scripts', 'verify-android-artifact-resources.mjs'),
  join(mainRoot, 'res', 'xml', 'network_security_config.xml'),
  join(mainRoot, 'res', 'xml', 'backup_rules.xml'),
  join(mainRoot, 'res', 'xml', 'data_extraction_rules.xml'),
  join(mobileRoot, 'index.html'),
], { stdio: 'inherit' })

const forbiddenMarkers = ['ANYTTY_ANDROID_GO_TAGS', 'anytty_android_spike', 'createSpike', 'android-spike-daemon']
for (const path of [appGradle, nativeConnection, nativeFilePicker, androidGoLog, androidGoProduction]) {
  forbidIncludes(path, forbiddenMarkers, 'Android production boundary')
}

console.log('Android source contract passed')

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
