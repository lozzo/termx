# 核心模型

本文档定义 termx TUI 的核心概念模型。所有交互设计、布局系统、渲染架构都建立在这些概念之上。

## 概念总览

```
termx Server (daemon)
┌─────────────────────────────────────────────────┐
│  Terminal Pool                                  │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ │
│  │ T1   │ │ T2   │ │ T3   │ │ T4   │ │ T5   │ │
│  │ bash │ │ vim  │ │ htop │ │ tail │ │ agent│ │
│  └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘ │
└─────┼────────┼────────┼────────┼────────┼──────┘
      │        │        │        │        │
      │  ┌─────┘   ┌────┘        │   ┌────┘
      │  │         │             │   │
┌─────┼──┼─────────┼─────────────┼───┼───────────┐
│ TUI │  │         │             │   │            │
│     ▼  ▼         ▼             │   │            │
│  Workspace "dev"               │   │            │
│  ┌─ Tab "coding" ──────────┐   │   │            │
│  │ ┌─────────┬────────────┐│   │   │            │
│  │ │ VP-A    │ VP-B       ││   │   │            │
│  │ │ →T1     │ →T2        ││   │   │            │
│  │ │ fit     │ fit        ││   │   │            │
│  │ └─────────┴────────────┘│   │   │            │
│  └─────────────────────────┘   │   │            │
│                                ▼   ▼            │
│  ┌─ Tab "monitor" ─────────────────────────┐    │
│  │ ┌──────────┬──────────┐                 │    │
│  │ │ VP-C     │ VP-D     │  ┌───────────┐  │    │
│  │ │ →T3      │ →T4      │  │ VP-E      │  │    │
│  │ │ fit      │ fit      │  │ →T5       │  │    │
│  │ └──────────┴──────────┘  │ fixed     │  │    │
│  │                          │ 浮动层     │  │    │
│  │                          └───────────┘  │    │
│  └─────────────────────────────────────────┘    │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│ App Client (外部)                                │
│  VP-X → T1 (fixed 120x40)                       │
│  VP-Y → T5 (fit)                                │
└─────────────────────────────────────────────────┘

注意：
- T1 同时被 TUI 的 VP-A 和 App Client 的 VP-X 观察
- T5 同时被 TUI 的 VP-E 和 App Client 的 VP-Y 观察
- T3 只在 TUI 的 VP-C 里，但关闭 VP-C 后 T3 仍然运行
```

## Terminal

Terminal 是 termx 的唯一实体。它由 server 管理，包含：

- 一个 PTY 进程（bash、vim、htop...）
- 一个虚拟终端缓冲区（屏幕内容 + scrollback）
- 一组 key-value tags（元数据）
- 一个尺寸（cols × rows）

### 自动 Tag

非声明式模式下（用户手动分屏创建 Terminal），TUI 自动为新 Terminal 打上以下 tag：

```
C-a % 分屏创建新 Terminal
  → 自动 tag：
      ws=default          ← 当前 workspace 名
      tab=coding          ← 当前 tab 名
      cmd=zsh             ← 命令名（取 command[0] 的 basename）
      created=manual      ← 标记创建方式（manual / layout / api）

声明式模式下（从 layout 文件创建）：
  → 自动 tag：
      created=layout
  → 加上 layout 文件中声明的 tag

API 创建：
  → 自动 tag：
      created=api
  → 加上 API 调用时指定的 tag
```

自动 tag 的作用：
- `C-a f` Picker 搜索时可按 workspace/tab/command 过滤
- `save-layout` 时用这些 tag 作为匹配依据
- orphan Terminal 不会成为"幽灵"——至少知道它的来源

用户可以事后手动补充 tag：
```
C-a : tag role=api-server       ← 给当前 Viewport 的 Terminal 加 tag
C-a : untag role                ← 删除 tag
```

**自动 tag 的语义：出生证明，不是当前地址。**

```
自动 tag 在创建时写入，之后不会自动更新：
  - 重命名 workspace/tab → 已有 Terminal 的 ws/tab tag 不变
  - Terminal 被 attach 到另一个 workspace/tab → tag 不变
  - 用户手动 :tag / :untag 可以修改

理由：
  - 静态 tag 保证 save-layout 的匹配结果是确定的
  - Picker 搜索同时搜 tag 和 Viewport 当前位置，不会误导
  - 如果 tag 是动态的，多客户端场景下同步成本很高
```

