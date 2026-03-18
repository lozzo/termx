# 声明式布局

本文档定义 termx 的声明式布局系统：如何用 YAML 描述 workspace 结构，如何通过 tag 匹配 Terminal，以及布局的加载和保存机制。

## 核心思想

```
tmux:  布局是运行时状态，手动操作产生，难以复现
termx: 布局是声明式配置，可版本控制、可共享、可一键恢复

类比：
  tmux 的布局 ≈ 手动在画布上摆放元素
  termx 的布局 ≈ CSS/HTML 声明式排版
```

布局文件不写死 Terminal ID，而是用 tag 匹配。Terminal 是活的、持久的，布局只是一个"镜头"。

## 布局文件格式

路径：`~/.config/termx/layouts/<name>.yaml`

### 基本示例

```yaml
# ~/.config/termx/layouts/dev.yaml
name: dev
tabs:
  - name: coding
    tiling:
      split: horizontal          # 左右分割
      ratio: 0.6
      children:
        - terminal:
            tag: "role=editor"
            command: vim .        # 如果匹配不到，用此命令创建
            cwd: ~/project
        - split: vertical        # 右侧上下分割
          ratio: 0.5
          children:
            - terminal:
                tag: "role=build"
                command: make watch
                cwd: ~/project
            - terminal:
                tag: "role=log"
                command: "tail -f logs/app.log"

  - name: git
    tiling:
      terminal:
        command: lazygit

  - name: scratch
    tiling:
      terminal:
        command: zsh
```

对应的布局：

```
Tab "coding":
┌────────────────────────┬──────────────────┐
│                        │                  │
│  vim .                 │  make watch      │
│  tag: role=editor      │  tag: role=build │
│                        │                  │
│         60%            ├──────────────────┤
│                        │                  │
│                        │  tail -f ...     │
│                        │  tag: role=log   │
│                        │                  │
└────────────────────────┴──────────────────┘

Tab "git":                    Tab "scratch":
┌────────────────────┐       ┌────────────────────┐
│                    │       │                    │
│  lazygit           │       │  zsh               │
│                    │       │                    │
└────────────────────┘       └────────────────────┘
```

### 带浮动层的示例

```yaml
name: ai-dev
tabs:
  - name: workspace
    tiling:
      split: horizontal
      ratio: 0.5
      children:
        - terminal:
            tag: "role=shell"
            command: zsh
        - terminal:
            tag: "role=test"
            command: "npm run test:watch"
    floating:
      - terminal:
            tag: "role=ai-agent"
            command: claude-code
          width: 80
          height: 24
          position: center       # center | top-left | top-right | ...
          mode: fixed            # 浮动层默认 fixed
```

对应的布局：

```
Tab "workspace":
┌───────────────────┬───────────────────┐
│                   │                   │
│  zsh              │  npm run test     │
│                   │                   │
│         ┌─────────────────────┐       │
│         │ claude-code         │       │
│         │ (浮动, fixed 80x24) │       │
│         │                     │       │
│         │ tag: role=ai-agent  │       │
│         └─────────────────────┘       │
│                   │                   │
└───────────────────┴───────────────────┘
```

### 自动网格排列

当你有一组同类 Terminal（比如多个 log tail），不需要手动定义分割树：

```yaml
name: monitoring
tabs:
  - name: logs
    tiling:
      arrange: grid              # 自动网格排列
      match:
        tag: "type=log"          # 匹配所有带 type=log 的 Terminal
      min_size: [40, 10]         # 每个 Viewport 最小 40x10
```

```
假设 server 上有 4 个 type=log 的 Terminal：

arrange: grid 自动排列为：
┌──────────────────┬──────────────────┐
│ T3               │ T5               │
│ api.log          │ worker.log       │
│                  │                  │
├──────────────────┼──────────────────┤
│ T7               │ T9               │
│ nginx.log        │ redis.log        │
│                  │                  │
└──────────────────┴──────────────────┘

如果后来又多了一个 type=log 的 Terminal：
→ 自动重排为 3+2 或 2+3 网格
```

## Tag 匹配机制

布局文件通过 tag 匹配 Terminal，不写死 ID。

