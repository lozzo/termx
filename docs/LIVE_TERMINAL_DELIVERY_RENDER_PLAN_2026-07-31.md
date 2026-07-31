# AnyTTY 实时终端传输与渲染方案

日期：2026-07-31

代码基线：`master@d17db574`

状态：已实施，已通过测试，最终交叉复审中

## 1. 结论

当前问题不能只在 TUI renderer 内修补。需要统一处理 daemon 输出聚合、跨网络 live screen 传输、客户端 latest cache 和各前端的呈现调度，同时保持 history、PTY input、resize ownership 和 Endpoint 安全边界不变。

已落地为以下结构：

```text
PTY output
  -> daemon 有界共享 buffer（只合并当前已排队数据，不等时间窗口）
  -> daemon latest native screen + 单调 revision
  -> 客户端 LiveScreenNext(observed_revision)（单请求 + 单 pending）
  -> 客户端 received screen cache（持续合并 revision）
  -> 平台自己的单写者呈现调度器（busy 时只保留 latest dirty state）
  -> TTY / xterm / future desktop renderer
```

关键决策：

1. 不使用固定 `25ms`、`40fps` 或用户可调 FPS 窗口。收到变化后，只要当前呈现周期结束就立即处理最新状态。
2. daemon 不保存客户端待渲染队列，也不接收“已经显示到屏幕”的 ACK。`observed_revision` 表示客户端已经收到并合并的 daemon revision；protocol session 只短时保存该 revision 的紧凑行指纹基线，用它直接计算下一次 full/delta，不能把它解释为 renderer/painter 完成状态。
3. Local Unix、SSH、Direct WebRTC、Managed Cloud WebRTC 共用同一个 live screen 协议，不按 route 实现不同的渲染语义。
4. TUI、移动/Web 和未来桌面端共用 wire contract 与 revision 规则，但各自拥有呈现调度器；不建立跨语言万能 renderer。
5. 不保留旧 live invalidation 路径的兼容实现。项目尚未上线，切换时同步删除旧命令、事件消费者和死代码。

## 2. 当前问题

### 2.1 写屏完成与下一次取帧被严格串行

当前 TUI 只有在 `FrameWriteCompletion` 后才重新 arm one-shot live request。远端持续输出吞吐上限约为：

```text
1 / (网络 RTT + client merge + render + physical write)
```

因此 prompt 尾部的小输出块可能落在当前 request 的后面，表现为最后的 `ζ` 等字符需要下一次刷新才显示。增加多个并发 long-poll 不能正确解决：它们会同时被同一个 revision 唤醒、返回重复 snapshot，并占用每连接 request slot，放大 DataChannel head-of-line blocking。

### 2.2 TUI 把写屏 gate 和 live delivery ownership 混在一起

`frameWriteInFlight` 可以避免重复构建必然被覆盖的帧，但当前 completion 同时负责：

- 释放本地 writer；
- 决定下一次 live request；
- 推进 live patch baseline；
- 推断哪些 terminal 仍应订阅。

这导致多 pane、copy mode、resize 和普通 UI 更新之间存在错误耦合。一个局部 patch 的 completion 不能代表整个屏幕所有 live pane 的呈现与订阅状态。

### 2.3 live 增量丢失滚屏呈现语义

daemon 现在提供 sparse changed rows，但 TUI 把变化压成第一行至最后一行的连续 rewrite。滚屏通常会触达大部分行，于是增量路径接近整块重写，并绕过完整 FrameSink 已有的 scroll-shift 检测。

rewrite patch 完成后还会使完整帧缓存失效，下一次普通 UI 帧容易退化为清屏重绘。

### 2.4 daemon 输出 buffer 删除了原有即时批处理

旧 live ingest queue 会把当前已经排队的小块合并到最多 `16 KiB` 后再调用 vterm。当前共享 buffer 对每个 node 分别 ingest、增加 revision 并发布 invalidation。高频碎片输出因此制造更多解析、锁、revision 和通知。

### 2.5 移动/Web 仍使用另一套 live 模型

移动/Web 的 `ProtoTerminalProtocolSession` 当前订阅 `TERMINAL_LIVE_INVALIDATED`，收到事件后再执行一次不带 `observed_revision` 的完整 `LiveScreenGet`，随后生成清屏 replay 并进入 React state。

这没有使用当前 Proto 已有的 delta 字段，也会在远程连接上额外增加 RTT。未来桌面端如果继续复制这一实现，会形成第三套 live 调度模型。

## 3. 不变量

以下规则在改造中必须保持：

