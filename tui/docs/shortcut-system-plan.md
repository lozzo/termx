# TUI 快捷键系统收敛计划

## 目标

本计划定义 `tui` 后续快捷键系统的唯一目标形态：中立 `tui/action` domain 拥有 keyboard、mouse、drag 和内容 CTA 共用的 canonical action identity、invocation 与参数 contract；所有可配置键盘入口、sticky 场景按钮、footer/help/overlay 快捷键提示都从同一份 `tui.shortcuts` 编译 catalog 生成，并通过 canonical invocation 进入 app handler。未修饰 Esc 是 catalog 之外的全局返回导航，由 reducer-owned state 层级决定。

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
      n:
        action: floating.new
        show: true
      "[1...9]":
        action: floating.summon.{key}
        label: summon
        show: true
      o:
        action: floating.overview
        show: true
      x:
        action: floating.close
        show: true
```

语义：

- `shortcuts.actions` 定义快捷键场景的 action label override；未覆盖时使用 `tui/action` 默认语义 label。
- `shortcuts.global` 定义默认工作台输入态直接可按的键。
- `shortcuts.system` 定义 `Ctrl-G` 进入后的系统控制场景，避免和 root/global 直接快捷键混用。
- `shortcuts.panel`、`shortcuts.floating`、`shortcuts.tab`、`shortcuts.workspace`、`shortcuts.resize`、`shortcuts.copy` 定义对应场景内可按的键。
- overlay 场景也使用同一结构，内置场景名为 `terminal_picker`、`terminal_pool`、`workbench_tree`、`clipboard_history`、`floating_overview`、`prompt`、`help`。
- 未修饰 `Esc` 不属于任何 scene，也不能配置。它固定按 prompt suggestion、overlay、当前 view copy/history、sticky interaction 的顺序返回一层；没有可返回层时透传给前台 terminal。
- `menu.<scene>` 只能进入 `tui/shortcut` scene registry 声明为可进入的内置场景，例如 `ctrl-p: menu.panel`；当前分支不开放用户自定义 scene，未来扩展由插件分支单独设计。
- 用户删除某个 shortcut 后，该按键不能触发，对应提示也不能展示。
- binding 的 `show: false` 只隐藏 footer 提示，按键执行和 Help 完整目录仍保留；省略 `show` 时沿用内置 shortcut binding/catalog policy 的 footer 可见性。
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
  -> action.ActionInvocation{id, params}
  -> app Action Dispatcher
  -> reducer/effect
```

- input 只负责 key canonicalization、scene lookup 和 invocation 生成。
- 不依赖 `app/render/config/shortcut` 的中立 `tui/action` domain 是 canonical action id、invocation、参数 schema 和默认语义 label 的唯一 owner；keyboard、mouse、drag 和内容 CTA 都引用它。
- `tui/shortcut` 只拥有可绑定 scene、scene+key -> action、快捷键 label override、footer `show` 和宿主 capability 条件；它不能声明 mouse-only action 或通用 click policy。
- app action dispatcher 引用同一 action spec，只拥有 reducer/effect handler、组合步骤和失败语义；config 不得反向依赖 app。
- 参数化配置引用必须 canonicalize：`tab.jump.3` 解析为 `action.ActionInvocation{ID: "tab.jump", Params: {index: 3}}`，`floating.summon.3` 同理；action domain 只注册基础 action id 和参数 schema。原始配置字符串只可作为 `SourceActionID` 用于错误定位，不参与执行身份。
- renderer 只消费 view-model 中的 invocation 和展示元数据，不反向成为键盘 action 的执行依赖。
- clickable/hint-only 是具体 render view-model projection 的性质：只有携带唯一 invocation 的 projection 才可点击；它不是 action 或 shortcut 的全局属性。
- 键盘和鼠标点击必须产生相同 invocation；`tab.jump.3`、`floating.summon.3` 等参数化 action 不得在展示压缩时丢失参数。
- 合并多个不同 invocation 的提示只能作为不可点击 hint，或者为每个 invocation 保留独立点击目标；不得用一个固定点击动作冒充一组方向或编号动作。
- 组合 action 由 app registry 定义有序步骤和失败条件；配置只引用 action id，不允许声明脚本或任意命令。

## 写法

短写适合绝大多数配置：

```yaml
panel:
  x: panel.close
```

长写用于覆盖单个场景内的展示文案和 footer 可见性：

