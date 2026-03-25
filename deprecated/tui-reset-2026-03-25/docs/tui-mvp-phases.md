# TUI 分阶段 MVP 计划

本文档基于 [spec-tui-client.md](spec-tui-client.md) 的完整功能定义，将其拆分为可独立交付的阶段。每个阶段结束后 TUI 都应是可用的。

## 可用的 Server 能力

TUI 作为 client，可以使用以下 server 操作：


| 类别   | 操作         | 说明                                                             |
| ---- | ---------- | -------------------------------------------------------------- |
| CRUD | Create     | 创建 terminal（command, name, tags, size, env）                    |
| CRUD | Get        | 查询单个 terminal 信息                                               |
| CRUD | List       | 列出 terminal（按 state/tags 过滤）                                   |
| CRUD | Kill       | 终止 terminal                                                    |
| CRUD | SetTags    | 更新 terminal 元数据                                                |
| I/O  | WriteInput | 发送原始字节到 PTY stdin                                              |
| I/O  | Subscribe  | 订阅 terminal 输出流（返回 StreamMessage）                              |
| I/O  | Snapshot   | 获取完整屏幕状态（screen + scrollback + cursor + modes）                 |
| 控制   | Resize     | 调整 PTY 尺寸                                                      |
| 事件   | Events     | 订阅 server 事件（created/state-changed/resized/removed/read-error） |


StreamMessage 有三种类型：Output（原始 PTY 数据）、SyncLost（丢弃字节通知）、Closed（terminal 退出）。

---

## Phase 0：单 Pane 全屏（最小可交互）

**目标**：能连上 daemon、创建一个 terminal、全屏显示、正常输入输出。等价于 `termx attach` 但用 bubbletea 实现。

**功能**：

- 连接 daemon（未运行时自动拉起）
- Create 一个 terminal（运行 default shell）
- Subscribe 输出流，用本地 VTerm 解析
- 键盘输入转发给 terminal（WriteInput）
- 窗口 resize → Resize terminal
- terminal 退出 → TUI 退出
- Ctrl-C / Ctrl-D 正常传递给 terminal

**不做**：prefix key、split、tab、状态栏、鼠标。

**验收标准**：

- 启动 TUI → 看到 shell prompt
- 能运行 `ls`、`vim`、`htop` 等程序，显示正确
- resize 终端窗口后内容自适应
- shell 中 `exit` 后 TUI 退出

**涉及的 server 操作**：Create, Subscribe, WriteInput, Resize, Snapshot（初始恢复）

---

## Phase 1：Prefix Key + 多 Pane 分屏

**目标**：引入 prefix key 系统和分屏布局，成为一个基本可用的终端复用器。

**功能**：

- **Prefix key**（Ctrl-A）
  - Normal 模式：所有输入发给当前 pane
  - Ctrl-A 后进入 prefix 等待状态（带超时）
  - Ctrl-A Ctrl-A → 发送原始 Ctrl-A
- **分屏**
  - `C-a "` 水平分割（上下）
  - `C-a %` 垂直分割（左右）
  - 布局树（LayoutNode 二叉树）递归计算 pane 矩形
  - 分割时自动 Create 新 terminal + Resize
- **Pane 导航**
  - `C-a h/j/k/l` 或 `C-a ←↓↑→` 切换焦点
  - 活跃 pane 边框高亮
- **Pane 关闭**
  - `C-a x` 关闭当前 pane（Kill terminal）
  - 最后一个 pane 关闭 → TUI 退出
- **Zoom**
  - `C-a z` 当前 pane 全屏切换
- **Pane 边框**
  - Unicode box drawing
  - 标题显示 terminal command/name
- **Detach**
  - `C-a d` 退出 TUI，terminal 继续运行

**不做**：tab、workspace、浮动面板、鼠标、状态栏、命令模式。

**验收标准**：

- 能分出 2-4 个 pane，各自运行不同程序
- h/j/k/l 导航在各 pane 间切换
- zoom 能正确放大/恢复
- 关闭 pane 后布局自动重排
- Ctrl-A d 退出后 `termx ls` 能看到 terminal 仍在运行

**涉及的 server 操作**：Create, Kill, Subscribe, WriteInput, Resize, Snapshot

---

## Phase 2：多 Tab + 状态栏

**目标**：支持多 tab 工作流，加入状态栏提供上下文信息。

**功能**：

- **Tab**
  - `C-a c` 新建 tab（含一个默认 pane）
  - `C-a 1-9` 跳转到第 N 个 tab
  - `C-a n` / `C-a p` 前/后切换 tab
  - `C-a &` 关闭当前 tab（kill 所有 pane，需确认）
  - `C-a ,` 重命名 tab
- **Tab 栏**（顶部）
  - 显示所有 tab 编号和名称
  - 高亮当前 tab
- **状态栏**（底部）
  - 当前 pane 信息（command、terminal ID）
  - 快捷键提示
- **Pane 交换和 resize**
  - `C-a {` / `C-a }` 交换 pane 位置
  - `C-a H/J/K/L` 调整 pane 边界
  - `C-a Space` 循环预定义布局

**不做**：workspace、浮动面板、鼠标、命令模式、copy 模式。

**验收标准**：

- 能创建多个 tab，各自有独立布局
- tab 切换时 pane 内容正确保留
- 状态栏信息准确
- 预定义布局切换正常

