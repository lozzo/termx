# termx TUI 产品规格书

状态：Draft v1

适用范围：

- termx 交互式 TUI 客户端
- 面向本地用户、开发者、OPS/SRE、值班/故障处理场景
- 约束于 termx 当前架构：TUI 与 server 托管 terminal runtime 解耦

---

## 1. 产品定位

termx TUI 不是 tmux/zellij 的复刻版。

它的目标是：

- 在使用感知上更接近 zellij：上手直接、结构直观、进入即能工作
- 在能力模型上保留 termx 特性：terminal 由 server 托管，可被复用、观察、重新附着

一句话定义：

- `像 zellij 一样直接工作`
- `像 terminal runtime 控制台一样组织 server 上的 terminal 池`

---

## 2. 核心架构前提

## 2.1 TI 与 TR 解耦

- TUI 是 `TI`：Terminal Interface
- server 托管的 terminal runtime 是 `TR`
- TUI 只负责：
  - 展示 terminal
  - 选择 terminal
  - attach / detach
  - 操纵 terminal
  - 组织 workspace / tab / pane

- TUI 不负责 terminal 的根生命周期

结论：

- 关闭或 detach TUI，不应导致 terminal 退出
- 同一个 terminal 可以被多个 pane 同时观察
- workspace 不是 terminal 的宿主，而是工作现场的组织方式

## 2.2 与 tmux / zellij 的区别

tmux/zellij 更像：

- 一个把布局、会话、terminal 生命周期绑在一起的系统

termx 更像：

- 一个有 terminal pool 的 server
- 一个附着其上的 TUI 工作台

因此产品设计必须优先表达：

- attach / reuse 是第一等能力
- metadata 是 terminal 级别，而不是 pane 级别
- workspace/layout 是“如何进入和组织工作现场”

---

## 3. 目标用户与高频任务

## 3.1 开发者

典型任务：

- 打开项目工作区，立刻有 shell 可输入
- 分屏运行 server、test、git、临时命令
- 新建 tab 区分 `dev / logs / build`
- 复用已有 terminal 到新 pane / 新 tab
- 给 terminal 命名、打 tag，便于后续恢复与匹配

## 3.2 OPS / SRE

典型任务：

- 同时观察多个长期运行 terminal
- 在多个环境 workspace 间切换
- 临时拉起 floating pane 观察 `htop`、日志、诊断 shell
- 尽量只 attach 观察，不误抢控制
- 某个 terminal 退出后快速恢复或重连

## 3.3 值班 / 故障处理

典型任务：

- 保留主操作 pane，同时在浮窗看日志/监控
- 快速切换 tab / pane / floating 焦点
- 出错时查看日志文件与最近渲染/attach 信息
- detach 后重新进入，尽量恢复原工作现场

---

## 4. 产品目标与非目标

## 4.1 目标

- 直接运行 `termx` 即可进入可工作的 workspace
- 用户只需理解少量概念：`workspace / tab / pane / terminal`
- attach / reuse / restore 成为自然工作流，而不是高级功能
- tiled 与 floating 共存时焦点清晰、层级清晰、行为稳定
- metadata、picker、workspace 切换对 OPS/恢复场景真正可用

## 4.2 非目标

- 不追求完全兼容 tmux 的概念和快捷键
- 不把所有底层内部实现概念暴露给用户
- 不在本阶段做复杂 IDE 型能力
- 不让 UI 设计继续在旧补丁上叠补丁

---

## 5. 用户可见概念

## 5.1 保留的概念

### workspace

- 用户的最外层工作单元
- 对体验上可类比 zellij/tmux session
- 表达“当前正在处理哪组工作现场”

### tab

- workspace 内的页面单位
- 对体验上可类比 tmux window
- 用于分组不同任务面，如 `dev / logs / build`

### pane

- 屏幕上的一个可见区域
- 可以是 tiled pane，也可以是 floating pane
- pane 是 terminal 的展示入口，不是 terminal 本体

### terminal

- server 托管的真实运行实体
- 有自己的 name、tags、command、state
- 可以被多个 pane attach / reuse