```yaml
panel:
      k:
        action: panel.kill_and_close
        label: kill+close
        show: true
```

同类数字按键可以在配置加载期展开，运行时 catalog 仍只保存具体按键：

```yaml
floating:
  "[1...9]":
    action: floating.summon.{key}
    label: summon
    show: true

global:
  "ctrl+[1...5]":
    action: tab.jump.{key}
    label: tab
    show: false
```

- 范围只支持升序单数字区间；`[1...9]` 展开为 `1` 到 `9`，`ctrl+[1...5]` canonicalize 为 `ctrl-1` 到 `ctrl-5`。
- `{key}` 只允许出现在范围 binding 的 action 中，替换值是当前展开数字。
- 展开后的每个具体键继续走既有 key canonicalization、scene/action、参数范围和重复绑定校验；范围重叠直接报错。
- `show: true` 表示进入 footer，`show: false` 表示仅从 footer 隐藏；按键和文案在 footer 中必须整体保留或整体隐藏。

文案优先级：

1. 场景按键长写的 `label`。
2. `shortcuts.actions.<action>.label`。
3. 中立 `tui/action` domain 的默认语义 label。
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

- `menu.panel`：中立 action identity 表示进入快捷键场景；可进入的 scene 集合由 `tui/shortcut` 声明。
- `panel.close`：panel/pane 领域动作。
- `floating.new`：floating 领域动作。
- `tab.jump.1`：带参数的固定动作。
- `terminal.kill`：terminal lifecycle 相关请求，执行时必须路由到 owning endpoint daemon。
- `panel.kill_and_close`：本地组合动作，执行顺序和失败语义由 app handler 定义。

组合动作的 identity/参数属于 `tui/action`，有序步骤和失败语义属于 app handler；配置文件只引用 action id，不直接声明执行脚本。

## 领域边界

- config loader 负责把 `tui.shortcuts` 解析为 `state.TUIShortcutConfig`，并在加载期完成基础校验。
- `tui/action` 是全部交互入口共享的 action identity/invocation/参数真值，不拥有按键或 surface 几何。
- shortcut catalog 是 TUI 客户端侧快捷键真值，派生 input router 和各 surface 的快捷键 token/label；非快捷键 mouse/drag/CTA 只引用中立 action invocation，不伪装成 shortcut binding。
- input router 只消费已解析 catalog，不直接读取配置文件。
- renderer 只消费 view-model，不直接读配置、runtime service 或 protocol client；具体 projection 自己决定 geometry、visual metadata 和能否点击，但不得重定义 action identity。
- app action dispatcher 负责把 invocation 翻译成 reducer message、pane command、workbench command 或 effect。
- terminal lifecycle、kill、restart、input、resize 仍必须经过 owning endpoint daemon 或当前 reducer 权威链路，不能写入 workbench storage。

## 校验规则

- `shortcuts` 下未知顶层块报错，避免静默丢配置。
- 同一场景内同一个键只能绑定一个 action。
- 编译到同一输入场景的 scene 别名不能绑定同一个运行时 key，例如 `panel.x` 和 `pane.x` 不能同时存在。
- action id 必须符合命名规则。
- action id 必须存在于当前 `tui/action` registry；本分支不接受未知 action 静默空转。
- action 必须出现在 `tui/shortcut` 的目标 scene allowlist；overlay 不能引用其他 overlay 的领域 action。
- 参数化 action 必须在加载期验证参数范围；`tab.jump.N` 和 `floating.summon.N` 当前只接受 `1..9`。
- routed 和 overlay scene 都必须使用同一 canonical key 签名检查冲突；`ctrl-a`/`ctrl-A`、`enter`/`return` 等等价输入不能重复绑定。
- 未修饰 `esc`/`escape` 是保留的全局返回键，配置加载期直接拒绝；带修饰的 `ctrl-esc`、`alt-esc` 等仍按普通 binding 处理。
- 配置期验证 key token 的语法和协议理论支持能力；启动期由 TerminalHost 检测实际 capability；运行期按 capability 激活 binding 并投影 available/unavailable 状态。
- 同一份配置不因启动环境不同而解析失败；当前宿主不支持的增强键位不能触发，footer/help 必须隐藏或明确标记 unavailable。只有同一 invocation 另有 available binding 时才展示替代键，不自动生成用户未配置的 fallback；默认 catalog 自身保留 `Ctrl-T` 后按数字的路径。
- label 必须是单行字符串。
- `menu.xxx` action 必须存在于 `tui/action`，并指向 `tui/shortcut` 声明为可进入的内置场景。
- 默认 catalog 必须能覆盖当前实际可按行为；用户显式声明任一 shortcut scene 后，以用户 scene catalog 为完整 binding 真值。空 map 和 action-only 配置继续遵循前述默认继承语义。

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
- 旧 `render.ActionSpecCatalog` 必须删除，或改成引用 `tui/action` identity 的纯渲染 metadata；它只能补充图标、颜色、几何和布局优先级，不得重新声明 action 存在性、默认 label、参数规则、允许绑定 scene 或执行映射。具体 view-model projection 只在携带唯一 canonical invocation 时 clickable，完备性 harness 必须验证该约束。

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
- 运行 `go test ./tui/... -count=1` 和 `git diff --check`。
- 提交前使用子 Agent 做只读审核。

