# `remote-ui/` Agent Notes

当前项目根目录：`remote-ui/`

## Boundary

- `remote-ui` 是 Web / embedded Web UI 的产品壳，不是 `termx-core`。
- `remote-ui` 负责连接建立、运行时 WebRTC session、前端 terminal/file/events/api 消费层，以及 UI 状态编排。
- `remote-ui` 不应反向定义或污染 `termx-core` 的 shell-neutral runtime 边界。
- 当前产品方向是 APP-first：
  - `remote-ui` 后续应演进为手机 APP 或 APP 内复用的 UI/runtime，而不是 Web Control 内的 terminal 页面。
  - 默认打开后的第一屏是机器列表，不是 terminal、不是真正的 Web Control dashboard，也不是 marketing/landing page。
  - 第一屏只展示用户需要操作的机器状态、最近 terminal、连接状态和添加/扫描入口，信息少、密度高、决策直接。
  - 本地 Web 入口只作为开发调试或临时 embedded local web；正式线上操作入口以后应是 APP。
  - 如果后续决定砍掉本地 Web 入口，公共 runtime、terminal/file/api/events 组件和连接策略仍应能被 APP 复用。

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
- APP 连接策略必须按阶段升级：
  - 优先使用扫码保存的本地地址、LAN 地址或本地直连信息。
  - local/LAN 失败后再尝试 `public_p2p` rendezvous/signaling。
  - `public_p2p` 失败后再尝试 `managed`，由 Hub/ICE/TURN 决定是否 relay。
  - 每个阶段都要向用户给出简短、可行动的状态提示；不要让用户理解底层 NAT/ICE 细节。
- relay 不是客户端单独选择的 transport 类型。`managed` 路径下是否走 relay，必须由 Hub / TURN / ICE 侧策略决定，而不是由客户端抽象出另一套 transport 类型。
- 客户端只应把 relay 视为连接属性或 capability / policy 结果，例如 `relayInUse`、`fileTransferAllowed`，而不是额外的 transport 家族。
- 开发阶段 rendezvous 和 managed relay 可先对注册/dev 用户免费开放以跑通主流程；后续计费、订阅、限额再作为 policy/capability 结果接回。即使开发期免费，也不能把 relay 作为第四种 path。
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

## Remote Web / Hub Integration

- 后续接入真实 Web Control Plane / Hub API 时，`remote-ui` 仍只能通过 `RtcSession`、connector、signaling adapter、capability/policy 接口消费连接能力。
- Web Control 只提供账号、机器/agent/client 状态、Hub 列表、ticket、rendezvous、device-code 登录确认、强制下线等控制面 API；`remote-ui` 不应要求 Web Control 承载 terminal/file 操作页。
- 不要恢复 `RemoteTransport` / `PeerTransport` / `TerminalTransport` / `anonymous_p2p` / `managed_p2p` / `paid_relay` 等旧抽象或旧 path taxonomy。
- 真实 API adapter 可以使用 HTTP 调用 web/hub 的 signaling、discovery、pairing、rendezvous、policy API，但 terminal/file/api/events 运行时必须继续走 WebRTC DataChannel。
- 付费、订阅、支付、邮件、OAuth、外部 API 等业务在前端只能消费 mock 或真实 provider 返回的 policy/capability 结果；开发期先按 dev-free policy 跑通 rendezvous/relay，不要在连接 path 中编码套餐或 relay 类型。
- relay 只能显示为 capability / policy / connection info，例如 `relayInUse`、`relayBytesRemaining`、`relayThrottled`，不能变成第四种 transport。
- 如果前端切片依赖尚未实现的外部服务，使用 mock API client 或 fake provider 先完成 UI/state/adapter 行为，并在 `docs/remote-rebuild/WORKFLOW.md` 记录 deferred external item。
- 触及 remote web/hub/agent buildout 的 `remote-ui` 改动必须遵守根 `AGENTS.md` 的文件化 todo、TDD、subagent review 规则。

## APP-First UI Direction

- 机器列表是产品默认首页：
  - 顶部只保留账号/同步状态、搜索或筛选、添加/扫描入口。
  - 列表项展示机器名、在线状态、最近连接状态、terminal 数量、最后可见时间和简短连接来源。
  - 空状态只引导添加/扫描或登录，不展示长篇说明。
- 添加/扫描流程：
  - 支持扫描 `termx` CLI 输出的二维码。
  - 二维码 payload 可包含本地地址、公网地址、machine id、pairing/session info、Hub/Web Control endpoint、app certificate/bootstrap metadata；具体 schema 后续切片定义。
  - app private key 必须留在 APP 安全存储内，machine private key 绝不能进入 APP。
- 机器点击后的状态机：
  - `idle -> trying_local -> trying_public_p2p -> trying_managed -> connected | failed`。
  - 阶段失败提示要短：例如本地不可达、可登录尝试公网 P2P、P2P 失败可使用中转。
  - terminal/file/api/events 组件只能看到已连接的 `RtcSession` 和 capability，不关心底层阶段。
- UI 生成或实现时应保持简单、高效、信息密度高；不要引入 dashboard-heavy、marketing-heavy、workspace/tab/pane/tmux 概念。
- 可以参考 `../tgent/tgent-app` 的 Home/Scan/local store/connection store/native bridge 交互结构：
  - 可采用机器列表首页、扫码添加、本地簿与云端状态合并、连接 phase、重连和 native bridge 分层。
  - 必须把 tgent 的 server/session/window/pane/private-key download/`local|hub|p2p|relay` taxonomy 改写为 TermX 的 `machine -> terminal`、APP-owned key/cert、`local|public_p2p|managed`。
  - 参考 tgent 时只迁移交互和分层模式，不复制 tmux API、HTTP runtime proxy 或 WebSocket runtime。
