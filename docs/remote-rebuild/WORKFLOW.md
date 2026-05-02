# TermX Remote Rebuild Workflow

Status file for unattended remote rebuild work. Update this file before starting and after completing every todo.

## Current State

- Current phase: Remote Web / Hub / Agent Buildout.
- Active todo: `3` Machine, app certificate, and control model committed; next todo `4`.
- Last updated: 2026-05-03T06:42:00+08:00.
- Worktree note: repository was already dirty at task start. Existing dirty files include root and package AGENTS files, remote rebuild docs, `go.work.sum`, and untracked `docs/remote-rebuild/hub-web-implementation-plan.md` plus `remote-ui/docs/relay-plan-product-policy.md`. Do not revert or overwrite those user-provided changes.
- Current product conclusion: unauthenticated/offline free users may use only `local` / LAN / self-hosted FRP or public ports; registered free users may use TermX rendezvous/signaling plus STUN for `public_p2p`; registered free users must not receive TermX TURN credentials; paid users may use `managed` relay subject to quota, session limit, and throttling.
- Client-visible paths are only `local / public_p2p / managed`. Relay is not a fourth client transport and may appear only as connection info, capability, policy, quota, or telemetry.
- Allowed statuses include `pending`, `in_progress`, `blocked`, `blocked_external`, `review`, `completed`, `resolved`, `deferred`, and `deferred_external`.
- Required workflow check: `bash docs/remote-rebuild/check_workflow_rules.sh`.

## Ordered Todos

| ID | Phase | Todo | Status | Commit |
| --- | --- | --- | --- | --- |
| 0 | docs/workflow | Harden AGENTS and reset `WORKFLOW.md` for the web/control-plane + hub + daemon-agent buildout | completed | `41671b21` |
| 1 | web-control | Create Web Control Plane skeleton with Go backend, SQLite migration/test helper, health API, and Vite React shell | completed | `e3f8541b` |
| 2 | web-control/auth | Implement web auth/account/plan/subscription foundations with provider interfaces and mock payment | completed | `f04a92c1` |
| 2-A | web-control/auth-review | Fix Slice 2 review findings around SQLite upgrade migration, deterministic auth, atomic refresh, subscription expiry, mock default, provider ownership, and workflow staleness | completed | `f04a92c1` |
| 2-A-A | web-control/auth-self-review | Harden provider order identity and missing token issuer error paths found during local self-review | completed | `f04a92c1` |
| 2-A-B | web-control/auth-follow-up-review | Fix Slice 2 follow-up review findings around Me nil issuer, pending payment sync, and payment/subscription transaction atomicity | completed | `f04a92c1` |
| 2-B | external | Defer real payment/email/OAuth/billing/tax/risk integrations behind provider interfaces | deferred_external |  |
| 3 | web-control/machines | Implement machine, app device, app certificate, revocation, bootstrap, and claim control model | completed | `732689ba` |
| 3-A | web-control/machines-self-review | Reject uploaded private-key fields and prevent bootstrap mutation of claimed machines | completed | `732689ba` |
| 3-B | web-control/machines-review | Fix Slice 3 review findings for strict certificate metadata, certificate signature verification, claim proof, and conditional bootstrap writes | completed | `732689ba` |
| 3-B-A | web-control/machines-deferred | Preserve signed certificate bytes for future cross-service verification before hub/agent certificate envelope use | deferred | `732689ba` |
| 3-B-B | web-control/machines-deferred | Add claim token TTL/rotation policy with daemon pairing UX | deferred | `732689ba` |
| 4 | public_p2p | Implement registered public P2P rendezvous with authenticated channel, offer/answer/candidate forwarding, TTL, rate limits, and STUN-only policy | pending |  |
| 5 | hub | Create Hub skeleton and agent registry with register, heartbeat, poll, answer, and expiry behavior | pending |  |
| 6 | managed-signaling | Implement managed signaling without TURN relay as HTTP control/signaling only, runtime still WebRTC DataChannel | pending |  |
| 7 | paid-relay | Implement paid TURN/STUN relay MVP with temporary TURN credentials, relay lease, and no free TURN | pending |  |
| 8 | quota | Implement relay quota, active relay session limit, heartbeat, TTL cleanup, and throttling | pending |  |
| 9 | daemon-agent | Integrate `termx daemon` cloud bootstrap, hub heartbeat/poll/answer, and WebRTC offer handling | pending |  |
| 10 | remote-ui | Connect `remote-ui` to real Web Control / public_p2p / managed API adapters while keeping `RtcSession` runtime boundary | pending |  |
| 11 | devstack | Build local devstack and optional external server smoke runbook for public STUN/TURN/signaling tests | pending |  |

## Buildout Todo Details

### 0 Documentation, AGENTS, And Workflow Reset

- 状态：completed
- 父条目：none
- 来源：用户要求施工前先检查 git status、读取指定文档、更新根 AGENTS、创建或更新 `docs/remote-rebuild/WORKFLOW.md`，并把后续 web/hub/agent 工作流文件化。
- 目标：固化当前无人值守、TDD、小切片、subagent review、mock 外部依赖、三类 connection path、relay 非第四 transport、外部服务器安全规则，并让 `WORKFLOW.md` 成为新主线事实 todo。
- 范围：`AGENTS.md`、`termx-core/AGENTS.md`、`remote-ui/AGENTS.md`、新建服务目录的 `AGENTS.md`、`docs/remote-rebuild/WORKFLOW.md`、`docs/remote-rebuild/check_workflow_rules.sh`。
- 非目标：不实现 web/hub/agent 业务代码；不修改旧 P2/P3 历史记录语义；不使用外部服务器。
- 外部依赖：无。
- mock 策略：不适用。
- 先写的失败测试：新增 `docs/remote-rebuild/check_workflow_rules.sh`，检查 AGENTS 和 WORKFLOW 是否包含新 buildout 主线、stable todo、deferred external、subagent review 和三类 path 规则。
- 预期失败结果：`bash docs/remote-rebuild/check_workflow_rules.sh` 在旧 workflow 上失败，报缺少 `Remote Web / Hub / Agent Buildout`。
- 实现摘要：已新增 `docs/remote-rebuild/check_workflow_rules.sh`；已将 `WORKFLOW.md` 顶部切到 Remote Web / Hub / Agent Buildout 主线，保留旧 P2/P3 记录为历史区；已创建 `web-control/AGENTS.md` 和 `termx-hub/AGENTS.md`，明确服务边界、TDD/review/workflow、外部依赖 mock、三类 connection path 和 relay policy 规则。
- 重构摘要：旧移动 P4-A 不再是 active todo；新的 stable todo 使用 `0` 到 `11`，并为每个主切片写入完整条目字段。
- 运行命令：`git status --short`; `bash docs/remote-rebuild/check_workflow_rules.sh`。
- 测试结果：预期失败已确认：旧 `WORKFLOW.md` 缺少新主线。修复后 `bash docs/remote-rebuild/check_workflow_rules.sh` passed。
- subagent review：已发起 `Descartes` (`019dea02-35d8-71c0-8d5a-760ca9d24d2f`) 对 Slice 0 AGENTS/WORKFLOW/check script 做独立 review。
- review 发现：`Descartes` reported: high stale bottom `Next Exact Action` still pointed to P4-A mobile migration; medium workflow check was only string-presence and did not catch stale next action or required fields; low workflow text still said the check needed a rerun in the reviewed snapshot.
- review 后修复：strengthened `check_workflow_rules.sh` with forbidden stale-text checks and required-field validation for todos `0..11`; confirmed it fails on the old mobile-migration next action before cleanup; replaced the bottom next-action section with current buildout next steps; updated this review/result text.
- 新增派生条目：暂无。
- deferred human items：暂无。
- 剩余风险：现有工作树已有用户改动，后续 commit 必须只提交本任务相关文件，避免带入无关 dirty 文件。
- 下一步：complete Slice 0 after final workflow check, then continue Slice 1 web-control skeleton.
- commit：`41671b21`

### 1 Web Control Plane Skeleton

- 状态：completed
- 父条目：none
- 来源：整体目标架构要求 Web Control Plane 使用 Go 后端 + Vite React 前端 + SQLite 测试/开发数据库。
- 目标：创建 `web-control/`，提供 Go HTTP backend skeleton、SQLite migration/test helper、health API、Vite React shell、Tailwind 构建接入。
- 范围：`web-control/`、`go.work` 或 workspace 配置中必要的模块接入、`docs/remote-rebuild/WORKFLOW.md`。
- 非目标：不实现 auth、subscription、machine、hub、rendezvous、relay 业务。
- 外部依赖：无真实外部系统。
- mock 策略：数据库使用 SQLite 临时文件或内存测试库；外部 provider 暂不接入。
- 先写的失败测试：计划创建 `web-control/internal/httpapi/health_test.go` 覆盖 `GET /api/health`，`web-control/internal/store/migrations_test.go` 覆盖 SQLite open/migrate/idempotency，`web-control/frontend/src/App.test.tsx` 覆盖 Vite React shell health/status rendering，另用 `npm run build` 做 frontend build smoke。
- 预期失败结果：新测试在实现前应因 `httpapi`/`store`/frontend app skeleton 缺失而 fail/build fail。
- 实际失败结果：`cd web-control && go test ./...` failed because `web-control` was not yet listed in `go.work`; `cd web-control/frontend && npm test` failed with missing `./App`; `cd web-control/frontend && npm run build` failed with missing `tsconfig.json`.
- 实现摘要：created independent `web-control` Go module and added it to `go.work`; added `internal/httpapi` health router with `/api/health`; added `internal/store` SQLite open/migrate helper with idempotent schema for core future control-plane tables; added `cmd/web-control` minimal server; added Vite React frontend shell with TailwindCSS, connection-path status, and build/test/typecheck setup.
- 重构摘要：kept Slice 1 limited to skeleton behavior only; no auth/subscription/machine/hub/relay business logic implemented yet. After review, tightened SQLite dev opener to one physical connection until a pooled connector/hook is introduced, added startup DB migration wiring, and added negative FK enforcement coverage.
- 运行命令：`cd web-control && go test ./...`; `cd web-control && GOWORK=off go test ./...`; `cd web-control/frontend && npm install`; `cd web-control/frontend && npm test`; `cd web-control/frontend && npm run typecheck`; `cd web-control/frontend && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `go test ./web-control/...`; `git diff --check`。
- 测试结果：expected failures recorded first. After implementation, `cd web-control && go test ./...` passed; frontend `npm test`, `npm run typecheck`, and `npm run build` passed; workflow check passed; `go test ./web-control/...` passed; `git diff --check` passed. After review fixes, `cd web-control && GOWORK=off go test ./...`, `go test ./web-control/...`, frontend `npm run build`, workflow check, and `git diff --check` passed.
- subagent review：`Mencius` (`019dea0d-a4a6-79a2-aee4-73ed8c8b9609`) reviewed Slice 1.
- review 发现：high stale `Next Exact Action` and Slice 1 `下一步` still described old work; medium SQLite foreign key PRAGMA was not proven across real usage; medium runnable backend reported health without opening/migrating SQLite.
- review 后修复：updated next-action text; added FK negative insert test; constrained SQLite opener to one physical connection for the skeleton; added `openStoreFromEnv` startup migration and test proving the runnable backend opens/migrates SQLite.
- 新增派生条目：暂无。
- deferred human items：暂无。
- 剩余风险：需要避免把 web-control 放入 `termx-core`，并避免引入非 Tailwind 全局 CSS 系统。
- 下一步：rerun review-fix checks, commit Slice 1, record commit hash, then start Slice 2 auth/account/plan with provider-interface mocks.
- commit：`e3f8541b`

### 2 Web Auth, Account, Plan, Subscription

- 状态：completed
- 父条目：none
- 来源：Web Control Plane 需要用户注册、登录、session/token、plans、subscriptions 和 mock payment provider。
- 目标：实现 users、sessions/tokens、plans、subscriptions、PaymentProvider interface、MockPaymentProvider，并覆盖订阅激活/过期/失败状态流转。
- 范围：`web-control/` backend/API/db/tests，必要前端 shell 可只消费基础 `me`/plan 状态。
- 非目标：不接真实支付、发票、税务、短信、OAuth、外部风控。
- 外部依赖：真实支付、真实订阅/发票/税务、真实邮件/OAuth 以后需要人类配置。
- mock 策略：用 `PaymentProvider` interface 和 `MockPaymentProvider`/local fake；如需要邮件/OAuth，先建 interface + in-memory/local provider。
- 先写的失败测试：planned `web-control/internal/account/account_test.go` for register/login/refresh/default plan/unauthenticated rejection, `web-control/internal/account/mock_payment_test.go` for `PaymentProvider` and mock activation/failure/expiry flows, plus HTTP API tests for auth endpoints.
- 预期失败结果：focused tests should fail before implementation because `internal/account`, provider interfaces, token/session store, password hashing, and auth API handlers do not exist yet.
- 实际失败结果：`cd web-control && GOWORK=off go test ./internal/account ./internal/httpapi` failed as expected because `internal/account` had no non-test Go files and HTTP auth handlers/account wiring were absent.
- 实现摘要：added `internal/account` with user registration/login/refresh/me service, HMAC access token issuer, refresh token persistence/rotation, default registered-free plan policy, `PaymentProvider` interface, and `MockPaymentProvider` with success/failure/expiry simulation; extended SQLite migrations for sessions and payment orders; added auth HTTP endpoints for register/login/refresh/me; wired runnable backend auth service creation from `TERMX_WEB_CONTROL_TOKEN_SECRET`.
- 重构摘要：kept real external payment/email/OAuth out of the slice. Payment behavior is isolated behind `PaymentProvider`; mock payment is a local provider for tests/dev and is not hard-coded into core policy. Self-review hardening added access-token expiry verification, old refresh-token rejection after rotation, and startup auth wiring requiring an explicit token secret.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/account ./internal/httpapi`; `cd web-control && GOWORK=off go test ./...`; `go test ./web-control/...`; `cd web-control/frontend && npm test`; `cd web-control/frontend && npm run typecheck`; `cd web-control/frontend && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`。
- 测试结果：expected failure recorded first. After implementation, focused account/httpapi tests passed; `GOWORK=off go test ./...` passed; workspace `go test ./web-control/...` passed; frontend test/typecheck/build passed; workflow check and `git diff --check` passed. After self-review hardening, `GOWORK=off go test ./...`, `go test ./web-control/...`, workflow check, and `git diff --check` passed again. After subagent review, `2-A` regression tests failed as expected on schema upgrade/provider override gaps, then focused `GOWORK=off go test ./internal/store ./internal/account ./internal/httpapi ./cmd/web-control` passed after fixes. Broader validation after `2-A`, `2-A-A`, and `2-A-B`: focused tests passed, `GOWORK=off go test ./...` passed, `go test ./web-control/...` passed, frontend `npm test`/`npm run typecheck`/`npm run build` passed, workflow check passed, and `git diff --check` passed.
- subagent review：`Tesla` (`019dea1d-6b04-7e91-8967-bc312c8102c8`) reviewed Slice 2, reviewed follow-up fixes, and gave final no-blocker disposition.
- review 发现：high existing SQLite DBs from Slice 1 are not upgraded when adding `subscriptions.provider_order_id`; high access token verification used wall clock instead of injected service clock; high refresh-token rotation was not atomic; medium subscription entitlement ignored `current_period_end`; medium mock payment provider was the default service behavior; medium payment sync trusted provider status without checking order ownership; medium workflow next action and expected deferred child items were stale.
- review 后修复：`2-A` added regression tests and implemented idempotent SQLite upgrade migration, injected-clock access verification, transactional refresh-token rotation, subscription period-end entitlement fallback, explicit payment provider requirement, provider order ownership validation, and workflow/deferred external tracking. `2-A-A` and `2-A-B` fixed the follow-up provider/token/payment transaction findings.
- 新增派生条目：`2-A` review hardening fixes; `2-A-A` local self-review provider/token boundary fix; `2-A-B` follow-up review fixes; `2-B` deferred external provider integrations.
- deferred human items：真实支付、发票/税务、邮件、OAuth provider、生产密钥。
- 剩余风险：auth/token implementation is a first local skeleton; real production token rotation, password policy hardening, email verification, OAuth, billing provider, invoice/tax, and fraud/risk systems remain deferred external integrations.
- 下一步：commit Slice 2, record commit hash, then start Slice 3 machine/app certificate/control model.
- commit：`f04a92c1`

### 2-A Slice 2 Review Hardening

- 状态：completed
- 父条目：2
- 来源：Slice 2 subagent review by `Tesla`.
- 目标：fix the blocking review findings with regression tests before code changes.
- 范围：`web-control/internal/account`, `web-control/internal/store`, `web-control/cmd/web-control`, `docs/remote-rebuild/WORKFLOW.md`.
- 非目标：do not add real payment, email, OAuth, invoice, tax, fraud/risk, or external provider integrations.
- 外部依赖：none for this fix slice.
- mock 策略：mock payment remains test/dev only and must be explicitly injected; production-like construction must not silently default to mock.
- 先写的失败测试：planned tests for upgrading Slice 1 schema, deterministic token expiry using injected clock, old refresh token single-use/atomic rotation, expired paid subscription falling back to free policy, explicit payment provider requirement, and mismatched provider order rejection.
- 预期失败结果：focused tests should fail against the reviewed implementation.
- 实现摘要：added idempotent migration upgrade inspection for `subscriptions.provider_order_id`; changed access-token verification to use the service clock; rotated refresh tokens inside a transaction with conditional revocation; made paid order creation/sync require an explicitly injected provider; validated provider order user/plan ownership; made active paid subscriptions fall back to free policy after `current_period_end`; added `MockPaymentProvider.OverrideOrder` only for test/dev mutation.
- 重构摘要：split current-plan lookup into db/tx query paths so auth issuance does not query through the pooled DB while holding a SQLite transaction; kept mock payment isolated behind `PaymentProvider`.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/store ./internal/account ./internal/httpapi ./cmd/web-control`; `cd web-control && GOWORK=off go test ./...`; `go test ./web-control/...`; `cd web-control/frontend && npm test`; `cd web-control/frontend && npm run typecheck`; `cd web-control/frontend && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`。
- 测试结果：red path confirmed before fixes: migration upgrade failed because old Slice 1 `subscriptions` lacked `provider_order_id`, and account tests failed while provider override/token-clock fixes were not implemented. After fixes, focused store/account/httpapi/main tests passed. Broader module/workspace/backend/frontend/workflow/diff checks all passed, and passed again after `2-A-A`.
- subagent review：covered by parent Slice 2 review; follow-up review requested from `Tesla` after `2-A` fixes.
- review 发现：follow-up review found medium risks: `Me` nil-panics when `TokenIssuer` is missing; pending/unknown provider statuses create subscription history; payment order status update and subscription insert are not atomic. It also noted the concurrent refresh test is low-value for true pooled-connection races because the SQLite opener currently uses one connection. A registration partial-state finding was already fixed in `2-A-A` before the review result was received.
- review 后修复：implemented the fixes listed above; broader validation passed; follow-up review request was updated to include `2-A-A`; remaining follow-up findings were fixed in `2-A-B` and final review found no blockers.
- 新增派生条目：`2-A-A` local self-review hardening for provider order identity and missing token issuer errors; `2-A-B` follow-up review fixes.
- deferred human items：none.
- 剩余风险：future production auth/payment hardening remains outside this fix slice and is tracked by `2-B`.
- 下一步：included in Slice 2 commit.
- commit：`f04a92c1`

### 2-A-A Provider And Token Boundary Self-Review Fix

- 状态：completed
- 父条目：2-A
- 来源：local self-review while waiting for Slice 2 follow-up subagent review.
- 目标：ensure provider order identity is validated, local order IDs are not controlled by provider IDs, and missing token issuer paths return errors without partial writes or panics.
- 范围：`web-control/internal/account`, account tests, `docs/remote-rebuild/WORKFLOW.md`.
- 非目标：do not implement real payment provider, email/OAuth, invoice/tax, fraud/risk, or public paid API endpoints.
- 外部依赖：none.
- mock 策略：continue using explicitly injected `MockPaymentProvider`; use provider mutation helpers only in tests.
- 先写的失败测试：tests for rejecting provider ID mismatch, keeping local order ID separate from provider order ID, failing register/refresh cleanly without a token issuer, and ensuring failed register without token issuer rolls back the user row.
- 预期失败结果：focused account tests should fail before implementation because current payment sync does not compare provider order ID, refresh can dereference a nil token issuer, and register can leave a partial user after token issuance failure.
- 实现摘要：added explicit missing-token-issuer errors on refresh; split local payment order IDs from provider order IDs; validated provider order ID in addition to user and plan before syncing subscription state; made registration insert/plan seed/session issuance transactional so token failures roll back.
- 重构摘要：updated mock payment tests to mutate provider state by `ProviderOrderID` while service sync still uses local `PaymentOrder.ID`, preserving provider/local boundary; reused transaction-aware auth issuance for registration.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/account -run TestRegisterFailsCleanlyWithoutTokenIssuer`; `cd web-control && GOWORK=off go test ./internal/account`。
- 测试结果：red path confirmed first: refresh without token issuer panicked, separated order IDs required test contract updates, and register without token issuer left a partial user row. After fixes, focused account tests passed. Full Slice 2 focused/module/workspace/frontend/workflow/diff checks passed after the transactional register fix.
- subagent review：included in the updated Slice 2 follow-up review request to `Tesla`.
- review 发现：pending.
- review 后修复：implemented local self-review fixes; broader validation passed and follow-up subagent review accepted the fixes.
- 新增派生条目：none.
- deferred human items：none.
- 剩余风险：real provider webhook signature and idempotency remain deferred external/business hardening.
- 下一步：included in Slice 2 commit.
- commit：`f04a92c1`

