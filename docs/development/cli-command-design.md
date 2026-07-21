# Muxvia CLI 命令体系设计

## 1. 目的

本文定义 `muxvia` 面向用户、脚本和运维的长期命令体系。目标不是逐字复制 tmux，而是吸收 tmux 已验证的控制面能力：稳定 target、完整生命周期、可组合输出、非交互控制、事件等待和清晰别名，同时保持 Muxvia 自己的领域模型。

Muxvia 的 terminal 是 daemon 拥有的长期实体；workspace、tab、pane 和 floating window 只是客户端视图。多机目标由 `EndpointID + TerminalID` 唯一确定。CLI 不得把 pane 当成 terminal lifecycle truth，也不得因 Cloud、SSH 或 local transport 不同而复制三套 terminal 命令。

本文是命令面设计门禁，不表示所有命令已经实现。每个阶段必须先有真实 protocol/domain owner，再挂 CLI；不得用 CLI 内部状态、shell fallback 或解析 TUI 画面伪造能力。

目录 ownership 和依赖方向以 [`repository-layout.md`](repository-layout.md) 为准。

## 当前连接运行时状态

CLI 命令树使用 Endpoint/Route registry、`TerminalRef` 和统一 `ClientRuntime/SessionOwner`；local Unix、Direct WebRTC TCP、SSH WebRTC TCP 与 managed WebRTC 都作为同一 planner/runtime 的 Route connector 接入。

共享 runtime 已完成 local/Direct/SSH route race、priority hedge、显式 override/sticky reconnect、winner/loser cleanup、ReadyPeerSession 身份门禁和 operation generation fence。SSH 使用 Go client + `direct-tcpip` 承载同一 WebRTC DataChannel；CLI/TUI 不再拥有直接 route/dial owner，也不得恢复旧 helper、raw protocol adoption、进程型 OpenSSH proxy 或 only-viable local 假路径。

CLI 只保留以下职责：

- 解析 Cobra 参数、`EndpointID:TerminalID` target 和可选 `--route` override。
- 调用 `client/runtime` planner/session owner，不在 `cmd/muxvia` 内复制 route 选择或 session 状态。
- 输出稳定的人类/JSON 结果和 typed error 对应的退出码。
- 保持 route 切换不改变 EndpointID 或 TerminalRef。

具体 planner、race、generation、取消和资源回收契约只在 [`tui/docs/multi-endpoint-transport-plan.md`](../../tui/docs/multi-endpoint-transport-plan.md) 维护；任务状态和准入只看 `workflow.md`。CLI 不得绕过统一 runtime fallback 到 local、原始 SSH shell 或旧 remote runtime。

## 2. 现状核对

本文不手工维护“当前有哪些命令”的快照。当前产品命令、别名、参数和 help 以 `muxvia --help`、各子命令 `--help` 及 `cmd/muxvia` 黑盒测试为准；活动切片完成状态只看 `workflow.md`。

设计文档只保留稳定约束：命令按领域对象组织，target 使用 TerminalRef，machine output 和退出码可脚本化，开发 harness 不进入产品树，尚无真实 domain/protocol contract 的能力不得只靠 Cobra command 伪造。

## 3. 设计原则

### 3.1 对象优先

canonical command 使用 `muxvia <object> <verb>`。高频旧短命令可以作为同一个 command handler 的别名保留，但不能维护第二套执行路径。

### 3.2 真值不下沉到 CLI

- terminal lifecycle、metadata、live 和 history 来自 owning daemon。
- endpoint 配置来自共享 connection registry；运行连接状态来自实际 dial/session。
- workspace 是客户端视图投影，不得包含 terminal running/exited truth。
- Cloud 账号、节点 ownership 和订阅来自 Control Plane/Companion contract。
- CLI 只做参数解析、target resolution、调用和输出，不缓存第二份领域状态。

### 3.3 transport 中立

同一条 `terminal list/create/send/capture` 必须能路由到 local、SSH 或 managed Hub endpoint。Cloud 失败不能 fallback 到 local，SSH 失败不能 fallback 成原始 shell。

### 3.4 人类与脚本同等重要

默认输出适合终端阅读；所有查询命令提供稳定 JSON。批量和写操作必须有明确 stdin、timeout、确认及退出码语义。

### 3.5 开发命令不进入产品树

`smoke`、`visual-snapshot`、`tmux-*-smoke` 和 history 内部 backlog harness 移入测试脚本或单独开发工具，不进入 release `muxvia --help`。`v3` namespace 在新命令完成后直接删除，不保留 legacy fallback。

## 4. 目标与寻址

### 4.1 TerminalRef 文本形式

canonical target 为：

```text
<endpoint-id>:<terminal-id>
```

例如：

```text
local:api
office-ssh:build
cloud-sg:prod-shell
```

