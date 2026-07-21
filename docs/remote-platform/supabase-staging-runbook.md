# Supabase PostgreSQL Staging Runbook

## 范围

本手册只部署 Muxvia Controller 的标准 PostgreSQL 持久化。Supabase 不拥有 Account、Session、Subscription、HubAssignment、CommandOutbox、Relay quota 或 UsageLedger 业务，不启用 Supabase Auth、Realtime、PostgREST 或 Edge Functions。

生产使用 Supabase Pro；Free project 只用于短期 staging 验证。Controller 与数据库选择同一或相邻区域，避免 control/usage 事务跨洲。

## 连接方式

长驻 `muxvia-cloud-controller` 按以下顺序选择连接：

1. Controller 网络支持 IPv6：使用 Supabase direct connection `db.<project>.supabase.co:5432`。
2. Controller 只有 IPv4：使用 Supavisor session mode `<region>.pooler.supabase.com:5432`。
3. 禁止使用 transaction mode `:6543`，因为当前 Controller 使用长驻连接、事务和 prepared query 行为。

远程 DSN 必须显式包含 `sslmode=require`、`verify-ca` 或 `verify-full`。`postgres.ValidateDSN` 会拒绝远程明文、缺少 TLS 和 keyword DSN。完整 DSN 只能进入 0600 Controller config 或部署 secret，不得写入 manifest、日志或浏览器配置。

官方参考：[Supabase database connections](https://supabase.com/docs/guides/database/connecting-to-postgres)、[Supabase pricing](https://supabase.com/pricing)、[Supabase backups](https://supabase.com/docs/guides/platform/backups)。

## 所需 Secret

Supabase staging：

```text
MUXVIA_SUPABASE_POSTGRES_DSN
```

异地 R2 备份：

```text
MUXVIA_BACKUP_AGE_RECIPIENT
MUXVIA_BACKUP_AGE_IDENTITY
MUXVIA_R2_BUCKET
MUXVIA_R2_ENDPOINT_URL
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
```

数据库密码、age identity 和 R2 secret 不提交仓库，不进入 Controller manifest，不放入 Web/Android 构建变量。

## Staging 验收

使用专用 Supabase staging project。devcloud 会基于 artifact 目录创建并重建一个 `muxvia_dev_*` schema，不接触其它 schema：

```bash
export MUXVIA_DEV_POSTGRES_DSN="$MUXVIA_SUPABASE_POSTGRES_DSN"
make cloud-dev
```

验收必须记录：

- Controller 启动并自动应用 `muxvia_schema_migrations`；
- manifest 只有 `database_engine=postgresql`，不含 host、user、password 或 DSN；
- 一个 Controller、两个 Edge 完成 full projection 和独立 generation；
- 注册、登录、refresh、交易、assignment、CommandOutbox、Relay reservation、usage settlement 成功；
- Controller 重启后账号、assignment、command、quota 和 usage 保持；
- 数据库暂时不可用时请求 fail closed，恢复后不产生重复 journal、双 reservation 或 sequence 回退；
- Android managed P2P/Relay E2E 由后续 `CLOUDP007` 继续完成。

## 备份

创建独立 age identity，并把 identity 存入离线 secret manager；Controller 主机只需要 recipient：

```bash
age-keygen -o muxvia-controller-backup.agekey
age-keygen -y muxvia-controller-backup.agekey
```

创建加密备份：

```bash
export MUXVIA_CONTROLLER_POSTGRES_DSN="$MUXVIA_SUPABASE_POSTGRES_DSN"
export MUXVIA_BACKUP_AGE_RECIPIENT='age1...'
export MUXVIA_R2_BUCKET='muxvia-controller-backups'
export MUXVIA_R2_ENDPOINT_URL='https://<account-id>.r2.cloudflarestorage.com'
export AWS_ACCESS_KEY_ID='...'
export AWS_SECRET_ACCESS_KEY='...'
scripts/backup-controller-postgres.sh
```

脚本执行 schema-scoped custom-format `pg_dump`、SHA-256 manifest、age 加密和可选 R2 上传。Supabase 的每日备份不能替代该异地备份。

## 恢复

恢复演练必须指向独立空数据库或独立 Supabase project，禁止直接覆盖在线生产库：

```bash
export MUXVIA_RESTORE_POSTGRES_DSN='postgresql://.../postgres?sslmode=require'
export MUXVIA_BACKUP_AGE_IDENTITY='/secure/path/muxvia-controller-backup.agekey'
scripts/restore-controller-postgres.sh controller-backup.tar.age
```

恢复后至少核对 schema migration、账号数、未完成 CommandOutbox、active reservation、usage sequence head 和 Hub assignment。仓库本地门禁为：

```bash
make test-postgres-backup
```

该门禁已经证明 dump 校验、age 解密、`pg_restore` 和数据核对链路；云端 R2 上传和 Supabase 恢复仍必须使用真实凭据验收。
