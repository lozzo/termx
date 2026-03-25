# termx TUI 架构设计

状态：Draft v1
日期：2026-03-23

---

## 1. 架构目标

新 TUI 架构必须解决旧版最核心的 4 个问题：

1. 输入路径重复
2. 领域状态、运行时状态、渲染状态混合
3. shared terminal 规则没有成为一等模型
4. render cache 和业务逻辑耦合

因此，新架构要求：

- 先定义模型，再实现功能
- 先定义接口，再连接基础设施
- 先把状态边界切清，再做复杂 UI

---

## 2. 产品架构

产品层按 4 个面组织：

### 2.1 工作台面

包含：

- workspace
- tab
- tiled pane
- floating pane

职责：

- 组织工作位
- 提供焦点切换
- 承载主要操作上下文

### 2.2 terminal 资源面

包含：

- terminal picker
- terminal manager
- metadata prompt

职责：

- 发现 terminal
- 复用 terminal
- 管理 terminal 元数据和生命周期

### 2.3 恢复与声明式入口面

包含：

- workspace restore
- layout resolve
- waiting slot

职责：

- 进入既有工作现场
- 将静态声明映射成运行现场

### 2.4 系统反馈面

包含：

- help
- notice
- confirm prompt
- error prompt

职责：

- 降低学习成本
- 明确解释复杂行为
- 承接危险操作确认

---

## 3. 领域模型

### 3.1 核心聚合

推荐按下面聚合定义：

- `WorkspaceState`
- `TabState`
- `PaneState`
- `TerminalRef`
- `ConnectionState`
- `OverlayState`

### 3.2 PaneState

只保留 pane 作为观察窗口所需的最小领域状态：

- pane id
- pane kind
  - tiled / floating
- pane geometry
- terminal connection
- slot state
  - connected / empty / exited / waiting

不放：

- vterm 实例
- stream handler
- render dirty
- cache
- 鼠标拖拽草稿

### 3.3 ConnectionState

这是共享 terminal 的一等模型。

至少表达：

- terminal id
- connected pane ids
- owner pane id
- owner acquisition policy
- owner 迁移规则
- tab auto-acquire 配置

任何 terminal 控制面权限判断都必须先经过它。

### 3.4 OverlayState

统一表达当前叠层：

- none
- picker
- manager
- workspace picker
- help
- prompt
- confirm

---

## 4. 技术架构

### 4.1 总体分层

推荐采用 6 层：

1. `intent layer`
2. `application layer`
3. `domain layer`
4. `runtime layer`
5. `render layer`
6. `infrastructure layer`

### 4.2 Intent Layer

职责：

- 把键盘、鼠标、定时器、server 事件统一翻译成显式 intent
- 不直接修改状态

示例 intent：

- `SplitPane`
- `ConnectTerminal`
- `CreateTerminal`
- `MoveFloatingPane`
- `ResizeFloatingPane`
- `AcquireTerminalResize`
- `OpenTerminalManager`
- `CommitPrompt`

### 4.3 Application Layer

职责：

- 协调 reducer、policy、effect 生成
- 决定一个 intent 对应的业务流程
- 产出 effect plan

输出：

- next state
- effects
- notices

### 4.4 Domain Layer

职责：

- 定义纯状态模型和纯规则
- 不依赖 bubbletea、protocol client、timer、logger

建议包含：

- workspace rules
- pane slot rules
- connection rules
- layout rules
- restore rules

### 4.5 Runtime Layer

职责：

- 执行 effects
- 与 server、pty、timer、event stream、storage 交互

接口示例：

- `TerminalService`
- `WorkspaceStore`
- `LayoutStore`
- `Clock`
- `Scheduler`
- `Logger`

### 4.6 Render Layer

职责：

- 只根据 screen model 和 runtime snapshot 绘制
- 自己管理 dirty 和 cache

要求：

- 业务层不手写 render invalidation
- overlay 和 pane 重绘路径局部化
- cache 策略可 benchmark、可回归

#### 4.6.1 渲染主轴

后续 renderer 必须严格按下面顺序组织：

