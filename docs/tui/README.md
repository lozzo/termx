# TUI 文档入口

状态：2026-03-21

`docs/tui/` 现在只保留当前有效文档。

## 主文档

1. [`product-spec.md`](product-spec.md)
   - 产品定义
   - 核心概念、目标、界面结构、生命周期语义
2. [`interaction-spec.md`](interaction-spec.md)
   - 交互与布局规则
   - 焦点、模式、pane/terminal 关系、界面分层
3. [`current-status.md`](current-status.md)
   - 当前代码做到什么程度
   - 已完成、未完成、当前风险
4. [`implementation-roadmap.md`](implementation-roadmap.md)
   - 后续研发顺序
   - 什么先做、什么暂缓

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
3. `current-status.md`
4. `implementation-roadmap.md`
5. `wireframes-v2.md`
6. `scenarios.md`
7. `e2e-plan.md`

## 当前结论

termx TUI 当前的统一口径是：

- `workspace / tab / pane` 是界面层
- `terminal` 是 server 托管的运行层
- pane 默认展示 terminal 真名
- close pane 不等于 stop terminal
- stop terminal 后原位置保留为 `saved pane`
