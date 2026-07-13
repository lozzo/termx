# TUI 快捷键现状盘点

## KS012 最终审计基线

KS012 重新盘点后的机器可读基线位于 `shortcut-contract-debt.json`。默认 catalog 有 203 个 scene+key 条目，其中 166 个进入 root/sticky 输入路由。KS013 后中立 `tui/action` registry 有 159 个 canonical spec，覆盖 keyboard、mouse-only、drag 与内容 CTA；旧 footer/chrome/content 名称不再冒充 action identity。render 有 123 个本地 `ProjectionID`，可执行投影通过 `CanonicalActionID` 引用 action domain，聚合提示则只携带具体 invocation。

审计把每个输入、binding、spec、handler、projection 和提示来源归为 `conforming` 或 `debt`。债务项必须同时声明目标 owner、源码锚点和 KS013/KS015/KS016 目标切片；测试独立锁定债务 ID、inventory 数量和源码锚点，因此不能只修改 JSON 来接受新增债务。KS013-KS016 消除债务时必须同步删除对应清单项和门禁基线，不保留兼容 registry 或字符串桥接。

## 盘点范围

本文件记录 KS001 时点的现有快捷键真值和提示来源，用于后续迁移到 `tui.shortcuts`。

主要来源：

- 实际键盘路由：`tui/input/bindings.go`、`tui/input/types.go`。
- copy/history 输入：`tui/app/copymode.go`。
- overlay 专用输入：`tui/app/ui_input.go`。
- footer/action 展示：`tui/render/action_ids.go`、`tui/render/vm.go`。
- help 内容：`tui/render/product_content.go`。
- 旧配置入口：`tui/config/config.go`、`tui/state/config.go`。

## 总体问题

| 问题 | 现状 | 后续处理 |
| --- | --- | --- |
| 输入真值分散 | KS001 时点主 binding 在 `input/bindings.go`，copy mode 在 `app/copymode.go`，overlay Ctrl 组合键在 `app/ui_input.go` | KS003 已建立 shortcut catalog 和 action registry；overlay 提示/输入同源留到 KS004 |
| 展示真值分散 | footer 来自 `render/action_ids.go` 和 `render/vm.go`，help 来自 `render/product_content.go` | KS004 由 catalog 派生 footer/help/overlay |
| 旧 `tui.keymap` 未接入实际路由 | KS001 时点 config 解析和校验存在，但 input router 仍使用硬编码 `bindingCatalog` | KS002 删除旧 keymap，不保留兼容 |
| sticky mode 会回查 root binding | KS001 时点处于 pane/resize/global/floating/tab/workspace 时，未命中当前 mode 会继续匹配 root | KS003 在 catalog 中显式建模，不保留隐式 fallback |
| 大写键较多 | `X`、`N`、`HJKL`、`T`、`P` 等需要 Shift | 默认 shortcuts 应优先使用小写，必要动作用新键位或长写 label 表达 |
| overlay footer 有不一致 | Clipboard History footer 显示 `n/e/x`，实际键盘处理是 `Ctrl-N/Ctrl-E/Ctrl-X` | KS004 统一 overlay 提示和输入 |
| 按键名格式不一致 | footer 混用 `^P`、`Ctrl+T`、`PgUp`、`S+arrows`、`HJKL` | KS002 定义统一 key token，KS004 统一渲染 |

