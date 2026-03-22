# termx TUI 产品规格书

状态：Draft v2
日期：2026-03-21

---

## 1. 产品定义

termx TUI 是一个本地终端里的工作台，用来观察、组织、复用、恢复 server 托管的 terminal。

它不是纯粹的 tmux/zellij 复刻，也不是一个只会 attach 单个 shell 的瘦客户端。

一句话定义：

- `像 zellij/tmux 一样组织界面`
- `像 terminal runtime 控制台一样管理 terminal 池`

---

## 2. 设计目标

termx TUI 必须同时满足两件事：

1. 普通用户直接运行 `termx` 就能开始工作
2. 高级用户能显式利用 terminal 复用、共享、恢复、管理这些能力

产品目标：

- 直接启动即可进入可工作的 workspace
- 用户优先理解 `workspace / tab / pane / terminal`
- 复用已有 terminal 是自然路径，不是隐藏能力
- tiled / floating 共存时焦点和层级清楚
- 退出 TUI 不会隐式结束 terminal
- metadata、picker、manager、restore 对开发和运维都可用

非目标：

- 不追求完整兼容 tmux 语义
- 不追求把底层 server/runtime 模型完全隐藏
- 不在当前阶段做 IDE 式复杂管理面板

---

## 3. 核心心智模型

### 3.1 两层模型

termx TUI 的核心不是单层 session，而是两层：

- 界面层
  - workspace
  - tab
  - pane
  - floating pane
- 运行层
  - terminal

用户应该能逐步理解这句话：

- `pane 是工作位`
- `terminal 是持续运行的实体`

### 3.2 与 tmux / zellij 的关系

使用感受上：

- 新建 pane / tab / floating
- 切焦点
- 关闭 pane
- 打开 picker

这些都应该尽量顺手，尽量像 tmux/zellij。

但语义上：

- pane 不是 terminal 本体
- terminal 可以被多个 pane 共享
- 关闭 pane 不等于结束 terminal
- detach TUI 不等于结束 terminal

---

## 4. 用户可见概念

### workspace

- 最外层工作现场
- 类似 tmux session / zellij session
- 用来区分不同任务或环境

### tab

- workspace 内的页面单位
- 类似 tmux window
- 用来区分 `dev / logs / ops / build` 等任务面

### pane

- 一个可见区域
- 可以是 tiled pane，也可以是 floating pane
- 是 terminal 的展示入口
- pane 默认不是一个需要单独命名的用户对象
- 当 pane 绑定 terminal 时，pane 标题默认展示 terminal 的真实名称
- 只有在没有 terminal 的状态下，才展示 `saved pane` / `waiting pane` 这类状态名

### terminal

- server 托管的真实运行实体
- 有自己的 `name / tags / command / state`
- 可被多个 pane 同时 attach

### floating pane

- tab 内悬浮显示的 pane
- 用于临时观察、监控、诊断、快速操作
- 与 tiled pane 并存

### display attributes

以下不是独立主概念，而是 pane 的显示属性：

- fit / fixed
- readonly
- pin
- size lock warn

---

## 5. 入口与启动规则

### 5.1 默认启动

用户执行 `termx` 时：

- 直接进入一个临时 workspace
- 默认创建一个可输入的 shell pane
- 不应落到只显示帮助文字的空壳页

### 5.2 workspace 与 layout 的关系

- `layout` 是模板
  - 描述 pane 结构、floating 结构、匹配规则、创建策略
- `workspace` 是实体
  - 描述某次真实工作现场的运行状态

关系：

- workspace 可以由 layout 派生
- workspace 运行中的变化不会自动回写 layout
- 显式保存时才分别保存为 workspace state 或 layout template

### 5.3 恢复原则

恢复失败时必须满足：

- 不闪退
- 不黑屏
- 不破坏已有 terminal
- 允许降级到 picker、saved pane 或临时 workspace

---

## 6. 主界面布局

### 6.1 顶栏

职责：

- 展示当前 workspace
- 展示 tab strip
- 展示 workspace 级状态摘要

建议放置的信息：

- 当前 workspace 名称
- tab 列表与 active tab
- pane / terminal / floating 计数
- notice / error / auto-acquire 等全局状态

### 6.2 pane 标题栏

职责：

- 左侧显示 pane/terminal 名称
- 右侧显示该 pane 与 terminal 的关系状态

命名规则：

- pane 不强调独立名字
- pane 绑定 terminal 时，标题左侧优先显示 terminal 名称
- pane 未绑定 terminal 时，显示 pane 状态名
- pane 本质上是 terminal 的观察/操作视角，而不是独立命名实体

当前建议状态词：

- `live`
- `saved`
- `exit`
- `fixed / fit`
- `share:N`
- `obs`
- `ro`
- `pin`
- `lock`

### 6.3 底栏

职责：

- 左侧：当前 mode 的快捷键提示
- 右侧：当前焦点对象的极简摘要

