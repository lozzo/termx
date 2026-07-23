# NETUX001 Android 连接与网络 E2E

## 范围与产物

- 最终 APK：`clients/mobile/android/app/build/outputs/apk/debug/app-arm64-v8a-debug.apk`。
- APK SHA-256：`e18f86d6b2deec436b23f80252dc9f179711c43328a104d32fa7bd46f3fe8aac`。
- 已安装 `base.apk` SHA-256：`e18f86d6b2deec436b23f80252dc9f179711c43328a104d32fa7bd46f3fe8aac`。
- 模拟器：AVD `termx-pa005n1`，`arm64-v8a`，API 35，1080x2400，density 420。
- Endpoint：`NETUX001-emulator`；测试 daemon Direct signaling `:43120`、ICE-TCP `:43121`。
- 用户动作由已安装 App UI 发起；CDP 只点击真实 WebView 控件和读取可访问 DOM，daemon/logcat/文件摘要只作为 oracle。

## 真实 UI 证据矩阵

| 流程 | App UI 操作 | 结果 oracle | 实际结果 | 结论 |
| --- | --- | --- | --- | --- |
| 自动连接 | 打开设备、打开 `netux-final`、进入 Connection & network | Go binding session snapshot | Route `Direct`，path `Direct`，host/host，TCP/TCP，RTT 有值，selection reason `first_ready` | PASS |
| 实时刷新 | 展开 Diagnostics，点击 Refresh | 两次 `ConnectionSnapshotGet` | `2:25:29 / 3031·2808 B` 更新为 `2:25:59 / 3277·3942 B` | PASS |
| 禁用原因 | 查看 Direct/SSH/Cloud 顶层选择 | Go `ConnectionPolicyState` | Direct 可选；SSH 和 Cloud 均禁用并显示 `Not configured`，radio 的 `aria-describedby` 指向对应原因 | PASS |
| 强制与恢复 Auto | 活动 terminal 中从 Direct 切换 Auto，点击 Apply & reconnect 并确认 | Go Endpoint registry 与 session generation | 确认框出现；策略变为 Auto；generation 从 `1` 增加为 `2`；重连仍由 planner 选中 Direct | PASS |
| 失败恢复 | 选择 Direct，在确认后暂时停止测试 daemon | App failure surface | 有界失败后显示 `Retry`、`Restore Auto` 和 `client session is unavailable`，没有无限等待 | PASS |
| 重试 | 恢复同一 daemon，点击 Retry | 新 ReadyPeerSession snapshot | Direct 重新 Ready，generation `3`，host/host、TCP/TCP、RTT 和 traffic 重新可见 | PASS |
| 大字体竖屏 | 系统字体设为 200%，打开连接弹窗 | DOM 尺寸与截图 | viewport 412x818，document/dialog 横向 overflow 均为 0，文本换行且 footer 可达 | PASS |
| 大字体横屏 | 200% 字体旋转横屏并滚动内容区到底 | DOM rect、scroll 与截图 | viewport 818x388，dialog 576px 宽；内容区 161/824px 独立滚动，固定 footer 无重叠且按钮文字完整 | PASS |
| 可访问语义 | 打开弹窗并检查焦点、modal 与禁用项 | 真实 APK DOM + UI 单测 | `role=dialog`、`aria-modal=true`、标题关联、初始焦点在关闭按钮、四个背景 sibling 均 `aria-hidden + inert`；Tab/Escape/焦点恢复由测试覆盖 | PASS |
| crash scan | 完成上述流程后扫描 logcat | crash buffer 与关键字扫描 | 无 `FATAL EXCEPTION`、`Fatal signal`、`SIGSEGV`、`SIGABRT` 或 tombstone | PASS |

截图位于 `.artifacts/netux001/`：`connection-dialog.png`、`failure-recovery.png`、`font200-portrait.png` 和 `font200-landscape.png`。该目录是本地验收产物，不进入 Git。

## 真值边界

- 连接偏好与可用性来自 Go Endpoint registry/planner；Android/TypeScript 不再根据 route 数组自行猜测。
- 当前连接来自同一个 Go-owned ReadyPeerSession。Refresh 经 `NativeSessionLease -> ProtoBindingSession -> ConnectionSnapshotGet -> ConnectionSnapshotProvider` 重新采样，不复用首次 open snapshot。
- Direct 路径没有 Relay transport，因此显示 `Not provided`。本次 Pion selected candidate stats 未提供稳定 network class，也保持 `Not provided`；两者均未根据 Wi-Fi、模拟器或地址推断。
- 模拟器镜像未预装 TalkBack，只包含 Android Accessibility Menu。本轮在真实 APK 上完成可访问 DOM/焦点检查，并由 `ConnectionInfoDialog.test.tsx` 覆盖键盘焦点行为；未宣称完成真人语音朗读体验测试。

## 准入结果

```text
Proto descriptor / round-trip / generated checks
  passed

Go endpoint / runtime / binding / Direct / SSH / managed / Cloud tests
  passed

UI
  typecheck passed
  45 files / 154 tests passed

Mobile
  4 files / 36 tests passed
  cap:build passed

Android
  testDebugUnitTest assembleDebug passed

git diff --check
  passed
```

同一 UX reviewer 在 Go 策略真值、精确失败重试、实时重采样、焦点约束和最终模拟器证据补齐后复审 PASS；并独立复跑 Go、UI、Mobile、typecheck 与 `git diff --check`。