## Root / normal 输入态

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| `Ctrl-P` | 进入 pane 场景 | footer `^P PANE`，help `Ctrl-p pane` | 同时存在控制字节和 named char 两个 binding |
| `Ctrl-R` | 进入 resize 场景 | footer `^R RESIZE`，help `Ctrl-r resize` | 同上 |
| `Ctrl-G` | 进入 global 场景 | footer `^G GLOBAL`，help `Ctrl-g global` | 同上 |
| `Ctrl-O` | 进入 floating 场景 | footer `^O FLOAT` | help 只列 floating 分组，不列入口详情 |
| `Ctrl-T` | 进入 tab 场景 | footer `^T TAB` | 后续可加 `Ctrl-1..9` 直跳 tab |
| `Ctrl-W` | 进入 workspace 场景 | footer `^W WORKSPACE` | 与部分 shell/readline 常用键冲突，需可配置 |
| `Ctrl-F` | 打开 Terminal Picker | footer `^F PICKER`，help `Ctrl-f picker` | empty/exited 内容也写死 `Ctrl-F` 文案 |
| `Ctrl-V` | 进入 copy/history | footer `^V COPY` | root 下抢占 terminal 的 literal Ctrl-V |
| `PageUp` | 进入 copy/history，copy active 时请求 older | copy footer `PgUp SCROLL` | 鼠标滚轮也有类似入口 |
| `Esc` | normal 下透传 ESC；按 suggestion、overlay、copy、sticky 层级返回一层 | 可返回时 footer 自动显示 `Esc BACK` | 全局保留键，不来自 shortcut 配置 |
| mouse wheel up/down | normal 下进入 copy 或请求 older/newer；copy active 时翻历史 | copy footer 显示 `wheel` | terminal mouse passthrough 时不接管 |

## Pane 场景

进入方式：root `Ctrl-P`。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| `x` / `w` | close pane | footer 显示 `w CLOSE` | `x` 能按但 footer 不展示；`w` 是旧 tuiv2 别名 |
| `d` | detach pane | footer `d DETACH` |  |
| `r` | reconnect pane | 未展示 | 能按但无 footer/help 入口 |
| `R` | restart pane | 未展示 | 大写，需要 Shift |
| `a` | take owner | 未展示 | resize/floating 也有 owner |
| `s` | terminal size lock | 未展示 | resize footer 展示 `s LOCK` |
| `%` / `Ctrl-D` | split right | footer `% VSPLIT` | `Ctrl-D` 能按但未展示，且和 terminal EOF 冲突 |
| `"` / `Ctrl-E` | split down | footer `" HSPLIT` | `Ctrl-E` 能按但未展示 |
| `X` | kill pane accepted | 未展示 | 大写破坏性动作，不经过确认提示模型 |
| `z` | toggle zoom | footer `z ZOOM` |  |
| `b` | balance panes | footer `b BALANCE` |  |
| `c` | card presentation | footer `c CARD` |  |
| `p` | split-line presentation | footer `p LINE` |  |
| `n` / `l` / `j` / right/down | focus next | footer 合并 `h/j/k/l FOCUS` | `n` 未展示 |
| `N` / `h` / `k` / left/up | focus previous | footer 合并 `h/j/k/l FOCUS` | `N` 未展示且需 Shift |
| `Esc` | 退出 pane 场景 | footer 自动显示 `Esc BACK` | 全局返回导航 |

## Resize 场景

进入方式：root `Ctrl-R`。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| arrows / `h` / `j` / `k` / `l` | resize delta=2 | footer `←/h`、`→/l`、`↑/k`、`↓/j` | 连续操作会留在 resize 场景 |
| `H` / `J` / `K` / `L` | resize delta=6 | 未展示 | 大写快捷加速 |
| `a` | take owner | 未展示 |  |
| `s` | terminal size lock | footer `s LOCK` |  |
| space | terminal layout toggle | footer `space LAYOUT` |  |
| `A/S/W/D` 或 Shift+arrows | pan terminal layout | footer `S+arrows PAN` | 大写 WASD 表达不直观 |
| `0` / `$` / `^` / `B` | align left/right/top/bottom | footer `0/$/^/B ALIGN` | 三个需要 Shift 或特殊键 |
| `m` / `|` / `_` | center / center-x / center-y | footer `m/|/_ CENTER` | `|`、`_` 需要 Shift |
| `r` | layout reset | footer `r RESET` |  |
| `b` / `=` | pane balance | footer `= BALANCE` | `b` 能按但未展示 |
| `Esc` | 退出 resize 场景 | footer 自动显示 `Esc BACK` | 全局返回导航 |

## Global 场景

