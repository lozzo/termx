# termx TUI 用户场景

状态：Draft v1
日期：2026-03-23

这份文档只讨论用户要完成什么，不讨论内部实现。

---

## 1. 启动与进入

### S1 直接运行 `termx`

- 用户执行 `termx`
- 直接进入可工作的 workspace
- 默认有一个可输入 shell pane
- 退出 TUI 后 terminal 仍继续运行

### S2 从 workspace 恢复

- 用户再次进入 termx
- 希望恢复上次工作现场
- 恢复失败时允许降级，不应黑屏或闪退

### S3 从 layout 文件启动

- 用户通过 layout 定义进入工作现场
- layout 可描述 pane 结构、floating 结构和 terminal 匹配策略

---

## 2. 基础工作流

### S4 当前 pane 内直接工作

- 用户进入后立即可以执行 shell 输入
- 不需要先理解复杂概念或切模式

### S5 split 当前 pane

- 用户把当前 pane 分成两个工作位
- 新 pane 可以新建 terminal
- 也可以 connect 已存在 terminal

### S6 调整 pane 布局

- 用户在多个 pane 间切焦点
- resize 后布局仍稳定
- 不出现焦点丢失和布局错乱

### S7 关闭 pane

- 用户关闭的是当前工作位
- terminal 默认继续运行
- UI 能解释清楚 close pane 与 stop terminal 的差异

---

## 3. Tab 与 Workspace

### S8 新建 tab

- 用户为另一个任务面新建 tab
- 新 tab 可以直接开始工作

### S9 切换 tab

- 用户在 `dev / logs / ops / build` 等 tab 间快速切换
- tab 切换后焦点和上下文清楚

### S10 创建 / 切换 workspace

- 用户在多个项目或环境间切换
- 切换 workspace 不影响后台 terminal
- workspace picker 应支持树形展示 tab 和 pane
- 用户可以直接跳到某个 pane

---

## 4. Terminal 复用

### S11 connect 已有 terminal 到当前 pane

- 用户打开 picker
- 搜索 terminal
- connect 到当前 pane

### S12 connect 到新 split

- 同一个 terminal 同时出现在两个 pane
- 两个 pane 各自有自己的显示几何

### S13 connect 到新 tab

- 同一个 terminal 在多个 tab 可见

### S14 connect 到 floating pane

- 同一个 terminal 在 tiled 和 floating 中同时可见
- 焦点切换和 z-order 稳定

### S15 terminal exited 后恢复

- terminal 中程序退出后，pane 进入“程序已退出”状态
- 用户可以 restart
- 也可以 connect 其他 terminal

---

## 5. Floating

### S16 新建 floating pane

- 用户临时开一个浮窗做观察或辅助操作

### S17 在 floating 和 tiled 间切焦点

- 用户清楚当前在浮层还是主布局
- 可以稳定退回 tiled

### S18 呼回跑远的浮窗

- 用户把 floating 拖到主视口外
- 通过快捷键居中呼回

### S19 多浮窗管理

- 用户切换浮窗
- 调整 z-order
- 移动和缩放
- 隐藏和恢复所有浮窗

---

## 6. Metadata 与管理

### S20 创建 terminal 时命名

- 用户创建 terminal 时输入友好名称

### S21 给 terminal 打 tags

- tags 用于搜索、分组、layout 匹配

### S22 修改已有 terminal metadata

- 运行中的 terminal 也能修改 name / tags
- 所有已连接 pane 的标题同步刷新

### S23 terminal manager 管理 terminal pool

- 用户查看 terminal 当前状态和位置
- 用户可以在 manager 中直接把当前 pane connect 到选中的 terminal
- 执行 here / new tab / floating / edit / stop

---

## 7. 共享 terminal

### S24 shared terminal 只允许一个 owner

- 一个 terminal 可被多个 pane connect
- 但同一时刻只有一个 owner

### S25 resize 需要显式 acquire

- follower 不隐式改写 terminal size
- acquire 后才允许 resize terminal

### S26 owner 迁移

- owner pane 被关闭、解绑或退出时
- 系统稳定迁移 owner

### S27 任意 pane 或客户端可获取 owner

- 任何已附着的 pane 或客户端都可以请求 owner
- 获取后才能执行 terminal 控制面动作

---

## 8. 恢复与错误处理

### S28 stop terminal 后保留未连接 terminal 的 pane

- 用户 stop terminal 后
- 布局保留
- 用户仍可在原位置重新 connect / create

### S29 remote remove / exit 同步

- 其他客户端导致 terminal remove 或 exit
- 当前客户端能收到正确状态更新

### S30 detach / quit TUI

- 用户退出 TUI
- terminal 继续运行
- 再次进入后仍可继续 connect

---

## 9. 高频用户任务

### S31 开发者日常开发

- 一个 workspace 多个 tab
- 每个 tab 多个 pane
- 一部分浮窗临时观察日志和系统状态

### S32 运维巡检

- 同时 connect 多个长期 terminal
- 关键 terminal 用 floating 临时拉高观察
- workspace 用于切环境

### S33 发布与故障处理

- 同时保留命令行、日志、runbook、诊断工具
- 同一个 terminal 在多个位置复用观察

---

## 10. 场景使用原则

这份文档后续主要用于两件事：

1. 校验产品设计有没有漏主路径
2. 驱动回归测试和 E2E 命名