### 匹配流程

```
加载布局
  │
  ▼
遍历布局中每个 terminal 声明
  │
  ├─ 有 tag 匹配条件？
  │   │
  │   ├─ 在 server 的 Terminal Pool 中搜索匹配的 Terminal
  │   │   │
  │   │   ├─ 找到 → attach 到 Viewport
  │   │   │
  │   │   └─ 没找到 → 三种策略（可配置）：
  │   │       │
  │   │       ├─ create（默认）：用 command 创建新 Terminal，自动打上 tag
  │   │       ├─ prompt：提示用户选择（创建 / 跳过 / 手动指定）
  │   │       └─ skip：留空，Viewport 显示 [waiting for terminal]
  │   │
  │   └─ 匹配到多个？
  │       → 按稳定排序规则选择（见下方）
  │       → 取排序后第一个未被同一布局中其他 Viewport 使用的
  │       → 如果全部已被使用，创建新的
  │
  └─ 没有 tag，只有 command？
      → 直接创建新 Terminal
```

### 多匹配稳定排序规则

tag 匹配是严格 AND 语义：声明的所有 tag 都必须匹配，不满足的不进入候选集。

当候选集中有多个 Terminal 时，按以下优先级排序（从高到低）：

```
第一步：严格 AND 过滤（不是排序，是过滤）
  声明 tag: "role=editor,project=api"
  T1 tags: {role: editor, project: api, type: dev}  → 全部匹配 ✓ 进入候选
  T2 tags: {role: editor}                            → 缺 project=api ✗ 排除
  T3 tags: {role: editor, project: api}              → 全部匹配 ✓ 进入候选

  候选集：[T1, T3]

第二步：在候选集中稳定排序

优先级 1：Terminal 状态（running > exited）
  T1 状态: running
  T3 状态: exited

  → T1 优先（活着的比死了的有用）

优先级 2：创建时间（越早越优先）
  T1 created: 09:00
  T3 created: 10:30

  → T1 优先（更早创建的更稳定）

优先级 3：Terminal ID 字典序（兜底）
  → 保证在所有其他条件相同时，结果仍然确定
```

**为什么这个顺序**：

- 排序规则只使用静态属性（状态、创建时间、ID），不引入动态信号（如最近 attach 时间）
- 这保证同一份 layout 文件在不同时间、不同客户端加载时，匹配结果是确定的
- `save-layout` 生成的 `_hint_id` 已经足够处理"恢复上次绑定"的场景，不需要排序规则来猜

**save-layout 的额外保障**：

`save-layout` 保存时，除了 tag 匹配条件，还会记录当时绑定的 Terminal ID 作为 hint：

```yaml
# save-layout 生成的 YAML
terminal:
  tag: "role=editor"
  _hint_id: "t-abc123"    # 加载时优先尝试匹配此 ID
```

加载时：
1. 先尝试 `_hint_id` 精确匹配（如果 Terminal 还在）
2. 匹配不到再走 tag 匹配 + 排序规则
3. `_hint_id` 是 hint 不是硬绑定，Terminal 被 kill 后自动降级到 tag 匹配

### 匹配示例

```
Server Terminal Pool:
  T1 (bash)       tags: {role: editor}
  T2 (make watch) tags: {role: build, project: api}
  T3 (tail -f)    tags: {role: log, type: log}
  T4 (zsh)        tags: {}
  T5 (tail -f)    tags: {type: log, service: worker}

布局声明:
  - terminal: {tag: "role=editor"}     → 匹配 T1
  - terminal: {tag: "role=build"}      → 匹配 T2
  - terminal: {tag: "type=log"}        → 匹配 T3（第一个）
  - terminal: {tag: "type=log"}        → 匹配 T5（T3 已被使用）
  - terminal: {tag: "role=ci"}         → 没找到 → 创建新 Terminal
```

### 多 tag 匹配

```yaml
# AND 语义：所有 tag 都必须匹配
terminal:
  tag: "type=log,service=api"    # type=log AND service=api

# 只匹配 tag key（不限 value）
terminal:
  tag: "role"                    # 有 role tag 就行，不管值是什么
```

