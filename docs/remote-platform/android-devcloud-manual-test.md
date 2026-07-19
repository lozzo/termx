# Android Dev Cloud 手测

状态：历史 Cloud 验收已完成；当前命令按 RTC009 单一 App 装配更新。

本清单验证同一个 TermX App 的显式 `dev-local` Cloud profile。标准 APK 仍包含 first-party Cloud module，但没有 development origin时 managed Route fail closed；Direct/SSH 始终独立可用。生产 OAuth/TLS 不由本清单替代。

## 1. 前置条件

- Android 设备已开启 USB 调试，`adb devices -l` 显示状态 `device`。
- 手机与 daemon 开发机处于可互通的同一局域网。`adb reverse` 只转发 Control Plane/Hub 的 TCP HTTP，不转发 WebRTC UDP。
- 开发机已安装 `jq`。扫码导入可选安装 `qrencode`；没有时使用 App 的手工文本入口。
- 仓库根目录是当前工作目录。

## 2. 构建产物

完整单 App 边界：

```bash
make test-clients
make test-android
```

显式 dev cloud APK：

```bash
npm run cap:build
cd clients/mobile/android
./gradlew -PtermxDevCloud=true testDebugUnitTest assembleDebug
cd ../../..
cp clients/mobile/android/app/build/outputs/apk/debug/app-debug.apk \
  .artifacts/android/app-devcloud-debug.apk
```

`app-debug.apk` 不启用 dev gateway；ADB 手测必须安装 `app-devcloud-debug.apk`。
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
adb install -r .artifacts/android/app-devcloud-debug.apk
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

## 7. 局部失败与 Direct/SSH 隔离

移除 Hub reverse 后强制一次新连接：

