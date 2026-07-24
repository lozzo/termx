# muxvia

`muxvia` 是一个以 daemon 为核心的 terminal workspace 系统。

> 开发状态：当前分支正在执行客户端目录与连接 runtime 的破坏性重构。旧 CLI/TUI 连接 owner 已删除，`client/runtime` 尚未接回前 `cmd/muxvia` 预期无法完整编译；精确状态见 [`workflow.md`](workflow.md)。

核心理念很简单：**terminal 是长期存在的工作实体，TUI、GUI、mobile app、workspace、pane 和 floating window 只是观察和操作它的入口。**

传统 terminal multiplexer 通常把工作内容绑定到 tab、pane 或 session 里。`muxvia` 反过来把 terminal 放进独立的 terminal pool，由 core daemon 管理 terminal lifecycle、history、live surface 和输入路由；当前 UI 只负责把这些 terminal 组织成某个工作视图。

这意味着你不需要频繁在很多 tab/session 之间切换，也不需要因为当前 workspace 布局变化就丢掉实际运行中的 terminal。一个 terminal 可以长期存在、可以异常退出后保留现场、可以从 picker 重新绑定到新的 pane，也可以通过不同 endpoint 从本机或远端接回来。

## 为什么它不只是另一个 tmux

`muxvia` 不是把 tmux/Zellij/WezTerm 的界面重新写一遍。它的主要差异在模型层：

- **Terminal pool 是事实，workspace 是视图**：terminal 不属于某个固定 pane；pane 只是当前 UI 对 terminal 的连接意图。
- **Terminal picker 是主入口**：在一个入口里查看、搜索、创建、连接本机和远端 endpoint 上的 terminal，不靠层层 tab/session 导航找工作现场。
- **TUI 只是观察入口**：TUI 不拥有 terminal lifecycle、committed history 或 history truth；这些都属于 core daemon。
- **程序退出不等于 terminal 消失**：进程异常退出后，panel 和 terminal 可以保留，用户能看到退出态、历史输出和重启入口。
- **历史记录由 core 管理**：copy/history 走 authoritative history window，不让 TUI 从本地 scrollback、snapshot 或 wrapped rows 拼出第二份 truth。
- **多 endpoint 是一等能力**：本地 daemon、SSH 远端 daemon 和 managed WebRTC 设备都通过 `EndpointID + TerminalID` 的 `TerminalRef` 隔离路由。
- **远程失败局部化**：某个 endpoint 离线、认证失败或 transport 断开，不会清空其他 endpoint 的 terminal pool、layout、copy/history 状态。

## 当前能力

- TUI v3 自有 runtime，不以 Bubble Tea 作为主运行时。
- Terminal picker / Terminal Manager 可按 endpoint 聚合展示 terminal。
- 本地 unix socket endpoint 已作为标准 transport 接入。
- SSH transport 已用于连接远端 `muxvia` daemon，不 fallback 成原始 SSH shell。
- `connections.yaml` 是 CLI/TUI 共享的 endpoint registry，独立于 TUI 偏好配置。
- Workbench/layout storage 持久化 endpoint-aware terminal binding。
- live/input/resize/owner/copy/history 按 `TerminalRef` 路由隔离。
- first-party create 优先使用用户可见 terminal name 作为 daemon-local key，并在单 endpoint 内拒绝重名。
- managed WebRTC endpoint 已通过公开 Cloud Companion contract 接入 TUI；Companion 缺失时只让对应 endpoint unavailable，不影响 local/SSH。
- Android App 已删除旧 Hub/session-token Connector，使用同一 endpoint/relay/error fixture；Community build 对官方 cloud 明确 fail closed，Official build 已通过固定私有 source set 装配同一公开 contract。
- `muxvia cloud` 已提供 signed install/update、login/enroll、status/doctor、logout 和 uninstall；完整 monorepo 的默认产品构建内嵌同版本 Cloud Companion，公开源码构建没有该私有 artifact 时 managed cloud 明确不可用，不影响 local/SSH。
- TUI 与 Android App 共用 endpoint、配对、credential reference 和稳定错误语义；平台层只实现各自的 WebRTC primitive。

## 快速开始与日常命令

### 构建

在完整 monorepo 根目录构建内置 Muxvia Cloud 的单文件产品 CLI：