### Orphan 可见性

```
orphan = running + refcount=0（没有 Viewport 观察的 Terminal）

可见性保障：
  1. C-a f Picker 中 orphan 用 ○ 标记，排在列表末尾
  2. 状态栏右侧显示 orphan 数量（如果 > 0）：
     │ ... │ 3 orphans │ C-a ? │
  3. :list-orphans 命令列出所有 orphan Terminal
```

Terminal 的生命周期完全独立于任何客户端视图。

### 生命周期状态机

```
                    Create()
                       │
                       ▼
               ┌───────────────┐
               │               │
               │   creating    │ ← PTY fork+exec 中
               │               │
               └───────┬───────┘
                       │ PTY 启动成功
                       ▼
               ┌───────────────┐
               │               │◄──────────────────────────────┐
               │   running     │                               │
               │               │  Viewport 操作不影响此状态      │
               │  refcount ≥ 0 │  （attach/detach 只改 refcount）│
               │               │                               │
               └───┬───────┬───┘                               │
                   │       │                                   │
      程序退出      │       │ Kill()                            │
      (exit/crash) │       │                                   │
                   ▼       ▼                                   │
           ┌────────┐  ┌────────┐                              │
           │        │  │        │                              │
           │ exited │  │ killed │                              │
           │        │  │        │                              │
           │ code=N │  │        │                              │
           └───┬────┘  └───┬────┘                              │
               │           │                                   │
               │           │ 立即进入 GC 候选                    │
               │           ▼                                   │
               │      ┌─────────┐                              │
               │      │         │                              │
               │      │  gone   │ ← 从 pool 中移除，不可恢复    │
               │      │         │                              │
               │      └─────────┘                              │
               │                                               │
               ├── Restart()                                   │
               │   → 用相同 command+tags 创建新 Terminal ───────┘
               │   → 新 Terminal，新 ID
               │   → 只有触发 restart 的 Viewport 绑定到新 Terminal
               │   → 其他观察旧 Terminal 的 Viewport 仍显示 [exited]
               │
               └── GC 规则（见下方）
                   → 满足条件后进入 gone

状态说明：
  creating  PTY 正在 fork+exec，短暂过渡态
  running   PTY 进程存活，可读写
  exited    PTY 进程已退出，屏幕缓冲区仍保留（可翻 scrollback）
  killed    被用户显式 Kill，立即清理
  gone      已从 Terminal Pool 中移除
```

### 引用计数（refcount）

```
refcount = 当前观察该 Terminal 的 Viewport 数量（跨所有客户端）

  Viewport attach T1  → T1.refcount++
  Viewport detach T1  → T1.refcount--
  Client 断开连接     → 该 client 的所有 Viewport 对应的 refcount--

refcount 不影响 Terminal 状态转换：
  refcount = 0 的 running Terminal 仍然运行（orphan）
  refcount > 0 的 exited Terminal 仍然是 exited
```

### GC 规则

```
Terminal 在以下条件下被自动回收（exited → gone）：

  条件 1：exited + refcount = 0 + 超过 GC 宽限期（默认 5 分钟）
    → 没人看的已退出 Terminal，等一段时间后清理
    → 宽限期内用户仍可通过 Picker 找到并 attach

  条件 2：exited + 所有观察它的 Viewport 都已关闭
    → 最后一个 Viewport 关闭时启动 GC 宽限期计时器
    → 如果宽限期内有新 Viewport attach，取消计时器

  条件 3：用户显式 Kill
    → 立即 gone，不等宽限期

  不会被 GC 的情况：
    running + refcount = 0（orphan）→ 不回收，继续运行
    exited + refcount > 0 → 不回收，有人还在看

GC 时序示例：

  t=0   T3 程序退出 → exited, refcount=2 (VP-C, VP-D 在看)
  t=1   用户关闭 VP-C → refcount=1
  t=2   用户关闭 VP-D → refcount=0, 启动 5min 计时器
  t=3   (3 分钟后) 用户 C-a f 找到 T3, attach → refcount=1, 取消计时器
  ...或者...
  t=7   (5 分钟到) T3 → gone, 从 pool 移除
```

### Kill 保护