```bash
adb reverse --remove tcp:41002
adb shell am force-stop com.termx.app
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

当前 managed Route 必须显示稳定失败/可重试状态，不能改走旧 Hub 或 Web Controller。其它已配置 Direct/SSH Endpoint 必须仍可连接。恢复映射后重试：

```bash
adb reverse tcp:41002 "tcp:$HUB_PORT"
```

标准 APK fail-closed 检查会替换同 application ID 的当前 APK：

```bash
adb install -r .artifacts/android/app-debug.apk
adb shell am force-stop com.termx.app
adb shell monkey -p com.termx.app -c android.intent.category.LAUNCHER 1
```

标准 APK 对 managed Route 必须返回稳定的 Cloud 未配置/未登录错误，不能建立隐藏 cloud 连接；Direct/SSH 不受影响。完成后重新安装 `app-devcloud-debug.apk`。

## 8. 结果记录

只有以下项目全部观察到，才能把 `workflow.md` 的 CLOUD005 改为完成：

- ADB 安装和启动成功。
- pairing import 成功且无 grant 泄漏。
- 真实 daemon List/Attach/Input/Output 成功。
- 2 秒和 10 秒后台恢复成功。
- Hub reverse 失败保持 endpoint 局部且无 fallback。
- 标准 APK 的 managed Route fail closed，Direct/SSH 保持可用。

### 2026-07-12 历史实测

以下记录来自旧双构建时期，仅保留仍能说明 Cloud 消息链路和失败边界的历史证据；产物名与 flavor 结论不再是当前构建基准。

- 设备：`24129PN74C`，Android 16；Official dev APK 安装、启动和保留应用数据覆盖安装均成功。
- pairing：清空旧应用数据后通过 native importer 导入，raw grant 进入 Android Keystore；页面和 Web storage 只暴露 endpoint metadata 与 `grant_ref`。
- 正向链路：依次观察到 resolving、signaling、connecting、authorizing、connected；同一 `protocol` DataChannel 完成真实 daemon List、Attach、Input、Output。
- live screen：初始屏幕使用 core-v2 `live.screen.get`；输入后由 `live.invalidation.next` 通知更高 revision，再拉取权威屏幕并立即重绘。
- 输入输出：`android-e2e-ok`、`android-live-ok` 均在同一 `android-e2e` terminal 中得到真实 shell 回显。
- presence：daemon 从 06:39 持续到 07:15 后仍可被新 App 进程 resolve，跨越多个两分钟 admission TTL；期间没有新的 `managed cloud presence stopped`，证明每轮使用 fresh proof 续约。
- 恢复：2 秒和 10 秒后台后都经过 verifying 回到 connected，machine identity 未变化，并分别继续输入 `resume-2s-ok`、`resume-10s-ok`。
- Hub 局部失败：移除 `tcp:41002` 后新连接停在 signaling 并进入 failed，没有 WebRTC connected，也没有 local、SSH、旧 Hub 或 Web Controller fallback；恢复 reverse 后同一 endpoint 重新连接成功。
- 日志：对完整 logcat 扫描未发现测试输入、`termx-grant-v1`、账号 token、Hub ticket 或 CapabilityGrant；terminal diagnostics 只记录长度、revision 和帧统计。
- 准入：当时的 `remote` 全量测试、clean-env `cmd/termx` 测试、客户端测试和 Android 构建均通过；当前准入以 `workflow.md` 为准。

## 9. CLOUD011 Control Plane 中断验收

使用公网 HTTP staging Official development APK 登录一次并缓存 edge credential/HubDirectory，确认 direct 与显式 `Use relay` 各成功一次。随后只阻断手机到 Control Plane `41101/tcp`，保持 Hub `41102/tcp` 和 TURN `41003/udp` 可达，强制停止并重新启动 App。

成功标准：不重新登录、不领取 managed admission；新 direct 必须回到 `connected/direct`，新 Relay 必须回到 `connected/single_relay`。Nginx access log 在中断窗口只能看到 Hub 的 `/v1/endpoints/resolve`、`/v1/relay/leases/acquire` 和 `/v1/signaling/create`，不能出现连接阶段 Control Plane 请求；token/directory 过期时必须 fail closed 并提示刷新，不能接受旧 Hub、local 或 SSH fallback。

### 2026-07-12 CLOUD011 实测

- 设备 `24129PN74C`（Android 16）无 ADB reverse，安装当前 Official public HTTP staging APK；使用 WebView CDP 完成干净 pairing 导入和 DOM 状态检查。
- 首次启动只执行一次 `/v1/login/begin`、`/v1/login/complete`，随后 direct 为 `connected/direct`、`prflx / host`。
- 真机测试暴露原 gateway 只在进程内保存 `AccountSession`；已改为独立 Android Keystore AES-GCM session store，并补进程重建、Hub 变更和 directory version 回滚 harness。SharedPreferences 不保存 token 明文。
- 服务器仅拒绝该手机公网 IP 到 `41101/tcp`，保持 Hub `41102/tcp` 与 TURN `41003/udp` 开放；强停并重建 App 进程后新 direct 仍为 `connected/direct`。
- 同一中断窗口切换 `Use relay` 后为 `connected/single_relay`、`relay / host`，RTT 62 ms。Nginx 只出现 `/v1/endpoints/resolve`、`/v1/relay/leases/acquire`、`/v1/signaling/create`，没有 login 或 admission。
- 测试结束已删除临时 iptables 拒绝规则；三个 staging 服务保持 active。`make test-android` 与 APK class boundary 通过。

### 2026-07-12 公网 HTTP staging 真机实测

- 使用当时的公网 HTTP staging profile 构建并覆盖安装旧 Official debug APK；当前等价属性为 `-PtermxPublicHTTPStaging=true`。手机全程走 5G，`adb reverse --list` 为空。
- 从安全渠道导入短期 pairing 后，`Public staging daemon` 完成 List/Attach/Input/Output；`echo android-mobile-input-ok` 在同一远端 shell 得到回显。
- Connection Info 显示 `P2P direct`、`prflx / host`、手机公网映射到 daemon 公网 UDP 候选，实测 RTT 约 51-64 ms。
- 后台 8 秒再启动 App 后恢复到同一 terminal，core-v2 屏幕和输入回显仍在，没有创建第二份 terminal lifecycle truth。
- 修复 Android `Use relay` 只更新 UI 偏好但复用旧 native P2P store 的问题：`forceRelay=true` 现在进入 native 时收敛为 `relay_only`，模式不一致的旧 store 会被释放重建。
- 真机诊断证明 `114.66.58.243:41003/udp` 双向可达，手机与 daemon 均成功完成 TURN allocation；此前“云安全组阻断”判断不成立。实际失败是 daemon 与 TURN 同机时也强制 `ICETransportPolicyRelay`，Pion allocation 成功却无法发布可用 daemon relay candidate，answer 因此没有 remote ICE candidate。
- Answerer 现在先验证 relay-only offer 只含 `typ relay` candidate，再显式发布 daemon gathering candidate，并允许同机 daemon 使用 host candidate。5G 真机最终显示 `Mode=Relay`、`Path=single_relay`、`Candidates=relay / host`，RTT 49 ms；protocol channel 和 terminal inventory 均成功，未回退 direct。

## 10. CLOUD012 统一账号与节点归属验收

使用公网 HTTP staging Official APK，从 App 设置页发起设备码登录。系统浏览器必须显示与 App 相同的 user code；注册或登录 Web 账号并批准后，App 只能取得该账号的 edge session，不得接收密码或浏览器 Cookie。随后使用同一账号签发的一次性 daemon enrollment code 注册 daemon，并从 App 导入该账号名下 pairing。

成功标准：App 显示 Web 账号身份和该账号名下节点；terminal inventory 请求通过端到端 DataChannel 到达 daemon；direct 与显式 single Relay 分别显示真实 ICE candidate pair。只阻断手机到 Control Plane `41101/tcp` 后，两种模式的新连接仍由 Hub 完成；强停 App 后 Keystore Session 和 pairing 可恢复。有效缓存或委派预算耗尽时必须 fail closed，不得伪造成功或回退 local、SSH、旧 Hub。

### 2026-07-13 CLOUD012 实测

- 设备 `24129PN74C`（Android 16）安装当前 Official public HTTP staging APK；App 打开系统浏览器，浏览器展示的 user code 与 App 一致，新注册 Web 账号批准后 App 显示同一账号身份。
- 使用该账号的一次性 enrollment code 注册已有 DeviceIdentity；Web 用户中心、Hub device projection 和 daemon 的 DeviceID 一致，节点在线且未撤销。enrollment code 使用后立即失效。
- 导入账号名下 pairing 后打开节点，terminal inventory 返回 `No active terminals`。该结果来自真实 daemon List 响应，证明 Hub admission、DTLS、CapabilityGrant 验证和 DataChannel protocol 链路成立；文件管理同时成功读取 daemon 根目录。
- direct 显示 `Mode=P2P direct`、`Path=direct`、`Candidates=prflx / host`，手机公网候选连接 daemon 公网 UDP 候选，实测 RTT 约 109-157 ms。
- 显式 `Use relay` 显示 `Mode=Relay`、`Path=single_relay`、`Candidates=relay / host`，实测 RTT 约 92-114 ms，没有复用旧 direct store 或伪装 Relay。
- 服务器只拒绝该手机公网 IP 到 Control Plane `41101/tcp`，保持 Hub `41102/tcp` 和 TURN 可达；中断窗口内 direct 与 single Relay 均重新建连成功。Nginx 只记录 Hub `/v1/endpoints/resolve`、`/v1/signaling/create`、`/v1/relay/leases/acquire`，没有 login 或 admission 请求。
- 在 Control Plane 仍被阻断时强停并重建 App 进程，Keystore 账号 Session 和 native pairing store 均恢复，节点列表显示一个可用账号节点；切换 P2P 后再次以 `prflx / host` 建连成功。
- 多次强停留下的短期 Relay allocation 最终使再次 Relay 请求返回 Managed Free `quota_exhausted`。此前全新进程和 Control Plane 中断窗口内的 Relay 已分别成功；该错误证明并发预算在 Hub/Relay 本地 fail closed，没有回退 direct 或其他 transport。
- 测试结束删除临时 Control Plane 拒绝规则；Cloud、Companion、daemon 三个 staging unit 保持 active。App 保留登录、pairing 和 direct 连接状态，便于继续人工检查。