### KS003：输入路由

- 建立内置快捷键 action registry；KS013 将其中通用 action identity 迁到中立 `tui/action`，这里只作为已完成历史阶段记录。
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
- 建立 action spec/invocation 与 dispatcher 前身；KS013 负责把通用 identity/参数迁到中立 `tui/action`，KS016 负责最终 shortcut visibility 与具体 render projection clickability。
- input 命中 binding 后保留原始 action id 和参数；app 建立 dispatcher handler contract。
- 本切片不修改 footer 点击、不删除 Esc fallback、不实现 terminal lifecycle 组合动作。

### KS006：键盘与点击等价

- 先补键盘与点击产生相同 invocation 的 harness。
- footer/content/overlay 点击携带完整 invocation；方向和参数化动作不得压成无参数 render action。
- 聚合多个不同 invocation 时只能 hint-only，不能生成歧义 hit region。

### KS007：删除 fallback 与提示完备性

- 先补显式空 catalog 下输入、footer、help、overlay 都无 fallback 的 harness。
- 当时 sticky/copy Esc、Help close 和 Prompt suggestion 键位回到 catalog；KS011D 已用统一返回导航取代这部分历史实现，并删除 `interaction.exit`、`copy.exit`、`prompt.suggestion_exit` 配置 action及 `prompt_suggestion` scene。
- help 的普通说明可静态存在，任何键位、按钮和 hit region 必须来自 catalog。

### KS011D：全局返回导航

- Esc 的 domain owner 是 `Root.CurrentBackNavigationLayer` 与 `NewBackNavigationReducer`，不是 shortcut scene。
- 返回优先级固定为 prompt suggestion、overlay、当前 view copy/history、sticky interaction；每次只退出一层。
- footer 根据同一层级模型自动展示 `Esc BACK`，用户配置为空也不能移除该逃生入口。
- 普通 live terminal 没有可返回层时，Esc 不被 TUI 消费，继续发送给 PTY。
- CSI-u Esc/Enter/Tab/Backspace 必须归一为标准命名 Key；Shift-Tab 归一为 `KeyShiftTab`，Alt 控制键降级到传统 PTY 字节时保留 ESC 前缀。

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

KS005-KS010 每个切片都运行 `go test ./tui/... -count=1`、`git diff --check`，提交前使用子 Agent 只读审核。

### KS011：增强键盘协议

- TerminalHost 是宿主键盘协议 owner：进入 raw/alt-screen 后独立 push Kitty keyboard protocol disambiguate flag，只有 push 成功才记录并在正常退出或后续启动失败时 pop；push 前失败不能 pop 空栈。
- push 后发送 `CSI ? u` 查询，响应 `CSI ? flags u` 经 `HostCapabilityMsg` 写入 reducer-owned `HostCapabilities`；flag 1 才表示宿主确认 disambiguate 生效。
- raw TTY parser 以 CSI-u 字节为 truth，例如 `CSI 49;5 u` 解析为 `InputEvent{Char:"1", Ctrl:true}`；普通 `1` 不得猜测为 Ctrl 输入。
- CSI-u 只负责生成标准 InputEvent，真实动作仍由 shortcut catalog 决定；`ctrl+[1...9] -> tab.jump.{key}` 不新增第二套路由。
- 未命中的 CSI-u 不能把宿主协议 escape sequence 原样发送给 PTY；可降级的 Ctrl 字母、Alt 字符按语义编码，不可由传统 PTY 表达的 Ctrl+数字直接丢弃。
- 当前只启用 disambiguate flag，不启用 report-all-keys 或 key release reporting，普通 UTF-8 文本输入保持不变。
- 不支持 Kitty keyboard protocol、或宿主关闭“允许应用改变键报告模式”时，保留 `Ctrl-T` 后数字的 sticky tab fallback，不把普通数字伪装成 root Ctrl+数字。

