# `web-control/` Agent Notes

## 定位

- Web Controller 是管理面服务（Next.js），不做 runtime 代理，不做连接时认证。
- 职责：用户登录、hub 目录（discover/heartbeat）、机器列表（agent 注册/在线状态）、connection ticket 颁发、订阅/踢下线控制。
- 不得承担：offer/answer 转发、app cert 验证、terminal/file/events 代理。

## Current P0 Task（WF-502）

**目标**：部署配置就绪，能按 `.env.example` 一键启动。

**新增文件**：`web-control/.env.example`

```
# JWT 签名密钥（生产环境必须替换为随机强密钥，至少 32 字节）
JWT_SECRET=termx-development-jwt-secret-change-me

# Hub 与 Web Controller 之间的共享密钥（必须与 termx-hub 的 TERMX_HUB_CONTROL_SECRET 一致）
HUB_SECRET=termx-development-hub-secret-change-me

# SQLite 数据库路径（默认 ./data/termx-web.sqlite，目录需存在或可创建）
# SQLITE_PATH=./data/termx-web.sqlite

# Web Controller 对外地址（用于 hub 注册响应中的 control_url 字段）
# APP_URL=https://control.example.com

# GitHub OAuth（可选，不填时禁用 GitHub 登录）
# GITHUB_CLIENT_ID=
# GITHUB_CLIENT_SECRET=
```

**无需 DB migrate 命令**：SQLite schema 在首次访问时通过 `ensureSqliteSchema` 自动创建，直接 `npm run dev` 即可。

**最小启动步骤**：
```bash
cd web-control
cp .env.example .env
# 按需修改 .env（开发环境默认值可直接用）
npm install
npm run dev
# 访问 http://localhost:3000/api/health → {"status":"ok"}
# 访问 http://localhost:3000/register → 注册第一个用户
```

**验证**：
- `GET /api/health` → `{"status":"ok"}`
- `POST /api/auth/register` 能注册新用户
- Hub heartbeat `POST /api/internal/hubs/heartbeat`（带 `X-TermX-Hub-Secret: <HUB_SECRET>`）返回 200

## Architecture Rules

- Web Controller **不做**：连接时 cert 验证、offer/answer 审核、runtime 代理、hub 信令转发。
- Hub heartbeat 接收端点（`/api/internal/hubs/heartbeat`）只同步 hub/agent 在线状态，返回 `kick_agents`；不参与单次连接流程。
- connection ticket（`/api/v1/machines/{id}/connect-tickets`）只在机器 online + 有关联 hub 时颁发；ticket 内容对 hub 不透明（hub 不解码验证）。

## Key Env Vars

| 变量 | 必需 | 默认值（dev）| 说明 |
|------|------|-------------|------|
| `JWT_SECRET` | 是 | dev fallback | JWT 签名密钥 |
| `HUB_SECRET` | 是 | dev fallback | Hub 共享密钥 |
| `SQLITE_PATH` | 否 | `./data/termx-web.sqlite` | SQLite 路径 |
| `APP_URL` | 否 | — | 对外地址，影响 control_url 返回值 |
| `GITHUB_CLIENT_ID` | 否 | — | GitHub OAuth |
| `GITHUB_CLIENT_SECRET` | 否 | — | GitHub OAuth |

## Workflow

- 遵守根 `AGENTS.md` 与根 `workflow.md`。
- WF-502 完成标准：`.env.example` 文件存在且内容正确，按说明能启动并通过上述验证。