原则：

- 左侧只放操作提示
- 右侧只放当前焦点状态
- 不把所有信息都堆到底栏
- 左侧快捷键展示采用接近 zellij 的连续 segment 风格
- 每个快捷键块用类似 `<g> LOCK`、`<p> PANE` 的形式呈现
- 各 segment 之间用连续分隔符连接成一整条操作带

### 6.4 overlay

包括：

- terminal picker
- workspace picker
- prompt
- help

要求：

- 居中显示
- 实色背景遮挡底层
- 关闭后不留渲染残影

---

## 7. pane 与 terminal 生命周期

### 7.1 关闭 pane

- 只关闭当前展示入口
- 默认不结束 terminal
- 应给出明确提示：`pane closed; terminal keeps running`

### 7.2 terminal exited

- pane 进入 exited 状态
- 历史内容继续可读
- 退出后的历史颜色要回到中性前景
- 任一绑定 pane 可触发 restart

### 7.3 terminal removed / stopped

- 所有绑定该 terminal 的 pane 解除绑定
- pane 保留为 `saved pane`
- 几何布局保持不变
- 用户可在原位置 attach / create / close

### 7.4 多 pane 共享 terminal

允许：

- 同一个 terminal 同时出现在多个 tab / pane / floating 中

原则：

- pane 共享 terminal
- pane 不共享各自的几何
- terminal 真实 PTY size 只有一份

### 7.5 resize 原则

termx 的正确方向是：

- pane 几何变化不自动等于 terminal resize
- resize 必须通过 acquire 语义主动获取
- 可配置 tab auto-acquire
- 可配置 size lock warn

---

## 8. 核心交互面

### 8.1 Terminal Picker

定位：

- 快速选择器
- 服务于“把某个 terminal 带到当前工作位”

动作：

- attach 到当前 pane
- split 后 attach
- 创建新 terminal
- 编辑 metadata
- stop terminal

### 8.2 Terminal Manager

定位：

- 运行实体管理页
- 不是普通 picker 的放大版

职责：

- 查看 terminal pool
- 看某个 terminal 当前显示在哪些 pane 中
- bring here
- open in new tab
- open in floating pane
- edit metadata
- stop terminal

当前分组：

- `NEW`
- `VISIBLE`
- `PARKED`
- `EXITED`

### 8.3 Metadata Prompt

作用：

- 编辑 terminal 的 `name / tags`
- 必须明确表达“你正在编辑 terminal，而不是 pane”

要求：

- 两步流：name -> tags
- 显示 step 信息
- 显示 terminal id 与 command
- 保存后所有 attach pane 同步刷新
- 即使 terminal 当前 parked，也要有成功反馈

### 8.4 Floating pane

floating pane 是比 tiled pane 更自由的观察层。

要求：

- floating pane 不应被严格限制在 tab 主视口矩形内
- 用户可以把 floating pane 移动到 tab 主内容区域之外
- 系统应提供快捷键把当前 floating pane 呼回并重新居中
- “呼回并居中”应是正式交互，不依赖鼠标修正

---

## 9. 快捷键结构

当前交互结构以直接 mode 为主：

- `Ctrl-p` pane
- `Ctrl-r` resize
- `Ctrl-t` tab
- `Ctrl-w` workspace
- `Ctrl-o` floating
- `Ctrl-v` display
- `Ctrl-f` terminal picker
- `Ctrl-g` global

要求：

- `Esc` 永远是统一退路
- 错误按键必须无害
- mode 是辅助，不应成为理解产品前提
- 用户态不再保留 legacy `Ctrl-a`
- `Ctrl-a` 直接透传给当前 terminal
- mode 默认 hold 3 秒，可配置调整
- 连续操作类 mode 在每次有效动作后续期 3 秒窗口

---

## 10. 当前视觉方向

当前实现的方向已经收敛到：

- active pane：高亮绿色边框
- inactive pane：亮灰色边框
- pane 顶部 chrome 回归单线边框，不使用厚重阴影感
- 顶栏与底栏分层
- picker / prompt 居中显示
- terminal manager 全屏展示
- overlay 使用实色背景遮挡

后续视觉优化方向：

- 继续压缩底栏信息密度
- 把底栏快捷键统一收口为 zellij 风格 segment 样式
- 继续提升 modal/picker 的完整性和统一性
- 继续把状态尽量上移到 pane 标题栏与顶栏

---

## 11. 当前产品结论

当前 termx TUI 的正确产品描述应该是：

- 对新用户来说，它首先是一个“能直接工作的终端工作台”
- 对高级用户来说，它进一步是一个“terminal pool 的管理与恢复界面”
- 它的真正价值不在于复刻 tmux，而在于把 `terminal runtime` 从 `TUI 界面` 中解耦出来，并把这种能力做成可理解、可操作、可恢复的产品
