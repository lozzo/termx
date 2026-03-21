# 交互设计

本文档定义 TUI 客户端的交互模型：模式、快捷键、核心交互流程。

设计原则：交互层尽量与 tmux 一致（降低迁移成本），差异化体现在模型层（Viewport 解耦、Terminal 复用）。

## 模式

TUI 有四个模式：

```
┌──────────┐   C-a [   ┌──────────┐
│          │ ────────→ │          │
│  Normal  │           │   Copy   │
│          │ ←──────── │  Scroll  │
└────┬─────┘   q/Esc   └──────────┘
     │
     │ C-a :          C-a f / C-a s
     ▼                    │
┌──────────┐         ┌───▼──────┐
│ Command  │         │  Picker  │
│          │         │          │
└──────────┘         └──────────┘
   Esc/Enter            Esc/Enter
     回到 Normal          回到 Normal
```

| 模式 | 说明 | 进入 | 退出 |
|------|------|------|------|
| Normal | 键盘输入发送给当前 Viewport 的 Terminal | 默认 | — |
| Copy/Scroll | 浏览 scrollback、选择文本、搜索 | `C-a [` | `q` / `Esc` |
| Command | 类 vim 命令行 | `C-a :` | `Esc` / `Enter` |
| Picker | 交互式列表（terminal/workspace 选择） | `C-a f` / `C-a s` | `Esc` / `Enter` |

## Prefix Key

**默认：`Ctrl-a`**

```
按下 Ctrl-a → 进入 prefix 等待状态（800ms 超时）
  │
  ├─ 按下后续键（如 c、%、"）→ 执行对应命令
  ├─ 再按一次 Ctrl-a → 发送原始 Ctrl-a 给 Terminal
  └─ 超时 → 取消 prefix，回到 Normal
```

## 快捷键

### Viewport 操作

```
C-a "       水平分割（上下）— 在当前 Viewport 下方创建新 Viewport
C-a %       垂直分割（左右）— 在当前 Viewport 右侧创建新 Viewport
C-a x       关闭当前 Viewport（detach，Terminal 继续运行）
C-a X       Kill Terminal（销毁 Terminal 和 Viewport）
C-a z       Zoom 切换（当前 Viewport 全屏 / 恢复）

C-a h/j/k/l     在 Viewport 间导航（vim 方向）
C-a ←/↓/↑/→     在 Viewport 间导航（方向键）
C-a H/J/K/L     调整 Viewport 边界（每次 2 行/列）
C-a {           与前一个 Viewport 交换位置
C-a }           与后一个 Viewport 交换位置
C-a Space       循环切换预定义布局
```

### 新建 Viewport（选择已有 Terminal 或新建）

分屏、开新 Tab、开浮窗时，termx 会先给你一个 chooser：

```
C-a % / C-a "   新分屏
C-a c           新 Tab
C-a w           新浮动 Viewport
```

chooser 的顶部固定是 `+ new terminal`，所以：

```
Enter           在这个新 Viewport 里创建一个新 Terminal
搜索 + Enter    在这个新 Viewport 里 attach 已有 Terminal
Esc             取消，不创建新 Viewport
```

如果你已经在当前布局里，并且只是想把某个 Terminal 换进来，仍然可以走旧的 attach 流程：

```
C-a f       → 打开 Picker
Enter       → attach 到当前 Viewport（替换当前 Terminal）
Tab         → 在当前 Viewport 旁边分屏 + attach

C-a : attach <terminal-id>
           → 将指定 Terminal attach 到当前 Viewport
```

**关闭 vs Kill 的区别**：

```
C-a x  关闭 Viewport（detach）
  │
  ├─ Viewport 从布局中移除
  ├─ Terminal 继续在 server 上运行
  └─ 随时可以通过 C-a f 找回并重新 attach

C-a X  Kill Terminal
  │
  ├─ 向 Terminal 发送 kill 信号
  ├─ Terminal 和 PTY 进程被销毁
  └─ 所有观察该 Terminal 的 Viewport 显示 [killed]
```