规则：

- `--endpoint <id>` 与裸 `<terminal-id>` 组合后等价于完整 TerminalRef。
- 未提供 endpoint 时使用 connection registry 的 default endpoint。
- `--all-endpoints` 只允许用于明确支持聚合的只读命令。
- endpoint ID 和 terminal ID 必须采用不会与 `:` 冲突的规范字符集；不得靠猜测拆分任意旧字符串。
- 输出 target 时始终显示完整 TerminalRef，避免不同 daemon 上同名 terminal 混淆。
- `--socket` 降级为 local transport 的专家覆盖参数，不能继续代表通用 endpoint。

### 4.2 其他 target

workspace、view 和 client 若后续进入 CLI，使用带类型的独立 ID，不复用 TerminalID。涉及 pane/view 的命令必须显式要求 view target，不能用 terminal target 推断当前 TUI pane。

## 5. 目标命令树

```text
muxvia                                  打开 TUI
muxvia terminal <verb>                  terminal lifecycle 与数据面控制
muxvia endpoint <verb>                  endpoint registry 与连通性
muxvia daemon <verb>                    本机 daemon 运行与服务管理
muxvia workspace <verb>                 workbench 视图管理
muxvia file <verb>                      owning daemon 文件操作
muxvia pair <verb>                      端到端 capability 配对
muxvia cloud <verb>                     用户账号和托管云能力
muxvia config <verb>                    配置发现、校验与修改
muxvia doctor                           聚合本机公共能力诊断
muxvia version
muxvia completion <shell>
muxvia licenses
```

### 5.1 terminal

| 命令 | 语义 | 实现基础 |
| --- | --- | --- |
| `terminal create [-- COMMAND...]` | 在 owning endpoint 创建 terminal；支持 `--name`、`--cwd`、`--env`、`--cols/--rows`、`--attach` | Create 已存在；需 endpoint-aware CLI |
| `terminal list` | 列出一个或多个 endpoint 的 terminal | List 已存在 |
| `terminal show TARGET` | 展示 lifecycle、command、cwd、size、tags、exit 信息 | List/metadata 已存在 |
| `terminal attach TARGET` | 进入 TUI attach 指定 TerminalRef | local 已有；需 endpoint target |
| `terminal restart TARGET` | 按 daemon 保存的 process spec 重启 | Restart 已存在 |
| `terminal kill TARGET` | 终止进程，保留 exited 记录与历史 | Kill 已存在 |
| `terminal remove TARGET` | 删除 inventory 记录；运行态默认拒绝 | Remove 已存在 |
| `terminal rename TARGET NAME` | 修改展示名/metadata，不改变稳定 ID | SetMetadata 已存在 |
| `terminal tag TARGET [KEY=VALUE...]` | 设置或删除 tags | SetTags 已存在 |
| `terminal send TARGET ...` | 向已授权 terminal 输入 bytes/keys/stdin | Input 已存在；需一次性 attach/input contract |
| `terminal capture TARGET` | 从 authoritative history/live 复制文本 | HistoryCopy/LiveScreen 已存在 |
| `terminal resize TARGET COLS ROWS` | 请求 owning daemon resize，显示 owner/拒绝原因 | resize contract 已存在 |
| `terminal wait TARGET --state ...` | 等待 created/running/exited/removed 或超时 | events 已存在 |
| `terminal events [TARGET]` | 输出 lifecycle 事件流，支持 NDJSON | events 已存在 |

保留以下高频别名，别名必须指向同一 command object：

```text
muxvia new       -> muxvia terminal create
muxvia ls        -> muxvia terminal list
muxvia attach    -> muxvia terminal attach
muxvia kill      -> muxvia terminal kill
muxvia rm        -> muxvia terminal remove
```

### 5.2 endpoint

```text
muxvia endpoint list
muxvia endpoint show ID
muxvia endpoint add local|ssh|cloud ID [flags]
muxvia endpoint update ID [flags]
muxvia endpoint remove ID
muxvia endpoint enable|disable ID
muxvia endpoint set-default ID
muxvia endpoint test ID
```

`endpoint list/show` 展示 registry 期望状态与一次显式探测结果，但不得把缓存配置冒充在线状态。`endpoint test` 必须完成 transport、protocol Hello 和 daemon identity 校验；SSH 不执行原始 shell fallback，Cloud 不输出 token/grant。

### 5.3 daemon

```text
muxvia daemon run [--cloud]              前台运行
muxvia daemon start [--cloud]            启动当前用户 daemon service
muxvia daemon stop
muxvia daemon restart
muxvia daemon status [--json]
muxvia daemon logs [--follow]
muxvia daemon doctor
```

