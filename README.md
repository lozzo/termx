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
- **多 endpoint 是一等能力**：本地 daemon、SSH 远端 daemon 和 managed WebRTC 设备都通过 `EndpointID + TerminalID` 的 `TerminalRef` 隔离路由。
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
- managed WebRTC endpoint 已通过公开 Cloud Companion contract 接入 TUI；Companion 缺失时只让对应 endpoint unavailable，不影响 local/SSH。
- Android App 已删除旧 Hub/session-token Connector，使用同一 endpoint/relay/error fixture；Community build 对官方 cloud 明确 fail closed，Official build 已通过固定私有 source set 装配同一公开 contract。
- `termx cloud` 已提供 signed install/update、login/enroll、status/doctor、logout 和 uninstall；源码构建不含官方 release root 时 managed cloud 明确不可用，不影响 local/SSH。
- TUI 与 Android App 共用 endpoint、配对、credential reference 和稳定错误语义；平台层只实现各自的 WebRTC primitive。

## 快捷键配置与 Ctrl+数字

TUI 快捷键配置文件优先使用：

```text
$XDG_CONFIG_HOME/termx/tui-v3.yaml
```

未设置 `XDG_CONFIG_HOME` 时，默认路径是 `~/.config/termx/tui-v3.yaml`。

`tui.shortcuts` 是按键执行、footer 提示和 Help 展示的共同来源。同一个 action 可以绑定多个按键；每个 binding 可以通过 `show` 单独控制是否进入 footer。

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

修改增强键盘相关配置或 iTerm2 Profile 设置后，需要彻底退出并重新启动 `termx`，因为协议启用和 capability 查询发生在 TerminalHost 启动阶段。

如果 `Ctrl+数字` 没有反应，可用输入诊断启动：

```bash
TERMX_TUI_DIAG=1 \
TERMX_TUI_INPUT_TRACE=1 \
TERMX_LOG_FILE=/tmp/termx.log \
termx
```

更完整的 action、scene、范围表达式和展示规则见 [`tui/docs/shortcut-system-plan.md`](tui/docs/shortcut-system-plan.md)。

## 适合的使用场景

`termx` 主要面向高强度 terminal 用户：

- 同时维护多个项目、服务、测试环境或 long-running jobs。
- 经常在本机和多台远端机器之间切换。
- 使用 Claude Code、Codex、Gemini CLI、OpenCode 等 agent terminal，且需要长期保留输出现场。
- 需要在程序异常退出后保留 panel、历史输出和重启上下文。
- 希望从 TUI 或 mobile app 接回同一个 terminal pool。

如果你的工作流只有一两个短生命周期 shell，传统 terminal emulator 或 tmux 已经足够。`termx` 的优势主要在 terminal 数量变多、生命周期变长、入口变多之后显现。

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
EndpointManager
  - local unix socket
  - SSH daemon endpoint
  - managed WebRTC endpoint
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
- SSH transport 只连接远端 `termx` daemon，不隐式退化成普通 shell。

## 仓库结构

- `cmd/termx/`：`termx` 命令行入口，组装 core-v2 与 tui-v3。
- `core/`：core daemon 主线，负责 terminal lifecycle、history、live surface、storage 和 protocol 服务端能力。
- `tui/`：当前 TUI 主线，负责 AppRuntime、TerminalHost、EndpointManager、state、render、copy/history 投影和交互。
- `vterm/`：终端语义解释来源，把 PTY bytes 解释成 terminal 语义事件或 transaction。
- `shared/`：共享 connection registry、transport、remote auth 和 Cloud Companion contract。
- `internal/protocol/`、`proto/`：daemon/client wire contract 与协议类型。
- `remote/`：公开 WebRTC client/daemon orchestration、DataChannel 授权与 fake harness。
- `clients/ui/`：App 与浏览器客户端共享的公开 UI、状态编排和平台中立 runtime interface。
- `clients/mobile/`：Android App 壳、native bridge 和 Community managed-cloud fail-closed 实现。
- `testkit/`：测试辅助能力。
- `fixtures/`、`scripts/`、`Makefile`、`go.work`：测试、发布审计和 workspace 支撑。

托管 Control Plane、Hub、Relay、计费和官方 Cloud Companion 是独立交付能力，不属于公开源码构建依赖。Community CLI、TUI、daemon、App 以及 local/SSH runtime 在没有这些服务时仍可独立构建和使用。

## 实现状态

当前公开客户端基线包括：

- `EndpointID` / `TerminalRef` 状态模型。
- connection registry 基础结构。
- Terminal picker / Terminal Pool endpoint 聚合。
- live/input/resize/owner/copy/history endpoint 隔离。
- endpoint-aware workbench binding。
- local unix socket 标准 transport。
- SSH transport 连接远端 `termx` daemon。
- terminal name identity 第一阶段。
- managed WebRTC identity、capability、signaling 和 Relay contract。
- TUI/CLI hub endpoint dialer 与 Android App managed endpoint adapter。
- signed Cloud Companion 安装和 versioned local IPC public contract。

## 构建与测试

常用命令：

```bash
go test ./cmd/termx -count=1
go build ./cmd/termx
```

按模块运行测试：

```bash
go test ./tui/... -count=1
go test ./core/... -count=1
go test ./shared/... -count=1
go test ./internal/protocol/... -count=1
```

共享 UI 与 App：

```bash
npm ci
npm run proto && npm test && npm run typecheck && npm run build
npm run cap:sync
export ANDROID_HOME=/absolute/path/to/android-sdk
cd clients/mobile/android && ./gradlew testDebugUnitTest assembleDebug
```

完整公开快照和许可证准入见 [`docs/remote-platform/public-snapshot-manifest.md`](docs/remote-platform/public-snapshot-manifest.md)。

## 许可证与贡献

适用许可证以仓库根 `LICENSE` 为准。正式 public snapshot 使用 Apache License 2.0，并携带 `NOTICE`、`THIRD_PARTY_NOTICES.md` 和 artifact-specific notice；托管服务与官方闭源 cloud 交付物不因公开客户端许可证而自动获得相同授权。

公开贡献流程见 `CONTRIBUTING.md` 和 `DCO`。提交不得把 TUI/App 变成 terminal lifecycle 或 history truth owner，也不得引入旧 core、旧 TUI、legacy remote fallback 或私有服务构建依赖。