- daemon 的 native screen 是 live terminal 唯一真值；客户端 cache 只是 projection。
- history/copy 继续使用 frozen history contract，不能从 live screen 反推历史。
- PTY input 保持每 terminal 有序；live screen 慢消费不能阻塞 input ACK。
- resize owner、attachment、EndpointID、daemon identity 和 session generation 继续由现有 owner 管理。
- terminal 输出总内存继续受共享 buffer 和 aggregate budget 限制；`block`/`drop` 语义不改变。
- session generation 变化后，旧 generation 的 request、snapshot、input 和 completion 全部失效。
- 没有可见 live view demand 的 `TerminalRef` 不保持 steady live request；已发出的 long-poll 必须远端取消并释放 daemon request slot，恢复可见 live demand 时重新取得 daemon latest screen。
- 同一客户端中，同一个 `TerminalRef` 即使显示在多个 pane，也只建立一个 live source 和一个 received cache。

## 4. Daemon 输出处理

### 4.1 恢复无等待的 opportunistic batch

共享 `terminalOutputBuffer` 保留现有有界容量、双 consumer、gap、flush fence 和失败所有权，只修改 consumer 取数方式：

1. consumer 被唤醒后立即取第一个 node；
2. 继续拿取当时已经排队、属于同一 consumer、没有跨 gap/fence 的相邻 node；
3. live batch 总量最多 `16 KiB`；history 使用现有适合自身落盘的上限；
4. 不 sleep、不等待未来 node、不添加时间窗口；
5. 一次 batch 只调用一次 live vterm ingest、增加一次 revision、发布一次 dirty signal。

这恢复的是已有正常热路径能力，不改变 buffer 的慢消费者策略。

### 4.2 latest screen 与 revision

daemon 保存：

- 当前完整 native screen；
- 当前 `live_revision`；
- 最近一次必须 full replace 的 revision 边界；
- 当前 screen 的行指纹；
- 每个 protocol session + terminal 的短时 confirmed/offered 行指纹基线；
- cursor、modes、size、alternate-screen 状态。

客户端下一次带回 `observed_revision` 时，daemon 只有在同一 session 中仍持有该 revision 的 confirmed 基线，才从旧、新行指纹计算 `row copy` 和 replacement；否则直接 full replace。基线只保存 revision、generation、size、alternate-screen 和每行 64-bit 指纹，不保存旧 cell matrix。resize、restart、alternate-screen 边界、live parser gap、基线过期或无法证明 delta 完整时返回 full replace。daemon 不为客户端保存中间 frame 队列。

## 5. 传输协议

### 5.1 单一 latest-screen 操作

daemon 只向客户端暴露 `LiveScreenNext(observed_revision)`：

1. `observed_revision=0`：首次 attach、session generation 更换或 delta 校验失败，立即返回 full bootstrap。
2. observed 落后：立即返回相对该 revision 的 latest delta，无法续接时 full replace；observed 超前直接 full replace。
3. observed 等于 daemon current：one-shot long-poll，下一次变化后直接返回 latest delta。

旧 `LiveScreenGet`、`LiveInvalidationNext`、`LiveInvalidationResult` 和公共 `LiveInvalidatedEvent` 同步删除，不保留兼容层。core 内部 invalidation broker 只作为长轮询的无丢唤醒边沿。

协议约束：

- revision 只能在同一个 `(EndpointSession generation, TerminalRef)` 中比较；
- delta 的 `base_revision` 必须等于请求的 `observed_revision`；
- `row_copies` 全部同时从客户端 base screen 读取，随后再应用 `row_replacements`；空 replacement 行表示清空；
- cursor、modes 和 alternate-screen 即使没有 changed rows 也必须提交；
- resize、restart、alternate-screen 切换、output gap、observed 超前/过旧或无法续接时返回 full replace；
- daemon 不保存 rendered revision、客户端 visibility、renderer callback 或待消费 frame queue。

### 5.2 单请求 + 单 pending 流控

呈现器保持单写者：TUI runtime 全局最多一个 renderer write in-flight；Web/移动/桌面 renderer adapter 按各自实例串行提交。本地 writer 是否 busy 不进入 daemon 协议状态。

与此同时，每个 active `TerminalRef` 最多有一个网络/待呈现槽，该槽至多处于以下一种状态：

```text
- 一个等待 daemon 下一次变化的 one-shot request
- 一张已经收到但尚未提交给 renderer 的 latest pending screen
```

因此系统任一时刻可以同时存在“一次 renderer write + 每个相关 ref 的一个 request”，或“一次 renderer write + 每个相关 ref 的一张 latest pending”；不会为同一 ref 累积第二张 pending。

状态规则：

1. response 到达后先合并进 received cache，并置 presentation dirty。
2. 当该 received revision 被选入一次 renderer submission 后，才允许 arm 下一次 `NextLiveScreen`。
3. 下一次 response 如果在 renderer busy 时到达，只替换 received latest state并保留一个 pending，不继续 arm 第三个 request。
4. renderer completion 后立即提交 pending latest；提交时再 arm 下一次 request。
5. `observed_revision` 表示 reducer/cache 已合并的 revision，不等待物理 TTY、xterm callback 或浏览器 paint。

`NextLiveScreen` 是可取消的长请求。当前客户端只在本地 abandon waiter、daemon 继续等待的行为不能沿用，必须补齐以下最小协议闭环：