### 2-A-B Follow-Up Review Fixes

- 状态：completed
- 父条目：2-A
- 来源：Slice 2 follow-up review by `Tesla`.
- 目标：fix remaining real behavior risks: `Me` must fail cleanly without token issuer, pending/unknown payment sync must not create subscription history, and payment status update plus subscription creation must be atomic.
- 范围：`web-control/internal/account`, account tests, `docs/remote-rebuild/WORKFLOW.md`.
- 非目标：do not implement real payment webhooks, provider idempotency keys, invoice/tax, OAuth/email, or public subscription HTTP endpoints.
- 外部依赖：none.
- mock 策略：continue using explicit `PaymentProvider`; mock provider remains test/dev only.
- 先写的失败测试：planned tests for `Me` without token issuer returning an error, pending payment sync not creating subscriptions, unknown provider status not creating subscriptions, and failed subscription insert rolling back local payment status.
- 预期失败结果：focused account tests should fail before implementation because `Me` lacks token guard, pending sync currently inserts a past_due subscription, and payment sync is not transactional.
- 实现摘要：added `Me` token-issuer guard; made pending payment sync update only local payment order status and return free policy without creating subscription history; rejected unknown provider statuses; wrapped paid/failed/expired payment status update plus subscription insert in one transaction.
- 重构摘要：kept provider status interpretation in service layer; retained `PaymentProvider` boundary and no HTTP subscription endpoint in this slice.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/account -run 'TestMeFailsCleanlyWithoutTokenIssuer|TestSyncPaymentPendingDoesNotCreateSubscription|TestSyncPaymentRejectsUnknownProviderStatus|TestSyncPaymentRollsBackOrderStatusWhenSubscriptionInsertFails'`; `cd web-control && GOWORK=off go test ./internal/store ./internal/account ./internal/httpapi ./cmd/web-control`; `cd web-control && GOWORK=off go test ./...`; `go test ./web-control/...`; `cd web-control/frontend && npm test`; `cd web-control/frontend && npm run typecheck`; `cd web-control/frontend && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`。
- 测试结果：red path confirmed first: `Me` nil issuer panicked, pending payment created `past_due` subscription, unknown provider status was accepted, and subscription insert failure left payment order status as `paid`. After fixes, focused follow-up review regression tests passed. Full focused/module/workspace/frontend/workflow/diff checks passed.
- subagent review：follow-up review produced these findings; final disposition pending after fixes.
- review 发现：medium `Me` nil issuer panic; medium pending/unknown payment sync creates subscription history; medium payment update/subscription insert not atomic; low concurrent refresh test does not prove pooled-connection race due current one-connection SQLite opener.
- review 后修复：implemented all medium follow-up findings; broader validation passed; final review disposition from `Tesla` reported no remaining blocker findings.
- 新增派生条目：none.
- deferred human items：real provider webhook signature/idempotency remains deferred external.
- 剩余风险：real billing semantics and production-grade idempotent provider sync remain for future provider integration.
- 下一步：included in Slice 2 commit.
- commit：`f04a92c1`

### 2-B Deferred Real External Providers

- 状态：deferred_external
- 父条目：2
- 来源：Slice 2 introduces provider interfaces and local mock payment flows but real payment/email/OAuth/billing integrations require external accounts and human configuration.
- 目标：record human-dependent integrations so they do not block the unattended buildout.
- 范围：future `web-control` provider implementations and deployment configuration.
- 非目标：do not call real payment processors, email/SMS providers, OAuth providers, invoice/tax systems, or fraud/risk APIs in Slice 2.
- 外部依赖：real payment account, subscription/billing provider, invoice/tax setup, email provider, OAuth app credentials, third-party risk/analytics credentials, production secrets.
- mock 策略：current Slice 2 uses `PaymentProvider` plus explicitly injected `MockPaymentProvider` for tests/dev. Future real providers replace that interface.
- 先写的失败测试：not applicable for deferred external item.
- 预期失败结果：not applicable.
- 实现摘要：deferred.
- 重构摘要：deferred.
- 运行命令：not run.
- 测试结果：not run.
- subagent review：Tesla required this deferred item.
- review 发现：mock provider must not be treated as production default.
- review 后修复：recorded as deferred_external and `2-A` will require explicit provider injection.
- 新增派生条目：none.
- deferred human items：provide real provider accounts, API keys, webhook signing secrets, OAuth client config, invoice/tax setup, production secret management.
- 剩余风险：until real providers exist, subscription/payment behavior is suitable only for local/dev/test.
- 下一步：resume when human-provided production provider details are available.
- commit：will be included in Slice 2 commit.

### 3 Machine, App Certificate, And Control Model

- 状态：completed
- 父条目：none
- 来源：machine/app certificate/control model 是 web-control 与 daemon agent 的 ownership 和安全边界。
- 目标：实现 machines、app_devices、app_certificates、revocation、agent bootstrap API、machine claim API，确保 private key 不上传。
- 范围：`web-control/` backend/db/API/tests；必要时只调整 `termx-core`/`termx-cli` 的公共合同，不塞入 web 业务。
- 非目标：不实现完整 hub signaling、TURN relay、terminal runtime HTTP proxy。
- 外部依赖：无真实外部系统；机器真实 claim UX 可以先本地/mock。
- mock 策略：claim challenge 用 local fake clock/key 测试；不 mock ownership 校验本身。
- 先写的失败测试：planned `web-control/internal/machines/machines_test.go` for machine bootstrap without private key storage, claim ownership, cross-user owner rejection, app certificate metadata without private key, certificate revocation, and revoked/expired cert validation rejection; planned `web-control/internal/httpapi/machines_test.go` for authenticated machine/certificate API behavior.
- 预期失败结果：focused tests should fail before implementation because `internal/machines`, machine service methods, auth-protected machine HTTP handlers, and certificate validation APIs do not exist yet.
- 实际失败结果：`cd web-control && GOWORK=off go test ./internal/machines ./internal/httpapi` failed as expected: `internal/machines` had no non-test Go files and HTTP tests could not build without machine service/router APIs.
- 实现摘要：added `internal/machines` service with bootstrap, claim, owner-scoped list/detail, app certificate metadata registration/list/revoke/validate, private-key stripping for certificate payload metadata, and startup/router wiring for machine endpoints. `3-A` then hardened private-key upload rejection and claimed-machine bootstrap mutation rejection.
- 重构摘要：kept machine/app certificate business in `web-control`; reused the account service for authenticated HTTP user lookup; no terminal/file/api/events runtime proxy or WebRTC implementation types were introduced.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/machines ./internal/httpapi`; `cd web-control && GOWORK=off go test ./...`; `go test ./web-control/...`; `cd web-control/frontend && npm test`; `cd web-control/frontend && npm run typecheck`; `cd web-control/frontend && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`。
- 测试结果：red path confirmed before implementation with missing `internal/machines` implementation and router wiring. After implementation and FK-real test adjustments, focused machine/httpapi tests passed. Broader web-control module tests, workspace tests, frontend test/typecheck/build, workflow guard, and diff check all passed before review. `3-A` red tests failed as expected, then focused machine/httpapi tests passed after hardening. Full broader checks passed again after `3-A`.
- subagent review：requested from `Arendt` (`019dea39-7057-7580-a691-a180ec4e5621`) after broader checks passed; review request updated to include `3-A`; final disposition after `3-B` reported no blocker findings.
- review 发现：`Arendt` found high risk that `certificate_payload` could still store private-key-shaped metadata such as JWK `d` or PEM values; high risk that app certificate validation checked only revocation/expiry and not machine-signature authenticity or payload binding; medium risk that machine claim used only `machine_id`; medium risk that bootstrap upsert was not write-condition protected against claimed-machine mutation races.
- review 后修复：`3-B` implemented strict certificate metadata, Ed25519 signature/binding verification before storage, claim token proof, and conditional bootstrap writes. Final review found no blockers.
- 新增派生条目：`3-A` bootstrap private-key and claimed-machine hardening; `3-B` review fixes for certificate metadata/authenticity, claim proof, and conditional bootstrap writes; deferred `3-B-A` signed-bytes canonical envelope follow-up; deferred `3-B-B` claim token TTL/rotation follow-up.
- deferred human items：生产 claim/pairing UX 可后置，但接口和测试先稳定；claim token TTL/rotation and cross-service certificate envelope canonicalization are deferred non-blocking follow-ups before hub/agent production use.
- 剩余风险：agent-side certificate verification is still Slice 9; future hub/agent consumers should verify exact signed certificate bytes or use a canonical envelope before relying on stored payload across services.
- 下一步：commit Slice 3, record commit hash, then start Slice 4 public_p2p rendezvous.
- commit：`732689ba`

### 3-A Machine Bootstrap Private-Key And Ownership Hardening

- 状态：completed
- 父条目：3
- 来源：local self-review during Slice 3 review wait found bootstrap accepted private-key fields and could mutate already claimed machines by caller-supplied ID.
- 目标：reject machine/app private-key fields at service and HTTP boundaries and ensure unauthenticated bootstrap cannot update an owned machine.
- 范围：`web-control/internal/machines`, `web-control/internal/httpapi`, tests, `docs/remote-rebuild/WORKFLOW.md`.
- 非目标：do not implement real machine signature challenge or production claim UX in this child slice.
- 外部依赖：none.
- mock 策略：no external systems; tests use SQLite and account service users.
- 先写的失败测试：planned tests for service/bootstrap rejecting `MachinePrivateKey`, HTTP bootstrap rejecting `machine_private_key`, app certificate registration rejecting `AppPrivateKey`, HTTP certificate registration rejecting `app_private_key`, and claimed-machine bootstrap update rejection.
- 预期失败结果：focused machine/httpapi tests should fail before implementation because current code strips private fields and allows bootstrap update by ID.
- 实现摘要：`Bootstrap` now rejects `MachinePrivateKey`; bootstrap by supplied machine ID now refuses to update an already claimed machine; app certificate registration rejects `AppPrivateKey`; HTTP APIs surface those rejections instead of stripping secrets silently.
- 重构摘要：wrapped bootstrap upsert in a transaction so claimed-machine inspection and insert/update happen together.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/machines ./internal/httpapi`。
- 测试结果：red tests confirmed first: service accepted uploaded machine/app private keys, HTTP bootstrap accepted `machine_private_key`, and bootstrap could mutate an owned machine by ID. After hardening, focused machine/httpapi tests passed. Full Slice 3 focused/module/workspace/frontend/workflow/diff checks passed after the hardening.
- subagent review：will be included in Slice 3 review/follow-up.
- review 发现：pending.
- review 后修复：local self-review fix implemented; final subagent review accepted this child fix with no blockers.
- 新增派生条目：none.
- deferred human items：production machine signature challenge remains future Slice 9/agent integration.
- 剩余风险：without real daemon signature proof, bootstrap identity remains local/dev control-plane skeleton only.
- 下一步：included in Slice 3 commit.
- commit：`732689ba`

### 3-B Slice 3 Review Fixes

- 状态：completed
- 父条目：3
- 来源：Slice 3 subagent review by `Arendt`.
- 目标：replace permissive certificate payload storage with strict public metadata, verify app certificate signatures/bindings, require a claim proof beyond machine_id, and enforce claimed-machine bootstrap protection in the write itself.
- 范围：`web-control/internal/machines`, `web-control/internal/httpapi`, tests, `docs/remote-rebuild/WORKFLOW.md`.
- 非目标：do not implement production daemon claim UX, real local pairing UI, or hub/agent runtime certificate verification in this slice.
- 外部依赖：none.
- mock 策略：tests generate local Ed25519 keys in-process; no external providers.
- 先写的失败测试：planned tests for rejecting private-key-shaped certificate metadata values (`d`, PEM/private-key material), rejecting invalid certificate signature or mismatched payload binding, requiring a claim token, and preventing claimed-machine bootstrap mutation by SQL write condition.
- 预期失败结果：focused machine/httpapi tests should fail before implementation because current code strips only key names, does not verify signatures/bindings, accepts claim by machine_id, and relies on pre-upsert check for claimed-machine updates.
- 实现摘要：bootstrap now issues a claim token and stores only its hash; claim requires the token and clears it on success; machine migration adds `claim_token_hash` with upgrade coverage; app certificate registration rejects private-key-shaped metadata and verifies Ed25519 signatures against the machine public key with payload binding for `machine_id`, `app_public_key`, and `expires_at`; bootstrap upsert now uses a write-side `WHERE machines.owner_user_id IS NULL` guard.
- 重构摘要：replaced permissive certificate payload stripping with strict validation before storage; kept claim proof local/control-plane only until daemon production UX arrives.
- 运行命令：`cd web-control && GOWORK=off go test ./internal/machines ./internal/httpapi ./internal/store`; `cd web-control && GOWORK=off go test ./...`; `go test ./web-control/...`; `cd web-control/frontend && npm test`; `cd web-control/frontend && npm run typecheck`; `cd web-control/frontend && npm run build`; `bash docs/remote-rebuild/check_workflow_rules.sh`; `git diff --check`。
- 测试结果：red tests confirmed first: bootstrap returned no claim token, claim by machine_id succeeded without proof, private-key-shaped certificate metadata was accepted, and signature/binding checks were absent. After fixes, focused machine/httpapi/store tests passed. Full web-control module tests, workspace tests, frontend checks, workflow guard, and diff check passed; focused store migration regression also passed after adding old-machine-schema upgrade coverage.
- subagent review：`Arendt` found the issues above; final disposition pending after fixes.
- review 发现：see parent `3`.
- review 后修复：implemented all `Arendt` findings; broader validation passed; final review disposition reported no blocker findings.
- 新增派生条目：`3-B-A`; `3-B-B`.
- deferred human items：production claim UX remains deferred to daemon/app integration, but current claim token must be real enough for control-plane ownership tests.
- 剩余风险：agent-side certificate verification is still Slice 9; this slice verifies metadata before storage. Exact signed bytes/canonical envelope and claim token TTL/rotation are deferred in `3-B-A` and `3-B-B`.
- 下一步：included in Slice 3 commit.
- commit：`732689ba`

### 3-B-A Signed Certificate Envelope Follow-Up

- 状态：deferred
- 父条目：3-B
- 来源：Slice 3 final review residual risk: stored `certificate_payload` is re-marshaled after verification, so future hub/agent consumers should not assume it is the exact signed byte sequence.
- 目标：before hub/agent certificate envelope use, preserve exact signed bytes or define a canonical envelope and tests proving cross-service verification uses the same bytes.
- 范围：future web-control/hub/agent certificate envelope code.
- 非目标：not required for current Slice 3 storage/metadata behavior.
- 外部依赖：none.
- mock 策略：use local Ed25519 fixtures.
- 先写的失败测试：deferred.
- 预期失败结果：deferred.
- 实现摘要：deferred.
- 重构摘要：deferred.
- 运行命令：not run.
- 测试结果：not run.
- subagent review：Arendt identified this as residual risk, not a blocker.
- review 发现：future cross-service consumers must verify exact signed bytes or canonical form.
- review 后修复：deferred non-blocking follow-up.
- 新增派生条目：none.
- deferred human items：none.
- 剩余风险：do not use stored re-marshaled payload as a cross-service signed envelope until this is resolved.
- 下一步：resolve before hub/agent consumes app certificate envelopes.
- commit：`732689ba`

### 3-B-B Claim Token TTL And Rotation Follow-Up

- 状态：deferred
- 父条目：3-B
- 来源：Slice 3 final review residual risk: claim token is random and hashed, but has no TTL/rotation policy yet.
- 目标：add claim token TTL/rotation and daemon/local pairing UX before production cloud claim.
- 范围：future machine claim/pairing service, daemon bootstrap, web-control schema/tests.
- 非目标：not required for current local control-plane skeleton because claim token proof already prevents machine_id-only claim.
- 外部依赖：production pairing UX may involve daemon/app integration but no third-party provider.
- mock 策略：use fake clock and local daemon/app test harness.
- 先写的失败测试：deferred.
- 预期失败结果：deferred.
- 实现摘要：deferred.
- 重构摘要：deferred.
- 运行命令：not run.
- 测试结果：not run.
- subagent review：Arendt identified this as residual risk, not a blocker.
- review 发现：claim tokens need TTL/rotation before production use.
- review 后修复：deferred non-blocking follow-up.
- 新增派生条目：none.
- deferred human items：none.
- 剩余风险：long-lived claim tokens are acceptable only for this skeleton until production pairing work lands.
- 下一步：resolve with daemon pairing/claim UX.
- commit：`732689ba`

### 4 Registered Public P2P Rendezvous

- 状态：pending
- 父条目：none
- 来源：注册免费用户允许 TermX rendezvous/signaling + STUN 做 `public_p2p`，免费用户不得拿 TURN。
- 目标：实现 authenticated public P2P channel create、offer/answer/candidate forwarding、TTL、payload limit、rate limit、STUN-only response、unauthenticated reject。
- 范围：可先在 `web-control/` 或独立 `termx-rendezvous/` 落地，需与 `remote-ui` public_p2p adapter 合同一致。
- 非目标：不承载 terminal/file/api/events 数据面；不发 TURN；不实现 paid relay。
- 外部依赖：公网 DNS/TLS/真实 STUN 部署 deferred。
- mock 策略：本地 fake clock、in-memory/SQLite channel store、local STUN config。
- 先写的失败测试：待创建 TTL、payload limit、rate limit、no TURN、unauthenticated reject、free public_p2p 不含 TURN 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：预计创建 deferred external DNS/TLS/public STUN deploy。
- deferred human items：生产域名、TLS 证书、公网部署账号、abuse/captcha provider。
- 剩余风险：旧 anonymous 命名必须隔离或迁移，不能恢复匿名云端免费 rendezvous 产品结论。
- 下一步：Slice 3 完成后开始。
- commit：待提交。

### 5 Hub Skeleton And Agent Registry

- 状态：pending
- 父条目：none
- 来源：Hub/Signaling/Relay 服务需要 agent registry、heartbeat、poll-answer 基础。
- 目标：创建 `termx-hub/`，实现 hub service skeleton、agent register/heartbeat/poll/answer、in-memory registry、expiry cleanup。
- 范围：`termx-hub/` Go module/service/tests、必要协议包；可参考 `../tgent` hub registry 思路但不照搬对象模型。
- 非目标：不做 TURN、quota、terminal/file HTTP runtime proxy。
- 外部依赖：无。
- mock 策略：Control Plane client 用 fake interface；registry 第一版可 in-memory，TTL 行为必须真实。
- 先写的失败测试：待创建 agent online/offline、heartbeat expiry、poll timeout、answer correlation 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：暂无。
- deferred human items：生产 hub identity/registration secret 后置。
- 剩余风险：Hub 只能做 signaling/control/ICE/TURN，不得成为 HTTP runtime proxy。
- 下一步：Slice 4 后开始。
- commit：待提交。

### 6 Managed Signaling Without TURN

- 状态：pending
- 父条目：none
- 来源：先实现 managed connect ticket + hub signaling，但 runtime 仍只使用 WebRTC DataChannel。
- 目标：web creates managed connect ticket，app/browser submits offer to hub，hub forwards to agent，agent answers；expired/wrong-machine tickets rejected。
- 范围：`web-control/` ticket API, `termx-hub/` session signaling, `termx-core`/`termx-cli` daemon agent client where needed.
- 非目标：不发 TURN credential，不实现 relay lease，不承载 terminal/file/api/events HTTP runtime。
- 外部依赖：无真实外部系统。
- mock 策略：Control Plane ticket verifier interface + signed/local test keys；agent fake for hub tests。
- 先写的失败测试：待创建 expired ticket rejected、wrong machine rejected、free managed direct no relay、HTTP not runtime proxy 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：暂无。
- deferred human items：生产 ticket signing key rotation 后置。
- 剩余风险：HTTP long-poll 只能是建链前职责。
- 下一步：Slice 5 后开始。
- commit：待提交。

### 7 TURN/STUN Paid Relay MVP

- 状态：pending
- 父条目：none
- 来源：付费用户允许 managed relay，注册免费用户不允许 TermX TURN relay。
- 目标：实现 Pion TURN/STUN、temporary TURN credentials、relay session lease、paid user gets TURN、free user does not。
- 范围：`termx-hub/` TURN/STUN service and policy enforcement, `web-control/` relay lease API, tests.
- 非目标：不把 relay 变成客户端 path；不实现真实支付；不做多 region reconciliation。
- 外部依赖：生产公网 IP、DNS、TLS、TURN ports/firewall、cloud account deferred。
- mock 策略：local TURN secret, fake payment/subscription provider, in-memory/local relay traffic hooks.
- 先写的失败测试：待创建 credential expiry、policy deny、relay not client path、free/public_p2p no TURN 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：预计 external server testing deferred/runbook 条目。
- deferred human items：公网 TURN DNS/TLS/端口/云账号/备案或安全审批。
- 剩余风险：TURN 流量统计必须能绑定 relay session，不能只粗略绑定 agent。
- 下一步：Slice 6 后开始。
- commit：待提交。

### 8 Quota, Active Session Limit, And Throttling

- 状态：pending
- 父条目：none
- 来源：relay 是付费能力，需按 quota、session limit、throttle 策略执行。
- 目标：实现 monthly relay usage、relay session heartbeat、TTL cleanup、active session limit、over-quota terminal-friendly throttle。
- 范围：`web-control/` SQLite models/transactions/API/tests, `termx-hub/` heartbeat/usage client and local limiter.
- 非目标：不接真实计费，不做全球强同步 per-packet 计量。
- 外部依赖：真实 billing/subscription reconciliation deferred。
- mock 策略：fake clock, SQLite transaction tests, mock payment provider plan states, local limiter fake traffic.
- 先写的失败测试：待创建 quota exceeded throttled、session limit rejects extra lease、cleanup expires stale sessions、usage delta persisted 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：暂无。
- deferred human items：真实账单对账、发票、税务、支付回调。
- 剩余风险：SQLite transaction、TTL cleanup、quota/session limit 必须是真行为，不是只在内存 mock 成立。
- 下一步：Slice 7 后开始。
- commit：待提交。

### 9 TermX Daemon Cloud Integration

- 状态：pending
- 父条目：none
- 来源：`termx daemon` 内置 agent 需要 cloud bootstrap、hub register/heartbeat/poll/answer，并用既有 runtime 回答 WebRTC offer。
- 目标：实现 daemon cloud bootstrap、agent policy、hub register/heartbeat、signaling poll/answer、ticket/cert/signature verification、DataChannel labels `terminal:{terminal_id}`/`api`/`events`/`file:{transfer_id}`。
- 范围：`termx-core/` shell-neutral remote runtime, `termx-cli/` daemon command integration, tests.
- 非目标：不发布独立 agent 二进制；不把 web-control 支付/订阅业务塞进 core；不引入 browser WebRTC types。
- 外部依赖：真实 cloud account/token distribution deferred.
- mock 策略：fake web-control/hub servers in tests, local signing keys, fake clock.
- 先写的失败测试：待创建 agent verifies ticket/cert/signature、rejects wrong terminal、no browser WebRTC type leaks into core 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：暂无。
- deferred human items：production bootstrap token issuance/operator setup.
- 剩余风险：machine private key 必须只保存在本机。
- 下一步：Slice 8 后开始。
- commit：待提交。

### 10 remote-ui Real API Adapters

- 状态：pending
- 父条目：none
- 来源：remote-ui 后续只接真实 web/hub API adapter，运行时仍只通过 `RtcSession`。
- 目标：实现 Web Control API client、public_p2p signaling adapter、managed signaling adapter、capabilities UI/state，保留 path 仅 `local/public_p2p/managed`。
- 范围：`remote-ui/` TypeScript adapters/hooks/tests, generated localweb assets only when needed.
- 非目标：不恢复 `RemoteTransport` / `TerminalTransport` / `paid_relay` / `anonymous_p2p` / `managed_p2p` 等旧抽象。
- 外部依赖：真实 web/hub production endpoints deferred.
- mock 策略：mock API client/fake provider for adapter tests until real services run locally.
- 先写的失败测试：待创建 paths only local/public_p2p/managed、relayInUse info only、terminal/api/file/events use RtcSession only 测试。
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：暂无。
- deferred human items：真实 cloud endpoints、OAuth/payment policy feeds。
- 剩余风险：browser `RTCPeerConnection` / `RTCDataChannel` types must stay inside browser adapter and direct tests only.
- 下一步：Slice 9 后开始。
- commit：待提交。

### 11 Devstack And External Server Tests

- 状态：pending
- 父条目：none
- 来源：最终需要 local devstack 和可选公网 STUN/TURN/signaling smoke。
- 目标：提供 reproducible local devstack; optional safe test on `root@114.66.58.243` only when a slice needs public network behavior and after recording reason/start/stop/cleanup commands.
- 范围：`docs/remote-rebuild/RUNBOOK.md` or devstack scripts, `web-control/`, `termx-hub/`, `termx-cli/` integration tests.
- 非目标：不修改 SSH config、iptables、防火墙、systemd 常驻服务；不清空系统目录。
- 外部依赖：公网服务器、DNS/TLS/certificates may be required later.
- mock 策略：local devstack first; external server only for network smoke that cannot be represented locally.
- 先写的失败测试：待创建 devstack smoke checks and runbook validation.
- 预期失败结果：待记录。
- 实现摘要：待完成。
- 重构摘要：待完成。
- 运行命令：待记录。
- 测试结果：待记录。
- subagent review：待发起。
- review 发现：待记录。
- review 后修复：待记录。
- 新增派生条目：暂无。
- deferred human items：production DNS/TLS/cloud account/security approval.
- 剩余风险：external server state must be recorded and cleaned; temporary services go under `/tmp/termx-devstack` or `/opt/termx-devstack`.
- 下一步：After slices 1-10 provide runnable services.
- commit：待提交。

## Historical P2/P3 Records

The entries below predate the current Remote Web / Hub / Agent Buildout. They are retained for traceability but are no longer the active todo tree.

## TDD Log

### P3-D-C shared TerminalList.tsx boundary

- Tests written before implementation: `remote-ui/src/terminalInventory.test.ts` and `remote-ui/src/TerminalList.test.tsx`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/terminalInventory.test.ts src/TerminalList.test.tsx` fails because `./terminalInventory` and `./TerminalList` do not exist yet. The tests cover normalizing a machine-scoped terminal list, rejecting tgent session/window/pane-shaped records, dispatching terminal-open user intent, rendering terminal title/command/size, empty state copy without forbidden public concepts, and public props that only expose machine/terminal semantics.
- Actual failing tests before implementation: focused P3-D-C tests failed as expected with missing module errors for `./terminalInventory` and `./TerminalList`.
- Planned scope: adapt only the useful list interaction shape from tgent `SessionList.tsx`; do not copy createSession/createWindow/splitPane/movePane/killPane or session/window/pane drag/drop semantics. The public list model remains machine -> terminal.
- Review regression tests: added direct coverage that stray `sessions` fields are rejected by terminal inventory and terminals explicitly belonging to another machine are not silently re-scoped to the parent machine. These failed before the review fixes.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test` passed 28 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm audit` passed with 0 vulnerabilities.
- Broader tests after implementation and review fixes: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Meitner` found terminal inventory could silently re-scope terminals from another machine and that the forbidden-session coverage only passed because the sample also contained windows/panes. Fixed with inventory-level session/sessionId rejection and machine ownership validation.
- Result: completed. Commit: `c423ba3`.

### P3-D-D shared FileManager boundary

- Tests written before implementation: `remote-ui/src/fileApi.test.ts`, `remote-ui/src/useFileManager.test.tsx`, `remote-ui/src/FileManager.test.tsx`, and `remote-ui/src/test/mockFileTransport.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/fileApi.test.ts src/useFileManager.test.tsx src/FileManager.test.tsx` fails because `./fileApi`, `./useFileManager`, and `./FileManager` do not exist yet. The tests cover API-channel file list/stat/error behavior, directory navigation through injected transport, visible error state, component rendering, no workspace/tab/window/pane/session copy, and no direct browser/native/WebRTC implementation props.
- Actual failing tests before implementation: focused P3-D-D tests failed as expected with missing module errors for `./fileApi`, `./useFileManager`, and `./FileManager`.
- Planned scope: adapt tgent file manager/client structure behind TermX `PeerTransport.openApi()` / file transfer interfaces without copying relay policy leakage into components. File actions should operate on machine/terminal context and stable reducer/message state where practical.
- Review regression tests: added coverage that a file transport connected to another machine is rejected before `openApi()` and that stale initial directory responses cannot overwrite a later navigation result. These failed before the review fixes with missing machine mismatch errors and stale path overwrite.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test -- --run src/useFileManager.test.tsx src/fileApi.test.ts src/FileManager.test.tsx` passed 12 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after implementation and review fixes: `cd remote-ui && npm test` passed 40 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Sartre` found missing machine identity validation before file API use, stale async directory responses overwriting newer navigation, and stale workflow text. Fixed with pre-request `getConnectionInfo()` validation, per-request sequence guards, regression tests, and workflow cleanup. Follow-up review by `Planck` found the file manager still allowed unscoped terminal use because `terminalId` was optional and missing `ConnectionInfo.terminalId` did not fail.
- Follow-up review regression tests: added coverage requiring `FileManagerProps.terminalId`, and requiring `useFileManager` to reject missing or mismatched transport terminal IDs before `openApi()`. The missing-terminal regression failed before the fix, proving unscoped terminal use was still possible.
- Follow-up review fix: `FileManagerProps` and `UseFileManagerOptions` now require `terminalId`; `useFileManager` rejects missing or mismatched `ConnectionInfo.terminalId` before opening the file API channel; `remote-ui` typecheck now includes `.tsx` files so component contract tests are enforced.
- Final follow-up review: `Mencius` found no remaining code findings after terminal scoping fixes. Low finding: workflow text was stale and still described the terminal scoping fix as in progress. This entry is the workflow cleanup for that finding.
- Result: completed. Commit: `138aba3`.

### P3-E local embedded web terminal/file wiring

- Active slice: P3-E-A browser adapter and app shell contract for local embedded web.
- Tests written before implementation: `remote-ui/src/localAgentApi.test.ts`, `remote-ui/src/localWebRtcTransport.test.ts`, and `remote-ui/src/LocalRemoteApp.test.tsx`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/localAgentApi.test.ts src/localWebRtcTransport.test.ts src/LocalRemoteApp.test.tsx` fails because `./localAgentApi`, `./localWebRtcTransport`, and `./LocalRemoteApp` do not exist yet. The tests cover local status/terminal normalization, local pair and RTC offer payloads without private key/TURN leakage, browser WebRTC data-channel adapter boundaries, terminal identity scoping, and shared `TerminalList`/`Terminal`/`FileManager` composition.
- Actual failing tests before implementation: focused P3-E-A tests failed as expected with missing module resolution errors for the three new modules.
- Planned scope: keep browser WebRTC/fetch details inside adapter modules, expose only the existing `LocalAgentApi` and `PeerTransport` interfaces to components/hooks, require `machineId + terminalId` for terminal and file manager, and preserve native-like message/reducer behavior for visible state.
- Implementation notes: added `localAgentApi` for local status/terminals/pair/RTC offer normalization, `localWebRtcTransport` for injectable browser WebRTC data-channel transport, and `LocalRemoteApp` as the embedded local web app shell that composes shared `TerminalList`, `Terminal`, and `FileManager`. The adapter intentionally does not open the optional `events` channel because current Go local RTC closes unknown labels; API-channel file reads are translated to the current Go `POST` JSON-body contract while keeping UI components behind semantic interfaces.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test -- --run src/localAgentApi.test.ts src/localWebRtcTransport.test.ts src/LocalRemoteApp.test.tsx` passed 10 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after implementation and review fixes: `cd remote-ui && npm test` passed 50 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Rawls` found P1 issues that `LocalRemoteApp` did not call `transport.connect()`, the first implementation generated the WebRTC offer before data channels existed, and the browser terminal channel still does not implement the current Go binary terminal protocol. It also found missing package exports and local RTC client metadata drift. Fixed in this slice: package exports, documented `client.type=browser` / `transport=local`, pre-offer `terminal:{terminal_id}` and `api` channel creation, and `LocalRemoteApp` connect lifecycle with regression tests. Deferred to P3-E-B: browser terminal client compatibility with the current Go binary terminal protocol.
- P3-E-A defers actual bundling into `termx-core/internal/remote/localweb/static` until the adapter/app shell contracts compile and pass unit tests. The follow-up P3-E-B slice should add build/embed wiring and browser smoke.
- Result: completed. Commit: `83ad016`.