## 5.2 对用户弱化的概念

### view

- 不再作为一等用户概念
- `fit / fixed / readonly / pin` 作为 pane 的属性呈现

### panel

- 不再引入为单独概念

---

## 6. 信息架构

主层级：

1. workspace
2. tab
3. pane
4. terminal

其中：

- workspace/tab/pane 是 UI 组织结构
- terminal 是被组织、被 attach 的底层资源

规则：

- 一个 tab 内可有多个 tiled pane
- 一个 tab 内可有零到多个 floating pane
- 一个 terminal 可被多个 pane 同时 attach
- 一个 pane 任一时刻只绑定一个 terminal

---

## 7. 启动与进入规则

## 7.1 默认启动

用户执行 `termx` 时：

- 直接进入一个临时 workspace
- 若没有显式 workspace/layout 参数：
  - 默认创建一个可立即输入的 shell pane
- 不应先落在“只有说明文字、不能工作”的壳页面

这是最重要的主路径。

## 7.2 从 workspace/layout 启动

支持以下入口语义：

- `termx --workspace <file>`
- `termx --layout <file>`
- 进入后打开 workspace picker 选择

关系定义：

- `layout` 是模板
  - 描述 pane 结构、floating 结构、匹配规则、创建策略
  - 可以复用到多个 workspace
- `workspace` 是实体
  - 表示某一次真实工作现场
  - 包含当前 tabs、panes、floating、active focus、已附着 terminal 等运行态

可以理解为：

- layout = blueprint / template
- workspace = live instance / stateful session

规则：

- workspace 可以从某个 layout 派生出来
- workspace 后续运行中的变化不会自动回写 layout
- 若需要保存为可复用模板，应显式导出为 layout
- 若需要保存当前工作现场，应保存 workspace state

- 如果文件可直接解析并匹配 terminal，则直接进入
- 如果部分 terminal 无法解析，则按策略：
  - attach existing
  - create new
  - prompt user
  - skip unresolved

## 7.3 恢复入口

当存在上次 workspace 状态时：

- 可以作为显式恢复入口
- 恢复失败时必须稳定降级：
  - 不闪退
  - 不黑屏
  - 给出明确提示
  - 允许用户退回 workspace picker 或临时 workspace

---

## 8. 主界面规格

## 8.1 顶栏

职责：

- 显示当前 workspace
- 显示 tab 列表
- 清楚标识当前 tab

要求：

- 结构轻量，接近 tmux 的直接感
- active tab 与 inactive tab 区分明显
- 不堆过多状态文字

建议信息密度：

- 左：workspace badge
- 中：tab strip
- 右：简短 workspace 状态

## 8.2 中央工作区

职责：

- 渲染 tiled pane
- 叠加 floating pane
- 展示 active focus

要求：

- tiled 与 floating 视觉层级清楚
- 非焦点 pane 降权，但仍可读
- floating pane 必须完整遮挡底层文字，不能穿透

## 8.3 底栏

职责：

- 左侧只展示当前模式下最重要的快捷键提示
- 右侧展示当前状态摘要

要求：

- 左右分离
- 不堆满整屏说明
- 模式 badge 明确

右侧建议状态：

- 当前焦点层：tiled / floating
- active pane/terminal 简要身份
- terminal state
- 必要时显示 notice / error

---

## 9. Pane 模型

## 9.1 Tiled pane

用途：

- 主要工作层
- 长时间停留和连续输入

支持动作：

- split
- focus move
- resize
- close pane
- attach existing terminal
- create new terminal

## 9.2 Floating pane

用途：

- 临时观察
- 辅助操作
- 快速呼出与快速关闭

支持动作：

- new floating
- attach existing terminal
- cycle focus
- raise / lower z-order
- move
- resize
- hide/show all
- close floating pane

核心规则：

- floating 是 pane 的展示层，不是新资源类型
- 同一个 terminal 可同时出现在 tiled 和 floating
- 焦点在 tiled 和 floating 之间切换必须稳定

---

## 10. Terminal 复用与 attach 规则