1. `LayoutProjection`
   - 从 workspace/tab/pane 状态投影出 tiled pane rect、floating pane rect、z-order
2. `WorkbenchSurface`
   - 把 terminal surface 贴到 pane rect 里
   - 这是主界面第一主体
3. `FloatingComposite`
   - 在 tiled workbench 之上叠放 floating pane
   - 负责 clipping、overlap、raise/lower、active window
4. `OverlayComposite`
   - 在 workbench 之上盖 overlay / modal / mask
   - 不能替代底层 workbench 主体
5. `HUDChrome`
   - 最后叠最小 header/footer/notice
   - chrome 只做导航，不得主导空间分配

#### 4.6.2 明确禁止

渲染层明确禁止走下面这些方向：

- 先画 dashboard/card/rail，再把 pane 作为附属内容塞进去
- 让 overlay 成为主界面主体
- 让 summary/context 面板长期侵占 pane surface 的主要宽度
- 把工作台的主要可读性建立在说明字段，而不是 pane 布局本身

### 4.7 Infrastructure Layer

职责：

- protocol client 适配
- filesystem persistence
- bubbletea integration
- 日志、benchmark、test harness

---

## 5. 技术分层细化

### 5.1 推荐目录边界

后续代码建议按下面边界组织：

- `tui/domain`
  - 纯状态模型、规则、policy
- `tui/app`
  - intent、reducer、effect planning
- `tui/runtime`
  - effect executor、service adapter
- `tui/render`
  - screen model、layout projection、canvas、cache
- `tui/bt`
  - bubbletea 适配壳
- `tui/testkit`
  - harness、fixture、golden helper

### 5.2 接口先行

按照仓库约束，优先先写接口再写实现。

建议最先定义的接口：

- `TerminalClient`
- `EventSource`
- `WorkspaceRepository`
- `LayoutRepository`
- `Clock`
- `Renderer`
- `OverlayPresenter`

### 5.3 状态拆分

推荐最少拆成 4 类状态：

- `DomainState`
  - workspace / tab / pane / connection
- `UIState`
  - overlay、focus、mode、prompt draft
- `RuntimeState`
  - stream connection、pending request、timer state
- `RenderState`
  - cache、dirty region、measured rect

---

## 6. 数据流

推荐统一数据流：

1. 输入或事件进入
2. 转成 `Intent`
3. `Reducer` 处理 intent，得到新状态和 `Effects`
4. `Runtime` 执行 effects
5. `Runtime event` 再回流为新的 intent
6. `Render` 基于当前 screen model 输出视图

强约束：

- 不允许在 render 中改业务状态
- 不允许在 reducer 中直接调 protocol client
- 不允许在 input handler 里绕过 intent 直接改 model

---

## 7. 复杂点的专项设计

### 7.1 shared terminal

必须由专门 policy 负责：

- connect 默认 follower
- owner 选举
- owner 获取
- owner 迁移
- auto-acquire
- terminal control-plane 权限判断

### 7.2 floating

必须把下面几件事拆开：

- 几何状态
- z-order
- 激活态
- 拖动草稿
- resize 草稿

不要再把 floating 的过程态塞回 pane 领域对象。

### 7.3 render cache

缓存至少按下面层级管理：

- pane frame cache
- pane body cache
- overlay cache
- tab composite cache

并且每类 cache 都要有明确失效源。

---

## 8. 迁移原则

从 legacy 迁到新架构时：

1. 继承产品规则
2. 继承可用模型
3. 不继承大一统 `Model`
4. 不继承旧版 cache 耦合方式
5. 不继承 key/event 双状态机

最优先可迁移资产：

- `layout`
- `layout_decl`
- `workspace_state`
- `client interface`
- 共享 terminal 的规则语义

---

## 9. 架构完成标准

架构达到可继续长期演进，至少需要满足：

1. 输入统一进入 intent
2. reducer 可单测
3. runtime effect 可替身测试
4. render cache 由 render layer 自治
5. shared terminal 的 owner/follower 成为一等模型
6. floating、restore、manager 这些复杂功能不再依赖 ad-hoc 补丁