### P3-E-B browser terminal binary protocol adapter

- Active slice: implement browser-side terminal transport compatibility with the current Go binary terminal protocol over `terminal:{terminal_id}`.
- Tests written before implementation: `remote-ui/src/termxProtocol.test.ts` and `remote-ui/src/localTerminalProtocolTransport.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/termxProtocol.test.ts src/localTerminalProtocolTransport.test.ts` fails because `./termxProtocol` and `./localTerminalProtocolTransport` do not exist yet. The tests cover Go-compatible binary frame codec, resize payload encoding, hello + attach, TypeOutput events, TypeInput/TypeResize mapping, snapshot text fallback, and terminal identity rejection before writing protocol frames.
- Actual failing tests before implementation: focused P3-E-B tests failed as expected with missing module resolution errors for `./termxProtocol` and `./localTerminalProtocolTransport`.
- Planned scope: keep the public UI model as machine -> terminal, keep browser DataChannel details in adapter modules, and do not introduce workspace/tab/pane/session concepts, TURN relay credentials, or machine private key handling. This slice will add a narrow binary protocol bridge beneath the existing `TerminalTransport` interface: encode/decode TermX frames, perform hello + attach, map TypeOutput to terminal output events, map BinaryChannel input/resize messages to TypeInput/TypeResize, and provide a lightweight snapshot text fallback.
- Implementation notes: added `termxProtocol` for Go-compatible frame encode/decode and resize payloads, plus `localTerminalProtocolTransport` to perform hello + attach over `terminal:{terminal_id}` and map TypeOutput/TypeInput/TypeResize through the existing `TerminalTransport` / `BinaryChannel` boundary. `localWebRtcTransport` now routes terminal channels through this binary protocol bridge instead of raw-output/JSON placeholder behavior. A local self-review caught that request `params` must be JSON objects/raw payloads, not JSON strings, and the implementation/tests were corrected to match `termx-core/protocol.Request`.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/termxProtocol.test.ts src/localTerminalProtocolTransport.test.ts src/localWebRtcTransport.test.ts` passed 10 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after implementation before code review: `cd remote-ui && npm test` passed 57 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Pauli` found that real `TerminalClient` ordering opens the terminal before subscribing, but late subscribers were not forwarded to an already-created protocol bridge, so output/snapshot/closed events would be dropped. It also found close/reopen would reuse a closed cached terminal channel/protocol instead of creating a new `terminal:{terminal_id}` channel and redoing hello/attach.
- Review regression tests: added `localWebRtcTransport` coverage for subscribe-after-open ordering and close/reopen creating a fresh terminal data channel/protocol handshake. The focused review tests failed as expected before the fix: late output events were not delivered to the subscriber, and the reopened terminal reused the first closed data channel.
- Review fix after failing regressions: `localWebRtcTransport` now registers one protocol-bridge dispatcher per terminal and dispatches into the current subscriber set, so subscribers added after `openTerminal()` still receive output/closed/snapshot events. `closeTerminalChannel()` now clears cached terminal protocol/channel state so reopen creates a fresh `terminal:{terminal_id}` channel and repeats hello/attach.
- Focused tests after review fix: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts src/localTerminalProtocolTransport.test.ts src/termxProtocol.test.ts` passed 12 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after review fix: `cd remote-ui && npm test` passed 59 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Follow-up code review: `Aristotle` found a high issue where stream frames arriving before attach response are dropped, a medium issue where raw RTC data-channel close events are not forwarded as terminal closed events, and a low test gap proving data channels are created before `createOffer()`.
- Follow-up review regression tests: added pre-attach stream-frame buffering, data-channel close forwarding, and pre-offer channel creation ordering coverage. Focused tests failed as expected before the fix because early stream output was dropped and raw close did not emit a terminal closed event; the pre-offer channel ordering test passed against the current implementation and now guards the intended behavior.
- Follow-up review fix: `localTerminalProtocolTransport` now buffers stream frames until attach names the stream channel, then flushes matching channel frames; `localWebRtcTransport` now bridges RTC data-channel `close` into a terminal closed event through an adapter-level `onClose` callback without exposing browser primitives to UI components.
- Focused tests after follow-up review fix: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts src/localTerminalProtocolTransport.test.ts src/termxProtocol.test.ts` passed 14 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after follow-up review fix: `cd remote-ui && npm test` passed 61 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Final follow-up code review: `Ptolemy` found real-browser risks where RTC binary messages can arrive as `Blob` unless `binaryType` is set or Blob is decoded, terminal hello can be sent while the data channel is still `connecting`, and raw channel close emits `closed` without clearing cached terminal channel/protocol state for retry.
- Final review regression tests: added coverage for browser Blob binary messages, opening terminal while the RTC data channel is still `connecting`, and retry after raw close creating a fresh terminal channel. Focused tests failed as expected before the fix: `binaryType` remained `blob`, hello was sent before channel open, and retry reused the closed channel.
- Final review fix: local terminal data channels now set `binaryType='arraybuffer'` and still decode Blob messages defensively; the protocol bridge waits for terminal channel `open` before sending hello; raw terminal channel close now clears cached protocol/channel state before forwarding `closed` to subscribers.
- Focused tests after final review fix: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts src/localTerminalProtocolTransport.test.ts src/termxProtocol.test.ts` passed 17 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after final review fix: `cd remote-ui && npm test` passed 64 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Final post-fix review: `Einstein` found a medium issue where raw terminal-channel close before `openTerminal()` leaves the precreated channel cached, plus stale next-action workflow text.
- Final post-fix review regression: added coverage for raw terminal-channel close after `connect()` but before `openTerminal()`. The focused test failed as expected before the fix because the first `openTerminal()` reused the closed pre-created data channel.
- Final post-fix review fix: terminal channels now install raw close cache cleanup immediately when created, before the protocol bridge exists. The protocol bridge still forwards `closed` to subscribers once open, and retries after pre-open close create a fresh `terminal:{terminal_id}` channel.
- Focused tests after final post-fix review fix: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts src/localTerminalProtocolTransport.test.ts src/termxProtocol.test.ts` passed 18 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after final post-fix review fix: `cd remote-ui && npm test` passed 65 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Result: completed. Commit: `4ef0557`.

### P3-E-C-A build/embed local web shell assets

