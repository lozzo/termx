# Monorepo Agent Notes

## Scope

- 仓库根目录现在是 monorepo 壳，不再默认等同于原来的 Go module 根。
- 当前 Go core 项目位于 `termx-core/`。
- 当前 TUI 项目位于 `tuiv2/`。
- 当前 CLI 项目位于 `termx-cli/`。
- 当前 Web Control Plane 项目位于 `web-control/`。
- 当前 Hub / Signaling / Relay 项目位于 `termx-hub/`。

## Routing

- 如果任务涉及 `termx-core/`，优先遵循 `termx-core/AGENTS.md`。
- 如果任务涉及 `tuiv2/`，优先遵循 `tuiv2/AGENTS.md`。
- 如果任务涉及 `termx-cli/`，优先遵循 `termx-cli/AGENTS.md`。
- 如果任务涉及 `remote-ui/`，优先遵循 `remote-ui/AGENTS.md`。
- 如果任务涉及 `web-control/`，优先遵循 `web-control/AGENTS.md`。
- 如果任务涉及 `termx-hub/`，优先遵循 `termx-hub/AGENTS.md`。
- `web/`、`mobile/`、未来的 TURN / WebRTC 服务目录，默认不继承 `termx-core/` 的 TUI/协议实现假设。

## Layout

- 新增项目优先放到独立顶级目录，不要继续把不同产品壳混塞回 `termx-core/`。
- 跨项目共享能力，优先先明确边界，再决定是提 shared package、独立服务还是协议层复用。

## Frontend Styling

- `web/`、`mobile/`、`remote-ui/` 和本地内嵌 Web UI 默认使用 TailwindCSS 作为样式系统。
- 不要为页面和组件重新手写一套通用 CSS 系统；新增 UI 样式优先使用 Tailwind utility class、Tailwind theme/config 和组件内 class 组合。
- 第三方库自身必须导入的 CSS 可以保留，例如 `@xterm/xterm/css/xterm.css`。
- 只有 Tailwind 无法表达或必须覆盖第三方内部 DOM 时，才允许写极窄作用域的兼容 CSS；这类 CSS 必须局限在对应入口或组件附近，不能扩散成全局样式体系。
- 如果某个前端包还没有 Tailwind 构建链，先补 Tailwind 配置/构建接入，再继续新增 UI 样式。

## Current Task: `remote-ui/` WebRTC Rewrite

- 当前任务要求对 `remote-ui/` 的 transport / connection / terminal protocol 相关实现进行整块重写，不做“逐层兼容旧抽象”的渐进式修补。
- 运行时唯一 transport 抽象必须收敛为平台中立 `RtcSession`；HTTP 只允许用于 signaling / discovery / pairing / rendezvous / hub poll-answer 等建链前职责。
- 客户端可见连接路径只能是 `local`、`public_p2p`、`managed`。禁止新增或保留 `relay` / `paid_relay` / `anonymous_p2p` / `managed_p2p` 作为客户端 transport taxonomy。
- 浏览器 `RTCPeerConnection` / `RTCDataChannel` 类型只能出现在 browser adapter 及其直接测试/辅助层，terminal/api/file/events 消费层必须依赖公共接口。
- 当前任务默认要求无人值守推进：除非用户显式暂停、改变目标或出现高风险不可逆操作，否则 agent 不应停在“先给方案/等确认”的中间状态，而应持续推进到文档、实现、测试、审查和收尾。
- 当前任务默认要求使用文件作为工作记录驱动，防止上下文压缩导致信息丢失。至少维护以下文档：
  - `remote-ui/docs/webrtc-rewrite-architecture.md`
  - `remote-ui/docs/webrtc-rewrite-log.md`
- 当前任务默认要求 TDD 推进：每个切片先写或修订失败测试，再实现，再重构，再验证。
- 当前任务默认要求切片级审查：如果当前代理环境支持 sub-agent / review agent，则每个切片完成后都必须发起一次独立 code review；如果工具不可用，必须在日志里记录未执行原因并做等价自审。

## Current Task: Remote Web / Hub / Agent Buildout

- 当前任务要求实现 TermX 远程体系的 Web Control Plane、Hub / Signaling / Relay 服务、以及 `termx daemon` 内置 agent 的云端接入。
- 技术栈：
  - Web Control Plane 使用 Go 后端 + Vite + React 前端。
  - 测试/开发数据库先使用 SQLite。
  - Hub / Signaling / Relay 使用 Go。
  - 可参考 `../tgent` 的 web、hub、agent、TURN、traffic、registry、signaling 实现思路，但不能照搬 workspace/session/window/pane/tmux 模型，也不能照搬 HTTP proxy / WebSocket terminal runtime。
