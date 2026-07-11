# tui 补丁代码审计

状态：切片 116 审计基线
日期：2026-06-06

## 1. 审计结论

当前 v3 主线可以继续推进，但必须把下面规则作为后续切片准入：

- 可接受的面条代码：单 owner、线性、可测试、仍经过 reducer/effect/message/service result 的业务流程。
- 不可接受的补丁代码：绕过 reducer-owned state、服务直接改 UI state、renderer 读取 runtime/service、重复维护同一份 truth、action 字符串在 render/app 两边分叉、只用 fake harness 证明真实协议行为、默认入口重新 import 旧 `termx-core` 或 `tuiv2`。

本切片已做的小范围收口：

- render/app 共享的 content action id 已收敛到 `render.ActionIDCatalog()`。
- 生产代码里的 `empty.*`、`picker.*`、`pool.*`、`workbench.*`、`prompt.*`、`help.*`、`floating.*`、`pane.*` hit region action id 不再在 render 和 app 两侧手写分叉。
- render 包新增 guard：不得 import app、services、terminalhost 或 Bubble Tea contract。
- input 包新增 guard：生产 binding catalog 只能有一个 owner。

## 2. 当前可接受的线性流程

- `AppRuntime` 串行处理 message，effect result 通过 message 回投；这是可接受的线性流程，不是补丁。
- `NewShellReducer` 中根据 `ShellContentActionMsg` 转成 pane、floating、pool、workbench、prompt 等命令；当前仍较长，但 owner 单一，且不直接做 IO。
- terminal pool create/attach/kill/edit、live attach/input/resize 和 history request 都通过 effect 调用 service，再用 result message 回到 reducer。
- renderer 只消费 `StateRoot -> RenderVM -> RenderResult`，不读取 core client、runtime 或 terminal service。

## 3. 已收口风险

### 3.1 action id 字符串分叉

历史问题：

- render 产出 hit region 时手写 `picker.attach`、`pool.kill` 等字符串。
- app reducer 另一侧用同样字符串 switch。
- 新页面或按钮很容易只改一边，形成可点击但不生效的补丁。

当前收口：

- `tui/render/action_ids.go` 是 action id catalog owner。
- app 只引用 catalog 常量，不再手写生产 action switch 字符串。
- `TestActionIDCatalogIsUniqueAndCoversRenderedActions` 覆盖 catalog 唯一性和关键 action。

### 3.2 renderer 越界风险

历史风险：

- render framework 随功能增长可能反向 import app/services 读取运行时状态。

当前 guard：

- `TestRenderPackageDoesNotImportRuntimeOrServices` 禁止 render import `app`、`services`、`terminalhost`。
- 原 Bubble Tea guard 保留。

### 3.3 input binding 分叉风险

历史风险：

- 键盘/鼠标映射容易回到多个 switch 分散维护。

当前 guard：

- `bindings.go` 是唯一生产 binding catalog owner。
- `TestInputBindingCatalogHasSingleProductionOwner` 防止第二套 catalog 出现。

## 4. 仍需后续切片处理的风险

- `NewShellReducer.reduceShellContentAction` 仍偏长。它现在是可接受面条代码，因为 owner 单一且可测；后续当 Terminal Pool、Workbench Tree、Prompt 行为继续增长时，应拆成 per-content action reducer，但不能在本切片做大重构。
- `ShellContentActionMsg.ActionID` 仍是字符串字段。这是 render hit region contract 的现状；若后续需要更强类型，应把 `render.ActionID` 下沉到 message 字段，但要同步 render/app 测试。
- 部分 app 测试仍用 action id 字符串断言可见行为。测试中允许保留用于读者可读性；生产代码必须继续用 catalog。
- remote legacy/fallback 文件仍有旧依赖，这是 workflow 明确隔离范围；默认路径依赖守卫必须持续通过。

## 5. 后续准入边界

后续切片如果新增 UI action，必须：

1. 先在 `render.ActionIDCatalog()` 增加 action id。
2. render 只产出 hit region，不执行业务行为。
3. app reducer 通过 catalog action 转成 semantic command 或 service effect。
4. service IO 必须通过 result message 回投。
5. 至少补一个 render hit region 测试和一个 app reducer/runtime 行为测试。

后续切片如果新增 service 或 protocol 能力，必须：

1. 先定义 service request/result message。
2. effect 调 service，不直接改 state。
3. reducer 只接 result message 修改 state。
4. 真实 protocol 或 server harness 至少覆盖一条正向路径；fake harness 只能补边界和错误分支。