### KS012：最终现状审计与总契约守卫

- 重新盘点 raw bytes、`InputEvent`、binding catalog、action domain spec、shortcut bind policy、invocation、app handler、render projection、footer/help/overlay/content CTA 和 mouse hit region。
- 产出机器可读 debt manifest 和守卫：每个默认 binding、action spec、handler、可点击 projection 和展示键位都必须归类为已符合或一个带 owner/来源/目标切片的已知 gap；不允许未分类项，也不允许新增 debt。
- 明确列出仍在声明 action identity、label、scene、click policy 或执行映射的第二真值。KS012 只建立基线和守卫，允许 manifest 中存在将由 KS013-KS016 删除的 gap，不写预期失败测试、不放宽产品断言，也不提前实现后续切片。
- 允许删除已经被新模型替代的测试 helper、旧 action 表和历史兼容路径；删除规模不作为阻止正确架构的理由。

### KS013：单一 action domain 与分发架构

- 新建中立 `tui/action` domain，统一 keyboard、mouse-only、drag、内容 CTA 共用的 canonical action identity、invocation、参数 schema 和默认语义 label。
- `tui/shortcut` 只拥有 scene+key -> action binding、快捷键 label override/show/capability 条件；它引用 action domain，不拥有 mouse-only action 或通用 click policy。
- 删除或降解 `render.ActionSpecCatalog` 等重复语义声明；renderer 只保留几何、图标、颜色和布局优先级 metadata，并引用 action domain ID。KS013 建立 projection contract 和迁移清单，不迁移各 render surface 的提示/点击链路。
- app handler registry 拥有 handler contract、组合步骤和失败语义，不维护可漂移的 alias/scene/display 表；KS013 完成 keyboard invocation -> app handler contract，surface invocation 迁移留给 KS016。
- 禁止为保留旧测试而建立双 registry、桥接 fallback 或字符串互转链。

KS013 实际收口边界：

- `tui/action` 的 `ID`、`Spec`、`Invocation`、`ParamSpec`、默认语义 label 和 mouse/drag/CTA canonical ID 是唯一 identity 真值；该包只能依赖标准库，视觉 projection 名称不得注册为 action。
- `tui/shortcut` 持有 `DefaultBinding`、`BindingPolicy`、唯一内置 scene registry、footer/help binding visibility；`tui/input`、`tui/config` 和 `tui/render` 只能通过 scene API 编译或投影 catalog。
- app 的 `actionHandlerRegistry` 覆盖全部 206 个默认 keyboard binding，只按 canonical `action.ID` 选择 handler；overlay 同样直接进入该 registry，alias、scene、source string 和 render projection 均不参与执行。
- render 使用本地 `ProjectionID`，`ProjectionSpec.CanonicalActionID` 单向引用 canonical action，并已删除 dispatch、固定 footer/help 元数据和 `ShortcutActionRenderID`。所有可执行 footer 与 HitRegion 直接携带 canonical invocation。
- pane/header/content/drag 中仍未携带 invocation 的 producer 继续由 debt manifest 锁定到 KS016；不得在 KS013 用 legacy projection ID fallback 冒充 canonical invocation。

### KS014：输入协议、按键规范化与 scene 状态

- 汇总传统 TTY、Kitty CSI-u、Alt/Ctrl/Shift、命名键、UTF-8、mouse 和不可表达组合的 raw bytes -> `InputEvent` contract。
- root shortcut、sticky scene、copy、overlay、shortcut lock、双击前缀透传和 Esc back navigation 只能按 reducer-owned state 决定优先级。
- 用户 catalog 的无配置、action-only、显式空 scene、完整替换、范围展开和 capability 条件必须共享同一编译结果。
- 未命中按键的 PTY 透传必须保留语义；宿主协议控制序列不得泄漏给 terminal。

KS014 实际输入契约：

Kitty 编码、modifier/event-type 与 PUA functional key 范围以官方 [keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/) 为准；本阶段只启用 disambiguation flag，不启用 alternate/text/release 扩展。

