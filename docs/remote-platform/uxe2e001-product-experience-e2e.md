# UXE2E001 Web/App 产品体验总验收

## 范围

- 公网 Web Controller：`https://muxvia.com`。
- Android：ARM64、API 35 模拟器 `termx-pa005n1`，真实公网 HTTPS APK。
- 用户动作必须从 Web 或 App UI 发起；CLI、daemon capture、文件系统摘要和日志只作为结果 oracle。
- 本切片不处理 PG004 的 R2 恢复、Edge Presence 恢复、网络切换自动重建或 CLOUDP007 商业能力矩阵。

## 当前切片修复

1. Android 设置页把 Cloud 账号登录前移到语言、诊断和终端外观之前，首次登录不再需要跨越整页终端设置。
2. 设备列表不再把 `device-*` 技术 ID 当作可见名称；优先使用用户名称或 hostname，否则显示本地化的 `Muxvia daemon`。
3. Android 的 workspace、inventory 与文件传输复用同一个 Go binding session owner，并分别取得 UI lease。底层 session 只在 machine runtime 销毁或 native generation 更换时关闭，避免多个 UI 消费者各自 `openSession` 后互相使 generation 失效。
4. 共享底层 connect 不再继承任一 UI lease 的 `AbortSignal`；单个文件操作或 workspace 调用只能取消自己的等待，不能取消其他消费者正在等待的同一次连接。

## Web 证据

- 真实 Web UI 完成账号注册/登录、手机 activation 创建与批准、daemon enrollment 创建与批准。
- 真实 daemon 使用 Web 批准后的单次码完成 enrollment；Android 使用同一账号登录并同步 daemon。
- Playwright 对公网 Web 完成 `360x780`、`768x900`、`1280x900`、`1440x960` 四档英文 150% 缩放走查；四档 `scrollWidth - clientWidth = 0`。
- `360x780` 与 `1440x960` 另完成简体中文 150% 缩放走查；主导航概览/设备/套餐/账号全部可达，横向溢出为 0。
- 截图位于 `.artifacts/uxe2e001-web/`。

## Android 证据

- Web 创建并批准手机 activation；App 手工输入 `MXA` 登录码完成登录，同一账号设备目录可见。
- App 手工输入 daemon pairing 短码完成 Direct 授权；重新进入设备后能读取 terminal inventory。
- App UI 打开 `uxe2e-shell` 并输入 `pwd`；daemon authoritative capture 返回 `/Users/lozzow/Documents/workdir/termx`。
- App UI 通过 Android SAF 选择 `uxe2e-upload.txt` 并上传到 `/private/tmp/uxe2e-upload.txt`。源文件、远端文件 SHA-256 均为 `fdb61cb06cf18ffc99799302d3364dc49fdd2007e520b7f3ad7e2a5feba84de5`。
- App UI 下载 `/private/tmp/muxvia-uxe2e001/uxe2e-upload.txt` 到 `Downloads/Muxvia/uxe2e-upload.txt`；Android 保存文件 SHA-256 与源文件一致。
- App UI 上传 67 MB fixture 后点击取消；传输中心显示 `cancelled`，远端目录未留下目标文件。
- Android 系统字体设为 150%，英文设备页与简体中文设置页无重叠、裁切或不可达主操作；截图位于 `.artifacts/uxe2e001-android-font150-*.png`。
- 最终公网 HTTPS APK：`.artifacts/android/app-public-https-uxe2e001.apk`，SHA-256 `9ad2ac917fe506f135ce4bab778de81e525392abfcef30299ad5bb2f7386be7b`。模拟器已安装 APK 的 `base.apk` 摘要与该产物完全一致，并在此精确产物上重复通过 terminal `pwd`、20 B 上传/下载摘要校验和 67 MB 上传取消。

## 准入结果

```text
make test-clients
  UI: 43 files / 145 tests passed
  Mobile: 4 files / 32 tests passed
  i18n: App 405 keys, Web 393 keys matched
  typecheck and builds passed

make test-android
  default APK and Dev Cloud APK built
  Android unit tests passed
  single-App Cloud assembly boundary passed

./gradlew -PmuxviaPublicHTTPSStaging=true -PmuxviaArmOnly=true testDebugUnitTest assembleDebug
  passed

git diff --check
  passed
```

Android `logcat` 未发现本轮 `FATAL EXCEPTION`、native crash 或 tombstone 记录。PG004 已登记的账号 refresh、Edge Presence、网络切换 workspace 自动重建和 R2 独立恢复仍按原顺序后续处理，不属于本切片 PASS 条件。

架构 reviewer 与代码 reviewer 在修复共享 connect cancellation ownership、合法 `device-lab` 名称和最终 APK 精确安装证据后均明确 `PASS`，当前切片无阻塞 finding。PG004 的网络切换、Edge Presence、account refresh 与 R2 恢复仅作为不阻塞的后续范围保留。
