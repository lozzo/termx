# CONNCOPY001 用户连接文案验收

## 产物与环境

- APK：`clients/mobile/android/app/build/outputs/apk/debug/app-arm64-v8a-debug.apk`。
- APK 与模拟器已安装 `base.apk` SHA-256：`67397c31ef1f97e83af2b91c05db7f852549bdc6b9ebc10af1dc0d648962b691`。
- 模拟器：AVD `termx-pa005n1`，`arm64-v8a`，Android API 35，1080x2400，density 420。
- Direct daemon：隔离 state/config/socket，host signaling `127.0.0.1:43120`、ICE-TCP `127.0.0.1:43121`；设备侧通过 ADB reverse 使用 `127.0.0.1:41120/41121`。
- 本地证据位于 `.artifacts/conncopy001/`，不进入 Git。

## 用户流程与结果

| 流程 | App UI 操作 | 结果 oracle | 实际结果 | 结论 |
| --- | --- | --- | --- | --- |
| 英文连接阶段 | 在真实 App 的添加设备表单输入 daemon 生成的 `MXP1` 短码，点击 Add device | WebView 可访问 DOM 与 Go Direct daemon | 350ms overlay 显示 `Connecting to device`、`Negotiating the ICE connection...`；约 1 秒后连接完成并自动收起 | PASS |
| 当前连接 | 从 workspace 点击 Connection info，展开 Advanced network details | 同一 ReadyPeerSession 的 connection snapshot | Route `Direct`、path `P2P direct`、RTT `28 ms`、host/host、TCP/TCP；标签使用 Connection attempt、ICE candidates、ICE transport | PASS |
| 简体中文 | 从真实设置页把 App language 切换为简体中文，再打开设备与添加设备流程 | `document.documentElement.lang`、可访问 DOM 与截图 | `lang=zh-CN`；设备、设置、短码配对与动作文本均切换为中文，布局无重叠或截断 | PASS |
| 生命周期恢复 | 强制停止并重新启动 App，从 UI 打开相同 Endpoint 与文件页 | Go engine 新 generation、真实 daemon 文件列表 | App 恢复 Endpoint，文件页列出 daemon 根目录；未直接调用 JNI/binding 发起业务动作 | PASS |
| 稳定性 | 完成上述流程后扫描 logcat | Java/native crash 关键字 | 无 `FATAL EXCEPTION`、ANR、`Fatal signal`、`SIGSEGV`、`SIGABRT` 或 tombstone | PASS |

## 显示边界

- `RtcConnectionPhase` 是连接过程显示真值；底层自由 `statusText` 不再覆盖已知 phase，因而 JNI、native runtime、handle 或 Go 实现词不能进入连接 overlay/banner。
- phase 仅投影用户概念：Direct、SSH、P2P、ICE、Relay、设备访问验证、重连和等待网络，不改变 Endpoint/Route/session、鉴权或 generation 行为。
- 诊断数据仍来自 Go-owned ReadyPeerSession；本切片只把 `Generation` 改为用户可理解的 `Connection attempt / 连接轮次`，底层 generation 真值不变。
- 英文和简体中文 locale 均为 461 个 scalar key，键集合完全一致；11 个连接 phase 有完整双语映射测试。

## 准入结果

```text
UI proto generation                 PASS
UI tests                            45 files / 158 tests PASS
UI typecheck / production build     PASS
Mobile tests                        4 files / 36 tests PASS
Mobile Capacitor build/sync         PASS
Android testDebugUnitTest           PASS
Android ARM64 assembleDebug         PASS
APK install/hash/UI smoke/crash     PASS
git diff --check                    PASS
```
