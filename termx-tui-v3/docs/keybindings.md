# tui-v3 快捷键核查

本文件是 tui-v3 快捷键实现核查表。实际可触发快捷键以 `termx-tui-v3/input/bindings.go` 的 `BindingCatalog` 为准；footer 展示以 `termx-tui-v3/render/action_ids.go` 的 `ActionSpecCatalog` 和 `termx-tui-v3/render/vm.go` 的 `footerActionCatalog` 为准。

## 核查规则

- footer 展示的键必须能触发对应动作，除非它是鼠标/overlay 的可点击 action token。
- `BindingCatalog` 中存在但 footer 未展示的键必须在本文件列为“未展示但可触发”，否则应删除。
- pane split right/down 按 `tuiv2 迁移基准` 提供 `%` / `Ctrl-D` 与 `"` / `Ctrl-E` 键盘入口，同时继续复用 pane chrome action、鼠标 hit region、测试/smoke harness 和后续 command palette / CLI mini command 的同一 semantic command。
- UI mode 下未绑定按键必须被吞掉，不得漏发给 terminal；normal mode 未绑定 raw key 继续透传 terminal。

## tuiv2 迁移基准

后续把 v3 快捷键迁移到 tuiv2 风格时，以 `tuiv2/input/catalog.go` 的 `defaultBindingCatalog` 为主依据。`StatusText` 是底部 status bar 展示基准，`FooterText` 是 overlay footer button 的补充基准；`tuiv2/render/overlay_footer_actions.go`、`tuiv2/render/terminal_pool_layout.go` 和 `tuiv2/render/workspace_tree_overlay.go` 只用于确认 overlay 按钮文案和鼠标 action，不反过来替代 catalog。

