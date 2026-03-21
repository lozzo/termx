# 交互设计 v3

本文档是 `interaction-v2.md` 的下一版提案，方向改为更接近 zellij 的多模式键盘系统：

- 不再使用统一的 `Ctrl-a` prefix
- 直接用不同的 `Ctrl-*` 组合键进入不同管理模式
- 按下模式键后，底部状态栏实时展示该模式的可用按键
- 模式内用普通字母连续操作，`Esc` 统一退出

这版的核心不是 tmux 式 `prefix + command`，而是 zellij 式 `mode trigger + bottom hints`。

## 设计原则

1. 不设统一 prefix 按钮，直接用不同组合键进入不同模式
2. 底部状态栏是第一帮助入口，用户不需要背整张快捷键表
3. 模式切换要有清晰语义：Pane / Tab / Workspace / Floating / View
4. 模式内尽量用普通字母，不再堆叠 `Ctrl` / `Alt` 组合
5. `Esc` 统一退出所有管理模式，降低心智负担
6. 允许后续 remap；因为 direct `Ctrl-*` 天然会和 shell / editor 部分键位冲突

## v3 总体模型

```
Normal
  ├─ Ctrl-p -> Pane mode
  ├─ Ctrl-r -> Resize mode
  ├─ Ctrl-t -> Tab mode
  ├─ Ctrl-w -> Workspace mode
  ├─ Ctrl-o -> Floating mode
  ├─ Ctrl-v -> View mode
  ├─ Ctrl-f -> Picker mode
  ├─ Ctrl-b -> Scroll mode
  └─ Ctrl-g -> Global mode

进入任一 mode 后：
  - 底部栏立即切换成该 mode 的提示条
  - 模式内可连续执行多个动作
  - Esc 退出，回到 Normal
```

## 默认快捷键配置草案

### Mode Trigger

```
Ctrl-p   Pane mode
Ctrl-r   Resize mode
Ctrl-t   Tab mode
Ctrl-w   Workspace mode
Ctrl-o   Floating mode
Ctrl-v   View mode
Ctrl-f   Terminal Picker mode
Ctrl-b   Scroll / Copy mode
Ctrl-g   Global mode
```

说明：

- `Ctrl-p` / `Ctrl-o` 保留最强语义入口，符合你前面提的方向
- `Ctrl-g` 作为少量全局动作容器，不再承担统一 prefix 职责
- 这不是 tmux 的单 prefix 树，而是多个 direct mode trigger

## 各模式键位

### 1. Pane mode

入口：`Ctrl-p`

```
"           水平分屏（上下）
%           垂直分屏（左右）
h/j/k/l     在 pane 间切焦点
Left/Down/Up/Right
            在 pane 间切焦点
{ / }       与前/后一个 pane 交换位置
z           toggle zoom
x           关闭当前 viewport（detach）
X           kill 当前 terminal
c           新建 tab
f           打开 terminal picker
Esc         退出 Pane mode
```

底部提示：

```
[PANE] ":split %:split hjkl:focus {}:swap z:zoom x:close X:kill c:new-tab f:pick Esc:exit
```

### 2. Resize mode

入口：`Ctrl-r`

```
h / Left    向左调整边界
j / Down    向下调整边界
k / Up      向上调整边界
l / Right   向右调整边界
H/J/K/L     大步调整
=           balance 当前布局
Space       循环布局预设
Esc         退出 Resize mode
```

底部提示：

```
[RESIZE] hjkl:resize HJKL:coarse =:balance Space:layout Esc:exit
```

### 3. Tab mode

入口：`Ctrl-t`

```
1-9         跳到第 N 个 tab
n           下一个 tab
p           上一个 tab
c           新建 tab
r           重命名当前 tab
x           关闭当前 tab（只关 viewport，不 kill terminal）
f           打开 terminal picker
Esc         退出 Tab mode
```

底部提示：

```
[TAB] 1-9:jump n/p:next-prev c:new r:rename x:close f:pick Esc:exit
```

### 4. Workspace mode

入口：`Ctrl-w`

```
s           切换 workspace
c           新建 workspace
r           重命名当前 workspace
x           删除当前 workspace
n           下一个 workspace
p           上一个 workspace
f           打开 workspace / terminal picker
Esc         退出 Workspace mode
```

底部提示：

```
[WORKSPACE] s:switch c:new r:rename x:delete n/p:next-prev f:pick Esc:exit
```

说明：`x` 统一承担"关闭/删除当前上下文"，避免和 detach 混淆。

### 5. Floating mode

入口：`Ctrl-o`

```
n           新建 floating viewport
Tab         在浮窗间切换焦点
]           提升到最顶层
[           降到最底层
h/j/k/l     移动浮窗
H/J/K/L     调整浮窗大小
v           toggle 所有浮窗显示/隐藏
x           关闭当前浮窗 viewport（detach）
f           打开 terminal picker
Esc         退出 Floating mode
```

