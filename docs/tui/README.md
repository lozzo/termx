# termx TUI 文档入口

状态：Draft v2
日期：2026-03-25

这套文档的当前统一方向只有一句话：

- `termx TUI 必须先做成 pane-first、terminal-first 的真实工作台`

补充说明：

- overlay 是辅助层，不是主界面主体
- modal 可以漂亮，但不能替代工作台
- 不再把 `card / deck / rail / dashboard` 当成主工作台方向
- `deprecated/tui-legacy/` 是工作台层的重要参考区，可借鉴布局、叠放、遮挡和 E2E 思路

---

## 推荐阅读顺序

1. [`product-spec.md`](product-spec.md)
   - 先看产品目标与主概念
2. [`interaction-spec.md`](interaction-spec.md)
   - 先看布局、焦点和交互边界
3. [`wireframes.md`](wireframes.md)
   - 先看最终应该长成什么样
4. [`architecture.md`](architecture.md)
   - 再看渲染主轴和技术分层
5. [`implementation-roadmap.md`](implementation-roadmap.md)
   - 再看阶段顺序和交付节奏
6. [`testing-strategy.md`](testing-strategy.md)
   - 再看怎么验证“它真的能用”
7. [`e2e-plan.md`](e2e-plan.md)
   - 看产品级 E2E 场景如何分批锁住
8. [`current-status.md`](current-status.md)
   - 看当前主线是否仍然和上面一致

---

## 文档列表

1. [`product-spec.md`](product-spec.md)
   - 产品定义
   - 核心概念
   - 生命周期与产品原则
2. [`interaction-spec.md`](interaction-spec.md)
   - 布局模型
   - 焦点模型
   - 输入与交互规则
3. [`wireframes.md`](wireframes.md)
   - terminal-first 主工作台线框
   - overlay 覆盖态线框
4. [`architecture.md`](architecture.md)
   - 产品架构
   - 技术架构
   - 渲染主轴
5. [`implementation-roadmap.md`](implementation-roadmap.md)
   - 编码工作分块
   - 阶段目标
   - 验收口径
6. [`testing-strategy.md`](testing-strategy.md)
   - E2E 优先策略
   - 回归池
   - 性能与稳定性验证
7. [`e2e-plan.md`](e2e-plan.md)
   - 产品级 E2E 场景矩阵
8. [`current-status.md`](current-status.md)
   - 当前方向
   - 偏线清理
   - 下一阶段工作块
9. [`state-model.md`](state-model.md)
   - 状态模型
   - 字段与不变量
10. [`intent-effect-spec.md`](intent-effect-spec.md)
   - intent / effect 规范
11. [`workspace-picker-spec.md`](workspace-picker-spec.md)
   - 树形导航与直达 pane
12. [`testing-plan-concrete.md`](testing-plan-concrete.md)
   - 测试文件落点与落地顺序
13. [`legacy-archive-review.md`](legacy-archive-review.md)
   - 旧版资产的正确参考方式
14. [`wireframes-v2.md`](wireframes-v2.md)
   - 旧文件名兼容入口

---

## 当前统一口径

termx TUI 当前只保留以下用户主概念：

- `workspace`
- `tab`
- `pane`
- `terminal`

同时明确：

- `pane` 是观察和操作 terminal 的主表面
- `terminal` 是 server 托管的运行实体
- `connect` 表示 pane 与 terminal 的连接关系
- `close pane` 不等于 `stop terminal`
- `owner / follower` 是 terminal 控制权关系
- 新主线允许参考旧版工作台实现思路，但不恢复旧版大一统 `Model`

---

## 面向 AI 编码的最小阅读集

如果目的是继续指导 AI 写代码，至少先读：

1. `current-status.md`
2. `wireframes.md`
3. `architecture.md`
4. `implementation-roadmap.md`
5. `testing-strategy.md`

不先读这 5 份，就很容易再次回到错误主线。