`muxvia daemon` 可以作为 `daemon run` 的简写。start/stop/status 必须由明确的跨平台 service manager 或 pid/socket ownership contract 实现，不能使用宽泛 `pkill muxvia`。系统级 daemon 与当前用户 daemon 必须显式区分；停止 daemon 的帮助文本必须说明它会影响该 endpoint 的所有客户端，退出 TUI 不应调用 daemon stop。

### 5.4 workspace

```text
muxvia workspace list
muxvia workspace show ID
muxvia workspace create --name NAME
muxvia workspace rename ID NAME
muxvia workspace remove ID
muxvia workspace export ID
```

该 namespace 只管理 workbench/layout 投影。当前 daemon contract 支持 versioned mutation 和 snapshot export，但没有原子 snapshot replace，因此 `workspace import` 延后，禁止用多次 create/split 重放制造半导入状态。tab/pane/floating 的细粒度脚本控制延后到稳定 view target contract；不得照搬 tmux 的 window/pane lifecycle 并把 terminal 状态写回 workspace storage。

### 5.5 file

```text
muxvia file list ENDPOINT [PATH]
muxvia file stat ENDPOINT PATH
muxvia file cat ENDPOINT PATH
muxvia file download ENDPOINT REMOTE [LOCAL]
muxvia file upload ENDPOINT LOCAL [REMOTE]
muxvia file mkdir ENDPOINT PATH
muxvia file rename ENDPOINT OLD NEW
muxvia file copy ENDPOINT SRC... DEST
muxvia file move ENDPOINT SRC... DEST
muxvia file remove ENDPOINT PATH...
```

ENDPOINT 用于选择 owning daemon，当前 protocol session 仍必须持有显式文件权限；文件系统能力不绑定某个 TerminalID，也不允许 Control Plane、Hub 或 Relay看到路径和内容。批量操作的部分成功必须使用结构化 per-item result，不能只返回最后一个错误。

### 5.6 pair

```text
muxvia pair create [--terminal TARGET] [--ttl DURATION] [--out FILE]
muxvia pair import FILE [--id ENDPOINT] [--relay MODE]
muxvia pair inspect FILE
```

create/import/inspect 已实现；inspect 只能显示非秘密 metadata。list/revoke 必须先建立 daemon-owned grant registry/revocation contract，当前不进入产品树。raw grant 永不进入普通 JSON、日志、`endpoints.yaml` 或 shell completion。

### 5.7 cloud

```text
muxvia cloud login
muxvia cloud logout
muxvia cloud status
muxvia cloud doctor
muxvia cloud node enroll
muxvia cloud companion install|update|status|uninstall
```

`cloud node list/revoke` 需要 Control Plane/Companion 增加账号节点目录与撤销 contract，当前不以 Web Controller 页面抓取或本地缓存伪造。旧的 `cloud enroll/install/update/uninstall` 作为兼容入口暂时保留，canonical help 使用 `cloud node` 与 `cloud companion` 分组。

用户命令不能要求 `dbus-run-session`、`runuser`、IPC socket 环境变量或手工启动 Companion：

- login/status 自动发现并受限启动当前用户的已验证 Companion。
- node enroll 自动连接系统/用户 daemon owner 的受权限保护本地控制面；DeviceIdentity private key 仍由 daemon owner 使用。
- CLI 只提交一次性 enrollment code，不读取或复制 daemon private key。
- client Companion 与 daemon Companion 的选择由安装/IPC contract 决定，不由用户填写任意 executable 或 socket。
- production 未安装可信 Companion 时 fail closed；staging 通过显式受信开发安装 profile 解决，不用 shell wrapper 冒充产品体验。

### 5.8 config 与通用诊断

```text
muxvia config paths
muxvia config show [--effective]
muxvia config get KEY
muxvia config set KEY VALUE
muxvia config unset KEY
muxvia config validate [FILE]
muxvia config edit
muxvia doctor [--json]
muxvia version [--json]
```

config 修改必须使用现有严格 parser 和原子 writer；secret 只显示引用或 `<redacted>`。`doctor` 聚合 binary、config、daemon socket、endpoint registry、Companion 可用性和权限，不发起 terminal 输入，也不打印凭据。

## 6. 输出契约

### 6.1 三种输出模式

- 默认 human table/detail：允许改善排版，不作为机器稳定接口。
- `--json`：稳定 envelope 和字段，禁止 ANSI；列表也输出 JSON object，不输出裸数组。
- `--format TEMPLATE`：轻量字段模板，用于 shell；字段来自同一 view model，不读取 renderer 文本。

建议 JSON envelope：

```json
{
  "schema_version": 1,
  "kind": "terminal_list",
  "items": []
}
```

`--json`、`--format` 互斥。事件流使用 `--json=stream` 或显式 `--output ndjson`，每行一个带 schema version 的对象。secret 字段从 schema 层排除，不能先序列化再字符串脱敏。

