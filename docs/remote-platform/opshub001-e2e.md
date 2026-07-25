# OPSHUB001 Hub 管理与动态目录验收

日期：2026-07-25

## 完成范围

- `hub_deployments` 是 Hub/Relay 公开目录、身份批准、容量、drain、archive 和 directory revision 的唯一持久真值；Controller 配置与 Companion manifest 不再保存静态 Hub URL。
- Operator 可以创建 pending deployment、编辑目录、核对并批准 Hub/Relay identity fingerprint、开始或取消 drain，并在有效 assignment 清零后 disable/archive；所有 mutation 使用 expected revision、request ID 和 operator audit。
- Controller 的 enrollment candidate、mobile/daemon session、Relay lease/usage、policy publisher 和 Edge control attachment 每次读取 registry；目录变化不要求重启 Controller 或重建客户端。
- drain 只阻止新 assignment，不移动已有 assignment；迁移复用既有 CommandOutbox、source control generation 和 assignment epoch fence，成功后原子切换到目标 Hub。
- archive 保留 deployment 和审计，不提供硬删除，也不把 control readiness 写成数据库在线真值。

## 真实链路证据

- `scripts/with-test-postgres.sh sh -c 'cd private/cloud/devcloud && env GOWORK=off go test ./cmd/muxvia-cloud-dev -run TestOPSHUB001AddsEdgeWithoutControllerRestart -count=1 -v'`。
- process E2E 从两个已有 deployment、真实 account/device 和 epoch 1 assignment 启动 Controller、源 Edge 与 standby Edge，再通过 Operator HTTP API 创建第三个 pending deployment；目标 Edge 在 identity 批准前不能 Ready，批准后无需重启 Controller 即完成 Hub/Relay control attachment，原有 assignment 仍停留在源 Hub epoch 1。
- `TestControllerManagesDynamicHubDirectoryWithoutRestart` 覆盖 create/approve、directory revision、已有 assignment 不漂移、drain、精确 source epoch fence、目标 Hub epoch 2 assignment 与零 assignment disable/archive。
- `TestHUB007EdgeRestartAssignmentMigrationAndControllerOutage` 继续以真实 Controller、双 Edge 和 Operator `MIGRATE_ASSIGNMENT` CommandOutbox 覆盖 source Edge 消费 fence、原子 assignment 切换、旧 epoch 拒绝与故障恢复。
- `scripts/with-test-postgres.sh go test ./private/cloud/edge ./private/cloud/devcloud/cmd/muxvia-cloud-dev -count=1`：Edge public adapter、双 Edge supervisor、控制面故障恢复和既有命令链路回归通过。

## UI 证据

- `npm run typecheck` 与 `npm run build`。
- `npx playwright test e2e/opshub001.spec.ts` 覆盖创建、编辑、批准、drain、取消 drain、再次 drain 和 archive；API 使用确定性浏览器 fixture，真实服务端 mutation 由上述 Controller process E2E 覆盖。
- 桌面：`.artifacts/opshub001/hub-lifecycle-desktop.png`。
- 移动端 390px：`.artifacts/opshub001/hub-lifecycle-mobile.png`。
- 两个视口均无横向溢出；archive 后只保留只读状态，不再展示 Edit、Drain 或 Disable 动作。

## 安全与状态边界

- Operator mutation 只在独立 listener 暴露，继续要求 admin、同源 CSRF 和近期认证。
- pending 或 fingerprint 不匹配的 Edge 无法取得 control generation；disabled/archive deployment 不进入新 enrollment candidate。
- Hub control、Relay control 和 Controller projection 继续使用独立密钥角色。目录标签或 URL 编辑不改变 Relay usage event 的部署凭据有效期。
- readiness、control generation 和 freshness 只来自当前进程 attachment projection；PostgreSQL 不保存伪造的 online bool。
