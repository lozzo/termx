# termx-tui-v3 配置管理设计

状态：设计基准
日期：2026-06-19

## 1. 目标

TUI 配置只表达用户对当前 TUI 客户端的偏好，例如主题、主色、次色、chrome 展示、交互阈值和快捷键覆盖。

它不表达 workbench truth、terminal lifecycle、history truth 或 pane 的业务状态。workspace/tab/pane/floating 的布局仍归 core 托管的 workbench storage；terminal 是否 running/exited、退出码、命令和历史内容仍归 core-v2。

本设计先定义配置项和消费边界。实现时不得复用 `tuiv2/shared` 作为 v3 运行时依赖，也不做旧配置迁移兼容。

## 2. 文件和加载优先级

v3 使用独立配置文件，避免旧 `termx.yaml` 被 tuiv2 的简化 parser 误读：

```text
$XDG_CONFIG_HOME/termx/tui-v3.yaml
~/.config/termx/tui-v3.yaml
```

实现入口优先级：

1. 代码内置默认值。
2. 宿主终端 theme/palette probe，生成 host-aware 默认主题。
3. 用户配置文件 `tui-v3.yaml`。
4. 环境变量覆盖，例如 `TERMX_TUI_THEME_PRIMARY`。
5. CLI 会话级覆盖，例如后续的 `--tui-theme-primary`。

解析失败规则：

- 启动读取失败或 schema 错误：不进入 TUI，向用户输出明确错误。
- 运行中 reload 失败：保留上一份已应用配置，并用 toast 展示错误。
- 未知字段视为错误，避免用户拼错配置后静默无效。

host theme probe 是动态事件。宿主颜色变化时，reducer 重新计算 resolved theme；但用户显式配置过的 token 不会被 host palette 覆盖。

## 3. 数据流

```text
config file / env / flags
  |
  v
ConfigLoader
  |
  v
TUIConfigSnapshot
  |
  v
reducer-owned StateRoot.Config
  |
  +--> ResolveTheme(Config.Theme, StateRoot.HostTheme)
  |
  +--> ResolveKeymap(Config.Keymap)
  |
  v
RenderVMBuilder / input router / app timers
  |
  v
Renderer / TerminalHost
```

边界要求：

- `ConfigLoader` 可以读文件、env 和 CLI flag，但不能改 `StateRoot`。
- reducer 持有已验证的 `TUIConfigSnapshot`，配置变更必须通过 message/effect 回到主循环。
- `HostThemeStore` 只表达宿主探测结果，不是用户配置。
- renderer 只消费 `ResolvedTheme` 和 view-model，不读文件、env、CLI flag、core client 或 service。
- input router 只消费 `ResolvedKeymap`，不读配置文件。
- terminal live 内容的 ANSI 颜色直通，不被 TUI theme 重映射。

## 4. 初始 schema

```yaml
version: 1

tui:
  profile: default

  theme:
    mode: dark              # dark | light | system
    palette: host           # host | builtin
    primary: "#d65cff"      # 主色：active、focus、主要快捷键 token
    secondary: "#66e3ff"    # 次色：次级强调、信息型标签

    foreground: ""          # 留空表示沿用 host-aware 默认值
    background: ""
    muted: ""
    success: ""
    warning: ""
    danger: ""
    info: ""

    border:
      panel: ""
      active: ""
      inactive: ""
      muted: ""

    surface:
      chrome_bg: ""
      status_bg: ""
      overlay_bg: ""
      toast_bg: ""

  chrome:
    header: true
    footer: true
    panel_presentation: split-line  # split-line | card
    tab_create_icon: "󰐕"

  interaction:
    mouse: true
    sticky_prefix_timeout_ms: 3000
    confirm_destructive: true

    clipboard_history:
      max_items: 200
      name_width: 34
      preview_width_ratio: 0.68

    picker:
      fuzzy_match: subsequence
      highlight_matches: true

  keymap:
    root:
      terminal_picker: ctrl-f
      copy_mode: ctrl-v
      tab_mode: ctrl-t
      workspace_mode: ctrl-w
      floating_mode: ctrl-o
      pane_mode: ctrl-p
      resize_mode: ctrl-r
      global_mode: ctrl-g

    copy:
      clipboard_history: h
      paste_latest: p
      paste_system: shift-p

    tab:
      create: c
      close: x
      rename: r
      next: n
      previous: p

    workspace:
      navigator: w
      create: c
      delete: x
      rename: r
```

## 5. 配置项定义

