# termx 实施计划

## Current Status (2026-03-18)

- Phase 0-4 已完成：核心 PTY/VTerm/Server/Protocol/CLI 均已落地并通过测试
- Phase 5 正在进行：已有基础 TUI、tab/pane 布局、attach/create、快捷键、自动启动 daemon
- TUI 渲染架构已从“字符串拼接”升级为“cell-based compositor”，用于提升全屏程序在 resize 时的正确性，并为后续 dirty-line 增量渲染做准备
- 近期性能评审结论：协议层不是当前主要瓶颈；终端数据面已经是二进制帧，当前瓶颈主要在 TUI 的本地 VT 解析、pane 合成、全量 View 重建和缺少背压/批量刷新

## Context

termx 是一个 Go 编写的 PTY 托管服务。项目最初状态是**仅有完整设计文档（10 份 spec），零代码实现**；当前已经完成 MVP 服务端与基础 TUI，并继续按阶段推进。

核心价值：服务端只管理扁平的 Terminal 池，所有组织结构（workspace/tab/pane）由客户端自行实现。

---

## Phase 0: 项目脚手架 + 技术验证 Spike

**目标**：初始化 Go 模块，验证三个关键第三方库的可行性。

### 0.1 Go module 和目录骨架
- `go mod init` (Go 1.24+)
- 创建空包目录：`pty/`, `vterm/`, `fanout/`, `protocol/`, `transport/`, `transport/unix/`, `transport/memory/`, `cmd/termx/`
- 添加依赖：`creack/pty`, `charmbracelet/x/vt`

### 0.2 Spike: creack/pty
- 文件：`pty/spike_test.go`
- 验证点：
  - `pty.StartWithSize()` 启动 bash
  - 写入 `echo hello\n`，读取输出含 "hello"
  - `pty.Setsize()` resize
  - 进程组 kill（`SIGHUP -> -pid`）
  - `cmd.Wait()` 在 kill 后返回

### 0.3 Spike: charmbracelet/x/vt
- 文件：`vterm/spike_test.go`
- 验证点：
  - `vt.NewSafeEmulator(80, 24)` 创建
  - 写入 ANSI 序列后 `CellAt()` 读取正确内容
  - Scrollback 溢出后最老行被丢弃
  - `IsAltScreen()` 在写入切换序列后为 true
  - 并发 `Write()` + `CellAt()` 无 race

### 0.4 Spike: bubbletea + PTY 联动
- 最小 bubbletea 程序：spawn PTY，双向传递 I/O
- 验证 raw mode 与 PTY 输出共存

**交付物**：go.mod + 目录结构 + 3 个 spike 测试通过（或记录 API 差异）

---

## Phase 1: 核心包实现（pty / vterm / fanout / event / terminal）

**目标**：实现所有底层组件，纯 Go API，无网络。

### 1.1 `pty/pty.go` — PTY 管理
- `Spawn(SpawnOptions) (*PTY, error)` — creack/pty.StartWithSize, Setpgid, 环境变量合并
- `Read/Write/Resize/Kill/Wait/ExitCode/Close`
- Kill 三阶段：SIGHUP(500ms) → SIGTERM(2s) → SIGKILL（进程组）
- 环境变量：inherit + `TERM=xterm-256color` + `TERMX=1` + `TERMX_TERMINAL_ID=<id>` + 用户自定义

### 1.2 `vterm/vterm.go` — VTerm 封装
- 封装 `charmbracelet/x/vt SafeEmulator`，不暴露第三方类型
- `New(cols, rows, scrollbackSize)`, `Write`, `Resize`
- `CellAt`, `ScreenContent`, `ScrollbackContent`, `CursorState`, `IsAltScreen`
- 定义 termx 原生类型：`Cell`, `CellStyle`, `CursorState`, `TerminalModes`, `ScreenData`

### 1.3 `fanout/fanout.go` — 输出多播
- `Subscribe(ctx) <-chan StreamMessage` — buffer 256
- `Broadcast(data)` — 满则 drop + 累计 droppedBytes，下次成功发送时插入 SyncLost
- `Close()` — 发送 StreamClosed 给所有订阅者
- `StreamMessage` 类型：Output / SyncLost / Closed

### 1.4 `event.go` — EventBus
- `Subscribe(ctx, ...EventsOption) <-chan Event` — buffer 64
- `Publish(Event)` — best-effort（满则丢弃）
- 过滤：按 terminal ID 和/或 event type
- 事件类型：Created, StateChanged, Resized, Removed, CollaboratorsRevoked

### 1.5 `terminal.go` — Terminal 类型和状态机
- 状态机：starting → running → exited
- ID 生成：nanoid 8 字符 (0-9a-z)
- 内部 `readLoop`：PTY read → fanout.Broadcast → vterm.Write
- 内部 `waitLoop`：等待进程退出 → 状态转换 → 定时移除