```bash
make build
```

产物位于 `.artifacts/bin/muxvia`。只有生成公开源码快照时才使用
`make build-public` 构建不含 Cloud 的 `.artifacts/bin/muxvia-public`。下面的示例假设已经把产品产物加入 `PATH`：

```bash
export PATH="$PWD/.artifacts/bin:$PATH"
muxvia --help
```

也可以不修改 `PATH`，把下文的 `muxvia` 替换为 `./.artifacts/bin/muxvia`。

### 启动 TUI

日常使用只需要运行：

```bash
muxvia
```

`muxvia` 会连接本机 core-v2 daemon；daemon 尚未运行时会自动在后台启动。首次没有 terminal 时，TUI 会直接打开 Terminal Picker，让用户选择或创建 terminal。退出 TUI 后，后台 daemon 和其中仍在运行的 terminal 不会因为界面关闭而被主动终止。

常用 TUI 操作：

```text
Ctrl+F              打开 Terminal Picker
Ctrl+G，然后按 q    退出 TUI
Ctrl+P，然后按 d    当前 pane 与 terminal 脱离，不终止 terminal
Ctrl+P，然后按 X    终止当前 pane 连接的 terminal
Esc                 关闭当前菜单或返回上一层
```

`Ctrl+C` 默认会发送给 terminal 内的前台程序，不用于退出 TUI。退出 TUI 应使用 `Ctrl+G`、`q`；这两个键按顺序输入，不是同时按下。

### 管理 terminal

CLI 和 TUI 操作的是同一个 daemon terminal pool：

```bash
# 创建默认 shell；--name 同时作为该 daemon 内的 terminal ID
muxvia new --name dev-shell

# 创建并运行指定命令
muxvia new --name api -- bash -lc 'npm run dev'

# 查看 terminal ID、命令、状态和尺寸
muxvia ls

# 进入指定 terminal 的 TUI
muxvia attach dev-shell

# 终止 terminal 中的进程；记录和历史仍保留为 exited 状态
muxvia kill dev-shell

# 从 daemon inventory 删除 terminal 记录
muxvia rm dev-shell
```

需要彻底删除一个正在运行的 terminal 时，先执行 `kill`，确认 `muxvia ls` 显示其已经退出，再执行 `rm`。关闭 pane、从 pane detach 或退出 TUI 都不会代替 `kill`。

### 手动运行或停止 daemon

通常不需要单独启动 daemon；`muxvia`、`muxvia new`、`muxvia ls` 等命令会在连接失败时自动启动它。排查日志或由进程管理器托管时，可以前台运行：

```bash
muxvia daemon
```

按 `Ctrl+C` 或向该进程发送 `SIGTERM` 会优雅停止前台 daemon。当前 CLI 没有 `muxvia daemon start|stop|status` 子命令；自动启动的后台 daemon 可先定位 PID，再显式停止：

```bash
pgrep -fl 'muxvia.*daemon'
kill -TERM <PID>
```

停止 daemon 会断开该 endpoint 的全部客户端，并结束 daemon-owned runtime；如果目的只是离开当前界面，应退出 TUI，不要停止 daemon。使用自定义 socket 时，daemon 和所有客户端命令必须传入同一个路径：

```bash
muxvia --socket /tmp/my-muxvia.sock daemon
muxvia --socket /tmp/my-muxvia.sock ls
```

### 使用 Muxvia Cloud

Cloud 是可选能力，local 与 SSH 不依赖账号或订阅。Cloud Companion 默认不随 `muxvia` 二进制一起安装；使用官方发行版时先执行 `muxvia cloud install`，再进行登录。直接从源码构建的 `muxvia` 不包含官方 release root，不能验证或安装官方 Companion，需要先换用官方 `muxvia` 发行版。

安装完成后的客户端登录顺序为：

```bash
# 打开设备码登录流程，在浏览器中登录并批准
muxvia cloud login --device-code

# 查看账号、Companion 和本机设备状态
muxvia cloud status
muxvia cloud doctor

# 启动 TUI；登录后的 managed endpoint 会出现在 Terminal Picker
muxvia
```

要让当前机器作为账号名下的云节点，先在 Web 用户中心生成一次性 enrollment code，然后在 daemon 所在机器执行：

