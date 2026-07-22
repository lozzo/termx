# APPUX001 Android 首次使用与设备信息架构

## 结论

APPUX001 已完成 Android 首次使用、设备列表/详情、扫码与手工码并列入口，以及 terminal/file 剩余用户文案迁移。共享 UI 继续只消费现有 Go Client Engine、Proto command/event 和平台扫码能力，没有在 TypeScript、Kotlin 或 Java 中增加网络、认证、重连或 Route 真值。

## 用户行为

- 未登录且没有设备时，首屏同时提供“登录 Muxvia Cloud”和“添加本地设备”，不再把 Cloud 登录当成本地使用前置条件。
- 添加本地设备页同时展示摄像头扫码与 `MXP1` 配对码/分享链接输入；拒绝相机权限后保留手工输入，并显示明确的回退提示。
- 设备列表优先展示友好名称和 hostname；Device ID、平台、来源 Hub 等技术信息只进入设备详情。
- terminal、文件浏览、传输、预览、取消、粘贴确认和工具栏用户文案全部进入英文/简体中文 locale。
- Direct/SSH 继续由同一个本地设备分享链接入口进入，Cloud 由独立登录入口进入；三者仍复用 Go-owned Endpoint/Route/session，不在 UI 建立平行连接模型。

## 自动化准入

- `make test-clients`：通过；共享 UI `43` 个测试文件、`142` 条测试，Mobile `3` 个测试文件、`29` 条测试。
- `node scripts/check-i18n.mjs`：共享 App UI 英文/简体中文 `405` 个 key 对称。
- `make test-android`：通过；standard/devcloud APK 构建、Android unit test 和 single-App Cloud boundary 校验通过。
- `git diff --check`：通过。

## ARM64 模拟器验收

- AVD：`termx-pa005n1`
- 设备：`emulator-5554`
- ABI：ARM64
- API：35
- APK：`.artifacts/android/app-devcloud-debug.apk`
- SHA-256：`3762bb73b402f8a34cf1a5db1c5eeb11790667b79cc28f6937f150c3cd1266ed`

真实 App UI 已覆盖：

1. 英文未登录首屏和本地设备添加入口。
2. 扫码与手工配对码/分享链接同屏。
3. 拒绝相机权限后仍可手工输入，且提示不再退化为通用错误。
4. 简体中文首屏。
5. `150%` 系统字体下的中文竖屏。
6. `150%` 系统字体下的中文横屏可滚动布局。
7. Cloud 登录入口与 Direct/SSH 共用本地分享入口 smoke。
8. `logcat` 未发现 `FATAL EXCEPTION`、ANR、SIGSEGV、SIGABRT 或 Go/native crash。

截图证据位于本地忽略目录 `.artifacts/appux001/`，不作为运行时或发布资产提交。

## 延后边界

本切片不重复执行 terminal/file 的完整公网交互，也不处理 R2、Controller/Edge 恢复、quota 或 suspend。Web/App 全用户流程由下一切片 `UXE2E001` 从真实 UI 发起验收。