| 字段 | 默认值 | 归属 | 说明 |
| --- | --- | --- | --- |
| `version` | `1` | loader | schema 版本。实现只接受当前版本，不做旧版本迁移。 |
| `tui.profile` | `default` | loader | 预留多 profile；第一阶段只解析当前 profile 名称，不做 profile merge。 |
| `theme.mode` | `dark` | theme resolver | 选择内置亮暗基线；`system` 第一阶段等同 host-aware dark，后续可接宿主检测。 |
| `theme.palette` | `host` | theme resolver | `host` 表示先用宿主 foreground/background/palette 推导默认 token；`builtin` 表示完全使用内置默认值再应用用户覆盖。 |
| `theme.primary` | 当前 `DefaultTheme().Accent` | theme resolver | 主色。用于 active pane border、选中标记、当前 item underline、主要快捷键 token、重要 CTA。 |
| `theme.secondary` | 当前 `DefaultTheme().Info` | theme resolver | 次色。用于次级标签、信息提示、非破坏性辅助动作、preview 中的轻量强调。 |
| `theme.foreground/background` | host-aware 默认值 | theme resolver | TUI chrome 的默认前景/背景，不影响 terminal 内容 ANSI。 |
| `theme.muted` | host palette 8 或内置灰色 | theme resolver | inactive、disabled、弱文本、未选中边框的默认来源。 |
| `theme.success/warning/danger/info` | host palette 2/3/1/4 或内置值 | theme resolver | 状态语义色。显式设置后不再被 host palette 覆盖。 |
| `theme.border.*` | 从 primary/muted 派生 | renderer | panel、active、inactive、muted 边框 token。 |
| `theme.surface.*` | 从 background 派生 | renderer | header/footer/status/overlay/toast 背景 token。 |
| `chrome.header/footer` | `true` | VM builder | 控制 shell header/footer 是否占空间。隐藏时状态必须转入 toast/help，不得丢失关键 mode。 |
| `chrome.panel_presentation` | `split-line` | layout VM | `split-line` 和 `card` 只改变视觉布局，不改变 pane id、terminal binding 或 copy mode 绑定。 |
| `chrome.tab_create_icon` | `󰐕` | header renderer | tab create 按钮图标。必须按 cell width 校验，不能挤压 tab hit region。 |
| `interaction.mouse` | `true` | TerminalHost/input | 控制宿主鼠标模式和 hit region 消费。 |
| `interaction.sticky_prefix_timeout_ms` | `3000` | app timer | sticky shortcut mode 空闲退出时间。overlay/copy 这类显式页面不受它影响。 |
| `interaction.confirm_destructive` | `true` | reducer/app | close/delete/kill 这类破坏性动作是否需要确认或二次意图。 |
| `clipboard_history.*` | 见 schema | overlay VM | 只控制 TUI 展示和条目数量上限；历史内容本身仍由 core 托管的 clipboard storage 保存。 |
| `picker.fuzzy_match` | `subsequence` | picker reducer | 默认沿用 data picker 式子序列匹配，例如 `gft` 可命中 `git commit fix terminal`。 |
| `keymap.*` | 内置键位 | input router | 快捷键覆盖。冲突在加载期报错，不能运行时让两个 action 抢同一按键。 |

## 6. Theme resolution

最终 `ResolvedTheme` 的计算顺序：

1. 从 `render.DefaultTheme()` 取内置 token。
2. 如果 `theme.palette = host`，应用 `HostThemeStore` 推导的 foreground、background、semantic token。
3. 应用用户显式配置的 token。
4. 派生未显式设置的 border/surface token。
5. 调用 fallback，确保每个 renderer 需要的 token 都非空。

第一阶段可以继续把 `render.Theme.Accent` 作为 resolved primary 使用；但配置文档和 loader 应使用 `primary`/`secondary` 命名。后续代码收敛时，可以在 `render.Theme` 中增加 `Primary`/`Secondary` 字段，或把 `Accent` 明确重命名为 `Primary`。

派生规则：

- `ActivePaneBorder = border.active || primary`
- `PanelBorder = border.panel || mix(muted, primary, 0.45)`
- `MutedBorder = border.muted || muted`
- `InactivePane = border.inactive || muted`
- `Info = info || secondary`
- `ChromeBG/StatusBG/OverlayBG/ToastBG = surface.* || mix(background, foreground, ratio)`

颜色格式第一阶段只接受 `#RRGGBB`。不接受命名色、256 色 index 或 alpha，避免不同 terminal 的解释差异。

## 7. 全局使用规则

- 新 UI 代码不得直接写业务颜色常量，必须通过 theme token。
- panel、header、footer、overlay、toast、picker、navigator、clipboard history 都消费同一份 `ResolvedTheme`。
- terminal content renderer 只在 chrome/cursor/selection overlay 使用 TUI token；terminal 输出自己的 cell style 保持原样。
- 搜索命中使用 `Warning` token，选中/focus 使用 `Primary` token，辅助信息使用 `Secondary` 或 `Muted` token。
- 状态颜色使用 `Success/Warning/Danger/Info`，不能用 primary 临时代替。
- snapshot/plain dump harness 应记录 token 解析后的 styled frame，避免只看 Unicode glyph。

## 8. 实现切片建议

后续实现不要一次铺满。建议拆成：

1. `TUIConfigSnapshot`、`ThemeConfig`、`ResolvedTheme` domain model 和 resolver harness。
2. v3 独立 config loader，接入 `tui-v3.yaml`、env 覆盖和 CLI session override。
3. `StateRoot.Config`、`ConfigLoadedMsg`、`ConfigReloadRequestedMsg` 和 reducer。
4. renderer 全局改用 `ResolvedTheme`，清理散落颜色常量。
5. keymap config resolver 和冲突检测。
6. chrome/interaction 配置接入 header/footer、panel presentation、sticky timeout 和 clipboard history overlay。

每个切片都要有 harness：配置解析失败、默认值、host palette + 用户覆盖、颜色派生、keymap 冲突和 renderer token 消费。
