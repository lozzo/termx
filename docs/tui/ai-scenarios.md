# AI 场景

termx 的架构天然适合 AI agent。本文档描述具体的 AI 使用场景和 termx 在其中的角色。

核心定位：**termx 是 AI agent 的终端运行时，不是 AI 本身。**

## 为什么 termx 适合 AI

```
传统方式：AI agent 直接操作 shell
  AI → subprocess.Popen("bash") → 读写 stdin/stdout
  问题：
    - 人类看不到 agent 在干什么（除非 agent 主动打日志）
    - 无法中途干预（Ctrl-C、手动输入）
    - 进程和 agent 绑死，agent 崩了进程也没了
    - 多个 agent 无法共享同一个 shell 环境

termx 方式：AI agent 通过 API 操作 Terminal
  AI → termx.Create() → termx.WriteInput() → termx.Subscribe()
  优势：
    - 人类通过 TUI 实时观察 agent 的操作
    - 随时可以往 Terminal 里输入（Ctrl-C 中断、手动修正）
    - Terminal 独立于 agent 存活，agent 崩了 Terminal 还在
    - 多个 agent 或人+agent 可以操作同一个 Terminal
```

## 场景 1：AI Agent 宿主

给每个 AI agent 分配专属 Terminal，人类通过 TUI 观察。

```
termx Server
┌─────────────────────────────────────────────┐
│  T1 (bash)        tags: {role: shell}       │
│  T2 (claude-code) tags: {role: ai-agent}    │
│  T3 (aider)       tags: {role: ai-agent}    │
└─────────────────────────────────────────────┘

TUI 布局：
┌──────────────────────┬──────────────────────┐
│                      │                      │
│  你的 shell (T1)     │  claude-code (T2)    │
│                      │                      │
│  你在这里工作         │  实时看 agent 在干什么 │
│                      │                      │
│                      │  agent 在跑 git diff  │
│                      │  agent 在改文件...     │
│                      │                      │
└──────────────────────┴──────────────────────┘

操作：
  C-a l  → 焦点切到 agent 的 Viewport
  输入 Ctrl-C → 中断 agent 正在执行的命令
  C-a h  → 焦点切回你的 shell
```

agent 的 Terminal 带上 `role=ai-agent` tag，你可以：
- 在任何 workspace 里通过 `C-a f` 搜索 `ai-agent` 找到它
- 在声明式布局里用 `tag: "role=ai-agent"` 自动放置

## 场景 2：人机协作

人和 AI 操作同一个 Terminal。

```
Terminal T1 (bash)
  │
  ├── 人类通过 TUI Viewport 输入
  │     cd /project && make test
  │
  └── AI agent 通过 API 输入
        termx.WriteInput(T1, "git add -A && git commit -m 'fix: ...'")

两者的输入都发送到同一个 PTY
Terminal 的输出同时推送给两者
```

```
TUI 显示：
┌────────────────────────────────────────────┐
│ ~/project $ make test                      │  ← 人类输入
│ PASS: all tests passed                     │
│ ~/project $ git add -A && git commit ...   │  ← AI 输入
│ [main abc1234] fix: resolve race condition │
│ ~/project $ _                              │
└────────────────────────────────────────────┘

人类和 AI 看到完全相同的终端状态
人类随时可以 Ctrl-C 中断 AI 的操作
```

## 场景 3：Agent 编排

起多个 agent 分别执行不同任务，用 TUI 统一监控。

```
termx Server
┌──────────────────────────────────────────────────┐
│  T1 tags: {agent: lint}     → 跑 eslint         │
│  T2 tags: {agent: test}     → 跑 pytest         │
│  T3 tags: {agent: build}    → 跑 docker build   │
│  T4 tags: {agent: deploy}   → 等 T1-T3 完成后部署│
└──────────────────────────────────────────────────┘

声明式布局：
  tabs:
    - name: agents
      tiling:
        arrange: grid
        match:
          tag: "agent"

TUI 自动排列：
┌──────────────────┬──────────────────┐
│ lint (T1)        │ test (T2)        │
│ ✓ 0 errors       │ ● running...     │
│                  │ 42/100 passed    │
├──────────────────┼──────────────────┤
│ build (T3)       │ deploy (T4)      │
│ Step 3/7...      │ waiting...       │
│                  │                  │
└──────────────────┴──────────────────┘

所有 agent 完成后，Terminal 仍在
你可以翻 scrollback 看完整输出
```

