# TUI 快捷键系统收敛计划

## 目标

本计划定义 `termx-tui-v3` 后续快捷键系统的唯一目标形态：所有键盘入口、sticky 场景按钮、footer/help/overlay 提示都从同一份 `tui.shortcuts` 配置解析出的 shortcut catalog 生成，并通过同一种 action invocation 进入 app action registry。

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
- `shortcuts.system` 定义 `Ctrl-G` 进入后的系统控制场景，避免和 root/global 直接快捷键混用。
- `shortcuts.panel`、`shortcuts.floating`、`shortcuts.tab`、`shortcuts.workspace`、`shortcuts.resize`、`shortcuts.copy` 定义对应场景内可按的键。
- overlay 场景也使用同一结构，内置场景名为 `terminal_picker`、`terminal_pool`、`workbench_tree`、`clipboard_history`、`floating_overview`、`prompt`、`help`。
- `menu.<scene>` 只能进入 domain registry 声明为可进入的内置场景，例如 `ctrl-p: menu.panel`；当前分支不开放用户自定义 scene，未来扩展由插件分支单独设计。
- 用户删除某个 shortcut 后，该按键不能触发，对应提示也不能展示。
- 用户完全没有写 `tui.shortcuts` 时使用内置默认 catalog。
- 只配置 `shortcuts.actions` 时继承默认按键，只覆盖 action 文案。
- 一旦配置任意 scene，该 scene catalog 集合就是完整按键真值，不再继承未配置的默认 scene；parser 必须保留 scene 是否显式出现，显式空 scene catalog 不回退默认。
- `shortcuts: {}` 等价于没有配置 shortcuts，继续使用默认 catalog；手写 parser 如果暂不支持 inline empty map，必须明确报错而不是误判为空用户 catalog。
- 空的 `shortcuts:` 是 YAML null，配置加载直接报错，避免把 null 和空 map 混成一个语义。
- `shortcuts.global: {}` 或其他显式空 scene 表示用户已经声明 scene catalog，整个用户 scene catalog 可以为零且不继承默认 bindings；parser 必须显式记录 scene 是否出现，不能通过 map 长度推断。

## 统一 Action Invocation

shortcut binding 命中后必须保留原始 action id 和参数，不能在 input 层提前压成 command、shell action 或 render action：

```text
InputEvent + scene
  -> ShortcutBinding
  -> ActionInvocation{id, params}
  -> app Action Dispatcher
  -> reducer/effect
```

- input 只负责 key canonicalization、scene lookup 和 invocation 生成。
- 不依赖 `app/render/config` 的 shortcut domain registry 是 action id、参数 schema、允许 scene、默认文案和展示策略的唯一 owner。
- app action dispatcher 引用同一 domain spec，只拥有 reducer/effect handler、组合步骤和失败语义；config 不得反向依赖 app。
- 参数化配置引用必须 canonicalize：`tab.jump.3` 解析为 `ActionInvocation{ID: "tab.jump", Params: {index: 3}}`，`floating.summon.3` 同理；domain registry 只注册基础 action id 和参数 schema。原始配置字符串只可作为 `SourceActionID` 用于错误定位，不参与执行身份。
- renderer 只消费 view-model 中的 invocation 和展示元数据，不反向成为键盘 action 的执行依赖。
- 键盘和鼠标点击必须产生相同 invocation；`tab.jump.3`、`floating.summon.3` 等参数化 action 不得在展示压缩时丢失参数。
- 合并多个不同 invocation 的提示只能作为不可点击 hint，或者为每个 invocation 保留独立点击目标；不得用一个固定点击动作冒充一组方向或编号动作。
- 组合 action 由 app registry 定义有序步骤和失败条件；配置只引用 action id，不允许声明脚本或任意命令。

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
- app action dispatcher 负责把 invocation 翻译成 reducer message、pane command、workbench command 或 effect。
- terminal lifecycle、kill、restart、input、resize 仍必须经过 owning endpoint daemon 或当前 reducer 权威链路，不能写入 workbench storage。

## 校验规则