| tuiv2 Mode | Status / Footer 展示 | 实际键/组合 | 迁移语义 |
| --- | --- | --- | --- |
| normal | `P PANE` | `Ctrl-P` | 进入 pane mode |
| normal | `R RESIZE` | `Ctrl-R` | 进入 resize mode |
| normal | `T TAB` | `Ctrl-T` | 进入 tab mode |
| normal | `W WORKSPACE` | `Ctrl-W` | 进入 workspace mode |
| normal | `O FLOAT` | `Ctrl-O` | 进入 floating mode |
| normal | `V COPY` | `Ctrl-V` | 进入 display / copy mode |
| normal | `F PICKER` | `Ctrl-F` | 打开 terminal picker |
| normal | `G GLOBAL` | `Ctrl-G` | 进入 global mode |
| pane | `h/j/k/l FOCUS` | `h` / `j` / `k` / `l`、方向键 | 聚焦相邻 pane |
| pane | `% VSPLIT` | `%`、`Ctrl-D` | 垂直分屏 |
| pane | `" HSPLIT` | `"`、`Ctrl-E` | 水平分屏 |
| pane | `d DETACH` | `d` | detach 当前 pane 的 terminal |
| pane | `r RECONNECT` | `r` | 通过 terminal picker 重连 pane |
| pane | `R RESTART` | `R` | 重启 exited terminal |
| pane | `a OWNER` | `a` | 当前 pane 获取 terminal ownership |
| pane | `s LOCK` | `s` | 切换 terminal size lock |
| pane | `X CLOSE+KILL` | `X` | 关闭 pane 并 kill terminal |
| pane | `z ZOOM` | `z` | zoom / unzoom pane |
| pane | `w CLOSE` | `w` | close pane |
| pane | `Esc BACK` / `close` | `Esc` | 退出 pane mode |
| resize | `h/j/k/l RESIZE` | `h` / `j` / `k` / `l`、方向键 | 小步调整 pane 尺寸 |
| resize | `H/J/K/L RESIZEx2` | `H` / `J` / `K` / `L` | 大步调整 pane 尺寸 |
| resize | `a OWNER` | `a` | 当前 pane 获取 terminal ownership |
| resize | `s LOCK` | `s` | 切换 terminal size lock |
| resize | `= BALANCE` | `=` | 平衡 pane 尺寸 |
| resize | `Space LAYOUT` | `Space` | 切换 layout |
| resize | `Shift+WASD PAN` | `W` / `A` / `S` / `D`、Shift 方向键 | 平移 pane 内 terminal 内容 |
| resize | `0/$ ^/B ALIGN` | `0` / `$` / `^` / `B` | 内容对齐到 pane 边缘 |
| resize | `m CENTER` | `m`、竖线键、`_` | 居中 terminal 内容 |
| resize | `r RESET` | `r` | 重置内容偏移 |
| resize | `Esc BACK` / `close` | `Esc` | 退出 resize mode |
| tab | `C NEW` | `c` | 新建 tab |
| tab | `R RENAME` | `r` | 重命名当前 tab |
| tab | `N/P NEXT/PREV` | `n` / `p` | 切换下一个 / 上一个 tab |
| tab | `1-9 JUMP` | `1`-`9` | 跳转到 tab 编号 |
| tab | `X KILL` | `x` | kill 当前 tab terminals 并关闭 tab |
| tab | `Esc BACK` / `close` | `Esc` | 退出 tab mode |
| workspace | `F PICK` | `f`、`s` | 打开 workspace picker |
| workspace | `C NEW` | `c` | 新建 workspace |
| workspace | `R RENAME` | `r` | 重命名当前 workspace |
| workspace | `X DELETE` | `x` | 删除当前 workspace |
| workspace | `N/P NEXT/PREV` | `n` / `p` | 切换下一个 / 上一个 workspace |
| workspace | `Esc BACK` / `close` | `Esc` | 退出 workspace mode |
| floating | `N NEW FLOAT` | `n` | 新建 floating pane |
| floating | `h/j/k/l MOVE` | `h` / `j` / `k` / `l` | 移动 floating pane |
| floating | `H/J/K/L RESIZE` | `H` / `J` / `K` / `L` | 调整 floating pane 尺寸 |
| floating | `c CENTER` | `c` | 居中 floating pane |
| floating | `m COLLAPSE` | `m` | 折叠 floating pane |
| floating | `o OVERVIEW` | `o` | 打开 floating overview |
| floating | `1-9 SUMMON` | `1`-`9` | 按 slot 召回 floating pane |
| floating | `a OWNER` | `a` | 当前 floating pane 获取 terminal ownership |
| floating | `v ALL` | `v` | 折叠或恢复全部 floating panes |
| floating | `= FIT` | `=` | 按内容 fit floating pane |
| floating | `s AUTO-FIT` | `s` | 切换 floating auto-fit |
| floating | `x CLOSE` | `x` | 关闭 active floating pane |
| floating | `f PICK` | `f` | 为 active floating pane 打开 terminal picker |
| floating | `Esc BACK` / `close` | `Esc` | 退出 floating mode |
| floating overview | `UP/DOWN MOVE` | `Up` / `Down`、`k` / `j` | 移动 floating selection |
| floating overview | `Enter OPEN` / `open` | `Enter` | 恢复并聚焦选中 floating pane |
| floating overview | `s SHOW ALL` / `show-all` | `s` | 展开全部 floating panes |
| floating overview | `c COLLAPSE ALL` / `collapse-all` | `c` | 折叠全部 floating panes |
| floating overview | `x CLOSE` / `close-pane` | `x` | 关闭选中 floating pane |
| floating overview | `1-9 SUMMON` | `1`-`9` | 按 slot 打开 floating pane |
| floating overview | `Esc BACK` / `close` | `Esc` | 关闭 floating overview |
| display | `MOVE CURSOR` | 方向键、`h` / `j` / `k` / `l` | 移动 copy cursor |
| display | `PG SCROLL` | `PgUp` / `PgDn` | page up / page down |
| display | `u/d HALF` | `u` / `d` | half-page up / down |
| display | `HOME/END LINE` | `Home` / `End` | 行首 / 行尾 |
| display | `g/G EDGE` | `g` / `G` | 顶部 / 底部 |
| display | `Space MARK/COPY` | `Space` | mark 或 copy selection |
| display | `y COPY` | `y`、`Enter` | copy selection；Enter copy 后退出 |
| display | `p/P PASTE` | `p` / `P` | paste last copy 或 system clipboard |
| display | `H HISTORY` | `H` | 打开 clipboard history |
| display | `Esc BACK` / `close` | `Esc` | 退出 display / copy mode |
| global | `? HELP` | `?` | 打开 help overlay |
| global | `t TERMINALS` | `t` | 打开 terminal pool |
| global | `q QUIT` | `q` | 退出 termx |
| global | `Esc BACK` / `close` | `Esc` | 退出 global mode |
| picker | `UP/DOWN MOVE` | `Up` / `Down` | 移动 terminal picker 选择项 |
| picker | `TYPE FILTER` | 输入文本 | 过滤 terminal picker |
| picker | `Enter HERE` / `attach` | `Enter` | attach selected terminal 到当前 pane |
| picker | `Tab SPLIT` / `split+attach` | `Tab` | 分屏并 attach selected terminal |
| picker | `Ctrl-E EDIT` / `edit` | `Ctrl-E` | 编辑 terminal metadata |
| picker | `Ctrl-K KILL` / `kill` | `Ctrl-K` | kill selected terminal |
| picker | `Ctrl-X DELETE` / `delete` | `Ctrl-X` | 删除 terminal inventory entry |
| picker | `Esc BACK` / `close` | `Esc` | 关闭 picker |
| terminal manager | `UP/DOWN MOVE` | `Up` / `Down` | 移动 terminal pool 选择项 |
| terminal manager | `TYPE FILTER` | 输入文本 | 过滤 terminal pool |
| terminal manager | `Enter HERE` / `here` | `Enter` | attach selected terminal 到当前 pane |
| terminal manager | `Ctrl-T TAB` / `tab` | `Ctrl-T` | attach selected terminal 到新 tab |
| terminal manager | `Ctrl-O FLOAT` / `float` | `Ctrl-O` | attach selected terminal 到 floating pane |
| terminal manager | `Ctrl-E EDIT` / `edit` | `Ctrl-E` | 编辑 terminal metadata |
| terminal manager | `Ctrl-K KILL` / `kill` | `Ctrl-K` | kill selected terminal |
| terminal manager | `Ctrl-X DELETE` / `delete` | `Ctrl-X` | 删除 terminal inventory entry |
| terminal manager | `Esc BACK` / `close` | `Esc` | 关闭 terminal manager |
| workspace picker | `UP/DOWN MOVE` | `Up` / `Down` | 移动 workspace tree 选择项 |
| workspace picker | `TYPE FILTER` | 输入文本 | 过滤 workspace tree |
| workspace picker | `Enter OPEN` / `Open` | `Enter` | 打开选中 workspace / tab / pane |
| workspace picker | `Ctrl-N NEW` / `New Workspace` | `Ctrl-N` | 创建 workspace |
| workspace picker | `Ctrl-R RENAME` / `Rename` / `Rename Tab` | `Ctrl-R` | 重命名 workspace 或 tab |
| workspace picker | `Ctrl-X REMOVE` / `Delete` / `Close Tab` / `Close` | `Ctrl-X` | 删除 workspace / tab / pane 项 |
| workspace picker | `Ctrl-D DETACH` / `Detach` | `Ctrl-D` | detach 选中 pane |
| workspace picker | `Ctrl-Z ZOOM` / `Zoom` | `Ctrl-Z` | zoom 选中 pane |
| workspace picker | `Esc BACK` / `close` | `Esc` | 关闭 workspace picker |
| prompt | `TYPE INPUT` | 输入文本 | 编辑 prompt 输入 |
| prompt | `Backspace DELETE` | `Backspace` | 删除 prompt 输入 |
| prompt | `Enter CONTINUE` / `submit` | `Enter` | 提交 prompt |
| prompt | `Esc BACK` / `cancel` | `Esc` | 取消 prompt |
| help | `Esc BACK` / `close` | `Esc` | 关闭 help |
| clipboard history | `UP/DOWN MOVE` | `Up` / `Down` | 移动 clipboard history 选择项 |
| clipboard history | `TYPE FILTER` | 输入文本 | 过滤 clipboard history |
| clipboard history | `Enter PASTE/NEW` | `Enter` | paste 或新建 clipboard entry |
| clipboard history | `Ctrl-E EDIT` | `Ctrl-E` | 编辑 clipboard entry |
| clipboard history | `Ctrl-X DELETE` | `Ctrl-X` | 删除 clipboard entry |
| clipboard history | `Esc BACK` | `Esc` | 关闭 clipboard history |

