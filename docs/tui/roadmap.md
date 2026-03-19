# 路线图

本文档定义 termx TUI 从设计到实现的完整路线，分为两个阶段：技术验证（Spike）和功能实现（Build）。

## 当前进展（2026-03-19）

```
已完成 / 已落地：
  - M1 Viewport 重构核心骨架
  - M2 fit/fixed 核心交互
    - C-a M fit/fixed 切换
    - C-a P pin / unpin
    - C-a Ctrl-h/j/k/l 与 Ctrl-Arrow 平移 offset
    - fixed 模式裁剪渲染、光标跟随、边界 clamp
    - fixed 模式窗口变化不触发 PTY resize
  - M7 第一阶段渲染优化
    - render tick batching
    - View() / tab canvas / row cache
    - canvas joined-string cache / ANSI style cache
    - dirty pane content-only redraw（frame/title 未变时跳过边框重绘）
    - title cell cache / cursor-only redraw path / active-switch benchmarks
    - fixed viewport crop cache
    - fixed viewport direct visible-region rendering（避免每帧全量 VTerm grid 转换）
    - fit/live/snapshot pane direct visible-region rendering
    - SyncLost snapshot 恢复
    - catching-up 退化路径
    - fixed viewport render benchmarks（cached / recrop）
    - floating overlay damage-aware repaint（底层 dirty 时重绘受影响上层，避免闪烁/丢边框）
    - floating overlay render benchmarks（tiled dirty / floating dirty）
    - benchmark harness 跟随 chooser-first 交互更新，避免基准退化成空布局/假快路径
    - terminal picker dirty-filter benchmark（100 items）
    - terminal picker search/render cache（预计算 lowercase search text / row body / width-bound trimmed lines）
    - pane body blank-row fill cache（降低大面积 body clear 的重复构造成本）
    - incremental dirty-row tracking（简单 shell 输出优先走局部 body redraw）
    - floating overlay tiled-dirty row benchmark（验证局部重绘路径）
    - incremental dirty-span tracking（单行追加输出进一步缩小到列区间）
    - floating overlay tiled-dirty span benchmark（验证局部列区间重绘）
    - active pane 保守策略：live VTerm 仍走完整 body redraw，snapshot/非 live 可走局部 dirty span
    - terminal-level floating overlay e2e harness（Bubble Tea ANSI output -> VTerm screenshot）
    - e2e failure artifacts（latest frame + recent terminal frames dump）
    - 当前作为“可继续迭代的性能基线”封存，允许切回后续功能章节
  - M8 的 readonly 基础能力
    - C-a R toggle readonly
    - readonly 下仅 Ctrl-C 透传
  - 诊断基础设施
    - CLI / daemon / TUI 统一日志落盘
    - 支持 `--log-file` 与 `TERMX_LOG_FILE`
    - attach / chooser / pane stream / daemon transport 关键路径打点
    - transport 生命周期加固：idle client 不再阻塞 daemon shutdown
    - protocol client `Close()` 等待 read loop 退出，避免挂起请求与测试清理竞态
  - M3 Terminal Picker 第一阶段增强
    - 列表显示 running/exited、运行时长、tags
    - kill 后已观察的 viewport 保留并显示 [killed]
    - startup chooser：进入 `termx` 时先选择 attach 现有 terminal 或新建
    - `termx attach <id>` 进入完整 TUI layout，而不是 raw shell passthrough
    - 新 Viewport chooser：`C-a %` / `C-a "` / `C-a c` / `C-a w` 支持 attach 现有 terminal 或新建
    - exited viewport 恢复：活动 viewport 按 `r` 可用原 command+tags 重建新 terminal，并只重绑当前 viewport
    - exited restart 覆盖单测 + TUI e2e（chooser attach -> exit -> restart -> shell 可继续执行）
  - M4 第一阶段基础设施
    - YAML layout 解析与校验骨架
    - tag 匹配（严格 AND）与稳定排序 / _hint_id 选择器
    - 当前 workspace 导出为可回读的 layout YAML
    - LayoutSpec -> Workspace builder 骨架（匹配复用 / waiting placeholder / create plan）
    - load-layout runtime 骨架（替换 workspace、attach 已匹配 terminal、create 缺失 terminal）
    - floating layout 第一阶段集成：save-layout 导出 floating entry 尺寸，load-layout / `--layout` 可恢复 floating viewport
    - `load-layout <name> prompt` 第一阶段：未匹配 terminal 时弹 chooser，支持手动 attach / create / Esc skip
    - layout create 路径会回写声明式 tag，保证第二次 load 同一 layout 时优先复用已有 terminal
    - 命令模式接入 save-layout / load-layout / list-layouts / delete-layout
    - load-layout 支持项目级 `.termx/layouts/<name>.yaml` 优先于用户级
    - `termx --layout <name|path>` 启动直接进入 layout 恢复路径，优先于 startup chooser
    - 命令结果状态提示（save/load/list）
  - M6 第一阶段基础设施
    - `workspace-state.json` 导出 / 解析 / 运行时恢复骨架
    - TUI 正常退出后自动保存 workspace state
    - 启动优先级链第一版：`--layout` → workspace state → 项目级 `.termx/layout.yaml` → 用户级 `default-layout.yaml` → chooser
    - state 恢复对缺失 terminal 保守降级为 `[exited]` viewport，对仍在运行的 terminal 重新 attach
    - workspace state 升级为多 workspace 格式（保留单 workspace 兼容读取）
    - `C-a s` Workspace Picker 骨架（创建 / 切换 workspace）
    - workspace 切换时当前 workspace layout 保留，切回时重建 attach
    - 同一个 terminal 可被不同 workspace 独立观察