进入方式：root `Ctrl-G`。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| `h` | toggle header | footer `h HEADER` |  |
| `f` | toggle footer | footer `f FOOTER` |  |
| `c` | clear toasts | 仅有 action spec，宽度/可用性影响展示 |  |
| `T` | close toast | 仅有 action spec，需有 toast | 大写 |
| `p` / `m` / `t` | open Terminal Pool | footer 默认 `t TERMINALS` | `p`、`m` 能按但通常不展示 |
| `w` | open Workbench Tree | footer `w TREE` |  |
| `l` | toggle shortcut lock | footer `l KEYLOCK` |  |
| `:` | open prompt | 未展示在 global footer | help 分组展示 prompt |
| `?` | open help | footer `? HELP` |  |
| `q` | quit TUI | footer `q QUIT` |  |
| `Esc` | 退出 global 场景 | footer 自动显示 `Esc BACK` | 全局返回导航 |

## Floating 场景

进入方式：root `Ctrl-O`。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| `n` | new floating | footer `n NEW FLOAT` |  |
| `o` | floating overview | footer `o OVERVIEW` |  |
| `1..9` | summon floating by index | footer `1-9 SUMMON` |  |
| `f` | picker | footer `f PICK` |  |
| `a` | take owner | footer `a OWNER` |  |
| `x` | close active floating | footer `x CLOSE` |  |
| `z` / `m` | collapse/hide | footer 显示 `m HIDE` | `z` 能按但未展示 |
| `c` | center | footer `c CENTER` | floating overview 中 `c` 是 collapse all，语义冲突 |
| `v` | toggle all | footer `v ALL` |  |
| `=` | fit | footer `= FIT` |  |
| `s` | toggle auto-fit | footer `s AUTO-FIT` | floating overview 中 `s` 是 show all |
| arrows / `h` / `j` / `k` / `l` | move | footer `arrows move` | hjkl 能按但 footer 未列 |
| `H/J/K/L` | resize floating | footer `HJKL size` | 大写 |
| `Esc` | 退出 floating 场景 | footer 自动显示 `Esc BACK` | 全局返回导航 |

## Tab 场景

进入方式：root `Ctrl-T`。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| `c` | create tab | footer `c NEW` |  |
| `n` / `l` / `]` | next tab | footer `n NEXT` | `l`、`]` 未展示 |
| `p` / `h` / `[` | previous tab | footer `p PREV` | `h`、`[` 未展示 |
| `1..9` | jump tab index | footer `1-9 jump` | root `Ctrl-1..9` 当前未建模 |
| `r` | rename tab | footer `r RENAME` |  |
| `x` | close tab | footer `x CLOSE` |  |
| `X` | kill tab accepted | 未展示 | 大写破坏性动作 |
| `Esc` | 退出 tab 场景 | footer 自动显示 `Esc BACK` | 全局返回导航 |

## Workspace 场景

进入方式：root `Ctrl-W`。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| `c` | create workspace | footer `c NEW` |  |
| `n` / `l` / `]` | next workspace | footer `n NEXT` | `l`、`]` 未展示 |
| `p` / `h` / `[` | previous workspace | footer `p PREV` | `h`、`[` 未展示 |
| `r` | rename workspace | footer `r RENAME` |  |
| `x` | delete workspace accepted | footer `x DELETE` |  |
| `t` / `f` / `s` | open Workbench Tree | footer 覆盖显示 `f PICK` | `t`、`s` 能按但未展示 |
| `Esc` | 退出 workspace 场景 | footer 自动显示 `Esc BACK` | 全局返回导航 |

## Copy/history 输入

进入方式：root `Ctrl-V`、`PageUp` 或 wheel up。