CLI005 固定 `--format` 为 Go template 语法，字段名使用 JSON 同源的小写稳定名称，例如 `{{.target}}|{{.state}}|{{.cols}}x{{.rows}}`；未知字段必须失败。`terminal events --output ndjson` 每行输出独立的 `schema_version=1` `terminal_event` 对象，不能混入 human 文本或 ANSI。

### 6.2 查询通用参数

```text
--endpoint ID
--all-endpoints
--state running|exited|...
--tag KEY[=VALUE]
--filter EXPR
--sort FIELD
--reverse
--limit N
--json
--format TEMPLATE
--no-header
```

首轮只实现明确字段过滤，不复制 tmux 完整表达式语言。format/filter 语法必须版本化、有测试并拒绝未知字段。

### 6.3 写操作通用参数

```text
--timeout DURATION
--yes
--dry-run
--no-start
--quiet
```

根命令另提供 `muxvia --timeout DURATION ...`，显式设置时覆盖从 endpoint 拨号、protocol Hello 到最终 RPC/流操作的完整命令生命周期；默认 `0` 表示不施加根级 deadline，避免 TUI、daemon foreground 和事件流被隐式截断。子命令自己的 `--timeout` 可以进一步缩短 deadline，不能延长根 context。

危险操作在交互终端可确认；非交互调用若需要确认但未给 `--yes` 必须失败。`--no-start` 禁止查询命令隐式启动 local daemon，适用于监控和 service manager。

### 6.4 退出码

| Code | 类别 |
| --- | --- |
| 0 | 成功 |
| 1 | 未分类运行失败 |
| 2 | 参数或配置错误 |
| 3 | target 不存在 |
| 4 | 状态冲突，例如删除 running terminal |
| 5 | 认证、授权或 capability 拒绝 |
| 6 | endpoint/daemon/Companion 不可用 |
| 7 | timeout 或取消 |
| 8 | 部分成功；详细结果必须可机器读取 |

Cobra usage 只在参数错误时输出；网络或业务错误不得刷整页 usage。错误写 stderr，正常数据写 stdout。

## 7. tmux 能力映射

| tmux 能力 | Muxvia 对应 | 说明 |
| --- | --- | --- |
| `new-session` | `terminal create [--attach]` | Muxvia terminal 不属于 session；workspace 另管视图 |
| `list-sessions` | `terminal list` + `workspace list` | 不把两类真值合并成假 session |
| `attach-session` | `terminal attach TARGET` | target 是 TerminalRef |
| `kill-session/pane` | `terminal kill` | kill 后保留 exited/history；remove 是另一动作 |
| `respawn-pane` | `terminal restart` | 使用 daemon 保存的 process spec |
| `send-keys` | `terminal send` | 支持 key、literal bytes、stdin，禁止隐式 shell quoting |
| `capture-pane` | `terminal capture` | 使用 authoritative HistoryWindow/LiveScreen，不抓 TUI canvas |
| `resize-pane` | `terminal resize` | 尊重 resize owner，拒绝原因可见 |
| `list-panes -F` | `terminal list --format` | 格式字段来自稳定 view model |
| `wait-for` | `terminal wait` / `terminal events` | 基于 daemon events，不轮询 TUI |
| `pipe-pane` | 后续 `terminal pipe` | 先定义背压、生命周期和密文边界，不在首轮实现 |
| control mode | 后续 batch/NDJSON 控制流 | 不暴露未经授权的 raw internal RPC |
| window/pane layout | `workspace` 与未来 view commands | layout 不拥有 terminal lifecycle |

不计划复制 tmux 的 key table、status-line option、server-side shell condition 和任意 `run-shell`。Muxvia TUI 配置、renderer 和 daemon 控制面保持独立；通用自动化应通过结构化命令和事件完成。

## 8. 明确延后

- tmux 完整 format/filter 表达式兼容。
- raw RPC console 或任意 server-side shell execution。
- plugin command namespace。
- Relay Mesh、多区域路由管理和复杂计费 CLI。
- 未定义 view/client owner 前的强制 detach、swap-pane、join-pane、pipe-pane。
- shell completion 中枚举 secret、grant、token 或远端文件内容。

## 9. 验收总则

每个产品命令必须至少有：

1. help/usage/示例测试。
2. human 与 JSON golden test。
3. target 不存在、状态冲突、权限拒绝、endpoint 不可用和 timeout 测试。
4. `stdout`/`stderr`/exit code 黑盒测试。
5. 至少一个真实 daemon protocol harness；endpoint-aware 命令还需对应 transport E2E。
6. secret scan，确保账号 token、CapabilityGrant、DeviceIdentity private key、TURN credential 和文件内容不会进入非预期输出。
7. `muxvia --help` 不出现迁移版本号、smoke harness、旧 remote 或 archive 命令。