进行中：
  - M5 浮动层第一阶段
    - Tab 支持 floating layer 数据结构与渲染
    - C-a w 通过 chooser 创建 / attach floating viewport（默认 fixed）
    - C-a W toggle floating layer show/hide
    - C-a Tab cycle floating focus
    - C-a ] / C-a _ 调整 floating z-order
    - Esc 从 floating focus 返回 tiling focus
    - C-a Alt-h/j/k/l 移动浮窗
    - C-a Alt-H/J/K/L 调整浮窗大小
    - 外层 resize 时 floating rect clamp
    - 浮窗标题显示 [floating] / [floating z:n]
    - help/status 暴露 floating layer 状态与快捷键
    - 浮窗渲染回归测试（边框/标题/底层重绘不遮挡）
    - 浮窗 terminal-frame e2e（持久边框 / hide-show）
    - 浮窗 terminal-frame e2e（z-order / top window 可见性）
  - M4 load/save-layout 运行时集成
  - M4 / M5 完成后继续推进 M6 Workspace 管理
  - 后续里程碑按 roadmap 继续推进

性能基线已封存（可后续继续优化，不阻塞功能开发）：
  - 当前基线目标已达成：有 benchmark、有 row/span dirty path、有 e2e/real-e2e 回归覆盖
  - 当前策略：
    - non-active pane：可走 dirty row / dirty span
    - active live pane：仍保守走完整 body redraw，优先保证正确性
  - 后续 TODO（不阻塞 roadmap 继续）：
    - active + live VTerm 的安全局部重绘
    - 高频输出下 dirty region 合并 / 批处理
    - pprof 拆解 `fill` / `drawSourceRegion` / `String()`
    - 更大规模 benchmark：多浮窗 / 多 tab / 长时间输出
    - 如后续功能引入新渲染热点，再回到 M7 第二阶段集中处理
```

## 阶段一：技术验证（Spike）

在写任何正式代码之前，先验证设计中的关键假设。每个 spike 是一个独立的小实验，产出是"可行/不可行 + 备选方案"。

### Spike 顺序和依赖关系

```
S1 Bubble Tea render tick
 │
 ├──→ S2 行级 diff 输出（依赖 S1 结论）
 │
 └──→ S3 背压与 SyncLost 恢复（依赖 S1 结论）

S4 组合键可识别性（独立，可并行）

S5 fixed 模式 VTerm 尺寸（独立，可并行）
 │
 └──→ S6 多 Viewport VTerm 一致性（依赖 S5 结论）

时间线：
  Week 1: S1 + S4 + S5 并行
  Week 2: S2 + S3 + S6（依赖前一周结论）
