# tuiv2 Agent Notes

当前项目根目录：`tuiv2/`

## Boundary

- `tuiv2` 是 shell，不是 core；可以依赖 `termx-core` 的 public package，但不能把 shell 约束反推回 core。
- `Visible*`、projection、render view-model 路径必须保持纯读；禁止在这些路径里做 normalize、补状态、修 cache 或其他隐式 mutation。
- `screen update` / `snapshot` / `bootstrap` 相关线上传输保持二进制编码；不要为调试或兼容回退到 JSON live path。

## Current Line Focus

- 当前主动开发线只关注 `tuiv2` 与直接相关的 core history contract。
- 这轮 `tuiv2` 主要收口：
  - copy-mode backing model
  - cursor / selection 对 canonical row refs 对齐
  - observer viewport 只做 projection，不发明新的 history truth
  - attach / re-entry / transaction restore / stale-page 语义与 core 保持一致
- runtime/app 必须保留 hot-only latest snapshot 的 `LoadedRows=0` 语义；不要把当前可见 hot rows 提升成 committed depth。
- older-page prepend 只接受 same-generation committed window merge；`offset=0` latest replace 继续是 authoritative boundary reset。
- paged-response ownership 目前仍是 live-vs-copy-mode split，不是 pane-scoped contract；新逻辑不要假设 pane 级 owner routing 已经存在。
- pane-terminal 绑定、owner handoff、resize 协调必须优先收口到 `orchestrator` 或明确 service，不要把同一事务散落写进 `app`、`workbench`、`runtime` 和 render。

## UI Constraints

- 所有新 UI 配色从宿主终端主题推导，入口统一以 `render/styles.go` 的 `uiThemeFromHostColors` 为中心。
- modal 内不要展示快捷键字符串；快捷键说明放在 status/help。

## Workflow

- 唯一有效驱动文件是 repository-root `workflow.md`。
- 每完成一个 module-sized slice，立即更新并压缩根 `workflow.md`，只保留 live 决策、最新证据、当前风险和下一步。
- 当 `tuiv2` 需要新的 core/tui contract 时，先把结论写进根 `workflow.md`，再实现。
