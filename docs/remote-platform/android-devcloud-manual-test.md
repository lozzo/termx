# Official Android Dev Cloud 手测

状态：CLOUD005/CLOUD008 ADB 验收完成；CLOUD011 Control Plane 中断验收待设备连接；2026-07-12

本清单只验证显式 `dev-local`、单区域 direct WebRTC。默认 Official 和 Community 继续 fail closed；生产 OAuth/TLS、Android Relay 和公网环境不在本切片内。

## 1. 前置条件

- Android 设备已开启 USB 调试，`adb devices -l` 显示状态 `device`。
- 手机与 daemon 开发机处于可互通的同一局域网。`adb reverse` 只转发 Control Plane/Hub 的 TCP HTTP，不转发 WebRTC UDP。
- 开发机已安装 `jq`。扫码导入可选安装 `qrencode`；没有时使用 App 的手工文本入口。
- 仓库根目录是当前工作目录。

## 2. 构建产物

完整 Community/Official 边界：

```bash
make test-clients
make test-android
```

显式 dev cloud APK：

```bash
npm run cap:build
cd clients/mobile/android
./gradlew \
  -I ../../../private/cloud/mobile/android/official-cloud.init.gradle \
  -PtermxOfficialDevCloud=true \
  testDebugUnitTest assembleDebug
cd ../../..
cp clients/mobile/android/app/build/outputs/apk/debug/app-debug.apk \
  .artifacts/android/official-devcloud-debug.apk
```

`official-debug.apk` 不启用 dev gateway；ADB 手测必须安装 `official-devcloud-debug.apk`。
`npm run cap:build` 负责先构建共享 UI 并同步 Capacitor assets；只运行 Gradle 可能把旧 Web 资源打进 APK。

## 3. 启动 Cloud 与 Daemon

先构建 CLI。在终端 A 前台启动 dev cloud：

```bash
make build
make cloud-dev
```

在终端 B 启动 daemon Companion：

```bash
go run ./private/cloud/companion/cmd/termx-cloud serve \
  --socket /tmp/termx-cloud-daemon.sock \
  --profile daemon-dev \
  --dev-manifest .artifacts/cloud-dev/runtime.json
```

在终端 C 初始化 daemon 身份、签发 pairing bundle，再启动 managed daemon：

```bash
export DAEMON_STATE=/tmp/termx-managed-android-daemon-state
export DAEMON_SOCKET=/tmp/termx-managed-android-daemon.sock
export ENROLLMENT_CODE="$(jq -r '.enrollment_code' .artifacts/cloud-dev/runtime.json)"

TERMX_CLOUD_COMPANION_SOCKET=/tmp/termx-cloud-daemon.sock \
  XDG_STATE_HOME="$DAEMON_STATE" \
  .artifacts/bin/termx cloud enroll "$ENROLLMENT_CODE"

XDG_STATE_HOME="$DAEMON_STATE" \
  .artifacts/bin/termx pair create \
  --label "Android dev daemon" \
  --ttl 24h \
  --out /tmp/termx-android-pairing.json

TERMX_CLOUD_COMPANION_SOCKET=/tmp/termx-cloud-daemon.sock \
  XDG_STATE_HOME="$DAEMON_STATE" \
  .artifacts/bin/termx --socket "$DAEMON_SOCKET" daemon --cloud
```

在终端 D 创建一个真实 terminal，并确认 daemon inventory：

```bash
XDG_STATE_HOME="$DAEMON_STATE" \
  .artifacts/bin/termx --socket "$DAEMON_SOCKET" new \
  --name android-e2e -- /bin/zsh -l

XDG_STATE_HOME="$DAEMON_STATE" \
  .artifacts/bin/termx --socket "$DAEMON_SOCKET" ls
```

## 4. ADB Reverse 与安装

从 manifest 提取随机 host 端口，并映射到 APK 固定的 device 端口：