## 场景 4：Terminal 作为 AI 的 I/O 通道

不需要给 AI 一个真正的 PTY——通过 termx API 程序化操作。

```go
// AI agent 的代码
func main() {
    client := termx.Connect("/tmp/termx.sock")

    // 创建专属 Terminal
    term, _ := client.Create(ctx, termx.CreateOptions{
        Command: []string{"bash"},
        Tags:    map[string]string{"role": "ai-workspace"},
    })

    // 执行命令
    client.WriteInput(ctx, term.ID, []byte("cd /project && make test\n"))

    // 订阅输出
    sub := client.Subscribe(ctx, term.ID)
    for frame := range sub {
        output := string(frame.Payload)
        if strings.Contains(output, "FAIL") {
            // 测试失败，AI 决定修复
            client.WriteInput(ctx, term.ID, []byte("vim src/bug.go\n"))
        }
    }
}
```

termx 在这里的角色：

```
AI Agent                    termx                     Shell/PTY
  │                          │                          │
  │  Create(bash)            │                          │
  │ ──────────────────────→  │  fork + exec bash        │
  │                          │ ──────────────────────→  │
  │  WriteInput("ls\n")      │                          │
  │ ──────────────────────→  │  write to PTY stdin      │
  │                          │ ──────────────────────→  │
  │                          │                          │
  │                          │  ←── PTY stdout ──────── │
  │  ←── Subscribe output    │                          │
  │                          │                          │
  │                          │                          │
  │  同时，人类通过 TUI       │                          │
  │  也在观察这个 Terminal    │                          │
  │                          │                          │
```

## 场景 5：AI 感知的 Workspace

用声明式布局为 AI 工作流预定义环境。

```yaml
# ~/.config/termx/layouts/ai-coding.yaml
name: ai-coding
tabs:
  - name: work
    tiling:
      split: horizontal
      ratio: 0.5
      children:
        - terminal:
            tag: "role=human-shell"
            command: zsh
        - split: vertical
          ratio: 0.6
          children:
            - terminal:
                tag: "role=ai-agent"
                command: claude-code
            - terminal:
                tag: "role=test-runner"
                command: "npm run test:watch"
    floating:
      - terminal:
            tag: "role=ai-scratch"
            command: bash
          width: 80
          height: 20
          position: center
          mode: fixed
```

```
启动：termx --layout ai-coding

┌──────────────────────┬──────────────────────┐
│                      │                      │
│  你的 shell          │  claude-code         │
│  (human-shell)       │  (ai-agent)          │
│                      │                      │
│                      ├──────────────────────┤
│                      │                      │
│                      │  test:watch          │
│                      │  (test-runner)       │
│                      │                      │
└──────────────────────┴──────────────────────┘
         ┌──────────────────────┐
         │ bash (ai-scratch)    │  ← 浮动，AI 的临时工作区
         │ C-a W 切换显示/隐藏   │
         └──────────────────────┘
```

## termx 不做什么

- **不内置 AI 模型** — termx 是运行时，不是 AI
- **不做 AI 命令建议** — 那是 shell 插件的事（如 github copilot CLI）
- **不做自然语言命令解析** — 那是 AI agent 自己的能力
- **不做 agent 编排框架** — termx 只提供 Terminal 原语，编排逻辑由上层实现

termx 的价值是提供一个干净的、可观测的、多方共享的终端抽象层。AI 工具在这个抽象层上自由发挥。

## API 设计原则

为了让 AI agent 能良好地使用 termx，API 需要满足：

```
1. 可编程    — Create/Kill/WriteInput/Subscribe/Resize 都有 API
2. 可观测    — Subscribe 拿到实时输出，Snapshot 拿到屏幕状态
3. 可标记    — Tags 让 agent 标记自己的 Terminal，方便管理
4. 可共享    — 多个 agent 或人+agent 可以同时操作同一个 Terminal
5. 生命周期独立 — agent 崩了 Terminal 还在，重启 agent 可以重新 attach
```

这些 termx 的 server API 已经具备。AI 场景不需要额外的 "AI 专用 API"，只需要现有 API 足够干净和稳定。