## 10.1 复用是主能力

termx 必须把“复用已有 terminal”当成主能力，而不是附加能力。

用户应能：

- attach 到当前 pane
- attach 到新 split
- attach 到新 tab
- attach 到 floating pane

## 10.2 attach 行为规则

- attach 后进入 termx 的完整布局，不得直接裸进 shell
- attach 不得绕过边框、状态栏、焦点系统
- attach existing terminal 与 create new terminal 应共用一致的进入体验

## 10.3 默认启动创建的 terminal

当用户直接执行 `termx` 且当前没有指定 workspace/layout 时：

- 系统会在临时 workspace 中默认创建一个 shell terminal
- 该 terminal 默认继承：
  - 当前 cwd
  - 当前环境变量

退出语义：

- 退出/detach TUI 不自动删除该 terminal
- terminal 是否继续存在，由 server runtime 与显式 kill/close 决定
- 如果用户明确 kill terminal，才视为删除 terminal

这是因为：

- TUI 与 terminal runtime 解耦
- TUI 退出不应隐式销毁 server 上的 terminal

## 10.4 terminal 移除与 pane 生命周期

规则区分：

- `terminal exited but retained`
  - terminal 已退出，但 server 仍保留元数据/恢复信息
  - pane 可显示 exited 状态，并提供 restart/rebuild
- `terminal removed/killed`
  - terminal 资源已被明确移除
  - 绑定它的 pane 应自动消失

面向用户的行为要求：

- 在 tab 中，某个 pane 对应的 terminal 被移除后，该 pane 一并关闭
- 关闭后其余 pane 自动重排
- 不保留“空壳 pane”

这条规则更接近 zellij/tmux 的使用预期。

## 10.5 多重 attach 规则

- 同一 terminal 可同时被多个 pane 观察
- terminal 尺寸变化的竞争必须有明确定义
- 至少要保证：
  - 不崩溃
  - 不闪退
  - 不出现明显错误渲染
  - 用户可理解当前谁在主导尺寸

备注：

- 这部分当前先以“稳定优先”实现
- 更细的尺寸仲裁策略可作为后续优化项

## 10.6 共享 terminal 的尺寸规则

一个 terminal 在 server 侧任一时刻只有一个真实 PTY 尺寸。

因此当同一个 terminal 同时出现在多个 pane 时，必须区分：

- terminal runtime size
- pane display size

产品规则：

- 共享 terminal 不采用隐式 owner 模型
- pane 几何变化本身，不自动改写 terminal 的真实 PTY 尺寸
- terminal size 只有在用户或策略显式 `acquire resize` 时才会改变

显式 acquire 的来源：

- 用户在当前 pane 明确执行 resize acquire
- 用户对 floating pane 执行显式“获取尺寸控制”后再缩放
- tab 配置开启“进入 tab 自动 acquire resize”时，切回该 tab 可自动获取一次 resize 控制

未 acquire 的 pane 显示规则：

- 允许裁剪、滚动、留白或局部不可见
- 不因为 pane 自身尺寸变化自动抖动 terminal
- 用户应能理解“当前 pane 只是观察，不在驱动 terminal resize”

## 10.7 共享 terminal 的 size lock

某些 terminal 内部运行的是 TUI 程序。

如果频繁 resize，可能导致：

- 渲染错乱
- 局部重绘异常
- alt-screen/TUI 程序状态异常

因此 terminal 需要支持一个可配置的 size lock 提示标签。

建议统一使用 terminal tag 表达，例如：

- `termx.size_lock=off`
- `termx.size_lock=warn`

语义：

- `off`
  - 不提示
  - 允许显式 acquire 后直接 resize
- `warn`
  - 在用户尝试 acquire/resize 时给出提示
  - 提示“变更 terminal size 可能影响内部输出”
  - 用户确认/解锁后再继续 resize

说明：

- 这不是绝对禁止 resize 的硬锁
- 本质上是一个可配置的保护提示
- 可按 terminal 元数据配置，默认值可由系统统一写入
## 10.8 共享 terminal 的关闭与销毁规则