- 运行时必须继续统一为 WebRTC DataChannel。HTTP 只允许用于 signaling / discovery / pairing / rendezvous / hub poll-answer / control-plane API。
- 客户端可见连接路径只能是 `local`、`public_p2p`、`managed`。禁止新增或保留 `relay` / `paid_relay` / `anonymous_p2p` / `managed_p2p` 作为客户端 transport taxonomy。
- relay 只能作为 connection info / capability / policy / quota / telemetry 字段存在，例如 `relayInUse`、`relayBytesRemaining`、`relayThrottled`。

## Remote Build Workflow

- 当前任务默认无人值守推进：除非用户显式暂停、改变目标，或出现高风险不可逆操作，否则 agent 不应停在“等确认”的中间状态。
- 所有工作流必须文件化，主记录文件是 `docs/remote-rebuild/WORKFLOW.md`。
- `WORKFLOW.md` 是事实上的 todo list。任何主线任务、派生问题、阻塞、review、测试结果、commit、deferred 外部依赖都必须落盘，不允许只留在上下文里。
- todo 必须使用稳定编号和树状分支：
  - 顶级示例：`3`
  - 子任务示例：`3-A`
  - 如果 `3-A` 中发现新问题，新建 `3-A-A`
  - 如果 `3-A-A` 继续发现问题，新建 `3-A-A-A`
- 派生条目必须写明来源、是否阻塞父条目、状态和解决方式。
- 状态至少使用：
  - `pending`
  - `in_progress`
  - `blocked`
  - `blocked_external`
  - `review`
  - `completed`
  - `resolved`
  - `deferred`
  - `deferred_external`
- 开始切片、发现问题、遇到外部依赖、写失败测试、实现、跑测试、发起 review、修复 review、commit 后，都必须实时更新 `WORKFLOW.md`。
- 如果 todo 文件和实际代码状态不一致，必须先修 todo 文件，再继续施工。

## External Dependency Policy

- 遇到需要人类对接的外部系统时，不要停下来等待。
- 外部系统包括但不限于：
  - 真实支付
  - 真实订阅/发票/税务系统
  - 真实短信/邮件服务
  - 真实 OAuth provider
  - 真实对象存储
  - 第三方统计/风控
  - DNS、TLS 证书、云服务账号、商店签名、人工审批
- 默认做法：
  - 抽象 provider/interface。
  - 实现 mock / fake / local stub。
  - 用测试覆盖完整业务状态流转。
  - 在 `WORKFLOW.md` 中创建 `deferred_external` 或 `blocked_external` 条目。
  - 写明真实接入未来需要替换的接口、需要人类提供什么、当前 mock 覆盖到什么程度、剩余风险。
- 不允许把 mock 写死进核心业务导致未来无法替换。
- 不允许因为支付、外部 API、云账号等事项中断主线；这些事项应 mock 后继续，最终统一汇报给人类。

## Remote Build TDD And Review

- 每个切片必须 TDD：
  - 先定义目标行为。
  - 先写失败测试。
  - 跑 focused tests 并记录预期失败。
  - 再写最小真实实现。
  - 再重构。
  - 再跑 focused tests 和相关 broader tests。
  - 再更新 `WORKFLOW.md`。
- 每个切片必须做 subagent code review。若当前环境不支持 subagent，必须在 `WORKFLOW.md` 记录原因，并执行等价强制自审。
- review 必须重点检查：
  - 测试是否只是迎合实现。
  - 是否存在 fake test / tautological test / 过度 mock / 只证明 mock 被调用。
  - 是否存在为了过测试而写的伪行为。
  - mock 是否隔离在 provider/interface 后面，未来能替换真实支付、邮件、OAuth 或外部 API。
  - HTTP/WebSocket 是否被错误包装成 terminal/file/api/events 的运行时 transport。
  - relay 是否被抽成第四种客户端 transport/path。
  - free/public_p2p 是否能拿到 TURN credentials。
  - web/hub/agent 的认证、授权、ownership 边界是否真实执行。
  - app/machine private key 是否可能泄漏或上传。
  - browser `RTCPeerConnection` / `RTCDataChannel` 类型是否泄漏到公共业务层。
  - SQLite transaction、TTL cleanup、quota/session limit 是否是真行为，不是只在测试里成立。
  - 是否遗漏 `WORKFLOW.md` / AGENTS / docs 更新。

## External Server Testing

- 需要公网环境时，可以使用 `ssh root@114.66.58.243`。
- 使用外部服务器前，必须先在 `WORKFLOW.md` 写明为什么当前切片需要公网环境。
- 禁止执行破坏性命令，禁止清空系统目录。
- 不要修改 SSH 配置、iptables、防火墙、systemd 常驻服务，除非当前切片明确需要并已经在 `WORKFLOW.md` 记录。
- 临时测试服务必须放到清晰目录，例如 `/tmp/termx-devstack` 或 `/opt/termx-devstack`。
- 必须记录启动、停止、清理命令。
- 测试完成后必须记录结果和遗留状态。
