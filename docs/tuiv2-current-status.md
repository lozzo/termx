# TUIV2 当前状态与目标

状态：Active
日期：2026-03-31

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
- tab create / switch / close
- workspace picker 基础路径
- zoom pane
- floating pane 基础渲染
- help overlay
- stream recovery 基础重连
- scrollback 基础查看
- save + quit 基础链路

### 1.4 代码与验证状态

当前基线验证通过：

```bash
cd /home/lozzow/workdir/termx
PATH="$PWD/.toolchain/go/bin:$PATH" go build ./...
PATH="$PWD/.toolchain/go/bin:$PATH" go test ./tuiv2/...
```

并且：

```bash
grep -r '"github.com/lozzow/termx/tui"' tuiv2/
```

应保持空输出。

---

## 2. 当前最重要的结论

### 2.1 MVP 不是问题，规范收口才是问题

现在最需要做的，不是再证明一次 shell 能跑，而是：

- 把快捷键体系收口
- 把 help / status / mode 文案统一
- 把“临时为 MVP 加的直接快捷键”回收到正式结构
- 把剩余功能放回明确的 mode / workflow 里

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

- `docs/tuiv2-keybinding-spec.md`

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

- workspace mode 的完整二段式动作
- global mode 中 terminal manager 入口
- resize / floating mode 的最小可用动作或明确行为
- 错误消息自动清除
- OSC 2 标题更新

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

### B. Workspace / Global / Manager 工作流补齐

当前还需要补：

- workspace mode 内动作
- global mode 内动作
- global → terminal manager 路径
- terminal manager 与全局管理行为的边界

### C. 错误与标题体验补齐

仍待完成：

- 错误消息 5 秒自动清除
- OSC 2 标题更新到 pane title

---

## 5. 当前工作的边界约束

后续所有实现都应遵守：

1. `docs/tuiv2-keybinding-spec.md` 是快捷键唯一规范
2. 不允许继续无文档地向 normal 模式添加新快捷键
3. `tuiv2/` 不得重新依赖旧 `tui/`
4. `workbench.PaneState.TerminalID` 仍是唯一可写绑定真相
5. 每完成一轮输入系统调整，都必须跑 build + `go test ./tuiv2/...`

---

## 6. 建议的下一步顺序

1. 完成 canonical keymap 回滚的剩余部分
2. 补 workspace mode / global mode 二段式动作
3. 接上 terminal manager 的正式入口
4. 做 error auto-clear
5. 做 OSC 2 title update

---

## 7. 当前权威文档

保留并继续使用：

- `docs/tuiv2-current-status.md`（本文件）
- `docs/tuiv2-keybinding-spec.md`
- `docs/tui-v2-migration-architecture-plan.md`
- `docs/tui-product-definition-design.md`
- `docs/tuiv2-render-migration-guide.md`

其中：

- 本文件负责描述“当前做到哪、下一步做什么”
- `tuiv2-keybinding-spec.md` 负责定义快捷键规范
