# Monorepo Agent Notes

## Scope

- 仓库根目录现在是 monorepo 壳，不再默认等同于原来的 Go module 根。
- 当前 Go core 项目位于 `termx-core/`。
- 当前 TUI 项目位于 `tuiv2/`。
- 当前 CLI 项目位于 `termx-cli/`。

## Routing

- 如果任务涉及 `termx-core/`，优先遵循 `termx-core/AGENTS.md`。
- 如果任务涉及 `tuiv2/`，优先遵循 `tuiv2/AGENTS.md`。
- 如果任务涉及 `termx-cli/`，优先遵循 `termx-cli/AGENTS.md`。
- 如果任务涉及 `remote-ui/`，优先遵循 `remote-ui/AGENTS.md`。
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
