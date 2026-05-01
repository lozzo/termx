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

## Unattended Remote Rebuild Workflow

当任务涉及远程能力重建、`docs/remote-rebuild/`、mobile app、web-control、rendezvous、hub/TURN、remote agent 或相关协议时，默认进入无人值守执行模式，除非用户明确要求只讨论方案。

### Source of Truth

- 先阅读并遵循 `docs/remote-rebuild/README.md`。
- 远程产品和协议决策以 `docs/remote-rebuild/` 下文档为准。
- 可复用 `../tgent` 的行为经验，但不能把 workspace/tab/pane 作为 TermX remote 公开模型带回来。
- 远程公开对象模型必须保持 `machine -> terminal`。
- 匿名/免费路径只能使用公共 STUN 和轻量 rendezvous signaling，不能给匿名免费用户发 TermX TURN relay credentials。
- App 只能持有 app private key 和 app certificate，不能下载、解密或保存 machine private key。

### Persistent Workflow File

- 必须创建并持续维护 `docs/remote-rebuild/WORKFLOW.md`。
- 每个 todo 开始前和完成后都要更新该文件。
- 文件至少记录：
  - 当前 phase 和 active todo。
  - todo 列表与状态。
  - 每个完成 todo 对应的 commit hash。
  - TDD 中先写的测试、测试失败原因、最终测试结果。
  - subagent 分工和 code review 结论。
  - mock、placeholder、TODO 和需要人类介入的事项。
  - 当前风险和下一步精确动作。

### Execution Discipline

- 使用 TDD：先写失败测试，再实现，再跑 focused/broader tests。
- 每完成一个 todo 必须提交一次代码。
- commit message 要详细说明动机、范围、关键实现、行为变化和测试。
- 完成每个 todo 后要明确记录“todo 名称 -> commit hash”。
- 能并行的任务必须使用 subagent 并行处理，拆分时保持文件/模块 ownership 清晰，避免互相覆盖。
- 每个开发 todo 完成后必须启动 subagent 做 code review。
- code review 重点检查：
  - 是否偏离 `docs/remote-rebuild/` 主线。
  - 是否误引入 workspace/tab/pane remote 模型。
  - 匿名/免费流程是否错误使用 TermX TURN relay。
  - app 是否错误接触 machine private key。
  - Web/native transport 是否仍通过 interface 隔离。
  - 测试是否覆盖当前 todo 的关键行为。

### Human-Intervention Deferral

- 遇到 DNS、公网部署、证书、支付、应用商店签名、真实账号密钥、人工产品决策等需要人类介入的内容，不要停住等待。
- 用 mock、placeholder、窄 TODO 或注释先推进主线。
- 必须把 deferral 写入 `docs/remote-rebuild/WORKFLOW.md`，说明：
  - 缺少什么人类输入。
  - 当前 mock/placeholder 在哪里。
  - 后续如何替换。

### Completion Requirements

- 不要在仍有必要命令运行时结束。
- 每一轮结束前尽量保持 `git status --short` 干净。
- 如果确实不能干净，必须在回复和 `WORKFLOW.md` 中说明原因、剩余文件和下一步。
