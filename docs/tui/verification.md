# 待验证事项

本文档列出设计文档中尚未经过实际验证的假设和方案。在实现前需要逐项验证，确认可行后再编码。

状态标记：
- `[ ]` 未验证
- `[~]` 验证中
- `[x]` 已验证
- `[!]` 验证失败，需要调整方案

---

## V1. Bubble Tea render tick batching

来源：[rendering.md](rendering.md) — Batching 机制

假设：paneOutputMsg 的 Update() 返回 nil cmd 时，Bubble Tea 不会调用 View()。只有 renderTickMsg 返回非 nil cmd 时才触发 View()。

```
需要验证：
  [ ] bubbletea 在 Update 返回 (model, nil) 时是否真的跳过 View()
  [ ] 如果不跳过，是否可以通过其他方式控制 View() 频率
  [ ] tea.Tick 的精度在高频消息下是否稳定（16ms tick 会不会被消息队列延迟）
  [ ] 多个 tea.Tick 同时 pending 时的行为（是否会堆积）

验证方法：
  写一个最小 bubbletea 程序：
  - 高频发送自定义 Msg（模拟 paneOutputMsg）
  - Update 中只修改 model 状态，返回 nil
  - 用 tea.Tick 16ms 发 renderTickMsg
  - 在 View() 中计数，观察调用频率
  - 对比：不用 tick，每次 Update 都触发 View 的帧率

风险：
  如果 bubbletea 每次 Update 都调 View()，整个 batching 方案需要重新设计。
  备选方案：在 View() 内部做时间节流（记录上次渲染时间，间隔不足则返回缓存字符串）
```

## V2. Bubble Tea 行级 diff 输出

来源：[rendering.md](rendering.md) — 第二级：行级 dirty

假设：bubbletea 的 alt screen renderer 对 View() 返回的字符串做行级 diff，只输出变化的行。

```
需要验证：
  [ ] bubbletea renderer 的实际 diff 策略是什么（行级？全量？）
  [ ] 如果我们在 canvas.String() 中用 cursor motion 跳过 clean 行，
      bubbletea renderer 是否会干扰（二次 diff 导致闪烁）
  [ ] bubbletea 是否支持自定义 renderer 或 raw write 模式

验证方法：
  阅读 bubbletea renderer 源码（tea/renderer.go）
  重点关注 repaint / altScreenRenderer 的实现
```

## V3. 高频输出下的消息队列背压

来源：[rendering.md](rendering.md) — 背压与退化

假设：当 PTY 输出速度 > TUI 处理速度时，bubbletea 的消息队列会堆积 paneOutputMsg。

```
需要验证：
  [ ] bubbletea 的消息 channel 是否有 buffer 限制
  [ ] 消息堆积时 bubbletea 的行为（阻塞发送方？丢弃？OOM？）
  [ ] 我们的 fanout subscriber channel 满时，丢弃 + SyncLost 的恢复路径是否可靠
  [ ] Snapshot 请求在高频输出下的延迟和正确性

验证方法：
  写压力测试：
  - 创建一个 Terminal 跑 `yes` 或 `cat /dev/urandom`
  - TUI attach 该 Terminal
  - 观察内存使用、CPU、渲染帧率
  - 触发 SyncLost 后验证 Snapshot 恢复是否正确
```

## V4. 同一 Terminal 多 Viewport 的 VTerm 一致性

来源：[model.md](model.md) — 布局系统

假设：同一个 Terminal 被多个 Viewport 观察时，每个 Viewport 有自己的 VTerm 副本，通过 Subscribe 保持同步。

```
需要验证：
  [ ] 多个 VTerm 副本在长时间运行后是否会出现状态漂移
  [ ] SyncLost 恢复后，多个 Viewport 的 VTerm 是否能重新对齐
  [ ] 一个 fit Viewport resize 了 Terminal，其他 fixed Viewport 的 VTerm 如何处理
      （fixed Viewport 的 VTerm 尺寸 ≠ PTY 尺寸，屏幕内容如何映射？）

验证方法：
  - 创建一个 Terminal，两个 Viewport 同时 Subscribe
  - 跑 htop / vim 等全屏程序
  - 在一个 Viewport 里 resize
  - 对比两个 Viewport 的 VTerm 屏幕内容
```