## Workspace 管理

### 多 Workspace

```yaml
# ~/.config/termx/workspaces.yaml（可选的全局配置）
workspaces:
  - layout: dev          # 引用 layouts/dev.yaml
    auto_start: true     # termx 启动时自动加载
  - layout: monitoring
    auto_start: false    # 手动加载
```

### Workspace 切换

```
Workspace "dev"                    Workspace "monitoring"
┌─ coding ─────────────────┐      ┌─ logs ─────────────────────┐
│ ┌────────┬──────────┐    │      │ ┌──────────┬──────────────┐│
│ │ vim    │ build    │    │      │ │ api.log  │ worker.log   ││
│ │        ├──────────┤    │      │ ├──────────┼──────────────┤│
│ │        │ log (T3) │    │      │ │ T3 (同!) │ redis.log    ││
│ └────────┴──────────┘    │      │ └──────────┴──────────────┘│
└──────────────────────────┘      └────────────────────────────┘

C-a s → Picker 列出所有 Workspace → 选择切换

注意：T3 同时出现在两个 Workspace 中
切换 Workspace 不影响任何 Terminal 的运行状态
```

### 布局保存

当前运行时的布局可以保存为 YAML 文件：

```
C-a : save-layout mysetup

→ 遍历当前 Workspace 的所有 Tab
→ 记录每个 Viewport 的：
    - 绑定的 Terminal 的 tags（用于匹配）
    - 绑定的 Terminal 的 command（用于创建）
    - Viewport 模式（fit/fixed）
    - 分割方向和比例
    - 浮动 Viewport 的位置和大小
→ 写入 ~/.config/termx/layouts/mysetup.yaml
```

## 布局文件完整 Schema

```yaml
# 顶层
name: string                     # 布局名称
tabs:                            # Tab 列表
  - name: string                 # Tab 名称
    tiling: LayoutNode           # 平铺层（必须）
    floating:                    # 浮动层（可选）
      - FloatingEntry

# LayoutNode（递归，二叉树）
# 叶节点：
terminal:
  tag: string                    # tag 匹配表达式（可选）
  command: string                # 命令（匹配不到时用于创建）
  cwd: string                    # 工作目录（可选）
  mode: fit | fixed              # Viewport 模式（默认 fit）
  env:                           # 环境变量（可选）
    KEY: value

# 或自动排列叶节点：
arrange: grid | horizontal | vertical
match:
  tag: string                    # 匹配多个 Terminal
min_size: [cols, rows]           # 每个 Viewport 最小尺寸

# 分支节点：
split: horizontal | vertical     # 分割方向
ratio: float                     # 0.0 ~ 1.0，第一个子节点占比
children:                        # 恰好两个子节点
  - LayoutNode
  - LayoutNode

# FloatingEntry
terminal:                        # 同上
width: int                       # 宽度（列数）
height: int                      # 高度（行数）
position: center | top-left | top-right | bottom-left | bottom-right
mode: fit | fixed                # 默认 fixed
```

## 布局文件管理

### 文件来源

布局文件有三个来源，不需要手写 YAML：

```
来源 1：从当前状态保存（最常用）
  你手动分屏、调整布局、attach terminal，搞到满意了
  C-a : save-layout dev
  → 自动生成 YAML，保存到 ~/.config/termx/layouts/dev.yaml
  → YAML 是 save-layout 的输出，不是用户的输入

来源 2：手动编辑（高级用户）
  已经有了 save 出来的文件，想微调
  → 改 tag 匹配规则、加 cwd、调整 ratio
  → C-a : edit-layout dev  （用 $EDITOR 打开）

来源 3：项目级布局（团队共享）
  项目根目录放一个 .termx/layout.yaml
  → 可以 git commit，团队共享
  → 类似 .vscode/、.idea/ 的概念
```

### 查找顺序