这是 termx 与 tmux 最重要的交互差异。tmux 的 `C-b x` 是 kill pane（= kill 进程）。termx 的默认操作是 detach，保留 Terminal。

### 浮动 Viewport

```
C-a w       打开 chooser，新建或 attach 一个浮动 Viewport（默认 fixed）
C-a W       切换所有浮动 Viewport 的显示/隐藏
Esc         焦点从浮动层回到平铺层
```

### Tab 操作

```
C-a c       打开 chooser，新建或 attach 一个 Tab
C-a ,       重命名当前 Tab
C-a 1-9     跳转到第 N 个 Tab
C-a n       下一个 Tab
C-a p       上一个 Tab
C-a &       关闭当前 Tab（关闭所有 Viewport，Terminal 继续运行）
```

### Workspace 操作

```
C-a s       列出 Workspace（Picker 模式）
C-a $       重命名当前 Workspace
C-a d       Detach（退出 TUI，所有 Terminal 继续运行）
```

### 诊断日志

termx 现在支持把 CLI / daemon / TUI 的关键日志写到文件：

```bash
termx --log-file /tmp/termx.log
TERMX_LOG_FILE=/tmp/termx.log termx
```

默认路径：

```bash
$XDG_STATE_HOME/termx/termx.log
# 或
~/.local/state/termx/termx.log
```

### Terminal Picker（核心交互）

```
C-a f       打开 Terminal Picker
```

Terminal Picker 是 termx 的核心交互入口，不只是"找 terminal"：

```
┌─ Find Terminal ──────────────────────────────────────┐
│  > build                                             │
│                                                      │
│  ● T2  make watch       ws:dev / tab:coding    fit   │
│  ● T4  tail -f app.log  ws:dev / tab:monitor   fit   │
│  ○ T8  npm run build    (未被任何 Viewport 使用)       │
│                                                      │
│  ● = 有 Viewport 观察中    ○ = 无人观察（orphan）       │
│                                                      │
│  [Enter] attach 到当前 Viewport                       │
│  [Tab]   在新 Viewport 中打开（分屏）                   │
│  [C-k]   Kill Terminal                               │
│                                                      │
│  3 matches                                           │
└──────────────────────────────────────────────────────┘
```

**搜索范围**：terminal command、tags、所在 workspace/tab 名称

**操作**：
- `Enter` — 将选中的 Terminal attach 到当前 Viewport（替换当前 Terminal）
- `Tab` — 在当前 Viewport 旁边分屏，新 Viewport attach 选中的 Terminal
- `C-k` — Kill 选中的 Terminal
- `Esc` — 取消

在新建分屏 / 新 Tab / 浮窗时使用的 chooser 也会复用这份 Terminal 列表，
只是顶部会额外增加一个 `+ new terminal` 入口。

**这是 termx 的杀手功能**。tmux 的 `C-b s` 只能在 session 间切换，而且切换的是整个 session 视图。termx 的 picker 让你在 terminal 粒度上操作——任何 terminal 都可以被拉进当前视图。

### Viewport 模式切换

```
C-a M       切换当前 Viewport 的模式（fit ↔ fixed）
C-a R       切换只读模式（readonly）
C-a P       切换锚定（pin）— 仅 fixed 模式下有效
```

### Fixed 模式完整交互

