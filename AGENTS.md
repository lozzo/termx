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

