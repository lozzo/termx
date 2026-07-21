# BRAND005 Muxvia 发布候选验收

## 验收对象

- 日期：2026-07-21
- 标准 APK：`.artifacts/android/app-debug.apk`
- 标准 APK SHA-256：`ed15b2269e1e2d34bda1cc14616869fd3ea17d2a2a91e94cd74c7ab0a68258d7`
- Dev Cloud APK：`.artifacts/android/app-devcloud-debug.apk`
- Dev Cloud APK SHA-256：`a59131fdcc803fbe04dedb3404c0c75a0142e3b00b1c8076e01c5e9e817283d8`
- Android applicationId：`com.muxvia.app`
- 模拟器：AVD `termx-pa005n1`，`Android SDK built for arm64`，`arm64-v8a`，Android API 35；AVD 名称是既有本地测试环境标识，不是 APK 发布身份
- Direct daemon：新 `muxvia` 二进制、隔离 socket/state/config，host signaling `127.0.0.1:43120`，host ICE-TCP `127.0.0.1:43121`
- 设备映射：`tcp:41120 -> tcp:43120`、`tcp:41121 -> tcp:43121`

## Android UI 证据矩阵

| 验收项 | 用户动作与预期 | 实际结果 | 证据 | 结论 |
| --- | --- | --- | --- | --- |
| APK 身份 | 安装标准 APK；系统包名和应用名必须为正式身份 | package 为 `com.muxvia.app`，application label 为 `Muxvia`，运行数据目录为 `/data/user/0/com.muxvia.app` | `.artifacts/brand005-e2e/final-apk-{sha256,identity}.txt`、`final-package-runtime.txt` | PASS |
| 模拟器 | 使用真实 ARM64 Android 模拟器 | AVD `termx-pa005n1`，ABI `arm64-v8a`，API 35 | `.artifacts/brand005-e2e/final-avd.txt` | PASS |
| Direct 网络 | App 只能通过设备侧 `41120/41121` 到达隔离 daemon | 最终重启前移除 Cloud reverse；ADB reverse 清单只包含 Direct signaling 与 ICE-TCP 到 host `43120/43121` | `.artifacts/brand005-e2e/final-direct-reverses.txt`、daemon 隔离运行目录 | PASS |
| 品牌首页 | 启动最终 APK；首页不得显示 `TX` 或小写系统应用名 | 首页显示 `MV`，APK label 为 `Muxvia` | `.artifacts/brand005-e2e/09-final-relaunch.png`、`final-apk-identity.txt` | PASS |
| Endpoint 与 terminal 列表 | 从 App UI 打开已导入的 `Muxvia-Brand005` Endpoint；应列出 owning daemon terminal | App 显示 `brand005-direct`、`Running` 和 terminal 尺寸 | `.artifacts/brand005-e2e/11-final-terminal-list.png`、`final-direct-logcat.txt` | PASS |
| Direct session | 从 App UI 打开 terminal；应建立可靠有序 DataChannel | `client.channel_bound` 显示 `proto-terminal:brand005-direct`、`readyState=open`、generation `1` | `.artifacts/brand005-e2e/final-direct-logcat.txt` | PASS |
| terminal 输入输出 | 通过最终 APK 的 xterm 输入 `echo MUXVIA_BRAND005_DIRECT_ONLY_OK \| tee /tmp/muxvia-brand005-direct-only.txt`；UI 和 daemon oracle 都应出现 marker | terminal 画面与主机结果文件均为 `MUXVIA_BRAND005_DIRECT_ONLY_OK` | `.artifacts/brand005-e2e/12-final-apk-command.png`、`final-terminal-oracle.txt` | PASS |
| 稳定性 | 完成流程后扫描 Java/native fatal 与 ANR | 扫描结果 0 行 | `.artifacts/brand005-e2e/final-crash-scan.txt` | PASS |

初次干净数据配对和连续两次交互截图保留在同一证据目录的 `02-mv-home.png`、`04-paired.png`、`05-terminal-open.png`、`08-two-commands.png`。最终 APK 重建后保留已有 Go-owned Endpoint/credential，再由真实 App UI 重新打开 Endpoint、terminal 并输入最终 marker；测试未直接调用 JNI/binding 发起业务动作。一次性配对 bundle 不属于发布证据，不进入 Git。

## 品牌残留结论

- 活动 Go module、Proto package/type URL、CLI、URI、环境变量、C ABI/JNI、Android package、npm scope、UI、Cloud 二进制和活动文档均使用 Muxvia 身份。
- `private/archive/`、`docs/history/` 与 Git 历史按约定保留历史事实，不进入活动门禁。
- 活动目录中的 `termx-core`、`termx-remote`、`termx-hub` 等字符串只存在于依赖守卫、禁止恢复说明、真实 archive 路径、历史手测环境和迁移矩阵；它们用于保存 legacy 事实或阻止旧架构回归，不是活动发布身份。
- `.gitignore` 已迁移为 `muxvia-debug-*.zip`，活动 UI 不再保留 `TX` 字标。

## 准入结果

- `make test`：PASS。
- `make test-private`：PASS。
- `make test-cloud-controller-edge`：PASS，Controller 与两个独立 Edge 进程 harness 通过。
- `make test-clients`：PASS，UI 134/134、mobile 27/27。
- `make test-android`：PASS，标准与 Dev Cloud APK 构建及单 App 边界检查通过。
- `make build-web-controller`：PASS。
- `make doctor`：PASS，generated、Android source integrity 与 repository layout 均通过。
- `git diff --check`：PASS。

构建过程仍有既有 Gradle deprecated API/feature 与 Vite chunk size warning；它们没有形成当前品牌迁移的行为失败，不在 BRAND005 扩大处理。