```
Kill 操作（C-a X / Picker C-k / :kill-terminal）的保护规则：

  running Terminal：
    → 弹出确认："Terminal T3 (tail -f app.log) is running. Kill? [y/N]"
    → 如果 refcount > 1："Terminal T3 is observed by 3 viewports. Kill? [y/N]"

  exited Terminal：
    → 不需要确认，直接 kill

  带保护 tag 的 Terminal：
    → tag 包含 protected=true 时，总是要求确认
    → 用于 AI agent 等长期运行的重要 Terminal
```

## Viewport

Viewport 是客户端侧的概念，代表"观察一个 Terminal 的窗口"。

一个 Viewport 绑定到一个 Terminal，但一个 Terminal 可以被多个 Viewport 同时观察。

```
Viewport 的属性：
┌─────────────────────────────────────┐
│ Viewport                            │
│                                     │
│  terminal_id: T1                    │
│  mode: fit | fixed                  │
│  size: (cols, rows)     ← 显示区域  │
│  offset: (x, y)        ← 裁剪偏移  │
│  pin: bool              ← 锚定视角  │
│                                     │
│  位置由布局层决定：                    │
│    平铺层 → layout tree 分配         │
│    浮动层 → 绝对坐标 + z-order       │
└─────────────────────────────────────┘
```

### fit 模式（默认）

Viewport 大小变化时，自动发 resize 给 Terminal，PTY 跟着变。这是 tmux 的传统行为。

```
Viewport 80x24          Viewport 缩小到 40x12
┌──────────────────┐    ┌─────────┐
│ ~/project $ make │    │~/project│
│ building...      │    │building.│
│ [1/5] compiling  │    │[1/5] co │
│ [2/5] compiling  │    │[2/5] co │
│ ...              │    │...      │
│                  │    │         │
│ PTY = 80x24     │    │PTY=40x12│ ← PTY 跟着缩小
└──────────────────┘    └─────────┘

resize viewport → resize PTY → 程序重排内容
```

适用场景：大多数日常使用。你的 shell、vim、htop 都应该用 fit 模式。

### fixed 模式

Viewport 不发 resize 给 Terminal。Terminal 保持自己的 PTY 尺寸，Viewport 只显示其中一部分。

```
Terminal T1 (PTY 80x24)
┌────────────────────────────────────────────────────────────────────────────────┐
│ ~/project $ make build                                                        │
│ building...                                                                   │
│ [1/5] compiling main.go                                                       │
│ [2/5] compiling server.go                                                     │
│ [3/5] compiling handler.go                                                    │
│ [4/5] linking                                                                 │
│ [5/5] done                                                                    │
│ ~/project $ _                                                                 │
│                                                                               │
│         ┌─ Viewport (fixed, 30x6) ─┐                                         │
│         │                          │                                          │
│         │  只显示这个区域的内容       │                                          │
│         │  默认跟随光标位置          │                                          │
│         │                          │                                          │
│         └──────────────────────────┘                                          │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘

Viewport 显示的内容：
┌──────────────────────────────┐
│ [4/5] linking                │
│ [5/5] done                   │
│ ~/project $ _                │  ← 光标在这里，viewport 跟过来
│                              │
│                              │
│                              │
└──────────────────────────────┘
```

**视角控制**：

```
默认：跟随光标
  光标移动 → viewport 自动平移，保持光标在可见区域内

锚定（pin）：快捷键 C-a P
  固定当前视角，不再跟随光标
  再按一次 → 取消锚定，恢复跟随

手动平移：锚定状态下可用方向键移动 viewport 偏移
```

适用场景：
- 多客户端观察同一个 terminal 时，次要客户端用 fixed 避免抢 resize 权
- 在小窗口里观察一个大 terminal 的输出（比如浮动窗口里看全屏程序）
- AI agent 的 terminal 用 fixed 模式，人类观察但不干扰尺寸

## 布局系统：平铺 + 浮动统一模型

平铺和浮动不是两套系统，是同一个 Viewport 机制的两种摆放策略。

