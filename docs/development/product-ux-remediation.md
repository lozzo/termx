# Muxvia 产品体验整改基线

## 目标

本轮只解决已经由真实 Web、Android 截图和可复现二维码载荷证明的问题：

1. Web Controller 与 Android App 缺少完整的英文/简体中文能力。
2. 普通用户的账号、手机激活、daemon 注册和配对任务被技术状态与高级管理能力淹没。
3. daemon pairing 二维码承载完整 signed bundle，尺寸超出普通终端和部分显示器的稳定展示范围。

本轮不建设通用设计系统、翻译后台、服务端语言偏好、Web terminal、桌面 GUI、iOS UI 或与当前流程无关的视觉组件库。

## 走查证据

- Web 手机 activation payload 为 `muxvia-cloud-activate:v1:<MXA code>`，约 60 字符，QR Version 4、33x33 modules。
- 当前 daemon pairing bundle 实测为 599 bytes；base64url URI 为 826 字符，QR Version 23、109x109 modules。终端半块字符渲染仍需要约 109 列、55 行。
- Web Controller 已有 i18next 与 `en`、`zh-CN`、`ru` 资源，但 `AccountPage` 未消费翻译资源，直接显示英文、Proto enum 名和内部控制面术语。
- Android/共享 UI 没有语言 owner；登录、扫码、设备、terminal、file 与错误状态仍以英文 literal 分散在组件中。
- 当前 `AccountPage.tsx`、`RemoteControlApp.tsx` 和 `MuxviaApp.tsx` 分别约 847、2637 和 880 行。只允许按本轮用户流程拆出局部组件与 locale，不进行仓库级 UI 框架重写。

## 语言基线

- 首期正式支持：`en`、`zh-CN`。
- 默认语言：匹配系统语言；非中文默认英文。
- 用户选择：只保存在当前客户端本地表现层，不写入账号、Controller 或 Proto。
- fallback：未知 key 在开发测试中失败；运行时未知 locale 回退英文。不得用英文 fallback 冒充某语言已经完整支持。
- 状态与错误：UI 只按稳定 enum/error code 映射 locale；服务端英文 message 仅作为诊断，不参与文案分支。
- 日期、时间、数字和流量使用当前 locale 的 `Intl` formatter。

核心术语固定如下：

| English | 简体中文 |
| --- | --- |
| Device | 设备 |
| Daemon | 守护进程 |
| Pair | 配对 |
| Direct | 直连 |
| Relay | 中转 |
| Terminal | 终端 |
| File | 文件 |
| Machine | 设备 |

## 信息架构

### Web Controller

普通账号一级导航固定为：概览、设备、套餐、账号。Topology、CommandOutbox 和原始 session 信息进入高级管理，不与添加设备同级。

设备页只有一个主操作“添加设备”，随后进入单任务向导：

```text
选择设备类型 -> 生成短期凭据 -> 等待设备提交 -> 核对设备 -> 批准 -> 完成
```

危险操作在具体设备上触发近期认证，不再使用页面顶部的全局解锁条。列表优先显示友好名称、在线状态和最近活动；DeviceID、fingerprint、Hub 和 revision 只在详情中显示。

### Android App

未登录且没有设备时，首屏优先提供“登录 Muxvia Cloud”和“添加本地设备”。扫码和手工输入是同级入口。设备列表优先显示友好名称、是否可用和当前连接状态；技术 ID、candidate、Hub 与 generation 进入连接详情。

## 二维码边界

`QR001` 只改善现有输出：

- CLI 支持文本和 PNG 文件输出。
- 终端尺寸不足时拒绝输出残缺二维码，并给出明确替代命令。
- Web QR 保持正方形、quiet zone、响应式尺寸和手工 code。

`QR002` 解决根因：

- daemon 创建 128-bit、十分钟、单次、内存持有的 pairing claim。
- QR 只携带协议版本、必要 route seed、daemon identity reference 和 claim。
- 完整 signed pairing bundle 由 App/客户端到 owning daemon 的端到端配对链路取得。
- Controller、Hub、Relay 和 Web 不存储、不解析、不签发 CapabilityGrant 或完整 bundle。
- 目标二维码不高于 QR Version 10；无摄像头设备可输入短码或粘贴短 URI。

## 验收矩阵

| 范围 | 最小验收 |
| --- | --- |
| Web i18n | 360/768/1280/1440，英文/中文，键盘导航，150% 缩放，无 raw enum |
| App i18n | ARM64 模拟器，英文/中文，系统大字体，竖屏/横屏，无重叠和截断主操作 |
| Web 设备流程 | 手机 activation 与 daemon enrollment 都完成创建、等待、核对、批准和完成 |
| 配对 | 小终端不裁切；PNG/text fallback 可用；短码完成后扫码和无摄像头输入使用同一 claim |
| 最终 E2E | App UI 发起登录、添加设备、配对、terminal 输入输出和文件操作，并扫描 crash/logcat |