```bash
export CONTROL_PORT="$(jq -r '.control_plane_url | capture(":(?<port>[0-9]+)$").port' .artifacts/cloud-dev/runtime.json)"
export HUB_PORT="$(jq -r '.hub_url | capture(":(?<port>[0-9]+)$").port' .artifacts/cloud-dev/runtime.json)"

adb reverse tcp:41001 "tcp:$CONTROL_PORT"
adb reverse tcp:41002 "tcp:$HUB_PORT"
adb install -r .artifacts/android/official-devcloud-debug.apk
adb shell am force-stop com.termx.app
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

同时打开脱敏日志观察连接阶段：

```bash
adb logcat -c
adb logcat -s \
  TermxNativePlugin TermxConnStore TermxWebRTC TermxChannelMgr \
  TermxHeartbeat TermxBridgeServer TermxBridgeRouter
```

日志中不得出现 `termx-grant-v1`、pairing JSON、账号 access token、Hub ticket 或 terminal 输入内容。

## 5. 配对与 Terminal

在 App 首页选择扫描新机器，再选择手工输入，把 `/tmp/termx-android-pairing.json` 的完整 JSON 粘贴进去。可选二维码方式：

```bash
qrencode -o /tmp/termx-android-pairing.png < /tmp/termx-android-pairing.json
open /tmp/termx-android-pairing.png
```

成功标准：

- App 只保存 endpoint metadata 与 `grant_ref`，不在 Web storage 展示原始 grant。
- 连接阶段依次经过 resolving、signaling、connecting、authorizing、connected。
- 机器页列出 `android-e2e`，打开后可看到真实 shell。
- 输入 `printf 'android-e2e-ok\n'`，屏幕出现 `android-e2e-ok`。
- 关闭 terminal view 再重新打开，仍 attach 同一 daemon terminal，不创建客户端 lifecycle truth。

## 6. 前后台恢复

快速恢复：

```bash
adb shell input keyevent KEYCODE_HOME
sleep 2
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

较长后台恢复：

```bash
adb shell input keyevent KEYCODE_HOME
sleep 10
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

成功标准：App 可以短暂显示 verifying/reconnecting，随后回到 connected；machine identity 不重复，terminal 可重新 attach 并继续输入。若旧 PeerConnection 不可恢复，只重建该 managed endpoint。

## 7. 局部失败与 Community

移除 Hub reverse 后强制一次新连接：

```bash
adb reverse --remove tcp:41002
adb shell am force-stop com.termx.app
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

当前 managed endpoint 必须显示稳定失败/可重试状态，不能改走旧 Hub、Web Controller、local 或 SSH。恢复映射后重试：

```bash
adb reverse tcp:41002 "tcp:$HUB_PORT"
```

Community fail-closed 检查会替换同 application ID 的当前 APK：