```
┌─ Tab ──────────────────────────────────────────────────┐
│                                                        │
│  ┌─ 平铺层 (Tiling Layer) ──────────────────────────┐  │
│  │                                                  │  │
│  │  Viewport 的位置和大小由 layout tree 决定          │  │
│  │  通常用 fit 模式                                  │  │
│  │                                                  │  │
│  │  ┌──────────────────┬───────────────────┐        │  │
│  │  │ VP-A (fit)       │ VP-B (fit)        │        │  │
│  │  │ → T1             │ → T2              │        │  │
│  │  │                  │                   │        │  │
│  │  │                  ├───────────────────┤        │  │
│  │  │                  │ VP-C (fit)        │        │  │
│  │  │                  │ → T3              │        │  │
│  │  └──────────────────┴───────────────────┘        │  │
│  └──────────────────────────────────────────────────┘  │
│                                                        │
│  ┌─ 浮动层 (Floating Layer) ────────────────────────┐  │
│  │                                                  │  │
│  │  Viewport 的位置和大小由用户自由设定               │  │
│  │  可以用 fit 或 fixed 模式                         │  │
│  │  有 z-order，后创建的在上面                        │  │
│  │                                                  │  │
│  │       ┌─────────────────────────┐                │  │
│  │       │ VP-D (fixed 60x20)     │                │  │
│  │       │ → T3  ← 同一个 T3！     │                │  │
│  │       │ z: 1                   │                │  │
│  │       └─────────────────────────┘                │  │
│  └──────────────────────────────────────────────────┘  │
│                                                        │
└────────────────────────────────────────────────────────┘

VP-C 和 VP-D 观察同一个 T3：
  VP-C 在平铺层，fit 模式 → T3 的 PTY 跟随 VP-C 的大小
  VP-D 在浮动层，fixed 模式 → 不发 resize，只裁剪显示
```

### 平铺层

- 由二叉分割树（layout tree）管理
- 每个叶节点是一个 Viewport
- 支持水平/垂直分割，可调比例
- 支持预定义布局（even-horizontal、even-vertical、main-left、tiled）

### 浮动层

- 覆盖在平铺层之上
- 每个浮动 Viewport 有独立的位置 (x, y)、大小 (w, h)、z-order
- 默认居中创建，80% 宽高
- 可拖动移动、拖动边缘调整大小
- `C-a W` 切换所有浮动 Viewport 的显示/隐藏

### 两层的关系

```
渲染顺序（从底到顶）：
  1. 平铺层的所有 Viewport
  2. 浮动层的 Viewport，按 z-order 从低到高

焦点规则：
  浮动层有可见 Viewport 时 → 焦点默认在最顶层浮动 Viewport
  所有浮动 Viewport 隐藏时 → 焦点回到平铺层
  C-a h/j/k/l → 只在当前层内导航
  Esc（焦点在浮动层时）→ 焦点回到平铺层
```

## 多客户端 Resize 仲裁

一个 Terminal 只有一个 PTY 尺寸，但可能被多个 Viewport 同时观察。

**策略：last-writer-wins**

```
Terminal T1 (PTY size = 最后收到的 resize)

  TUI Client A                    App Client B
  ┌──────────────┐               ┌─────────────────────┐
  │ VP (fit)     │               │ VP (fit)             │
  │ 40x20        │               │ 120x40               │
  └──────┬───────┘               └──────────┬──────────┘
         │ resize(40,20)                    │ resize(120,40)
         └──────────┬───────────────────────┘
                    ▼
              termx Server
              T1.PTY = 最后收到的 resize

  时序：
    t=0  A resize → PTY=40x20   → B 的显示可能不匹配
    t=1  B resize → PTY=120x40  → A 的显示可能不匹配
```

**如何避免冲突**：

```
场景 1：一个 fit + 一个 fixed（推荐）
  Client A: fit 模式  → 发 resize，控制 PTY 尺寸
  Client B: fixed 模式 → 不发 resize，只裁剪观察
  → 没有冲突

场景 2：两个 fit（会冲突）
  两个客户端都在发 resize
  → PTY 尺寸在两个值之间跳动
  → 程序（vim 等）会频繁重排
  → 不推荐，但不阻止

场景 3：同一个 TUI 内，同一个 Terminal 在两个 Viewport 里
  VP-A (fit, 平铺层) 和 VP-D (fixed, 浮动层) 都指向 T3
  → 只有 fit 模式的 VP-A 发 resize
  → VP-D 用 fixed 模式裁剪显示
  → 没有冲突
```

## 多写入者仲裁

一个 Terminal 可以被多个客户端（人类 + AI agent）同时写入。

**策略：不仲裁，raw 透传，last-byte-wins**