```

### S1. Bubble Tea render tick（P0，对应 V1）

```
目标：验证 paneOutputMsg 的 Update() 返回 nil 时 Bubble Tea 是否跳过 View()

做法：
  1. 写一个最小 bubbletea 程序
  2. 高频发送自定义 Msg（1000/s，模拟 PTY 输出）
  3. Update 中只修改 model 状态，返回 nil
  4. 用 tea.Tick 16ms 发 renderTickMsg
  5. 在 View() 中计数，打印实际调用频率

预期结果：View() 调用频率 ≈ 60fps，不是 1000fps

如果失败：
  备选 A：View() 内部做时间节流（记录上次渲染时间，间隔不足返回缓存字符串）
  备选 B：用 tea.Program 的 WithoutRenderer + 自己控制输出

产出：spike-render-tick/ 目录，包含测试程序和结论
```

### S4. 组合键可识别性（P0，对应 V11）

```
目标：验证 C-a Ctrl-hjkl、C-a Alt-hjkl、C-a Tab 等组合键在主流终端中能否被识别

做法：
  1. 写一个最小 bubbletea 程序，打印收到的 tea.KeyMsg 详情
  2. 在以下终端中逐个测试：
     - Alacritty / kitty / WezTerm / iTerm2
     - GNOME Terminal / macOS Terminal.app
     - tmux 内嵌场景
     - SSH 远程
  3. 记录每个终端对每个组合键的识别结果

关键风险：
  - Ctrl-h = Backspace (0x08)，可能无法区分
  - Alt 键的 ESC 前缀 vs Meta bit 差异
  - Ctrl-Arrow 的 escape sequence 不统一

如果部分键不可用：
  - C-a Ctrl-hjkl 不可用 → 改用 C-a o + hjkl（两步：C-a o 进入 offset 模式）
  - C-a Alt-hjkl 不可用 → 改用 C-a m + hjkl（两步：C-a m 进入 move 模式）
  - 降级为子模式方案，牺牲一点速度换取兼容性

产出：spike-keybindings/ 目录，包含测试程序和兼容性矩阵
```

### S5. Fixed 模式 VTerm 尺寸（P0，对应 V5）

```
目标：验证 fixed Viewport 的本地 VTerm 应该用什么尺寸，以及 PTY resize 后的行为

做法：
  1. 创建 Terminal (80x24)
  2. 两个 goroutine 同时 Subscribe
  3. goroutine A 的 VTerm 用 80x24（= PTY 尺寸）
  4. goroutine B 的 VTerm 用 40x12（= viewport 显示尺寸）
  5. 在 Terminal 里跑 vim，然后 resize PTY 到 120x40
  6. 对比 A 和 B 的 VTerm 屏幕内容

关键问题：
  - B 的 VTerm 尺寸 ≠ PTY 尺寸时，escape sequence 解析是否正确？
  - PTY resize 后，B 的 VTerm 是否需要同步 resize？
  - 如果需要同步，server 是否需要广播 resize 事件？

预期结论：
  fixed Viewport 的 VTerm 应该用 PTY 的真实尺寸，不是 viewport 的显示尺寸
  → server 需要在 resize 时广播事件给所有 subscriber
  → fixed Viewport 收到 resize 事件后 resize 本地 VTerm，但不改变显示区域大小

产出：spike-fixed-vterm/ 目录，包含测试程序和结论
```

### S2. 行级 diff 输出（P2，对应 V2，依赖 S1）

```
目标：验证 Bubble Tea renderer 的 diff 策略，以及我们能否在 canvas.String() 中做行级优化

做法：
  1. 阅读 bubbletea renderer 源码（tea/renderer.go）
  2. 确认 alt screen renderer 的 diff 粒度
  3. 如果是行级 diff：我们只需要保证变化行的字符串不同即可
  4. 如果是全量重绘：我们需要在 canvas.String() 中用 cursor motion 跳过 clean 行

产出：结论文档，是否需要自定义 renderer
```

### S3. 背压与 SyncLost 恢复（P1，对应 V3，依赖 S1）

```
目标：验证高频输出下的消息队列行为和 SyncLost 恢复路径

