# 交互设计 v2

本文档是 interaction.md 的重写版本，核心变更是**分层 prefix 系统**。

## 与 v1 的变更记录

```
变更 1：快捷键系统从"单一 C-a prefix"改为"C-a + 子 prefix 分层"
  原因：v1 所有操作挤在 C-a 下，30+ 个键无语义分组，
        为避免冲突出现 C-a Ctrl-hjkl、C-a Alt-hjkl、C-a _ 等反直觉键，
        review-v3 指出组合键兼容性存疑（V11）

变更 2：完全消除 Ctrl-组合和 Alt-组合键
  原因：Ctrl-h=Backspace、Alt 的 ESC 前缀差异、Ctrl-Arrow 不统一
        分层 prefix 后这些键全部变成子模式下的普通字母键，V11 问题消失

变更 3：浮动操作改为 sticky 子模式（C-a o）
  原因：浮窗操作通常是连续的（移动→再移动→调大小），
        v1 每次操作都要按 C-a 前缀，太繁琐

变更 4：Viewport 设置操作归入 C-a v 子模式
  原因：fit/fixed、readonly、pin、offset pan 都是 viewport 级设置，
        v1 散落在 C-a M / C-a R / C-a P / C-a Ctrl-hjkl，无语义关联

变更 5：Workspace 操作从 C-a 直接键移到 C-a w 子模式
  原因：workspace 操作低频，不值得占直接键位

变更 6：Tab 的 new/rename/close 移到 C-a t 子模式，但切换（1-9/n/p）和新建（c）保留直接键
  原因：切换和新建太高频不能多一次按键，rename/close 低频可以

变更 7：模式图更新，新增 Floating 子模式和 Offset Pan 子模式
  原因：这两个是 sticky 模式，需要在模式图中体现

变更 8：C-a s（workspace picker）移到 C-a w s
  原因：归入 workspace 子模式

变更 9：C-a $（rename workspace）移到 C-a w r
  原因：同上

变更 10：C-a ,（rename tab）移到 C-a t ,
  原因：归入 tab 子模式

变更 11：C-a &（close tab）移到 C-a t x
  原因：同上

未变更：
  - chooser 机制（分屏/新 tab/浮窗时弹出 terminal 选择器）
  - Terminal Picker（C-a f）的行为和 UI
  - Copy/Scroll 模式的内部键位
  - 命令模式的命令列表
  - 鼠标支持
  - 状态栏设计
  - 核心交互流程（日常开发、Terminal 复用、AI 协作、程序退出恢复）
```

---

## 设计原则

1. 交互层尽量与 tmux 一致（降低迁移成本），差异化体现在模型层
2. 高频操作两次按键（C-a + key），低频操作三次按键（C-a + 子prefix + key）
3. 只占用 C-a 一个 Ctrl 键，不侵占 Terminal 内部程序的快捷键
4. 不使用任何 Ctrl-组合或 Alt-组合作为第二键，保证跨终端兼容性

## 模式

```
┌──────────┐   C-a [    ┌──────────┐
│          │ ─────────→ │          │
│  Normal  │            │   Copy   │
│          │ ←───────── │  Scroll  │
└────┬─────┘   q/Esc    └──────────┘
     │
     ├─ C-a :           C-a f
     ▼                     │
┌──────────┐          ┌───▼──────┐
│ Command  │          │  Picker  │
└──────────┘          └──────────┘
   Esc/Enter             Esc/Enter

     │ C-a o                │ C-a v o
     ▼                      ▼
┌──────────┐          ┌──────────┐
│ Floating │ sticky   │ Offset   │ sticky
│ 子模式    │ Esc 退出 │ Pan 模式  │ Esc 退出
└──────────┘          └──────────┘
```

| 模式 | 说明 | 进入 | 退出 |
|------|------|------|------|
| Normal | 键盘输入发送给当前 Viewport 的 Terminal | 默认 | — |
| Copy/Scroll | 浏览 scrollback、选择文本、搜索 | `C-a [` | `q` / `Esc` |
| Command | 类 vim 命令行 | `C-a :` | `Esc` / `Enter` |
| Picker | 交互式列表（terminal 选择） | `C-a f` | `Esc` / `Enter` |
| Floating | 浮窗操作（移动/resize/z-order） | `C-a o` | `Esc` |
| Offset Pan | fixed 模式下手动平移视角 | `C-a v o` | `Esc` |

