# termx TUI 旧实现解耦重构设计

状态：Draft
日期：2026-03-26

## 1. 目标

这份文档定义 termx TUI 的第一阶段重构方案：

- 产品交互目标以 [2026-03-25-tui-product-definition-design.md](/home/lozzow/workdir/termx/docs/superpowers/specs/2026-03-25-tui-product-definition-design.md) 为准
- 实现基础以 `deprecated/tui-legacy/pkg` 为主要参考资产
- 重构目标不是继续堆补丁，而是把旧 TUI 拆成清晰的状态、应用、runtime、render、input、feature 边界

本设计同时回答三件事：

1. 旧实现里哪些能力应该保留
2. 哪些历史交互和历史抽象应该冻结或删除
3. 新主线 `tui/` 应该如何分层，才能在保持第一阶段产品能力的前提下更容易维护和扩展

## 2. 非目标

本阶段不做以下事情：

- 不重新定义产品定位
- 不追求兼容旧 TUI 的全部交互细节
- 不保留旧代码里所有实验性或复杂模式能力
- 不同时完成 GUI / mobile 或项目目录独立子系统
- 不把“先能跑”当唯一目标而继续保留 God Object 结构

## 3. 产品对齐基线

第一阶段产品行为必须服从产品定义文档，而不是服从旧实现现状。

### 3.1 第一阶段必须对齐的产品范围

- `Workbench` 作为默认入口
- `pane / terminal` 解耦语义
- `live / exited / unconnected` 三种 pane 状态
- `close / disconnect / reconnect / kill / remove` 的明确区分
- 独立 `Terminal Pool` 页面
- `overlay` 承载 connect / prompt / help 等高频局部动作

### 3.2 旧实现中不再作为第一阶段目标的能力

以下能力即使旧实现存在，也不再作为第一阶段保留要求：

- `fit / fixed / pin`
- `auto-acquire resize`
- 复杂 prefix 子模式体系
- 以旧 terminal manager / picker 为中心的产品结构
- 与产品定义冲突的历史快捷键与流程

这些能力不是“永远不做”，而是本阶段主动降级或删除，避免继续污染主架构。

## 4. 现状诊断

旧 `deprecated/tui-legacy/pkg` 的主要问题不是功能缺失，而是职责坍塌。

### 4.1 当前核心问题

- `Model` 同时承担状态容器、输入状态机、运行时编排、渲染调度、workspace store、prompt 管理、stream 生命周期管理等职责
- `Viewport` 同时承担 terminal 运行时、显示状态、渲染缓存、恢复状态等多种角色
- `Update` 大量直接处理副作用，导致状态变化与 daemon 调用高度耦合
- `input -> action -> client` 的路径直接穿透，缺少稳定的中间抽象
- terminal picker / terminal manager / workspace picker / prompt 都以 nullable 字段挂在大模型上，通过硬编码优先级分发
- 同一类 `Create -> Attach -> Snapshot -> Stream` 逻辑在多个文件里重复出现

### 4.2 旧实现中值得保留的部分

- layout tree 与几何计算
- shared terminal 的 owner / follower 语义
- cell-based renderer 的核心绘制思路
- workspace state 与 layout declaration 的持久化能力
- attach / snapshot / stream 驱动的 terminal runtime 基础路径

## 5. 总体设计原则

### 5.1 公开入口先稳定

在第一阶段重构完成前，保留以下公开入口稳定：

- `tui.Run`
- `tui.Client`
- `tui.Config`

这样可以把变化限制在内部结构，避免外部 CLI 和调用方同时跟着大改。

### 5.2 统一采用 `intent / reducer / effect / runtime`

新主线的控制流固定为：

`input -> intent -> reducer -> effect -> runtime -> message -> reducer`

其中：

- input 只翻译用户输入
- reducer 只做纯状态变更
- runtime 统一执行副作用
- render 只消费状态投影

### 5.3 renderer 保留思路，不保留耦合方式

旧 renderer 的 cell canvas、damage、缓存思路值得保留；
但 renderer 不再直接依赖业务对象内部的 runtime 状态，不再通过修改 `Pane` / `Viewport` 驱动自身。

### 5.4 feature 以产品切片组织，而不是以历史文件组织

重构后的 feature 应按产品对象组织：

- workbench
- terminal pool
- overlay / prompt / help

而不是继续沿用“一个超大模型里挂很多 nullable 子页面”的方式。

## 6. 新主线技术架构

第一阶段目标目录为 `tui/`。

### 6.1 目录职责

#### `tui/`

公开入口层，只负责：

- 保持 `Run` / `Client` / `Config`
- 组装 app / runtime / render / input
- 提供最薄的外部适配

#### `tui/core/`

纯状态与纯规则层，只放不依赖 Bubble Tea、client、副作用的内容：

- `workspace`
- `tab`
- `pane`
- `layout`
- `terminal metadata`
- `connection ownership`
- 局部类型与纯函数

#### `tui/app/`

应用壳层，负责：

- `screen`
- `overlay`
- `focus`
- `intent`
- `effect`
- `reducer`

这里的模型是“可持久化、可测试的应用状态”，不是运行时对象容器。

#### `tui/runtime/`

副作用层，负责：

- terminal create / attach / snapshot / input / resize / stream / kill / remove
- runtime session store
- daemon event 订阅
- workspace state restore / rebind
- reducer effect 的执行与结果回填

#### `tui/render/`

渲染层，负责：

- screen snapshot / projection
- canvas composition
- frame / title / body / overlay 的绘制
- 增量渲染与缓存

render 不直接调用 client，也不直接修改应用状态。

#### `tui/input/`

输入翻译层，负责：

- 键盘 / 鼠标输入到 `intent`
- 不同页面和 overlay 的输入路由
- 不直接调 runtime client

