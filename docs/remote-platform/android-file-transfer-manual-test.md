# Android 文件传输真机验收

本文记录 FILE004 的公网 Official Android 验收。只有上传和下载内容都经过 SHA-256 校验，FILE004 才能完成。

## 当前测试环境

- 公网主机：`root@114.66.58.243`，Ubuntu x86_64。
- 独立目录：`/opt/muxvia-file004`。
- 独立服务：`muxvia-file004-cloud.service`、`muxvia-file004-companion.service`、`muxvia-file004-daemon.service`。
- DeviceID：`device-v4-559N-ft-rcTVYBl1wKw`。
- TerminalID：`file004-public`。
- 服务器测试目录：`/opt/muxvia-file004/test-files`。
- 下载源文件：`download-source.bin`，大小 `2097152` bytes。
- 下载源 SHA-256：`69708069862e7a31f3d449233f45c527a3156ee9dacf581e8cffe886cb7215e0`。
- Official APK：`.artifacts/android/official-devcloud-file-v4-debug.apk`。
- APK SHA-256：`a95a3be2a0d7179186bc90cb05b3746054eaad9a39d05907c0b0c87719478d8f`。
- Pairing：`.artifacts/file004/pairing.json`，owner-only；不得提交其内容。
- 本机 SSH tunnel：`127.0.0.1:41001` 到远端 Control Plane，`127.0.0.1:41002` 到远端 Hub。

验收当时已有的 legacy 进程 `termx-hub`、`tgent-hub`、`termx-web-control` 和 3478/8447 监听保持运行；这些名称只记录历史测试环境，FILE004 服务不覆盖这些进程或端口。

## 设备接入

```bash
adb devices -l
adb reverse tcp:41001 tcp:41001
adb reverse tcp:41002 tcp:41002
adb install -r .artifacts/android/official-devcloud-file-v4-debug.apk
adb shell am force-stop com.muxvia.app
adb shell monkey -p com.muxvia.app -c android.intent.category.LAUNCHER 1
```

期望设备 ID 为 `8d0e6e41`。覆盖安装必须保留应用数据；若 pairing 尚未存在，使用 App 手工导入 `.artifacts/file004/pairing.json` 的完整 JSON。

## 验收证据

- [x] terminal 列表出现并可打开 `file004-public`，强停恢复后仍为 Running。
- [x] 文件页浏览 `/opt/muxvia-file004/test-files`。
- [x] `preview.txt` 显示 `FILE004 public download verification` 与 `line-2`。
- [x] 下载 `download-source.bin` 到 `Download/muxvia/download-source.bin`；Android 与服务器 SHA-256 均为 `69708069862e7a31f3d449233f45c527a3156ee9dacf581e8cffe886cb7215e0`。
- [x] Android picker 上传 `android-upload-source.bin`，大小 `3145728` bytes；手机源与服务器目标 SHA-256 均为 `cb529cec00e00b22e14db65ad98f14983364d82da2fe7f09ce969d2f0264032c`。
- [x] 下载 `cancel-download.bin` 后立即取消，`Download/muxvia/cancel-download.bin` 不存在，私有 `.part` 已删除。
- [x] 上传 `cancelupload` 后立即取消，服务器目标和 `.muxvia-upload-*` 均不存在。
- [x] 下载 `resume-download.bin` 在后台 10 秒推进到 `12976128/67108864` bytes，强停后从 `.part` 恢复并完成；Android 与服务器 SHA-256 均为 `85e3d365619c2f3b49fb3534090256e23b45b9c87ccba18b8315888e27c1c43b`。
- [x] 上传 `resumeupload.bin` 强停前保留 daemon `resume_transfer_id` 和 `7143424` bytes 临时文件；新 session 接管后推进并完成，手机源与服务器目标 SHA-256 均为 `d02405d20342b04f90ab5e3394ca6e3bcec9e571a2cf9ed51ffb6d11d3293194`。
- [x] connection info 实测 `P2P direct`、`Path direct`、公网 remote `114.66.58.243`，RTT 约 `81 ms`。
- [x] single Relay 由 `private/cloud/devcloud` 的真实 TURN/DataChannel harness `TestManagedSingleRelayE2EAcrossRealBoundaries` 验证。当前 Android dev Relay 只监听远端 loopback，ADB 不转发 UDP，因此没有把真机 direct 记录冒充为 Relay。
- [x] 指定 Android 日志经敏感字段扫描，不含 capability grant、pairing JSON、account token、预览文件内容或 terminal 输入。

## 准入结果

- `make test-clients`：63 个测试文件、451 个测试通过，typecheck/build 通过。
- `make test-android`：Community/Official unit、APK assemble 与 class boundary 通过。
- `scripts/check-generated-code.sh`、文件协议 Go tests、FILE004 core/remote race、single Relay E2E 和 `git diff --check` 通过。
- 全量 `go test -race ./core ./remote/...` 仍会命中既有 `vterm` emulator restart race；FILE004 定向 race 通过，该问题不属于文件传输切片，未在本切片扩散修复。

## 校验命令

```bash
ssh root@114.66.58.243 \
  'sha256sum /opt/muxvia-file004/test-files/download-source.bin /opt/muxvia-file004/test-files/<uploaded-file>'

adb logcat -s \
  MuxviaNativePlugin MuxviaConnStore MuxviaWebRTC MuxviaChannelMgr \
  MuxviaFileTransfer MuxviaHeartbeat MuxviaBridgeServer MuxviaBridgeRouter
```

上述真机项均以 Android 文件系统、SQLite transfer 状态、daemon 临时文件和两端 SHA-256 为证据，不以 APK 构建、mock、UI 进度或文件名出现替代内容校验。