迁移注意：`tuiv2/app/status_hints.go` 会按当前 workbench/runtime 状态隐藏部分 status hint，例如没有 active pane 时不展示 pane 操作、没有 follower role 时不展示 `a OWNER`、没有多 workspace/tab 时不展示 next/prev。v3 迁移时应保留这种“catalog 为真、status 按上下文过滤”的模型。

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
| pane | `h/j/k/l FOCUS` | `h` / `j` / `k` / `l`、方向键 | focus pane |
| pane | `% VSPLIT` | `%`、`Ctrl-d` | vertical split |
| pane | `" HSPLIT` | `"`、`Ctrl-e` | horizontal split |
| pane | `d DETACH` | `d` | detach pane |
| pane | `r RECONNECT` | `r` | 打开 Terminal Picker 重连当前 pane |
| pane | `R RESTART` | `R` | restart 当前 pane 的 terminal |
| pane | `a OWNER` | `a` | 当前 pane 获取 terminal resize ownership |
| pane | `z ZOOM` | `z` | toggle zoom |
| pane | `b BALANCE`、`c CARD`、`p LINE` | `b`、`c`、`p` | balance、card presentation、split-line presentation |
| pane | `w CLOSE` | `w` | close pane |
| resize | `←/h`、`→/l`、`↑/k`、`↓/j` | 方向键、`h` / `j` / `k` / `l` | 按方向 resize，步长 2 |
| resize | `a OWNER` | `a` | 当前 pane 获取 terminal resize ownership |
| resize | `= BALANCE` | `=` | balance pane layout |
| resize | `s LOCK`、`space LAYOUT` | `s`、`Space` | 切换 active terminal view 的 size lock 与 layout mode |
| resize | `S+arrows PAN`、`0/$/^/B ALIGN`、`m/\|/_ CENTER`、`r RESET` | `Shift+WASD`、Shift 方向键、`0`、`$`、`^`、`B`、`m`、`|`、`_`、`r` | 修改 active terminal view 的 view-local content layout |
| global | `? HELP` | `?` | 打开 help overlay |
| global | `t TERMINALS` | `t` | 打开 Terminal Pool |
| global | `q QUIT` | `q` | quit TUI |
| global | `h HEADER`、`f FOOTER`、`w TREE`、`T TOAST`、`c CLEAR` | `h`、`f`、`w`、`T`、`c` | v3 shell chrome/toast 补充动作 |
| floating | `n NEW FLOAT`、`x CLOSE`、`f PICK`、`a OWNER`、`c CENTER`、`m COLLAPSE` | `n`、`x`、`f`、`a`、`c`、`m` | create / close floating pane、打开 picker、获取 ownership、居中、折叠 |
| tab | `c NEW`、`p PREV`、`n NEXT`、`r RENAME`、`x KILL`、`1-9 JUMP` | `c`、`p`、`n`、`r`、`x`、`1`-`9` | tab create / previous / next / rename / close / jump |
| workspace | `c NEW`、`p PREV`、`n NEXT`、`r RENAME`、`f PICK`、`x DELETE` | `c`、`p`、`n`、`r`、`f`、`x` | workspace create / previous / next / rename / tree / delete |