- Active slice: build the shared local web shell from `remote-ui/` and synchronize the Vite output into `termx-core/internal/remote/localweb/static` so the `termx` binary embeds the same shell that mobile will later reuse.
- Tests written before implementation: `remote-ui/src/localWebEntry.test.tsx`, an expanded `termx-core/internal/remote/localweb/handler_test.go` `TestHandlerServesDefaultEmbeddedAssets`, and a `remote-ui/src/LocalRemoteApp.test.tsx` regression for synchronous local transport setup errors rendering as an alert instead of crashing the embedded shell.
- Expected failing tests: `cd remote-ui && npm test -- --run src/localWebEntry.test.tsx` fails because `./localWebEntry` does not exist yet; `cd termx-core && go test ./internal/remote/localweb -run TestHandlerServesDefaultEmbeddedAssets` fails because the embedded static page is still a placeholder without a Vite module asset.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/localWebEntry.test.tsx` failed with `Failed to resolve import "./localWebEntry"`; after adding the first entry draft it failed with `TypeError: localStorage.getItem is not a function`, proving the browser-local adapter needed a safer storage boundary. `cd termx-core && go test ./internal/remote/localweb -run TestHandlerServesDefaultEmbeddedAssets` failed because the placeholder `index.html` lacked a module asset.
- Planned scope: add the smallest Vite build and sync path for local web shell assets, keep browser fetch/WebRTC/signing details behind adapter modules, verify no workspace/tab/pane UI text, no TURN credentials, and no machine private key exposure in this shell. Real browser terminal/file smoke moves to P3-E-C-B after browser-local pairing/signing is implemented.
- Implementation notes: added `remote-ui` Vite entry/build config, `npm run build:localweb`, a Node sync script that copies `dist/` into `termx-core/internal/remote/localweb/static`, a local web shell entry/CSS around `LocalRemoteApp`, and generated embedded assets. `LocalRemoteApp` now catches synchronous transport factory errors so the embedded shell renders the conservative pairing/signing placeholder error instead of crashing. The browser local entry currently refuses to open a terminal until an app certificate and offer signer exist, which is the conservative security path because the browser must never read or synthesize machine private key material.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/localWebEntry.test.tsx` passed; `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx src/localWebEntry.test.tsx` passed 4 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed and synced assets; `cd termx-core && go test ./internal/remote/localweb -run TestHandlerServesDefaultEmbeddedAssets` passed.
- Broader tests after implementation before code review: `cd remote-ui && npm test` passed 67 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Code review: `Feynman` found one low issue: `sync-localweb-assets.mjs` only removed `static/assets`, so stale generated root files could survive when Vite output changes. No high/medium issues; no workspace/tab/pane public model, TURN credential exposure, machine private key exposure, or UI transport-boundary leakage was found.
- Review regression test: added `remote-ui/scripts/sync-localweb-assets.test.mjs`, which failed before the fix because `stale-manifest.webmanifest` remained in the target static root.
- Review fix: `syncLocalWebAssets` now replaces the whole embedded static directory before copying `dist/`, and the script exposes a tested function while still working as the `npm run build:localweb` entrypoint.
- Final focused tests after review fix: `cd remote-ui && npm test -- --run scripts/sync-localweb-assets.test.mjs src/LocalRemoteApp.test.tsx src/localWebEntry.test.tsx` passed 5 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/localweb -run TestHandlerServesDefaultEmbeddedAssets` passed.
- Final broader tests after review fix: `cd remote-ui && npm test` passed 68 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Result: completed. Commit: `f68bf9d`.

### P3-E-C-B-A browser-local app identity and offer signing

- Active slice: browser-local app keypair, certificate storage, and canonical local WebRTC offer signing primitives.
- Tests written before implementation: `remote-ui/src/localAppIdentity.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/localAppIdentity.test.ts` fails because `./localAppIdentity` does not exist yet.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/localAppIdentity.test.ts` failed with missing module resolution for `./localAppIdentity`.
- Planned scope: add pure `remote-ui` logic that uses an injected WebCrypto-like Ed25519 boundary and storage interface to create/load an app identity, persist only app metadata and the app certificate in string storage, keep the app private key behind a non-exportable IndexedDB/WebCrypto key-store boundary, canonicalize/sign local WebRTC offers in the same format as `termx-core/internal/remote/rtc.CanonicalOfferSignatureMessage`, and prove no machine private key or TURN credentials are stored or returned. UI/harness and real browser smoke remain for P3-E-C-B-B.
- Implementation notes: added `localAppIdentity` with storage adapter, injectable app crypto boundary, browser WebCrypto adapter, app identity creation, pair claim helper, canonical local offer message, and offer signer. `localWebEntry` now uses stored app certificate plus browser-local signer instead of the placeholder throw. Added Go golden coverage so daemon canonical message and remote-ui canonical message stay aligned. After review, app private key storage was moved out of localStorage: browser private keys are generated as non-exportable WebCrypto keys and stored behind an IndexedDB key-store boundary; localStorage-style storage now holds only app metadata and the app certificate.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/localAppIdentity.test.ts src/localWebEntry.test.tsx src/localWebRtcTransport.test.ts` passed 14 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/rtc -run 'TestCanonicalOfferSignatureMessageMatchesRemoteUIContract|TestOfferSignature'` passed.
- Broader tests after implementation before code review: `cd remote-ui && npm test` passed 71 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Code review: `Heisenberg` found a medium issue where the browser app private key was exportable and stored as JWK in localStorage, conflicting with the documented WebCrypto non-exportable key plus IndexedDB boundary. No workspace/tab/pane public model, TURN credential exposure, machine private key exposure, or canonical signing mismatch was found.
- Review regression tests: updated `remote-ui/src/localAppIdentity.test.ts` to fail when `termx.local.appPrivateKey` or any private-key-shaped field is stored in string storage, and added a real WebCrypto check proving generated private keys are non-exportable.
- Review fix: `LocalAppCrypto` now saves/loads app private keys via an injected key-store boundary; the browser implementation uses IndexedDB object store `app-private-keys` and `crypto.subtle.generateKey({ name: 'Ed25519' }, false, ...)`, while public key raw export remains available for pairing.
- Final focused tests after review fix: `cd remote-ui && npm test -- --run src/localAppIdentity.test.ts src/localWebEntry.test.tsx src/localWebRtcTransport.test.ts` passed 16 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/rtc -run 'TestCanonicalOfferSignatureMessageMatchesRemoteUIContract|TestOfferSignature'` passed.
- Final broader tests after review fix: `cd remote-ui && npm test` passed 73 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Result: completed. Commit: `7185c8d`.

### P3-E-C-B-B browser smoke

- Active slice: local pair UI/harness and embedded browser smoke.
- Tests written before implementation: `remote-ui/src/LocalPairPanel.test.tsx`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/LocalPairPanel.test.tsx` fails because `./LocalPairPanel` does not exist yet.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/LocalPairPanel.test.tsx` failed with missing module resolution for `./LocalPairPanel`.
- Planned scope: serve the embedded local web through `termx`/localweb, complete local pair claim UI/harness using the B-A primitives, then smoke terminal list, terminal open, and file manager against local WebRTC-over-TCP. If real user pairing UI still needs product input, use a narrow local test harness and record the remaining UX decision here.
- Implementation notes: added `LocalPairPanel` as a narrow pair harness that accepts pair id/secret, calls `pairLocalApp`, stores the app certificate through the B-A primitives, and avoids old workspace/tab/pane/session UI text by using `Pair ID` rather than pair session wording. `LocalRemoteApp` now accepts optional pair configuration through interfaces and renders the panel without importing concrete browser transport details. `localWebEntry` supplies browser storage/crypto/pair API wiring for the embedded shell.
- Browser smoke result: Browser Use plugin files are present, but this Codex session does not expose the required Node REPL `js` tool, so in-app click automation could not be run. As an executable local smoke fallback, `termx-cli` `TestStartRemoteLocalWebServesEmbeddedPageAndStatus` now starts local web with ICE TCP, fetches the embedded HTML, fetches the embedded JS module, verifies the shared shell and pair harness markers (`termx-local-web-shell`, `termx-local-pair-panel`, `Pair ID`, `Pair secret`), checks local status/terminals/pair APIs, and verifies the asset/status do not expose machine private key strings or TURN URLs.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/LocalPairPanel.test.tsx src/LocalRemoteApp.test.tsx src/localWebEntry.test.tsx` passed 7 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-cli && go test ./cmd/termx -run TestStartRemoteLocalWebServesEmbeddedPageAndStatus -count=1` passed after smoke assertion tuning.
- Broader tests after implementation before code review: `cd remote-ui && npm test` passed 75 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Self-review hardening before final review: added `localWebEntry` coverage that the shell still mounts when browser pair crypto is unavailable, then guarded pair option creation so unsupported WebCrypto/IndexedDB conditions do not crash the local shell. Pairing is omitted in that unsupported environment; terminal transport still requires a real app certificate before opening.
- Code review: `Euler` found a high first-run issue where an embedded page with terminals but no app certificate auto-selected the first terminal, hit the missing-certificate transport error, and rendered only a fatal alert before the pair panel, so the user could not pair. The review found no workspace/tab/pane public model leakage, TURN credential exposure, machine private key exposure, or transport boundary issue.
- Review regression test: added `LocalRemoteApp` coverage that first-run missing-certificate errors keep `termx-local-pair-panel` reachable and that a successful pair stores the certificate and retries transport creation. This failed as expected before the fix because the app rendered only the fatal alert.
- Review fix: `LocalRemoteApp` now renders post-machine-load errors inside the machine shell instead of replacing the shell, and `onPaired` clears the error and bumps a transport retry token so the terminal/file wiring can reconnect after the certificate exists.
- Focused tests after review fix: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` passed 5 tests; `cd remote-ui && npm test -- --run src/LocalPairPanel.test.tsx src/LocalRemoteApp.test.tsx src/localWebEntry.test.tsx` passed 9 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed.
- Broader tests after review fix: `cd remote-ui && npm test` passed 77 tests; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Follow-up code review: `Darwin` found no findings after the first-run pairing fix. It confirmed the certificate error stays non-fatal after machine/terminal load, the pair panel remains visible, pair success clears the error and retries transport creation, tests cover the regression, and the scoped files did not introduce workspace/tab/pane/session UI wording, TURN credentials, machine private key exposure, or app private key boundary drift.
- Result: completed. Commit: `be45093`.

### P3-E-C-B-D local WebRTC DataChannel open gating

- Active slice: fix the manual embedded-web local test failure `Failed to execute 'send' on 'RTCDataChannel': RTCDataChannel.readyState is not 'open'`.
- Tests written before implementation: `remote-ui/src/localWebRtcTransport.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` should fail because the `api` request path sends immediately on a connecting DataChannel and `openFileTransfer()` resolves before the `file:{transfer_id}` DataChannel is open.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` failed as expected. The API test timed out waiting for the request frame after opening the channel because the old request had already failed while `connecting`; the file transfer test observed `openFileTransfer()` resolving before the RTC channel opened.
- Planned scope: keep the fix inside the browser-local transport adapter, preserving the `PeerTransport`/`JsonRpcChannel`/`BinaryChannel` interfaces for UI/business components. The change must not introduce workspace/tab/pane public concepts, TURN relay credentials, or any machine private key exposure.
- Implementation notes: `LocalApiChannel` now registers request waiters immediately but waits for the RTC `api` DataChannel to open before sending, so early responses are not dropped and pre-open sends are avoided. `openFileTransfer()` now waits for `file:{transfer_id}` to open before returning a `BinaryChannel` to callers.
- Focused tests after implementation before code review: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` passed 12 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm test` passed 79 tests; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed.
- Code review: `Hilbert` found no issues. It confirmed the fix stays inside the browser-local WebRTC adapter, preserves `PeerTransport` / `JsonRpcChannel` / `BinaryChannel` boundaries, keeps RTC primitives out of UI/business components, and does not introduce workspace/tab/pane/session concepts, TURN relay credentials, or machine private key exposure. Residual risk: no automated in-app browser click smoke was available in this Codex session.
- Final focused tests after review: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` passed 12 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm test` passed 79 tests; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Result: completed. Commit: `a1edab3`.

### P3-E-C-B-E local file manager loading hang

- Active slice: fix manual embedded-web local test feedback that the file manager remains on `Loading files` instead of rendering real files.
- Tests written before implementation: `remote-ui/src/localWebRtcTransport.test.ts` and, if needed, `remote-ui/src/useFileManager.test.tsx`.
- Expected failing tests: focused tests should fail because pending `LocalApiChannel` requests are not rejected when the `api` RTC DataChannel closes before a chunked response is delivered, allowing `useFileManager` to stay loading indefinitely.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` timed out in the new pending API request close test, proving the request promise stayed unresolved after `api` DataChannel close.
- Planned scope: keep the fix inside the browser-local transport / file hook boundary. The UI should either render files from a valid response or surface a recoverable error; it must not hang forever. The change must not introduce workspace/tab/pane/session public concepts, TURN relay credentials, or machine private key exposure.
- Code review before completion: `Fermat` found that malformed `0xc0` API chunks with truncated or impossible headers, non-final chunks that never complete, and async Blob/FileReader conversion failures could still leave file requests pending and keep `Loading files` visible. Follow-up scope: add malformed-header, conversion-failure, open-timeout, and response-timeout regressions before completing this todo.
- Follow-up failing tests before implementation: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` failed as expected in four added regressions: malformed `0xc0` API chunk headers, async response conversion failure, `api` DataChannel never opening, and non-final API response never completing all left requests pending.
- Implementation notes: `LocalApiChannel` now rejects pending requests on close/error, invalid or malformed API chunks, async message conversion failures, open timeout, and response timeout. Response timeout starts only after a request is actually sent, so a never-open channel reports an open timeout instead of an unrelated response timeout. This keeps `useFileManager` from staying on `Loading files` forever when the local API channel fails.
- Follow-up code review: `Pascal` found a P1 pre-commit asset tracking issue where `index.html` referenced a generated JS asset that had not yet been added to git, plus a P3 timer cleanup issue where some waiter removal paths left response timers alive until they fired. The asset issue is resolved by staging the regenerated static directory with the commit; the timer issue is fixed by routing waiter resolve/reject through helpers that delete the waiter and clear the stored timer.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` passed 18 tests; `cd remote-ui && npm test` passed 85 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2E_WebRTCFileAPIAndTransfer|TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer' -count=1` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Result: completed. Commit: `80bbeb1`.

### P3-E-C-B-F local API DataChannel readiness during connect

- Active slice: fix manual embedded-web local test feedback `timed out opening data channel api`.
- Tests written before implementation: `remote-ui/src/localWebRtcTransport.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` should fail because `connect()` resolves after setting the remote description even if the pre-created `api` DataChannel is still `connecting`, allowing `LocalRemoteApp` to mount `FileManager` on a half-ready transport.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` failed as expected because `connect()` resolved while the mock `api` DataChannel was still `connecting`.
- Planned scope: keep readiness handling inside `localWebRtcTransport`. The local app should only expose `connectedTransport` after required pre-offer channels are ready, or fail the transport connect path so the shell shows an actionable error/pair panel. Do not introduce workspace/tab/pane/session public concepts, TURN relay credentials, or machine private key exposure.
- Implementation notes: `LocalWebRtcPeerTransport.connect()` now waits for the required pre-created `api` DataChannel to open, with a bounded timeout, after applying the local RTC answer. This prevents `LocalRemoteApp` from mounting `FileManager` on a transport where terminal connection setup might proceed but file API is not yet usable. If the daemon rejects/closes `api`, connect fails and the shell can surface the transport error instead of letting `FileManager` independently time out.
- Focused tests after implementation before code review: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` passed 20 tests; `cd remote-ui && npm test` passed 87 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2E_WebRTCFileAPIAndTransfer|TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer' -count=1` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Code review before completion: `Carson` found that connect-time `api` readiness failures left the partially negotiated PeerConnection/channels alive and that `waitChannelOpen` did not fail promptly on DataChannel `error`. Follow-up scope: add regressions for cleanup after connect readiness failure and for `api` DataChannel `error` during connect, then close/reset transport state before rethrowing.
- Review failing tests before fix: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` failed as expected because a connect-time API readiness timeout left the mock PeerConnection open, and an `api` DataChannel `error` before opening did not reject connect.
- Review fix: `connect()` now wraps setup in a failure cleanup path that calls `disconnect()` before rethrowing, and `waitChannelOpen()` now rejects on DataChannel `error` as well as close/timeout. Regression tests prove connect-time API readiness timeout cleans up the PeerConnection and that API DataChannel `error` rejects connect.
- Final focused tests after review fixes: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` passed 22 tests; `cd remote-ui && npm test` passed 89 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2E_WebRTCFileAPIAndTransfer|TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer' -count=1` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Result: completed. Commit: `9a51792`.

### P3-E-C-B-G browser local ICE gathering before offer

- Active slice: fix manual embedded-web local test feedback that the browser still reports `timed out opening data channel api` after the connect-time API readiness gate.
- Tests written before implementation: `remote-ui/src/localWebRtcTransport.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` should fail because the current browser local WebRTC transport signs and sends the offer immediately after `setLocalDescription()`, before browser ICE gathering has completed. The local daemon path is non-trickle, so an offer without local host/TCP candidates can produce an answer but never open the `api` DataChannel.
- Planned scope: keep the fix inside `remote-ui/src/localWebRtcTransport.ts` by waiting for `iceGatheringState === "complete"` or the `icegatheringstatechange` event before reading `pc.localDescription.sdp`, signing the offer, and calling `/api/local/rtc/offer`. Do not expose browser primitives to UI/business components, and do not introduce workspace/tab/pane public concepts, TURN credentials, or machine private key handling.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` failed as expected because `signOffer` saw `offer-sdp` before mock ICE gathering completed and before `localDescription.sdp` changed to the gathered-candidate SDP.
- Follow-up server failing test before implementation: `cd termx-core && go test ./internal/remote/rtc -run TestAnswerOfferSurvivesCanceledRequestContext -count=1` failed because the `api` DataChannel never opened after the simulated HTTP request context was canceled. This matched the real browser symptom: `/api/local/rtc/offer` returned an answer, but the daemon-side PeerConnection was tied to the short-lived request context and closed before ICE/SCTP could finish.
- Implementation notes: browser local WebRTC now waits for ICE gathering completion before signing and sending the non-trickle local offer, and rejects/cleans up on ICE gathering timeout. Server-side `AnswerOfferWithOptions` now separates offer/answer setup from WebRTC session lifetime by accepting an optional session context; the local web path binds sessions to the daemon/server lifecycle instead of the HTTP request context, so the PeerConnection survives after `/api/local/rtc/offer` returns and is still closed on server shutdown or peer connection failure.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts` passed 24 tests; `cd remote-ui && npm run typecheck` passed; `cd termx-core && go test ./internal/remote/rtc -run 'TestAnswerOffer(SurvivesCanceledRequestContext|DefaultSessionContextFollowsCallerContext|SessionContextClosesDataChannel|ChannelPolicyRejectsWrongTerminalChannel)' -count=1` passed; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed.
- Broader tests after implementation: `cd remote-ui && npm test` passed 91 tests; `cd remote-ui && npm run build:localweb` passed and regenerated `termx-core/internal/remote/localweb/static/assets/index-hkl3rMAj.js`; `cd termx-core && go test . -run 'TestE2E_WebRTCFileAPIAndTransfer|TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer' -count=1` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Real browser smoke: built `/tmp/termx-local-test`, restarted one local daemon at `http://127.0.0.1:18888` with ICE TCP `127.0.0.1:18889`, created terminal `1`, launched Chrome headless over CDP, filled the pair panel, completed local pairing, opened browser WebRTC, loaded `assets/index-hkl3rMAj.js`, opened the `api` DataChannel, and verified the `FileManager` rendered real root directory entries instead of `Loading files` or `timed out opening data channel api`.
- Cleanup regressions: added `TestAnswerOfferSessionContextClosesDataChannel` to prove the server/daemon session context still closes the PeerConnection and browser data channel after a session cancellation, and `TestAnswerOfferDefaultSessionContextFollowsCallerContext` to prove non-local/default callers remain tied to their owning context.
- Code review: `Wegener` found no findings for the ICE-gathering browser adapter change. It confirmed no workspace/tab/pane public concepts, TURN relay credentials, machine private key handling, or transport-boundary leakage in the scoped changes; residual note was to include the generated asset replacement in the final commit. Follow-up review by `Descartes` found a high issue: default `AnswerOffer` had been accidentally detached from the caller context, so non-local remote sessions could outlive their owning runtime. Fixed by making `SessionContext` optional and defaulting to the caller context, while only local web passes the daemon session context; added `DefaultSessionContextFollowsCallerContext` to guard the default cleanup behavior.
- Result: completed. Commit: `9531237`.

### P3-E-C-B-H xterm.js interactive terminal surface

- Active slice: move embedded local web terminal UI from readonly `<pre>` output to a tgent-aligned xterm.js surface after the local WebRTC protocol path has been verified.
- Tests written before implementation: `remote-ui/src/Terminal.test.tsx`.
- Expected failing tests: `cd remote-ui && npm test -- --run src/Terminal.test.tsx` should fail because the current `Terminal.tsx` renders plain text instead of constructing an xterm.js terminal, does not wire xterm `onData` into TermX terminal input, and does not fit/send terminal resize through the existing `TerminalTransport` interface.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/Terminal.test.tsx` failed as expected: xterm instance count stayed at 0, streaming output only appeared in the old `<pre>`, xterm input could not be emitted, and no resize was sent through the mock terminal channel.
- Planned scope: use `../tgent/tgent-app/src/components/Terminal.tsx` as the xterm usage reference for `XTerm`, `FitAddon`, `term.write`, `term.onData`, and fit/resize behavior, but keep the TermX public identity as `machineId + terminalId` only. Browser/WebRTC details must stay inside transport adapters, not UI components. This slice deliberately avoids importing tgent pane/session/window concepts, TermX TURN credentials, or any machine private key behavior.
- User correction before completion: frontend styling must use TailwindCSS as the default styling system instead of growing handwritten CSS. Root `AGENTS.md` now records this rule. P3-E-C-B-H must first wire Tailwind into `remote-ui` before adding new UI styling; `@xterm/xterm/css/xterm.css` remains allowed as a third-party library CSS import.
- Implementation notes: added TailwindCSS/PostCSS config to `remote-ui`, replaced `localWebEntry.css` handwritten layout with Tailwind directives, moved local shell/list/file/pair layout styling to JSX utility classes, added `@xterm/xterm` and `@xterm/addon-fit`, and replaced the terminal `<pre>` with an xterm surface that writes accumulated terminal text, forwards `onData` through `TerminalTransport.sendInput`, fits via `FitAddon`, and sends deduplicated resize messages only after the terminal channel is open.
- Review regression tests: added `Terminal.test.tsx` coverage for xterm construction, output writes, input forwarding, open-channel resize sending, and delayed resize when `FitAddon.proposeDimensions()` is initially unavailable. Non-terminal app-shell tests mock `Terminal` so jsdom does not instantiate browser-only xterm internals.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/Terminal.test.tsx` passed 6 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after implementation: `cd remote-ui && npm test` passed 94 tests; `cd remote-ui && npm run build:localweb` passed and regenerated `termx-core/internal/remote/localweb/static/assets/index-D5d9blrT.js` plus `index-D4ZdPfWr.css`; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Real browser smoke: rebuilt `/tmp/termx-local-test`, restarted the local daemon at `http://127.0.0.1:18888` with ICE TCP `127.0.0.1:18889`, created terminal `1`, generated fresh pair sessions, drove Chrome headless over CDP to pair the page, opened browser WebRTC, verified `/assets/index-D5d9blrT.js` loaded, verified `.xterm-screen` exists, verified the file manager rendered real root entries instead of `Loading files`, focused the xterm helper textarea, sent `printf 'termx_xterm_smoke_1777646413677\n'`, and observed that text in the terminal surface. The smoke also confirmed the page body did not contain `workspace`, `tab`, `window`, or `pane`.
- Code review: `Faraday` found one medium issue that the regenerated embedded assets were referenced by `index.html` but still untracked. Fixed by staging the new `index-D5d9blrT.js` / `index-D4ZdPfWr.css` assets and deleting the old generated files in the implementation commit. Faraday found no workspace/tab/pane/session public model drift, TURN credential exposure, machine private key handling, or UI/business WebRTC primitive leakage. Residual note: input before channel readiness is still covered by existing `TerminalClient` input-dropped behavior, not by a component-level xterm test.
- Result: completed. Commit: `db9c7065`.

### P3-E-C-B-I mobile terminal interaction shell

