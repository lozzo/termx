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

当前补充结论（2026-03-19）：
  [x] client `Close()` 会等待协议 read loop 退出，并同步失败所有挂起请求
  [x] daemon shutdown / context cancel 不会再被 idle transport session 卡住
      （已有回归测试覆盖 unix socket idle client 场景）
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

当前补充结论（2026-03-19）：
  [x] benchmark harness 已修正为 chooser-first 工作流，避免把“未真正创建 pane/floating”当成性能结果
  [x] 样本基准（Intel Xeon E5-2450 v2）：
      - floating tiled-dirty: `~2.02ms/op`（整 pane dirty，保守上界）
      - floating tiled-dirty rows: `~0.54ms/op`（局部 row dirty）
      - floating tiled-dirty span: `~0.45-0.52ms/op`（单行列区间 dirty）
      - floating active tiled-dirty span: `~2.40ms/op`（live active pane 仍保守全量 redraw）
      - floating floating-dirty: `~0.36-0.55ms/op`
      - floating floating-move: `~2.40ms/op`
      - fixed viewport recrop: `~0.14ms/op`
      - terminal picker dirty filter (100 items): `~0.60ms/op`
      - batched output + render tick: `~0.03-0.04ms/op`
  [x] terminal picker dirty path 已通过缓存显著收敛：
      - 约 `8.07ms/op -> 0.60ms/op`
      - 约 `1.42MB/op -> 308KB/op`
      - 约 `2732 allocs/op -> 480 allocs/op`
  [x] pane body blank-row fill cache 已落地，render hot path 新增单测覆盖
  [x] incremental dirty-row tracking 已落地：
      - 普通单行输出优先记录 dirty row range，而不是整 pane 脏
      - 当前先应用在非 active pane 的 body redraw，避免影响正在输入的 pane 正确性
      - 遇到 escape sequence / 高风险滚屏场景时保守回退到整 pane redraw
      - 局部 row dirty benchmark 相比整 pane dirty 约 `2.02ms/op -> 0.54ms/op`
  [x] incremental dirty-span tracking 已落地：
      - 同行、无 escape、无换行的简单输出继续收紧到列区间
      - tiled dirty span benchmark 相比整 pane dirty 约 `2.02ms/op -> 0.46ms/op`
      - active pane 目前仅对 snapshot/非 live 路径启用；live active pane 仍保守走完整 body redraw
  [x] floating move damage repaint 已落地：
      - 浮窗位移/resize 不再强制丢弃整个 tab canvas cache
      - 会清理旧 rect + 新 rect 的 damage 区域，并仅重绘受影响 pane
      - floating move benchmark 约 `2.95ms/op -> 2.40ms/op`
      - 分配量约 `671KB/op -> 105KB/op`
  [x] render tick 已补交互帧节奏：
      - 空闲维持 `16ms` batching 节奏
      - 输入/鼠标交互窗口提升到 `8ms`
      - 已补单测覆盖 idle/interactive flush 间隔切换
  [x] 鼠标拖拽已补 motion coalescing：
      - 交互窗口内的高频 drag motion 会先更新 rect，再挂到下一次 fast tick flush
      - 避免每个 mouse motion 都同步重绘整帧
      - 已补单测覆盖“geometry 立即更新、render 延后到 tick flush”语义
  [x] 错误 prefix/sticky 组合键已降级为安全忽略：
      - 浮窗 sticky / offset-pan sticky 下输错键会自动退出前缀态
      - raw 输入路径同样会消费并清理 prefix，不再出现“按错后卡住”
  [x] 渲染调优日志已补：
      - render ticker 每 `10s` 输出一次 `view_calls / frames / cache_hits / fps`
      - 已补单测覆盖统计日志字段
  [x] workspace-state 解析已更稳健：
      - 允许读取首个合法 JSON 对象后忽略尾随脏数据
      - startup 遇到损坏 state 会自动降级到后续 bootstrap，而不是直接把错误抛给用户
  [x] 当前典型热点仍在 16ms 帧预算内，可作为后续功能开发的性能基线
  [x] 浮窗交互基线已补齐：
      - 新建多个浮窗会默认错位摆放，避免标题栏和边框完全重叠
      - 状态栏会显示 floating 数量、当前焦点层以及 `Esc` / `C-a Tab` 切换提示
      - 鼠标点击浮窗会聚焦并置顶，左键拖拽会移动浮窗，越界时自动 clamp
      - 已补充单测与 terminal-frame e2e 覆盖错位展示 / status hint / 鼠标拖拽边界
  [x] 可延期到后续专项的优化 TODO：
      - active + live VTerm 的安全局部重绘
      - dirty region 合并 / 批处理
      - 更细粒度 pprof 与大规模场景 benchmark

