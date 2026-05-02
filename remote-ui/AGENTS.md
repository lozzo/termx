# `remote-ui/` Agent Notes

当前项目根目录：`remote-ui/`

## Boundary

- `remote-ui` 是 Web / embedded Web UI 的产品壳，不是 `termx-core`。
- `remote-ui` 负责连接建立、运行时 WebRTC session、前端 terminal/file/events/api 消费层，以及 UI 状态编排。
- `remote-ui` 不应反向定义或污染 `termx-core` 的 shell-neutral runtime 边界。

## Transport Architecture

- `remote-ui` 的运行时 transport 统一基于 WebRTC DataChannel。
- 平台中立公共接口层必须以 `RtcSession` 作为唯一运行时连接对象，并以 `RtcBinaryChannel` / `RtcJsonRpcChannel` / `ConnectionInfo` / `ConnectionCapabilities` 等中性类型对外暴露能力。
- HTTP 只允许承担 signaling / discovery / pairing / rendezvous / hub poll-answer 等建链前职责；HTTP 不是运行时 transport。
- 前端实现必须先抽象平台中立的公共接口，再提供浏览器 WebRTC 实现；不要把浏览器 `RTCPeerConnection` / `RTCDataChannel` 直接扩散到高层业务代码。
- 当前浏览器端实现只是第一种 runtime adapter。后续需要支持基于 WebView 的移动端接入，并由 Android(Java/Kotlin) 与 iOS(Swift) 分别提供原生 WebRTC transport 实现。因此：
  - terminal protocol client、api/file/events 消费层必须依赖公共接口，而不是直接依赖浏览器 WebRTC 类型。
  - signaling、session、channel、connection info、capability 等边界应先在前端接口层稳定，再由 browser/native 各自实现。
  - 不要把“浏览器 WebRTC 实现细节”误写成整个 `remote-ui` 的公共架构。
- 客户端可见的连接路径只分 3 类：
  - `local`：本机 WebRTC，允许通过 ICE TCP / WebRTC over TCP 建链。
  - `public_p2p`：通过公共 ICE / STUN 基础设施建立的普通 WebRTC P2P 连接。
  - `managed`：通过自部署 Hub / ICE 基础设施建立的 WebRTC 连接。
- relay 不是客户端单独选择的 transport 类型。`managed` 路径下是否走 relay，必须由 Hub / TURN / ICE 侧策略决定，而不是由客户端抽象出另一套 transport 类型。
- 客户端只应把 relay 视为连接属性或 capability / policy 结果，例如 `relayInUse`、`fileTransferAllowed`，而不是额外的 transport 家族。
- `remote-ui` 的正确分层应是：
  - `connector/signaling`
  - `rtc session`（唯一运行时连接对象）
  - `terminal protocol client`
- 不要继续沿用或扩张 `local transport / remote transport / terminal transport` 这种按名词堆叠的错误抽象。
- 禁止保留 `RemoteTransport` / `PeerTransport` / `TerminalTransport` 作为长期公共运行时边界；迁移期内若出现同义类型，必须在当前切片删除或重命名为正确职责。

## Current Rewrite Task

- 当前任务是对 `remote-ui` transport / connection / terminal protocol 相关实现做整块重写。
- 当前任务默认要求无人值守推进：除非用户显式暂停、改变方向或出现高风险不可逆操作，否则必须持续推进，不停在中间方案态。
- 当前任务默认要求以文件记录驱动整个过程，至少维护：
  - `docs/webrtc-rewrite-architecture.md`
  - `docs/webrtc-rewrite-log.md`
- `webrtc-rewrite-architecture.md` 必须描述：
  - 旧抽象为什么错误
  - 新架构如何分层
  - `local` / `public_p2p` / `managed` 三类路径如何建链
  - relay 为什么不是客户端 transport 类型
  - capability / policy 应落在哪一层
- `webrtc-rewrite-log.md` 必须按切片持续记录：
  - 目标
  - 失败测试
  - 实现和重命名
  - 删除了哪些旧抽象
  - 验证命令
  - review 发现
  - 修复内容
  - 剩余风险

## Workflow

- 当前任务必须采用 TDD：
  - 先定义目标行为
  - 先写失败测试
  - 再写最小实现
  - 再重构
  - 再跑验证
  - 再更新日志
- 当前任务必须做切片级 code review：
  - 如果当前代理环境支持 sub-agent / review agent，每个切片完成后必须发起一次独立审查。
  - 审查重点必须包括：
    - 测试是否只是迎合实现
    - 是否存在 fake test / tautological test / 只验证 mock 交互的测试
    - 是否残留错误抽象
    - 是否把浏览器 WebRTC 类型泄漏进 `RtcSession` 公共接口或业务层
    - 是否把 relay 表达成第四种客户端 transport 类型
    - 是否为未来 native adapter 保留了清晰边界
    - 是否遗漏文档或 AGENTS 更新
  - 如果工具不可用，必须在日志中写明原因并完成等价自审。

## Naming

- `RtcSession` 只能表示平台中立公共接口，不应用作浏览器具体类名。
- 持有 `RTCPeerConnection` 并统一管理 `terminal:*` / `api` / `events` / `file:*` DataChannel 的浏览器对象，应命名为 `BrowserRtcSession`、`BrowserRtcPeerAdapter`、`WebRtcBrowserSession` 或等价名称，不应继续以 `local...Transport` 之类名字误导职责。
- 在 terminal binary channel 上执行 `hello/attach/snapshot/output/resize control` 的对象，应命名为 `TerminalProtocolClient`、`TermxTerminalProtocolClient` 或等价名称，不应命名为 transport。
- 建链前只负责拿 signaling 参数并返回已连接 session 的对象，应命名为 connector / signaling adapter。
- 浏览器实现层可使用 `BrowserRtc...`、`WebRtcBrowser...` 等命名；未来原生实现层应能够并列提供 `NativeRtc...` 或桥接实现，而不要求改动上层业务接口。

## Validation

- 与本任务相关的改动，至少要运行：
  - `npm test`
  - `npm run typecheck`
  - `npm run build`
- 如果改动影响本地 embedded Web UI 资产同步，还必须运行：
  - `npm run build:localweb`