#### `tui/features/`

产品切片层，负责承载页面与 overlay 的组织逻辑：

- `workbench`
- `terminalpool`
- `overlay`

feature 可以依赖 `app/core/render/runtime`，但不反向拥有它们。

## 7. 核心对象重组

### 7.1 Pane 与 Terminal 明确分离

新主线中：

- `Pane` 是工作位
- `Terminal` 是共享运行实体
- `Pane` 不再拥有 terminal runtime
- `Pane` 只引用 terminal id，并保存本地显示状态

### 7.2 运行时会话单独建模

terminal attach / channel / stream / snapshot / readonly 等运行时信息从旧 `Viewport` 中拆出，进入 runtime session 模型。

这样做的目的：

- reducer 不持有不可持久化的对象
- renderer 不依赖 client runtime
- workspace persistence 不再混入运行时对象

### 7.3 渲染所需状态采用投影

renderer 不直接读 runtime service，而是读取一份投影后的 screen snapshot。

投影层负责把以下内容合成为渲染输入：

- app state
- workspace / pane 布局
- terminal session snapshot
- overlay / focus / notice

固定渲染链路为：

`state -> projection -> canvas composition -> output`

## 8. 数据流

### 8.1 用户输入

- `input` 根据当前 screen / overlay / focus 把原始键鼠事件翻译为 `intent`
- `intent` 进入 reducer
- reducer 产出新状态与 `effects`

### 8.2 副作用执行

- runtime 统一消费 `effects`
- runtime 调用 `Client`
- runtime 把成功/失败结果回发为 message
- reducer 再把 message 归并进状态

### 8.3 daemon 事件

- runtime 订阅 daemon events
- runtime 把 daemon event 规范化为 app message
- reducer 统一处理 terminal removed / state changed / collaborator revoked / read error 等事件

### 8.4 渲染

- render 从当前 app model 中提取 screen projection
- projection 输入 canvas renderer
- renderer 输出最终 view

## 9. 资产处理策略

### 9.1 可直接迁移的资产

以下内容允许直接迁移或轻改后迁移：

- `deprecated/tui-legacy/pkg/layout.go`
- `deprecated/tui-legacy/pkg/connection_state.go`
- `deprecated/tui-legacy/pkg/render.go` 中与 canvas / border / draw cell 相关的纯渲染实现
- `deprecated/tui-legacy/pkg/workspace_state.go` 中与可持久化结构导入导出相关的纯状态部分
- `deprecated/tui-legacy/pkg/layout_decl.go` 中与 layout spec 解析、导出、校验相关的纯逻辑

### 9.2 只可参考、不可整包迁移的资产

以下内容只能抽思路，不能整包搬回：

- `deprecated/tui-legacy/pkg/model.go`
- `deprecated/tui-legacy/pkg/input.go`
- `deprecated/tui-legacy/pkg/picker.go`
- `deprecated/tui-legacy/pkg/terminal_manager.go`
- `deprecated/tui-legacy/pkg/workspace_picker.go`

原因：

- 它们直接把状态、副作用、UI 流程和输入路由混在一起
- 一旦整包迁回，新主线很快会重新长成 God Object

### 9.3 第一阶段应主动冻结或删除的历史能力

以下历史能力在第一阶段应显式冻结、移除或不迁移：

- prefix mode 及其复杂子模式
- `fit / fixed / pin`
- `auto-acquire resize`
- 以 terminal manager 作为主管理中心的旧交互
- 与产品 spec 不一致的旧空态、旧默认入口和旧快捷键语义

## 10. 第一阶段迁移顺序

### 10.1 先建立主骨架

先建：

- `core`
- `app`
- `runtime`
- `render`
- `input`

这一步的目标不是功能全，而是先让“职责边界”成立。

### 10.2 先迁移 Workbench 主路径

优先完成：

- 默认启动到 workbench
- 启动 live shell pane
- `unconnected / live / exited`
- `connect / create / disconnect / reconnect / kill / remove`

### 10.3 再迁移 Terminal Pool

独立页面必须保留：

- terminal list
- preview
- metadata
- connections
- open here / new tab / floating / kill / remove

### 10.4 再处理持久化与 layout

待主路径稳定后，再把以下内容接回：

- workspace restore
- layout declaration
- 相关 prompt / overlay

## 11. 测试与验证策略

### 11.1 验证原则

- 每次迁移必须带相关测试
- 优先测试纯状态与纯规则
- runtime 路径通过 stub client 验证消息与副作用接线
- renderer 优先测 projection 和关键 view 结果，不依赖真实终端

### 11.2 第一阶段最低验证要求

- 修改包的定向 `go test`
- 全量 `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./... -count=1`

## 12. 风险与控制

### 12.1 最大风险

最大风险不是功能损坏，而是“名义上分层，实质上旧耦合继续潜入新结构”。

最典型的风险信号：

- `Model` 再次开始直接持有 client / stream / vterm
- input 直接改 pane 或直接调 runtime
- renderer 反向依赖 runtime session 内部结构
- feature 代码越写越多又回到一个大文件里

### 12.2 控制策略

- reducer 永远不直接碰 client
- runtime 永远不直接决定页面结构
- renderer 永远只消费投影输入
- 复杂历史能力不为了“先兼容”而强行保留
- 任何新增文件都先回答三个问题：
  - 它的单一职责是什么
  - 它依赖谁
  - 谁依赖它

## 13. 结论

termx TUI 第一阶段不应继续沿着旧 `Model` 打补丁，而应采用“产品目标不变、实现架构重建、旧资产选择性迁移”的方式推进。

最终原则是：

- 产品交互按产品 spec 收敛
- 旧代码按资产价值拆解吸收
- 新主线按 `core / app / runtime / render / input / features` 建立长期可维护边界
