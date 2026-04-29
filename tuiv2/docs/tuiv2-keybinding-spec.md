# TUIV2 快捷键规范

状态：Canonical
日期：2026-03-31

> **目的**
> 本文档定义 `tuiv2` 的快捷键系统基线。
> 后续所有快捷键实现、help overlay、status bar 提示、e2e 测试，都应以本文档为准。
>
> **来源**
> 主要整理自旧版设计与遗留文档：
> - `deprecated/tui-legacy/docs/interaction-spec.md`
> - `deprecated/tui-legacy/docs/wireframes-v2.md`
> - `tui-product-definition-design.md`

---

## 1. 总体原则

### 1.1 mode 是短驻留工具，不是产品本体

快捷键系统采用 **mode / prefix** 结构，而不是在 normal 状态下堆一大把直接快捷键。

目标是：
- normal 状态下稳定工作，主要键盘输入直通 terminal
- 用户需要“结构操作”时，先进入一个短驻留 mode
- mode 只是一层工具性入口，不应成为理解产品的前提

### 1.2 normal 状态默认负责日常工作

normal 状态应满足：
- shell / vim / htop / less 等程序可直接输入
- 非必要按键不要被 TUI 抢走
- 只有少量顶层入口允许从 normal 状态直接触发

### 1.3 错误按键直接忽略，不应卡死

在 active mode 中：
- 无效按键直接忽略
- 不应进入异常状态
- `Esc` 必须总能安全退出当前 mode / modal

### 1.4 底栏提示不是纯快捷键表

底栏左侧展示：
- 当前 mode 的快捷键提示
- 采用连续 segment 风格
- 更接近 zellij 的视觉语义，而不是把全部按键平铺成噪音

Help overlay 则负责：
- 分组解释 Most used / Pane / Tab / Workspace / Floating / Shared terminal / Exit
- 解释概念模型，而不是只列按键表

---

## 2. 顶层快捷键（Canonical）

以下是旧版已明确的顶层 keymap，也是 `tuiv2` 应回归的主结构：

- `Ctrl-p` → `pane mode`
- `Ctrl-r` → `resize mode`
- `Ctrl-t` → `tab mode`
- `Ctrl-w` → `workspace mode`
- `Ctrl-o` → `floating mode`
- `Ctrl-v` → `display mode`
- `Ctrl-f` → `terminal picker`
- `Ctrl-g` → `global mode`
- `Esc` → 关闭当前 mode / modal

这是当前唯一的 **canonical root keymap**。

### 2.1 非 canonical 直接快捷键

除非本文档后续明确补充，否则以下类别的快捷键 **不应直接进入 normal 状态**：

- 直接 split（例如 `Ctrl-d` / `Ctrl-e`）
- 直接 quit（例如 `Ctrl-q`）
- 直接 scrollback（例如 `Ctrl-u` / `Ctrl-y`）
- 直接 workspace 切换
- 直接 zoom

这些能力如果要支持，应先明确归属到某个 mode，再补进本规范。

---

## 3. 各 mode 的职责边界

### 3.1 `pane mode` (`Ctrl-p`)

负责 pane 层面的结构操作，例如：
- 分屏
- 关闭 pane
- 聚焦 pane
- 断开 / 换绑 terminal
- 关闭 pane 并 kill terminal

### 3.2 `resize mode` (`Ctrl-r`)

负责 pane 或相关可见区域的 resize 行为。

### 3.3 `tab mode` (`Ctrl-t`)

负责：
- 新建 tab
- 切换 tab
- 关闭 tab
- 后续 tab 重命名也应优先落在此 mode

### 3.4 `workspace mode` (`Ctrl-w`)

负责：
- workspace 切换
- workspace 创建
- workspace 删除 / 重命名（若后续支持）

### 3.5 `floating mode` (`Ctrl-o`)

负责：
- 新建 floating pane
- 浮窗移动 / resize / 置顶 / 呼回

### 3.6 `display mode` (`Ctrl-v`)

负责显示相关动作，例如：
- viewport / offset-pan
- scrollback / 历史查看
- fit / fixed / follow 等显示行为
- zoom pane（如最终归入显示层）

### 3.7 `terminal picker` (`Ctrl-f`)

负责：
- attach/bind 已有 terminal
- 创建新 terminal
- 从当前 pane 工作流中以最短路径完成连接

### 3.8 `global mode` (`Ctrl-g`)

负责：
- 全局级动作
- 不属于 pane/tab/workspace/floating/display 的少数能力

---

## 4. `Esc` 与 Help 的统一规则

### 4.1 `Esc`

`Esc` 是统一退出键：
- 退出当前 mode
- 关闭 picker / prompt / help / terminal manager
- 不应在普通 shell 输入态之外抢占 terminal 的必要行为

### 4.2 Help

Help 第一阶段就应存在，但它不是“纯快捷键表”。

至少覆盖：
- Most used
- Pane / Tab / Workspace
- Shared terminal
- Floating
- Exit

参考旧线框中的结构：
- `Most used`
  - `Ctrl-p` pane actions
  - `Ctrl-t` tab actions
  - `Ctrl-w` workspace actions
  - `Ctrl-o` floating actions
  - `Ctrl-f` terminal picker
- `Exit`
  - `Esc` close current mode/modal

---

## 5. mode 生命周期规则

旧版设计里，mode 是短驻留的，不是永久切换：
- 默认 hold 一小段时间
- 连续有效动作后可自动续期
- `Esc` 可立即退出
- 错误按键不应让系统卡住

旧版文档里提到过：
- mode hold 默认约 3 秒
- 可通过配置调整

`tuiv2` 迁移阶段允许暂时简化实现，但最终行为应向这个模型收敛。

---

## 6. 与 `tuiv2` 当前实现的关系

当前 `tuiv2` 中若存在以下临时直接快捷键：
- `Ctrl-d` / `Ctrl-e`
- `Ctrl-q`
- `Ctrl-u` / `Ctrl-y`
- `Ctrl-z`
- `Ctrl-\`

它们都只能视为 **迁移期临时实现**，不是最终规范。

后续应逐步改为：
- 先进入对应 mode
- 再在 mode 内执行动作
- help/status 文案与本文档一致

---

## 7. 后续实现约束

后续所有相关改动都应遵循：

1. 修改 `input/keymap.go` 前，先检查是否符合本文档
2. 修改 `modal/help.go` 时，文案分组必须与本文档对齐
3. 修改 `render/frame.go` 的 status bar 提示时，应以本文档的 root keymap 为准
4. 新增快捷键前，必须先决定它属于哪个 mode，并先更新本文档
5. 不允许继续无文档地向 normal 状态追加临时快捷键

---

## 8. 建议的下一步

`tuiv2` 下一轮输入系统工作，应按以下顺序推进：

1. 回收当前临时直接快捷键
2. 重建 root keymap 为 `Ctrl-p/r/t/w/o/v/f/g`
3. 先实现 `pane mode` / `tab mode` / `workspace mode` 三条主链
4. 再实现 `display mode`，把 scrollback / zoom 等动作收进去
5. 最后统一 help/status/e2e 测试
