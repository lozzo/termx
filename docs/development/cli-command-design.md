# TermX CLI 命令体系设计

## 1. 目的

本文定义 `termx` 面向用户、脚本和运维的长期命令体系。目标不是逐字复制 tmux，而是吸收 tmux 已验证的控制面能力：稳定 target、完整生命周期、可组合输出、非交互控制、事件等待和清晰别名，同时保持 TermX 自己的领域模型。

TermX 的 terminal 是 daemon 拥有的长期实体；workspace、tab、pane 和 floating window 只是客户端视图。多机目标由 `EndpointID + TerminalID` 唯一确定。CLI 不得把 pane 当成 terminal lifecycle truth，也不得因 Cloud、SSH 或 local transport 不同而复制三套 terminal 命令。

本文是命令面设计门禁，不表示所有命令已经实现。每个阶段必须先有真实 protocol/domain owner，再挂 CLI；不得用 CLI 内部状态、shell fallback 或解析 TUI 画面伪造能力。

## 2. 当前命令盘点

当前顶层命令如下：

| 命令 | 当前行为 | 主要问题 |
| --- | --- | --- |
| `termx` | 连接或自动启动 local daemon，进入 TUI | 可用，但自动启动、目标选择和错误输出未形成统一 contract |
| `new` | 在 local daemon 创建 terminal | 名称过于宽泛，仅支持 local socket，缺少 cwd/env/输出格式 |
| `ls` | 列出 local terminal | 没有标题、过滤、JSON、format 或 endpoint 维度 |
| `attach` | attach local terminal 并进入 TUI | target 只有裸 TerminalID，不能表达 TerminalRef |
| `kill` | 终止 terminal 进程 | 与删除、重启的差异没有在 help 中说明 |
| `rm` | 删除 daemon inventory 记录 | 缺少运行态保护、确认和稳定退出码 |
| `daemon` | 前台运行 core-v2 daemon | 没有 start/stop/status/logs；`--cloud` 暴露了未装配细节 |
| `pair create/import` | 签发或导入 capability bundle | 缺少 inspect/list/revoke；import 同时修改 credential 与 registry，但结果不可查询 |
| `cloud ...` | Companion 安装、登录、enroll、状态和诊断 | 用户 Companion 与 daemon Companion 未在命令模型中分开；staging 会泄漏 socket/runuser/DBus 装配细节 |
| `completion` | 生成 shell completion | 可保留 |
| `licenses` | 输出第三方许可 | 可保留，缺少 `version` |
| `v3 ...` | 重复 local 命令并暴露 smoke/tmux harness | 内部迁移名和测试工具不应出现在产品 CLI |

已有 protocol 实际支持 Create/List/Kill/Restart/Remove、metadata/tags、Attach/Detach、Input、Resize、LiveScreen、HistoryWindow/Copy、events、workbench storage 和文件操作。CLI 只暴露了其中很小一部分。另一方面，daemon client 列表、跨客户端强制 detach、pairing grant revoke 和稳定 server service manager 尚无完整 contract，不能只靠新增 Cobra command 冒充完成。

## 3. 当前问题结论

1. 命令没有按 domain object 分组，五个无描述的短命令与 Cloud、pair、daemon 混在同一层。
2. local `--socket` 被当成全局目标，但 SSH/Hub endpoint 只能从 TUI 使用，CLI 不是多 endpoint 一等客户端。
3. 裸 `TerminalID` 被当成全局 target，和 `TerminalRef` 硬语义冲突。
4. human 输出没有稳定列，machine 输出没有 JSON schema、format、filter 或退出码约定。
5. `v3` 同时承载重复产品命令、内部诊断和 tmux 测试，迁移实现泄漏到用户界面。
6. daemon 自动启动与显式前台运行并存，却没有可观察的 status/stop/restart contract。
7. Cloud 用户登录、Companion 安装和 daemon enrollment 混在一个 namespace；Linux keyring、IPC socket 和 service user 暴露给用户。
8. 缺少 tmux 用户依赖的自动化能力：send、capture、wait、events、稳定 target 和格式化列表。
9. 命令失败大多只返回普通 error，脚本无法可靠区分 usage、not found、conflict、auth、unavailable 和 timeout。

## 4. 设计原则

### 4.1 对象优先

canonical command 使用 `termx <object> <verb>`。高频旧短命令可以作为同一个 command handler 的别名保留，但不能维护第二套执行路径。

### 4.2 真值不下沉到 CLI

- terminal lifecycle、metadata、live 和 history 来自 owning daemon。
- endpoint 配置来自共享 connection registry；运行连接状态来自实际 dial/session。
- workspace 是客户端视图投影，不得包含 terminal running/exited truth。
- Cloud 账号、节点 ownership 和订阅来自 Control Plane/Companion contract。
- CLI 只做参数解析、target resolution、调用和输出，不缓存第二份领域状态。

### 4.3 transport 中立