- Active slice: refactor the embedded local web mobile interaction shell after user feedback that the current mobile page behaves like a compressed desktop web page instead of a native terminal app. The implementation should use `../tgent/tgent-app/src/pages/TerminalPage.tsx`, `components/Terminal.tsx`, `components/VirtualKeybar.tsx`, `hooks/useTerminalKeyboard.ts`, and `hooks/useTerminalInput.ts` as interaction references while keeping TermX's public model as `machine -> terminal`.
- Tests written before implementation: `remote-ui/src/mobileTerminalInput.test.ts`, updated `remote-ui/src/Terminal.test.tsx`, and updated `remote-ui/src/LocalRemoteApp.test.tsx`.
- Expected failing tests: focused `remote-ui` tests should prove the current shell has no stable terminal-first mobile switcher, no mobile virtual keybar, no keyboard-aware terminal handle, and no mobile-safe pair sheet behavior. These failures are expected before implementation.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/mobileTerminalInput.test.ts src/Terminal.test.tsx src/LocalRemoteApp.test.tsx` failed as expected because `mobileTerminalInput` did not exist, `TerminalHandle` only exposed `sendInput/sendResize/reattach`, the shell still rendered `Config`, no `Pair device` action existed, and the first-run certificate error did not open a `termx-pair-sheet`.
- Planned scope: keep WebRTC/fetch/native details inside transport adapters; do not introduce workspace/tab/pane/session public concepts; do not issue TURN relay credentials; do not expose machine private key material. Use Tailwind utility classes for new UI styling and only narrow CSS for xterm internal DOM overrides.
- Implementation notes: refactored `LocalRemoteApp` into a terminal-first shell with persistent xterm surface, mobile terminal switcher sheet, pair sheet, bottom navigation, and file panel overlay. Added `MobileTerminalKeybar` plus pure `mobileTerminalInput` helpers for Ctrl/Alt one-shot/locked behavior, connected keybar modifier state into `Terminal` so system keyboard input can send control characters, and extended `TerminalHandle` with focus/blur/fit/paste/cursor/input-position methods without exposing tgent pane/session concepts. Removed the old embedded-entry grid class that overrode the full-screen shell and kept new styling in Tailwind utilities, with only narrow xterm DOM CSS overrides.
- Review fixes before completion: fixed the prior review findings by removing the old `grid gap-4 p-4 md:grid-cols[...]` entry class, adding a local entry regression for terminal-first flex layout, wiring keybar modifier state through the shell into `Terminal`, consuming `--termx-keyboard-bottom` in xterm helper textarea/composition CSS, removing the generic `.hide-scrollbar` utility, adding `visualViewport` keyboard-offset handling, and stabilizing the cursor callback path so modifier-state changes do not recreate the xterm instance.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test -- --run src/localWebEntry.test.tsx src/LocalRemoteApp.test.tsx src/Terminal.test.tsx src/mobileTerminalInput.test.ts` passed 20 tests; `cd remote-ui && npm run typecheck` passed.
- Code review: `Gibbs` found two issues after the first pass: `Terminal` could recreate/dispose xterm when callback props changed, which could blank the terminal until new output arrived, and `FileManager` depended on callers passing `relative` for its absolute scrolling body. Added failing regressions, then fixed `Terminal` by storing `onCursorMove`/`onBufferChange` in refs outside the xterm construction effect and fixed `FileManager` by making its root `relative min-h-0`. No workspace/tab/pane/session public model drift, anonymous/free TURN credentials, machine private key exposure, or UI/business transport leakage was found. Residual note: ensure generated localweb assets are included in the commit.
- Final focused tests after code review fixes: `cd remote-ui && npm test -- --run src/Terminal.test.tsx src/FileManager.test.tsx` passed 12 tests; `cd remote-ui && npm test -- --run src/localWebEntry.test.tsx src/LocalRemoteApp.test.tsx src/Terminal.test.tsx src/mobileTerminalInput.test.ts` passed 20 tests; `cd remote-ui && npm run typecheck` passed.
- Final broader tests after implementation: `cd remote-ui && npm test` passed 101 tests; `cd remote-ui && npm run build:localweb` passed and regenerated `termx-core/internal/remote/localweb/static/assets/index-D4TtbuV4.js` plus `index-DLJaNUDj.css`; `cd remote-ui && npm audit` passed with 0 vulnerabilities; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Real local smoke: rebuilt `/tmp/termx-local-test`, restarted tmux session `termx-local-test` with local web `127.0.0.1:18888` and ICE TCP `127.0.0.1:18889`, confirmed `GET /` references `/assets/index-D4TtbuV4.js` and `/assets/index-DLJaNUDj.css`, and confirmed `/api/local/status` reports machine `device-b9702aff8b30c634` with ICE TCP enabled. The daemon remains running for manual inspection.
- Result: completed. Commit: `be748f93`.

### P3-E-C-B-J termx remote local management commands

- Active slice: complete the `termx remote` CLI surface for the local-only capabilities already implemented in P3: dynamic local web serving, local WebRTC over ICE TCP, local pairing, and status/info display. The CLI must make the currently usable product path discoverable without requiring users to memorize daemon environment variables.
- Tests written before implementation: `termx-core/protocol/client_test.go`, `termx-core/remote_localweb_test.go`, and `termx-cli/cmd/termx/main_test.go`.
- Expected failing tests: focused Go tests prove there is no protocol method for dynamic local web status/enable/disable yet and no `termx remote enable/local-only/disable/info/show/pair/open` commands under the `remote` command.
- Actual failing tests before implementation: focused protocol/core/CLI tests failed as expected with missing `RemoteLocalEnableParams`, `RemoteLocalStatus`, `Client.RemoteLocalEnable/Status/Disable`, `Server.RemoteLocalEnable/Status/Disable`, and missing `remote` subcommands.
- Planned scope: add runtime local web/ICE TCP management behind protocol interfaces; keep `termx pair` working; add `termx remote pair` as the discoverable paired command; keep anonymous/free local flow from exposing TermX TURN credentials; do not implement managed remote account login, billing, DNS, public relay, or TURN subscription behavior in this slice.
- Implementation notes: added `remote.local.enable`, `remote.local.status`, and `remote.local.disable` protocol/client methods; moved dynamic local web + ICE TCP lifecycle into `termx-core` as `Server.RemoteLocalEnable/Status/Disable`; wired `daemon` env startup through the same runtime; expanded `termx remote` with `status --json`, `info/show`, `enable --local-only`, `local-only`/`local_only`, `disable`, `pair`, and `open`. `remote enable` without `--local-only` returns an explicit managed-remote deferral and never issues TURN credentials.
- Review fixes: `Leibniz` found that `remote disable` only closed listeners and left existing local RTC sessions alive, and that failed reconfiguration tore down the old working runtime first. Added regressions, then bound local RTC sessions to the local runtime context so disable cancels active data channels, and changed enable to start the replacement runtime before swapping so failed binds preserve the existing runtime. The stale workflow finding is resolved by this entry. `Helmholtz` was launched for a final scoped review after fixes but timed out before completion; the completed `Leibniz` review satisfied the required development review and its findings were fixed.
- Focused tests after implementation and review fixes: `cd termx-core && go test ./protocol -run 'TestClientRemoteLocalManagement|TestClientRemotePairStart'` passed; `cd termx-core && go test . -run 'TestRemoteLocalEnableFailureKeepsExistingRuntime|TestE2ERemoteLocalDisableClosesActiveRTCSessions|TestE2ERemoteLocalEnableStatusAndDisable' -count=1` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemote(InfoShowEmitsJSON|DisableEmitsJSONLocalStatus|OpenPrintsOrLaunchesRunningLocalURL|OpenRequiresEnabledLocalRuntime|EnableManagedPathIsExplicitlyDeferred|LocalOnlyAliasEnablesRuntime|EnableLocalOnlyEmitsLocalStatus|PairUsesRunningLocalPairURL|StatusIncludesLocalRuntime)' -count=1` passed.
- Broader tests after implementation and review fixes: `cd termx-cli && go test ./cmd/termx` passed; `cd termx-cli && go test ./...` passed; `cd termx-core && go test ./protocol ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test ./internal/remote/... ./protocol` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Real command smoke: with a temporary socket/log/config/state directory, `go run ./termx-cli/cmd/termx remote enable --local-only --addr 127.0.0.1:0 --ice-tcp-addr 127.0.0.1:0 --json`, `remote status`, `remote info --json`, `remote pair --ttl 1m --json`, `remote open --print`, and `remote disable --json` all passed. Output included dynamic local web URL, ICE TCP port, pair session id/secret, open URL, and disabled local status. Temporary smoke daemons were stopped after the run.
- Deferrals: managed remote enablement, public control-plane login/claim, DNS/TLS, billing/subscription, and TermX TURN relay authorization remain deferred by policy. Commands that would imply managed relay return explicit not-yet-implemented guidance instead of silently issuing relay credentials.
- Result: completed. Commit: `dd5e1502`.

### P3-E-C-B-K terminal resize ownership and size-lock policy

- Active slice: prevent local/mobile remote terminal views from automatically changing the daemon PTY size when the browser/mobile viewport differs from the real terminal size, while preserving explicit resize ownership and existing size-lock semantics.
- Tests written before implementation:
  - `termx-core` protocol/server regressions proving attach responses expose resize control, follower attaches cannot send `TypeResize`, owner collaborator attaches can still resize, observers cannot resize, and size-locked terminals report/deny resize.
  - `remote-ui` terminal/protocol regressions proving xterm can fit locally without sending remote resize when resize control is follower/locked, imperative `fit()` does not steal PTY size, and the local WebRTC terminal protocol requests follower resize policy for embedded/mobile views.
- Expected failing tests: focused Go/TypeScript tests should fail before implementation because resize control metadata and attach resize policy do not exist yet, and `Terminal.tsx` currently sends resize whenever xterm fits after the channel opens.
- Actual failing tests before implementation:
  - `cd termx-core && go test . -run 'TestAttachResizeControlPolicyAndSizeLock|TestResizeRequestRequiresResizeOwnerAttachment' -count=1` failed to build because `protocol.ResizeControl`, resize policy constants, and attachment `canResize` do not exist yet.
  - `cd remote-ui && npm test -- --run src/Terminal.test.tsx src/localTerminalProtocolTransport.test.ts` failed because the terminal protocol bridge omits `resize_policy`, does not emit resize control, does not suppress follower resize frames, and `Terminal.tsx` has no resize-control state.
- Planned scope: add an attach-level `resize_policy`/`resize_control` contract behind existing protocol/transport interfaces; keep UI public identity as `machine -> terminal`; do not expose workspace/tab/pane; do not add TURN credentials or machine private key handling.
- Current assumption: embedded local web/mobile remote views should default to follower resize policy so they render to their own viewport but do not resize the daemon PTY unless a later owner-control action explicitly requests owner. Legacy collaborator attach without an explicit resize policy remains owner-capable for compatibility.
- Implementation notes: added protocol `resize_policy` / `resize_control` fields, `AttachWithOptions`, server-side resize control calculation, raw `TypeResize` gating, and request-path resize owner enforcement. Embedded local web now requests follower resize policy by default, suppresses follower resize frames in the protocol adapter, and `Terminal.tsx` always fits xterm locally while only sending remote resize when resize control says owner. The existing mobile viewport/keybar touch changes were folded into this todo because regenerated embedded assets must match source and the changes support mobile viewport/key behavior around local fit.
- Review regression: `Maxwell` found a high issue where unscoped request-path `resize` still succeeded with no owner attachment. Added `TestScopedResizeRequestRequiresAttachmentOwner` and changed `TestHandleRequestGetResizeSetTagsMetadataAndSnapshot` so request-path resize requires an owner attachment; the previously unowned resize path failed before the fix and now returns 403.
- Focused tests after implementation and review fix: `cd termx-core && go test . -run 'TestHandleRequestGetResizeSetTagsMetadataAndSnapshot|TestScopedResizeRequestRequiresAttachmentOwner|TestResizeRequestRequiresResizeOwnerAttachment|TestFollowerCollaboratorAttachCanInputButCannotResize' -count=1` passed; `cd remote-ui && npm test -- --run src/localTerminalProtocolTransport.test.ts src/Terminal.test.tsx src/localWebRtcTransport.test.ts src/terminalClient.test.ts` passed 46 tests.
- Broader tests after implementation and review fix: `cd remote-ui && npm test` passed 105 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed and regenerated embedded assets; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Maxwell` confirmed no workspace/tab/pane remote public model drift, no anonymous/free/local TURN credential issuance, no app access to machine private key material, and no WebRTC transport details leaking into UI/business components. Its high finding on unscoped request-path resize bypass was fixed with owner-attachment enforcement and regression coverage.
- Result: completed. Commit: `23539e7d`.

### P3-E-C-B-L xterm viewport height regression

- Active slice: fix user-reported embedded local xterm behavior where the terminal only shows one visible line even though the local WebRTC/protocol path is connected.
- Tests written before implementation: `remote-ui/src/Terminal.test.tsx` regressions proving the terminal root/container expose full-height/min-height layout classes and xterm performs a delayed post-open fit after the initial layout reports one row.
- Expected failing tests: `cd remote-ui && npm test -- --run src/Terminal.test.tsx` should fail before implementation because `Terminal.tsx` does not force full-height/min-height containment and only performs immediate fit calls, so an early one-row measurement can persist until a resize observer fires.
- Planned scope: keep the fix inside shared `Terminal.tsx` and its tests; do not change terminal protocol, resize ownership policy, WebRTC transport boundaries, machine private key handling, TURN credentials, or public model names. Existing uncommitted `remote-ui/src/LocalRemoteApp.tsx` mobile header/navigation changes are treated as external work and will not be overwritten by this todo.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/Terminal.test.tsx` failed as expected because the terminal root class lacked `h-full`/`min-h-0` and the simulated early one-row xterm measurement persisted at `rows = 1`.
- Implementation notes: `Terminal.tsx` now makes the terminal root/container full-height, min-height-zero, and overflow-hidden with Tailwind utilities; xterm fit is scheduled again after mount/open via a double `requestAnimationFrame`, and visual viewport delayed fit timers are cleaned up on unmount. Resize sending remains gated by `resizeControl.canResize`, so follower local/mobile views can fit visually without resizing the daemon PTY.
- Generated assets: embedded localweb static assets were regenerated from a temporary clean worktree containing only the `Terminal.tsx`/`Terminal.test.tsx` changes, so the existing uncommitted `remote-ui/src/LocalRemoteApp.tsx` mobile header/navigation change was not included in this todo's bundle. The JS asset changed from `index-CU4ZmuiR.js` to `index-KYAdvw6r.js`; CSS stayed on `index-CHUlPGQj.css`.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/Terminal.test.tsx` passed 12 tests; `cd remote-ui && npm test -- --run src/Terminal.test.tsx src/LocalRemoteApp.test.tsx` passed 18 tests; clean-worktree `cd remote-ui && npm test -- --run src/Terminal.test.tsx` passed.
- Broader tests after implementation: clean-worktree `cd remote-ui && npm test` passed 106 tests; clean-worktree `cd remote-ui && npm run typecheck` passed; clean-worktree `cd remote-ui && npm run build:localweb` passed with only the existing Vite large chunk warning; clean-worktree `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; clean-worktree `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Code review: `Peirce` found no issue with the core xterm delayed-fit fix, resize ownership gating, transport boundaries, TURN credential handling, machine private key handling, or workspace/tab/pane public-model drift. It found a medium scope issue that the existing uncommitted `LocalRemoteApp.tsx` mobile header/nav changes would pollute this todo's embedded bundle; fixed by regenerating assets from a temporary clean worktree that only contains the xterm fix. It also found this workflow entry stale; fixed by this update.
- Result: completed. Commit: `134c35f7`.

### P3-E-C-B-M re-embed current local web frontend assets

- Active slice: rebuild and sync the embedded local web static bundle from the current `remote-ui` source after user requested a fresh frontend embed.
- Tests written before implementation: none; this is a rebuild/sync todo rather than a code behavior change. The current worktree already contains an uncommitted `remote-ui/src/LocalRemoteApp.tsx` mobile header/navigation adjustment, so the generated bundle is expected to include it.
- Planned verification: run `cd remote-ui && npm run build:localweb`, then run focused localweb/CLI tests that prove embedded assets still serve. Run `git diff --check` before commit.
- Scope: do not alter remote protocol, pairing, resize ownership, TURN relay behavior, machine private key handling, or transport interfaces. This todo should only sync current frontend source into `termx-core/internal/remote/localweb/static` and record the result.
- Implementation notes: rebuilt current `remote-ui` local web assets and synced them into `termx-core/internal/remote/localweb/static`. The current mobile header/navigation change moves the mobile terminal/files controls from the bottom nav into the top header segmented control. After focused testing exposed the missing command-style accessible name, `aria-label="Open terminal"` and `aria-label="Open files"` were restored on the segmented buttons.
- Actual failing test before accessibility fix: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` failed because the updated `Files` segmented button no longer had the expected accessible name `/open files/i`.
- Focused tests after fix: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` passed 6 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm run build:localweb` passed with only the existing Vite large chunk warning.
- Broader tests after embed: `cd remote-ui && npm test` passed 106 tests; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestStartRemoteLocalWebServesEmbeddedPageAndStatus|TestRemoteLocal'` passed; `git diff --check` passed.
- Code review: `Pasteur` found no issues. It confirmed no workspace/tab/pane/session public model drift, no TURN credential or machine private key exposure, no WebRTC/fetch/native transport leakage into `LocalRemoteApp`, embedded assets match `remote-ui/dist` by SHA-256, and current accessible command names are covered.
- Result: completed. Commit: `fc5e92aa`.

### P3-E-C-B-N mobile terminal chrome height reclaim

- Active slice: respond to user feedback that the mobile web terminal still wastes vertical space on Console/Files/Terms-style controls and lacks an obvious way to return from the terminal page to the terminal list. Use `../tgent/tgent-app/src/pages/TerminalPage.tsx` as the interaction reference: a compact header with a back/list button and small action icons, with secondary tools in overlays/menus instead of persistent bottom navigation.
- Tests written before implementation: planned `remote-ui/src/LocalRemoteApp.test.tsx` regressions proving there is no bottom navigation, the terminal header exposes a clear terminal-list back button, file manager opens through a compact header action, and files overlay does not replace or compress the mounted terminal surface.
- Expected failing tests: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` should fail before implementation because current mobile shell still uses a visible Console/Files segmented header control rather than compact icon actions and a tgent-style terminal-list back button.
- Planned scope: change `LocalRemoteApp.tsx` mobile chrome and tests only, then rebuild embedded localweb assets. Do not change protocol, pairing primitives, resize ownership, TURN behavior, app/machine key handling, or transport interfaces. Preserve public machine -> terminal model and avoid workspace/tab/pane public concepts.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` failed as expected because there was no accessible `Back to terminal list` button and the existing mobile header still exposed the older segmented `Open terminal` / `Open files` control.
- Implementation notes: mobile local web now uses a tgent-style compact header with a back-to-terminal-list button, machine/terminal title, and small Files/Pair actions. Files opens as an absolute overlay with its own close button; the terminal panel remains `flex-1` and mounted underneath, and the mobile keybar is hidden only while the files overlay is active.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` passed 6 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after implementation: `cd remote-ui && npm test` passed 106 tests; `cd remote-ui && npm run build:localweb` passed with only the existing Vite large chunk warning; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestStartRemoteLocalWebServesEmbeddedPageAndStatus|TestRemoteLocal'` passed; `git diff --check` passed.
- Code review: `Halley` found no issues. It confirmed the mobile header exposes `Back to terminal list`, terminal title, and compact icon actions; terminal panel remains `flex-1`; Files is an overlay; keybar visibility restores in terminal mode; embedded assets match `remote-ui/dist`; and there is no workspace/tab/pane/session public model drift, TURN credential change, machine private key exposure, or UI transport-boundary leakage.
- Result: completed. Commit: `0be902f9`.

### P3-E-C-B-O local terminal list page

- Active slice: add the missing local embedded terminal list page. Local web should open directly to the terminal list page, not auto-enter the first terminal; selecting a terminal list item should connect and navigate to the terminal page. Mobile app machine list remains deferred per user direction.
- Tests written before implementation: planned `remote-ui/src/LocalRemoteApp.test.tsx` regressions proving initial local web render shows a terminal list page, does not connect/open a terminal until a terminal item is clicked, then enters the terminal page and Back to terminal list returns to the list page.
- Expected failing tests: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` should fail before implementation because `LocalRemoteApp` currently auto-selects `terminalList[0]`, connects immediately, and renders the terminal page on first load.
- Planned scope: keep changes in `LocalRemoteApp.tsx` and focused tests, then rebuild embedded localweb assets. Do not add mobile app machine pages yet, and do not change protocol, pairing primitives, TURN behavior, app/machine key handling, resize ownership, or transport interfaces.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` failed as expected because `termx-terminal-list-page` was missing, the mocked terminal was already mounted, and `createTransport` had been called before any terminal-list item click. After the list-first implementation, two old setup-error tests still failed because they expected immediate connection errors on first render; those tests were corrected to click `Open zsh` first, matching the new delayed-connect product behavior.
- Implementation notes: `LocalRemoteApp` now has explicit page state for `terminal-list` vs `terminal`, no longer auto-selects `terminalList[0]`, renders a true list page with machine header and `TerminalList`, enters the terminal page only from `openTerminal`, and returns via `Back to terminal list`. Desktop terminal sidebar is only rendered on the terminal page, list-page Pair opens the same pair sheet, and the transport lifecycle is gated by `page === "terminal"` so returning to the list disconnects the active local transport instead of keeping a hidden terminal connection alive.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` passed 6 tests; `cd remote-ui && npm run typecheck` passed.
- Review regression tests: after self-review, added coverage that returning from the terminal page to the list disconnects the active transport, and that completing Pair from the list page after a previous terminal visit keeps the app on the list page without auto-reconnecting to the hidden old terminal. The pair/list regression failed before the fix because pair success used stale `activeTerminalId` to return to the terminal page.
- Review fixes: transport creation is now gated by `page === "terminal"`; pair success no longer changes pages; the desktop terminal sidebar now includes an explicit `Show terminal list` control because the mobile `Back to terminal list` button is hidden at desktop widths. Tests now prove both the mobile header back control exists and the desktop list control returns to the true terminal list page.
- Code review: `Harvey` found a medium issue where desktop terminal pages had no visible way back to the true terminal list page; the existing test clicked a `md:hidden` mobile button that jsdom still exposed. Fixed with the desktop `Show terminal list` sidebar action and updated tests. The review found no workspace/tab/pane/session public model drift, TURN credential exposure, machine private key exposure, or transport-boundary leak.
- Final focused tests after review fix: `cd remote-ui && npm test -- --run src/LocalRemoteApp.test.tsx` passed 6 tests; `cd remote-ui && npm run typecheck` passed.
- Final broader tests after review fix: `cd remote-ui && npm test` passed 106 tests; `cd remote-ui && npm run build:localweb` passed with only the existing Vite large chunk warning and regenerated `termx-core/internal/remote/localweb/static/assets/index-DjSTbu7Y.js`; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestStartRemoteLocalWebServesEmbeddedPageAndStatus|TestRemoteLocal'` passed; `git diff --check` passed. `remote-ui/dist` and `termx-core/internal/remote/localweb/static` were verified byte-identical for `index.html`, `index-BwdcQlsw.css`, and `index-DjSTbu7Y.js`.
- Result: completed. Commit: `b7f07517`.

