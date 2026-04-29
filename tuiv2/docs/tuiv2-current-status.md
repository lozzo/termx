# TUIV2 当前状态与目标

状态：Active
日期：2026-04-03

> **目标一句话**
> `tuiv2` 现在已经具备可工作的 MVP；当前阶段的任务不是继续堆功能，而是把输入系统、工作流和日常使用体验收口到一个稳定、可维护、可持续演进的状态。

---

## 1. 当前状态（已完成）

### 1.1 MVP 已打通

当前 `termx` 默认直接启动 `tuiv2`，已经具备以下能力：

- 启动 `termx` 后进入 TUI 主界面
- startup 路径能打开 terminal picker
- picker 中可创建新 terminal
- shell 能在 pane 中真正跑起来
- 键盘输入能送进 shell
- shell 输出能在 pane 中渲染
- 全屏程序（如 `htop` / `vim` / `less`）的基本终端链路已打通
- 已有真实端到端测试覆盖：创建 shell → attach → 输出渲染 → 输入交互

### 1.2 启动 / 恢复已打通

- `tuiv2` 现在是默认入口，旧 `--tui-v2` 过渡开关已经移除
- 启动时读取 workspace state 文件
- 空状态时 fallback 到默认 startup
- restore 后会自动对有 `TerminalID` 的 pane 执行 re-attach
- re-attach 失败时会静默清除失效绑定，避免保留脏状态
- 关键结构变更后会自动保存状态

### 1.3 核心结构操作已具备

已经打通的主要结构行为：

- pane split / close / focus
- tab create / switch / close / jump / rename / kill
- workspace picker 基础路径
- workspace create / rename / delete / prev / next
- zoom pane
- floating pane create + 基础渲染
- help overlay
- terminal picker / terminal manager / create-terminal prompt 基础路径
- scrollback 基础上下滚动
- save + quit 基础链路
- Terminal Pool 底栏快捷键在内容溢出时仍会保留
- protocol client 已串行化发送，避免 `list terminals` 等并发请求导致 `broken pipe`

### 1.4 代码与验证状态

当前基线验证通过：

```bash
cd /path/to/termx-monorepo/tuiv2
PATH="$PWD/.toolchain/go/bin:$PATH" go test ./...
PATH="$PWD/.toolchain/go/bin:$PATH" go test ./app -count=1
```

---

## 2. 当前最重要的结论

### 2.1 当前基线的重点是“真实状态”，不是补想象中的功能

现在最需要做的，是把“已经实现”和“尚未实现”清楚分开：

- 已实现能力继续保持可编译、可测试、可回归
- 未实现动作只保留为待办，不通过测试或文档暗示它们已可用
- help / status / 输入模式 / 测试只描述真实存在的行为

### 2.2 快捷键系统必须回归 prefix / mode 结构

`tuiv2` 未来的输入系统必须以：

- `Ctrl-p` pane mode
- `Ctrl-r` resize mode
- `Ctrl-t` tab mode
- `Ctrl-w` workspace mode
- `Ctrl-o` floating mode
- `Ctrl-v` display mode
- `Ctrl-f` terminal picker
- `Ctrl-g` global mode

为唯一 root keymap。

具体规范见：

- `tuiv2-keybinding-spec.md`

这份文档现在是 **唯一 canonical keybinding 文档**。

### 2.3 当前允许存在迁移期实现，但不允许无规范漂移

为了快速打通 MVP，曾经引入过一些直接快捷键（如直接 split / quit / scrollback）。

这些实现可以暂时存在于内部能力层，但：

- 不再作为最终规范
- 不再继续扩散
- 必须逐步收回到对应 mode
- help / status / 测试 都必须以 canonical 文档为准

---

## 3. 当前阶段目标

### 3.1 第一目标：完成 canonical keymap 收口

当前最优先目标是：

- root keymap 全部稳定为 `Ctrl-p/r/t/w/o/v/f/g`
- normal 模式只保留 root 入口，不继续堆直接动作
- pane / tab / workspace / display / global mode 的二段式动作补齐
- help overlay 与 status bar 与规范一致
- 输入相关测试和 e2e 文案全部跟规范同步

### 3.2 第二目标：把“能工作”推进到“可日常使用”

在 keymap 收口之后，继续完成：

- pane swap / reconnect / close+kill
- floating move / resize / center / toggle / close

### 3.3 第三目标：再考虑进一步 polish

在前两项完成前，不应该优先做：

- 新增更多快捷键
- 大范围 UI 美化
- 更多概念扩展
- 与 keymap 规范冲突的临时交互

---

## 4. 当前剩余重点工作

### A. Canonical keymap 收口（最高优先级）

要完成的事情：

- root keymap 全部回到 `Ctrl-p/r/t/w/o/v/f/g`
- 各 mode 的动作映射补齐
- 清理迁移期临时直接快捷键路径
- 保证 help/status/tests 一致

### B. Terminal Pool / Global 工作流收口

当前状态：

- global → Terminal Pool 已切到 first-class page
- picker / prompt / help / workspace picker 仍保持 overlay
- Terminal Pool 不再与 modal session 混用
- attach picker 已过滤 exited terminal，避免对不可附着对象发 attach

### C. 尚未实现但已明确保留为待办的动作

当前测试基线里，这些动作只作为待办名义保留，不代表已可用：

- pane: `swap-pane-left` / `reconnect-pane` / `close-pane-kill`
- floating: `move-floating-right` / `resize-floating-right` / `center-floating-pane` / `toggle-floating-visibility` / `close-floating-pane`

## 5. 当前工作的边界约束

后续所有实现都应遵守：

1. `tuiv2-keybinding-spec.md` 是快捷键唯一规范
2. 不允许继续无文档地向 normal 模式添加新快捷键
3. `tuiv2/` 不得重新依赖旧 `tui/`
4. `workbench.PaneState.TerminalID` 仍是唯一可写绑定真相
5. 每完成一轮输入系统调整，都必须至少跑 `go test ./...`

---

## 6. 建议的下一步顺序

1. 完成 canonical keymap 回滚的剩余部分
2. 补 pane / floating / global mode 剩余二段式动作
3. 继续补手工 smoke test

---

## 7. 当前权威文档

保留并继续使用：

- `tuiv2-current-status.md`（本文件）
- `tuiv2-keybinding-spec.md`
- `tui-v2-migration-architecture-plan.md`
- `tui-product-definition-design.md`
- `tuiv2-render-migration-guide.md`

其中：

- 本文件负责描述“当前做到哪、下一步做什么”
- `tuiv2-keybinding-spec.md` 负责定义快捷键规范