### 测试
- 每个包独立单元测试
- 验证标准：可以在单个 Go test 中创建 Terminal、写输入、订阅输出、获取快照、kill、观察状态转换

---

## Phase 2: Server 和 Go 公开 API

**目标**：实现 `termx.go` Server 类型，暴露完整 Go API。termx 可作为嵌入式 Go 库使用。

### 2.1 `termx.go` — Server 类型
- `NewServer(opts ...ServerOption) *Server`
- ServerOption：WithSocketPath, WithDefaultSize, WithDefaultScrollback, WithDefaultKeepAfterExit, WithLogger
- 内部：terminal pool (`sync.RWMutex` + `map[string]*Terminal`), EventBus

### 2.2 CRUD 方法
- `Create(ctx, CreateOptions) (*TerminalInfo, error)`
- `Get(ctx, id)`, `List(ctx, ...ListOptions)`, `Kill(ctx, id)`, `SetTags(ctx, id, tags)`

### 2.3 I/O 方法
- `WriteInput`, `SendKeys`（键名→字节映射）, `Resize`, `Subscribe`, `Snapshot`

### 2.4 生命周期
- `ListenAndServe(ctx)`, `Shutdown(ctx)`
- 进程退出后按 KeepAfterExit 定时移除 Terminal

### 2.5 `snapshot.go` — 快照序列化
- JSON 编码，缩写字段名（r/w/s/fg/bg/b/i/u/k/rv/st）
- 行尾空白裁剪，默认值省略
- Scrollback 分页（offset/limit）

### 2.6 错误类型
- ErrNotFound, ErrDuplicateID, ErrInvalidCommand, ErrTerminalExited, ErrSpawnFailed, ErrServerClosed

### 测试
- 集成测试：Create → WriteInput → Subscribe → 验证输出
- Kill → 验证 EventTerminalStateChanged + StreamClosed
- SetTags merge, List tag filter, Snapshot 内容验证, auto-removal

---

## Phase 3: 线协议 + 传输层

**目标**：实现 wire protocol、Transport 接口和 Unix socket / Memory 两种传输实现。

### 3.1 `protocol/` — 帧编解码
- Frame 结构：Channel(uint16) + Type(uint8) + Length(uint32) + Payload
- `Encoder.WriteFrame`, `Decoder.ReadFrame`，大端序，max 4MB

### 3.2 `protocol/messages.go` — 控制消息
- Request/Response/Event/Error JSON 结构体
- Hello 握手，方法定义：create/kill/list/get/resize/attach/detach/snapshot/events/set_tags
- Channel 分配器（free list）

### 3.3 `transport/` — 接口定义
- `Transport`: Send, Recv, Close, Done
- `Listener`: Accept, Close, Addr

### 3.4 `transport/memory/` — 内存传输（测试核心）
- `NewPair()` → (client, server) Transport
- `MemoryListener`

### 3.5 `transport/unix/` — Unix socket 传输
- SOCK_STREAM, 4 字节长度前缀，0600 权限
- 启动时清理 stale socket
- `Dial(path)` 客户端连接

### 3.6 Server session handler
- `handleTransport(t)`: Hello 握手 → 循环读帧 → 路由到 Server 方法
- Attach/Detach：分配 I/O channel，创建订阅，转发输出
- Observer vs Collaborator 权限控制
- 背压：control > snapshot > events > output

### 测试
- Protocol 帧编解码 round-trip
- Memory transport 端到端：Hello → Create → Attach → I/O → Snapshot → Detach → Kill

---

## Phase 4: CLI — **MVP 里程碑**

**目标**：实现 CLI 子命令 daemon/new/ls/kill/attach。此阶段后 termx 是可用工具。

### 4.1 CLI 框架 (`cmd/termx/`)
- 子命令：daemon, new, ls, kill, attach
- 全局 flag：`--socket`

### 4.2 `daemon` — 启动服务
- Unix socket 监听，前台运行
- SIGINT/SIGTERM 优雅关闭

### 4.3 `new` — 创建终端
- `termx new [--name NAME] [--tag K=V]... -- CMD [ARGS...]`
- 无命令时使用 $SHELL 或 /bin/sh

### 4.4 `ls` — 列出终端
- 表格输出：ID, Name, Command, State, Size, Age

### 4.5 `kill` — 终止终端