### P3-E-C-B-P terminal list metadata and machine-level files overlay

- Active slice: respond to user feedback that the terminal list should show useful terminal metadata such as name, command/environment intent, size, size-lock state, liveness, cwd, and currently available lifecycle metadata; and that Files should be a machine-level popup whose open/close state does not mutate terminal-list or terminal-page state.
- Tests written before implementation: planned `remote-ui/src/TerminalList.test.tsx`, `remote-ui/src/LocalRemoteApp.test.tsx`, and local terminal normalization tests proving terminal metadata rendering, size-lock normalization/display, list-page Files open/close without connecting a terminal, terminal-page Files open/close without changing the mounted terminal state, and file path state persists when closing/reopening from list and terminal pages.
- Expected failing tests: focused remote-ui tests should fail before implementation because `TerminalList` only renders title/command/size/state dot, the local terminal model does not normalize `size_locked`/tags/cwd/environment labels, and `LocalRemoteApp` still models Files as `panelMode === "files"` inside the terminal page using the active terminal connection.
- Planned scope: keep UI/business components behind `LocalAgentApi`, `LocalRemoteTransportFactory`, `PeerTransport`, and `FileManager` interfaces; do not introduce workspace/tab/pane/session public concepts; do not change TURN, pairing, app key, or machine private key behavior. UI will treat Files as machine-level state, but current core local file API still requires a terminal-scoped WebRTC transport, so this slice may use a hidden internal file context until a future machine-scoped file transport exists.
- Actual failing tests before implementation: `cd remote-ui && npm test -- --run src/TerminalList.test.tsx src/LocalRemoteApp.test.tsx src/localAgentApi.test.ts` failed as expected because the terminal list did not render cwd/environment/size-lock metadata and the Files flow still unmounted with terminal-page-local state. The first implementation pass then exposed a test-stack issue where hidden-overlay assertions used unsupported `toHaveAttribute`; those assertions were corrected to plain DOM property checks before final verification.
- Implementation notes: `termx-core/internal/remote/localweb/handler.go` and `termx-core/remote_localweb.go` now expose local terminal metadata for `size_locked`, `size_lock_mode`, `cwd`, and `environment` using the existing terminal tag inventory without introducing any workspace/tab/pane concepts. `remote-ui/src/model.ts` and `remote-ui/src/localAgentApi.ts` normalize that metadata into the shared machine->terminal model. `remote-ui/src/TerminalList.tsx` now renders terminal title, environment badge, command, cwd, liveness, size, size-lock state, and last-active metadata. `remote-ui/src/LocalRemoteApp.tsx` now treats Files as machine-level state with a dedicated overlay transport, keeps the overlay instance mounted but hidden when closed so file browsing state survives list/terminal navigation, and keeps terminal page/list page state independent from file open/close actions.
- Focused tests after implementation: `cd remote-ui && npm test -- --run src/TerminalList.test.tsx src/LocalRemoteApp.test.tsx src/localAgentApi.test.ts` passed 15 tests; `cd remote-ui && npm run typecheck` passed; `cd termx-core && go test ./internal/remote/localweb -run TestHandlerLocalTerminalsUsesTerminalModelOnly -count=1` passed.
- Review fixes: `Noether` found that the UI label `Last active` was misleading because current local terminal inventory only exposes creation time (`CreatedAt`), and that Files persistence was only proven with a mocked `FileManager`. This slice now renders the truthful label `Created`, adds a real `LocalRemoteApp.files.test.tsx` flow using the actual `FileManager`/`useFileManager` stack, and proves the intended rule: same terminal preserves file path state across list/terminal navigation while switching the file context terminal resets to that terminal’s own cwd.
- Final focused tests after review fixes: `cd remote-ui && npm test -- --run src/TerminalList.test.tsx src/LocalRemoteApp.test.tsx src/LocalRemoteApp.files.test.tsx src/localAgentApi.test.ts` passed 17 tests; `cd remote-ui && npm run typecheck` passed.
- Final broader tests after review fixes: `cd remote-ui && npm test` passed 109 tests; `cd remote-ui && npm run build:localweb` passed with only the existing Vite large chunk warning and regenerated `termx-core/internal/remote/localweb/static/assets/index-CN9mAXzL.js` plus `index-CmvYIe2j.css`; `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-cli && go test ./cmd/termx -run 'TestStartRemoteLocalWebServesEmbeddedPageAndStatus|TestRemoteLocal'` passed; `git diff --check` passed.
- Deferred implementation detail: the UI now behaves as machine-level Files, but the current local file API still binds over a terminal-scoped WebRTC transport. This slice reuses a hidden terminal-scoped file context internally and preserves file-manager state across navigation without exposing terminal-scoped semantics in the UI. A future core slice can replace that hidden transport with a true machine-scoped file channel when available.
- Subagent launched: `Noether` (`019de7d3-ed2e-7303-a06f-100ba2dc937f`) for P3-E-C-B-P code review focused on docs alignment, forbidden remote public concepts, transport boundaries, no TURN/private-key leakage, and test coverage. Result: one medium correctness issue on misleading activity wording and one low test-gap issue on mock-only Files persistence; both fixed. No workspace/tab/pane public model drift, TURN credential exposure, machine private key exposure, or transport-boundary leakage found.
- Result: completed. Commit: `b0de33ee`.

### P3-E-C-B-Q terminal inventory push over WebRTC

- Active slice: push terminal inventory changes from `termx` to web/app over WebRTC so terminal create/remove/resize/state/metadata changes refresh the terminal list automatically without polling. The UI-facing consumption path must be abstracted behind a shared interface rather than hard-wiring browser details into `remote-ui`.
- Tests written before implementation: planned `termx-core/remote_localweb_test.go`, `termx-core/server_contract_test.go`, `remote-ui/src/transport.test.ts`, `remote-ui/src/LocalRemoteApp.test.tsx`, and focused browser adapter tests for a machine-level inventory watcher.
- Expected failing tests: current local RTC path only accepts `api`, `terminal:{terminal_id}`, and `file:{transfer_id}` labels; `termx-core` does not publish a metadata-change event for `SetTags`/`SetMetadata`; `remote-ui` has no machine-level inventory subscription interface; and `LocalRemoteApp` only refreshes terminals on initial load.
- Planned scope: reuse the existing `termx-core` protocol/event bus rather than inventing a web-only event system. Add a machine-level WebRTC events path, keep app/browser transport details inside adapters, and make `remote-ui` consume only a shared inventory event subscription interface. Do not reintroduce workspace/tab/pane public concepts, do not issue TURN credentials on local/anonymous paths, and do not expose machine private key material.
- Actual failing tests before implementation: `cd termx-core && go test ./termx-core -run TestSetMetadataAndTagsPublishTerminalStateChangedEvent -count=1` failed because `SetTags`/`SetMetadata` emitted no event at all; `cd remote-ui && npm test -- --run src/localWebRtcTransport.test.ts src/LocalRemoteApp.test.tsx` failed because there was no machine-level inventory event interface/adapter path; follow-up focused runs also exposed existing UI-test drift from user-edited local web chrome, which was corrected so the red tests only represented the new inventory-push gap.
- Implementation notes: added a new protocol/core event type `EventTerminalMetadataChanged` and now publish it from `termx-core/terminal.go` when tags/metadata change. Reused the existing protocol `events` request/event-bus path instead of inventing a new web-only channel. Local RTC now accepts a dedicated `events` DataChannel alongside `api`, `terminal:{terminal_id}`, and `file:{transfer_id}`; machine-level inventory sessions are events-only, allow empty `terminal_id`, and are constrained by `transportScope{MachineEventsOnly:true}` so they can subscribe only to terminal inventory event types and cannot issue create/kill/get/snapshot or session-scoped requests. `remote-ui` now exposes a shared `TerminalInventoryEvents` interface, `localAgentApi` adds a machine-level inventory RTC answer path, `localWebRtcTransport` implements a browser inventory-events connection over the `events` DataChannel, `localWebEntry` wires a lazy browser inventory-events adapter, and `LocalRemoteApp` refreshes `listTerminals()` automatically when inventory events arrive, including after pairing retries via the existing `transportRetryToken`.
- Focused tests after implementation: `cd termx-core && go test ./termx-core -run 'TestSetMetadataAndTagsPublishTerminalStateChangedEvent|TestE2ERemoteLocalWebHandlerAnswersMachineInventoryEventsOffer' -count=1` passed; `cd remote-ui && npm test -- --run src/localAgentApi.test.ts src/localWebRtcTransport.test.ts src/transport.test.ts src/LocalRemoteApp.test.tsx src/LocalPairPanel.test.tsx src/localAppIdentity.test.ts src/localWebEntry.test.tsx` passed 48 tests; `cd remote-ui && npm run typecheck` passed.
- Broader tests after implementation: `cd remote-ui && npm test` passed 112 tests; `git diff --check` passed.
- Deferred implementation detail: local embedded web now auto-refreshes terminal inventory from machine-level WebRTC push, but mobile/native still needs its own adapter implementation to consume the same shared `TerminalInventoryEvents` interface in P4-A. The current local path intentionally uses a dedicated machine-level events RTC session rather than overloading an active terminal session.
- Worktree note: the shared workspace currently also contains user-driven local UI and fast-start changes outside this todo (`README.md`, `Makefile`, several `remote-ui` presentation files, and regenerated embedded assets). This slice was kept scoped to the machine-level inventory push path and intentionally did not revert or overwrite those unrelated in-flight edits.
- Code review: `Nietzsche` found a high issue where machine-events-only sessions still accepted empty `types` and could subscribe to non-inventory events, plus a medium test gap where metadata-change refresh was not proven through the real `events` channel. Fixed by requiring explicit allowed terminal inventory event types in machine-events-only scope, adding an end-to-end machine inventory metadata event test over local WebRTC, and retaining the shared interface boundary. No workspace/tab/pane drift, TURN credential exposure, machine private key exposure, or browser/native boundary leak was found.
- Result: completed. Commit: `936e6cd9`.

### P3-E-C-B-R terminal list management and verification-first UX

- Active slice: add terminal list management behavior for local embedded web: long-press a terminal to open actions, create/update/delete local terminals, explicit pull-to-refresh / manual refresh fallback in case events are missed, and move pairing/verification ahead of management UI instead of keeping Pair only as a small top-right action.
- Tests written before implementation: planned `termx-core/internal/remote/localweb/handler_test.go`, `remote-ui/src/localAgentApi.test.ts`, `remote-ui/src/LocalRemoteApp.test.tsx`, and any focused component tests needed for long-press menus / local management sheets.
- Expected failing tests: current local embedded web only supports `getStatus`, `listTerminals`, `pair`, and RTC answer calls; there is no local terminal create/update/delete endpoint in `localweb`, no matching `LocalAgentApi` methods, no long-press action menu in `TerminalList`, no pull-to-refresh/manual refresh path, and the list page still exposes Pair as a small header icon instead of making verification a primary first-run path.
- Planned scope: keep all management behind machine->terminal public semantics, reuse existing server `Create`, `SetMetadata`, `SetTags`, `Kill`/`remove` behavior where appropriate, and keep browser/native specifics behind interfaces. Do not reintroduce workspace/tab/pane concepts or leak TURN / machine private key material.
- Implementation notes: the local web HTTP surface now exposes machine-level terminal create, update, and delete endpoints under `/api/local/terminals`, mapped onto the existing server create/metadata/tag/remove flows in `termx-core`. `remote-ui` now consumes those endpoints through `LocalAgentApi`, opens terminal management from long-press or context menu, gates open/files/manage flows behind explicit verification when local pairing is configured, adds manual refresh plus pull-to-refresh fallback, and rebuilds the embedded local web assets with the updated mobile-first shell.
- Focused tests after implementation: `go test ./termx-core/internal/remote/localweb` passed; `cd remote-ui && npm test -- src/localAgentApi.test.ts src/TerminalList.test.tsx src/LocalRemoteApp.test.tsx src/LocalPairPanel.test.tsx src/LocalRemoteApp.files.test.tsx src/localAppIdentity.test.ts` passed 29 tests.
- Broader validation and regression fix: `cd remote-ui && npm test` initially failed in `src/localWebEntry.test.tsx` because `localWebEntry` would construct browser pair helpers for a partial storage object and `LocalRemoteApp` would eagerly read `pair.storage.loadCertificate()` on first render. Fixed by gating browser pair, inventory-events, and transport helper creation on storage objects that actually implement `getItem` and `setItem`. After the fix, `cd remote-ui && npm test` passed 117 tests; `go test ./termx-core/... ./termx-cli/...` passed; `cd remote-ui && npm run build:localweb` passed and resynced embedded assets.
- Commit split note: the todo landed in two feature commits because the machine-level localweb API and the shared remote-ui UX changed at different layers. Core/localweb terminal management contract: `cd853ddd`. Shared local embedded web verification-first management UX and embedded asset refresh: `fb2fae90`.
- Result: completed. Primary commit recorded in the todo table: `fb2fae90`.

### P3-DX-A root local embedded web fast-start workflow

- Active slice: add root-level developer entrypoints so local embedded web assets and the `termx` CLI can be rebuilt, launched, and iterated from the monorepo root without manually stitching together frontend and Go commands.
- Tests written before implementation: planned `remote-ui/src/viteConfig.test.ts` plus broader remote-ui and Go regression coverage around the local daemon workflow.
- Expected failing tests: there was no root `Makefile`, `remote-ui` did not expose a Vite dev script, and the dev server did not proxy `/api/*` to the local daemon origin.
- Implementation notes: added a root `Makefile` with `localweb-build`, `termx-build`, `remote-dev`, `remote-daemon`, `remote-open`, `remote-status`, and focused test targets; documented the workflow and override variables in `README.md`; added `npm run dev` to `remote-ui`; and configured Vite to proxy `/api/*` to `TERMX_LOCAL_WEB_ORIGIN` with a focused test for the default target.
- Focused tests after implementation: `cd remote-ui && npm test -- src/viteConfig.test.ts` passed.
- Broader tests after implementation: `cd remote-ui && npm test` passed 117 tests; `go test ./termx-core/... ./termx-cli/...` passed.
- Result: completed. Commit: `e5416e0a`.

### P3-F anonymous rendezvous HTTP adapter/service

- Active slice: HTTP adapter/service for the existing anonymous rendezvous store.
- Tests written before implementation: `termx-core/internal/remote/rendezvous/http_handler_test.go`.
- Expected failing tests: `cd termx-core && go test ./internal/remote/rendezvous -run TestHTTP -count=1` should fail because `NewHTTPHandler` and `HTTPConfig` do not exist yet.
- Actual failing tests before implementation: `cd termx-core && go test ./internal/remote/rendezvous -run TestHTTP -count=1` failed as expected with undefined `NewHTTPHandler` and `HTTPConfig`.
- Planned scope: implement only the lightweight anonymous signaling HTTP surface from `docs/remote-rebuild/api.md` and `auth-and-pairing.md`: `POST /api/v1/anonymous/channels`, `GET /api/v1/anonymous/channels/{channel_id}/events`, `POST /api/v1/anonymous/channels/{channel_id}/offer`, and `POST /api/v1/anonymous/channels/{channel_id}/answer`. The HTTP layer must reuse the existing store for TTL, payload limit, channel secret verification, app public key binding, message type restrictions, and public STUN validation; it must not issue TermX TURN credentials or carry terminal/file data.
- Implementation notes: added `NewHTTPHandler` and `HTTPConfig` in `termx-core/internal/remote/rendezvous/http.go`. The handler exposes anonymous channel, events, offer, answer, and candidate endpoints; returns localweb-compatible error envelopes; uses `Authorization: Rendezvous {channel_id}:{channel_secret}` for events; uses body `channel_secret` for POSTs; maps request bodies into store messages; and reuses the store for TTL/payload/message/app-public-key/TURN-STUN validation. It deliberately does not validate app certificate business permissions because the docs require the agent to verify certificate/signature.
- Self-review regression: added nil-store HTTP coverage after noticing events/offer/answer could otherwise depend on store-level nil errors; `go test ./internal/remote/rendezvous -run TestHTTPHandlerWithoutStoreReturnsEnvelope -count=1` failed before the fix with `400 rendezvous_message_rejected`, then passed after adding a uniform `rendezvous_unavailable` check.
- Focused tests after implementation: `cd termx-core && go test ./internal/remote/rendezvous -run TestHTTP -count=1` passed; `cd termx-core && go test ./internal/remote/rendezvous -count=1` passed.
- Self-review regression after implementation: added `TestHTTPOfferForwardsCertificateAndSignatureEnvelope` after noticing the first HTTP adapter only forwarded the nested `offer` SDP payload and would drop `app_certificate`/`signature` needed by the daemon. The test failed before the fix with an events payload missing `app_certificate`.
- Code review: `Mendel` found a medium docs-alignment issue where the HTTP adapter could not post standalone ICE candidates even though anonymous rendezvous must forward offer/answer/ICE candidates and the store already supports `MessageCandidate`. No workspace/tab/pane public model, TURN credential issuance, machine private key exposure, rendezvous-side certificate business validation, terminal/file data relay, or store limit issue was found.
- Review regression tests: added `TestHTTPCandidateEndpointForwardsTrickleICE`, which failed before the fix with `404 not_found` for `/candidate`.
- Review fix: added `/api/v1/anonymous/channels/{channel_id}/candidate`, added offer/answer envelope forwarding for documented `app_certificate`, `offer`/`answer`, and `signature` fields, and tightened nested envelope validation so terminal/file/TURN-style fields remain rejected while daemon-verifiable certificate/signature material can traverse rendezvous.
- Focused tests after review fix: `cd termx-core && go test ./internal/remote/rendezvous -run 'TestHTTP|TestSignalingPayload|TestChannel|TestUnsupported|TestPerChannel|TestInvalidTURN' -count=1` passed.
- Follow-up hardening regression tests: added coverage that channel secrets are not stored in plaintext and that anonymous payloads reject TURN URLs and relay ICE candidates. `cd termx-core && go test ./internal/remote/rendezvous -run 'TestChannelSecretIsNotStoredInPlaintext|TestSignalingPayloadMustBeStructured' -count=1` failed before the fix because `secretHash` still contained the raw channel secret.
- Follow-up hardening fix: channel secrets are now stored as SHA-256 verifier material and compared by hashing submitted secrets; anonymous signaling payload validation now rejects TURN URLs and `typ relay` candidates recursively in nested envelope data.
- Follow-up code review: `Anscombe` found high plaintext channel secret storage, medium `/answer` missing `app_certificate`/`signature` forwarding, and low `auth-and-pairing.md` missing `/candidate`. The plaintext secret finding was already fixed before the notification was received and was reverified with a non-cached test; answer envelope forwarding and docs drift were then fixed.
- Follow-up regression tests after Anscombe: added `TestHTTPAnswerForwardsCertificateAndSignatureEnvelope`, which failed before the fix because answer events only carried `{"answer": ...}`. Updated `/answer` to forward `app_certificate`, `answer`, and `signature`, and updated `docs/remote-rebuild/auth-and-pairing.md` to include `/candidate`.
- Final focused tests after follow-up fixes: `cd termx-core && go test ./internal/remote/rendezvous -run 'TestHTTPAnswerForwardsCertificateAndSignatureEnvelope|TestChannelSecretIsNotStoredInPlaintext|TestHTTP|TestSignalingPayloadMustBeStructured' -count=1` passed; `cd termx-core && go test ./internal/remote/rendezvous -count=1` passed.
- Final broader tests after follow-up fixes: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Subagents: `Plato` confirmed placement in `termx-core/internal/remote/rendezvous`, endpoint shapes, localweb error envelope reuse, documented split between header auth for events and body secret for offer/answer, and the candidate endpoint gap. It recommended not adding a standalone `termx-rendezvous/` service shell yet.
- Result: completed. Commit: `a4ab3b2`.