同一条 `terminal list/create/send/capture` 必须能路由到 local、SSH 或 managed Hub endpoint。Cloud 失败不能 fallback 到 local，SSH 失败不能 fallback 成原始 shell。

### 4.4 人类与脚本同等重要

默认输出适合终端阅读；所有查询命令提供稳定 JSON。批量和写操作必须有明确 stdin、timeout、确认及退出码语义。

### 4.5 开发命令不进入产品树

`smoke`、`visual-snapshot`、`tmux-*-smoke` 和 history 内部 backlog harness 移入测试脚本或单独开发工具，不进入 release `termx --help`。`v3` namespace 在新命令完成后直接删除，不保留 legacy fallback。

## 5. 目标与寻址

### 5.1 TerminalRef 文本形式

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

### 5.2 其他 target

workspace、view 和 client 若后续进入 CLI，使用带类型的独立 ID，不复用 TerminalID。涉及 pane/view 的命令必须显式要求 view target，不能用 terminal target 推断当前 TUI pane。

## 6. 目标命令树

```text
termx                                  打开 TUI
termx terminal <verb>                  terminal lifecycle 与数据面控制
termx endpoint <verb>                  endpoint registry 与连通性
termx daemon <verb>                    本机 daemon 运行与服务管理
termx workspace <verb>                 workbench 视图管理
termx file <verb>                      owning daemon 文件操作
termx pair <verb>                      端到端 capability 配对
termx cloud <verb>                     用户账号和托管云能力
termx config <verb>                    配置发现、校验与修改
termx doctor                           聚合本机公共能力诊断
termx version
termx completion <shell>
termx licenses
```

