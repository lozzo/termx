# TUI 文档入口

状态：2026-03-22

`docs/tui/` 现在只保留当前有效文档。

## 主文档

1. [`product-spec.md`](product-spec.md)
   - 产品定义
   - 核心概念、目标、界面结构、生命周期语义
2. [`interaction-spec.md`](interaction-spec.md)
   - 交互与布局规则
   - 焦点、模式、pane/terminal 关系、界面分层
3. [`architecture-refactor.md`](architecture-refactor.md)
   - 当前 TUI 架构问题
   - 目标分层、核心状态模型、重构完成标准
4. [`current-status.md`](current-status.md)
   - 当前代码做到什么程度
   - 已完成、未完成、当前风险
5. [`implementation-roadmap.md`](implementation-roadmap.md)
   - 后续研发顺序
   - 什么先做、什么暂缓
6. [`refactor-roadmap.md`](refactor-roadmap.md)
   - 分阶段重构路线
   - 每一阶段的 TDD 和验收口径
7. [`todo.md`](todo.md)
   - 当前 TUI 主线待办
   - 分优先级推进清单

## 辅助文档

1. [`wireframes-v2.md`](wireframes-v2.md)
   - 线稿和界面示意
2. [`scenarios.md`](scenarios.md)
   - 用户任务清单
3. [`e2e-plan.md`](e2e-plan.md)
   - 端到端测试矩阵

## 阅读顺序

建议按下面顺序看：

1. `product-spec.md`
2. `interaction-spec.md`
3. `architecture-refactor.md`
4. `current-status.md`
5. `implementation-roadmap.md`
6. `refactor-roadmap.md`
7. `todo.md`
8. `wireframes-v2.md`
9. `scenarios.md`
10. `e2e-plan.md`

## 当前结论

termx TUI 当前的统一口径是：

- `workspace / tab / pane` 是界面层
- `terminal` 是 server 托管的运行层
- pane 默认展示 terminal 真名
- close pane 不等于 stop terminal
- stop terminal 后原位置保留为 `saved pane`
- `fit / fixed` 是 pane 的显示模式
- `owner / follower` 是共享 terminal 的连接关系
- 当前实现进入“分阶段重构 + 持续交付”阶段，而不是继续叠补丁