## 部分实现或未实现

| Mode | 快捷键 | 状态 | 缺口 |
| --- | --- | --- | --- |
| pane | `s LOCK` | 未实现 | tuiv2 catalog 有 pane mode 展示；当前真实入口收敛到 resize mode 的 view-local terminal layout command，未在 pane footer 常驻展示 |
| resize | `s LOCK`、`Space LAYOUT`、`Shift+WASD/Shift+Arrow PAN`、`0/$/^/B ALIGN`、`m/|/_ CENTER`、`r RESET` | 已实现 | 状态挂在 `TerminalViewBinding.Layout`，键盘与 footer action 走统一 semantic command，render projector 展示 layout metadata |
| floating | `o OVERVIEW`、`1-9 SUMMON` | 未实现 | render 仍是 floating overview placeholder，缺 overlay reducer/input |
| floating | `v ALL`、`= FIT`、`s AUTO-FIT` | 未实现 | 缺 floating group collapse、fit 与 auto-fit state |
| display | `Home/End`、`g/G`、`u/d`、`Enter copy 后退出` | 已核验 | 已走 authoritative `HistoryWindow` 上的 copy reducer；`Enter` 复制 selection 后退出 copy mode；见 `termx-tui-v3/app/integration_test.go` |
| display | `p/P PASTE`、`H HISTORY` | 后置 | clipboard paste/history overlay 依赖 history MVP，当前不在连续推进队列；恢复前需先重启 history MVP 切片 |
| picker | `Tab SPLIT`、`Ctrl-E EDIT`、`Ctrl-K KILL`、`Ctrl-X DELETE` | 部分实现 | 键盘分发复用 selected item、`ActionSpec` 和 `ShellContentActionMsg`；`Ctrl-X` 保留 delete 语义，tui-v3 service/reducer 接线排入 215C1 |
| terminal manager | `Ctrl-T TAB`、`Ctrl-O FLOAT`、`Ctrl-E EDIT`、`Ctrl-K KILL`、`Ctrl-X DELETE` | 部分实现 | attach tab/floating、edit、kill 均走 overlay action/reducer/effect；`Ctrl-X` 保留 delete 语义，tui-v3 service/reducer 接线排入 215C1 |
| workspace picker | `Ctrl-N NEW`、`Ctrl-R RENAME`、`Ctrl-X REMOVE`、`Ctrl-D DETACH`、`Ctrl-Z ZOOM` | 已实现 | Workbench Tree 键盘动作统一复用 content action reducer；detach 同步清理 pane terminal view binding |
| floating overview | 全部 | 未实现 | 当前只渲染 placeholder |
| clipboard history | 全部 | 后置 | 依赖 history MVP，当前不在连续推进队列 |