- 每个 request 保留现有唯一 request ID，daemon 为长请求建立由 session context 派生、又可单独取消的 per-request context；
- view demand 归零或 route race 失去 winner 时，客户端发送引用原 request ID 的 protocol cancel control；
- daemon 收到 cancel 后结束对应 long-poll，并立即释放 session/global request slot；session/generation 关闭仍统一取消其全部 request；
- cancel 与正常 response 竞态时只允许一个结果完成本地 waiter，迟到结果按 request ID 和 generation 丢弃；
- 客户端不能只把 waiter 标成 abandoned 后立即为同一 ref 再发 request。

这个 control 只补足长请求生命周期，不引入通用任务系统、重试框架或 screen 队列。

这会把当前严格串行周期从：

```text
RTT + render/write
```

降为：

```text
max(RTT, render/write)
```

同时不产生并发重复 long-poll，也不产生客户端 frame 队列。它仍然不能突破 `1/RTT` 的持续 update 上限；这是第二阶段是否需要 stream 的测量入口，而不是在当前实现中隐藏的假设。

### 5.3 高 RTT 扩展门槛

只有 `40ms/120ms RTT` 的真实 SSH、Direct P2P、Cloud direct/relay 验收证明 `1/RTT` 无法满足产品目标时，才进入有界 credit stream 阶段。不能使用无 ACK 持续 push，因为过期 screen 会进入可靠有序 DataChannel，与 input、control response 和 file stream 发生 head-of-line blocking。

若进入该阶段，协议状态属于 `protocol session + TerminalRef`，最小字段为：

```text
acked_revision
last_sent_revision
available_update_credit
available_byte_credit
dirty
```

规则：

- 客户端只在 delta 成功合并进 canonical cache 后发送累计 revision ACK，并补充 update/byte credit；不等待 renderer completion。
- 多个在途 delta 按 `base_revision = last_sent_revision` 串联；客户端可以不呈现中间状态，但必须按序合并。
- credit 为零或 transport 堵塞时 daemon 只置 `dirty=true`，不生成 snapshot、不排队 revision。
- 恢复 credit 后只从 daemon 当前 latest screen 构造下一张 delta。
- credit 数量和字节预算必须同时有界，并纳入现有 session resource budget。
- generation 变化关闭 resource、清 cache，并从 full bootstrap 重新开始。

本轮方案不预设 credit 数量、不实现 RTT/BDP 自适应，也不增加独立 DataChannel。先用测试数据决定是否需要这一阶段。

### 5.4 不采用的方案

- 不并发打开多个相同或相邻 revision 的 `NextLiveScreen`。
- 不把通用 application event queue 当作 screen transport。
- 不把物理写屏 completion 发回 daemon。
- 不直接做无 credit/ACK 的持续 push。
- 不同时长期保留 invalidated event、one-shot pull 和 stream 三套 live truth。

### 5.5 当前迁移方式

由于尚未上线，one-shot 方案接入完成后直接删除：

- 仅用于 live screen refresh 的 `LiveInvalidatedEvent` 生产和消费路径；
- TUI 由 `LiveFrameReadyMsg` 在物理写屏完成后才 arm 网络请求的代码；
- 移动/Web 的 invalidated-event + 不带 observed revision 的 full refresh loop。

生命周期事件仍走现有 event subscription。bootstrap、resync 和 steady live 全部统一使用 `LiveScreenNext`；`observed_revision=0` 表示请求 full bootstrap。

## 6. 客户端 received cache

所有客户端遵循相同 merge 规则：

1. `full_replace=true`：替换完整 screen，并把 received revision 设为 `live_revision`。
2. delta 的 `base_revision` 必须等于本地 received revision；从不可变 base 同时应用 `row_copies`，随后应用 `row_replacements`，再更新 cursor/modes/revision。
3. `live_revision <= received_revision`：丢弃 stale/duplicate update。
4. base 不连续、尺寸不一致或 merge 无法完成：取消当前 steady request，并以 `observed_revision=0` 重新取得一次 full screen。
5. received revision 表示已经合并进客户端 cache，不表示已经呈现。

每个 `TerminalRef` 的 source 状态只包含：

```text
demand
receivedRevision + receivedScreen
submittedRevision
requestInFlight
receivedDamage { fullReplace, changedRows }
```

`demand` 来自完整逻辑帧实际包含的可见 live target 集合，不来自 writer completion。同一 terminal 的多个 view 在该集合中去重并共用 source；同一 `TerminalRef` 同时出现在一个 copy pane 和一个 live pane 时，只要 live view 仍可见就保持唯一 source。全部 live view 不可见时取消 request；重新可见时继续以本地 canonical revision 请求，daemon 无法续接时自行返回 full screen。

`receivedDamage` 是有界行集合，不是 frame queue：连续 delta 合并 changed rows；遇到 full replace、尺寸/layout 边界或无法证明增量基线时只置 `fullDirty=true` 并清空行集合。每个 source 仍最多持有一个完整 received screen 和一个 pending presentation。

