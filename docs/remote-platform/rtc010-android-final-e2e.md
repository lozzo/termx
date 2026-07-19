# RTC010 Android 最终 APK 验收

## 验收对象

- APK：`.artifacts/android/app-rtc010-final-devcloud-debug.apk`
- SHA-256：`64f9440275dc2d243bacb23f98d201b352fe92cb31bcc00619004c86d9a708e7`
- 构建：单一 App debug APK，显式 `termxDevCloud=true` 测试 origin；不是独立产品 flavor
- 模拟器：`termx-pa005n1`，`arm64-v8a`，Android API 35
- App：`com.termx.app`，version `1.0` / code `1`
- 安装：`2026-07-19 21:34:07 +08:00`；从设备拉取的 `base.apk`、验收 APK 与 Gradle `app-debug.apk` 的 SHA-256 三者一致
- 用户动作：真实 APK UI，由 WebView DevTools/CDP、ADB 系统按键和 Android SAF 选择器驱动
- 结果 oracle：daemon authoritative capture、目标文件系统、SHA-256、ADB reverse、`lsof` 和 logcat

## 证据矩阵

| 流程 | UI 操作与网络条件 | 实际结果与证据 | 结论 |
| --- | --- | --- | --- |
| Direct | 只保留 `41120 -> 42120` signaling 与 `41121 -> 42121` ICE-TCP；App 打开 Endpoint、terminal 列表和 `rtc010-cloud-final`，连续输入两条命令 | daemon capture 包含 `RTC010_FINAL64_DIRECT_ONE`、`RTC010_FINAL64_DIRECT_TWO_OK`；`.artifacts/rtc010-e2e/final-apk-direct-{capture,reverses}.txt` | PASS |
| LAN/TCP mapping | Direct 使用 ADB reverse 模拟真实 TCP 端口映射，Cloud/SSH 入口移除 | 同一 Direct terminal 交互成功，reverse 清单只含映射后的 Direct TCP 入口 | PASS |
| SSH | 只保留 `41222 -> 42222`；App 使用已导入 SSH Route 和 Android Keystore signer，打开 terminal 并连续输入两条命令 | capture 包含 `RTC010_FINAL64_SSH_ONE`、`RTC010_FINAL64_SSH_TWO_OK`；`lsof` 显示 ADB 与隔离 `sshd` established；私钥未进入 UI/JS；`.artifacts/rtc010-e2e/final-apk-ssh-{capture,established,reverses}.txt` | PASS |
| Cloud | 只保留 `41001 -> 49593` Control、`41002 -> 49594` Hub；App 显示 `TermX Dev Account` / Cloud online，打开 terminal 并连续输入两条命令 | capture 包含 `RTC010_FINAL64_CLOUD_ONE`、`RTC010_FINAL64_CLOUD_TWO_OK`；logcat 为 Hub 路径且 DataChannel `client.channel_bound`；`.artifacts/rtc010-e2e/final-apk-cloud-{capture,reverses,route-log}.txt` | PASS |
| Cloud 隔离 | Cloud 入口移除后分别执行 Direct、SSH 流程 | 两条基础 Route 均无需 Cloud 入口仍可列出并交互 terminal | PASS |
| 上传 | App Files 进入 `/private/tmp/termx-rtc010-final`，点击 Upload，经 Android SAF 选择 `android-upload-source.bin` | UI 显示 `1 done`；客户端与 daemon 文件均为 `3145728` bytes，SHA-256 均为 `60c8dfecb7fdfb9e0bbde727574f483e432f288da3a9b616e373a168ff19a17b`；`final-apk-upload-{ui,sha256}.txt` | PASS |
| 下载 | App 对 `download-source.bin` 执行 Download | 文件保存到 `Downloads/TermX/download-source.bin`；两端均为 `2097152` bytes，SHA-256 均为 `2bcc1455ffc6173367d3a0a09cbbfa901e07b112ef9965b78264b472bb6cbb54`；`final-apk-download-{ui,sha256}.txt` | PASS |
| 取消 | App 对 64 MiB `android-upload-cancel.bin` 发起 Upload，在取消控件首次可见时点击 Cancel | UI 终态为 `cancelled`；实际 UI 目标 `/private/tmp/termx-rtc010-final/android-upload-cancel.bin`、daemon `.termx-upload-*.part`/`.part` 临时文件和对应打开句柄均不存在；pending open 的迟到 resource 由 binding cancelled-execute owner 清理；`final-apk-cancel-ui.txt`、`final-apk-cancel-daemon-cleanup.txt` | PASS |
| 后台恢复 | terminal 打开时按 Home，等待 WebView freeze，再从系统恢复 App 并输入命令 | native bridge loopback port 从 `43771` 更换为 `58411`，`handleForegroundResume` 后重新 `client.channel_bound`；capture 包含 `RTC010_FINAL64_BACKGROUND_OK`；`final-apk-lifecycle-{capture,logcat}.txt` | PASS |
| 网络切换 | Cloud terminal 打开时启用再关闭 airplane mode | Android `onAvailable` 重启 Go bridge并发出 `generationChanged`，UI 替换 binding client 后重新绑定；capture 包含 `RTC010_FINAL64_NETWORK_OK`；`final-apk-network-switch-{capture,logcat}.txt` | PASS |
| 受控弱网 | 模拟器设置 `network delay 300`、`network speed edge`，等待连接稳定后交互并恢复 `none/full` | 网络 epoch 变化先产生有界 `Go binding backend is closed`，在相同弱网条件下重建 App session 后可靠有序 DataChannel 完成输入输出；capture 包含 `RTC010_FINAL64_WEAK_RECOVERED_OK`，无无界等待、ANR 或 native crash；`final-apk-weak-network-{capture,logcat,crash-scan}.txt` | PASS |
| 稳定性 | 完整流程后扫描 logcat、AndroidRuntime、native fatal、ANR，并运行 Android instrumentation | `final-apk-crash-scan.txt` 为空；`:app:connectedDebugAndroidTest` 在 `termx-pa005n1` 完成 7/7；最终 APK 与 Gradle 输出哈希一致 | PASS |

