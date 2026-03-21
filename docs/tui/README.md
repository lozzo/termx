# TUI 文档入口

这一轮不再继续在旧设计稿上打补丁。

`docs/tui/deprecated/` 里保留了之前所有 TUI 设计文档，作为历史归档；从现在开始，以本目录下的新文档为准。

## 当前主文档

- [`product-spec.md`](product-spec.md)
  - 当前正式 TUI 产品规格书
  - 产品评审、交互收口、开发对齐时优先看这一份

## 配套文档索引

| 文档 | 作用 |
|------|------|
| [`product-spec.md`](product-spec.md) | 正式产品规格书，定义定位、概念、主流程、视觉与验收标准 |
| [`interaction-spec.md`](interaction-spec.md) | 交互规格，定义模式、焦点、pane 生命周期、picker/prompt 行为 |
| [`workspace-layout-spec.md`](workspace-layout-spec.md) | workspace 与 layout 关系、启动/恢复/保存规则 |
| [`wireframes-v2.md`](wireframes-v2.md) | 纯文本线稿，表达主界面、picker、floating、共享 terminal resize 等关键状态 |
| [`implementation-roadmap.md`](implementation-roadmap.md) | 代码级实施路线图，说明当前代码哪些保留、哪些重构、按什么顺序 TDD 推进 |
| [`scenarios.md`](scenarios.md) | 全量用户使用场景清单，按启动、导航、复用、恢复等主题拆分 |
| [`design-reset.md`](design-reset.md) | 重置背景与概念收口说明，解释为什么不再沿用旧补丁式设计 |
| [`e2e-plan.md`](e2e-plan.md) | 端到端测试矩阵：场景、目标行为、现有测试、待补测试 |

## 阅读建议

- 看正式规格：先读 `docs/tui/product-spec.md`
- 看交互落地：读 `docs/tui/interaction-spec.md`
- 看 workspace/layout 语义：读 `docs/tui/workspace-layout-spec.md`
- 看页面和交互长相：读 `docs/tui/wireframes-v2.md`
- 看用户任务是否覆盖完整：再读 `docs/tui/scenarios.md`
- 看为什么这样收口：再读 `docs/tui/design-reset.md`
- 看测试是否跟上：最后读 `docs/tui/e2e-plan.md`

## 当前原则

1. 先以产品规格统一语言，再推进交互和代码
2. 用户可见概念尽量收口到 `workspace / tab / pane / terminal`
3. UI 体验尽量接近 zellij，但不伪装成 zellij
4. 必须体现 termx 的核心差异：TUI 与 server 托管 terminal runtime 解耦
5. 所有主线能力优先用场景化 e2e 锁住
