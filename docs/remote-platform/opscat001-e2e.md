# OPSCAT001 套餐与用户特权验收

日期：2026-07-25

## 完成范围

- `PlanCatalog` 由 PostgreSQL 不可变 release、active head 和 canonical `plan_id + plan_version` 共同持久化；部署 JSON 只在空库执行一次 bootstrap。
- 新 checkout 读取 active catalog；旧订单、订阅和支付事件直接读取 canonical 历史套餐，不受后续发布次数影响。
- configured 套餐必须绑定明确 Creem product ID；included/contact 套餐禁止携带该 mapping。
- `EntitlementOverride` 只接受 `FieldMask + PlanCapability` 白名单字段，使用 expected revision CAS，拒绝重叠字段时间窗。
- 创建、更新、撤销、自然生效和到期与 Entitlement 重算在同一 PostgreSQL 事务提交，并记录 operator mutation audit。
- Controller 每分钟有界 reconciliation；提交后进入既有 policy publisher，Hub control acknowledgement 推进 projection revision。

## 自动化证据

- `scripts/check-generated-code.sh`
- `scripts/with-test-postgres.sh env GOWORK=off go test ./... -count=1`，在 `private/cloud/control-plane`、`private/cloud/controller` 和 `private/cloud/web-controller` 分别通过。
- `npm run typecheck --workspace @muxvia/web-controller`
- `npm run build --workspace @muxvia/web-controller`
- `npx playwright test e2e/opscat001.spec.ts`：真实 dev Controller、PostgreSQL 和两个 Edge；从 Operator 页面登录、发布 catalog、选择账号、创建类型化特权，并观察 Hub fleet projection acknowledgement。

## UI 证据

- 桌面：`.artifacts/opscat001/operator-desktop.png`
- 移动端 390x844：`.artifacts/opscat001/operator-mobile.png`
- 两个视口均无横向溢出；catalog editor 在版本切换和发布后复位到 JSON 开头。

## 安全边界

- Operator mutation 继续要求独立 listener、admin、CSRF 和五分钟近期认证。
- Creem API key、Webhook secret、普通账号 Session token 和 provider 原始 payload 均未写入 schema、仓库、截图或日志。
- Hub policy 只消费归一化 Entitlement，不读取价格、Creem mapping、覆盖原因或 terminal capability。