底部提示：

```
[FLOAT] n:new Tab:focus []:z-order hjkl:move HJKL:size v:toggle x:close f:pick Esc:exit
```

### 6. View mode

入口：`Ctrl-v`

```
m           fit <-> fixed
r           toggle readonly
p           toggle pin
h/j/k/l     平移 fixed viewport offset
Left/Down/Up/Right
            平移 fixed viewport offset
0           offset 回到左上角
$           跳到最右边
g           跳到顶部
G           跳到底部
z           reset viewport visual state
Esc         退出 View mode
```

底部提示：

```
[VIEW] m:fit/fixed r:readonly p:pin hjkl:pan 0/$/g/G:jump z:reset Esc:exit
```

说明：v3 把 offset pan 直接并入 View mode，不再单独进 `offset-pan` 子模式。

### 7. Picker mode

入口：`Ctrl-f`

```
/           过滤
Enter       attach 到当前上下文
s           attach 为 split
t           attach 到新 tab
o           attach 为 floating
n           新建 terminal
k           kill 选中 terminal
Esc         关闭 picker
```

底部提示：

```
[PICKER] /:filter Enter:attach s:split t:tab o:float n:new k:kill Esc:exit
```

### 8. Scroll mode

入口：`Ctrl-b`

```
h/j/k/l     移动
PgUp/PgDn   翻页
g / G       顶部 / 底部
/           搜索
n / N       下一个 / 上一个匹配
v           进入选择
y           复制
q / Esc     退出 Scroll mode
```

底部提示：

```
[SCROLL] hjkl/pgup/pgdn:move /:search n/N:next-prev v:select y:copy q:exit
```

### 9. Global mode

入口：`Ctrl-g`

```
?           打开 help
:           打开 command line
d           detach TUI
q           退出 TUI client
s           save layout / state
l           load layout
Esc         退出 Global mode
```

底部提示：

```
[GLOBAL] ?:help ::command d:detach q:quit s:save l:load Esc:exit
```

## 底部状态栏策略

### Normal 状态

默认底栏直接告诉用户有哪些 mode trigger：

```
[NORMAL] C-p:PANE C-r:RESIZE C-t:TAB C-w:WS C-o:FLOAT C-v:VIEW C-f:PICK C-b:SCROLL C-g:GLOBAL
```

规则：

- 不显示 `prefix` 字样
- 不显示 `prefix:t` / `prefix:w` 这种中间态
- 正常状态下只强调入口，不铺太多细节
- 一旦进入 mode，再展开该模式的详细提示

### Mode 状态

按下任意 direct trigger 后：

1. 底部栏立刻切到对应 mode
2. 左侧显示 `[MODE]`
3. 中间显示可执行动作
4. 右侧继续保留 pane id / terminal id / readonly / floating 数量等状态信息

例如：

```
[PANE] ":split %:split hjkl:focus {}:swap z:zoom x:close X:kill c:new-tab f:pick Esc:exit
```

## 为什么这版比 v2 更符合你的要求

### 1. 没有统一 prefix

不是：

- `Ctrl-a` 再分发到各子模式

而是：

- `Ctrl-p` 直接进 pane 管理
- `Ctrl-o` 直接进 floating 管理
- `Ctrl-t` 直接进 tab 管理

这点和你刚才说的方向一致。

### 2. 比 tmux 的 `prefix + key` 更短

例如：

- tmux / v2：`Ctrl-a` -> `o` -> `h`
- v3：`Ctrl-o` -> `h`

少一次按键，也少一层等待。

### 3. 提示是实时的，不靠记忆

用户不需要记住：

- `C-a t c`
- `C-a w s`
- `C-a v o h`

只需要记：

- `Ctrl-p` 进 Pane，看底栏
- `Ctrl-o` 进 Float，看底栏
- `Ctrl-v` 进 View，看底栏

## 风险与产品建议

### 风险

direct `Ctrl-*` 会和 shell / readline / vim 的一部分键位冲突，例如：

- `Ctrl-w`
- `Ctrl-t`
- `Ctrl-f`
- `Ctrl-b`
- `Ctrl-v`

这是这套方案换效率所付出的代价。

### 建议

如果 v3 真的作为产品主默认，我建议同时提供两套 preset：

1. `tmux-compatible`：统一 prefix 方案
2. `direct-mode`：本提案的 zellij 风格 direct trigger 方案

这样：

- 老 tmux 用户能无痛迁移
- 新用户或重度 TUI 用户可以直接选更高效的模式系统

## 迁移建议

从现有 v2 迁到 v3，建议按三步做：

1. 先把底部栏重构成 mode bar，支持实时提示
2. 再把 `Ctrl-p` / `Ctrl-o` / `Ctrl-t` / `Ctrl-w` / `Ctrl-v` 这些 direct trigger 加进去
3. 最后把旧的 `Ctrl-a + 子 prefix` 变成兼容别名或可选 preset