## 代码与仓库准入

- `go test ./...`：PASS。
- `go test -race ./...`：PASS。
- `npm test -- --run`（mobile）：23/23 PASS；`npm run build` PASS。
- `npm test -- --run`（UI）：134/134 PASS；`npm run typecheck` PASS。
- `make doctor`：generated code、Android source integrity、repository layout PASS。
- `:app:connectedDebugAndroidTest`：7/7 PASS；最终源码的 `:app:assembleDebugAndroidTest` PASS；显式 public-key fixture instrumentation 1/1 PASS。
- `git diff --check`、`gofmt -d`：PASS。
- 旧路径扫描：无 `stdio-proxy` 构造/执行、进程型 OpenSSH transport、旧 App product flavor 或独立 managed-only pairing runtime；TUI 既有错误展示测试中的历史字符串不构成运行路径。
- 平台真值扫描：Android registry 只持久化 opaque serialized Proto；Endpoint/Route/session truth 仍由 Go Client Engine 拥有。

## 修复记录

最终 E2E 实际发现并修复了四类当前切片问题：Android 下载原先未落入系统 Downloads、SAF 返回后旧 generation 被复用、窗口内多 chunk 上传 ACK 被误判、网络切换后 WebView 仍持有关闭的 binding backend。文件取消同时补齐了 pending-open 的 binding owner、session-owned resource 的 fail-closed 清理和迟到错误终态保护；cleanup 失败会撤销精确 generation、关闭底层 ReadyPeerSession 和全部 shared lease，daemon `FILE_ERROR` 也会唤醒被窗口 credit 阻塞的上传。全量 race 另暴露并修复了 protocol `failAll` 持锁通知 completed waiter 的死锁。所有修复均有最小 harness，并在同一最终 APK 上重新完成对应流程。