```
Terminal T1 (PTY stdin)

  人类 (TUI Viewport)              AI Agent (API WriteInput)
       │                                │
       │ 按键 'l' 's' '\n'              │ WriteInput("git add -A\n")
       │                                │
       └────────────┬───────────────────┘
                    │
                    ▼
              PTY stdin（字节流）
              所有输入按到达顺序拼接
              没有锁、没有排队、没有主控方

  结果：如果两边同时输入，PTY 收到的是交错的字节流
        比如：l s git add \n -A\n
        这会导致命令错乱
```

这跟两个人同时 ssh 到同一个 tmux session 打字一样——谁按键谁的字符就进去了。termx 不试图解决这个问题，因为：

1. 实际使用中，人和 AI 很少真正同时输入（通常是轮流的）
2. 加锁或排队会引入延迟和复杂度，得不偿失
3. 用户可以通过 Ctrl-C 随时中断

**但需要提供保护机制**：

### 只读模式（readonly）

```
Viewport 属性：
  readonly: bool（默认 false）

  readonly = true 时：
    → 键盘输入不转发给 Terminal
    → 唯一例外：Ctrl-C 仍然透传（紧急中断）
    → 状态栏显示 [readonly] 标记
    → 仍可使用 C-a 前缀键操作 TUI 本身

  切换：C-a R（toggle readonly）

  用途：
    - 观察 AI agent 工作时，避免误触
    - 多人观察同一个 Terminal 时，非主操作者设为 readonly
    - 演示/教学场景
```

### Ctrl-C 行为

```
Ctrl-C 总是无条件发送给 PTY，不管：
  - 当前 Terminal 是否有 AI agent 在操作
  - 是否有其他客户端在写入
  - Terminal 的 refcount 是多少
  - Viewport 是否处于 readonly 模式

理由：Ctrl-C 是用户的紧急中断手段，不应该被任何逻辑拦截。
这是 readonly 模式下唯一允许透传的控制键。
```

### 输入来源标识（可选，非 MVP）

```
server 可以在 EventBus 中标记输入来源：

  Event{
    Type: InputReceived,
    TerminalID: T1,
    Source: "tui-client-A" | "api-agent-claude" | ...
    Data: []byte("git add -A\n"),
  }

  用途：
    - 审计日志：谁在什么时候输入了什么
    - TUI 可以用不同颜色标记不同来源的输入（可选 UI 增强）
    - 不影响 PTY 行为，纯旁路信息

  MVP 不做，但 API 预留 source 字段。
```

## Workspace 和 Tab

Workspace 和 Tab 是纯客户端的视图组织概念，server 对此一无所知。

```
Workspace "dev"
├─ Tab "coding"
│   ├─ 平铺层: VP-A(T1), VP-B(T2), VP-C(T3)
│   └─ 浮动层: VP-D(T5)
├─ Tab "monitor"
│   ├─ 平铺层: VP-E(T3), VP-F(T4)    ← T3 又出现了
│   └─ 浮动层: (空)
└─ Tab "scratch"
    ├─ 平铺层: VP-G(T6)
    └─ 浮动层: (空)

Workspace "ops"
├─ Tab "logs"
│   ├─ 平铺层: VP-H(T4), VP-I(T7)    ← T4 跨 workspace 复用
│   └─ 浮动层: (空)
└─ ...
```

**关键语义**：

- 切换 Tab → 切换显示的 Viewport 集合，Terminal 不受影响
- 切换 Workspace → 切换整个 Tab 集合
- 关闭 Tab → 关闭其中所有 Viewport，Terminal 不受影响
- 关闭 Workspace → 关闭所有 Tab
- 只有显式 Kill Terminal 才会销毁 Terminal

## 程序退出后的 Viewport 行为

Terminal 内的程序退出（exit、crash、Ctrl-D）后：

```
程序退出
  │
  ▼
Terminal 进入 exited 状态（屏幕缓冲区保留）
  │
  ▼
所有观察该 Terminal 的 Viewport 显示 [exited] 标记
不自动关闭，保留在布局中
  │
  ├── 用户在某个 Viewport 里选择"重启"(r)
  │     → 用相同的 command + tags 创建新 Terminal（新 ID）
  │     → 该 Viewport 自动绑定到新 Terminal
  │     → 其他观察旧 Terminal 的 Viewport 仍显示 [exited]
  │     → 位置、大小、模式都保留（"模板"语义）
  │
  ├── 用户选择"关闭 Viewport"(c)
  │     → Viewport 从布局中移除
  │     → Terminal refcount--
  │     → 如果 refcount 降为 0，启动 GC 宽限期计时器
  │
  └── 用户不操作
        → Viewport 保持 [exited] 显示
        → Terminal 不会被 GC（因为 refcount > 0）
        → 用户随时可以回来操作
```

