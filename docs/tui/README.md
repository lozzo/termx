# termx TUI 文档入口

状态：Draft v1
日期：2026-03-23

这套文档是 termx TUI 重构后的新主线文档。

目标不是恢复旧实现，而是重新定义：

- 产品设计
- 产品概念
- 交互和线框
- 产品架构
- 技术架构与技术分层
- 单元测试 / 回归测试 / 端到端测试策略
- 编码与交付计划

## 文档列表

1. [`product-spec.md`](product-spec.md)
   - 产品定义
   - 核心概念
   - 生命周期与设计原则
2. [`interaction-spec.md`](interaction-spec.md)
   - 布局模型
   - 焦点模型
   - 交互规则与快捷键策略
3. [`wireframes.md`](wireframes.md)
   - 主界面线框
   - overlay / picker / manager / floating 状态示意
4. [`architecture.md`](architecture.md)
   - 产品架构
   - 技术架构
   - 技术分层
5. [`testing-strategy.md`](testing-strategy.md)
   - 单元测试
   - 回归测试
   - 端到端测试
   - benchmark 与质量门禁
6. [`implementation-roadmap.md`](implementation-roadmap.md)
   - 编码工作拆分
   - 分阶段交付计划
   - 里程碑与验收口径
7. [`current-status.md`](current-status.md)
   - 当前所处阶段
   - 已完成与未开始项
8. [`scenarios.md`](scenarios.md)
   - 用户任务场景
   - 产品和测试共同输入
9. [`e2e-plan.md`](e2e-plan.md)
   - E2E 场景矩阵
   - 分批恢复顺序
10. [`state-model.md`](state-model.md)
   - 面向 AI 编码的状态模型规范
   - 字段、枚举、不变量
11. [`intent-effect-spec.md`](intent-effect-spec.md)
   - 面向 AI 编码的 intent / effect 规范
   - reducer contract 与输入映射
12. [`workspace-picker-spec.md`](workspace-picker-spec.md)
   - workspace picker 树形导航细节
   - jump 到 pane 的精确定义
13. [`testing-plan-concrete.md`](testing-plan-concrete.md)
   - 面向 AI 编码的测试落地顺序
   - 测试文件与 harness 组织
14. [`legacy-archive-review.md`](legacy-archive-review.md)
   - 旧版归档整理
   - 用于迁移参考，不是当前规范

## 推荐阅读顺序

1. `product-spec.md`
2. `interaction-spec.md`
3. `wireframes.md`
4. `architecture.md`
5. `testing-strategy.md`
6. `implementation-roadmap.md`
7. `current-status.md`
8. `scenarios.md`
9. `e2e-plan.md`
10. `state-model.md`
11. `intent-effect-spec.md`
12. `workspace-picker-spec.md`
13. `testing-plan-concrete.md`

## 当前统一口径

termx TUI 当前只保留以下用户主概念：

- `workspace`
- `tab`
- `pane`
- `terminal`

同时明确：

- `pane` 是工作位
- `terminal` 是 server 托管的运行实体
- `close pane` 不等于 `stop terminal`
- `owner / follower` 是 terminal 控制权关系
- 新主线按 staged delivery 推进，不恢复旧版大一统 `Model`

## 面向 AI 编码的最小阅读集

如果目标是直接指导 AI 写代码，至少先读：

1. `architecture.md`
2. `state-model.md`
3. `intent-effect-spec.md`
4. `workspace-picker-spec.md`
5. `testing-plan-concrete.md`
