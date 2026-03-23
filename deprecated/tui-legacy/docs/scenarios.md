# TUI 用户使用场景

这份清单只讨论“用户想完成什么”，不预设旧设计中的概念和快捷键。

## A. 启动与进入

### A1. 直接运行 `termx`

- 用户在普通 shell 中输入 `termx`
- 系统直接进入一个临时 workspace
- 如果没有任何 terminal，可直接创建默认 shell(从当前路径,当前环境变量复制)
- 如果已有 terminal，可弹出复用选择，但默认路径仍应足够顺滑
- 用户退出 TUI 时，不自动销毁这个 terminal

### A2. 从指定 workspace / layout 启动

- 用户通过命令参数指定某个 workspace 文件或 layout 文件
- 系统直接进入对应 workspace
- 若需要 terminal 解析，按显式规则 attach / create / prompt

### A3. 启动后选择 workspace

- 用户先进入 termx，再从 workspace picker 里选择已有 workspace
- 适合从“入口壳”切到已有项目工作区

### A4. 恢复上次工作现场

- 用户进入 termx 时，希望恢复上次 workspace 状态
- 如果恢复失败，要有清晰降级路径，不闪退、不黑屏

## B. 基本操作

### B1. 当前 pane 内直接使用 shell

- 用户进入后立刻有一个可输入的 pane
- 不需要先理解复杂概念
- pane 顶部默认显示的是 terminal 真名，而不是 pane 自己的独立名称

### B2. 新建 pane / split

- 在当前 tab 内做上下/左右分屏
- 新 pane 可以：
  - 新建 terminal
  - 复用已有 terminal

### B3. 切 pane 焦点

- 在同一 tab 的多个 pane 之间移动
- 用户总能看清当前焦点在哪

### B4. 调整 pane 大小

- 快速缩放当前 pane
- 调整后其它 pane 不出现混乱布局

### B5. 关闭 pane

- 关闭的是当前显示入口，不一定 kill 底层 terminal
- 用户能清楚区分：
  - close pane
  - kill terminal
- terminal 被明确移除后，对应 pane 也要一并消失

## C. Tab / Workspace

### C1. 新建 tab

- 在同一 workspace 中新增一个 tab
- 新 tab 可以是空页，也可以直接新建/复用 terminal

### C2. 切换 tab

- 在同一 workspace 中跳转不同 tab
- tab 的识别必须轻量、直观

### C3. 新建 workspace

- 用户围绕一个项目/任务新开工作区
- workspace 更像 zellij/tmux 的 session，而不是一个抽象对象

### C4. 切换 workspace

- 用户在多个项目工作区间切换
- 切换不影响后台 terminal 运行

## D. Terminal 复用

### D1. 复用已有 terminal 到当前 pane

- 用户在 picker 里选择已有 terminal
- attach 到当前 pane

### D2. 复用已有 terminal 到新 split

- 同一个 terminal 在两个 pane 中同时可见

### D3. 复用已有 terminal 到新 tab

- 同一个 terminal 跨 tab 同时可见

### D4. 复用已有 terminal 到 floating pane

- 同一个 terminal 同时出现在 tiled pane 和 floating pane

### D5. terminal 退出后恢复

- 用户在 exited pane 上执行 restart / rebuild
- 新 terminal 继承原 command / metadata

## E. Floating

### E1. 新建 floating pane

- 用户临时开一个浮窗做观察或辅助操作
- 浮窗标题默认显示 terminal 真名
- 浮窗可以拖动到 tab 主内容区域之外

### E2. 在 tiled 和 floating 间切换焦点

- 用户要能明确从“主布局”切到“浮窗层”
- 也能稳定退回 tiled

### E2.1 呼回跑远的 floating pane

- 用户把浮窗拖到了 tab 主视口外
- 可以通过快捷键把当前浮窗呼回并自动居中
- 不需要依赖鼠标慢慢拖回来

### E3. 多浮窗管理

- 多个浮窗之间切焦点
- 调 z-order
- 移动 / 缩放

### E4. 隐藏 / 显示所有浮窗