建议复跑命令：
  `PATH="/home/lozzow/workdir/termx/.toolchain/go/bin:$PATH" go test ./tui -run '^$' -bench 'Benchmark(RenderTabCompositeFloatingOverlay(TiledDirty|TiledDirtyRows|TiledDirtySpan|ActiveTiledDirtySpan|FloatingDirty|FloatingMove)|HandlePaneOutputViewBatchedWithTick|PaneCellsForViewportFixed(Cached|Recrop)|ModelViewTerminalPicker100Items(DirtyFilter)?)$' -benchmem`
```

## V7. Workspace state 自动保存/恢复

来源：[layout.md](layout.md) — 启动优先级表

假设：TUI 退出时自动保存 workspace state 到 JSON，启动时自动恢复。

```
当前补充结论（2026-03-19）：
  [x] workspace state JSON 骨架已落地，TUI 正常退出后会自动保存
  [x] 启动时会优先尝试恢复 workspace state；仍在运行的 Terminal 会重新 attach
  [x] 恢复时缺失的 Terminal 先保守显示为 `[exited]` viewport
  [x] exited viewport 现在支持原地 `r` restart：新建 terminal、复用 command+tags、仅重绑触发 restart 的 viewport
  [x] restart 已补充单测与 TUI e2e 回归（attach exited shell -> restart -> 继续执行命令）
  [x] save-layout / load-layout 第一阶段已纳入 floating viewport：
      - 导出 YAML 会保留 floating entry 的 terminal 声明和尺寸
      - save-layout 现会额外导出 floating position 锚点（center / 四角）
      - load-layout / `--layout` 会恢复 floating viewport，并支持已有 terminal attach / create 缺失 terminal
      - 已补充单测与 TUI e2e 覆盖 startup layout -> floating viewport 恢复路径
  [x] `load-layout <name> prompt` 已落地第一阶段：
      - 未匹配 terminal 时会逐个打开 chooser
      - 可手动 attach 已有 terminal、create new、或 Esc 跳过保留 waiting viewport
      - 已补充单测与 TUI e2e 覆盖 prompt attach 流程
  [x] `arrange` 第一阶段已落地：
      - `grid` / `horizontal` / `vertical` 会把匹配到的多个 terminal 展开为多 pane
      - 按现有 layout 匹配排序规则稳定展开，并避开同一 layout 内已使用 terminal
      - 无匹配时保守降级为单个 waiting viewport
      - 已补充单测与 startup-layout TUI e2e 覆盖
  [x] 启动优先级链第一版已落地：`--layout` → workspace state → 项目级自动 layout → 用户级默认 layout → chooser
  [x] state 文件已升级为多 workspace 格式，同时保持单 workspace 兼容读取
  [x] `C-a s` workspace picker 已落地第一版，可创建 / 切换 workspace
  [x] workspace 切换会保留各自布局；切回时对 running terminal 重新 attach
  [x] 同一个 Terminal 可以被不同 workspace 独立观察（有回归测试）
  [x] workspace picker 选中后的清屏已补齐：
      - 创建/切换 workspace 后不再遗留 chooser 标题、query、footer 边框残影
      - 空 workspace welcome body 改为整宽输出，避免 Bubble Tea diff 留下旧字符
      - 已补 e2e 覆盖 create/switch/Esc-close/bootstrap-then-switch-back 路径
  [x] 若恢复出的 active workspace 为空，不再停留在无边框欢迎页：
      - 有可选 terminal 时自动进入 startup chooser
      - 无可选 terminal 时自动创建首个 pane
  [x] pane runtime 保护已补齐：流式 output / resize / recover 遇到缺失 VTerm 不再 panic
  [x] picker 呈现已从左侧抽屉切到页面中部 modal：
      - startup / split / floating / new-tab / layout-resolve / workspace picker 走统一居中渲染
      - 已补单测与 e2e 回归 startup / split / floating chooser 主路径
  [x] picker / chrome 第一轮视觉收敛已完成：
      - modal 改为窄一些的居中卡片，补上 backdrop / shadow / 更完整的边框
      - picker 行项补了高亮态与 create-row 强调态
      - 顶部 tab bar 和底部状态栏改成 pill/segment 风格，默认 `1:1` 标签不再裸露显示
      - picker body 改为固定宽度背景，避免左右两侧底色不一致 / 右上角边框看起来被截断
      - picker modal 已切换为 lipgloss 直接居中渲染，不再让 ANSI 样式文本经过 canvas 宽度计算
  [x] picker 选中后的过渡输入已收口：
      - pane create / attach 完成前暂时阻断输入，避免字符漏进旧 pane
  [x] prefix / status 交互已做第二轮收敛：
      - prefix 默认时长已延长，避免必须极快连按
      - 方向移动 / pane resize / viewport pan / 浮窗 move+resize 现支持在同一 prefix 窗口内连续触发
      - 状态栏已从长句式命令提示收缩为短 chip，优先保留当前 pane / floating / focus / prefix 关键信息

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