```bash
muxvia cloud enroll <ONE_TIME_CODE>
muxvia daemon --cloud
```

`muxvia daemon --cloud` 是前台运行方式；生产或 staging 服务器应由对应的进程管理器托管。退出本机 Cloud 账号只删除本地账号 Session，不会删除 daemon terminal：

```bash
muxvia cloud logout
```

公网 staging 的服务端启动、更新和 systemd 顺序见 [`docs/remote-platform/public-staging-runbook.md`](docs/remote-platform/public-staging-runbook.md)。

### 配置、日志与帮助

- TUI 配置：`$XDG_CONFIG_HOME/muxvia/tui-v3.yaml`，默认是 `~/.config/muxvia/tui-v3.yaml`。
- endpoint registry：`$XDG_CONFIG_HOME/muxvia/connections.yaml`，默认是 `~/.config/muxvia/connections.yaml`。
- 自定义日志：`MUXVIA_LOG_FILE=/tmp/muxvia.log muxvia` 或 `muxvia --log-file /tmp/muxvia.log`。
- 查看所有命令：`muxvia --help`；查看单个命令：`muxvia <command> --help`。

## 快捷键配置与 Ctrl+数字

TUI 快捷键配置文件优先使用：

```text
$XDG_CONFIG_HOME/muxvia/tui-v3.yaml
```

未设置 `XDG_CONFIG_HOME` 时，默认路径是 `~/.config/muxvia/tui-v3.yaml`。

`tui.shortcuts` 是按键执行、footer 提示和 Help 展示的共同来源。同一个 action 可以绑定多个按键；每个 binding 可以通过 `show` 单独控制是否进入 footer。

完整且可直接加载的默认配置模板见 [`tui/docs/tui-v3.example.yaml`](tui/docs/tui-v3.example.yaml)；包含显式 `tui.shortcuts` 替换语义的定制示例见 [`tui/docs/config.example.yaml`](tui/docs/config.example.yaml)。默认快捷键本身只由运行时 catalog 维护，文档不复制第二份手写全表。

省略 `shortcuts` 或写 `shortcuts: {}` 会使用内置默认 catalog；只写 `shortcuts.actions` 会继承默认 bindings，只覆盖 action 展示文案。一旦显式声明任一 scene（包括空的 `global: {}`），用户声明的整个 scene catalog 就完整替换默认 bindings，未列出的默认 scene 不会继承。

键位 token 支持可组合、顺序无关的 `ctrl-`、`alt-`、`shift-` 修饰键，以及以下基础键：

- 任意单字符和 `space`。
- `page-up`（别名 `pgup`）、`page-down`（别名 `pgdn`）、`up`、`down`、`left`、`right`、`home`、`end`。
- `delete`、`insert`、`backspace`、`tab`、`esc`（别名 `escape`）、`enter`（别名 `return`）、`f1` 至 `f12`。

命名键和修饰词不区分大小写，普通单字符则大小写敏感，`R` 与 `r` 是不同 binding。`ctrl-A` 与 `ctrl-a` 是兼容别名；需要显式 Shift 时写 `ctrl-shift-a`。同一 scene 中，等价拼写 canonicalize 到同一运行时键位时会导致配置加载失败，例如 `ctrl-A` 与 `ctrl-a`、`enter` 与 `return`、`ctrl-alt-x` 与 `alt-ctrl-x`；未知键、未知 action、scene 不允许的 action 和参数越界也会直接报错，不会静默跳过。未修饰 `Esc` 是例外的保留键，规则如下。

未修饰的 `Esc` 是 TUI 保留的全局返回键，不写入 `tui.shortcuts`，也不能被配置覆盖。它固定按交互层级返回一层：先退出 prompt suggestion，再关闭当前 dialog/overlay，然后退出 copy/history，最后退出 Panel/Resize/System/Floating/Tab/Workspace 快捷模式；普通 live terminal 没有可返回层时，`Esc` 会继续发送给前台程序。

下面只是 `workspace` scene 片段，必须合并到已有的完整 shortcut catalog，不能把它作为唯一的 `tui.shortcuts` 配置：

```yaml
tui:
  shortcuts:
    workspace:
      "t":
        action: system.open_workbench_tree
        show: true
      "f":
        action: system.open_workbench_tree
        show: false
      "s":
        action: system.open_workbench_tree
        show: false
```

