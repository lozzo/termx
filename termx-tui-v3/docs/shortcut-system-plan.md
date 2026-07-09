# TUI 快捷键系统收敛计划

## 目标

本计划定义 `termx-tui-v3` 后续快捷键系统的唯一目标形态：所有键盘入口、sticky 场景按钮、footer/help/overlay 提示都从同一份 `tui.shortcuts` 配置解析出的 shortcut catalog 生成。

当前分支仍处于开发阶段，本次改造不保留旧 `tui.keymap` 兼容，不做 deprecated 双路径，不用 fallback 掩盖旧行为。旧硬编码只允许作为默认 catalog 的迁移来源，迁移完成后不得继续作为第二份运行时真值。

## 非目标

- 不新增插件系统代码、协议或插件文档。
- 不把快捷键配置扩展成脚本系统。
- 不让 renderer、service 或 storage 直接读取配置文件。
- 不改变 daemon terminal lifecycle、history truth 或 endpoint routing 语义。

## 单一配置入口

快捷键配置只允许挂在 `tui.shortcuts` 下：

```yaml
tui:
  shortcuts:
    actions:
      panel.close:
        label: close
      panel.kill_and_close:
        label: kill+close

    global:
      ctrl-p: menu.panel
      ctrl-o: menu.floating
      ctrl-t: menu.tab
      ctrl-1: tab.jump.1

    panel:
      x: panel.close
      k:
        action: panel.kill_and_close
        label: kill+close

    floating:
      n: floating.new
      o: floating.overview
      x: floating.close
```

语义：

- `shortcuts.actions` 定义 action 默认展示文案。
- `shortcuts.global` 定义默认工作台输入态直接可按的键。
- `shortcuts.panel`、`shortcuts.floating`、`shortcuts.tab`、`shortcuts.workspace`、`shortcuts.resize`、`shortcuts.copy` 定义对应场景内可按的键。
- overlay 场景也使用同一结构，内置场景名为 `terminal_picker`、`terminal_pool`、`workbench_tree`、`clipboard_history`、`floating_overview`、`prompt`、`help`。
- `menu.<scene>` 表示进入某个快捷键场景，例如 `ctrl-p: menu.panel`。
- 用户删除某个 shortcut 后，该按键不能触发，对应提示也不能展示。

## 写法

短写适合绝大多数配置：

```yaml
panel:
  x: panel.close
```

长写用于覆盖单个场景内的展示文案：

```yaml
panel:
  k:
    action: panel.kill_and_close
    label: kill+close
```

文案优先级：

1. 场景按键长写的 `label`。
2. `shortcuts.actions.<action>.label`。
3. 内置 action registry 的默认 label。
4. 从 action id 派生的可读文案，例如 `panel.split_vertical` 派生为 `split vertical`。

同一个 action 可以被多个键触发：

```yaml
global:
  ctrl-1: tab.jump.1

tab:
  "1": tab.jump.1
```

## Action ID 命名

action id 只允许字母、数字、`_`、`-`、`.`。默认内置 action 使用小写命名。

命名约定：

- `menu.panel`：进入快捷键场景。
- `panel.close`：panel/pane 领域动作。
- `floating.new`：floating 领域动作。
- `tab.jump.1`：带参数的固定动作。
- `terminal.kill`：terminal lifecycle 相关请求，执行时必须路由到 owning endpoint daemon。
- `panel.kill_and_close`：本地组合动作，执行顺序由 action registry 定义。

组合动作仍然是 action registry 的职责，配置文件只引用 action id，不直接声明执行脚本。

## 领域边界

- config loader 负责把 `tui.shortcuts` 解析为 `state.TUIShortcutConfig`，并在加载期完成基础校验。
- shortcut catalog 是 TUI 客户端侧快捷键真值，派生输入 router、footer token、help 内容和 overlay/menu 提示。
- input router 只消费已解析 catalog，不直接读取配置文件。
- renderer 只消费 view-model，不直接读配置、runtime service 或 protocol client。
- app reducer/action registry 负责把 action id 翻译成 reducer message、pane command、workbench command 或 shell action。
- terminal lifecycle、kill、restart、input、resize 仍必须经过 owning endpoint daemon 或当前 reducer 权威链路，不能写入 workbench storage。

## 校验规则

- `shortcuts` 下未知顶层块报错，避免静默丢配置。
- 同一场景内同一个键只能绑定一个 action。
- action id 必须符合命名规则。
- label 必须是单行字符串。
- `menu.xxx` 必须指向内置场景或已配置场景。
- 默认 catalog 必须能覆盖当前实际可按行为；用户提供 `tui.shortcuts` 后，以用户配置为唯一真值。

## 旧系统删除策略

KS002 起删除旧 `tui.keymap`：

- 删除 `state.TUIKeymapConfig` 及 `TUIConfigStore.Keymap`。
- 删除 `config.Default()` 中的 keymap 默认值。
- 删除 `config.Parse` / `Validate` 中 `tui.keymap.*` 解析和冲突检测。
- 删除或替换示例文档里的 `tui.keymap`。
- 删除或替换 keymap parser/validation 测试。
- 不接受旧 `tui.keymap`，不转换、不警告后兼容、不作为 fallback。

KS004 起清理旧提示真值：

- footer 不再维护独立 action key 列表。
- help 不再维护独立快捷键文案列表。
- overlay/menu 提示不再单独写硬编码键位。
- `render.ActionSpecCatalog` 可以保留 action 元数据，但快捷键和是否展示必须来自 shortcut catalog。

## 阶段

### KS001：盘点与设计

- 新增 `shortcut-inventory.md`。
- 新增本计划文档。
- 更新 `workflow.md` 切片。
- 只运行 `git diff --check`。
- 提交前使用子 Agent 做只读审核。

### KS002：配置模型

- 新增 `TUIShortcutConfig`。
- 支持 `actions.<action>.label`。
- 支持场景短写 `key: action`。
- 支持场景长写 `key.action` / `key.label`。
- 删除旧 `tui.keymap`。
- 运行 `cd termx-tui-v3 && go test ./... -count=1` 和 `git diff --check`。
- 提交前使用子 Agent 做只读审核。

### KS003：输入路由

- 建立内置 action registry。
- 从 shortcut catalog 生成输入路由。
- 默认 catalog 行为与当前实际行为一致。
- 自定义 shortcuts 是唯一输入真值。
- 支持同一 action 多个按键触发。
- 运行准入测试并做子 Agent 只读审核。

### KS004：提示同源

- footer 从当前场景 shortcuts 生成。
- help 从同一 catalog 生成。
- overlay/menu 提示从同一 catalog 生成。
- 修复提示和真实按键不一致。
- 用户删除 shortcut 后提示自动消失。
- 运行准入测试并做子 Agent 只读审核。