## V11. v2 分层 prefix 迁移

来源：[interaction-v2.md](interaction-v2.md)

当前补充结论（2026-03-19）：
  [x] 第一阶段分层 prefix 状态机已落地：
      - `C-a t` / `C-a w` / `C-a v` one-shot 子前缀
      - `C-a o` floating sticky 模式
  [x] v2 子前缀已复用现有能力：
      - tab：create / rename / close
      - workspace：switch / create / rename / delete
      - viewport：mode / readonly / pin / pan / offset-pan
      - floating：create / close / hide / focus / z-order / move / resize
  [x] `C-a w` 与 v1 `new floating viewport` 冲突的兼容策略已落地：
      - 单独 `C-a w` 超时后仍回退到旧的 floating chooser
      - `C-a w s/c/r/x` 进入 workspace 子前缀
  [x] help / status 已补齐 v2 提示；floating chooser 从 sticky 模式打开后会自动退出 sticky，避免后续 shell 输入被吞
  [x] 单测、TUI e2e、render benchmark harness 已跟随新入口更新并全绿

后续未完成：
  [ ] 第二阶段移除冲突/冗余的 v1 直接键
  [ ] 按 interaction-v2.md 收紧 help 文案，切到 v2 为主、v1 为兼容别名
  [ ] 进一步评估 `C-a w` 超时兼容是否保留，还是在后续版本完全切到 `C-a o n`

## V12. AI 场景主链路

来源：[ai-scenarios.md](ai-scenarios.md)

当前补充结论（2026-03-19）：
  [x] 低层 API 主链路已具备：Create / Attach / Input / Snapshot / Subscribe / SetTags / Kill 在 e2e 覆盖下可用
  [x] readonly 规则已落地并有单测：
      - 普通输入被拦截
      - `Ctrl-C` 是 readonly 下唯一允许透传的控制键
  [x] TUI + API 协作 e2e 已补齐：
      - 外部 API 写入的 agent 输出会实时显示在已 attach 的 TUI viewport 中
      - 人类随后可在同一 terminal 中继续输入，验证共享终端状态
  [x] readonly + agent e2e 已补齐：
      - API 启动长任务（`sleep 30`）
      - TUI 切到 readonly 后仍可发送 `Ctrl-C` 中断
      - readonly 下普通输入继续被拦截

后续未完成：
  [ ] 更系统的人机共写冲突/仲裁验证（交错输入、长时间共写）
  [ ] 多 agent 编排场景的端到端回归（arrange grid + tags + 长任务）

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

来源：[interaction-v2.md](interaction-v2.md) — 分层 prefix 系统

**v2 更新**：分层 prefix 系统消除了所有 Ctrl-组合和 Alt-组合键。原来的 `C-a Ctrl-hjkl`、`C-a Alt-hjkl`、`C-a Ctrl-Arrow` 全部变成了子模式下的普通字母键。V11 的大部分风险已消除。

```
剩余需要验证的：
  [ ] C-a 后接子 prefix 键（t/w/o/v）的识别是否稳定
      → 这些都是普通字母键，风险极低
  [ ] Floating sticky 模式下 Esc 键的识别
      → Esc 在某些终端里是 Alt 键的前缀，可能有 timing 问题
      → bubbletea 通常能正确处理，但需要验证
  [ ] C-a Tab 的识别（Tab = 0x09 = Ctrl-I）
      → 已移到 C-a o 子模式内部，变成 floating 模式下的普通 Tab
      → 风险降低但仍需验证

验证方法：
  在主流终端中测试 C-a → t/w/o/v → 后续键 的完整链路
  重点验证 Esc 退出 sticky 模式的可靠性
```

---

## 优先级排序

| 优先级 | 编号 | 事项 | 理由 |
|--------|------|------|------|
| P0 | V1 | render tick batching | 渲染架构的基础假设，错了要重新设计 |
| P0 | V5 | fixed 模式 VTerm 尺寸 | 影响核心 Viewport 模型的可行性 |
| P0 | V4 | 多 Viewport VTerm 一致性 | 影响 Terminal 复用的核心 feature |
| P0 | V11 | 组合键可识别性 | v2 分层 prefix 消除了大部分风险，降为 P2 |
| P1 | V3 | 背压和 SyncLost 恢复 | 影响稳定性，但有降级方案 |
| P1 | V9 | GC 架构归属 | 影响 server/client 职责划分 |
| P1 | V10 | 自动 tag 语义 | 已定为"出生证明"，剩余 Picker UX 待确认 |
| P2 | V2 | 行级 diff | 优化项，不影响正确性 |
| P2 | V6 | 浮窗渲染性能 | 典型场景下大概率没问题 |
| P2 | V7 | state 保存恢复 | 非 MVP 功能 |
| P3 | V8 | tag 匹配性能 | 短期内不会有 100+ Terminal |
