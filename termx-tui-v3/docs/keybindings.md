# tui-v3 快捷键核查

本文件是 tui-v3 快捷键实现核查表。实际可触发快捷键以 `termx-tui-v3/input/bindings.go` 的 `BindingCatalog` 为准；footer 展示以 `termx-tui-v3/render/action_ids.go` 的 `ActionSpecCatalog` 和 `termx-tui-v3/render/vm.go` 的 `footerActionCatalog` 为准。

## 核查规则

- footer 展示的键必须能触发对应动作，除非它是鼠标/overlay 的可点击 action token。
- `BindingCatalog` 中存在但 footer 未展示的键必须在本文件列为“未展示但可触发”，否则应删除。
- pane split right/down 不提供键盘入口；它只通过 pane chrome action、鼠标 hit region、测试/smoke harness 或后续 command palette / CLI mini command 调用同一 semantic command。
- UI mode 下未绑定按键必须被吞掉，不得漏发给 terminal；normal mode 未绑定 raw key 继续透传 terminal。

## Footer 已展示并可触发

| Mode | Footer 展示 | 实际可触发键 | 语义动作 |
| --- | --- | --- | --- |
| normal | `^P pane` | `Ctrl-p` | 进入 pane mode |
| normal | `^R resize` | `Ctrl-r` | 进入 resize mode |
| normal | `^T tab` | `Ctrl-t` | 进入 tab mode |
| normal | `^W workspace` | `Ctrl-w` | 进入 workspace mode |
| normal | `^O float` | `Ctrl-o` | 进入 floating mode |
| normal | `^V copy` | `Ctrl-v`、`PageUp`、鼠标 wheel up | 进入 Display / Copy，或在 copy mode 请求 older history |
| normal | `^F picker` | `Ctrl-f` | 打开 Terminal Picker |
| normal | `^G global` | `Ctrl-g` | 进入 global mode |
| pane | `x close` | `x` | close pane |
| pane | `d detach` | `d` | detach pane |
| pane | `n focus` | `n` | focus next pane |
| pane | `z zoom` | `z` | toggle zoom |
| resize | `←/h`、`→/l`、`↑/k`、`↓/j` | 方向键、`h` / `j` / `k` / `l` | 按方向 resize，步长 2 |
| resize | `b balance` | `b` | balance pane layout |
| global | `h header`、`f footer` | `h`、`f` | toggle header / footer |
| global | `p pool`、`w tree` | `p`、`w` | 打开 Terminal Pool / Workbench Tree |
| global | `T toast`、`t clear` | `T`、`t` | close current toast / clear toasts |
| global | `q quit` | `q` | quit TUI |
| floating | `n new`、`x close` | `n`、`x` | create / close floating pane |
| tab | `n new`、`h prev`、`l next`、`r rename`、`x close`、`1-9 jump` | `n`、`h`、`l`、`r`、`x`、`1`-`9` | tab create / previous / next / rename / close / jump |
| workspace | `n new`、`h prev`、`l next`、`r rename`、`t tree`、`x delete` | `n`、`h`、`l`、`r`、`t`、`x` | workspace create / previous / next / rename / tree / delete |

## 未展示但可触发

| Mode | 快捷键 | 语义动作 | 保留原因 |
| --- | --- | --- | --- |
| normal | named ctrl aliases：`Ctrl-p` 可表现为 `Ctrl+p` 字符名等 | 进入对应 mode、picker 或 copy | 终端输入归一化兼容，覆盖 `root-*-named` 绑定 |
| pane | `w` | close pane | 旧用户肌肉记忆别名，当前测试覆盖 |
| pane | `X` | close pane and kill terminal，confirm accepted | danger 操作，不放 footer 常驻展示 |
| pane | `b`、`c`、`p` | balance、card presentation、split-line presentation | 低频 pane 操作，footer 只展示常用操作 |
| pane | `N`、`h` / `j` / `k` / `l`、方向键 | focus previous / next | 多键导航别名，当前几何方向尚未表达为独立 truth |
| resize | `H` / `J` / `K` / `L` | 按方向 resize，步长 6 | 大步长操作，footer 展示普通步长入口 |
| resize | `=` | balance pane layout | equalize 别名 |
| global | `m` | 打开 Terminal Pool | legacy alias，未放 footer |
| global | `:`、`?` | 打开 Prompt / Help | overlay 入口，未放 footer |
| floating | `z` / `m`、`c` | collapse / center | floating chrome/input 操作，footer 只展示 new/close 和移动提示 |
| floating | `h` / `j` / `k` / `l`、方向键、`H` / `J` / `K` / `L` | move / resize floating pane | footer 以 `arrows move`、`HJKL size` 汇总展示 |
| tab | `c`、`]`、`[`、`p`、`X` | create、next、previous、previous、kill | legacy 或 danger alias，未放 footer |
| workspace | `c`、`]`、`[`、`p`、`f`、`s` | create、next、previous、previous、tree、tree | legacy alias，未放 footer |

## 特意不做键盘入口

| 语义动作 | 不触发键 | 入口 |
| --- | --- | --- |
| pane split right | `Ctrl-p v`、`Ctrl-p %` | pane chrome split-right action、鼠标 hit region、semantic command |
| pane split down | `Ctrl-p s`、`Ctrl-p "` | pane chrome split-down action、鼠标 hit region、semantic command |

## 测试证据

- `termx-tui-v3/input/types_test.go`：覆盖 routing、catalog 唯一性、UI mode 吞键、pane split 键盘入口禁用。
- `termx-tui-v3/render/vm_test.go`、`termx-tui-v3/render/framework_test.go`：覆盖 footer token 展示。
- `termx-tui-v3/app/ui_input_test.go`、`termx-tui-v3/app/runtime_test.go`：覆盖 app reducer、footer click、pane chrome click 和 no terminal input leak。
- `termx-tui-v3/app/pane_command_adapter_test.go`：覆盖 pane split chrome hit region 到 semantic command 的映射。