## V5. Fixed 模式下的 VTerm 尺寸问题

来源：[model.md](model.md) — fixed 模式

假设：fixed 模式的 Viewport 不发 resize 给 Terminal，只裁剪显示。但 Viewport 本地的 VTerm 需要知道 Terminal 的真实 PTY 尺寸才能正确解析 escape sequence。

```
需要验证：
  [ ] fixed Viewport 的本地 VTerm 应该用什么尺寸初始化？
      a. 用 Terminal 的 PTY 尺寸（需要从 server 获取并跟踪变化）
      b. 用 Viewport 自己的显示尺寸（escape sequence 解析可能错误）
  [ ] 当 fit Viewport resize 了 PTY，fixed Viewport 的 VTerm 是否需要同步 resize？
  [ ] 如果需要同步，server 是否需要广播 resize 事件给所有 subscriber？

验证方法：
  - 创建 Terminal (80x24)
  - fit Viewport A (80x24) + fixed Viewport B (40x12)
  - 在 A 里跑 vim
  - resize A 到 120x40
  - 观察 B 的 VTerm 是否正确解析 vim 的重绘输出
```

## V6. 浮动 Viewport 的 z-order 渲染性能

来源：[rendering.md](rendering.md) — 浮窗 z-order 渲染

假设：画家算法（从底到顶逐层绘制）对 1-3 个浮窗的典型场景性能足够。

```
需要验证：
  [ ] 3 个浮窗覆盖 50% 屏幕面积时，View() 耗时是否在 16ms 内
  [ ] 浮窗移动/resize 时的渲染帧率是否流畅
  [ ] 是否需要提前引入 visible mask 优化

验证方法：
  在当前 TUI 代码中实现简单的浮窗渲染
  用 pprof 测量 View() 耗时
  对比有/无浮窗的性能差异
```

## V7. Workspace state 自动保存/恢复

来源：[layout.md](layout.md) — 启动优先级表

假设：TUI 退出时自动保存 workspace state 到 JSON，启动时自动恢复。

```
需要验证：
  [ ] TUI 被 kill -9 时能否保存 state（可能需要定期自动保存而非仅退出时保存）
  [ ] state 文件中的 Terminal ID 在 daemon 重启后是否仍然有效
      （daemon 重启 = 所有 Terminal 丢失，state 文件全部失效）
  [ ] 多个 TUI 实例同时运行时，state 文件是否冲突
      （每个实例保存自己的 state？还是合并？）

验证方法：
  设计 state 文件格式
  考虑多实例场景的文件命名策略
```

## V8. Tag 匹配的性能

来源：[layout.md](layout.md) — Tag 匹配机制

假设：tag 匹配在 Terminal 数量 < 100 时性能不是问题，线性扫描即可。

```
需要验证：
  [ ] 实际使用中 Terminal 数量的上限预期（10? 50? 100?）
  [ ] 如果需要支持 100+ Terminal，是否需要 tag 索引
  [ ] 多 tag AND 匹配 + 排序的实际耗时

验证方法：
  暂不需要，先假设 < 100 足够。
  如果后续有大规模场景再评估。
  标记为低优先级。
```

## V9. GC 宽限期的实现

来源：[model.md](model.md) — GC 规则

假设：exited + refcount=0 的 Terminal 在 5 分钟宽限期后自动回收。

```
需要验证：
  [ ] GC 计时器放在 server 端还是 client 端？
      server 端：所有客户端断开后统一计时，更合理
      client 端：只有 TUI 知道 refcount，但多客户端时不准确
  [ ] daemon 重启后 GC 计时器是否需要持久化？
  [ ] 宽限期内 daemon 重启，exited Terminal 的 VTerm 缓冲区是否丢失？

验证方法：
  确定 GC 的架构归属（server vs client）
  设计 server 端的 Terminal 状态持久化方案（如果需要）
```