```bash
adb install -r .artifacts/android/community-debug.apk
adb shell am force-stop com.termx.app
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

Community 对 managed endpoint 必须返回 `companion_missing`/官方 cloud module missing，不能建立隐藏 cloud 连接。完成后重新安装 `official-devcloud-debug.apk`。

## 8. 结果记录

只有以下项目全部观察到，才能把 `workflow.md` 的 CLOUD005 改为完成：

- ADB 安装和启动成功。
- pairing import 成功且无 grant 泄漏。
- 真实 daemon List/Attach/Input/Output 成功。
- 2 秒和 10 秒后台恢复成功。
- Hub reverse 失败保持 endpoint 局部且无 fallback。
- Community fail closed。

### 2026-07-12 实测

- 设备：`24129PN74C`，Android 16；Official dev APK 安装、启动和保留应用数据覆盖安装均成功。
- pairing：清空旧应用数据后通过 native importer 导入，raw grant 进入 Android Keystore；页面和 Web storage 只暴露 endpoint metadata 与 `grant_ref`。
- 正向链路：依次观察到 resolving、signaling、connecting、authorizing、connected；同一 `protocol` DataChannel 完成真实 daemon List、Attach、Input、Output。
- live screen：初始屏幕使用 core-v2 `live.screen.get`；输入后由 `live.invalidation.next` 通知更高 revision，再拉取权威屏幕并立即重绘。
- 输入输出：`android-e2e-ok`、`android-live-ok` 均在同一 `android-e2e` terminal 中得到真实 shell 回显。
- presence：daemon 从 06:39 持续到 07:15 后仍可被新 App 进程 resolve，跨越多个两分钟 admission TTL；期间没有新的 `managed cloud presence stopped`，证明每轮使用 fresh proof 续约。
- 恢复：2 秒和 10 秒后台后都经过 verifying 回到 connected，machine identity 未变化，并分别继续输入 `resume-2s-ok`、`resume-10s-ok`。
- Hub 局部失败：移除 `tcp:41002` 后新连接停在 signaling 并进入 failed，没有 WebRTC connected，也没有 local、SSH、旧 Hub 或 Web Controller fallback；恢复 reverse 后同一 endpoint 重新连接成功。
- Community：保留同一 managed endpoint 数据覆盖安装 Community APK，连接立即以 `companion_missing` fail closed，未进入 signaling/WebRTC。
- 日志：对完整 logcat 扫描未发现测试输入、`termx-grant-v1`、账号 token、Hub ticket 或 CapabilityGrant；terminal diagnostics 只记录长度、revision 和帧统计。
- 准入：`remote` 全量测试、clean-env `cmd/termx` 测试、`make test-clients`（62 files / 449 tests）和 `make test-android` 均通过，APK class boundary 通过。
- 产物：Community `sha256:839148503661e2f07bae215d9372086d16b76aea7e2984b288724a9d0585d8bf`；默认 Official `sha256:0600696cd68a3ab789ae9e9a5ed21cf4031a34d6cd2b9c368a038a42c19cf8a0`；Official dev `sha256:b6aa1bab3a652c0ad3ded7ef00a4ceb286718befabf21d8dfca682dffe326466`。
- 设备清理：Community 负向验收后设备从 `adb devices` 消失，当前手机仍安装 Community APK；这不改变已经观察到的产品 DoD，设备重连后应重新执行 Official dev APK 安装命令恢复日常测试环境。

## 9. CLOUD011 Control Plane 中断验收

使用公网 HTTP staging Official development APK 登录一次并缓存 edge credential/HubDirectory，确认 direct 与显式 `Use relay` 各成功一次。随后只阻断手机到 Control Plane `41101/tcp`，保持 Hub `41102/tcp` 和 TURN `41003/udp` 可达，强制停止并重新启动 App。

成功标准：不重新登录、不领取 managed admission；新 direct 必须回到 `connected/direct`，新 Relay 必须回到 `connected/single_relay`。Nginx access log 在中断窗口只能看到 Hub 的 `/v1/endpoints/resolve`、`/v1/relay/leases/acquire` 和 `/v1/signaling/create`，不能出现连接阶段 Control Plane 请求；token/directory 过期时必须 fail closed 并提示刷新，不能接受旧 Hub、local 或 SSH fallback。

当前 `adb devices -l` 为空，本节尚未真机执行，不得据 desktop 或 JVM contract 测试把 CLOUD011 标为完成。

### 2026-07-12 公网 HTTP staging 真机实测

- 使用 `-PtermxOfficialPublicHTTPStaging=true` 构建并覆盖安装 Official debug APK；手机全程走 5G，`adb reverse --list` 为空。
- 从安全渠道导入短期 pairing 后，`Public staging daemon` 完成 List/Attach/Input/Output；`echo android-mobile-input-ok` 在同一远端 shell 得到回显。
- Connection Info 显示 `P2P direct`、`prflx / host`、手机公网映射到 daemon 公网 UDP 候选，实测 RTT 约 51-64 ms。
- 后台 8 秒再启动 App 后恢复到同一 terminal，core-v2 屏幕和输入回显仍在，没有创建第二份 terminal lifecycle truth。
- 修复 Android `Use relay` 只更新 UI 偏好但复用旧 native P2P store 的问题：`forceRelay=true` 现在进入 native 时收敛为 `relay_only`，模式不一致的旧 store 会被释放重建。
- 真机诊断证明 `114.66.58.243:41003/udp` 双向可达，手机与 daemon 均成功完成 TURN allocation；此前“云安全组阻断”判断不成立。实际失败是 daemon 与 TURN 同机时也强制 `ICETransportPolicyRelay`，Pion allocation 成功却无法发布可用 daemon relay candidate，answer 因此没有 remote ICE candidate。
- Answerer 现在先验证 relay-only offer 只含 `typ relay` candidate，再显式发布 daemon gathering candidate，并允许同机 daemon 使用 host candidate。5G 真机最终显示 `Mode=Relay`、`Path=single_relay`、`Candidates=relay / host`，RTT 49 ms；protocol channel 和 terminal inventory 均成功，未回退 direct。