## Prefix Key

**默认：`Ctrl-a`**

```
按下 Ctrl-a → 进入 prefix 等待状态（800ms 超时）
  │
  ├─ 直接键（h/j/k/l/" /% /x /X /z /f /[ /: /? /d /Space /1-9 /n /p）
  │   → 执行对应命令，回到 Normal
  │
  ├─ 子 prefix 键（t / w / o / v）
  │   → 进入对应子模式，等待下一个键
  │   → one-shot 子模式：执行一个命令后回到 Normal
  │   → sticky 子模式（o）：持续接受命令，Esc 退出
  │
  ├─ 再按一次 Ctrl-a → 发送原始 Ctrl-a 给 Terminal
  └─ 超时 → 取消 prefix，回到 Normal
```

## 快捷键总览

```
C-a + key           Viewport 操作（高频，两次按键）
C-a t + key         Tab 管理（三次按键）
C-a w + key         Workspace 管理（三次按键）
C-a o → key...Esc   Floating 子模式（sticky，连续操作）
C-a v + key         Viewport 设置（三次按键）
```

## C-a 直接键（Viewport 操作，高频）

```
分屏与关闭
  "           水平分割（上下）— chooser 选择新建或 attach
  %           垂直分割（左右）— chooser 选择新建或 attach
  x           关闭当前 Viewport（detach，Terminal 继续运行）
  X           Kill Terminal（销毁 Terminal 和 Viewport）
  z           Zoom 切换（当前 Viewport 全屏 / 恢复）

导航
  h/j/k/l     在 Viewport 间导航（vim 方向）
  ←/↓/↑/→     在 Viewport 间导航（方向键）

调整边界
  H/J/K/L     调整 Viewport 边界（每次 2 行/列）
  { / }       与前/后一个 Viewport 交换位置
  Space       循环切换预定义布局

Tab 切换与新建（高频，保留在直接键）
  1-9         跳转到第 N 个 Tab
  n           下一个 Tab
  p           上一个 Tab
  c           新建 Tab（chooser）— 同 C-a t c 的快捷方式

全局
  f           Terminal Picker
  [           Copy/Scroll 模式
  :           命令模式
  ?           帮助
  d           Detach TUI
  C-a         发送原始 Ctrl-a 给 Terminal
```

## C-a t（Tab 管理，one-shot）

```
C-a t → 等待一个键 → 执行 → 回到 Normal

  c           新建 Tab（chooser 选择新建或 attach）
  ,           重命名当前 Tab
  x           关闭当前 Tab（关闭所有 Viewport，Terminal 继续运行）
```

## C-a w（Workspace 管理，one-shot）

```
C-a w → 等待一个键 → 执行 → 回到 Normal

  s           切换 Workspace（Picker）
  c           新建 Workspace
  r           重命名当前 Workspace
  x           删除当前 Workspace
```

## C-a o（Floating 子模式，sticky）

```
C-a o → 进入 Floating 子模式 → 连续操作 → Esc 退出

  进入时：
    如果当前 Tab 有浮动 Viewport → 焦点切到最顶层浮动 Viewport
    如果没有 → 提示 "no floating viewports"

  创建与关闭
    n           新建浮动 Viewport（chooser 选择新建或 attach，默认 fixed）
    x           关闭当前浮动 Viewport（detach）

  焦点
    Tab         在浮动 Viewport 之间循环切换焦点（按 z-order）

  z-order
    ]           将当前浮动 Viewport 提升到最顶层
    [           将当前浮动 Viewport 降到最底层

  移动与调整大小
    h/j/k/l     移动浮动 Viewport 位置（每次 4 列 / 2 行）
    H/J/K/L     调整浮动 Viewport 大小（每次 4 列 / 2 行）

  显示控制
    v           切换所有浮动 Viewport 的显示/隐藏

  退出
    Esc         退出 Floating 子模式，焦点回到平铺层
```