Go 和 TypeScript 分别实现 reducer，但必须覆盖 full、row copy、replacement、空行清除、cursor/modes-only、base mismatch、resize、alternate screen 和 stale generation。

## 7. TUI 呈现调度

### 7.1 最小状态机

TUI runtime 只保留一个全局 writer gate、一个 `renderPending` 标记和成功写出后才提交的视觉基线。live screen 不再维护局部 patch candidate 或 live region baseline。

行为：

1. 所有 domain message 始终先 reduce 和启动 effect；writer busy 不阻塞 input、resize、lifecycle 或 received cache 更新。
2. 仅在 writer idle 且 `renderPending=true` 时，从 reducer 的最新状态构建一张完整逻辑帧；触发点为 queue idle 或现有 message batch 边界。
3. 完整逻辑帧提交后立即根据其中的去重 `LiveTargets` 更新 demand、submitted revision，并允许对应 source 提前挂一个 `LiveScreenNext`；网络等待与物理写出重叠。
4. writer busy 期间只合并最新 canonical state，不构建中间帧。latest-only sink 丢弃尚未写出的帧时，runtime 只安排一次最新完整逻辑帧重试。
5. 只有成功 completion 才提交 hit regions 和 copy-history baseline；真实 `WriteFrame` error 不推进基线、不重试循环，直接返回并终止 runtime。
6. completion 到达时如果已有新状态 dirty，立即构建下一张最新逻辑帧；completion 不决定网络订阅集合。

copy-history 的既有滚动 patch 保留，因为它只在冻结历史视图内工作；live terminal 不再使用 `Frame.Patch`。

### 7.2 完整逻辑帧与物理增量

“完整逻辑帧”不等于每次清屏或写满整个 TTY。renderer 负责生成当前 UI 的完整确定状态，`FrameSink` 保存最后一次真实写出的 ANSI 行基线，并在同尺寸帧之间：

- 只写变化行；
- 能证明一行整体上移或下移时发送 scroll region 指令；
- 内容变短时用尾部空格覆盖，不清整行；
- 只有首帧、尺寸变化或显式 `ForceFullRepaint` 才发送 `ESC[2J`。

这删除了 `d17db574` 中 live 矩形 rewrite 写完便令 `hasLastFrame=false` 的交替路径。连续 live 输出不再出现“局部 patch -> 基线失效 -> 下一帧清屏重画”的闪烁。

### 7.3 row copy 与滚屏

公共协议只暴露经过最终行指纹证明的 canonical row copy。TUI 先在 reducer-owned canonical screen 上同时应用 row copies，再应用 replacements，然后构建完整逻辑帧。`FrameSink` 从前后完整 ANSI 行独立决定是否使用宿主 scroll 指令；无法证明滚动时只重写变化行，不增加第二套 damage algebra。

## 8. 移动/Web 与未来桌面端

### 8.1 共享边界

`clients/ui` 增加一个面向 Proto 的 canonical `LiveScreenCache` 和 terminal-level source owner，供现有 Web、Capacitor 移动端以及未来复用 Web UI 的桌面壳使用。它只负责：

- `LiveScreenNext` 生命周期和 view demand；
- revision/full/delta merge；
- latest dirty 状态；
- session generation fence。

它不负责 React 页面、键盘、文件、history、账号或 Endpoint discovery。

不继续扩展当前混合 ANSI 文本与 `raw: unknown` 的 `TerminalSnapshotPayload` 来承载 delta。新增内部 typed canonical screen/update 类型，显式包含 `TerminalRef`、generation、revision、base、full、rows、cursor、modes 和 alternate-screen；只有 renderer adapter 才把它编码成 ANSI。

source owner 按 `session generation + TerminalRef` 建立。当前 UI 每个 terminal 只有一个 channel owner；未来支持同 terminal 多 view 时，由上层把“至少一个 view 可见”投影成这个 owner 的二值 demand，不在协议状态中加入引用计数。

### 8.2 浏览器/xterm 呈现

浏览器使用自己的帧生命周期：

1. received cache 只发布一张可提交 snapshot；已有 xterm write callback 尚未完成时，source 只保留一张 pending latest。
2. cache revision 被提交给 `term.write` 时记为 submitted，并允许 source 预取下一张；网络等待与 xterm write 重叠。
3. xterm write callback 完成后释放 rendering revision，并立即发布当时的 pending latest。
4. initial/full boundary 必须正确恢复 normal/alternate buffer、cursor 和 terminal modes，再写完整 replay。
5. 少量连续行生成 cursor-position + erase/rewrite；可证明的整体 shift 使用 xterm 支持的 ANSI scroll；否则完整 replay。
6. 页面隐藏、history freeze 或 channel close 时用 `AbortController` 取消长轮询；恢复 demand 后从当前 canonical revision 继续。
7. 当前实现仍通过既有 terminal snapshot state 把一次受背压的 snapshot 交给 xterm；没有为消除这一次 React 更新引入新的外部 store。只有性能数据证明它成为瓶颈时再拆分。

