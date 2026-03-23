# termx TUI 架构重构说明

状态：Draft v1
日期：2026-03-22

---

## 1. 背景

termx TUI 已经具备可工作的主路径：

- 启动即进入 workspace
- tab / split / floating 可用
- terminal picker / terminal manager 可用
- metadata / restore / saved pane / exited pane 已落地
- shared terminal 的基础规则和回归测试已存在

问题不在于“功能完全没做”，而在于：

- 产品概念已经收口
- 代码内部结构还停留在多轮补丁叠加状态

如果继续只在当前结构上补功能，后面会越来越难稳定 shared terminal、floating、restore、manager 和 UI polish。

---

## 2. 为什么现在必须重构

当前重构不是为了追求代码好看，而是因为它已经直接影响下面几件事：

1. 输入路径重复，导致快捷键和模式行为容易分叉
2. pane 同时承载领域状态、运行时状态、渲染缓存，边界不清
3. shared terminal 的 `owner / follower` 语义还没有成为一等模型
4. 业务逻辑里散落了大量渲染缓存失效逻辑，后续很难继续优化性能
5. TDD 仍然能补洞，但测试目标对象不够稳定，维护成本持续升高

结论：

- 不能推倒重写
- 也不能继续纯补丁推进
- 正确做法是“冻结产品口径后，分阶段重构内部架构”

---

## 3. 当前结构问题

### 3.1 `Model` 过大

当前 [`tui/model.go`](/home/lozzow/workdir/termx/tui/model.go) 同时承担：

- 用户输入分发
- 业务状态变更
- 终端 attach / resize / stop 等副作用触发
- prompt / picker / manager 状态
- render 相关缓存失效

结果是：

- 任意需求变更都容易改到不相干逻辑
- 单测虽然能写，但定位变更边界越来越困难

### 3.2 输入状态机重复

当前 key/event 的模式机分散在：

- [`tui/model.go`](/home/lozzow/workdir/termx/tui/model.go)
- [`tui/input.go`](/home/lozzow/workdir/termx/tui/input.go)

这意味着：

- 一个交互可能在两个地方各自维护一份判断
- 后续继续收口 keymap 时风险会越来越高

### 3.3 `Pane` 混合了三类状态

现在的 pane 同时承载：

- 领域状态
  - 绑定哪个 terminal
  - saved / waiting / exited
  - viewport mode
  - owner / follower
- 运行时状态
  - 活动鼠标拖拽
  - 过程中的交互草稿
  - 临时 focus / hover 信息
- 渲染状态
  - render cache
  - dirty 标记
  - snapshot/grid 相关数据

这会让“业务变更”和“渲染优化”互相污染。

### 3.4 `owner / follower` 还不是一等模型

当前共享 terminal 的关键规则是对的：

- terminal 的 PTY size 只有一份
- pane 自己有几何
- 只有 owner 决定 terminal size
- follower 只观察

但实现上仍较依赖 `ResizeAcquired` 这类补丁式标志，而不是显式连接模型。

### 3.5 render cache 失效分散在业务逻辑中

当前 [`tui/render.go`](/home/lozzow/workdir/termx/tui/render.go) 与 [`tui/model.go`](/home/lozzow/workdir/termx/tui/model.go) 之间存在较多手工缓存失效。

这会导致：

- 继续修闪烁/残影时很容易误伤别的路径
- 想做性能优化时，很难知道应该在哪一层失效

---

## 4. 重构目标

重构完成后，TUI 需要形成下面 4 层结构。

### 4.1 Intent Layer

职责：

- 把键盘、鼠标、程序消息统一翻译成用户意图
- 不直接改业务状态

例子：

- `PaneSplitVertical`
- `PaneAttachTerminal`
- `PaneAcquireTerminalSize`
- `FloatMoveBy`
- `PromptCommit`

当前代码与目标之间的过渡状态：

- 还没有完整的显式 intent struct
- 但 key/event 主要 mode 已经共享 action 映射
- prefix enter/clear 已经先收口为统一 transition 入口

这意味着：

- 当前已经从“完全重复状态机”迈入“有统一入口的半过渡态”
- 下一步应该继续把 transition 前的输入解析提升为真正的 intent dispatch

### 4.2 State / Reducer Layer

职责：

- 接收 intent
- 纯粹更新 screen state
- 产出需要执行的 effect

要求：

- 绝大部分产品规则在这里完成
- 这一层尽量做成可直接单测的纯逻辑

### 4.3 Runtime Layer

职责：

- 执行 reducer 产出的副作用
- 与 server、pty、timer、logger、bubbletea cmd 交互

例子：

- attach terminal
- stop terminal
- resize terminal
- 启动/取消 prefix timer
- 定期打性能日志

### 4.4 Render Layer

职责：

- 只根据 screen state 和 runtime snapshot 绘制
- 自己管理 render cache 生命周期

要求：

