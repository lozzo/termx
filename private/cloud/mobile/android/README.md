# Android Cloud Module

TermX 只构建一个面向用户的 Android App。该目录是同一 App 的 first-party Cloud module，作为标准 source set 编入 APK；默认构建没有开发 Cloud origin，账号与 managed Route 均 fail closed，但 Direct/SSH 不受影响。

标准构建：

```bash
cd clients/mobile/android
./gradlew testDebugUnitTest assembleDebug
```

显式 loopback devcloud 构建：

```bash
./gradlew -PtermxDevCloud=true testDebugUnitTest assembleDebug
```

该 profile 固定使用 `http://127.0.0.1:41001` 和 `http://127.0.0.1:41002`，配合 `adb reverse`；只允许 loopback HTTP。

显式公网 HTTP staging 构建：

```bash
./gradlew -PtermxPublicHTTPStaging=true testDebugUnitTest assembleDebug
```

两个 development profile 互斥。公网 HTTP staging 只用于固定测试环境，不得承载生产账号或数据。

私有 module 只拥有账号 session、Control Plane/Hub 调用和 managed eligibility 投影。Endpoint、Route、WebRTC、DTLS binding、CapabilityGrant、DeviceIdentity signer、Proto API、DataChannel 和 terminal payload 仍由 Go Client Engine 与公开 daemon 持有。Relay 的订阅、配额和租约最终由服务端准入判定。

仓库根目录的 `make test-android` 构建标准 APK 与 devcloud APK，并执行单 App class boundary。ADB、daemon、登录、pairing、terminal 和故障隔离步骤见 [`docs/remote-platform/android-devcloud-manual-test.md`](../../../../docs/remote-platform/android-devcloud-manual-test.md)。

Production OAuth/TLS 仍未由本地 development profile替代。任何构建都不得恢复 archived Web Controller、旧 Hub token 协议或 legacy 多 DataChannel。
