# termx TUI 测试策略

状态：Draft v2
日期：2026-03-25

---

## 1. 测试目标

TUI 测试首先验证“这个产品是否真的能用”，其次才验证字段、文案和局部渲染细节。

当前测试主目标固定为 4 类：

1. 工作台主体是否可工作
2. floating / overlay 是否有真实层次关系
3. terminal 连接与共享规则是否稳定
4. 渲染与性能是否在功能完成后保持稳定

---

## 2. 测试优先级

### 2.1 第一优先级：产品级 E2E

后续 TUI 主线默认优先补 E2E。

优先锁住：

- 启动即进入可输入 pane
- split 后能继续工作
- floating 可见、可切换、可遮挡
- overlay 打开关闭不破坏工作台
- picker / manager / restore / layout 能回到真实 pane

### 2.2 第二优先级：关键纯逻辑测试

只在下面这些纯规则上补必要单测：

- layout projection
- connection owner/follower 规则
- reducer 关键状态迁移
- overlay / focus / mode 关键回退

### 2.3 第三优先级：渲染与性能回归

放在主路径可用之后处理：

- dirty region
- backpressure
- 残影 / 串屏
- benchmark

---

## 3. 当前不鼓励的测试方向

在工作台主体未完成前，不应该把主要精力放在：

- 字段可见性断言
- 大量 renderer 文案断言
- modal/chrome 局部 token 断言
- 说明栏、摘要栏、卡片栏的微调快照

这些测试不是不能有，但只能作为辅助手段，不能再代表主线进展。

---

## 4. E2E 主回归池

### 4.1 工作台主体

必须优先稳定：

1. `launch-into-working-workspace`
2. `single-pane-input`
3. `split-and-continue-working`
4. `switch-tab-and-continue-working`

### 4.2 floating 工作台

必须优先稳定：

1. `floating-basic`
2. `floating-overlap`
3. `floating-z-order`
4. `floating-move-resize`
5. `floating-center-recall`
6. `floating-focus-switch`

### 4.3 overlay 盖板

必须优先稳定：

1. `overlay-open-close`
2. `overlay-focus-return`
3. `overlay-no-residue`
4. `picker-over-workbench`
5. `manager-over-workbench`

### 4.4 terminal 资源主线

必须优先稳定：

1. `connect-existing-terminal`
2. `create-terminal`
3. `manager-connect-here`
4. `metadata-edit`
5. `restart-program-exited-terminal`

### 4.5 shared terminal / restore

主工作台稳定后补齐：

1. `shared-terminal-basic`
2. `shared-terminal-resize`
3. `shared-terminal-exit`
4. `restore`
5. `layout-startup`

---

## 5. 每轮最低验证要求

任何 TUI 主线改动，至少需要：

1. 跑被修改包的定向 `go test`
2. 跑全量 `go test ./... -count=1`
3. 至少补一条对应功能的 E2E 场景

如果某一轮没有功能代码、只补测试，必须明确说明原因。

---

## 6. 触发规则

下面改动必须带对应 E2E：

- workbench renderer 改动
- split / layout projection 改动
- floating 改动
- overlay / focus 改动
- terminal picker / manager 改动
- restore / layout 改动

下面改动建议补关键单测：

- reducer policy
- owner/follower 规则
- 纯 layout 算法

---

## 7. 命名原则

测试名优先描述用户目标，而不是内部实现：

- `TestE2ERunScenarioLaunchIntoWorkingWorkspace`
- `TestE2ERunScenarioSplitAndContinueWorking`
- `TestE2ERunScenarioFloatingPaneOverlapsTiledWorkbench`
- `TestE2ERunScenarioOverlayCloseRestoresWorkbench`

不要再把测试命名和目标长期绑定在：

- 某个 summary token
- 某个 rail/panel/card 的文本形态

---

## 8. 最终质量门禁

在进入颜色、性能和残影治理前，必须先达到下面状态：

1. 主工作台已真实可工作
2. floating 已有真实叠放关系
3. overlay 已回到辅助层
4. terminal 资源主线已接回真实 pane

做到这一步之后，再继续 benchmark、dirty region、颜色层次和渲染优化，才是正确顺序。
