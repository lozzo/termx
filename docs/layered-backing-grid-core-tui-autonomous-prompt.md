# Layered Backing Grid Core+TUI Autonomous Prompt

将以下内容复制到新的开发上下文中使用。

---

你在 `/Users/lozzow/Documents/workdir/termx` 工作。

先读：

- 根 `AGENTS.md`
- `termx-core/AGENTS.md`
- `tuiv2/AGENTS.md`
- 根 `workflow.md`
- `docs/terminal-history-layered-backing-grid-design.md`

当前开发约束：

- 本轮只关注 `termx-core/`、`termx-vterm/`、`tuiv2/`
- 只允许最小化触及这些 contract glue：
  - `internal/protocol/`
  - `termx-proto/`
  - `termx-cli/`
- 不要展开到：
  - `remote-ui/`
  - `termx-app/`
  - `web-control/`
  - 宽泛 remote 产品层改动

开发目标：

- 推进 `docs/terminal-history-layered-backing-grid-design.md` 中的 layered backing-grid 路线
- 优先级：
  1. canonical row identity / generation
  2. hot grid / cold store contract
  3. resize authority 从 screen-diff inference 向 explicit row movement 收敛
  4. copy-mode / selection / paging 对 canonical identity 对齐
- 保持 committed-row paging contract 不破
- 不重新引入 raw PTY journal 作为 UI history truth
- 继续遵守“一个真实 PTY size，observer 只做 projection”的原则

执行要求：

1. 不停在分析或提案，直接推进实现。
2. 每次开始前先：
   - `git status --short --branch`
   - 读根 `workflow.md` 的当前优先级与风险
3. 每完成一个 module-sized slice 后，必须更新根 `workflow.md`。
4. 更新 `workflow.md` 时必须压缩内容：
   - 只保留当前目标
   - 当前设计结论
   - 最新验证证据
   - 当前风险
   - 下一步
   - 不要把长流水命令日志无限追加进去
5. 如果需要保留长历史记录，移到 `docs/workflows/archive/`，并在 `workflow.md` 中只留一句指针。
6. 所有 git commit message 必须使用中文。
7. 不要回退用户已有改动。
8. 完成一轮后：
   - 跑 focused tests
   - 必要时跑 core+tui full verify
   - 如涉及 terminal history 语义，补 tmux smoke
   - kill 本地 TermX daemon/test daemon，但不要自动重启

测试基线：

- Focused:
  - `go test ./termx-core/...`
  - `go test ./termx-vterm/...`
  - `go test ./tuiv2/runtime/...`
  - `go test ./tuiv2/app/...`
  - 必要时 `go test ./internal/protocol/...`
- Full verify:
  - `go test ./internal/... ./termx-cli/... ./termx-core/... ./termx-proto/... ./termx-vterm/... ./tuiv2/runtime/... ./tuiv2/app/... ./tuiv2/render/...`
- tmux smoke:
  - copy-mode top reaches oldest retained content
  - resize does not collapse retained history
  - attach / re-entry does not regress loaded depth semantics

工作方式：

- 小步提交
- 每步先收口 contract，再扩实现
- 优先减少历史边界和 resize 猜测
- 遇到设计分叉，先把选择写进 `workflow.md`，再编码

输出风格：

- 进度更新简洁、直接
- 完成后报告：
  - 做了什么
  - 跑了什么验证
  - `workflow.md` 更新了哪些当前结论
  - 还剩什么风险 / 下一步

---