移动端进入后台时 demand 归零并取消 steady request；恢复 foreground generation 后重新 attach 并请求 full screen，不沿用旧 native generation 的 pending update。

### 8.3 原生桌面端

未来原生桌面端只需实现相同的三个本地接口：

```text
merge NativeScreenResult
schedule latest presentation
report local renderer failure
```

桌面端不需要新增 daemon API。如果桌面产品复用 `clients/ui`，直接复用 TypeScript cache/source owner；如果使用原生 renderer，只复用 Proto contract 和 fixtures，不强行共享 TUI/TypeScript 实现。

## 9. Route 与网络场景

| 场景 | live 行为 |
| --- | --- |
| Local Unix | 同一 latest-screen contract；低延迟下通常每次变化都能立即合并和呈现 |
| SSH | SSH 只负责到 daemon WebRTC listener 的 tunnel；live request 仍走已认证 Proto session |
| Direct P2P | one-shot 走可靠有序 WebRTC DataChannel；不增加逐帧 signaling |
| Managed Cloud direct | Cloud 只建立并授权 session；screen payload 不经过 Controller 账号业务 |
| Managed Cloud relay | 与 direct 使用同一协议；每 terminal 只有一个 request 和一个 pending，DataChannel byte budget 继续生效 |
| Route race/reconnect | winner generation 变化即取消旧 request；新 generation 首帧 full replace |
| 多客户端 | 每个授权 session 有独立 source/request 状态；daemon 仍只有一份 terminal native screen |

Cloud、Hub、Relay 不解释 terminal revision，也不缓存 screen truth。移动 App 仍不登录、不自动发现设备，Endpoint 仍只由扫码导入的本地授权配置决定。

## 10. 实施顺序

### 阶段 A：可复现基线与回归测试

- 固定 `f0ce4dff`、`d17db574` 的 TUI stress/perftrace 对比。
- 增加 prompt 尾字符、双 live pane、copy+live 同批、resize during write、sink error 测试。
- 增加 hidden/visible、route winner 切换时 cancel/response 竞态和 daemon request slot 释放测试。
- 增加可配置 `4ms/40ms/120ms RTT`、慢 transport 和慢 writer 的确定性 harness。
- 只建立业务路径测试，不为理论上不可达输入增加分支。

### 阶段 B：daemon 输出 batch（已完成）

- 在现有共享 buffer 内恢复 opportunistic batch。
- 保留容量、drop gap、block、flush 和 failure 语义。
- 验证 burst 下 vterm writes/revisions/dirty signals 明显合并，prompt 不被等待窗口延迟。

### 阶段 C：统一 latest-screen one-shot 协议（已完成）

- 将 bootstrap、resync 和 steady API 收口为直接返回 full/delta 的 `LiveScreenNext(observed_revision)`。
- 增加按 request ID 取消长请求的 protocol control、daemon per-request context 和 slot 释放闭环。
- 同步接入 Go runtime、TUI adapter 和 TypeScript Proto session。
- 同一阶段删除 live-invalidated + second fetch 旧路径，不做双栈兼容。
- lifecycle event subscription 保持独立。

### 阶段 D：TUI scheduler 与物理增量（已完成）

- 将 next request ownership 从 FrameSink completion 中删除，接入每 ref 单 request + 单 pending source。
- 落实单 writer、dirty、submission 和成功后提交视觉基线。
- 删除 live 专用 patch/region 状态，修复多 pane 可见集合、copy baseline、resize 和 sink error。
- 保留无 timer、单 writer、latest state 合并。

### 阶段 E：Web/移动端呈现（已完成）

- 接入 `LiveScreenCache`。
- 用 `LiveScreenNext` delta 取代 invalidated + full fetch。
- 用 xterm completion 合并呈现，writer busy 时只保留一张 pending latest。
- 验证 background/foreground generation、横竖屏 resize 和断网重连。

### 阶段 F：高 RTT 决策门

- 汇总真实 SSH、Direct P2P、Cloud direct/relay 的 RTT、持续输出 cadence、input ACK 和 DataChannel buffered amount。
- one-shot 如果达到产品目标，到此停止，不实现 stream。
- 如果明确受 `1/RTT` 限制，再单独评审并实现第 5.3 节的 update+byte credit stream，同时删除 steady one-shot；不做并存兼容。

每个阶段只提交该阶段必需的代码；不顺带重构 Endpoint、history、文件传输或 UI 组件。

## 11. 验收标准

### 正确性

- shell prompt 最后一个字符无需额外输入或刷新即可出现。
- 双 live pane 任意交错输出都不会使其中一个停止更新。
- copy/frozen view 不贡献 live demand；同 ref 仍有可见 live sibling 时 source 保持，全部 live view 隐藏后才取消 request。
- resize、restart、alternate screen、drop gap 和 reconnect 均通过 full replace 恢复。
- stale generation 的 update/completion 不改变当前客户端 cache 或屏幕。
- sink/write error 不被报告为 `Written=true`，也不推进呈现 baseline。