```
进入 fixed 模式后（C-a M）：

  默认行为：跟随光标
    光标移动 → viewport 自动平移，保持光标在可见区域内
    用户正常输入，viewport 跟着光标走

  锚定（C-a P）：
    → 固定当前视角，不再跟随光标
    → 状态栏显示 [pinned]
    → 再按 C-a P → 取消锚定，恢复跟随

  锚定状态下的手动平移：
    C-a Ctrl-h / C-a Ctrl-←    向左平移 offset（每次 4 列）
    C-a Ctrl-l / C-a Ctrl-→    向右平移 offset（每次 4 列）
    C-a Ctrl-k / C-a Ctrl-↑    向上平移 offset（每次 2 行）
    C-a Ctrl-j / C-a Ctrl-↓    向下平移 offset（每次 2 行）

  注意：平移 offset 用 C-a Ctrl-hjkl，移动浮窗位置用 C-a Alt-hjkl
  两者不冲突，即使 floating + fixed + pinned 的 Viewport 也有明确行为：
    C-a Ctrl-h → 平移 viewport 内容（offset 变化）
    C-a Alt-h  → 移动浮窗位置（窗口整体移动）

  边界处理：
    offset 不能超出 Terminal 屏幕范围
    如果 offset + viewport 大小 > Terminal 大小 → clamp 到边界
```

### 浮动层键盘操作

```
浮动层焦点切换：
  C-a Tab         在浮动 Viewport 之间循环切换焦点（按 z-order）
  Esc             焦点从浮动层回到平铺层

浮动 Viewport z-order：
  C-a ]           将当前浮动 Viewport 提升到最顶层
  C-a _           将当前浮动 Viewport 降到最底层

浮动 Viewport 移动（焦点在浮动层时）：
  C-a Alt-h/j/k/l     移动浮动 Viewport 位置（每次 4 列 / 2 行）
  C-a Alt-H/J/K/L     调整浮动 Viewport 大小（每次 4 列 / 2 行）
```

### Copy/Scroll 模式

```
C-a [       进入 Copy/Scroll 模式
```

进入后（不需要 prefix）：

```
j/k 或 ↑/↓       上下滚动
C-u / C-d         半页滚动
g                  跳到 scrollback 顶部
G                  跳到底部
v                  开始选择
y                  复制到系统剪贴板
/                  向下搜索
?                  向上搜索
n / N              下一个/上一个匹配
q 或 Esc           退出
```

### 其他

```
C-a ?       显示快捷键帮助
C-a :       进入命令模式
C-a C-a     发送原始 Ctrl-a 给 Terminal
```

## 命令模式

`C-a :` 进入，类似 vim 命令行。支持 Tab 补全。

```
:split -h                           水平分割
:split -v                           垂直分割
:new-tab [-n name] [command]        新建 Tab
:rename-tab <name>                  重命名 Tab
:rename-workspace <name>            重命名 Workspace
:kill-viewport                      关闭当前 Viewport
:kill-terminal                      Kill Terminal
:resize -D/-U/-L/-R [n]            调整 Viewport 大小
:set <option> <value>               运行时配置（如 :set mouse off）
:attach <terminal-id>               attach Terminal 到当前 Viewport
:mode fit|fixed                     切换 Viewport 模式

:tag <key>=<value>                  给当前 Terminal 加 tag
:untag <key>                        删除当前 Terminal 的 tag
:list-terminals                     列出 server 上所有 Terminal
:list-orphans                       列出无人观察的 Terminal

:save-layout <name>                 保存当前布局为模板
:load-layout <name> [create|prompt|skip]
                                    加载布局到新 Workspace
:list-layouts                       列出所有可用布局
:edit-layout <name>                 用 $EDITOR 打开布局文件
:delete-layout <name>               删除布局文件
```

## 核心交互流程

### 流程 1：日常开发

```
启动 termx
  │
  ▼
自动创建 Workspace "default" + Tab "1" + Viewport (新 Terminal, bash)
  │
  ├─ C-a %  → 垂直分割，右侧新 Terminal
  ├─ C-a "  → 右侧再水平分割，下方新 Terminal
  │
  │  ┌──────────┬──────────┐
  │  │ bash     │ vim .    │
  │  │          ├──────────┤
  │  │          │ make     │
  │  └──────────┴──────────┘
  │
  ├─ C-a c  → 新建 Tab "2"
  ├─ C-a d  → Detach，所有 Terminal 继续运行
  │
  └─ 重新启动 termx → 自动恢复布局和 Terminal
```