### P3-D-B shared terminal client and Terminal.tsx boundary

- Tests written before implementation: `remote-ui/src/terminalClient.test.ts`, `remote-ui/src/useTerminalSession.test.tsx`, `remote-ui/src/Terminal.test.tsx`, and `remote-ui/src/test/mockTerminalTransport.ts`.
- Expected failing tests: `cd remote-ui && npm test` fails because `./terminalClient`, `./useTerminalSession`, and `./Terminal` do not exist yet. The tests cover `terminalId`-only public identity, `terminal:{terminal_id}` channel labels, terminal output/snapshot/info handling without pane/session/window fields, reattach preserving terminal identity, reducer-backed app resume verification, and a lightweight TermX `Terminal.tsx` component boundary with no tgent pane/session props.
- Actual failing tests before implementation: `cd remote-ui && npm test` failed as expected with missing module errors for `./terminalClient`, `./useTerminalSession`, and `./Terminal`; existing P3-D-A tests still passed.
- Planned scope: create the TermX terminal client/hook/component seam in `remote-ui/` by adapting the tgent terminal client/component boundary without copying `paneId`, `PaneInfo`, session/window/pane messages, or direct browser/native transport dependencies. Full xterm rendering polish can follow after the client/message contract is stable.
- Review regression tests: added tests for streaming terminal output before snapshot, deriving connection mode from injected transport info, and preserving close reasons as failed terminal channel state. These failed before the review fixes.
- Focused tests after implementation and review fixes: `cd remote-ui && npm test` passed 21 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm audit` passed with 0 vulnerabilities.
- Broader tests after implementation and review fixes: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Hypatia` found terminal output chunks were dropped before rendering, close reasons were overwritten by clean close handling, and shared terminal session state was hardcoded to local mode. Fixed by storing terminal output text in the hook/component boundary, deriving mode from `transport.getConnectionInfo()`, and letting reasoned close lifecycle messages preserve failed channel state.
- Result: completed. Commit: `e87d37a`.

### P3-D-A shared remote UI logic package

- Tests written before implementation: `remote-ui/src/model.test.ts`, `remote-ui/src/connectionMessageReducer.test.ts`, `remote-ui/src/eventQueue.test.ts`, and `remote-ui/src/transport.test.ts`.
- Expected failing tests: `cd remote-ui && npm test -- --runInBand` should fail because `model`, `connectionMessageReducer`, `eventQueue`, and `transport` exports do not exist yet. The tests cover machine/terminal public model normalization, workspace/tab/window/pane key rejection, native-like app resume verification, offline/online reconnect intent preservation, visible error routing, event ordering/dedup/backpressure, and interface-only transport boundaries.
- Actual failing tests before implementation: `cd remote-ui && npm test` failed as expected because `./model`, `./connectionMessageReducer`, and `./eventQueue` were missing. `transport.test.ts` passed because it currently only performs compile-time interface assertions and imports the missing transport surface as type-only.
- Planned scope: create `remote-ui/` as a top-level TypeScript package, keep pure logic/adapters testable without a browser, and use tgent component/API structure only as reference while replacing `server/session/window/pane` public semantics with `machine/terminal`.
- Exploration: `Sagan` reviewed tgent component boundaries and confirmed `Terminal.tsx`, terminal client, and file client/file manager boundaries are useful references, while `SessionList.tsx` must only inform `TerminalList.tsx` structure and cannot carry session/window/pane public semantics. State for app resume, network switching, reattach, duplicate events, backpressure, and toast/banner/modal routing should live in `connectionMessageReducer`/`ConnectionEventQueue`.
- Focused tests after implementation: `cd remote-ui && npm test` passed 10 tests; `cd remote-ui && npm run typecheck` passed; `cd remote-ui && npm audit` passed with 0 vulnerabilities.
- Broader tests after implementation: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-cli && go test ./cmd/termx` passed; `git diff --check` passed.
- Code review: `Parfit` found no high/medium issues. Low findings were stale workflow text and a top-level-only transport implementation leakage guard. Fixed by updating this workflow and adding a regression proving nested `nativePlugin` payloads are rejected by `ConnectionEventQueue`/`assertMessageBoundary`.
- Result: completed. Commit: `fe9025d`.

### R0 seed persistent workflow file

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: not run; documentation/workflow-only todo.
- Result: completed. Commit: `8734d00`.

### R1 embedded local web first plan revision

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: not run; documentation/workflow-only todo.
- Broader tests: `git diff --check` passed.
- Result: completed. Commit: `6a657be`.

### R2 tgent-aligned UI and native-like message handling constraint

- Tests written before implementation: none; documentation/workflow-only todo.
- Expected failing test: not applicable.
- Focused tests: pending.
- Broader tests: `git diff --check` passed.
- Result: completed. Commit: `0ce0023`.

### P2-A machine key generation/load/fingerprint

- Tests written before implementation: `termx-core/internal/remote/identity/identity_test.go`
- Expected failing test: `go test ./internal/remote/identity` fails to build because `LoadOrCreateMachineKey`, `MachineKeyFilename`, and `MachinePublicKeyFingerprint` do not exist yet.
- Focused tests: failed as expected before implementation.
- Code review regression tests: added concurrent first-run and private-key JSON leak tests; `go test ./internal/remote/identity` fails as expected because `MachineKey.Sign` and hidden `privateKey` do not exist yet.
- Follow-up review regression tests: added formatting redaction test; `go test ./internal/remote/identity` failed as expected before `String`/`GoString` redaction.
- Final focused tests: `cd termx-core && go test ./internal/remote/identity` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `5aef5b8`.

### P2-B app certificate canonical/sign/verify/replay

- Tests written before implementation: `termx-core/internal/remote/cert/cert_test.go`
- Expected failing test: `go test ./internal/remote/cert` fails to build because `CanonicalPayload`, `AppCertificatePayload`, `SignAppCertificate`, `VerifyAppCertificate`, and `NewReplayWindow` do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: app public key length and duplicate capabilities initially accepted; `go test ./internal/remote/cert` failed before validation was tightened.
- Code review regression tests: signer initially accepted caller-supplied machine fingerprint and could issue a certificate it would later reject; `go test ./internal/remote/cert` failed before signer stamped the fingerprint from `machineKey.PublicKey`.
- Final focused tests: `cd termx-core && go test ./internal/remote/cert` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `62d1f70`.

### P2-C local pair session primitives

- Tests written before implementation: `termx-core/internal/remote/pairing/session_test.go`
- Expected failing test: `go test ./internal/remote/pairing` fails to build because `NewManager`, `Config`, `Manager`, and `ClaimRequest` do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: invalid app public key initially consumed the one-time secret; `go test ./internal/remote/pairing` failed before claim consumption moved after successful certificate issuance.
- Code review regression tests: unsupported requested capabilities initially received machine-signed certificates; `go test ./internal/remote/pairing` failed before capabilities were restricted to `terminal` and `file_manager`.
- Final focused tests: `cd termx-core && go test ./internal/remote/pairing` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `12067cb`.

### P2-D CLI pair skeleton and remote status regression

- Tests written before implementation: `termx-core/protocol/client_test.go`, `termx-core/remote_test.go`, and `termx-cli/cmd/termx/main_test.go`
- Expected failing test: `go test ./protocol -run TestClientRemotePairStart` fails to build because `PairStartParams`, `PairStartResult`, and `Client.RemotePairStart` do not exist yet.
- Additional expected failing test: `go test ./cmd/termx -run 'TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` fails to build because `pairStartClient` and the top-level `pair` command do not exist yet.
- Focused tests: failed as expected before implementation at protocol/server/CLI layers.
- Hardening regression tests: repeated pair starts with a new `--local-url` initially reused the stale URL; `go test . -run TestE2ERemotePairStartUsesLatestLocalPairURL` failed before server pairing config updates were added.
- Code review regression/fix: review found that replacing the server pairing manager on config changes could invalidate unexpired sessions. Fixed by adding `pairing.Manager.UpdateConfig`, storing session issuer config per session, and updating future-session config without discarding existing sessions.
- Final focused tests: `cd termx-core && go test ./protocol -run TestClientRemotePairStart` passed; `cd termx-core && go test . -run 'TestE2ERemote(PairStart|Status)'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` passed.
- Broader tests: `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `4b24258`.

### P3-A anonymous rendezvous interfaces/contracts

- Tests written before implementation: `termx-core/internal/remote/rendezvous/channel_test.go`
- Expected failing test: `go test ./internal/remote/rendezvous` fails to build because `NewMemoryStore`, `Config`, `CreateChannelRequest`, `Message`, and message type constants do not exist yet.
- Focused tests: failed as expected before implementation.
- Hardening regression tests: unsupported message types were initially accepted; `go test ./internal/remote/rendezvous` failed before message types were restricted to offer/answer/candidate.
- Code review regression tests: excessive TTL, non-JSON or non-signaling payloads, different app public keys after claim, and unbounded per-channel messages initially passed or failed to build; `go test ./internal/remote/rendezvous` failed before max TTL, structured signaling payload validation, app public key binding, and message count limits were added.
- Final focused tests: `cd termx-core && go test ./internal/remote/rendezvous` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `git diff --check` passed.
- Result: completed. Commit: `4012a1b`.

### P3-B embedded local web foundation

- Tests written before implementation: `termx-core/internal/remote/localweb/handler_test.go`, `termx-core/remote_localweb_test.go`, and `termx-cli/cmd/termx/main_test.go`.
- Expected failing test: `cd termx-core && go test ./internal/remote/localweb` fails to build because `Status`, `Terminal`, `NewHandler`, `Config`, and `NewStaticAssets` do not exist yet; `cd termx-core && go test . -run TestE2ERemoteLocalWebHandlerStatusTerminalsAndPair` fails to build because `Server.LocalWebHandler`, `LocalWebOptions`, and `NewLocalWebStaticAssets` do not exist yet.
- Additional expected failing test: `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocalWebAddrFromEnv|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` fails to build because `remoteLocalWebAddrFromEnv` and `startRemoteLocalWeb` do not exist yet.
- Focused tests: failed as expected before implementation at core handler, core server wrapper, and CLI daemon local web layers.
- Review regression tests: `cd termx-core && go test ./internal/remote/localweb -run TestHandlerErrorResponseUsesDocumentedEnvelope` failed before local API errors were changed to the documented `error.code`, `error.message`, and `error.request_id` envelope.
- Final focused tests: `cd termx-core && go test ./internal/remote/localweb` passed; `cd termx-core && go test . -run 'TestE2ERemote(LocalWeb|Pair|Status)'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemote(LocalWeb|Config)|TestStartRemoteLocalWeb|TestRootCmdHasRemoteStatusAndPairCommands|TestPairCmdEmitsJSONPairSession'` passed.
- Broader tests: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed; `git diff --check` passed.
- Code review: `Avicenna` pre-CLI review found pair response/docs and embedded asset coverage might be incomplete from an earlier snapshot, plus `last_active_at` creation-time semantics. `Bacon` final review found stale workflow state, missing CLI listener-path coverage for terminals/pair, the same `last_active_at` placeholder, and local API error envelope drift.
- Fixes after review: local pair response/docs include `machine_public_key_fingerprint` and `expires_at`; default assets use `go:embed`; CLI TCP listener test covers `/`, `/api/local/status`, `/api/local/terminals`, and `/api/local/pair`; local API errors now use documented `code/message/request_id`; `last_active_at` remains recorded as a placeholder until runtime activity metadata exists.
- Result: completed. Commit: `6d9048f`.

### P3-C local WebRTC signaling and ICE TCP mux

- Tests written before implementation: `termx-core/internal/remote/localweb/handler_test.go`, `termx-core/internal/remote/rtc/offer_signature_test.go`, `termx-core/internal/remote/rtc/tcp_mux_test.go`, `termx-core/remote_localweb_test.go`, and `termx-cli/cmd/termx/main_test.go`.
- Expected failing tests: `cd termx-core && go test ./internal/remote/localweb -run TestHandlerLocalRTCOfferAnswersWithLocalContract` failed to build because local RTC DTOs, interface, and route did not exist; `cd termx-core && go test ./internal/remote/rtc -run 'TestOfferSignature|TestLocalICETCPMux'` failed to build because offer signature helpers and local ICE TCP mux did not exist; `cd termx-core && go test . -run TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer` failed to build because public `StartLocalICETCPMux`, `LocalWebOptions.ICETCPMux`, and signature helpers did not exist; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocalICETCPAddrFromEnv|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` failed to build because CLI ICE TCP env parsing and local web mux injection did not exist.
- Focused tests after implementation: `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2ERemote(LocalWeb|Pair|Status)|TestE2E_WebRTC'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Broader tests before final code review: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed.
- Implementation notes: first P3-C slice uses an independent local ICE TCP listener instead of tgent-style same-port cmux; local offer verification checks machine-signed app certificate, app certificate machine_id against the local machine, app offer signature, nonce replay, requested terminal existence, and both terminal/file_manager capabilities before calling the existing WebRTC answer path.
- Deferred from this slice: browser smoke and same-port HTTP/ICE TCP cmux remain in later P3 work; current contract exposes the separate ICE TCP endpoint through local status and local RTC answer metadata.
- Scope constraints: no TermX TURN relay credentials in local responses, no workspace/tab/pane public model, app private key stays app-local, machine private key stays machine-local, and Web/native transport behavior remains behind interfaces.
- Code review findings before completion: signed local RTC offers verified `terminal_id`, but the accepted WebRTC terminal transport still exposed the full server protocol; data-channel capability checks were too broad in the first implementation; missing negative tests for wrong machine/terminal/capability/stale signature cases; workflow next action was stale.
- Next fix under TDD: add regression tests proving a local RTC offer signed for one terminal cannot snapshot/attach another terminal, wrong channel labels are denied, file-only/terminal-only certificates are rejected by the full local RTC contract, wrong local machine IDs are rejected, nonexistent terminals fail at the HTTP endpoint, and stale timestamps are rejected.
- Expected failing regression: `cd termx-core && go test . -run 'TestE2ERemoteLocalWebHandler(AnswersAuthenticatedRTCOffer|RejectsInvalidRTCOfferAuth)'` fails because a local RTC terminal data channel signed for terminal `1` can still snapshot terminal `2`.
- Fix after failing regression: added terminal-scoped transport enforcement for local RTC terminal data channels, added RTC channel policy to close unauthorized `terminal:*`, `api`, and `file:*` channels, and added negative tests for nonexistent terminals, stale offer signatures, insufficient capabilities, and app certificates signed for the wrong local machine.
- Final focused tests before review: `cd termx-core && go test . -run 'TestE2ERemoteLocalWebHandler(AnswersAuthenticatedRTCOffer|RejectsInvalidRTCOfferAuth)'` passed; `cd termx-core && go test ./internal/remote/rtc -run 'TestAnswerOfferChannelPolicyRejectsWrongTerminalChannel|TestOfferSignature|TestLocalICETCPMux'` passed.
- Final review findings: `Cicero` found one high issue where ICE TCP could be reported enabled without a usable loopback TCP candidate, plus stale workflow text. No workspace/tab/pane leakage, local TURN credentials, app machine-private-key exposure, transport-boundary leakage, or separate agent binary was found.
- Review regression: `cd termx-core && go test ./internal/remote/rtc -run TestLocalICETCPMuxAnswerIncludesLoopbackTCPCandidate` failed before the fix because the answer SDP contained TCP candidates on non-loopback interface IPs but did not contain `127.0.0.1`.
- Review fix: local ICE TCP mux now enables loopback candidate gathering and filters candidates to the bound listener IP when the listener is bound to a specific address; the answer SDP test now proves a passive loopback TCP host candidate exists.
- Final focused tests after review fix: `cd termx-core && go test ./internal/remote/localweb ./internal/remote/rtc ./internal/remote/fileapi` passed; `cd termx-core && go test . -run 'TestE2ERemoteLocalWebHandler(AnswersAuthenticatedRTCOffer|RejectsInvalidRTCOfferAuth)|TestE2E_WebRTC'` passed; `cd termx-cli && go test ./cmd/termx -run 'TestRemoteLocal(Web|ICE)|TestStartRemoteLocalWebServesEmbeddedPageAndStatus'` passed; `git diff --check` passed.
- Final broader tests after review fix: `cd termx-core && go test ./internal/remote/...` passed; `cd termx-core && go test ./...` passed; `cd termx-cli && go test ./...` passed.
- Result: completed. Commit: `52d964b`.

## Subagents