### 流控与资源

- one-shot 阶段 daemon 不保存每客户端 screen 队列；每客户端/terminal 最多只有一个 in-flight one-shot request。
- 客户端每 `TerminalRef` 只有一个 received screen 和一个 dirty presentation。
- demand 归零或 route winner 切换后，取消的 long-poll 必须释放 daemon session/global request slot；反复 hidden/visible 不增加 abandoned waiter 或在途请求数。
- 慢 renderer 不反压 daemon PTY ingest，不阻塞 terminal input ACK。
- 没有可见 live demand 的 hidden/copy/background terminal 没有活跃 steady request。
- protocol/resource/transport 的现有 byte 和数量预算继续生效。

### 性能与体验

- 本地路径没有固定帧率上限，writer idle 后立即呈现 latest state。
- one-shot 阶段不再把 RTT 与 render/write 串行相加；`4ms/40ms/120ms RTT` 的实际 cadence、带宽和 input latency必须记录。
- 若阶段 F 启用 credit stream，其持续 update cadence 必须不再受 `1/RTT` 限制，同时保持 update/byte credit 和 transport buffer 有界。
- 慢 writer 下 100 次更新只产生一个 in-flight write 和至多一个后续 latest presentation。
- 一行滚屏优先使用 scroll shift，不整块 rewrite 或触发下一帧清屏。
- 与 `f0ce4dff` 相比，renderer/vterm 单次 CPU 和分配不得出现无解释回退；端到端帧间隔和 input latency必须单独记录。

## 12. 明确不做

- 不增加 FPS 设置、25ms timer、debounce 窗口或“高性能屏幕模式”。
- 不为不同 route 维护不同 terminal screen 协议。
- 不实现客户端中间帧回放、daemon per-client framebuffer 或无界队列。
- 不增加多个并发 long-poll 来填充 RTT。
- 不在本轮增加独立 live DataChannel、unreliable transport 或自适应码率。
- 不为未上线旧客户端保留 Proto、YAML 或 runtime 兼容分支。
- 不把 TUI patch、xterm ANSI 或 React state 放进 core/daemon。

## 13. 已确认决策

1. 先实施单 `LiveScreenNext` + 单 pending，以真实高 RTT 验收决定是否进入 credit stream；本轮不建设 stream。
2. 移动/Web 与 TUI 在同一轮切换 one-shot live contract，不长期保留两套行为。
3. 公共 Proto 只增加 canonical row copy；是否使用 scroll 指令仍由各客户端 renderer 根据自身状态决定。

## 14. 多 Agent 审核记录

| 审核方向 | 审核意见 | 已进入本文的修订 |
| --- | --- | --- |
| TUI runtime / concurrency | 局部 patch completion 不能拥有全局订阅；双 live pane、copy+live 同批、resize during write 和 sink error 需要明确状态 | 删除 live patch/region baseline；使用完整逻辑帧、单 writer gate、latest dirty state 和成功写出后才提交的 FrameSink 基线 |
| Protocol / SSH / Cloud / P2P | 多并发 long-poll 会重复 snapshot并耗尽 request slot；本地 abandon waiter 不会取消 daemon 请求；无 ACK stream 会把旧画面送入可靠 DataChannel并阻塞 control/input | 当前只做单 one-shot + 单 pending，并增加按 request ID 的远端取消；高 RTT stream 必须用累计 merge ACK、update credit 和 byte credit，并由实测触发 |
| Web / Mobile / future Desktop | 当前 Web 不合并 delta、全屏 replay 进入 React state、xterm completion 与协议无关，后台 demand 和 alternate/modes 恢复不完整 | 增加 TypeScript canonical cache/source、二值 view demand、xterm callback 背压、typed update 和 full mode restore 边界 |

三方方案复审均已通过。一致意见：不引入固定帧率窗口，不让物理 renderer completion 进入 daemon truth，不按 route 复制协议，不保留旧客户端兼容层。用户已确认第 13 节决策。

修复后的 protocol 复审发现了一个真实阻断项：TUI 在 delta 的 `base_revision` 无法与本地 cache 衔接时，会保留旧 revision 并反复请求同一份无效 delta。`2be0655a` 已将该状态收口为一个 `NeedsBootstrap` 信号：保留最后一张有效画面，释放当前 in-flight request，下一次以 `observed_revision=0` 取得 full bootstrap。定向测试、TUI race 测试和全仓库测试均已通过。

## 15. TUI 变更对比与闪烁根因

### 15.1 三个对比点

- `e68adaea`：本轮“按渲染完成拉取增量帧”优化之前的提交。
- `d17db574`：原优化提交，新增 `tui/app/live_surface_patch.go`，并将 live changed rows 作为矩形 `Frame.Patch` 直接写入 TTY。
- `2be0655a`：当前实现，已删除 live 矩形 patch，完成协议、cache、渲染调度和 delta 失配重同步的收口。

