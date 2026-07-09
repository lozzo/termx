# termx

`termx` 是一个以 daemon 为核心的 terminal workspace 系统。

核心理念很简单：**terminal 是长期存在的工作实体，TUI、GUI、mobile app、workspace、pane 和 floating window 只是观察和操作它的入口。**

传统 terminal multiplexer 通常把工作内容绑定到 tab、pane 或 session 里。`termx` 反过来把 terminal 放进独立的 terminal pool，由 core daemon 管理 terminal lifecycle、history、live surface 和输入路由；当前 UI 只负责把这些 terminal 组织成某个工作视图。

这意味着你不需要频繁在很多 tab/session 之间切换，也不需要因为当前 workspace 布局变化就丢掉实际运行中的 terminal。一个 terminal 可以长期存在、可以异常退出后保留现场、可以从 picker 重新绑定到新的 pane，也可以通过不同 endpoint 从本机或远端接回来。

## 为什么它不只是另一个 tmux

`termx` 不是把 tmux/Zellij/WezTerm 的界面重新写一遍。它的主要差异在模型层：

- **Terminal pool 是事实，workspace 是视图**：terminal 不属于某个固定 pane；pane 只是当前 UI 对 terminal 的连接意图。
- **Terminal picker 是主入口**：在一个入口里查看、搜索、创建、连接本机和远端 endpoint 上的 terminal，不靠层层 tab/session 导航找工作现场。
- **TUI 只是观察入口**：TUI 不拥有 terminal lifecycle、committed history 或 history truth；这些都属于 core daemon。
- **程序退出不等于 terminal 消失**：进程异常退出后，panel 和 terminal 可以保留，用户能看到退出态、历史输出和重启入口。
- **历史记录由 core 管理**：copy/history 走 authoritative history window，不让 TUI 从本地 scrollback、snapshot 或 wrapped rows 拼出第二份 truth。
- **多 endpoint 是一等能力**：本地 daemon、SSH 远端 daemon 和未来 hub/P2P 设备都通过 `EndpointID + TerminalID` 的 `TerminalRef` 隔离路由。
- **远程失败局部化**：某个 endpoint 离线、认证失败或 transport 断开，不会清空其他 endpoint 的 terminal pool、layout、copy/history 状态。

## 当前能力

- TUI v3 自有 runtime，不以 Bubble Tea 作为主运行时。
- Terminal picker / Terminal Manager 可按 endpoint 聚合展示 terminal。
- 本地 unix socket endpoint 已作为标准 transport 接入。
- SSH transport 已用于连接远端 `termx` daemon，不 fallback 成原始 SSH shell。
- `connections.yaml` 是 CLI/TUI 共享的 endpoint registry，独立于 TUI 偏好配置。
- Workbench/layout storage 持久化 endpoint-aware terminal binding。
- live/input/resize/owner/copy/history 按 `TerminalRef` 路由隔离。
- first-party create 优先使用用户可见 terminal name 作为 daemon-local key，并在单 endpoint 内拒绝重名。
- hub/P2P 的身份、安全、中继策略和 registry contract 已收敛；真实 dialer 和跨设备发现属于后续切片。
- TUI 本地同步输入组正在实现，用于向一组 terminal 多播普通键盘输入和 paste。

## 适合的使用场景

`termx` 主要面向高强度 terminal 用户：

- 同时维护多个项目、服务、测试环境或 long-running jobs。
- 经常在本机和多台远端机器之间切换。
- 使用 Claude Code、Codex、Gemini CLI、OpenCode 等 agent terminal，且需要长期保留输出现场。
- 需要在程序异常退出后保留 panel、历史输出和重启上下文。
- 希望未来能从 TUI、GUI 或 mobile app 接回同一个 terminal pool。

如果你的工作流只有一两个短生命周期 shell，传统 terminal emulator 或 tmux 已经足够。`termx` 的优势主要在 terminal 数量变多、生命周期变长、入口变多之后显现。

## 架构边界