上面的 `t`、`f`、`s` 都可以打开 Workbench Tree，但 footer 只展示 `[t] TREE`。`show: false` 不会删除按键，也不会把它从 Help 的完整快捷键目录中移除。

连续数字 binding 可以在配置加载阶段展开。下面同样只是需要合并到现有完整 catalog 的 `global` 和 `tab` 片段；现有其他 scene 必须继续保留：

```yaml
tui:
  shortcuts:
    global:
      "ctrl-t": menu.tab
      "ctrl+[1...9]":
        action: tab.jump.{key}
        label: TAB
        show: false

    tab:
      "[1...9]":
        action: tab.jump.{key}
        label: JUMP
        show: true
```

这两组配置分别提供：

```text
Ctrl+1 ... Ctrl+9  -> 全局直接切换到对应 Tab
Ctrl+T，再按 1-9   -> 进入 Tab 场景后切换到对应 Tab
```

`[1...9]` 只在配置加载时展开；运行时仍然处理具体的 `1` 到 `9`。`{key}` 会替换成当前数字，例如 `Ctrl+3` 生成 `tab.jump.3`。

### 增强键盘协议

传统 TTY 输入无法可靠区分 `Ctrl+1` 和普通数字。`tui` 启动时会启用 Kitty keyboard protocol 的 disambiguate 模式，并查询宿主 terminal emulator 是否确认支持。只有 capability 确认后，`Ctrl+数字` 才会进入 footer/Help 的可用快捷键提示；实际执行以 TerminalHost 是否收到可解析的 CSI-u 输入为准。

在 iTerm2 中，对应 Profile 必须允许应用改变按键报告方式。若 iTerm2 禁止应用切换 keyboard reporting mode，或者当前 terminal emulator 不支持 Kitty keyboard protocol，`Ctrl+1...9` 不会产生可区分的输入事件。

此时仍可使用稳定 fallback：

```text
Ctrl+T，然后按 1-9
```

修改增强键盘相关配置或 iTerm2 Profile 设置后，需要彻底退出并重新启动 `muxvia`，因为协议启用和 capability 查询发生在 TerminalHost 启动阶段。

如果 `Ctrl+数字` 没有反应，可用输入诊断启动：

```bash
MUXVIA_TUI_DIAG=1 \
MUXVIA_TUI_INPUT_TRACE=1 \
MUXVIA_LOG_FILE=/tmp/muxvia.log \
muxvia
```

更完整的 action、scene、范围表达式和展示规则见 [`tui/docs/shortcut-system-plan.md`](tui/docs/shortcut-system-plan.md)。

## 适合的使用场景

`muxvia` 主要面向高强度 terminal 用户：

- 同时维护多个项目、服务、测试环境或 long-running jobs。
- 经常在本机和多台远端机器之间切换。
- 使用 Claude Code、Codex、Gemini CLI、OpenCode 等 agent terminal，且需要长期保留输出现场。
- 需要在程序异常退出后保留 panel、历史输出和重启上下文。
- 希望从 TUI 或 mobile app 接回同一个 terminal pool。

如果你的工作流只有一两个短生命周期 shell，传统 terminal emulator 或 tmux 已经足够。`muxvia` 的优势主要在 terminal 数量变多、生命周期变长、入口变多之后显现。

## 架构边界

```text
PTY bytes / resize
        |
        v
vterm semantic interpretation
        |
        v
core daemon
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
client/runtime
  - Endpoint/Route planner
  - local / SSH / managed attempts
  - ReadySession / generation
        |
        v
tui / clients/mobile / other public clients
```

几个硬边界：

- `core` 拥有 terminal lifecycle 和 history truth。
- `tui` 只消费 core 的 live surface、terminal metadata 和 authoritative history window。
- workbench storage 只保存布局和连接意图，不保存 terminal running/exited truth。
- `TerminalID` 只在单个 daemon/endpoint 内唯一；跨 endpoint 必须使用 `TerminalRef`。
- endpoint label、transport address、SSH host key、hub device fingerprint 和 grant ref 不能互相替代。
- SSH transport 只连接远端 `muxvia` daemon，不隐式退化成普通 shell。