- 业务层不再手工四处写缓存失效
- 缓存策略尽量局部、可验证、可 benchmark

---

## 5. 目标核心模型

### 5.1 `PaneState`

只保留用户可理解、可持久化的 pane 领域属性，例如：

- pane id
- pane geometry
- tile / floating
- terminal binding
- pane lifecycle
- viewport mode `fit / fixed`
- readonly / pin / size lock warn

### 5.2 `ConnectionState`

这是本轮重构的关键新增一等模型。

职责：

- 描述 pane 与 terminal 的连接关系
- 明确 shared terminal 的 size 决策权

最少应表达：

- terminal id
- attached pane ids
- current owner pane id
- owner 切换策略
- tab auto-acquire 配置

规则：

- 任一 terminal 同时最多一个 owner
- 新 attach 的 pane 默认 follower
- owner 被关闭或解绑时，需稳定迁移到剩余 pane 或清空

当前代码状态：

- 第一阶段已由 [`tui/connection_state.go`](/home/lozzow/workdir/termx/tui/connection_state.go) 落地
- 当前还是 snapshot/helper 形态，不是最终独立 runtime store
- 但 owner 归一化、迁移、状态判断已经不再散落在多处 ad-hoc 判断中

### 5.3 `PaneRuntime`

只放运行时过程状态，例如：

- 鼠标拖拽草稿
- prefix hold 剩余时间
- prompt 输入草稿
- 临时 hover / selection

### 5.4 `ScreenModel`

职责：

- 汇总 workspace / tab / pane / connection 的可渲染快照
- 作为 render layer 的唯一稳定输入

---

## 6. 共享 terminal 的最终规则

为避免产品和实现再分叉，统一口径如下：

### 6.1 两套概念必须分开

- `fit / fixed` 是 pane 的本地显示模式
- `owner / follower` 是 pane 与 terminal 的连接关系

它们不是一回事。

### 6.2 size 规则

- terminal 的 PTY size 只有一份
- 只有 owner 可以提交 terminal resize
- follower 改变自己的几何，不自动改写 terminal size
- tab 可配置 auto-acquire
- size lock warn 是提醒，不是强制锁死

### 6.3 attach 规则

- 现有 owner 不应因新 follower attach 而丢失所有权
- 新 pane 默认 follower
- 显式 acquire 后才切换 owner

### 6.4 remove / exit 规则

- stop/remove terminal 后，所有 pane 进入 saved pane
- terminal exited 后，pane 进入 exited pane
- 关闭 pane 只影响当前 pane，不影响 terminal 本体

---

## 7. 输入系统的重构目标

重构完成后，所有输入都应收口到一条路径：

- 键盘输入 -> intent
- 鼠标输入 -> intent
- 异步消息 -> intent

不应再出现：

- 同一个 mode 在两个地方各自推进
- 同一个动作在输入层直接改状态、在消息层再补一次

目标结果：

- 帮助文案和真实行为可以共用同一份动作定义
- e2e 失败时，能快速定位是 intent、reducer 还是 runtime effect 问题

---

## 8. 渲染层的重构目标

### 8.1 缓存失效回到 render layer

目标：

- 业务层不再到处写 `renderCache = nil`
- render layer 根据局部输入变化决定是否重建缓存

### 8.2 明确可缓存对象

优先拆分：

- 顶栏
- pane frame / title chrome
- fixed viewport crop
- overlay body
- terminal manager list rows

### 8.3 性能基线

重构后必须继续保留：

- benchmark
- 渲染次数日志
- alt-screen / shared terminal 的回归 e2e

---

## 9. TDD 策略

重构不能靠“先搬代码再补测试”。

统一顺序：

1. 先为要抽出的规则补单测
2. 再抽 reducer / state / runtime
3. 每一阶段结束补一轮 e2e 主路径回归
4. 对已知高风险渲染问题补 benchmark 或专项 e2e

测试分层：

- 单测：规则、状态迁移、owner/follower 归属、saved/exited 语义
- 组件/渲染测：frame、overlay、title chrome、残影清理
- e2e：shared terminal、floating、restore、manager、alt-screen
- benchmark：大输出、shared pane、overlay 开关、鼠标拖动

---

## 10. 完成标准

满足下面条件时，才能认为这一轮重构完成：

1. 输入只走一条 intent 路径
2. `owner / follower` 有独立模型，不再只是补丁标志
3. pane 领域状态、运行时状态、渲染状态基本分层
4. render cache 失效主要由 render layer 管理
5. 当前已存在主路径功能不回退
6. 全量单测、e2e、benchmark 基线持续可跑

---

## 11. 一句话结论

termx TUI 现在不是“重写前夜”，而是“进入受控重构阶段”。

主产品模型已经够清楚，接下来要做的是：

- 用重构把实现结构拉回可持续状态
- 用 TDD 把共享 terminal、渲染、输入系统彻底钉牢