做法：
  1. 创建 Terminal 跑 `yes` 或 `cat /dev/urandom | head -c 10M`
  2. TUI attach 该 Terminal
  3. 观察：内存使用、CPU、渲染帧率
  4. 故意让 fanout subscriber channel 满，触发 SyncLost
  5. 验证 Snapshot 恢复后屏幕内容是否正确

产出：压力测试程序和性能数据
```

### S6. 多 Viewport VTerm 一致性（P0，对应 V4，依赖 S5）

```
目标：验证同一 Terminal 被多个 Viewport 观察时 VTerm 是否保持一致

做法：
  1. 创建 Terminal，两个 goroutine 同时 Subscribe
  2. 各自维护独立的 VTerm
  3. 跑 htop / vim 等全屏程序 10 分钟
  4. 定期对比两个 VTerm 的屏幕内容（逐 cell 比较）
  5. 中途触发一次 SyncLost + Snapshot 恢复
  6. 恢复后再次对比

产出：一致性测试程序和结论
```

## 阶段二：功能实现（Build）

Spike 通过后，按以下顺序实现。每个里程碑是一个可用的增量。

### 里程碑依赖关系

```
M1 Viewport 重构
 │
 ├──→ M2 fit/fixed 模式
 │     │
 │     └──→ M5 浮动层
 │
 ├──→ M3 Terminal Picker
 │
 └──→ M4 声明式布局
       │
       └──→ M6 Workspace 管理

M7 渲染优化（可在任意里程碑后开始）

M8 AI 场景验证（可在 M1 后开始）
```

### M1. Viewport 重构

```
当前状态：TUI 的 pane 直接绑定 Terminal，没有 Viewport 抽象
目标：引入 Viewport 层，pane → viewport → terminal 解耦

改动：
  - 新增 Viewport struct（terminal_id, mode, offset, pin, readonly）
  - Pane 改为持有 Viewport 而不是直接持有 Terminal
  - 同一个 Terminal 可以被多个 Viewport 引用
  - 关闭 Viewport ≠ kill Terminal（当前是关 pane = kill）

验收标准：
  - 可以在两个 pane 里 attach 同一个 Terminal，输出实时同步
  - 关闭一个 pane，另一个 pane 不受影响
  - Terminal Picker (C-a f) 能列出所有 Terminal，包括 orphan
```

### M2. fit/fixed 模式

```
依赖：M1

改动：
  - Viewport 支持 fit/fixed 模式切换 (C-a M)
  - fit 模式：resize viewport → resize terminal PTY
  - fixed 模式：不发 resize，裁剪显示
  - 光标跟随（默认）和 pin 锚定 (C-a P)
  - pinned 状态下手动平移 offset

验收标准：
  - fit 模式下 resize 行为与当前一致
  - fixed 模式下 Terminal 保持原始尺寸，viewport 裁剪显示
  - pin 后光标移出可见区域不会自动平移
  - 手动平移 offset 正常工作
```

### M3. Terminal Picker 增强

```
依赖：M1

改动：
  - Picker 显示所有 Terminal（包括 orphan）
  - 搜索范围：command、tags、当前 workspace/tab 位置
  - Enter = attach 到当前 Viewport（替换）
  - Tab = 分屏 + attach
  - C-k = kill Terminal
  - 显示 Terminal 状态（running/exited）、运行时间、tag

验收标准：
  - 能搜索到 orphan Terminal 并 attach
  - Tab 键分屏 + attach 一步到位
  - Kill 后所有观察该 Terminal 的 Viewport 显示 [killed]
```

### M4. 声明式布局

```
依赖：M1

改动：
  - 实现 YAML 布局文件解析
  - 实现 tag 匹配机制（严格 AND + 稳定排序）
  - 实现 save-layout 命令（从运行时状态生成 YAML）
  - 实现 load-layout 命令
  - 实现 _hint_id 机制
  - 匹配不到时的三种策略：create / prompt / skip

验收标准：
  - termx --layout dev 能从 YAML 文件恢复布局
  - save-layout 生成的 YAML 能被 load-layout 正确加载
  - 第二次 load 同一个 layout 能匹配到已有 Terminal（不重复创建）
```

### M5. 浮动层

```
依赖：M2