- `tui/terminalhost.InputParser` 是 raw bytes 分帧 owner。普通 UTF-8、传统 control/Alt、CSI/SS3、Kitty CSI-u、SGR mouse、bracketed paste、OSC theme/capability/control 必须先形成完整 `InputEvent`；任意 read chunk 边界不能改变事件。单独 `Esc` 与 `Esc [`/`Esc O`/`Esc ]` 的歧义由 host 25ms 窗口收口，超时后分别提交 Esc 或传统 Alt+char。`CSI 200~...CSI 201~` 必须原子形成 paste semantic event，正文不得逐键经过 shortcut。
- Kitty press/repeat 归一为同一 key 语义；Escape/Enter/Tab/Backspace 使用 C0 codepoint，方向/导航/F1-F12 使用官方 CSI/SS3/tilde 编码，当前 input domain 不建模的 PUA functional key 统一归为 protocol-owned unknown。Super/Hyper/Meta 同样被消费，Caps Lock/Num Lock 位只描述锁定状态，不改变 Ctrl/Alt/Shift 语义。任何分支都不得把原 CSI-u bytes 或 PUA UTF-8 送入 PTY；未知/非法 CSI、SS3、OSC 统一归为 `EventKindHostControl` 并在 runtime ingestion 截断。
- SGR mouse 的 `Row`/`Col` 保留宿主 1-based 坐标，runtime 命中测试时统一转换；零/负坐标、水平滚轮、扩展按钮、wheel release 等当前模型不支持的序列作为 host control 消费，不进入 mouse route 或 PTY。
- shortcut catalog 编译时为传统 TTY 无法产生同一 canonical `InputEvent` 的组合标记 `RequiresKeyboardDisambiguation`。这类 binding 只有来源为 Kitty CSI-u 的规范化事件才能命中；传统 Ctrl 字母/NUL、Alt char、Shift-Tab 和 CSI modified named key 继续按可表达语义工作，`Ctrl-I/J/M/[/?` 等会被传统 parser 规范化成 named key 的组合必须使用增强协议。
- app 输入优先级只读取 reducer-owned state：prompt suggestion > overlay > active-view copy/history > sticky interaction > normal/root > PTY。Prompt scene 把 paste 正文作为一次文本编辑消费，其他 scene 不会把 paste 正文拆成按键。shortcut lock 与双击前缀通过一次性 passthrough token 把同一个 `InputMsg` 交给 terminal router，不异步伪造第二个按键。
- PTY passthrough 不直接转发 Kitty/OSC host protocol。可表达的输入语义重编码为传统 terminal bytes（包括 Ctrl/Alt/control、Shift-Tab、方向/导航键和 F1-F12，F3 固定使用无 CPR 冲突的 `13~`）；paste event 携带纯正文并绕过 shortcut，唯一 terminal router 解析 owning `TerminalRef` 后按该 surface 的 `BracketedPaste` mode 决定发送正文或重新包裹 marker。不可表达的 Ctrl-digit、Ctrl/Shift+Enter/Tab、未建模 PUA functional key 明确返回 `IntentNone`，不能降级成有副作用的普通按键或泄漏宿主协议。

### KS015：全部默认 action 真实功能闭环

- 按 global/system、panel/resize、tab/workspace、floating、copy、terminal picker/pool、workbench tree、clipboard history、prompt/help 场景逐项执行默认 binding。
- 每个默认 action 必须到达真实 reducer/effect/service 边界并产生可观察结果；只有 UI 提示、toast 或 placeholder 而没有真实功能的 action 必须实现完整或从默认 catalog 删除。
- endpoint-aware terminal action 必须保留完整 `TerminalRef`；kill/restart/remove/resize/owner 等失败不能靠关闭 pane、刷新列表或 local fallback 掩盖。
- 组合 action 必须声明事务顺序和部分失败结果，禁止为单 case 叠加判断。

KS015 实际执行契约：