---

## Phase 3：渲染优化

**目标**：解决当前渲染架构的性能问题，为后续功能铺路。

**功能**：

- **Render tick batching**
  - 16ms 全局 tick，paneOutputMsg 不触发 View()
  - renderTickMsg 检查 dirty → 条件触发 View()
- **Pane 级 dirty tracking**
  - 只重建有新输出的 pane 的 cell grid
  - 非 dirty pane 复用缓存
- **SyncLost 处理**
  - 收到 SyncLost → 请求 Snapshot → 重置本地 VTerm → full redraw
- **Resize 事务化**
  - resize 事件只标记需要重算，延迟到下一个 tick 统一刷新

**不做**：行级 dirty、终端原生 scroll、cursor motion 优化。

**验收标准**：

- 4 个 pane 同时 `yes | head -10000` 不明显卡顿
- 只有一个 pane 有输出时，其他 pane 不重建 cell grid（可通过计数器验证）
- SyncLost 后恢复正确

---

## Phase 4：鼠标 + 浮动面板

**目标**：增加鼠标交互和浮动面板。

**功能**：

- **鼠标**
  - 点击 pane 切换焦点
  - 点击 tab 栏切换 tab
  - 拖动 pane 边界调整分割比例
  - 滚轮进入 scrollback 预览（简单版：显示 snapshot scrollback）
- **浮动面板**
  - `C-a w` 创建浮动终端（居中 80% 宽高）
  - `C-a W` 切换所有浮动面板可见性
  - 浮动面板有边框和标题
  - `C-a x` 关闭浮动面板
  - 焦点管理：Esc 返回布局 pane
  - 画家算法渲染（底→顶）
- **鼠标 + 浮动面板**
  - 点击浮动面板切换焦点
  - 拖动标题栏移动
  - 拖动边缘 resize

**验收标准**：

- 鼠标点击能正确切换 pane/tab 焦点
- 浮动面板能正确覆盖布局 pane
- 多个浮动面板 z-order 正确

---

## Phase 5：Copy/Scroll 模式 + 命令模式

**目标**：补齐交互模式。

**功能**：

- **Copy/Scroll 模式**（`C-a [`）
  - 基于 Snapshot scrollback
  - j/k 或 ↑/↓ 滚动
  - Ctrl-U/Ctrl-D 半页滚动
  - g / G 顶部/底部
  - v 开始选择、y 复制到系统剪贴板
  - / 和 ? 搜索
  - q 或 Esc 退出
- **命令模式**（`C-a :`）
  - 文本输入框 + Tab 补全
  - 支持：new-window、split、rename-window、kill-pane、kill-window、resize-pane、select-pane、list-terminals、attach

**验收标准**：

- 能在 scrollback 中搜索和复制文本
- 命令模式能执行所有列出的命令

---

## Phase 6：Workspace + 模糊搜索 + 布局文件

**目标**：完整的多 workspace 工作流。

**功能**：

- **Workspace**
  - 多 workspace 支持（每个有独立 tab 集合）
  - `C-a s` 列出 workspace（picker）
  - `C-a $` 重命名 workspace
- **模糊搜索**（`C-a f`）
  - 搜索所有 terminal（跨 workspace/tab）
  - 搜索 server 上未 attach 的 terminal
  - 选中后跳转或 attach
- **布局文件**
  - YAML 声明式布局
  - `termx --layout dev` 启动
  - `:save-layout` / `:load-layout` 命令
- **重连恢复**
  - 本地 workspace 状态文件
  - 重启 TUI 后按 terminal ID 恢复布局

**验收标准**：

- 多 workspace 切换正确
- 模糊搜索能找到所有 terminal
- 布局文件能正确创建 workspace

---

## Phase 7：渲染进阶优化（中期）

**目标**：spec 中的中期渲染优化。

- 行级 dirty tracking（bitset）
- cell-diff 减少 ANSI 输出
- 浮窗 visibleMask 优化
- 高频 pane 渲染降级策略
- （可选）将 pane 热路径迁移到更底层 renderer

---

## 各 Phase 与 Server 操作的对应


| Server 操作  | P0  | P1  | P2  | P3  | P4  | P5  | P6  |
| ---------- | --- | --- | --- | --- | --- | --- | --- |
| Create     | ✓   | ✓   | ✓   |     | ✓   | ✓   | ✓   |
| Get        |     |     |     |     |     |     | ✓   |
| List       |     |     |     |     |     |     | ✓   |
| Kill       |     | ✓   | ✓   |     | ✓   | ✓   |     |
| SetTags    |     |     |     |     |     |     | ✓   |
| WriteInput | ✓   | ✓   | ✓   |     | ✓   |     |     |
| Subscribe  | ✓   | ✓   | ✓   | ✓   | ✓   |     |     |
| Snapshot   | ✓   | ✓   |     | ✓   |     | ✓   | ✓   |
| Resize     | ✓   | ✓   | ✓   | ✓   | ✓   |     |     |
| Events     |     |     |     |     |     |     | ✓   |


## 总结

- **Phase 0-1** 是最小可用终端复用器（约等于 tmux 的核心功能）
- **Phase 2-3** 让它真正可日常使用
- **Phase 4-6** 补齐差异化功能
- **Phase 7** 是性能打磨

建议从 Phase 0 开始，每个 Phase 完成后做一次集成验证再进入下一阶段。