改动：
  - Tab 支持平铺层 + 浮动层
  - C-a w 创建浮动 Viewport
  - C-a W 切换浮动层显示/隐藏
  - 浮动 Viewport 的移动、resize、z-order 操作
  - 焦点在平铺层和浮动层之间切换
  - 画家算法渲染（从底到顶）

验收标准：
  - 浮动 Viewport 覆盖在平铺层之上
  - 多个浮动 Viewport 的 z-order 正确
  - 焦点切换流畅
  - 外层 resize 时浮动 Viewport 位置 clamp 正确
```

### M6. Workspace 管理

```
依赖：M4

改动：
  - 多 Workspace 支持
  - C-a s Workspace Picker
  - Workspace 切换不影响 Terminal 运行
  - workspace-state.json 自动保存/恢复
  - 启动优先级链（--layout → state → 项目级 → 用户级 → 空白）
  - 项目级 .termx/layout.yaml 自动发现

验收标准：
  - 可以创建多个 Workspace，各自独立布局
  - 同一个 Terminal 可以出现在不同 Workspace 中
  - 退出 TUI 后重新启动，布局自动恢复
```

### M7. 渲染优化

```
依赖：S1-S3 的 spike 结论

改动（根据 spike 结论调整）：
  - render tick batching（如果 S1 验证通过）
  - Viewport 级 dirty tracking
  - 行级 dirty（如果 S2 验证通过）
  - 背压退化路径（catching up 状态）
  - 最小尺寸折叠

验收标准：
  - `yes` 命令不会导致 TUI 卡死
  - 只有一个 Viewport 有输出时，其他 Viewport 不重绘
  - 高频输出 Viewport 自动降级，不影响其他 Viewport 的交互响应
```

### M8. AI 场景验证

```
依赖：M1

改动：
  - 验证 AI agent 通过 API 操作 Terminal 的完整流程
  - 验证人机共写（人通过 TUI + AI 通过 API 同时操作同一个 Terminal）
  - readonly 模式（C-a R）
  - Ctrl-C 在 readonly 下仍然透传

验收标准：
  - 外部程序通过 termx API 创建 Terminal、写入命令、读取输出
  - TUI 实时显示 AI agent 的操作
  - readonly 模式下除 Ctrl-C 外不转发输入
```

### M9. 键位迁移（v1 → v2 分层 prefix）

```
依赖：M5（浮动层基本完成后再迁移，避免改两次）

参考：interaction-v2.md 的变更记录和对照表

第一步：引入子 prefix 分发框架
  - 在 handlePrefixEvent 里加入 t/w/o/v 四个子 prefix 入口
  - 新增 floatingMode / offsetPanMode / subPrefix 状态字段
  - 实现 handleTabSubPrefix / handleWorkspaceSubPrefix /
    handleFloatingMode / handleViewportSubPrefix / handleOffsetPanMode
  - 同时保留所有 v1 直接键作为兼容

第二步：逐步移除 v1 直接键
  - 确认子模式稳定后，移除 v1 的冗余绑定
  - 保留 C-a c 作为 C-a t c 的快捷方式（高频操作例外）
  - help 浮窗更新为 v2 键位

验收标准：
  - 所有子模式正常工作（one-shot 和 sticky）
  - sticky 模式下状态栏显示当前可用操作
  - Esc 退出 sticky 模式可靠
  - 无 Ctrl-组合或 Alt-组合键
```

## 总览

```
阶段一：技术验证（~2 周）
  Week 1: S1 + S4 + S5（并行）
  Week 2: S2 + S3 + S6（依赖前一周）

阶段二：功能实现
  M1 Viewport 重构        ← 核心，所有后续都依赖它
  M2 fit/fixed 模式       ← termx 的差异化能力
  M3 Terminal Picker 增强  ← 杀手功能
  M4 声明式布局            ← 高级功能
  M5 浮动层               ← 高级功能
  M6 Workspace 管理        ← 高级功能
  M7 渲染优化              ← 可在任意阶段穿插
  M8 AI 场景验证           ← 可在 M1 后开始
  M9 键位迁移 v1→v2        ← M5 完成后

关键路径：S1/S4/S5 → M1 → M2 → M5 → M9
```
