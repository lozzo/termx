# termx 对接 tgent 重构 TODO

状态图例：

- `[x]` 已完成
- `[>]` 进行中
- `[ ]` 未开始
- `[!]` 阻塞或待专门确认

## M0. 规格确认

- `[x]` 写出第一版远程规格。
- `[x]` 根据你的纠正，把方向改成“保留 tgent 产品壳，termx 替换 tmux+tgent agent 内核”。
- `[x]` 你已确认当前中文版 spec 方向。
- `[x]` 将这版修正后的 spec 单独提交。

验收标准：

- spec 明确保留 `tgent-web / tgent-app / hub / TURN / dashboard / account / pricing`。
- spec 明确设备端最终只保留 `termx` 常驻进程。
- spec 明确“session 列表页改 terminal 列表页”。

## M1. 先做兼容性盘点

- `[x]` 新增仓库内兼容性盘点文档：`docs/tgent-termx-compat-inventory.md`。
- `[ ]` 盘点 `tgent-web` 中哪些 API 强依赖 tmux/session 模型。
- `[ ]` 盘点 `tgent-app` 中哪些页面和状态强依赖 session 模型。
- `[ ]` 盘点 `tgent hub` 中哪些注册/信令/relay 逻辑可直接复用。
- `[ ]` 盘点原 `tgent agent` 哪些能力必须嵌入 `termx`。
- `[ ]` 盘点当前 `termx` 仓库哪些目录和包边界会阻碍后续接入。
- `[ ]` 盘点当前 `tuiv2/runtime` 和 `tuiv2/bridge` 哪些部分应提升为 shell-neutral client runtime。
- `[ ]` 盘点当前 `workbenchdoc/workbenchsvc` 与 `tuiv2/workbench` 的结构真相重复点。
- `[ ]` 盘点 `tgent-web` schema / telemetry / internal hub reporting 中哪些 session 模型不能直接沿用。
- `[ ]` 形成一份“直接复用 / 最小改造 / 必须替换”的清单。

验收标准：

- 明确哪些是“换后端数据源”即可。
- 明确哪些必须从 session 语义改成 terminal 语义。
- 明确未来代码应收敛到哪些目录边界。
- 明确哪些 `tuiv2` 能力应提升为多个 shell 共用层。

## M2. 先做目录与架构收口

- `[ ]` 先写失败测试或 characterization tests，覆盖计划移动的关键行为边界。
- `[x]` 抽出第一版 shell-neutral client API 落点：`internal/clientapi`，并让 `tuiv2/bridge` 退化为兼容 shim。
- `[x]` 通过 characterization tests 锁住 session RPC 基线后，将 `termx.go` 里的 session/workbench 服务端路径抽到独立文件。
- `[x]` 抽出 session/workbench RPC handler 落点：`internal/sessionrpc`，让根目录 `Server` 只保留 wiring 与 session event publish。
- `[x]` 抽出第一版 workbench doc codec 落点：`internal/workbenchcodec`，并让 `tuiv2/sessionstate` 退化为兼容 shim。
- `[x]` 把 `tuiv2/orchestrator` 收口为 layout/workbench-only 边界，移除未使用的 runtime 注入。
- `[x]` 把 `termx web` shell 逻辑迁到 `internal/webshell`，让 `cmd/termx` 只保留命令入口 wiring。
- `[x]` 为本地 workspace/tab/focus/zoom/viewport projection 建立 `tuiv2/viewstate` 落点，并让 `tuiv2/app` 退化为兼容 wrapper。
- `[ ]` 收口根目录过厚的 server internals。
- `[ ]` 把 workbench/session 能力从未来 remote 主路径中隔离出来。
- `[ ]` 为 shell-neutral client runtime 建立明确目录落点，并逐步从 `tuiv2` 名下移出。
- `[ ]` 为 embedded remote runtime 建立明确目录落点。
- `[ ]` 为 `tgent` compatibility adapter 建立明确目录落点。
- `[ ]` 把落在 `cmd/` 里的 shell/product 逻辑迁回 shell 层。
- `[ ]` 明确 `web/control` 和 `mobile/app` 是否作为正式 product shells 迁入本仓。

验收标准：