| 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- |
| arrows / `h` / `j` / `k` / `l` | 移动 cursor 或搜索命中 | help 仅概括 Display / Copy | footer 未展示 |
| `PageUp` / wheel up | older history | footer `PgUp SCROLL`、`wheel` |  |
| `PageDn` / wheel down | newer history | 未展示 |  |
| `Home` / `End` | 行首/行尾 | 未展示 |  |
| `g` / `G` | oldest/latest 或跳行 | 未展示 | `G` 大写 |
| `u` / `d` | half-page older/newer | 未展示 |  |
| space | set mark | 未展示 |  |
| `y` / `Enter` | copy selection | 未展示 |  |
| `/` then text | search | 未展示 |  |
| backspace | edit search query | 未展示 |  |
| `p` / `P` | paste latest/system clipboard | 旧 keymap 文档写 `p` / `shift-p` | `P` 大写 |
| `H` | open Clipboard History overlay | footer/action spec 显示 `H CLIPBOARD` | 旧 keymap 文档写默认 `h`，和实际不一致 |
| `Esc` | exit copy mode | footer 自动显示 `Esc BACK` | 全局返回导航，并释放当前 view history token |

## Overlay 输入

| Overlay | 实际按键 | 触发 | 页面展示 | 备注 |
| --- | --- | --- | --- | --- |
| Terminal Picker | `Up` / `Down` | 移动选择 | footer 未展示，只显示 `enter select` | 实际是 overlay 基础导航键 |
| Terminal Picker | `Enter` | attach 或打开 create prompt | footer `enter select` |  |
| Terminal Picker | `Backspace` / `Delete` / 普通字符 | 编辑搜索 query | footer 未展示 | 普通字符会被 overlay 吃掉，不透传 terminal |
| Terminal Picker | `Tab` | attach in split | help action 存在，footer 未展示 |  |
| Terminal Picker | `Ctrl-E` / `Ctrl-K` / `Ctrl-X` | edit / kill / delete | action spec 有 help，picker footer 只显示 enter/esc | 能按但 footer 不展示 |
| Terminal Picker | `Esc` | 关闭 overlay | footer 自动显示 `Esc BACK` | 全局返回导航 |
| Terminal Pool | `Up` / `Down` | 移动选择并刷新 preview | footer 未展示 | 实际是 overlay 基础导航键 |
| Terminal Pool | `Enter` | attach here | footer `enter ATTACH` |  |
| Terminal Pool | `Backspace` / `Delete` / 普通字符 | 编辑搜索 query 并刷新 preview | footer 未展示 | 普通字符会被 overlay 吃掉 |
| Terminal Pool | `Ctrl-T` / `Ctrl-O` / `Ctrl-R` / `Ctrl-E` / `Ctrl-K` / `Ctrl-X` | attach tab / float / restart / rename / kill / remove | footer 显示 `Ctrl+T/O/R/E/K/X` | 与 root `^T/^O/^R` 表达格式不同 |
| Terminal Pool | `Esc` | 关闭 overlay | footer 自动显示 `Esc BACK` | 全局返回导航 |
| Workbench Tree | `Up` / `Down` | 移动选择 | footer 未展示 |  |
| Workbench Tree | `Left` / `Right` | 折叠/展开当前节点 | footer `←/→ FOLD` |  |
| Workbench Tree | `Enter` | 展开/折叠节点或 open 当前项 | footer `open` |  |
| Workbench Tree | `Backspace` / `Delete` / 普通字符 | 编辑搜索 query | footer `search` |  |
| Workbench Tree | `Ctrl-N` / `Ctrl-R` / `Ctrl-X` / `Ctrl-D` / `Ctrl-Z` | new / rename / delete / detach / zoom | footer 只显示 search、fold、open、ctrl-n、focus、esc | 多数能按但没展示 |
| Workbench Tree | `Esc` | 关闭 overlay | footer 自动显示 `Esc BACK` | 全局返回导航 |
| Clipboard History | `Up` / `Down` | 移动选择 | footer `↑↓ SELECT` |  |
| Clipboard History | `Enter` | paste | footer `enter PASTE` |  |
| Clipboard History | `Backspace` / `Delete` / 普通字符 | 编辑搜索 query | footer 未展示 | `Delete` 在 query 为空时仍按删除 query 字符处理，不是删除条目 |
| Clipboard History | `Ctrl-N` / `Ctrl-E` / `Ctrl-X` | new / edit / delete | footer 显示 `n/e/x` | 展示和实际按键不一致 |
| Clipboard History | `Esc` | 关闭 overlay | footer 自动显示 `Esc BACK` | 全局返回导航 |
| Floating Overview | arrows | select | footer `↑/↓ select` |  |
| Floating Overview | `Enter` | summon/open | footer `enter OPEN` |  |
| Floating Overview | `1..9` | summon floating by index | footer 使用 `floating.summon` / row action | 之前容易被误归到 floating sticky 场景 |
| Floating Overview | `s` / `c` / `x` | show all / collapse all / close | footer 使用 action 默认 key，部分来自 floating 场景 | 与 floating 主场景 `s/c/x` 语义不同 |
| Floating Overview | `Esc` | 关闭 overlay | footer 自动显示 `Esc BACK` | 全局返回导航 |
| Prompt | 普通字符 / `Backspace` / `Delete` / `Left` / `Right` / `Home` / `End` | 编辑当前 prompt 字段 | footer `type` | help 只概括 Prompt / Help |
| Prompt | `Tab` / `Shift-Tab` / `Up` / `Down` | 字段移动、路径补全或 suggestion 选择 | 未完整展示 | suggestion focused 时语义会变成移动/接受建议 |
| Prompt | `Enter` / `Esc` | 提交 / 先退出 suggestion focus，再次 Esc 取消 prompt | footer `enter submit` + 自动 `Esc BACK` | Esc 每次只返回一层 |
| Help | `Enter` / `Esc` | close help | footer `enter close` + 自动 `Esc BACK` | Enter 可配置，Esc 固定全局返回 |

