# Controller PostgreSQL 与 Supabase 迁移

## 决策

- Muxvia Controller 的正式持久化契约是标准 PostgreSQL。
- Supabase 是首个生产 PostgreSQL 托管商，不是产品领域服务。
- Controller 使用 Go `pgx` 和 PostgreSQL wire protocol；不使用 Supabase Auth、Realtime、PostgREST、Edge Functions 或浏览器直连数据库。
- 当前只实现单区域单写 Controller，不建设多区域数据库、读写分离或供应商切换框架。
- PostgreSQL 切换完成后删除 SQLite runtime、driver、schema、配置和 fallback，不保留生产双写。

## 领域边界

领域 service 继续依赖各自的最小 Store port：

| 领域 | 持久真值 | 必须保留的事务语义 |
| --- | --- | --- |
| commerce | Account、Session、Order、PaymentAttempt、PaymentEvent、Subscription、Entitlement、Audit | provider event journal 与状态转换原子提交；revision/CAS；精确 replay |
| hubregistry | deployment identity、control generation、HubAssignment | generation 单调递增；assignment epoch 与 fence 原子切换 |
| topology | device ownership、最后可信 Presence/session/access projection | full replacement；旧 generation/revision 不覆盖新值 |
| CommandOutbox | parent、child、result journal、authority revoke | result journal 与 projection CAS 同事务；ACK 只能在提交后返回 |
| Relay quota | billing period、reservation | 过期清理、并发/额度判断和 reservation 写入同事务 |
| UsageLedger | signed event journal、sequence、period/session aggregate、settlement | journal、sequence、reservation、period 和 aggregate 全部提交后 ACK |
| Hub/Relay control | receive cursor、policy projection head | sequence/digest replay fencing；revision 由数据库串行分配 |

`control-plane/persistence.Store` 只供 Controller composition root 聚合这些端口。领域 service、HTTP handler 和 Web Controller 不得依赖 PostgreSQL row、`pgx.Tx` 或 concrete adapter。

## 迁移顺序

### PG001：契约和 schema

1. 保留现有领域 Store port，补齐 usage、projection 和 composition contract。
2. 将 Controller handler 从 `*sqlite.Store` 改为最小接口。
3. 建立 `postgres/migrations/0001_controller.sql`。
4. 静态门禁确保 schema 覆盖全部 Controller 持久真值且不含 SQLite 方言。

### PG002：PostgreSQL adapter

1. 使用 `pgxpool` 管理长驻 Controller 连接。
2. 启动时在 advisory lock 内执行 versioned migration。
3. 按领域实现 SQL 和 row/proto mapping，不抽取 ORM 或通用 repository。
4. PostgreSQL contract harness 覆盖原子回滚、唯一约束、CAS、并发 reservation、usage sequence 和重启恢复。

关键事务伪代码：

```text
BEGIN
  SELECT current row FOR UPDATE
  validate revision / generation / sequence / quota
  INSERT immutable journal row ON CONFLICT ...
  UPDATE projection WHERE revision = expected_revision
  verify exactly one row changed
COMMIT
return ACK
```

任何 validation、affected-row 或 commit 失败都返回领域错误；不得 fallback 到 SQLite、内存状态或第二次无条件更新。

### PG003：运行时切换

1. Controller 配置从 `database_path` 改为 PostgreSQL DSN secret reference。
2. controller、devcloud、Web 和测试装配统一打开 PostgreSQL adapter。
3. CI 使用临时 PostgreSQL；本地开发使用独立 development database。
4. 删除 SQLite adapter、driver、bootstrap 和数据库路径 manifest 字段。

当前状态：已完成。Controller、devcloud、Web 和全部 private Cloud 测试统一使用 PostgreSQL；SQLite package 与依赖已删除。

### PG004：Supabase staging

1. 在与 Controller 相同或邻近区域创建 Supabase PostgreSQL project。
2. 长驻 Controller 优先使用 TLS direct connection；IPv4-only 环境使用 Supavisor session mode。
3. Free project 只用于 staging；生产使用 Pro，避免空闲暂停并启用每日备份。
4. 使用真实 Controller、两个 Edge 验证 commerce、assignment、command、quota、usage 和重启恢复。
5. 定期执行 `pg_dump`，加密后写入独立 R2/对象存储，并从备份恢复到空数据库完成校验。

当前状态：远程 TLS DSN 门禁、加密备份/恢复脚本和本地恢复演练已完成；真实 Supabase/R2 验收等待项目凭据。具体操作见 `supabase-staging-runbook.md`。

## 禁止项

- 不把 Supabase Auth 作为第二套 Account/Session truth。
- 不让 App、Browser、Hub 或 Relay 直接访问 PostgreSQL。
- 不使用 PostgREST 绕过 Controller API Layer。
- 不双写 SQLite/PostgreSQL，不在数据库失败时 fallback 到本地文件。
- 不为了未来供应商或多区域提前建设通用数据库平台。