这就是"模板"概念：Viewport 记住了 command、tag、位置、大小、模式。Terminal 死了，模板还在，可以原地复活。

**Restart 的语义**：Restart 创建的是新 Terminal（新 ID、新 PTY 进程），不是"复活"旧的。旧 Terminal 在所有 Viewport detach 后按 GC 规则回收。

## 外层窗口 Resize 时的行为

整个 TUI 窗口大小变化时（比如用户拖动终端模拟器窗口）：

### 平铺层

按比例缩放，每个 Viewport 的容器按 layout tree 的 ratio 重新计算。

```
外层 200x50 → 100x25

┌──────────────┬──────────────┐      ┌───────┬───────┐
│              │              │      │       │       │
│  VP-A 60%   │  VP-B 40%   │  →   │VP-A   │VP-B   │
│  120x50     │  80x50      │      │60x25  │40x25  │
│              │              │      │       │       │
└──────────────┴──────────────┘      └───────┴───────┘

fit 模式的 VP → 发 resize 给 Terminal → PTY 跟着变
fixed 模式的 VP → 不发 resize → 裁剪区域变化
```

**最小尺寸保护**：

```
如果某个 Viewport 容器小于最小尺寸（默认 10x4）：
  → 隐藏该 Viewport，显示折叠标记 [···]
  → 空间让给相邻 Viewport
  → 外层恢复大小后自动展开

┌───────────────────────┐      ┌───────────────────────┐
│ VP-A      │ VP-B      │      │ VP-A                  │
│ 60x25     │ 40x25     │  →   │ 95x10                 │
│           │           │      │                       │
│           │           │      │              [···] B  │
└───────────┴───────────┘      └───────────────────────┘
  外层 100x25                    外层 100x10
                                 VP-B 太小，折叠
```

### 浮动层

**策略：保持大小，clamp 位置**

```
浮动 Viewport 原来：pos(100,10) size(60x20)
外层从 200x50 → 80x25

  1. 大小不变：60x20（不缩放）
  2. 位置 clamp：pos(100,10) 超出边界 → 推回到 pos(20,5)
  3. 如果大小超出外层：clamp 到外层大小 → size(80x25)

┌─────────────────────────────────────┐
│                                     │
│  ┌───────────────────────────────┐  │
│  │ VP-D (60x20)                 │  │  ← 大小不变
│  │ 位置被推回到可见区域内          │  │
│  │                              │  │
│  └───────────────────────────────┘  │
│                                     │
└─────────────────────────────────────┘
  外层 80x25
```

理由：浮动窗口通常是用户刻意设定的大小（比如 80x24 给 AI agent），缩放会破坏意图。

## 数据模型总结

```go
// 客户端侧的完整数据模型

type Model struct {
    client     Client              // 到 daemon 的连接
    workspaces []*Workspace
    activeWS   int
    width, height int              // TUI 窗口大小
}

type Workspace struct {
    Name      string
    Tabs      []*Tab
    ActiveTab int
}

type Tab struct {
    Name       string
    Tiling     *LayoutNode         // 平铺层：二叉分割树
    Floating   []*FloatingViewport // 浮动层：有序列表（z-order）
    FocusLayer Layer               // 当前焦点在哪层
    FocusID    string              // 当前焦点 Viewport ID
}

type Viewport struct {
    ID         string
    TerminalID string              // 绑定的 Terminal
    Mode       ViewportMode        // fit | fixed
    Offset     Point               // fixed 模式下的裁剪偏移
    Pin        bool                // 是否锚定视角
    Template   *Template           // 程序退出后的重启模板
}

type FloatingViewport struct {
    Viewport
    X, Y    int                    // 绝对位置
    W, H    int                    // 大小
    Z       int                    // z-order
    Visible bool
}

type LayoutNode struct {
    // 叶节点
    ViewportID string

    // 分支节点
    Direction  SplitDirection      // horizontal | vertical
    Ratio      float64             // 0.0 ~ 1.0
    First      *LayoutNode
    Second     *LayoutNode
}

type Template struct {
    Command []string
    Tags    map[string]string
    Mode    ViewportMode
}
```