## 未展示但可触发

| Mode | 快捷键 | 语义动作 | 保留原因 |
| --- | --- | --- | --- |
| normal | named ctrl aliases：`Ctrl-p` 可表现为 `Ctrl+p` 字符名等 | 进入对应 mode、picker 或 copy | 终端输入归一化兼容，覆盖 `root-*-named` 绑定 |
| pane | `x` | close pane | v3 早期别名，当前测试覆盖 |
| pane | `X` | close pane and kill terminal，confirm accepted | danger 操作，不放 footer 常驻展示 |
| pane | `N`、`n` | focus previous / next | v3 早期别名，当前几何方向尚未表达为独立 truth |
| resize | `H` / `J` / `K` / `L` | 按方向 resize，步长 6 | 大步长操作，footer 展示普通步长入口 |
| resize | `b` | balance pane layout | v3 早期别名，footer 展示 tuiv2 `=` |
| global | `p`、`m` | 打开 Terminal Pool | legacy alias，未放 footer |
| global | `:` | 打开 Prompt | overlay 入口，未放 footer |
| floating | `z` | collapse | `m COLLAPSE` 已展示为 tuiv2 主键，`z` 保留为 v3 早期别名 |
| floating | `h` / `j` / `k` / `l`、方向键、`H` / `J` / `K` / `L` | move / resize floating pane | footer 以 `arrows move`、`HJKL size` 汇总展示 |
| tab | `c`、`]`、`[`、`p`、`X` | create、next、previous、previous、kill | legacy 或 danger alias，未放 footer |
| workspace | `c`、`]`、`[`、`p`、`f`、`s` | create、next、previous、previous、tree、tree | legacy alias，未放 footer |

## 测试证据

- `termx-tui-v3/input/types_test.go`：覆盖 routing、catalog 唯一性、UI mode 吞键、pane split 键盘入口。
- `termx-tui-v3/render/vm_test.go`、`termx-tui-v3/render/framework_test.go`：覆盖 footer token 展示。
- `termx-tui-v3/app/ui_input_test.go`、`termx-tui-v3/app/runtime_test.go`、`termx-tui-v3/app/integration_test.go`：覆盖 app reducer、footer click、pane chrome click、copy/display canonical keys 和 no terminal input leak。
- `termx-tui-v3/app/pane_command_adapter_test.go`：覆盖 pane split chrome hit region 到 semantic command 的映射。
