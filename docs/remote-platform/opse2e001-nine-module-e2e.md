# OPSE2E001 九模块运营后台总验收

## 完成范围

本切片把用户、订单、订阅、套餐、Hub、Agent、版本、优惠码和用户特权九个已实现模块放入同一真实纵向流程。测试使用一个 Controller、两个独立 Edge、真实 PostgreSQL 和真实 Web build；运营 mutation 由 Playwright 页面发起，Go harness 只建立进程/Presence 夹具并读取最终 oracle。

真实 Creem checkout、Webhook 和轮询仍属于下一切片 `CREEM001`。本切片的订单使用显式 development test provider，只验证已经完成的订单、优惠、Subscription 和 Entitlement 运营闭环，不冒充支付 provider 验收。

## 真值与消息链路

- PostgreSQL 保存 Account、Order、Subscription、catalog release、Hub directory、release catalog、promotion、typed override、CommandOutbox 和 audit；页面不保存第二份业务真值。
- 两个 Edge 是独立子进程，并通过真实 Hub control transport 向 Controller 建立 attachment。两台 daemon 通过真实 EdgeAccess、DeviceProof 和 Presence/report inventory 进入 Agent 投影。
- 页面发起 Kick 后，CommandOutbox 经 Controller -> Edge -> Presence 执行；页面等待 `EXECUTION APPLIED`，Go oracle再确认同一 command id。
- development supervisor 生成临时 Ed25519 release key，只把 public key 交给 Controller；credentials 只保存已经签名的 Proto JSON，不包含 release private key。
- 无 CSRF header 的危险 mutation 在已登录浏览器中返回 `401`；admin/readonly、近期认证和 CSRF 的完整角色矩阵继续由真实 Operator API 测试证明。

## 九模块证据

| 模块 | Web UI 动作 | 持久/运行结果 |
| --- | --- | --- |
| 用户 | 选择账号、Suspend、Restore | Subscription 状态恢复 ACTIVE，mutation audit 保留 |
| 订单 | 账号页使用 test provider 完成折后 checkout | 1 个 PAID order，价格 `$5.00`，provider event timeline 可见 |
| 订阅 | Operator 创建独立 EXTEND adjustment | 1 个 ACTIVE Subscription，adjustment revision 可见 |
| 套餐 | 编辑 generated Proto JSON 并发布新 catalog | 2 个不可变 catalog release，最新 head ACTIVE |
| Hub | 页面观察两个真实 Ready Edge，并创建 pending deployment | 3 个 PostgreSQL Hub directory 记录；真实双 Edge 未被 pending 记录冒充 |
| Agent | 两条真实 daemon Presence；页面发起 Kick | 2 个按机器聚合 Agent；Kick command `EXECUTION APPLIED` |
| 版本 | 页面提交已签名 Android metadata 并激活 | 1 个验签 artifact 和 active channel head |
| 优惠码 | 页面发布 50%/单次 promotion 并在 checkout 使用 | 1 个 immutable promotion，订单保存折扣结果 |
| 用户特权 | 页面创建限时 `cloud_device_limit` typed override | 1 个 ACTIVE override，Entitlement/policy revision 推进 |

最终 restart oracle 位于 `.artifacts/opse2e001/runtime-evidence.json`。最后一次验收记录 1 个账号、1 个订单、1 个订阅、2 个 catalog release、3 个 Hub、2 个 Agent、1 个软件 release、1 个 promotion、1 个 privilege，并确认 Kick APPLIED。

## 重启与权限矩阵

| 场景 | 结果 |
| --- | --- |
| Edge B 进程重启 | PID 与 control generation 均推进；另一 Edge 保持运行 |
| PostgreSQL 17 进程 fast stop/start | 原 data dir 原地恢复，非新 schema 重建 |
| Controller 进程重启 | PID 推进，双 Edge 重新 attach，九模块数据和 command/audit 可查询 |
| admin + 近期认证 | 页面登录后九类 mutation 成功 |
| 缺少 CSRF | 同浏览器 cookie 下危险 release mutation 返回 `401`，无写入 |
| readonly | `TestOperatorAPIEnforcesRoleCSRFRecentAuthAndPersistsSubscriptionAudit` 返回 `403` |
| secret/log scan | password、operator token、DeviceIdentity private key 和 signaling/terminal payload 未出现在 Controller、Edge、Playwright 日志 |

## UI 证据

- `.artifacts/opse2e001/operator-desktop.png`
- `.artifacts/opse2e001/operator-mobile.png`
- `.artifacts/opse2e001/playwright.log`
- `.artifacts/opse2e001/runtime-evidence.json`

Playwright 使用 1440x960 和 390x844。总数据页面最初暴露 Directory 搜索栏和 commerce audit 的两处真实横向溢出；修复后 document overflow 不超过 1px。最终页面截图包含九模块状态与已 APPLIED 的管理命令。

## 准入命令

```sh
scripts/with-test-postgres.sh go test ./private/cloud/devcloud/cmd/muxvia-cloud-dev -run '^TestOPSE2E001NineModuleOperatorWorkflow$' -count=1
scripts/with-test-postgres.sh go test -race ./private/cloud/devcloud/cmd/muxvia-cloud-dev -run '^TestOPSE2E001NineModuleOperatorWorkflow$' -count=1
scripts/with-test-postgres.sh go test ./private/cloud/web-controller -run '^TestOperatorAPIEnforcesRoleCSRFRecentAuthAndPersistsSubscriptionAudit$' -count=1
npm run typecheck --workspace @muxvia/web-controller
npm run build --workspace @muxvia/web-controller
make test-private
GOFLAGS='-p=1' make test
./scripts/check-generated-code.sh
git diff --check
```

公共全量首次运行命中 `docs/remote-platform/opsuser001-e2e.md` 已记录的 IPC close/register 同进程时序 flake；20 个独立测试进程全部通过，随后完整串行门禁通过。本切片未修改 IPC。