### 6.1 terminal

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
termx new       -> termx terminal create
termx ls        -> termx terminal list
termx attach    -> termx terminal attach
termx kill      -> termx terminal kill
termx rm        -> termx terminal remove
```

### 6.2 endpoint

```text
termx endpoint list
termx endpoint show ID
termx endpoint add local|ssh|cloud ID [flags]
termx endpoint update ID [flags]
termx endpoint remove ID
termx endpoint enable|disable ID
termx endpoint set-default ID
termx endpoint test ID
```

`endpoint list/show` 展示 registry 期望状态与一次显式探测结果，但不得把缓存配置冒充在线状态。`endpoint test` 必须完成 transport、protocol Hello 和 daemon identity 校验；SSH 不执行原始 shell fallback，Cloud 不输出 token/grant。

### 6.3 daemon

```text
termx daemon run [--cloud]              前台运行
termx daemon start [--cloud]            启动当前用户 daemon service
termx daemon stop
termx daemon restart
termx daemon status [--json]
termx daemon logs [--follow]
termx daemon doctor
```

`termx daemon` 可以作为 `daemon run` 的简写。start/stop/status 必须由明确的跨平台 service manager 或 pid/socket ownership contract 实现，不能使用宽泛 `pkill termx`。系统级 daemon 与当前用户 daemon 必须显式区分；停止 daemon 的帮助文本必须说明它会影响该 endpoint 的所有客户端，退出 TUI 不应调用 daemon stop。

### 6.4 workspace

```text
termx workspace list
termx workspace show ID
termx workspace create --name NAME
termx workspace rename ID NAME
termx workspace remove ID
termx workspace export ID
termx workspace import FILE
```

该 namespace 只管理 workbench/layout 投影。tab/pane/floating 的细粒度脚本控制延后到稳定 view target contract；不得照搬 tmux 的 window/pane lifecycle 并把 terminal 状态写回 workspace storage。

### 6.5 file

```text
termx file list ENDPOINT [PATH]
termx file stat ENDPOINT PATH
termx file cat ENDPOINT PATH
termx file download ENDPOINT REMOTE [LOCAL]
termx file upload ENDPOINT LOCAL [REMOTE]
termx file mkdir ENDPOINT PATH
termx file rename ENDPOINT OLD NEW
termx file copy ENDPOINT SRC... DEST
termx file move ENDPOINT SRC... DEST
termx file remove ENDPOINT PATH...
```

ENDPOINT 用于选择 owning daemon，当前 protocol session 仍必须持有显式文件权限；文件系统能力不绑定某个 TerminalID，也不允许 Control Plane、Hub 或 Relay看到路径和内容。批量操作的部分成功必须使用结构化 per-item result，不能只返回最后一个错误。

### 6.6 pair

```text
termx pair create [--terminal TARGET] [--ttl DURATION] [--out FILE]
termx pair import FILE [--id ENDPOINT] [--relay MODE]
termx pair inspect FILE
termx pair list
termx pair revoke GRANT-ID
```

create/import 立即整理；inspect 只能显示非秘密 metadata；list/revoke 必须先建立 daemon-owned grant registry/revocation contract。raw grant 永不进入普通 JSON、日志、connections.yaml 或 shell completion。

### 6.7 cloud

```text
termx cloud login
termx cloud logout
termx cloud status
termx cloud doctor
termx cloud node list
termx cloud node enroll
termx cloud node revoke ID
termx cloud companion install|update|status|uninstall
```

用户命令不能要求 `dbus-run-session`、`runuser`、IPC socket 环境变量或手工启动 Companion：

- login/status 自动发现并受限启动当前用户的已验证 Companion。
- node enroll 自动连接系统/用户 daemon owner 的受权限保护本地控制面；DeviceIdentity private key 仍由 daemon owner 使用。
- CLI 只提交一次性 enrollment code，不读取或复制 daemon private key。
- client Companion 与 daemon Companion 的选择由安装/IPC contract 决定，不由用户填写任意 executable 或 socket。
- production 未安装可信 Companion 时 fail closed；staging 通过显式受信开发安装 profile 解决，不用 shell wrapper 冒充产品体验。

### 6.8 config 与通用诊断

```text
termx config paths
termx config show [--effective]
termx config get KEY
termx config set KEY VALUE
termx config unset KEY
termx config validate [FILE]
termx config edit
termx doctor [--json]
termx version [--json]
```

config 修改必须使用现有严格 parser 和原子 writer；secret 只显示引用或 `<redacted>`。`doctor` 聚合 binary、config、daemon socket、endpoint registry、Companion 可用性和权限，不发起 terminal 输入，也不打印凭据。

## 7. 输出契约

### 7.1 三种输出模式

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

### 7.2 查询通用参数

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

### 7.3 写操作通用参数

```text
--timeout DURATION
--yes
--dry-run
--no-start
--quiet
```

危险操作在交互终端可确认；非交互调用若需要确认但未给 `--yes` 必须失败。`--no-start` 禁止查询命令隐式启动 local daemon，适用于监控和 service manager。

### 7.4 退出码

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

## 8. tmux 能力映射

| tmux 能力 | TermX 对应 | 说明 |
| --- | --- | --- |
| `new-session` | `terminal create [--attach]` | TermX terminal 不属于 session；workspace 另管视图 |
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

不计划复制 tmux 的 key table、status-line option、server-side shell condition 和任意 `run-shell`。TermX TUI 配置、renderer 和 daemon 控制面保持独立；通用自动化应通过结构化命令和事件完成。

## 9. 实施切片

### CLI001：命令设计门禁

- 完成本文件、现状审计、target/output/error contract 和分期范围。
- 不修改运行代码。

### CLI002：骨架与 local terminal 闭环

- 建立 canonical `terminal` namespace、共享 target/output/error 层和完整 help。
- 接通 create/list/show/attach/restart/kill/remove/rename/tag。
- 保留五个高频别名但不保留重复 handler。
- 从产品命令树删除 `v3` 和 smoke harness；测试入口迁到 scripts/test binary。
- 验收 human/JSON、退出码和真实 local daemon lifecycle。

### CLI003：daemon 与配置生命周期

- 实现 daemon run/start/stop/restart/status/logs/doctor。
- 实现 config paths/show/get/set/unset/validate。
- macOS/Linux 分别使用明确 service ownership；不使用广泛 pkill。

### CLI004：endpoint-aware 控制

- 实现 TerminalRef parser、endpoint registry commands 和 local/SSH/Hub dialer 复用。
- terminal query/mutation 经 owning endpoint 路由，跨 endpoint 同名不冲突。
- 完成 SSH 与 managed direct/single Relay 真实 CLI E2E。

### CLI005：自动化数据面

- 实现 send/capture/resize/wait/events、NDJSON 和稳定 format。
- history/live/input 全部使用 owning daemon protocol，不读取 TUI renderer。

### CLI006：file、workspace、pair 与 Cloud UX

- 暴露已有文件 contract 和 workspace 投影。
- 补 pair inspect；list/revoke 只有在 daemon revocation contract 完成后进入。
- 收口 Cloud 当前用户/daemon Companion 自动发现，让 login/enroll 不暴露 DBus、runuser 或 socket。

## 10. 明确延后

- tmux 完整 format/filter 表达式兼容。
- raw RPC console 或任意 server-side shell execution。
- plugin command namespace。
- Relay Mesh、多区域路由管理和复杂计费 CLI。
- 未定义 view/client owner 前的强制 detach、swap-pane、join-pane、pipe-pane。
- shell completion 中枚举 secret、grant、token 或远端文件内容。

## 11. 验收总则

每个产品命令必须至少有：

1. help/usage/示例测试。
2. human 与 JSON golden test。
3. target 不存在、状态冲突、权限拒绝、endpoint 不可用和 timeout 测试。
4. `stdout`/`stderr`/exit code 黑盒测试。
5. 至少一个真实 daemon protocol harness；endpoint-aware 命令还需对应 transport E2E。
6. secret scan，确保账号 token、CapabilityGrant、DeviceIdentity private key、TURN credential 和文件内容不会进入非预期输出。
7. `termx --help` 不出现迁移版本号、smoke harness、旧 remote 或 archive 命令。