- 206 条默认 binding canonicalize 为 149 个唯一 invocation；测试从 `shortcut.DefaultBindings()` 动态生成闭集，在具备 terminal、9 个 tab、多个 workspace、floating、pool metadata、clipboard 与 frozen history 的可用 fixture 上，经正式 reducer 组合执行同步 effect/message 链直到静止。每个 invocation 必须产生排除 `Generation` 和新增 toast 后的真实状态变化，或抵达明确的 terminal/core/clipboard/workbench storage service owner；endpoint mutation 以 attach/detach/restart/reconnect/kill/remove/edit/tag/input/resize 的完整期望向量和精确 `TerminalRef` 列表验收，重复调用、同 endpoint 错 terminal、额外 mutation 或 fallback 均失败，不能把未执行 effect 或提示当作成功。
- `system.open_prompt` 打开 `action.command` prompt；提交值必须经 `tui/action.ParseInvocation` 校验并回到统一 `ShellShortcutActionMsg` dispatcher。未知 action 或没有 executable handler 的 surface identity 保持 prompt 打开并明确失败，不再以 toast 回显输入冒充执行。
- terminal picker edit 只按选中项的 `TerminalRef` 查找 pool metadata，打开与 terminal manager 共用的 rename prompt，并在提交时保留原 tags；metadata 缺失时 fail closed，禁止用原标题或人工标签伪造 edit。
- `panel.reconnect` 直接为 active pane 的 owning `TerminalRef` 生成 `TerminalPoolReconnectRequestMsg{LocalError:true}`，失败回投目标 view；`panel.restart` 无条件生成 endpoint-aware restart request。只有 exited CTA 保留 restart-if-exited 语义。
- split/attach-tab/attach-float 先创建明确 target slot，再向该 target 发送 endpoint-aware attach request；attach 失败保留该 slot 的局部失败/空连接投影，不关闭其他 pane、不切 local endpoint、不伪造成功。

### KS016：提示、点击与可用性同源

- 逐 surface 把 footer、Help、overlay、header/pane/floating chrome 和内容 CTA 迁移到中立 action domain 的 canonical invocation；这是 render surface 迁移的唯一 owning 切片。
- 所有快捷键 token 和快捷键提示 label 从同一编译 shortcut catalog 投影；header/pane/floating chrome、内容 CTA 等通用 action label 使用 `tui/action` 默认语义 label 或显式 view-model 内容，只有 surface 展示快捷键提示时才应用 shortcut label override。
- 删除 `Ctrl-F`、`Ctrl-T then c`、`R restart` 等会与用户配置漂移的硬编码键位文案；说明文字可以静态存在，但操作提示必须查询 invocation projection。
- keyboard 与 click 对同一 action 生成相同 invocation 和 reducer/effect 链；聚合或条件性 hint 无法确定唯一 invocation 时必须 hint-only。
- 可执行 HitRegion producer 同时声明 canonical Invocation 与 active/explicit TargetMode；row target 另以 HasRow 表达存在性，并在 app 进入 specialized/generic handler 前按当前列表验证边界，禁止缺失、过期或越界目标回退到 active/首末项。
- 窄屏裁剪、增强键盘 capability、显式隐藏、空 catalog、禁用 action 和不可用业务状态都不能留下孤立键位或无效 hit region。

### KS017：配置、文档与真实终端验收

- 更新默认快捷键统计、完整可加载 `tui-v3.yaml` 示例、支持键位/修饰键、替换语义、冲突错误、增强键盘前置条件和诊断方法。
- README、Help、示例和 inventory 必须与运行 catalog 由测试校验；不得手工维护另一份默认键表。
- 新增 `scripts/muxvia_shortcut_smoke.sh`，用隔离 socket/config/log 在 tmux 内执行默认 root/sticky/overlay/copy/退出链路，并注入 CSI-u 样本；脚本失败必须保留脱敏 artifact。
- 运行全量 TUI、clean-env CLI、`go test -race ./tui/...`、shortcut/CSI-u 定向 `-count=20` 和上述 tmux 黑盒，覆盖普通终端与支持 CSI-u 的路径。
- 删除本项目发现且已被新架构取代的旧 shortcut/action/render 代码、无效兼容测试和过期文档；最终 `rg` 守卫不得再出现已禁止的第二真值或 placeholder action。

## 阶段双 Agent 审查

KS012-KS017 每个阶段都执行仓库根 `AGENTS.md` 的双审查门禁：实现与测试完成后，同时启动架构 reviewer 和代码 reviewer；任何 finding 修复后重跑测试，并由原 reviewer 复审。两个 reviewer 对阶段实现 diff 都明确 `PASS` 后，只允许机械回填 `workflow.md` 状态与审查证据、运行 `git diff --check` 并提交；任何其他变化都必须重新复审。