- `shortcuts` 下未知顶层块报错，避免静默丢配置。
- 同一场景内同一个键只能绑定一个 action。
- 编译到同一输入场景的 scene 别名不能绑定同一个运行时 key，例如 `panel.x` 和 `pane.x` 不能同时存在。
- action id 必须符合命名规则。
- action id 必须存在于当前内置 action registry；本分支不接受未知 action 静默空转。
- action 必须允许出现在目标 scene；overlay 不能引用其他 overlay 的领域 action。
- 参数化 action 必须在加载期验证参数范围；`tab.jump.N` 和 `floating.summon.N` 当前只接受 `1..9`。
- routed 和 overlay scene 都必须使用同一 canonical key 签名检查冲突；`ctrl-a`/`ctrl-A`、`esc`/`escape` 等等价输入不能重复绑定。
- 配置期验证 key token 的语法和协议理论支持能力；启动期由 TerminalHost 检测实际 capability；运行期按 capability 激活 binding 并投影 available/unavailable 状态。
- 同一份配置不因启动环境不同而解析失败；当前宿主不支持的增强键位不能触发，footer/help 必须隐藏或明确标记 unavailable。只有同一 invocation 另有 available binding 时才展示替代键，不自动生成用户未配置的 fallback；默认 catalog 自身保留 `Ctrl-T` 后按数字的路径。
- label 必须是单行字符串。
- `menu.xxx` 必须指向 shortcut domain registry 声明为可进入的内置场景。
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
- 旧 `render.ActionSpecCatalog` 必须删除，或改成从 shortcut domain spec 派生的纯渲染投影；它只能补充图标、颜色和布局优先级，不得重新声明 action 存在性、默认 label、allowed scenes、参数规则、footer/help visibility、click policy 或执行映射，且完备性 harness 必须验证投影一一对应。

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

### KS005：domain spec、invocation 与 dispatcher contract

- 先补 action spec/invocation 完备性 harness。
- 建立底层 shortcut domain registry；展示策略明确为 footer/help visible 或 hidden、click clickable 或 hint-only。
- input 命中 binding 后保留原始 action id 和参数；app 建立 dispatcher handler contract。
- 本切片不修改 footer 点击、不删除 Esc fallback、不实现 terminal lifecycle 组合动作。

### KS006：键盘与点击等价

- 先补键盘与点击产生相同 invocation 的 harness。
- footer/content/overlay 点击携带完整 invocation；方向和参数化动作不得压成无参数 render action。
- 聚合多个不同 invocation 时只能 hint-only，不能生成歧义 hit region。

### KS007：删除 fallback 与提示完备性

- 先补显式空 catalog 下输入、footer、help、overlay 都无 fallback 的 harness。
- sticky/copy Esc、空 footer global、Help close 和 Prompt suggestion 键位全部回到 catalog。
- help 的普通说明可静态存在，任何键位、按钮和 hit region 必须来自 catalog。

### KS008：配置校验

- 先补 canonical key、scene-action 和参数范围矩阵。
- config 只消费 KS005 domain spec，不维护第二份 action 表。
- routed/overlay 使用同一 key 签名；`menu.<scene>` 只允许可进入的内置 scene。

### KS009：配置替换语义

- 先覆盖无 shortcuts、action-only、单 scene、显式空 scene、actions+scenes 和空 catalog。
- parser 显式记录 scene 是否声明，不能用 map 是否为空推断。
- action-only 继承默认 bindings；任意 scene 出现后不继承默认 scene。

### KS010：组合 action

- 先补成功、失败和 owning endpoint `TerminalRef` 路由 harness。
- `panel.kill` 和 `panel.kill_and_close` 使用独立 handler；明确 kill 请求、关闭 panel 的顺序和失败条件。
- 本切片不调整 registry 基础结构，不开放脚本或任意命令。

KS005-KS010 每个切片都运行 `cd termx-tui-v3 && go test ./... -count=1`、`git diff --check`，提交前使用子 Agent 只读审核。

### KS011：增强键盘协议

- 采用终端生态支持的增强键盘协议前先由 harness 固化字节格式；TerminalHost 生命周期 owner 负责启用、正常退出和错误清理时恢复模式。
- 配置期只验证协议理论支持；启动期检测实际 capability；运行期决定 binding 是否 available。
- raw TTY parser 必须真实产生 `Ctrl+1..9` 等事件后，文档和默认配置才允许声明可用。
- 不支持增强协议时保留 sticky tab 场景数字跳转，不伪造 root `Ctrl+数字` 能力。

### KS012-KS013：契约与文档收尾

- 汇总 raw bytes -> InputEvent -> invocation -> reducer 的跨切片端到端守卫；关键 harness 必须已在各实现切片先行建立。
- 汇总默认 catalog 完备性、键盘/点击等价、空 catalog、overlay 和组合 action 契约测试。
- 更新快捷键统计、可加载示例和支持键位说明；所有阶段提交前继续使用子 Agent 审核。