## 内容区 CTA 与写死提示

| 位置 | 实际按键 | 触发 | 页面展示 | 问题 |
| --- | --- | --- | --- | --- |
| empty pane CTA | `Up` / `Down` | 在 Attach / Create / Manager / Close 间移动选择 | 内容按钮高亮 | 不在 footer/help catalog 中 |
| empty pane CTA | `Enter` | 执行当前选中 CTA | 内容按钮 | 不在 footer/help catalog 中 |
| disconnected pane CTA | `Up` / `Down` | 在 reconnect / disconnect 间移动选择 | 内容按钮高亮 | 不在 footer/help catalog 中 |
| disconnected pane CTA | `Enter` | 执行当前选中 CTA | 内容按钮 | 不在 footer/help catalog 中 |
| disconnected pane CTA | `r` / `R` | 直接 reconnect | 内容按钮只显示 Reconnect this pane | 大写/小写都有效，但没有明确提示 |
| exited pane CTA | `Up` / `Down` | 在 restart / choose another terminal 间移动选择 | 内容按钮高亮 | 不在 footer/help catalog 中 |
| exited pane CTA | `Enter` | 执行当前选中 CTA | 内容按钮 | 不在 footer/help catalog 中 |
| exited pane CTA | `r` / `R` | 直接 restart | 内容按钮显示 `R restart current terminal` | `r` 也有效但文案只写大写 `R` |
| exited pane 内容 | 文案 `Ctrl-F choose another terminal` | 打开 picker | 内容按钮/提示 | 文案不从 key catalog 生成 |
| empty workspace 内容 | 文案 `Ctrl-F open terminal picker`、`Ctrl-T then c create a new tab` | 打开 picker / tab create | 内容提示 | 文案不从 key catalog 生成 |
| Help Most used | 文案 `Ctrl-p pane`、`Ctrl-r resize`、`Ctrl-f picker`、`Ctrl-g global` | 只读帮助 | help 内容 | 只列部分 root 入口，且格式和 footer 不一致 |

## 旧配置入口

KS001 时点，`tui.keymap` 存在于 `state/config.go`、`config/config.go`、示例配置和测试中，但运行时输入路由不消费它。也就是说，用户改 `tui.keymap.tab_mode.create` 只会通过解析和校验，不会改变 `input/bindings.go` 的实际按键。

KS002 已删除旧 `tui.keymap`，并用 `tui.shortcuts` 替换示例和测试。不得保留旧配置读取、自动迁移、deprecated 警告或 fallback。