```text
PTY bytes / resize
        |
        v
termx-vterm semantic interpretation
        |
        v
termx-core-v2 daemon
  - terminal lifecycle
  - live surface
  - authoritative history/copy
  - attachment / resize ownership
  - opaque storage
        |
        v
internal protocol + transport
        |
        v
EndpointManager
  - local unix socket
  - SSH daemon endpoint
  - future hub/P2P endpoint
        |
        v
termx-tui-v3 / future GUI / future mobile entry
```

几个硬边界：

- `termx-core-v2` 拥有 terminal lifecycle 和 history truth。
- `termx-tui-v3` 只消费 core 的 live surface、terminal metadata 和 authoritative history window。
- workbench storage 只保存布局和连接意图，不保存 terminal running/exited truth。
- `TerminalID` 只在单个 daemon/endpoint 内唯一；跨 endpoint 必须使用 `TerminalRef`。
- endpoint label、transport address、SSH host key、hub device fingerprint 和 grant ref 不能互相替代。
- SSH transport 只连接远端 `termx` daemon，不隐式退化成普通 shell。

## 仓库结构

- `termx-cli/`：`termx` 命令行入口，组装 core-v2 与 tui-v3。
- `termx-core-v2/`：core daemon 主线，负责 terminal lifecycle、history、live surface、storage 和 protocol 服务端能力。
- `termx-tui-v3/`：当前 TUI 主线，负责 AppRuntime、TerminalHost、EndpointManager、state、render、copy/history 投影和交互。
- `termx-vterm/`：终端语义解释来源，把 PTY bytes 解释成 terminal 语义事件或 transaction。
- `termx-shared/`：共享 connection registry、transport 等基础包。
- `internal/protocol/`、`termx-proto/`：daemon/client wire contract 与协议类型。
- `termx-hub/`：hub/P2P 身份、发现、中继和受限 transport 方向。
- `termx-testkit/`：测试辅助能力。
- `scripts/`、`Makefile`、`go.work`：开发、测试和 workspace 支撑。

当前分支已删除旧路径 `termx-remote/`、`termx-remote-v2/`、`termx-app/`、`remote-ui/`、`web-control/`。它们不属于当前主动开发主线，不得作为 fallback、只读参考或默认依赖恢复；需要判断范围时以 `workflow.md` 为准。

## 开发状态

当前主线是 **多 endpoint / 多 transport 管理**。活动驱动文件是仓库根目录的 [`workflow.md`](workflow.md)。

已完成的关键切片包括：

- `EndpointID` / `TerminalRef` 状态模型。
- connection registry 基础结构。
- Terminal picker / Terminal Pool endpoint 聚合。
- live/input/resize/owner/copy/history endpoint 隔离。
- endpoint-aware workbench binding。
- local unix socket 标准 transport。
- SSH transport 连接远端 `termx` daemon。
- terminal name identity 第一阶段。
- hub/P2P identity、security、relay 和 grant contract。

正在进行：

- TUI 同步输入组交互与 input 多播。

后续：

- hub/P2P transport dialer 与跨设备发现。

## 构建与测试

常用命令：

```bash
cd termx-cli
go test ./cmd/termx -count=1
go build ./cmd/termx
```

按模块运行测试：

```bash
cd termx-tui-v3 && go test ./... -count=1
cd termx-core-v2 && go test ./... -count=1
cd termx-shared && go test ./... -count=1
go test ./internal/protocol/... -count=1
```

文档-only 改动至少运行：

```bash
git diff --check
```

## 开发规则

本仓库的有效工作范围、任务顺序、测试准入和提交规则都以 [`workflow.md`](workflow.md) 为准。

简要规则：

- 每轮先读 `workflow.md`。
- 不跨当前切片扩展范围。
- 不把 TUI 写成 terminal lifecycle 或 history truth owner。
- 不引入旧 `termx-core`、旧 `tuiv2`、旧 remote fallback。
- 有效改动需要运行对应测试准入并提交。
- 中文注释用于说明关键 domain owner、truth source、消息链路或失败条件。
