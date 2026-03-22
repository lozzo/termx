# termx TUI 交互与布局规格

状态：Draft v2
日期：2026-03-21

本文件只回答 4 件事：

1. 当前有哪些概念
2. 当前界面怎么组织
3. 当前用户怎么操作
4. 当前 pane / terminal 生命周期怎么收口

---

## 1. 概念收口

### 1.1 当前主概念

termx TUI 只保留 4 个主概念：

- `workspace`
- `tab`
- `pane`
- `terminal`

补充概念：

- `floating pane`
- `saved pane`
- `terminal manager`

### 1.2 不再主推的概念

这些概念只保留在实现层，不再作为用户主心智：

- view
- viewport
- panel

用户看到的应该是：

- pane 的显示属性
- terminal 的运行状态

---

## 2. 布局模型

### 2.1 workspace

- 一个 workspace 包含多个 tab
- 一个 workspace 有自己的名称、活动 tab、恢复状态

### 2.2 tab

- 一个 tab 包含 tiled pane 和 floating pane
- tiled pane 由布局树组织
- floating pane 由独立矩形和 z-order 组织

### 2.3 pane

- pane 是屏幕上的一个可见区域
- pane 可以绑定一个 terminal，也可以处于 saved / waiting / exited 状态
- pane 默认不作为独立命名对象存在
- 当 pane 绑定 terminal 时，标题应展示 terminal 的真实名称
- pane 更像 terminal 的观察/操作视角

### 2.4 floating pane

- floating pane 与 tiled pane 在同一 tab 内共存
- active floating pane 高于其他 floating pane
- 可移动、可 resize、可切 z-order
- 可以移动到 tab 主内容区域之外
- 应提供快捷键把当前 floating pane 呼回并重新居中

---

## 3. 焦点模型

任一时刻，焦点应明确落在以下之一：

- 当前 active tiled pane
- 当前 active floating pane
- picker
- prompt
- help
- terminal manager

优先级从高到低：

1. prompt
2. picker / terminal manager / help
3. floating layer
4. tiled layer
5. terminal input

规则：

- 高层打开时，低层不直接接收键盘输入
- 高层关闭后，焦点回到最近的合法 pane
- `Esc` 是所有临时层的统一退出键

---

## 4. pane 状态模型

### live pane

- 绑定 running terminal
- 接收正常输入
- 可继续 split / attach / floating / metadata edit

### saved pane

- pane 还在
- terminal 已被移除或尚未绑定
- 当前正文应显示可选下一步：
  - start new terminal
  - bring running terminal here
  - open terminal manager
  - close pane

### exited pane

- terminal 已退出，但历史仍保留
- pane 继续存在
- 可 restart 或 attach 其他 terminal

### waiting pane

- 常见于 layout / restore 的未决状态
- 表示此 pane 预留给后续 attach/create

---

## 5. pane 与 terminal 的关系

### 5.1 绑定关系

- 一个 pane 同时只绑定一个 terminal
- 一个 terminal 可以被多个 pane 绑定

### 5.2 关闭 pane

- 只关闭 pane
- terminal 默认继续运行
- 不广播给其他客户端

### 5.3 stop terminal

- stop 的是 terminal 实体
- 所有绑定 pane 都进入 saved pane
- 其他仍在线的客户端应收到 notice
- stop 前必须有确认 prompt

### 5.4 detach TUI

- 当前客户端退出 TUI
- 不结束 terminal
- 不强提示其他客户端

---

## 6. resize / display 规则

### 6.1 display 属性

pane 当前显示层面有这些属性：

- fit / fixed
- readonly
- pin
- size lock warn

### 6.2 shared terminal 的 size 语义

当多个 pane 共享一个 terminal 时：

- pane 的几何可以不同
- terminal 的真实 PTY size 只有一份
- resize 必须通过 acquire 控制语义收口

### 6.3 当前产品方向

termx 的目标规则是：

- 几何变化不自动改写 terminal size
- acquire 后才允许把当前 pane 的尺寸提交给 terminal
- tab 可以配置 auto-acquire
- size lock warn 用来提醒交互式 TUI 风险

---

## 7. 模式与快捷键

当前 keymap 结构：

- `Ctrl-p` pane mode
- `Ctrl-r` resize mode
- `Ctrl-t` tab mode
- `Ctrl-w` workspace mode
- `Ctrl-o` floating mode
- `Ctrl-v` display mode
- `Ctrl-f` terminal picker
- `Ctrl-g` global mode

全局原则：

- mode 是短驻留工具，不是产品本体
- 用户在 normal 状态下就应能稳定工作
- 错误按键直接忽略，不应卡死
- 底栏左侧快捷键提示采用接近 zellij 的连续 segment 样式
- 每个 segment 形如 `<g> LOCK`、`<p> PANE`
- 底栏右侧继续只放状态，不与快捷键混排

---

## 8. 各交互面的职责

### 8.1 Terminal Picker

职责：

- 快速 bring / attach terminal
- 创建 terminal
- 从当前工作流里做最短路径选择

### 8.2 Terminal Manager

职责：

- 浏览 terminal pool
- 看 terminal 当前是否 visible / parked / exited
- 看某个 terminal 显示在哪些位置
- bring here / new tab / floating / edit / stop

### 8.3 Metadata Prompt

职责：

- 编辑 terminal 的 name 与 tags
- 明确提示当前编辑对象是 terminal

当前 prompt 规则：

- step 1/2 编辑 name
- step 2/2 编辑 tags
- prompt 中显示 terminal id
- prompt 中显示 command
- 保存成功后刷新所有 attach pane

### 8.4 Help

职责：

- 解释 keymap
- 解释当前概念模型
- 让新用户理解 workspace/tab/pane/terminal 的关系

---

## 9. 当前界面分层

### 顶栏

放：

- workspace badge
- tab strip
- workspace 级摘要和 notice

### pane 标题栏

放：

- pane/terminal 名称
- `live / saved / exit / fit / fixed / share` 等关系状态
- pane 顶部 chrome 使用单线边框表达，不使用厚重阴影感标题块

### 底栏

放：

- 左侧 mode 快捷键提示
- 右侧当前焦点的极简摘要

补充规则：

- 左侧快捷键展示尽量完全复刻 zellij 的 segment 气质
- 右侧不参与 segment 带，只保留状态摘要

### 中间 overlay

放：

- picker
- prompt
- help
- terminal manager

---

## 10. 当前空状态与默认路径

默认路径：

- 启动 termx
- 进入 main workspace
- 默认已有一个可输入 shell pane

不再主张：

- 启动后先显示一大段说明文字
- 用户先读说明再开始工作

saved pane 才是“无 terminal 但仍保留工作位”的状态；默认启动不应把 saved pane 当主入口。

---

## 11. 当前已达成的主要交互结论

当前交互模型已经比较明确：

- 普通工作：围绕 pane / tab / floating 进行
- terminal 复用：通过 picker / manager 进入
- terminal 管理：通过 terminal manager 进入
- terminal 停止：通过 confirm prompt 明确影响范围
- metadata：通过两步 prompt 明确编辑的是 terminal

这套模型已经足够支持后续继续做 UI 美化、快捷键收口和 e2e 扩展。