需要区分 3 类动作：

### close pane

- 只关闭当前 pane
- terminal 继续存在
- 其他绑定同一 terminal 的 pane 不受影响

### kill/remove terminal

- 明确销毁 terminal runtime
- 所有绑定该 terminal 的 pane 一并关闭
- 不保留空壳 pane

### terminal exited but retained

- terminal 已退出，但恢复信息仍在
- 所有绑定该 terminal 的 pane 都进入 exited 状态
- 保留历史内容，但已退出历史应回到中性前景色，不继续保留程序自身的高饱和前景色
- 任一 pane 都可触发 restart/rebuild

多人共享补充规则：

- `close pane`
  - 只影响当前客户端当前 pane
  - 不广播给其他客户端
- `detach TUI`
  - 只影响当前客户端
  - 不广播强提示给其他客户端
  - 可静默更新 attached 状态或 attached count
- `kill/remove terminal`
  - 影响共享资源
  - 必须通知其他仍 attach 的客户端
  - 若系统可识别操作者身份，提示中应包含操作者信息

建议提示文案：

- 当前操作者：
  - `terminal 'api-prod' removed; closed 3 bound panes`
- 其他客户端：
  - `terminal 'api-prod' was removed by another client`
  - 或 `terminal 'api-prod' was removed by lozzow@host`

权限建议：

- 只读观察者不允许 kill/remove terminal
- 具备控制权限的 attach 客户端才可 kill/remove
- 当 terminal 被多个客户端共享时，kill/remove 最好先做二次确认

如果某个共享 pane 被关闭：

- 若还有其他 pane 绑定该 terminal：
  - terminal 保持当前尺寸不变
  - 后续只有新的显式 acquire 才会改写 terminal size
- 若已没有任何 pane 绑定该 terminal：
  - terminal 仍可继续存在于 server，由显式 kill/remove 决定是否销毁

---

## 11. Metadata 规则

metadata 属于 terminal，不属于 pane。

字段：

- `name`
- `tags`
- `command`
- `state`

规则：

- 创建 terminal 时可直接输入 name
- 创建 terminal 时可直接输入 tags
- name 默认应生成友好名称，不显示随机短串作为主名称
- tags 用于：
  - picker 检索
  - layout 解析
  - workspace 恢复辅助
  - 用户分组识别

编辑规则：

- 运行中可编辑 terminal metadata
- 修改后所有 attach 到该 terminal 的 pane 立即刷新标题与 tags
- 当前 workspace 不自动重排

---

## 12. Picker 规格

## 12.1 Terminal picker

用途：

- 搜索 terminal
- attach existing terminal
- create new terminal
- 显示 terminal 的 name / tags / command / state / attached count

要求：

- 居中 modal
- 背景实色遮挡
- 选中项整行高亮
- 支持按 name / command / tags 过滤
- exited terminal 也可见，并提供合理的后续动作

## 12.2 Workspace picker

用途：

- 切换 workspace
- 创建 workspace
- 进入指定工作现场

要求：

- 居中 modal
- 关闭后不残留 UI 污染
- 新建和切换路径都应清晰

---

## 13. 模式与交互规则

termx 可以保留 mode-based 交互，但不能让模式系统成为认知负担。

要求：

- Normal 是默认稳定状态
- 错误按键不能让 UI 卡死
- `Esc` 永远是可靠退路
- 模式提示只显示当前最关键的动作

交互原则：

- 用户先完成工作，再理解模式
- 模式只服务高频操作聚类，不暴露内部概念

---

## 14. Floating 交互规格

## 14.1 焦点

- 用户必须能明确进入 floating 层
- 也必须能稳定返回 tiled 层

## 14.2 层级

- active floating pane 总在视觉上最突出
- z-order 切换后绘制结果与用户认知一致

## 14.3 鼠标

至少支持：

- 拖动移动 floating pane
- 拖动右下角缩放 floating pane

## 14.4 展示

- 多浮窗不应完全重叠到无法分辨
- 新建浮窗应有错开策略
- 状态栏需要表达当前是否处于 floating 焦点层

