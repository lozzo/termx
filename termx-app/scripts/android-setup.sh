#!/bin/bash
# termx Android 项目初始化脚本
# 在 npx cap add android 之后运行
# 用法: bash scripts/android-setup.sh

set -e
cd "$(dirname "$0")/.."

ANDROID_DIR="android"
BUILD_GRADLE="$ANDROID_DIR/app/build.gradle"
MANIFEST="$ANDROID_DIR/app/src/main/AndroidManifest.xml"

if [ ! -d "$ANDROID_DIR" ]; then
  echo "错误: android 目录不存在，请先运行 npx cap add android"
  exit 1
fi

sedi() {
  if [ "$(uname)" = "Darwin" ]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

echo "=== termx Android 项目初始化 ==="

# ─── 1. 添加权限 ────────────────────────────────────────────────────────────

add_permission() {
  local perm="$1"
  local attrs="$2"
  if ! grep -q "android.permission.$perm" "$MANIFEST"; then
    if [ -n "$attrs" ]; then
      sedi "/<uses-permission android:name=\"android.permission.INTERNET\"/a\\
    <uses-permission android:name=\"android.permission.$perm\" $attrs />" "$MANIFEST"
    else
      sedi "/<uses-permission android:name=\"android.permission.INTERNET\"/a\\
    <uses-permission android:name=\"android.permission.$perm\" />" "$MANIFEST"
    fi
    echo "[✓] 添加权限: $perm"
  else
    echo "[·] 权限已存在: $perm"
  fi
}

add_permission "ACCESS_NETWORK_STATE"
add_permission "WAKE_LOCK"
add_permission "VIBRATE"
add_permission "CAMERA"

# ─── 2. 复制原生 Java/Kotlin 文件 ────────────────────────────────────────────

NATIVE_JAVA_DIR="native/android"
TARGET_JAVA_DIR="$ANDROID_DIR/app/src/main/java/com/termx/app"

if [ -d "$NATIVE_JAVA_DIR" ]; then
  mkdir -p "$TARGET_JAVA_DIR"
  for ext in java kt; do
    for jfile in "$NATIVE_JAVA_DIR"/*.$ext; do
      [ -f "$jfile" ] || continue
      cp "$jfile" "$TARGET_JAVA_DIR/"
      echo "[✓] 复制 $(basename "$jfile")"
    done
  done
  for subdir in connectors util connection transport network transfer; do
    if [ -d "$NATIVE_JAVA_DIR/$subdir" ]; then
      mkdir -p "$TARGET_JAVA_DIR/$subdir"
      for ext in java kt; do
        for jfile in "$NATIVE_JAVA_DIR/$subdir"/*.$ext; do
          [ -f "$jfile" ] || continue
          cp "$jfile" "$TARGET_JAVA_DIR/$subdir/"
          echo "[✓] 复制 $subdir/$(basename "$jfile")"
        done
      done
    fi
  done
else
  echo "[!] 未找到 $NATIVE_JAVA_DIR 目录"
fi

# ─── 3. 注入 NativeConnection 依赖 ───────────────────────────────────────────

NATIVE_DEPS_MARKER="// termx NativeConnection dependencies"
if ! grep -q "$NATIVE_DEPS_MARKER" "$BUILD_GRADLE"; then
  python3 - "$BUILD_GRADLE" <<'PYEOF'
import sys
with open(sys.argv[1], 'r') as f:
    content = f.read()
deps = """    // termx NativeConnection dependencies
    implementation 'org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1'
    implementation 'io.github.webrtc-sdk:android:125.6422.07'
    implementation 'org.java-websocket:Java-WebSocket:1.5.6'
    implementation 'com.google.code.gson:gson:2.10'
    implementation 'androidx.lifecycle:lifecycle-process:2.7.0'
    implementation "org.jetbrains.kotlin:kotlin-stdlib:$kotlin_version"
"""
if "dependencies {\n" not in content:
    raise SystemExit("dependencies block not found")
content = content.replace("dependencies {\n", "dependencies {\n" + deps, 1)
with open(sys.argv[1], 'w') as f:
    f.write(content)
print("[✓] 注入 NativeConnection 依赖")
PYEOF
else
  echo "[·] NativeConnection 依赖已注入，跳过"
fi

# ─── 4. 配置 Kotlin Android plugin ─────────────────────────────────────────

ROOT_BUILD="$ANDROID_DIR/build.gradle"
VARIABLES_GRADLE="$ANDROID_DIR/variables.gradle"
if ! grep -q "kotlinVersion" "$ROOT_BUILD"; then
  python3 - "$ROOT_BUILD" <<'PYEOF'
import sys
path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()
content = content.replace(
    "        classpath 'com.android.tools.build:gradle",
    "        classpath 'org.jetbrains.kotlin:kotlin-gradle-plugin:1.9.24' // termx kotlinVersion\n        classpath 'com.android.tools.build:gradle",
    1,
)
with open(path, 'w') as f:
    f.write(content)
PYEOF
  echo "[✓] 添加 Kotlin Gradle plugin"
else
  echo "[·] Kotlin 已配置，跳过"
fi

if ! grep -q "kotlin_version" "$VARIABLES_GRADLE"; then
  python3 - "$VARIABLES_GRADLE" <<'PYEOF'
import sys
path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()
content = content.replace(
    "ext {\n",
    "ext {\n    kotlin_version = '1.9.24'\n",
    1,
)
with open(path, 'w') as f:
    f.write(content)
PYEOF
  echo "[✓] 添加 Kotlin 版本变量"
else
  echo "[·] Kotlin 版本变量已存在，跳过"
fi

if ! grep -q "kotlin-android" "$BUILD_GRADLE"; then
  sedi "/apply plugin: 'com.android.application'/a\\
apply plugin: 'kotlin-android'" "$BUILD_GRADLE"
  echo "[✓] 应用 kotlin-android plugin"
else
  echo "[·] kotlin-android 已应用，跳过"
fi

# ─── 5. 配置 network security ────────────────────────────────────────────────

RES_XML_DIR="$ANDROID_DIR/app/src/main/res/xml"
mkdir -p "$RES_XML_DIR"

if [ -f "native/android/res/xml/network_security_config.xml" ]; then
  cp "native/android/res/xml/network_security_config.xml" "$RES_XML_DIR/"
  echo "[✓] 复制 network_security_config.xml"
fi

# Add networkSecurityConfig to <application> if missing
if ! grep -q 'networkSecurityConfig' "$MANIFEST"; then
  sedi 's|<application|<application android:networkSecurityConfig="@xml/network_security_config" android:usesCleartextTraffic="true"|' "$MANIFEST"
  echo "[✓] 配置 networkSecurityConfig"
else
  echo "[·] networkSecurityConfig 已配置，跳过"
fi

# ─── 6. Release build 优化 ──────────────────────────────────────────────────

if ! grep -q 'shrinkResources' "$BUILD_GRADLE"; then
  python3 - "$BUILD_GRADLE" <<'PYEOF'
import sys
path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()
content = content.replace(
    """        release {
            minifyEnabled false
            proguardFiles getDefaultProguardFile('proguard-android.txt'), 'proguard-rules.pro'
        }""",
    """        release {
            minifyEnabled true
            shrinkResources true
            proguardFiles getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro"
        }""",
    1,
)
with open(path, 'w') as f:
    f.write(content)
PYEOF
  echo "[✓] 配置 release minify"
else
  echo "[·] minify 已配置，跳过"
fi

echo ""
echo "=== 初始化完成 ==="
echo "如需构建，运行: cd $ANDROID_DIR && ./gradlew assembleRelease"