**为什么 sticky**：浮窗操作通常是连续的——移一下、再移一下、调个大小、换个焦点。如果每次都要按 `C-a o`，太繁琐。进入子模式后连续操作，完了按 Esc 退出。

## C-a v（Viewport 设置，one-shot）

```
C-a v → 等待一个键 → 执行 → 回到 Normal

  m           切换 fit ↔ fixed 模式
  r           切换 readonly 模式
  p           切换 pin 锚定（仅 fixed 模式下有效）

  单次平移（one-shot，按一次回到 Normal）：
  h / ←       向左平移 offset 4 列
  l / →       向右平移 offset 4 列
  k / ↑       向上平移 offset 2 行
  j / ↓       向下平移 offset 2 行

  o           进入 Offset Pan 模式（sticky，用于连续平移）
```

单次平移只需要 `C-a v h`（3 次按键），适合"看一眼旁边的内容"。
连续平移用 `C-a v o → hjkl... → Esc`，适合"大范围浏览"。

### Offset Pan 模式（C-a v o，sticky）

```
C-a v o → 进入 Offset Pan 模式 → 连续平移 → Esc 退出

仅在 fixed + pinned 状态下有效。
如果当前 Viewport 不是 fixed 或未 pin，提示 "pin viewport first (C-a v p)"。

  h / ←       向左平移 offset（每次 4 列）
  l / →       向右平移 offset（每次 4 列）
  k / ↑       向上平移 offset（每次 2 行）
  j / ↓       向下平移 offset（每次 2 行）

  0           重置 offset 到 (0, 0)（左上角）
  $           跳到最右边
  g           跳到最上面
  G           跳到最下面

  Esc         退出 Offset Pan 模式，回到 Normal
```

## 新建 Viewport（chooser）

分屏、开新 Tab、开浮窗时，termx 弹出 chooser：

```
C-a "       新分屏（水平）→ chooser
C-a %       新分屏（垂直）→ chooser
C-a t c     新 Tab → chooser
C-a o n     新浮动 Viewport → chooser
```

chooser 的顶部固定是 `+ new terminal`：

```
Enter           创建新 Terminal
搜索 + Enter    attach 已有 Terminal
Esc             取消
```

## Terminal Picker（C-a f，核心交互）

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

chooser 也复用这份 Terminal 列表，只是顶部额外增加 `+ new terminal` 入口。

## Copy/Scroll 模式（C-a [）

进入后不需要 prefix：

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

## 命令模式（C-a :）

类似 vim 命令行，支持 Tab 补全。

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

## 快捷键速查表

