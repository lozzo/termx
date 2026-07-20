# HUB007 双 Edge 控制面 E2E 证据

## 范围

本切片验证多 Hub 控制面在 development 单区域装配下的真实进程、迁移、故障恢复、管理命令和隐私边界。它不启动 `CLOUDP007`，不覆盖 Android APK、Web terminal、多区域数据库或 Relay Mesh。

## 独立进程拓扑

`TestHUB007EdgeRestartAssignmentMigrationAndControllerOutage` 通过 `termx-cloud-dev --fault-harness` 构建并启动：

- 一个独立 `termx-cloud-controller` 进程；
- 两个独立 `termx-cloud-edge` 进程，分别拥有 `hub-edge-a/relay-hub-edge-a` 与 `hub-edge-b/relay-hub-edge-b`；
- 两个真实 daemon Presence：`daemon-edge-a`、`daemon-edge-b`；
- 两个真实 client principal：`client-dev-local`、`client-dev-secondary`。

supervisor manifest 记录 PID、listener、Hub/Relay identity、SQLite、usage outbox、配置和日志路径。`--fault-harness` 只为测试分配稳定 listener、控制透明 HubControl TCP proxy，并允许测试精确停止后重启单个子进程；默认 development 启动行为不变。

## 故障与迁移

| 场景 | 动作 | 证据 |
| --- | --- | --- |
| Edge restart | daemon B 先建立 Presence、上报 `runtime-b-before/revision1` full inventory；停止 Edge B，再用同一二进制和配置启动 | 旧 Presence 关闭；Hub/Relay control generation 从 1 递增到 2；Edge 从 Controller full projection 恢复 revision 1；daemon 以 `runtime-b-after` 重连并 replacement 上报，两个既有 client 再次 Resolve 成功 |
| HubControl network outage | 透明 TCP proxy 只断开 Edge B 到 Controller 的 control socket，不停止 Edge、Presence 或 client | 断线前 Controller 观察到 READY session；断线期间 daemon 上报空 revision3，两个既有 client 继续 Resolve；恢复后 generation 递增，full snapshot 清除旧 session；旧 revision2 runtime report 被 Edge 拒绝 |
| assignment migration | Operator API 创建 `daemon-edge-a: hub-edge-a/epoch1 -> hub-edge-b/epoch2` | CommandOutbox 固化 source Hub、source generation、source epoch 与 target epoch；旧 Presence 被 `FenceAssignment` 关闭；SQLite 在同一 receipt 事务中切换 assignment |
| target recovery | daemon 使用新 Hub token 在 Edge B 创建新 Presence | 两个 client 都能通过 Edge B `ResolveEndpoint` 解析迁移后的 daemon；旧 Presence handle 不复用 |
| Controller outage | 停止 Controller | Operator listener 不可用，但两个 Edge 进程继续存在；两份 outage 前已签发的 client session 仍通过 Edge B Resolve 两个 daemon |
| Controller restart | 用相同 SQLite 与稳定 listener 重启 Controller | Edge A generation 达到 2、Edge B 达到 4；`daemon-edge-a` 仍为 `hub-edge-b/epoch2`，initial config 不覆盖运行期 assignment truth |

模型级 harness 另外证明：`control_generation` 只证明 receipt 来自当前 stream，`execution_control_generation` 固定首次产生 fence 副作用的精确 generation；同一 Hub 进程重连可用新 stream generation replay 旧 execution receipt，新 Hub 进程不得替旧 generation 确认 fence；command replay 不重复迁移；错误 execution generation 的 receipt 会回滚 command projection 和 assignment 两项写入。

## 管理入口与命令

`npm run test:e2e:hub007 --workspace @termx/web-controller` 使用 Playwright 启动真实 supervisor，通过 Operator UI 完成：

1. 输入 development operator token 登录；
2. 查看两个 Hub/Relay fleet 项；
3. 选择 development 账号；
4. 从 UI 发起 `daemon-edge-a -> hub-edge-b` assignment migration，并验证 management command HTTP 202；
5. 从 UI 发起 `client-dev-local` device revoke；
6. 验证 UI 刷新后设备进入 revoked 状态。

命令链路证据矩阵：

| 命令/效果 | Harness |
| --- | --- |
| assignment fence/migration | `TestHUB007EdgeRestartAssignmentMigrationAndControllerOutage` |
| KickPresence exact target/replay | `TestHubCommandKicksOnlyExactPresenceAndReplaysResult` |
| device revoke authority | Playwright `hub007.spec.ts` 与 planner/SQLite command tests |
| managed PeerSession close | `TestSignedCloseCommandCrossesControllerEdgeAndDaemonHTTPBoundaries` |
| terminal grant revoke | 同一真实 Controller-Edge-daemon HTTP harness，daemon AccessStore deny-only receipt |
| Relay allocation close | 同一 harness 的真实 Pion Relay DataChannel、remote close、final usage drain 与 settlement |

## Proto 与隐私

- `AssignmentMigrationTarget` 是唯一迁移 API contract，包含 migration ID、source Hub/generation/epoch 与 target Hub/epoch/lease；生成 Go/TypeScript 和 descriptor fixture 同步更新。
- 账号用户 API 明确拒绝 assignment migration；只有 operator admin mutation 入口可以创建。
- runtime 日志扫描禁止账号密码、operator token、Controller projection/daemon-control 私钥、Hub/Relay 私钥、daemon fixture 私钥、SDP/ICE password 和 terminal payload marker。
- topology、management 和 usage contract 的既有 descriptor 测试继续禁止 terminal、grant、credential、private key 与 payload 泄漏。

## 准入命令

```sh
./scripts/check-generated-code.sh
go test ./proto/cloudpb -count=1
(cd private/cloud && go test ./... -count=1)
(cd private/cloud && go test -race ./control-plane/commandoutbox ./control-plane/sqlite ./controller ./hub ./devcloud/cmd/termx-cloud-dev -count=1)
(cd private/cloud/web-controller/web && npm run typecheck && npm run build && npm run test:e2e:hub007)
git diff --check
```