### 15.2 闪烁的直接原因

`d17db574` 的 live patch 写完后会使 `terminalhost.FrameSink` 的完整帧基线失效。下一张普通逻辑帧因此被当作无基线首帧，发出 `ESC[2J` 清屏；持续输出时会反复出现：

```text
live 矩形 patch -> 完整帧基线失效 -> 清屏重画 -> live 矩形 patch
```

这是屏幕闪烁的主因，也是 prompt 尾部字符呈现不稳定的原因之一。当前实现不再让 live update 绕过完整逻辑帧基线。

### 15.3 当前 TUI 保留与删除的内容

| 领域 | 当前处理 |
| --- | --- |
| 逻辑帧 | 每次从 reducer 最新状态构建完整逻辑帧，保证多 pane、overlay、resize 和 hit region 一致 |
| 物理写屏 | 保留 `FrameSink` 的行差分与 scroll-shift 检测；完整逻辑帧不等于整屏物理重写 |
| live patch | 删除 `live_surface_patch.go`、`LiveRegions` 及其局部 baseline；copy-history 专用 patch 保留 |
| 渲染背压 | 保留单 writer gate；writer busy 期间只标记 latest dirty state，写完后立即渲染最新帧 |
| 网络拉取 | 完整逻辑帧提交后即发起下一个 `LiveScreenNext`，与物理写屏重叠；不再等 writer completion 后才走网络 |
| 客户端增量 | 先合并 canonical row copies/replacements，再构建逻辑帧；基线无法衔接时强制 full bootstrap |
| 定时窗口 | 没有 25ms、FPS 限制、debounce 或用户参数；当前写屏周期结束后立即处理 latest state |

### 15.4 验证结果

- `go test ./...` 通过。
- `go test -race -count=1 ./core ./internal/protocol ./tui/...` 通过。
- Web/UI 64 个测试文件、343 个用例、全客户端 TypeScript typecheck、ESLint 和 production build 通过。
- generated-code check 与 `git diff --check` 通过。
- 回归测试明确断言：连续 live 帧没有 `Frame.Patch`，同尺寸连续帧不发出 `ESC[2J`，sink error 不进入重试循环，delta 无法合并时以 revision 0 重取全量。

### 15.5 仍需实机验收的边界

当前 one-shot 设计已将网络等待与客户端写屏从串行改为重叠，但连续 update cadence 仍有 `1/RTT` 上限。这不是 TUI 闪烁问题，也不应在没有数据前提前引入 credit stream。最终产品验收仍需在 Local、SSH、Direct P2P 和 Cloud relay 上实测高 RTT 持续输出、尾字符、输入延迟与断线重连。

## 16. 客户端短时增量基线

`3c65a787` 已把 live delta 从“全局最近几帧能否碰巧命中”改成客户端实际确认基线：

1. key 由 protocol session ownership 和 `TerminalID` 共同确定，不能跨授权 session 或 endpoint generation 复用；
2. 一次 response 先进入 `offered`，客户端下一次用它的 revision 请求时才提升为 `confirmed`；
3. confirmed 基线只保存行指纹及必要元数据，当前 cell screen 仍只有 Terminal 一份；
4. TTL 为 2 秒，长轮询使用期间 pin，结束后重新开始计时；
5. 每 session 最多 64 个 terminal、1 MiB，daemon 全局最多 64 MiB；超限或过期只退化为 full replace；
6. revision、尺寸、generation、terminal 实例或 alternate-screen 不匹配时不猜测，直接 full replace。

history frozen token 还带有 store 实例 ID；同一 TerminalID 删除后重建，新旧 store 不会再从 `linehist-1` 开始产生相同 token。旧请求无法释放或分页新 terminal 的 frozen window。

这满足“服务端知道这个客户端上一张已合并帧”的需求，同时不会让高频中间 revision 淘汰真正的客户端基线，也没有建立 per-client frame history。

## 17. CPU、吞吐与常量内存

### 17.1 已完成修改

- 保留共享 output buffer 对当前已经排队 node 的即时合并，最大 batch 16 KiB，不增加 sleep、帧率或时间窗口。
- live vterm 使用 `WriteForLatestFrame`，不为 live-only 解析生成 scrollback damage payload。
- 删除 PTY reader 的额外 `16 x 64 KiB` channel 预队列；PTY 读取直接交给有界 output buffer。`block` 会自然减慢 PTY，`drop` 会产生显式 gap 并使对应 consumer 进入 sync-lost，而不是继续累积内存。
- history older/newer 分页按压缩块流式访问 cold lines，同一个块只解压一次；每条 stored line 在展开成 grapheme cells 之前先执行 60 KiB response budget，超限立即停止，避免合法的 512 行请求造成瞬时内存放大。
- canceled latest history request 会释放已经创建的 frozen token；event subscription 在本地 stream 结束时释放 daemon resource；terminal 删除并用同 ID 重建时，旧 live long-poll 不能读到新实例。
- native/WASM binding 对会新建资源的 `LATEST/UNSPECIFIED history_window` 和 `event_subscribe` 等待有界 terminal response；如果资源已在 daemon 创建但 JS/Go consumer 此时取消，立即发送 `HistoryRelease` 或 `ReleaseResource`。`OLDER/NEWER/OLDEST` 只复用调用方已有 token，取消时绝不释放它。公共 Go `ApplicationSession.HistoryWindow` 和 event subscribe 建立阶段也使用同一终态语义。