```
┌─ C-a 直接键（两次按键）──────────────────────────────────────┐
│                                                              │
│  " % x X z         分屏 / 关闭 / zoom                        │
│  h j k l ← ↓ ↑ →   导航                                     │
│  H J K L           调整边界                                   │
│  { } Space         交换 / 循环布局                             │
│  1-9 n p c         Tab 切换 / 新建                               │
│  f                 Terminal Picker                            │
│  [ : ? d           Copy / Command / Help / Detach            │
│  C-a               发送原始 Ctrl-a                             │
│                                                              │
├─ C-a t（Tab，one-shot）──────────────────────────────────────┤
│  c                 新建 Tab (chooser)                         │
│  ,                 重命名                                     │
│  x                 关闭 Tab                                   │
│                                                              │
├─ C-a w（Workspace，one-shot）────────────────────────────────┤
│  s                 切换 (picker)                              │
│  c                 新建                                       │
│  r                 重命名                                     │
│  x                 删除                                       │
│                                                              │
├─ C-a o（Floating，sticky → Esc 退出）────────────────────────┤
│  n                 新建浮窗 (chooser)                          │
│  x                 关闭浮窗                                    │
│  Tab               循环焦点                                    │
│  ] [               raise / lower z-order                      │
│  h j k l           移动位置                                    │
│  H J K L           调整大小                                    │
│  v                 toggle 显示/隐藏                             │
│  Esc               退出 floating 模式                          │
│                                                              │
├─ C-a v（Viewport 设置，one-shot）────────────────────────────┤
│  m                 toggle fit/fixed                           │
│  r                 toggle readonly                            │
│  p                 toggle pin                                 │
│  h j k l           单次平移 offset                              │
│  o                 进入 offset pan 模式 (sticky → Esc)        │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

## 与 v1 的按键对照表

```
v1 (旧)                    v2 (新)                    理由
──────────────────────────────────────────────────────────────
C-a w   new floating       C-a o n                    归入 floating 子模式
C-a W   toggle floating    C-a o v                    同上
C-a Tab cycle float focus  C-a o Tab                  同上
C-a ]   raise z-order      C-a o ]                    同上
C-a _   lower z-order      C-a o [                    同上，且 [ 比 _ 更直觉
C-a Alt-hjkl move float    C-a o hjkl                 消除 Alt 组合键
C-a Alt-HJKL resize float  C-a o HJKL                 消除 Alt 组合键
C-a M   toggle fit/fixed   C-a v m                    归入 viewport 设置
C-a R   toggle readonly    C-a v r                    同上
C-a P   toggle pin         C-a v p                    同上
C-a Ctrl-hjkl pan offset   C-a v o → hjkl → Esc      消除 Ctrl 组合键
C-a s   workspace picker   C-a w s                    归入 workspace 子模式
C-a $   rename workspace   C-a w r                    同上
C-a c   new tab            C-a t c                    归入 tab 子模式
C-a c   new tab            C-a t c（同时保留 C-a c 作为快捷方式）
C-a ,   rename tab         C-a t ,                    归入 tab 子模式
C-a &   close tab          C-a t x                    同上
C-a w d delete workspace   C-a w x                    避免与 C-a d (detach) 混淆

未变更：
C-a " % x X z h j k l H J K L { } Space 1-9 n p c f [ : ? d C-a
```

## 核心交互流程

### 流程 1：日常开发

```
启动 termx
  │
  ▼
自动创建 Workspace "default" + Tab "1" + Viewport (新 Terminal, bash)
  │
  ├─ C-a %  → chooser → Enter → 垂直分割，右侧新 Terminal
  ├─ C-a "  → chooser → Enter → 右侧再水平分割，下方新 Terminal
  │
  │  ┌──────────┬──────────┐
  │  │ bash     │ vim .    │
  │  │          ├──────────┤
  │  │          │ make     │
  │  └──────────┴──────────┘
  │
  ├─ C-a t c  → chooser → Enter → 新建 Tab "2"
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
  ├─ C-a t c        → chooser → 搜索 "log" → 选 T3 → Enter
  │
  │  Tab "2"
  │  ┌────────────────────┐
  │  │ log (T3)           │  ← 同一个 T3，实时同步
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

  C-a o n  → chooser → Enter → 弹出浮动 Viewport，新 Terminal
             用来跑临时命令，不打断当前布局
  C-a o Esc → 退出 floating 模式
```

### 流程 4：浮窗操作

```
  C-a o     → 进入 Floating 子模式
  n         → chooser → Enter → 新建浮窗
  h h h     → 向左移动 3 次
  K K       → 高度增大 2 次
  Tab       → 切到下一个浮窗
  [         → 降到底层
  Esc       → 退出 Floating 子模式，回到平铺层
```

### 流程 5：程序退出后恢复

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
  │          │ [r]estart│  ← 提示可重启
  │          │ [c]lose  │
  └──────────┴──────────┘

  按 r → 用相同的 command 创建新 Terminal
       → Viewport 自动绑定，位置不变
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

Floating 子模式下状态栏变为：
┌────────────────────────────────────────────────────────┐
│  [floating] hjkl:move HJKL:resize Tab:focus ]:raise Esc:exit │
└────────────────────────────────────────────────────────┘

Offset Pan 模式下状态栏变为：
┌────────────────────────────────────────────────────────┐
│  [offset pan] hjkl:pan 0:home $:end g:top G:bottom Esc:exit  │
└────────────────────────────────────────────────────────┘
```

状态栏在子模式下显示当前可用操作，降低记忆负担。