- `Galileo` (`019ddf61-2c8a-7cf0-8a00-991250f8b294`): explorer for termx-core P2 identity/runtime integration points. Result: preserve existing `DeviceIdentity` status baseline, add machine-named key/cert/pairing primitives, and avoid exposing machine private key.
- `Lovelace` (`019ddf61-2c9d-76b2-b547-7b4afd1f5cfe`): explorer for termx-cli remote command/test structure. Result: future `termx pair` should be top-level, use protocol/public core API, and leave `termx remote status` unchanged.
- `Fermat` (`019ddf63-ea27-7260-94ef-84bb9b078503`): P2-A code review. Findings: first-run concurrency race, exported private key boundary, stale workflow. Result: fixed with exclusive install/reload, unexported private key plus signer method, and workflow updates.
- `Jason` (`019ddf6a-ef0e-7e32-89da-880e1a2590f5`): P2-A follow-up review. Findings: Go formatting could expose unexported private key fields, stale workflow. Result: fixed with redacted `String`/`GoString` and formatting regression tests.
- `Socrates` (`019ddf76-3981-7193-b7c7-2a9ba59d4941`): P2-B code review. Findings: signer could issue self-inconsistent machine fingerprint, workflow stale. Result: fixed by stamping `MachinePublicKeyFingerprint` from `machineKey.PublicKey` before canonical signing and updating workflow.
- `Dirac` (`019ddf7d-2bf6-78d0-89d0-e886099b6d01`): P2-C code review. Findings: arbitrary requested capabilities could be machine-signed, local pair request struct lacked snake_case JSON tags, workflow stale. Result: fixed with capability allowlist, JSON tags, and workflow updates.
- `Averroes` (`019ddf8c-a240-7063-8d97-e582d128011f`): P2-D code review. Findings: changing pair config replaced the manager and invalidated active sessions, workflow stale. Result: fixed with manager config updates that preserve existing sessions and workflow updates.
- `Epicurus` (`019ddf9b-6e36-7c32-9433-71c59a027d69`): P3-A code review. Findings: unbounded TTL, arbitrary data under signaling message types, missing app binding after claim, unbounded message retention, workflow stale. Result: fixed with max TTL, structured signaling payload validation, app public key binding, per-channel message limits, and workflow updates.
- R1: no subagent launched; documentation-only planning adjustment requested by the user and no implementation correctness review was required.
- `Raman` (`019de136-503d-7d10-8738-0d95fcd79bda`): explorer for P3-B core local web/server integration points. Result: use `Server.List`, `Server.RemoteStatus`, and the existing pairing manager through narrow localweb interfaces; avoid importing root `termx` from internal packages; add local status machine-key fingerprint without exposing the machine private key.
- `Avicenna` (`019de13e-d21e-76c2-98ac-f424e1250e2f`): P3-B code review before CLI daemon wiring. Findings: pair response/docs and embedded asset coverage looked incomplete from the reviewed snapshot, and terminal `last_active_at` currently maps creation time. Result: pair response/docs and embedded default assets are present in local files; CLI daemon serving path and `last_active_at` placeholder documentation remain active work inside P3-B.
- `Bacon` (`019de14b-634e-7e33-b3e8-231a41bd965a`): final P3-B code review after CLI daemon wiring. Findings: workflow stale, CLI listener test did not cover terminals/pair, `last_active_at` is creation time, and local error responses lacked `code/request_id`. Result: workflow updated, CLI listener coverage expanded, error envelope fixed; `last_active_at` remains a documented placeholder.
- `Nash` (`019de156-1151-77b3-ad01-5b2e67f253e0`): P3-C explorer for local RTC handler/contract boundaries. Result: keep `localweb` behind a narrow `RTCOfferAnswerer` interface, verify app certificate/signature/nonce/requested terminal before answering, reuse `rtc.AnswerOffer`, and expose `session_id`, `machine_id`, and `terminal_id` in local offer docs.
- `Linnaeus` (`019de156-272a-7541-8ef0-20fe88d2043f`): P3-C explorer for local WebRTC-over-TCP slice. Result: first slice should add an independent local ICE TCP listener and status metadata instead of same-port cmux; CLI envs are `TERMX_REMOTE_LOCAL_ICE_TCP_ENABLE` and `TERMX_REMOTE_LOCAL_ICE_TCP_ADDR`.
- `Beauvoir` (`019de165-691d-7d80-86da-dbd140400107`): P3-C code review before commit. Findings: signed local RTC offers did not scope the terminal protocol after data-channel acceptance; capability gating was too coarse; machine identity comparison and negative security tests needed explicit coverage; workflow next action was stale. Result: fixed with terminal-scoped transport, channel policy, machine identity checks, and negative tests.
- `Cicero` (`019de177-2141-7570-b3c6-30248c4bd521`): P3-C final code review. Findings: ICE TCP could be reported enabled without a usable loopback TCP candidate; workflow stale. Result: fixed with loopback candidate gathering, bound-listener IP filtering, answer SDP regression test, and workflow updates.
- `Sagan` (`019de189-868e-71c1-8010-25433d7758e6`): P3-D read-only explorer for tgent UI boundaries. Findings: keep `Terminal.tsx`, terminal client, file client, and file manager boundaries close where practical; convert `paneId` to `terminalId`; treat `SessionList.tsx` as structure-only input for `TerminalList.tsx`; move lifecycle, reattach, duplicate event handling, backpressure, and visible error routing to TermX reducer/queue.
- `Parfit` (`019de18f-90ad-70e2-998b-2ea5182f0390`): P3-D-A code review. Findings: stale workflow text and nested transport implementation leakage could pass the first boundary guard. Result: workflow updated and nested `nativePlugin`/browser transport leakage now fails under `ConnectionEventQueue` tests.
- `Hypatia` (`019de19b-9371-7c21-8fe9-988106a95441`): P3-D-B code review. Findings: output chunks were dropped, reasoned terminal closes were overwritten to clean closed state, and mode was hardcoded to local. Result: fixed with output text state, transport-derived mode, failed close preservation, and regression tests.
- `Meitner` (`019de1a3-ebfb-7911-94ea-b61df8f3cab3`): P3-D-C code review. Findings: terminal inventory could silently re-scope terminals from another machine, and forbidden tgent session coverage did not directly prove `sessions` keys were rejected. Result: fixed with machine ownership validation plus direct `session`/`sessions`/`sessionId` inventory rejection tests.
- `Sartre` (`019de1ac-8634-7f21-934e-7b268a416f9f`): P3-D-D code review. Findings: file manager accepted mismatched machine transports, older directory responses could overwrite newer navigation, and workflow had stale pending text. Result: fixed with transport identity validation, stale-response sequence guards, and workflow cleanup.
- `Planck` (`019de1b1-9b5d-7d70-8e5c-4459c1362296`): P3-D-D follow-up code review. Findings: file manager boundary still allowed unscoped terminal use because `terminalId` was optional and missing connection terminal identity did not fail; stale workflow text still appeared in the reviewed snapshot. Result: fixed with required terminal IDs, strict `ConnectionInfo.terminalId` validation before file API use, and workflow cleanup.
- `Mencius` (`019de1b6-0a36-7980-87e8-d6ac6ee54388`): P3-D-D final quick code review after terminal scoping fixes. Findings: no remaining code issues; low stale workflow text still described completed terminal scoping work as pending. Result: workflow cleanup in this checkpoint.
- `Euclid` (`019de1be-2f52-7890-a1b7-d17460a573d4`): P3-E read-only explorer. Findings: local RTC currently accepts `api`, `terminal:{terminal_id}`, and `file:{transfer_id}` only; `events` is documented optional but current Go closes unknown labels; API channel requests are unchunked JSON but responses are `0xC0` chunked binary; current Go/tgent file API uses POST JSON bodies for list/stat despite docs GET draft.
- `Rawls` (`019de1c5-a582-71b1-b49a-aa07c33a53cc`): P3-E-A code review. Findings: app shell did not call transport connect, WebRTC offer must be generated after data channel creation, terminal channel framing is not yet Go binary protocol compatible, package exports were missing, and local RTC client metadata drifted from docs. Result: fixed all P3-E-A adapter/app-shell findings; binary terminal protocol adapter deferred to the next P3-E slice with explicit tests before browser smoke.
- `Pauli` (`019de1d4-739c-7611-bd1b-fdaea64cea32`): P3-E-B code review. Findings: late terminal subscribers were not forwarded to an already-created protocol bridge, and close/reopen reused a closed terminal channel/protocol cache. Result: fixed with dispatcher-based subscriber forwarding, close cache cleanup, and regression tests.
- `Aristotle` (`019de1db-33c2-7db3-a7c4-b08d69ba288e`): P3-E-B follow-up code review after subscriber/reopen fixes. Findings: pre-attach stream frames are dropped, data-channel close is not forwarded as a terminal closed event, workflow text needed refresh, and pre-offer channel creation ordering lacked regression coverage. Result: fixed with early stream-frame buffering, RTC close forwarding, workflow refresh, and pre-offer channel creation tests.
- `Ptolemy` (`019de1e7-d94a-7d43-8027-5c7a0f6ce4ed`): P3-E-B final quick code review after Aristotle fixes. Findings: browser Blob messages and connecting data-channel open timing were not handled, raw RTC close did not clear retry caches, and workflow subagent text was stale. Result: fixed with `binaryType='arraybuffer'`, Blob fallback decoding, wait-for-open hello gating, raw-close cache cleanup, and workflow refresh.
- `Einstein` (`019de1f4-37fa-74e0-bf48-cf3953e3f28e`): P3-E-B post-Ptolemy final review. Findings: raw close between `connect()` pre-created terminal channel and `openTerminal()` leaves a closed channel cached, and workflow next action is stale. Result: fixed with pre-protocol raw close cache cleanup, regression coverage, and workflow next-action refresh.
- `Feynman` (`019de20d-3153-7ac3-9b4f-fa0e3e2ceb10`): P3-E-C-A code review for local web shell build/embed, generated static assets, forbidden model/credential leakage, transport boundaries, and test coverage. Findings: low deterministic-sync issue where stale root-level generated static files could remain. Result: fixed with full static directory replacement and a script regression test.
- `Heisenberg` (`019de221-51de-7dd3-ba21-c487cb0d7977`): P3-E-C-B-A code review for browser-local app identity storage, app certificate storage, canonical local WebRTC offer signing, machine-private-key/TURN boundaries, and test coverage. Findings: medium app private key storage issue because the first draft exported and stored a private JWK in localStorage. Result: fixed with non-exportable WebCrypto private keys stored behind IndexedDB and regression tests proving string storage does not hold app/machine private keys.
- `Euler` (`019de238-47c6-7551-b63d-8f70081db280`): P3-E-C-B-B code review for pair harness reachability, embedded smoke fallback, security boundaries, and old public-model drift. Findings: high first-run missing-certificate error hid the pair panel and blocked pairing when terminals existed. Result: fixed with shell-level error rendering, pair-success transport retry, and regression coverage.
- `Darwin` (`019de23e-6733-7bf2-b9af-1d5051844a32`): P3-E-C-B-B follow-up code review after Euler fix. Scope: first-run pair reachability, retry after pairing, no workspace/tab/pane/session public UI, no TURN credentials, no machine private key exposure, app private key boundary, interface separation, and test coverage. Result: no findings.
- `Plato` (`019de241-ebd8-79a3-8852-385c75a97938`): P3-F read-only explorer for anonymous rendezvous HTTP handler placement, localweb error envelope conventions, endpoint shape, and gotchas around no TURN credentials/channel secret auth. Result: recommended `termx-core/internal/remote/rendezvous/http.go`, localweb-style error envelope, documented endpoints only, events header auth, offer/answer body secret, and deferring candidate endpoint/service shell until adapter needs it.
- `Mendel` (`019de247-0019-78c1-be99-3d7f5c5a8003`): P3-F code review for docs alignment, secret/auth handling, no TURN credentials, no terminal/file data relay, and test coverage. Findings: medium missing standalone ICE candidate HTTP endpoint. Result: fixed with `/candidate` endpoint and regression coverage.
- `Anscombe` (`019de24b-23b1-7a72-b8ed-1db7b6f8294f`): P3-F follow-up code review after candidate/envelope changes. Findings: high plaintext channel secret storage, medium `/answer` did not forward `app_certificate`/`signature`, and low `auth-and-pairing.md` omitted `/candidate`. Result: fixed with hashed channel secret verifier storage, answer envelope forwarding, docs update, and regression tests.
- `Hilbert` (`019de26c-732d-74a3-93d0-54b7b6bbdcb7`): P3-E-C-B-D code review for local WebRTC DataChannel open gating, regenerated embedded localweb static assets, boundary drift, no TURN credentials, no machine private key exposure, and test coverage. Findings: no issues. Residual risk: no automated in-app browser click smoke was available in this Codex session.
- `Gibbs` (`019de442-39a1-71e3-8d21-51662e5be368`): P3-E-C-B-I code review for the mobile terminal interaction shell, terminal-first layout, modifier/keybar flow, xterm keyboard offset, Tailwind-only styling boundary, generated localweb assets, and remote rebuild scope drift. Findings: xterm could be recreated by callback prop changes, and shared `FileManager` depended on caller-provided `relative` positioning. Result: fixed with callback refs in `Terminal`, a self-contained `FileManager` root, and regression tests.
- `Maxwell` (`019de4a3-d2f7-77d0-9ce6-c84cf4578b52`): P3-E-C-B-K code review for terminal resize ownership, size lock handling, mobile/local follower fit behavior, transport boundaries, and remote rebuild scope drift. Findings: high unscoped request-path `resize` bypassed owner attachment when no matching attachment existed. Result: fixed by requiring request-path resize owner attachment, including scoped no-attachment regression and existing request-path test updates.
- `Peirce` (`019de66e-8d86-7192-a315-dcbae118f4ec`): P3-E-C-B-L code review for xterm viewport delayed fit, resize ownership gating, generated localweb assets, and scope drift. Findings: core xterm fix was sound; medium scope issue that unrelated `LocalRemoteApp.tsx` changes would pollute generated assets. Result: regenerated assets from a clean worktree containing only the xterm fix.
- `Pasteur` (`019de772-b556-7f81-9507-c8fa318ec7a7`): P3-E-C-B-M code review for current frontend re-embed, mobile header/navigation adjustment, accessibility labels, generated assets, and remote rebuild boundaries. Findings: no issues; generated static assets match `remote-ui/dist`, command names are covered, and no transport/security/model drift was found.
- `Halley` (`019de798-a39e-72d2-881f-3af1b5cbda73`): P3-E-C-B-N code review for mobile terminal chrome height reclaim, terminal-list back button, files overlay behavior, keybar visibility, generated assets, and remote rebuild boundaries. Findings: no issues; confirmed terminal panel remains `flex-1`, files is an overlay, embedded assets match `remote-ui/dist`, and no transport/security/model drift was found.
- `Harvey` (`019de7ab-0a73-7233-82af-8f6a183d792b`): P3-E-C-B-O code review for the new local embedded terminal list page, delayed terminal connection, list/terminal navigation, generated assets, and remote rebuild boundaries. Finding: desktop terminal pages lacked a visible path back to the true terminal list because the only back button was mobile-only. Result: fixed with a desktop sidebar `Show terminal list` action and regression coverage.

## Code Review Log

- P2-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-B review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-C review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, or transport boundary drift introduced.
- P2-D review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay changes, app machine-private-key exposure, CLI internal-package import, or transport boundary drift introduced.
- P3-A review complete. No remaining findings after implemented fixes; no workspace/tab/pane concepts, anonymous/free TURN relay credentials, terminal/file data relay, app machine-private-key exposure, or transport boundary drift introduced.
- R1 planning review: self-checked updated docs for scope drift. Plan now prioritizes embedded local web, shared `remote-ui/` components, and local WebRTC-over-TCP before mobile app migration; no new workspace/tab/pane public model and no anonymous/free TURN relay entitlement were introduced.
- R2 planning review: self-checked updated docs for scope drift. Plan now requires tgent-aligned page/component structure where practical, plus TermX-owned native-like message reducer/event queue behavior; no new workspace/tab/pane public model and no anonymous/free TURN relay entitlement were introduced.
- P3-B review complete. No remaining blocker after implemented fixes; no workspace/tab/pane public model, anonymous/free TURN relay credentials, app machine-private-key exposure, or transport boundary leakage introduced. Residual risk is limited to the documented `last_active_at` placeholder and the P3-C absence of local RTC/ICE TCP data plane.
- P3-C review complete. No remaining blocker after implemented fixes; no workspace/tab/pane public model, anonymous/free/local TURN relay credentials, app machine-private-key exposure, transport boundary leakage, or separate agent binary was introduced. Residual risk: browser smoke with the embedded web UI is still deferred to P3-E because P3-C only proves local signaling, terminal protocol scoping, file/API channels, and server-side ICE TCP candidate generation.
- P3-D-A review complete. No remaining blocker after implemented fixes; no workspace/tab/pane/window/session public model was introduced in `remote-ui`, signaling `sessionId` remains limited to local RTC interfaces, no anonymous/free/local TURN relay credentials or machine private key exposure were introduced, and business logic remains behind transport interfaces without direct browser/native implementation imports. Residual risk: React components, browser local adapter, terminal/file UI behavior, and browser smoke remain deferred to later P3-D/P3-E todos.
- P3-D-B review complete. No remaining blocker after implemented fixes; `Terminal.tsx`, `TerminalClient`, and `useTerminalSession` expose `machineId`/`terminalId` and `terminal:{terminal_id}` only, do not import browser/native transport implementations, do not expose machine private key material, and do not introduce TURN relay credentials. Residual risk: this is a minimal terminal boundary with plain text rendering; full xterm rendering, browser local adapter wiring, and browser smoke remain deferred to later P3-D/P3-E todos.
- P3-D-C review complete. No remaining blocker after implemented fixes; `TerminalList.tsx` and terminal inventory expose machine/terminal semantics only, reject tgent session/window/pane-shaped records, validate terminal machine ownership, do not import browser/native transport implementations, do not expose machine private key material, and do not introduce TURN relay credentials. Residual risk: list styling/actions are minimal and richer mobile-native interactions remain for later UI polish after local embedded web wiring.
- P3-D-D review complete after fixes. `FileManager.tsx`, `useFileManager`, and `fileApi` remain behind transport interfaces; `machineId + terminalId` is required and verified before file API use; no workspace/tab/pane/window/session public model, TURN relay credentials, machine private key exposure, or direct browser/native transport implementation was introduced. Residual risk: UI is intentionally minimal until P3-E browser wiring and later mobile-native polish.
- P3-E-B review complete after fixes. The browser terminal adapter now speaks the current Go binary protocol over `terminal:{terminal_id}`, buffers pre-attach frames, forwards late subscribers and close events, waits for RTC channel open, handles browser Blob messages, creates pre-offer terminal/API channels, and recreates terminal channels after close/retry. No workspace/tab/pane public model, TURN relay credentials, machine private key exposure, or UI/business browser transport leakage was introduced. Residual risk: rich `TypeScreenUpdate`/`TypeSyncLost`/`TypeBootstrapDone` rendering and real browser smoke are deferred to P3-E-C and later terminal rendering polish.
- P3-E-C-A review complete after deterministic sync fix. The embedded shell build path stays aligned with `docs/remote-rebuild`: static assets are generated from shared `remote-ui`, embedded into `termx-core`, public UI remains `machine -> terminal`, local flow receives no TURN credentials, browser code never touches the machine private key, and fetch/WebRTC details remain in adapter/entry wiring. Residual risk: real browser terminal/file smoke is still deferred to P3-E-C-B because browser-local app certificate storage and offer signing are not implemented yet.
- P3-E-C-B-A review complete after non-exportable app key storage fix. Browser-local app identity now stores only app metadata and app certificates in string storage, keeps app private keys behind an IndexedDB/WebCrypto key-store boundary, signs local offers with the Go-compatible canonical message, does not expose machine private key material, and does not introduce TURN credentials or workspace/tab/pane public concepts. Residual risk: pair UI/harness and real browser terminal/file smoke remain for P3-E-C-B-B.
- P3-E-C-B-B review complete after first-run reachability fix and follow-up review. The pair harness remains rendered through `LocalAgentApi` and app identity interfaces, missing-certificate errors no longer hide pairing, successful pairing retries the local transport, executable smoke covers embedded shell/pair markers plus no TURN or machine-private-key asset exposure, and no follow-up findings remain.
- P3-F review complete after candidate endpoint, envelope-forwarding, hashed-secret, and relay-candidate rejection fixes. Current HTTP adapter reuses the anonymous rendezvous store, forwards only lightweight signaling/certificate/signature envelopes needed by the daemon, stores only hashed channel-secret verifier material, and does not add TURN credentials, terminal/file payload forwarding, app certificate business authorization, machine private key exposure, or workspace/tab/pane public concepts.
- P3-E-C-B-D review complete. Local browser API requests and file transfers now wait for their RTC DataChannels to open inside the adapter boundary; UI/business components still only see `PeerTransport`, `JsonRpcChannel`, and `BinaryChannel`; no workspace/tab/pane/session public concepts, TURN relay credentials, or machine private key exposure were introduced. Residual risk: real in-app browser click automation remains deferred until the Browser Use Node REPL `js` tool is available.
- P3-E-C-B-H review complete after staged asset fix. Embedded local web terminal rendering now uses xterm.js behind `TerminalTransport`, TailwindCSS is wired as the frontend styling system, generated localweb assets are tracked, and real Chrome CDP smoke proved pair -> WebRTC -> xterm input/output -> file list on the rebuilt daemon. No workspace/tab/pane public model, TURN relay credentials, machine private key exposure, or UI/browser transport boundary leak was introduced. Residual risk: richer tgent mobile-native terminal interactions such as custom selection/keyboard composition/search remain future UI polish.
- P3-E-C-B-I review complete after Gibbs fixes. The embedded local web shell is now terminal-first on mobile, keeps terminal/file/pair interactions behind existing interfaces, persists the xterm instance across modifier/callback state changes, and makes `FileManager` layout self-contained. No workspace/tab/pane public model, TURN relay credentials, machine private key exposure, or UI/business transport boundary leak was introduced. Residual risk: advanced mobile-native terminal gestures such as selection handles, predictive keyboard accessory behavior, search, and alternate-screen-specific controls remain future UI polish before app migration.
- P3-E-C-B-K review complete after Maxwell fix. Remote attach now carries resize ownership metadata, embedded/mobile local web defaults to follower local-fit behavior, raw and request-path resize require owner control, observers and size-locked terminals cannot resize, and generated localweb assets match the shared UI source. No workspace/tab/pane public model, TURN relay credentials, machine private key exposure, or UI/browser transport boundary leak was introduced. Residual risk: explicit owner-acquire/release UI is still future work; current local/mobile path conservatively defaults to follower.
- P3-E-C-B-O review complete after Harvey fix. Embedded local web now opens to a true terminal list page, does not create a local transport until a terminal item is selected, disconnects when returning to the list, supports both mobile and desktop return-to-list controls, and keeps Pair on the current page. No workspace/tab/pane/session public model, TURN relay credentials, machine private key exposure, or UI/browser transport boundary leak was introduced. Residual risk: a real-browser click smoke would still be useful for responsive Tailwind visibility, but unit tests now cover both named return controls and generated assets are synchronized.

## Deferred Human Decisions And Placeholders

- Public rendezvous deployment, DNS, TLS certificates, billing/subscription provider, mobile signing, and app store configuration remain deferred by policy.
- `termx-core/remote_localweb.go` currently maps `last_active_at` from terminal creation time because the existing terminal inventory does not expose a separate last-activity timestamp. The UI now labels that field as `Created` instead of implying activity; replace it with real activity metadata when the terminal runtime publishes it.
- Local HTTP and ICE TCP currently use independent listeners. Same-port cmux remains deferred until browser smoke/local e2e proves that reducing exposed ports is worth the extra listener complexity.
- P3-E-B local terminal bridge now implements the minimum Go binary terminal protocol path over `terminal:{terminal_id}`. Richer incoming stream-frame rendering such as `TypeScreenUpdate`, `TypeSyncLost`, and `TypeBootstrapDone` remain for later terminal-rendering polish.
- P3-E-C-B-B in-app Browser Use click smoke is deferred by current tool availability: the Browser plugin is installed, but the required Node REPL `js` tool is not exposed in this session. Current fallback smoke is executable HTTP/embedded-asset coverage in `termx-cli`; P3-E-C-B-C should run in-app browser automation when the Node REPL browser tool is available.
- Managed `termx remote enable` remains a narrow placeholder for future authenticated control-plane work. Current command output explicitly points users to `termx remote enable --local-only`; no TURN relay credentials are issued on this path.

## Risks

- Existing baseline uses `DeviceID` terminology while remote rebuild docs require public `machine -> terminal` object language. The implementation should preserve compatibility where needed but introduce machine-key/certificate concepts without exposing workspace/tab/pane.
- Existing hub baseline may include relay fields. P3 anonymous paths must explicitly reject or omit TermX TURN relay credentials.
- New `remote-ui/` package must avoid carrying over tgent pane/session public concepts when copying `Terminal.tsx`, `SessionList.tsx`, and file manager code.
- Keeping TermX UI close enough to tgent for future synchronization conflicts with replacing tgent's web-like interaction state. The boundary is explicit: copy structure/components/adapters where possible, but normalize messages and lifecycle through TermX reducers/queues.
- P3-E browser adapter currently translates shared `GET /files/list` and `GET /files/stat` requests to the existing Go/tgent-style `POST` data-channel file API. Browser smoke in P3-E-C-B must validate that this adapter behavior works against the embedded daemon.
- P3-E-C-B-K keeps local/mobile views from stealing daemon PTY size, but explicit resize owner acquisition/release UI remains a future product slice. Until that exists, embedded local web requests follower resize policy by default.
- P3-E-C-B-O covers local embedded web list-first navigation with unit tests and embedded asset tests, but not a live browser viewport smoke for desktop/mobile Tailwind media visibility.
- P3-E-C-B-P now proves machine-level Files persistence with both mock and real FileManager flows, but still relies on a hidden terminal-scoped transport internally until a future machine-scoped file channel exists.

## Next Exact Action

1. Commit the Slice 3 workflow hash update.
2. Start Slice 4: write failing registered `public_p2p` rendezvous tests for auth, TTL, payload limit, rate limit, STUN-only ICE config, no TURN for free users, and unauthenticated reject.
