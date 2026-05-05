# TermX Remote Migration Agent Notes

## Current Mission

当前工作的唯一主线是把 remote 从 `termx-core` 中完整迁出，形成清晰的四层结构：

- `termx-core`：纯 daemon / terminal / protocol / transport / events / file / PTY / session
- `termx-remote`：remote 产品域，实现 hub / agent / pairing / signaling / registration / session orchestration
- `termx-cli`：唯一集成层，负责装配 `termx-core` 与 `termx-remote`
- `remote-ui`：interface-first 的 UI/runtime 层，目前只实现 browser adapter

## Non-Negotiables

- `termx-core` 中不得新增或保留 remote 域代码。
- `termx-core` 只暴露 shell-neutral daemon capability，不暴露 remote product capability。
- `termx-remote` 与 `termx-core` 的主边界必须是 Go `interface`；RPC 只是该接口的一种 adapter。
- local/LAN/managed 外网模式必须收敛到同一套 hub/signaling/ICE/session 流程，不允许继续分裂成两套产品链路。
- `remote-ui` 所有网络相关能力必须先定义 TypeScript `interface`，再提供 browser implementation。当前阶段不要实现 native，只保留 future-native 的工厂/适配器边界。
- relay 不是第四种客户端 transport path；客户端 path 只允许 `local`、`public_p2p`、`managed`。

## Workflow Discipline

- 本次迁移的唯一任务账本是仓库根目录 `workflow.md`。
- 开始任何切片前，先更新 `workflow.md`：
  - 标记正在处理的任务
  - 记录目标行为与依赖
  - 记录预期失败测试
- 完成任何切片后，必须再次更新 `workflow.md`：
  - 已完成内容
  - 新发现的问题 / 风险 / deferred item
  - 已执行的验证命令
  - review 发现与修复
- `workflow.md` 中的任务必须：
  - 使用稳定 ID
  - 按优先级排序
  - 保持完整，不遗漏新发现事项
  - 区分 open / in_progress / blocked / done
- 旧的 `docs/remote-rebuild/WORKFLOW.md` 只作为历史记录；当前执行以根 `workflow.md` 为准。

## Unattended Execution

- 这项工作默认无人值守推进。
- 除非遇到以下情况，否则不要停在半成品状态：
  - 破坏性不可逆操作
  - 需要用户凭证或外部人工介入
  - 关键架构目标相互冲突
- 如果发现新问题，不要口头遗忘；必须立刻写入 `workflow.md` 并重新排序优先级。

## TDD Rules

每个切片必须严格按下面顺序推进：

1. 定义目标行为
2. 写失败测试
3. 运行测试并记录失败结果到 `workflow.md`
4. 写最小实现
5. 重构
6. 运行 focused tests
7. 运行 relevant broader tests
8. 更新 `workflow.md`
9. 发起独立 code review
10. 修复 review 发现
11. 再次更新 `workflow.md`

禁止直接跳过“失败测试先行”。

## Review Rules

- 每个切片完成后，必须使用独立 subagent / code-review agent 做一次审查；目标是防止实现只是在迎合测试用例形状。
- review 重点必须包括：
  - 测试是否 fake / tautological / 只验证 mock 交互
  - 实现是否被测试 shape 绑架
  - 是否残留旧 remote/core 边界泄漏
  - 是否错误地把浏览器实现细节泄漏到公共接口
  - 是否把 local/external 做成两套业务流程
  - 是否遗漏 `workflow.md` 更新
- 如果当前环境没有 subagent 能力，必须在 `workflow.md` 中明确记录原因，并做一次显式 adversarial self-review。

## Validation Expectations

- 先跑与当前切片强相关的 focused tests。
- 再跑会被当前切片影响的 broader tests。
- 如果改动影响 CLI、core、hub、remote-ui 的边界，必须补跨目录验证。
- 不要在未更新 `workflow.md` 的情况下宣称切片完成。

## Directory Rule

- 如果在迁移过程中创建 `termx-remote/`，应在该目录下同步创建 `AGENTS.md`，并沿用本文件的迁移规则与 `workflow.md` 纪律。