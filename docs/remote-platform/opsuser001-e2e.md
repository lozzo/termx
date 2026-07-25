# OPSUSER001 用户与 Agent 运营闭环证据

## 范围

本切片完成用户管理和按机器聚合的 Agent 概览，不新增支付能力、数据库在线状态或页面私有 DTO。账号、Session、Device authority、Presence topology 与 CommandOutbox 继续由各自既有领域 owner 持有。

## 契约与事务

- `AccountSessionProjection` 只返回 Session ID、客户端设备 ID、有效期、revision 和 revoked，不返回 token、hash、密码或 secret。
- 精确 Session revoke 使用 `account_id + session_id + expected_revision`；批量撤销必须显式设置 `all_account_sessions`。撤销与 Operator audit 在同一 PostgreSQL 事务提交。
- 相同 `request_id` 只有 actor、action、resource、account、reason 和 revision 全部一致时幂等；输入变化返回 conflict。跨账号 Session ID 返回 not found，且不会撤销原账号凭据。
- Agent projection 以 daemon DeviceIdentity 为主键，实时组合 assignment、Presence 和 active PeerSession。`STALE` 不显示为 offline，也不能执行 Kick。

## 权限边界

- Operator deployment credential 的 `readonly/admin` 角色不可在运行时修改；readonly 写操作返回 forbidden。
- Session revoke、账号 suspend/restore、Device revoke 和 Agent command 都要求 admin、same-origin、CSRF 与近期认证。
- 用户账号 API 继续以当前 Cookie 账号覆盖请求账号，Operator API 只在独立 listener 暴露。

## 自动化证据

| 场景 | 证据 | 结果 |
| --- | --- | --- |
| Proto 无 secret/无 DB online 契约 | `TestOperatorUserAndAgentContractsExcludeSessionSecretsAndOnlineTruth` | PASS |
| Session CAS、跨账号隔离、审计、重放 | `TestOperatorSessionRevokeIsFencedAuditedAndReplaySafe` | PASS |
| stale Presence 禁止 Kick | `TestPlannerRejectsKickForStalePresence` | PASS |
| readonly/admin、Agent 查询、Kick、Session revoke HTTP | `TestOperatorAPIEnforcesRoleCSRFAndAccountIsolation` | PASS |
| 真实迁移、重启、Agent 查询、Kick 和 command applied | `TestHUB007EdgeRestartAssignmentMigrationAndControllerOutage` | PASS |
| Users/Agents、Session revoke、命令四段状态、1366/390 无溢出 | `e2e/opsuser001.spec.ts` | PASS |

真实进程测试使用临时 PostgreSQL、一个独立 Controller 与两个独立 Edge 进程。Controller 重启后，`daemon-edge-a` 仍以同一机器身份聚合到迁移后的 `hub-edge-b`、assignment epoch 2；Operator 精确 Kick 关闭该 Presence，持久命令进入 `APPLIED` 或 `ALREADY_SATISFIED`。

## UI 证据

- `.artifacts/opsuser001/users-agents-desktop.png`
- `.artifacts/opsuser001/users-agents-mobile.png`

页面截图使用 Proto JSON fixture，只证明用户操作、投影文案和响应式布局。真实 authority、topology 和 command execution 由上面的独立进程测试证明。

## 准入命令

```sh
./scripts/check-generated-code.sh
go test ./private/cloud/control-plane/commerce ./private/cloud/control-plane/commandoutbox ./private/cloud/web-controller -count=1
MUXVIA_DEV_POSTGRES_DSN='postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable' \
  go test ./private/cloud/devcloud/cmd/muxvia-cloud-dev \
  -run TestHUB007EdgeRestartAssignmentMigrationAndControllerOutage -count=1
npm run typecheck --workspace @muxvia/web-controller
npm run build --workspace @muxvia/web-controller
MUXVIA_OPSUSER_E2E_BASE_URL=http://127.0.0.1:5174 \
  npx playwright test e2e/opsuser001.spec.ts
git diff --check
```

测试 DSN 仅指向执行时临时创建、结束后删除的本地 PostgreSQL 容器；不访问线上数据。

## 非本切片阻碍记录

默认并行 `make test` 在整仓负载下两次触发既有 `shared/cloudcompanion/ipc` 用例 `TestIPCConnectionPreservesHelloUnaryAndStreamOwnership` 的 stream close/open 时序 flake：第二次 Presence registration 偶发先读到前一 stream 的结束帧。该目录与本切片无 diff，且相同用例独立重复 20 次全部通过。

本切片不跨范围修改 IPC。整仓覆盖使用 `GOFLAGS=-p=1 make test` 串行执行；后续独立切片可用以下命令复现负载相关问题：

```sh
make test
scripts/with-clean-muxvia-env.sh env GOWORK=off \
  go test ./shared/cloudcompanion/ipc \
  -run TestIPCConnectionPreservesHelloUnaryAndStreamOwnership -count=20
```

这条 deferred issue 不改变 OPSUSER001 的 Proto、PostgreSQL、Controller、双 Edge、Operator Web 或 CommandOutbox 证据。