### 4.6 `attach` — 附加到终端
- raw terminal mode, 双向 I/O 透传
- SIGWINCH 监听 → Resize
- 连接时先 Snapshot 恢复屏幕，再 Subscribe 实时输出
- SyncLost → 重新 Snapshot
- 脱离键：`Ctrl-\ Ctrl-\`

### 4.7 Protocol client 库
- `protocol.Client` 封装 Transport
- 请求/响应 ID 匹配，后台 I/O channel 读取

### MVP 定义
用户可以：
1. `termx daemon` 启动服务
2. `termx new -- bash` 创建终端
3. `termx ls` 列出终端
4. `termx attach <id>` 全 I/O 附加
5. 脱离后重新 attach，屏幕通过 snapshot 恢复
6. `termx kill <id>` 终止终端

---

## Phase 5: TUI 客户端 — 基础版

**目标**：bubbletea TUI，支持 split pane、tab、prefix key。

- LayoutNode 二叉树 + 递归布局计算
- Pane 渲染：每 pane 一个本地 VTerm，subscribe 服务端输出
- Prefix key (Ctrl-a) + 基础快捷键：split(`"/%`), navigate(`hjkl`), close(`x`), zoom(`z`)
- Tab 操作：new(`c`), switch(`1-9/n/p`), rename(`,`), close(`&`)
- `C-a d` detach
- `termx`（无子命令）启动 TUI
- Daemon 自动启动

### Phase 5 当前评审补充

在 Phase 5 进入全屏程序、频繁 resize、分屏数量增加之后，性能问题已经从“功能缺失”转为“渲染路径是否合理”。参考 tmux 和 zellij 的实现模式，后续工作按以下优先级推进：

1. **Pane output batching**
   - 不再让每个 output frame 都立刻驱动一次界面更新
   - 以 16ms/33ms 为窗口批量写入本地 VT，再触发一次 redraw

2. **Dirty pane / dirty line**
   - Pane 作为独立渲染单元维护 dirty 状态
   - 优先实现 dirty line；必要时退化到 pane full redraw
   - 避免把“整 tab 重绘”作为常态

3. **Resize 事务化**
   - resize 期间暂停中间态增量刷新
   - 批量调整 pane 和本地 VT 尺寸
   - 最后统一 full redraw，减少撕裂和重影

4. **Renderer 背压**
   - 当本地 renderer 跟不上输出时，允许跳过中间增量
   - 在必要时退化为“dirty-all + 最新状态重绘”
   - 避免无限追帧

5. **终端原生优化**
   - 后续评估是否引入 scroll/insert/delete line/synchronized updates 等能力
   - 目标是让常见全屏程序不再退化为逐 cell 全量重绘

### Phase 5 架构判断

- **保留**：Server、Protocol、PTY、Snapshot、本地 VT 的总体方向
- **继续演进**：cell-based compositor，补齐 dirty 信息和批量刷新
- **重点审视**：Bubble Tea 的整屏 `View()` 热路径是否还能满足目标性能
- **中期备选**：保留 Bubble Tea 做状态管理，pane 区域改为更底层的增量 renderer（例如自写 terminal writer 或 `tcell`）

---

## Phase 6: TUI 客户端 — 高级功能

- 浮动窗格（`C-a w/W`）
- Copy/Scroll 模式（`C-a [`）+ 搜索
- Command 模式（`C-a :`）
- Fuzzy finder（`C-a f`）
- 鼠标支持
- Workspace 管理 + 持久化
- YAML layout 文件
- `~/.config/termx/config.yaml` 配置
- WebSocket 传输（远程连接）

---

## 依赖关系

```
Phase 0 (脚手架 + Spike)
   ↓
Phase 1 (pty + vterm + fanout + event + terminal)
   ↓
Phase 2 (Server + Go API)
   ↓
Phase 3 (Protocol + Transport)
   ↓
Phase 4 (CLI) ← MVP
   ↓
Phase 5 (TUI 基础)
   ↓
Phase 6 (TUI 高级)
```

## 验证策略

| Phase | 测试方式 | 重点 |
|-------|---------|------|
| 0 | Spike 测试 | 第三方库 API 验证 |
| 1 | 单元测试 | 每个包独立测试 |
| 2 | 集成测试 | Server Go API 端到端（进程内） |
| 3 | 集成测试 | Protocol round-trip, memory transport 全流程 |
| 4 | 集成 + 手动 | CLI 全工作流 |
| 5-6 | 手动 + teatest | TUI 渲染和交互 |

## 风险

| 风险 | 缓解 |
|------|------|
| `charmbracelet/x/vt` API 不稳定（experimental） | Phase 0 spike 验证；wrapper 层隔离；pin 版本 |
| vt 缺少 scrollback API | Phase 0 验证；fallback: 自实现 scrollback ring buffer |
| bubbletea raw mode 与 PTY 冲突 | Phase 0 spike 验证 |

## 关键文件参考

- `docs/spec-terminal.md` — Terminal 状态机、ID、Tags
- `docs/spec-go-api.md` — Server 公开 API 契约
- `docs/spec-protocol.md` — 线协议帧格式和方法定义
- `docs/spec-vterm.md` — VTerm 封装设计
- `docs/spec-tui-client.md` — TUI 架构（LayoutNode、快捷键）