- 用户临时隐藏浮窗，不丢失上下文

## F. Metadata 与选择

### F1. 创建 terminal 时命名

- 用户创建 terminal 时可输入友好名称
- 不再依赖随机字符串

### F2. 创建 terminal 时写 tags

- tags 用于后续检索、布局匹配、归类

### F3. 修改已有 terminal metadata

- 运行中也能修改 terminal 的 name / tags
- 所有 attach 到它的 pane 要同步刷新标题和 tags

### F4. terminal picker 检索

- 用户按名称、命令、tags、位置搜索 terminal

### F5. workspace picker 检索

- 用户按 workspace 名称快速切换工作区

## G. 错误、退出与恢复

### G1. 误触模式键 / 组合键

- 错误组合不能让 TUI 卡死
- `Esc` 应始终是稳定退出当前模式
- 底栏快捷键提示应尽量像 zellij 一样直观，用连续 segment 展示主动作

### G2. terminal killed / exited

- killed 和 exited 的 UI 提示必须明确
- `exited but retained` 可以保留恢复入口
- `removed/killed` 则应关闭对应 pane

### G3. TUI detach / quit

- detach TUI 不 kill terminal
- 重新进入后可继续 attach

### G4. 日志与排障

- UI 卡住、attach 异常、render 异常时，日志里能找到上下文

## H. 布局与声明式启动

### H1. 从 layout 文件启动 workspace

- layout 指定 pane 结构、floating、匹配规则

### H2. layout 解析时 attach 现有 terminal

- 按 metadata / 规则匹配已有 terminal

### H3. layout 解析失败时交互补全

- 未匹配到 terminal 时，用户可选 attach / create / skip

## I. 按用户角色归纳的高频任务

这一组场景不按“功能模块”分，而按“用户今天要完成什么”来归纳，便于检查设计是否真的覆盖主线工作流。

### I1. 开发者日常开发

- 进入某个项目 workspace，默认立即进入 shell
- 开 2~4 个 pane：编辑器、server、test、git
- 新建 tab 区分 `dev / logs / build`
- 临时复用已有 terminal 到新 pane 或新 tab
- 给 terminal 命名和打 tag，方便后续复用和 layout 匹配

### I2. 运维 / SRE / OPS 巡检

- 同时观察多个长期运行 terminal
- 将关键 terminal 以 floating pane 临时拉到最上层观察
- 在多个 workspace 间切换不同环境，如 `prod / staging / canary`
- 只 attach 观察，不抢控制权
- terminal 退出后快速恢复、重连、继续排障

### I3. 发布 / 值班 / 故障处理

- 在一个 workspace 中同时保留 runbook、日志、交互 shell
- 将某个关键 terminal 复用到浮窗，边看边操作主 pane
- 快速切换 tab/pane/floating 焦点，不丢上下文
- 出错时查看日志文件和最近渲染/attach 事件

### I4. 临时呼出与快速观察

- 平时以 tiled pane 工作
- 临时呼出 floating pane 看 `htop`、`tail -f`、一次性 shell
- 观察完即关闭或隐藏，不破坏原有平铺布局
- 同一个 terminal 可被多个 pane 同时观察

### I5. 协作与只读观察

- 一个 terminal 被多人或多个 pane attach
- 某些 pane 只读观察，某些 pane 可输入
- metadata/tag 用于标记职责、环境、归属
- TUI detach 后，server 托管的 terminal 继续运行

### I6. 恢复与长期会话

- 用户退出 TUI 后，第二次进入继续 attach 现有 terminal
- workspace 可恢复 tab、pane、floating 结构
- layout 文件优先描述“如何进入工作现场”，而不是只描述静态布局
- 旧 terminal metadata 继续作为后续解析、检索、恢复依据

## 建议对外保留的用户概念

后续 UI 中，优先只保留：

- workspace
- tab
- pane
- terminal（在 picker / metadata 等需要时出现）

后续尽量弱化或隐藏：

- `view`
- `panel`

其中 `view` 更适合作为 pane 的内部属性，而不是用户每天都要理解的概念。
