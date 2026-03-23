# termx TUI 编码与交付计划

状态：Draft v1
日期：2026-03-23

---

## 1. 总体策略

这轮 TUI 工作按下面原则推进：

1. 先文档，后编码
2. 先接口，后实现
3. 先纯模型，后复杂交互
4. 先测试，后重构
5. 先恢复主路径，后恢复高级能力

当前不做：

- 恢复旧版大一统代码
- 先追求复杂视觉 polish
- 先做大量次要功能

---

## 2. 里程碑

### M0 文档冻结

目标：

- 冻结新主线产品设计
- 冻结概念定义
- 冻结测试策略与工作拆分

交付：

- `docs/tui/product-spec.md`
- `docs/tui/interaction-spec.md`
- `docs/tui/wireframes.md`
- `docs/tui/architecture.md`
- `docs/tui/testing-strategy.md`
- `docs/tui/implementation-roadmap.md`

验收：

- 你确认文档方向
- 后续编码按此为准

### M1 核心领域层

目标：

- 建立新 TUI 的纯模型基础

拆分：

1. `layout`
2. `workspace state`
3. `connection state`
4. `overlay/focus state`
5. `slot state`

验收：

- 纯逻辑单测通过
- 没有 bubbletea 依赖

### M2 Intent / Reducer / Effect 骨架

目标：

- 把输入路径统一到 intent
- reducer 和 runtime effect 分离

拆分：

1. intent 定义
2. key/mouse/event adapter
3. reducer
4. effect plan
5. runtime executor interface

验收：

- 非法输入不会锁死
- reducer 可单测
- runtime 可替身测试

### M3 最小可用工作台

目标：

- 恢复可启动、可输入、可 split、可切 tab 的最小 TUI

范围：

- 默认 workspace
- 默认 shell pane
- tiled split
- tab create / switch
- 基础底栏 / 顶栏

验收：

- 启动可工作
- `launch`、`split`、`tab` 主路径 E2E 通过

### M4 Terminal 复用主线

目标：

- 恢复 terminal picker、connect、metadata 基线

范围：

- terminal picker
- connect existing terminal
- create terminal
- metadata edit
- terminal manager connect-here
- unconnected / program-exited pane

验收：

- `picker`
- `metadata`
- `manager connect here`
- `restart program-exited terminal`

这些场景 E2E 通过。

### M5 Floating 与 Shared Terminal

目标：

- 恢复 floating pane 和共享 terminal 规则

范围：

- floating create / move / resize / z-order / center
- owner/follower
- acquire / auto-acquire
- owner acquisition

验收：

- floating 回归池通过
- shared terminal 回归池通过

### M6 Terminal Manager / Workspace / Restore

目标：

- 补全真正的工作台能力

范围：

- terminal manager
- workspace picker
- workspace switch
- workspace tree navigation to pane
- workspace restore
- layout startup / resolve

验收：

- restore 和 layout 主路径通过
- terminal manager 操作可回归
- workspace picker 可直达目标 pane

### M7 渲染优化与稳定性收尾

目标：

- 在功能齐备后处理性能和视觉收尾

范围：

- render cache
- dirty region
- benchmark
- 残影 / 串屏 / backpressure 修正

验收：

- benchmark 建基线
- real-program E2E 稳定

---

## 3. 编码工作分解

### 3.1 第一批代码任务

1. 新建 `tui/domain` 基础包
2. 抽 `layout` 新实现或迁移纯逻辑
3. 抽 `workspace state` schema
4. 抽 `connection state` 一等模型
5. 定义 `Intent` 和 `Effect` 接口

### 3.2 第二批代码任务

1. 新建 bubbletea 外壳
2. 建立最小 renderer
3. 打通启动、输入、主 pane
4. 打通 split 和 tab

### 3.3 第三批代码任务

1. picker
2. metadata prompt
3. unconnected / program-exited pane
4. terminal manager
5. workspace picker

### 3.4 第四批代码任务

1. floating
2. shared terminal
3. restore / layout
4. render cache

---

## 4. 每阶段测试安排

### 4.1 M1

- layout 单测
- workspace state 单测
- connection state 单测

### 4.2 M2

- intent mapping 单测
- reducer 单测
- mode/overlay 状态单测

### 4.3 M3

- 启动 E2E
- split E2E
- tab E2E

### 4.4 M4

- picker 回归测试
- metadata E2E
- program-exited pane restart E2E

### 4.5 M5

- floating 回归测试
- shared terminal E2E
- real program 样本测试

### 4.6 M6-M7

- restore / layout E2E
- manager 回归测试
- workspace tree jump 回归测试
- benchmark 和全量回归

---

## 5. 提交与评审策略

每个阶段尽量拆成三类提交：

1. `test:`
2. `refactor:`
3. `feat:` 或 `fix:`

原因：

- 便于定位回归
- 便于区分结构变化和行为变化
- 便于你审阅

---

## 6. 风险和对应策略

### 6.1 shared terminal 复杂度失控

策略：

- connection state 先成型
- owner/follower 先单测后接 UI

### 6.2 渲染重构过早

策略：

- 先最小 renderer
- 后做 cache 和 dirty

### 6.3 输入系统再次分叉

策略：

- 一切输入先转 intent
- 不允许 key 路径和 event 路径各写一套逻辑

### 6.4 restore / layout 太早拉高复杂度

策略：

- 先恢复主工作台
- 再接恢复与声明式入口

---

## 7. 每周工作节奏建议

如果按稳定推进节奏，建议以一周为一个小迭代：

### Week 1

- 冻结文档
- 建核心领域层
- 建基础单测

### Week 2

- 建 intent / reducer / effect
- 恢复最小可用工作台

### Week 3

- picker / metadata / unconnected-program-exited pane
- 首批主路径 E2E

### Week 4

- floating / shared terminal
- 第二批共享场景 E2E

### Week 5

- manager / workspace / restore / layout
- workspace tree jump
- 场景回归池扩充

### Week 6

- render 优化
- benchmark
- 文案与视觉收尾

---

## 8. 当前建议的第一步编码顺序

如果你确认这套文档，真正开始写代码时，我建议按这个顺序起手：

1. `tui/domain/layout`
2. `tui/domain/connection`
3. `tui/domain/workspace`
4. `tui/app/intent`
5. `tui/app/reducer`
6. 最小 bubbletea shell

这个顺序最稳，因为它先把最容易失控的边界锁死。

---

## 9. 完成定义

这轮 TUI 重构不以“文件都补回来了”为完成标准，而以这几件事为完成标准：

1. 主路径可工作
2. 概念清晰
3. shared terminal 稳定
4. restore / layout 可用
5. 测试分层健全
6. 后续继续开发不需要再回到补丁式推进