```
termx 查找布局文件的优先级（从高到低）：

  1. 命令行指定：termx --layout ./my-layout.yaml
     → 绝对路径或相对路径，直接使用

  2. 命令行指定名称：termx --layout dev
     → 在以下位置按顺序查找 dev.yaml：
        a. .termx/layouts/dev.yaml（项目级）
        b. ~/.config/termx/layouts/dev.yaml（用户级）

  3. 项目级自动发现：
     → 从当前目录向上查找 .termx/layout.yaml
     → 找到则自动加载（可通过 --no-auto-layout 禁用）

  4. 用户级默认：
     → ~/.config/termx/default-layout.yaml（如果存在）
```

### save-layout vs workspace state

```
两种不同的持久化，语义不同：

save-layout（模板）：
  → 用 tag 匹配 Terminal，不绑定具体 ID
  → 可跨 session 复用
  → 适合"我的开发环境长这样"
  → 文件：~/.config/termx/layouts/<name>.yaml

workspace state（快照）：
  → 记录具体的 Terminal ID
  → 用于 TUI 退出后恢复
  → 适合"我刚才的工作现场"
  → 文件：~/.local/state/termx/workspace-state.json
  → TUI 退出时自动保存，启动时自动恢复
```

### 管理命令

TUI 命令模式：
```
:save-layout <name>              保存当前布局为模板
:load-layout <name>              加载布局到新 Workspace
:list-layouts                    列出所有可用布局
:edit-layout <name>              用 $EDITOR 打开布局文件
:delete-layout <name>            删除布局文件
```

CLI：
```bash
termx layout list                列出所有布局
termx layout save <name>         保存当前 daemon 的某个客户端布局
termx layout show <name>         打印 YAML 内容
termx layout edit <name>         用 $EDITOR 打开
termx layout delete <name>       删除
termx layout path <name>         打印文件路径
```

## 使用方式

### CLI

```bash
# 从布局文件启动
termx --layout dev

# 从布局文件启动，指定 workspace 名称
termx --layout dev --workspace myproject

# 启动时加载多个布局为不同 workspace
termx --layout dev --layout monitoring

# 从项目级布局启动
cd ~/project && termx    # 自动发现 .termx/layout.yaml
```

### 启动优先级表

```
termx 启动时的行为，按优先级从高到低：

  优先级 1：--layout 指定
    → 加载指定的布局文件
    → 完全忽略上次 workspace state
    → 按 tag 匹配或创建 Terminal

  优先级 2：workspace state 恢复
    → 没有 --layout 时，检查 workspace-state.json
    → 按 Terminal ID 精确匹配仍在运行的 Terminal
    → 匹配不到的 Viewport 显示 [exited]
    → 这是"恢复上次工作现场"

  优先级 3：项目级自动发现
    → 没有 --layout 也没有 state 时
    → 查找 .termx/layout.yaml
    → 找到则加载

  优先级 4：用户级默认
    → 查找 ~/.config/termx/default-layout.yaml
    → 找到则加载

  优先级 5：空白启动
    → 都没有
    → 创建默认 Workspace "default" + Tab "1" + Viewport（运行 $SHELL）

  冲突处理：
    --layout dev --layout monitoring
    → 创建两个 Workspace，分别加载 dev 和 monitoring 布局
    → 不合并，各自独立
```

## 与 tmuxinator / tmuxp 的对比

```
tmuxinator / tmuxp:
  - 每次启动都创建全新的 session + window + pane
  - 关闭 session 后一切消失
  - 布局文件 = 启动脚本

termx 声明式布局:
  - 首次启动创建 Terminal，之后复用已有的
  - 关闭 TUI 后 Terminal 继续运行
  - 重新加载布局 → 自动匹配到还在运行的 Terminal
  - 布局文件 = 视图配置，不是启动脚本

关键区别：
  tmuxinator 每次 "mux start dev" 都是从零开始
  termx 每次 "termx --layout dev" 都是恢复到上次的状态
```

```
第一次启动：
  termx --layout dev
  → 没有匹配的 Terminal → 全部创建
  → T1(vim), T2(make), T3(tail)

退出 TUI：
  C-a d
  → T1, T2, T3 继续运行

第二次启动：
  termx --layout dev
  → tag 匹配到 T1, T2, T3 → 直接 attach
  → 不创建新 Terminal
  → 布局恢复，Terminal 状态连续
```