### 17.2 内存上限

同一 terminal 的输出内存不随累计输出增长：

- output buffer 默认每 terminal 32 MiB，daemon aggregate resident budget 默认 512 MiB；
- 永不换行的 logical line 每 256 KiB 切成连续 chunk；
- history 压缩 pending block 目标 256 KiB，落盘文件默认每 terminal 512 MiB retention；块索引也随 retention 一起受限；
- history window 查询最多持有一个已解压的 256 KiB block 和 60 KiB 预算内的投影，不随请求行数或累计历史增长；
- live screen 只保留当前 vterm screen；客户端基线只保留有总量上限的行指纹；
- 客户端只保留一张 canonical received screen 和一个 dirty/presentation 状态，不保留帧队列。

### 17.3 实测

命令：`python scripts/generate_terminal_stress.py --lines 100000`

| 路径 | 总耗时 | 备注 |
| --- | ---: | --- |
| 优化前 AnyTTY | 22.274s | 用户提供基线 |
| 外部终端 | 10.465s | 用户提供基线 |
| 当前 AnyTTY | 11.02s、11.86s | 两次实机验证；output buffer 峰值约 10.4 KiB、18.9 KiB，drop/gap/wait 均为 0 |

100008 行完整 history dump 从 220.22s 降到 32.24s；其中 `latest` 约 151ms、`oldest` 约 1.11s。完整 dump 是诊断导出路径，本轮不再为它增加缓存或索引层。

## 18. Live 到历史模式的连续视口

问题根因不是“最后一行选错”，而是 history 投影会裁掉 terminal 视口底部没有内容的空白行。如果客户端只把 frozen rows 滚到底部，Live 中位于屏幕中间的文字就会突然成为历史视图最后一行。

当前合同在 freeze/latest response 中增加 `HistoryViewportAnchor`：

```text
top_line_id + top_cell_offset
at_end
screen_cols + screen_rows
```

`top_line_id + top_cell_offset` 指向 freeze 时 Live 视口顶部在 logical history 中的位置。客户端按自己的当前列宽重新换行后再定位，所以 Local、SSH、P2P、Cloud，以及 TUI、Web/移动和未来桌面端都使用同一语义，不依赖 daemon 的 renderer 行号。

TUI 的处理：

- latest frozen window 接纳后按 logical line 和 display-cell offset 计算 `ViewportTop`；
- offset 恰好等于一行宽度时进入下一视觉行；
- history 尾部不足一个 viewport 时使用虚拟空白行，不把空白写入 history truth；
- latest 请求飞行期间发生 resize，只更新本地 reflow 宽度，不重复创建 frozen token。

Web/移动 xterm 的处理：

- daemon 保持 logical-line 协议真值；Web pager 在交给 xterm 前按当前 `cols` 本地 reflow，`loadedRows`、`viewportTop`、prepend 位移和尾部补白全部使用 visual row 数；
- history cell 仍按 style/link run 合并，但只合并字符宽度相同的 grapheme，所以客户端可以精确 reflow，不需要把每个字符放大成独立 protobuf 对象；
- ANSI replay 保留 logical line 的 soft-wrap 身份，复制不会为本地换行插入额外硬换行；fixed-grid/alternate 行按本地列宽裁剪，保证一条协议 grid row 只占一条 xterm row；
- pager 把同一 anchor 换算成 `viewportTop`，older prepend 时按新增 visual row 数平移；
- 每次重放 frozen history 时，只补齐 anchor 以下缺少的 viewport 空白行；
- 首次 replace 滚到 authoritative anchor，后续 prepend 继续用 xterm 实际新增行数保持当前位置。
- history 请求飞行中或已经显示后发生 resize，旧列宽投影都会被丢弃；客户端在当前 load/write 空闲后按新 `cols` 重载 frozen 视图，避免 xterm 活动末行在缩窄时截断。
- alternate-screen 不再返回伪空页，而是通过同一 frozen pager 读取 fixed-grid 行并保留 `alternate=true`。

alternate screen 以 frozen alt 第一行为锚点；空视口使用 `at_end`。锚点随 frozen token 固定，后续 Live repaint 不会改变已经进入的历史视图。

TUI 本地历史窗口裁剪掉 frozen tail 后，newer 分页只在“原 viewport anchor 已重新出现，且本地窗口已达 frozen 真实尾界”时恢复虚拟空白行。向下浏览会先消费真实历史行，再滚过这些空白行，不会把未取回的较新内容误判为空白。