## 仓库结构

- 目录 ownership 和依赖方向以 [`docs/development/repository-layout.md`](docs/development/repository-layout.md) 为准；下面只列主要入口。
- `cmd/muxvia/`：`muxvia` 命令行与 composition root，不实现连接 runtime。
- `client/endpoint/`：Endpoint/Route registry、assembler、planner 与 portable contract。
- `client/runtime/`：跨端 route race、ReadySession、generation 与 session owner。
- `client/port/`、`client/adapter/`：平台能力接口和 local/SSH/managed/protocol adapter。
- `core/`：core daemon 主线，负责 terminal lifecycle、history、live surface、storage 和 protocol 服务端能力。
- `tui/`：当前 TUI 主线，负责 UI state、reducer/effect、AppRuntime、TerminalHost、render、copy/history 投影和交互。
- `vterm/`：终端语义解释来源，把 PTY bytes 解释成 terminal 语义事件或 transaction。
- `shared/`：迁移期尚未归位的 transport、remote auth、Cloud Companion 与 infrastructure primitive；不得新增领域 owner。
- `internal/protocol/`、`proto/`：daemon/client wire contract 与协议类型。
- `remote/`：公开 WebRTC client/daemon orchestration、DataChannel 授权与 fake harness。
- `clients/ui/`：App 与浏览器客户端共享的公开 UI、状态编排和平台中立 runtime interface。
- `clients/mobile/`：Android App 壳、native bridge 和 Community managed-cloud fail-closed 实现。
- `private/cloud/`：闭源 Companion、Control Plane、Hub、Relay、Route Planner、Web Controller 与 Official App source set；保持独立部署 module。
- `private/archive/`：只读历史资产，不进入 workspace、构建或 runtime fallback。
- `testkit/`：测试辅助能力。
- `fixtures/`、`scripts/`、`Makefile`、`go.work`：测试、生成、发布审计和 workspace 支撑。
- `docs/development/`：当前维护与目录架构入口；`docs/history/`：只读已完成计划/审计记录。

托管 Control Plane、Hub、Relay、计费和官方 Cloud Companion 是独立交付能力，不属于公开源码构建依赖。Community CLI、TUI、daemon、App 以及 local/SSH runtime 在没有这些服务时仍可独立构建和使用。

## 实现状态

当前公开客户端基线包括：

- `EndpointID` / `TerminalRef` 状态模型。
- connection registry 基础结构。
- Terminal picker / Terminal Pool endpoint 聚合。
- live/input/resize/owner/copy/history endpoint 隔离。
- endpoint-aware workbench binding。
- local unix socket 标准 transport。
- SSH transport 连接远端 `muxvia` daemon。
- terminal name identity 第一阶段。
- managed WebRTC identity、capability、signaling 和 Relay contract。
- TUI/CLI hub endpoint dialer 与 Android App managed endpoint adapter。
- signed Cloud Companion 安装和 versioned local IPC public contract。

## 构建与测试

首次运行先安装根 npm workspace，并确保 Go、Node.js、Java 21、Android SDK、`protoc`、`protoc-gen-go` 和 `apkanalyzer` 可用：

```bash
npm ci
make doctor
```

日常维护只使用根 canonical 入口：

```bash
make build
make test
make test-private
make test-clients
make test-android
make test-all
make clean
```

仓库级产物写入 `.artifacts/`。命令语义、测试矩阵、生成代码和 Android 单一源码说明见 [`docs/development/README.md`](docs/development/README.md)。

完整公开快照和许可证准入见 [`docs/remote-platform/public-snapshot-manifest.md`](docs/remote-platform/public-snapshot-manifest.md)。

## 许可证与贡献

适用许可证以仓库根 `LICENSE` 为准。正式 public snapshot 使用 Apache License 2.0，并携带 `NOTICE`、`THIRD_PARTY_NOTICES.md` 和 artifact-specific notice；托管服务与官方闭源 cloud 交付物不因公开客户端许可证而自动获得相同授权。

公开贡献流程见 `CONTRIBUTING.md` 和 `DCO`。提交不得把 TUI/App 变成 terminal lifecycle 或 history truth owner，也不得引入旧 core、旧 TUI、legacy remote fallback 或私有服务构建依赖。