---

## 15. 视觉规格

## 15.1 视觉原则

- 先清晰，再美观
- 先层级，再装饰
- 遮挡必须真实，不允许文字穿透
- 非焦点组件降权，焦点组件高亮

## 15.2 色彩角色

- Primary：active tiled pane / 主状态
- Accent：active floating pane / 浮层焦点
- Muted：非焦点 pane / 降权信息
- Inverted：picker 选中项 / mode badge
- Error：错误、危险操作、敏感编辑

## 15.3 组件样式

- 全局使用统一单线边框
- modal 使用不透明背景
- 底栏不堆长文本说明
- 帮助页和 picker 可以比常态界面信息更密，但要保持分区清楚

---

## 16. 日志、错误与恢复

## 16.1 错误处理

要求：

- attach 失败、render 异常、layout 解析失败时有明确提示
- 不允许因为常见错误路径直接 panic 退出

## 16.2 日志

要求：

- 支持输出日志到文件
- 日志能定位：
  - attach / detach
  - terminal metadata update
  - layout load / resolve
  - render 性能摘要
  - 异常和恢复路径

## 16.3 恢复

要求：

- exited pane 上有清晰 restart 路径
- 恢复应尽量继承原 command / metadata
- 恢复失败要告知原因，并允许继续工作

---

## 17. 关键用户流程

## 17.1 默认启动

1. 用户执行 `termx`
2. 进入临时 workspace
3. 默认创建并聚焦一个 shell pane
4. 用户直接开始输入

退出补充：

- 若用户 detach/quit TUI，仅退出前端
- 默认 shell terminal 继续由 server 托管
- 若用户在 pane 内退出 shell，则按 terminal retained/removed 策略处理 pane

## 17.2 split 并继续工作

1. 用户在当前 pane 触发 split
2. 系统提供：
   - create new terminal
   - attach existing terminal
3. 用户完成选择
4. 新 pane 就位
5. 用户继续输入，不需要额外恢复焦点

## 17.3 attach 到 floating pane

1. 用户打开 floating chooser
2. 搜索并选择已有 terminal
3. terminal 进入 floating pane
4. tiled 主工作区保留
5. 用户可在 tiled / floating 之间切换焦点

## 17.4 编辑 terminal metadata

1. 用户对当前 terminal 触发 edit metadata
2. 先输入 name
3. 再输入 tags
4. 保存成功后所有 attach pane 同步刷新

## 17.5 detach 后重进

1. 用户 detach TUI
2. server 上 terminal 持续运行
3. 用户重新进入 termx
4. 通过 workspace/picker/layout 恢复附着现场

---

## 18. 验收标准

本规格至少要求以下主线场景可稳定通过：

- 直接启动进入可工作 workspace
- split 后继续输入
- 复用 terminal 到新 tab
- 复用 terminal 到 floating pane
- 编辑 terminal metadata 并同步刷新
- exited terminal 恢复
- workspace 切换
- 从 layout/workspace 文件启动

测试策略：

- 单元测试覆盖状态机、数据结构、渲染规则
- e2e 测试覆盖用户主场景
- 性能测试覆盖渲染回归与高频刷新路径

---

## 19. 当前已知待定项

- 最终快捷键映射是否完全沿用现行 v3
- terminal 尺寸竞争的最终仲裁模型
- workspace 文件格式与 layout 文件格式是否合并
- observer / collaborator / readonly 等控制语义是否还需要更细的视觉分层

这些不阻塞本规格作为当前主线开发依据。

---

## 20. 文档关系

本文件是当前 TUI 的主规格书。

配套文档：

- `docs/tui/interaction-spec.md`：交互、焦点、mode、pane 生命周期
- `docs/tui/workspace-layout-spec.md`：workspace 与 layout 的关系、启动/恢复/保存规则
- `docs/tui/scenarios.md`：更细的场景池
- `docs/tui/e2e-plan.md`：测试矩阵
- `docs/tui/design-reset.md`：重置背景与设计收口说明
- `docs/tui/deprecated/`：历史废弃方案归档
