# LOADUX001 方形沿边加载动画验收

## 产物与环境

- APK：`clients/mobile/android/app/build/outputs/apk/debug/app-debug.apk`。
- APK 与模拟器已安装 `base.apk` SHA-256：`c596185387904908fec7f3ed0f6f48f836b63f49422e25c1d33cf88e0503e0f2`。
- 模拟器：AVD `termx-pa005n1`，`arm64-v8a`，Android API 35，1080x2400，density 420。
- 浏览器和 Android 原始截图、像素结果与 crash scan 位于 `.artifacts/loadux001/`，不进入 Git。

## 视觉契约

- `.muxvia-square-spinner` 只拥有稳定尺寸和定位，不拥有 animation 或 transform。
- `::before` 是固定方形周长；`::after` 使用同尺寸周长 mask，只推进注册的 angle 变量。没有任何方框元素旋转，也没有超出自身 box 的负 inset。
- 14、16、20、24、28px 都由同一 primitive 渲染；`box-sizing: border-box` 由 primitive 自身保证，不依赖 Tailwind reset。
- `prefers-reduced-motion: reduce` 关闭活动段动画，保留可识别的静态方框和单段。
- 本切片未修改任何 React loading 条件、请求时序、Endpoint/Route/session、错误或取消行为，也未引入动画框架。

## 两帧与布局检查

| 检查 | 结果 | 结论 |
| --- | --- | --- |
| Chromium 多尺寸边界 | 两帧分别为 14/16/20/24/28px，所有 `left/top/right/bottom/width/height` 完全相同 | PASS |
| Chromium 运动像素 | 本次复跑 136 个像素变化；组件外变化 0，周长以内的内部变化 0 | PASS |
| Chromium reduced-motion | 两帧变化 0；两张 PNG SHA-256 均为 `e3e5b21dfa78fc125416db22a6454f883695ca316956290dee5b315c0ea40b2c` | PASS |
| Android App UI | 冷启动由真实 APK 进入 WebView loading；第 6/7 帧的蓝色方框边界均为 63x63 物理像素，活动段从上边移动到左下边 | PASS |
| Android 两帧像素 | 230 个变化像素，差异边界完全包含在固定 63x63 方框内；随后正常进入设备列表 | PASS |
| Android 稳定性 | App 进程存活；Java/native fatal、ANR、signal 和 tombstone 扫描为 0 行 | PASS |

Android smoke 从系统冷启动真实 `com.muxvia.app/.MainActivity` 发起，没有直接调用 Go/JNI/binding 或修改 UI 状态。原生 splash 与 WebView loading 已分开采样，系统过渡不作为本切片证据。

## 准入结果

```text
UI proto generation                 PASS
UI tests                            46 files / 160 tests PASS
UI typecheck / production build     PASS
Mobile tests                        4 files / 36 tests PASS
Mobile Capacitor build/sync         PASS
Android testDebugUnitTest           PASS
Android ARM64 assembleDebug         PASS
Browser pixel/reduced-motion        PASS
Android install/hash/UI/crash       PASS
UX reviewer                         PASS
git diff --check                    PASS
```

UX reviewer 复核固定外框、连续单段、五种尺寸和 reduced-motion 后明确 `PASS`，无阻塞 finding。