## V10. 自动 tag 的 ws/tab 名称同步

来源：[model.md](model.md) — 自动 Tag

**已定：tag 是"出生证明"，不随 rename/move 更新。** 见 model.md 自动 Tag 部分。

```
剩余验证项：
  [x] 语义已定：出生证明，不是当前地址
  [ ] Picker 搜索是否需要同时搜 tag 和 Viewport 当前位置（实现时确认 UX）
```

---

## V11. 组合键在不同终端中的可识别性

来源：[interaction.md](interaction.md) — Fixed 模式完整交互、浮动层键盘操作

假设：`C-a Ctrl-hjkl`、`C-a Alt-hjkl`、`C-a Ctrl-Arrow`、`C-a Tab` 等组合键能被 TUI 稳定接收。

```
需要验证：
  [ ] C-a Ctrl-h — Ctrl-h 在很多终端里等价于 Backspace (0x08)
      bubbletea 能否区分 Ctrl-h 和 Backspace？
  [ ] C-a Alt-h/j/k/l — Alt 在不同终端里的表现：
      - 有些终端发 ESC 前缀 (ESC + h)
      - 有些终端发 Meta bit (0x80 | h)
      - bubbletea 的 Alt 键处理是否统一？
  [ ] C-a Ctrl-←/→/↑/↓ — Ctrl+Arrow 的 escape sequence 不统一：
      - xterm: ESC[1;5A
      - rxvt: ESC[Oa
      - 有些终端根本不支持
  [ ] C-a Tab — Tab 键在 prefix 后是否能被正确识别
      （Tab = 0x09，和 Ctrl-I 相同）

验证方法：
  在以下终端中测试所有组合键的可识别性：
  - macOS Terminal.app
  - iTerm2
  - Alacritty
  - kitty
  - WezTerm
  - GNOME Terminal / xterm
  - tmux 内嵌（termx 跑在 tmux 里的场景）
  - SSH 远程连接

  写一个最小 bubbletea 程序，打印收到的 tea.KeyMsg：
  - 按下每个组合键，记录 Type / Runes / Alt 字段
  - 标记哪些终端无法区分的键

降级方案（如果某些键不可用）：
  C-a Ctrl-h 不可用 → 改用 C-a Alt-Shift-h 或其他组合
  C-a Alt-* 不可用  → 改用 C-a 后接两次按键（如 C-a o h = offset left）
  C-a Ctrl-Arrow 不可用 → 只保留 C-a Ctrl-hjkl，删除 Arrow 变体
```

---

## 优先级排序

| 优先级 | 编号 | 事项 | 理由 |
|--------|------|------|------|
| P0 | V1 | render tick batching | 渲染架构的基础假设，错了要重新设计 |
| P0 | V5 | fixed 模式 VTerm 尺寸 | 影响核心 Viewport 模型的可行性 |
| P0 | V4 | 多 Viewport VTerm 一致性 | 影响 Terminal 复用的核心 feature |
| P0 | V11 | 组合键可识别性 | 键位方案不可用则交互设计要改 |
| P1 | V3 | 背压和 SyncLost 恢复 | 影响稳定性，但有降级方案 |
| P1 | V9 | GC 架构归属 | 影响 server/client 职责划分 |
| P1 | V10 | 自动 tag 语义 | 已定为"出生证明"，剩余 Picker UX 待确认 |
| P2 | V2 | 行级 diff | 优化项，不影响正确性 |
| P2 | V6 | 浮窗渲染性能 | 典型场景下大概率没问题 |
| P2 | V7 | state 保存恢复 | 非 MVP 功能 |
| P3 | V8 | tag 匹配性能 | 短期内不会有 100+ Terminal |
