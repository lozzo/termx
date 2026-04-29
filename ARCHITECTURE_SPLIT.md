# termx Split Architecture

## Goal

把仓库明确拆成两个一级模块：

- `termx-core/`
- `tuiv2/`
- `termx-cli/`

目标不是“换目录名”，而是固定依赖方向：

- `tuiv2 -> termx-core`
- `termx-core -/-> tuiv2`

只要这个方向不被破坏，后续 `web`、`mobile`、TURN/WebRTC 服务、CLI 壳都可以独立演进。

## Module Roles

### `termx-core/`

只承载 shell-neutral / UI-neutral 能力：

- server / daemon
- terminal lifecycle
- pty / vterm / snapshot
- protocol / transport
- events
- session + workbench service
- shell-neutral Go API

### `tuiv2/`

只承载 TUI shell 能力：

- input / modal / render
- runtime / attach / local view orchestration
- workbench projection

### `termx-cli/`

只承载命令行产品壳能力：

- `termx` 根命令
- daemon / attach / ls / new / kill / web 命令
- 本地 TUI 启动 wiring
- minimal web bridge shell

## Stable Interfaces

下面这些包是跨模块边界的正式接口面。

### 1. Go client contract

包：

- `termx-core/clientapi`

职责：

- 定义 shell-neutral client interface
- 给 `tuiv2`、未来 `web/mobile` 壳复用

规则：

- 新 shell 只能依赖这个 public contract
- 不允许再通过 `internal/clientapi` 之类路径跨模块访问

### 2. Wire contract

包：

- `termx-core/protocol`
- `termx-core/transport`

职责：

- 定义 frame、request/response、stream、events、screen update

规则：

- 这是跨进程/跨设备的稳定边界
- 新 transport（例如 WebRTC）只接在这一层

### 3. Session/workbench document contract

包：

- `termx-core/workbenchdoc`
- `termx-core/workbenchops`
- `termx-core/workbenchsvc`

职责：

- 定义共享 session 的 document / op / service

规则：

- core 只认 doc/op/service，不认 `tuiv2` 本地 workbench 结构
- shell 自己负责把本地状态投影/转换成 `workbenchdoc`

### 4. TUI-local adapter contract

包：

- `tuiv2/workbenchcodec`
- `tuiv2/bridge`

职责：

- `workbenchcodec`：`tuiv2/workbench <-> termx-core/workbenchdoc`
- `bridge`：`tuiv2` 对 `termx-core/clientapi` 的轻量兼容层

规则：

- 这些 adapter 属于 shell，不属于 core
- core 不得反向依赖这些 adapter

## Hard Dependency Rules

1. `termx-core` 禁止 import `tuiv2/*`
2. `tuiv2` 只能 import `termx-core` 的 public package
3. 跨模块复用禁止走 `internal/*`
4. CLI / web bridge / mobile shell 都属于 app-shell，不属于 core
5. 新共享能力要么进 `termx-core` public package，要么留在各自 shell，禁止半共享半私有

## What Was Done

- 把原来混在一起的 Go 项目拆成：
  - `termx-core/`
  - `tuiv2/`
- 新增 `termx-cli/`，承载真正的命令行产品壳
- 把 shell-neutral client API 从 `internal/clientapi` 提升到 `termx-core/clientapi`
- 把 TUI-specific codec 从 `internal/workbenchcodec` 移到 `tuiv2/workbenchcodec`
- 把 `cmd/termx` 与 `internal/webshell` 从库模块里拿出，迁到 `termx-cli/`
- 建立 root `go.work`，让两个模块并行开发

## Next Cleanup

下面这些还值得继续做，但已经不阻塞当前双模块结构：

- 给 `tuiv2/` 补独立 README / AGENTS
- 把 `termx-core/README.md` 中仍然偏 TUI 的描述继续收口
- 评估把 `tuiv2/cmd/termx` 再拆成独立 `termx-cli/`
- 评估把 `tuiv2/internal/webshell` 再拆成独立 `web-shell/`
- 为未来 `web/mobile` 再抽一层更窄的 app-shell interface