### 流程 2：Terminal 复用

```
Workspace "dev" / Tab "coding"
  ┌──────────┬──────────┐
  │ vim      │ build    │
  │          ├──────────┤
  │          │ log (T3) │  ← T3 是 tail -f app.log
  └──────────┴──────────┘

想在另一个 Tab 里也看 T3：
  │
  ├─ C-a c          → 新建 Tab "monitor"
  ├─ C-a f          → 打开 Picker
  ├─ 搜索 "log"     → 找到 T3
  ├─ Enter          → attach T3 到当前 Viewport
  │
  │  Tab "monitor"
  │  ┌────────────────────┐
  │  │ log (T3)           │  ← 同一个 T3，实时同步
  │  │                    │
  │  └────────────────────┘
  │
  └─ 两个 Tab 里的 T3 显示完全同步的输出
```

### 流程 3：AI Agent 协作

```
# 先创建 AI agent 的 Terminal
termx new --tag role=ai-agent -- claude-code

# 启动 TUI
termx

  ┌──────────────────┬──────────────────┐
  │ 你的 shell       │ AI agent (T5)    │
  │ (fit)            │ (fit)            │
  │                  │                  │
  │ 你在这里工作      │ 看 agent 在干什么  │
  │                  │                  │
  └──────────────────┴──────────────────┘

  C-a l  → 焦点切到 agent 的 Viewport（右边）
  输入 Ctrl-C → 中断 agent
  C-a h  → 焦点切回你的 shell（左边）

  C-a w  → 弹出浮动 Viewport，创建新 Terminal
           用来跑临时命令，不打断当前布局
```

### 流程 4：程序退出后恢复

```
  ┌──────────┬──────────┐
  │ vim      │ build    │
  │          ├──────────┤
  │          │ server   │  ← server 进程 crash 了
  └──────────┴──────────┘

  server 的 Terminal 进入 exited 状态：

  ┌──────────┬──────────┐
  │ vim      │ build    │
  │          ├──────────┤
  │          │ [exited] │  ← 显示退出标记
  │          │          │
  │          │ [r]estart│  ← 当前 viewport 可直接重启
  │          │ [c]lose  │
  └──────────┴──────────┘

  按 r → 用相同的 command 创建新 Terminal
       → 只重绑当前触发 restart 的 Viewport，其他观察旧 Terminal 的 viewport 保持 exited
       → Viewport 自动绑定，位置 / fixed offset / pin / readonly 保持不变
       → "模板"复活
```

## 鼠标支持

默认启用，可通过 `:set mouse off` 关闭。

| 操作 | 效果 |
|------|------|
| 点击 Viewport | 切换焦点 |
| 拖动 Viewport 边界 | 调整分割比例 |
| 点击 Tab 栏 | 切换 Tab |
| 拖动浮动 Viewport 标题栏 | 移动位置 |
| 拖动浮动 Viewport 边缘 | 调整大小 |
| 滚轮 | 进入 Copy/Scroll 模式并滚动 |

## 状态栏

```
┌─ Tab bar ──────────────────────────────────────────────┐
│  [1:coding] [2:monitor*] [3:scratch]    ws:dev         │
├────────────────────────────────────────────────────────┤
│                                                        │
│                    (Viewport 区域)                      │
│                                                        │
├────────────────────────────────────────────────────────┤
│  T3 tail -f app.log │ fit │ 2h13m │ role=log │  C-a ? │
└────────────────────────────────────────────────────────┘
  │                      │      │        │          │
  Terminal 命令          模式   运行时间   tags     帮助提示
```

状态栏显示的是 Terminal 信息（命令、运行时间、tag），不只是 Viewport 编号。这强化了"Terminal 是一等公民"的心智模型。