- 后续 remote 接入不需要继续把代码堆到根目录或 `tuiv2/`。
- `termx core / workbench / client runtime / remote / compat / product shells` 六层边界清晰。
- shell-local state 和 shared structure truth 已经边界分明。

## M3. termx 原生持有远程能力

- `[ ]` 先写失败测试：`termx daemon` 可承载 embedded remote runtime。
- `[ ]` 先写失败测试：remote config 和本地身份材料。
- `[ ]` 把原本 `tgent agent` 的远程职责迁入 `termx`：
  - 登录/注册
  - hub 选择
  - hub 长连
  - signaling
  - WebRTC bridge
- `[ ]` 确保最终不再要求用户单独起 `tgent` 常驻二进制。

验收标准：

- 设备侧只跑 `termx`，就能具备原本 agent 的核心远程能力。

## M4. 对接 tgent 控制面与 hub

- `[ ]` 先写失败测试：termx 对接 `tgent-web` 的注册/发现/ticket 契约。
- `[ ]` 先写失败测试：termx 对接 `tgent hub` 的注册与 signaling 契约。
- `[ ]` 明确 control-plane 里的 session telemetry 如何映射到 termx 的 terminal / attachment 模型。
- `[ ]` 尽量复用现有：
  - auth
  - hub discover
  - heartbeat
  - connect ticket
  - relay / TURN
- `[ ]` 只在必要处做最小接口改造。

验收标准：

- `termx` 能作为 `tgent` 体系里的设备端 runtime 被 web/hub 识别。

## M5. 把 session 主入口改成 terminal 主入口

- `[ ]` 先写失败测试：设备 -> terminal 列表接口。
- `[ ]` 改造 `tgent-web` 或其 API，使其能返回 terminal list。
- `[ ]` 改造 `tgent-app` 的 session 列表页为 terminal 列表页。
- `[ ]` 清理直接依赖 tmux session 结构的页面状态和命名。

验收标准：

- 用户进入设备后，看到的是 terminal 列表，而不是 tmux session 列表。

## M6. 打通 live terminal 主路径

- `[ ]` 先写失败测试：connect ticket -> signaling -> WebRTC -> terminal attach。
- `[ ]` 复用现有 `tgent` signaling / TURN 路线。
- `[ ]` 在 DataChannel 上承载 `termx` protocol frame。
- `[ ]` 打通：
  - attach
  - input
  - resize
  - snapshot/bootstrap
  - screen_update
  - SyncLost recovery
- `[ ]` 改造 app terminal 页接入 `termx`。

验收标准：

- 手机端能真正通过 `tgent` 现有远程基础设施进入 `termx` terminal。

## M7. 补齐控制面和产品面回归

- `[ ]` dashboard 相关页面回归
- `[ ]` account / password / auth 路线回归
- `[ ]` pricing / billing / subscription 路线回归
- `[ ]` device online/offline 状态回归
- `[ ]` hub / relay 发现和 ticket 路线回归

验收标准：

- termx 接入没有把 `tgent` 现有产品壳打坏。

## M8. 收尾与后续安全改造预留

- `[ ]` 删除或废弃独立 `tgent` 设备端二进制路径
- `[ ]` 更新 README / 接入文档 / 运维文档
- `[ ]` 补本地 smoke test 和端到端回归
- `[ ]` 明确第二阶段安全改造 backlog：
  - 更 SSH 化的 client-held key 模型
  - 更干净的 proof-of-possession
  - 更合理的内部鉴权机制

验收标准：

- 第一阶段完成“termx 替换 tmux+tgent agent”。
- 第二阶段安全增强有明确 backlog，但不阻塞第一阶段落地。

## 每个里程碑都必须遵守

- `[ ]` 先写失败测试，再做实现。
- `[ ]` 迁移 `tgent` 旧代码前先补 characterization tests。
- `[ ]` 每个切片提交前做 findings-first 的 subagent review。
- `[ ]` 有 blocking findings 先修复再提交。
- `[ ]` commit 保持小而清晰，不堆无关改动。

## 当前说明

- 当前已进入 M2 结构收口。
- 已开始把 root server internals 和 `tuiv2` 内的通用层逐步迁到明确包边界。
