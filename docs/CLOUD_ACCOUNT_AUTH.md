# Cloud 账号认证

Cloud 控制台使用短期 Access JWT 和持久化 Refresh Token，不创建服务端 Access Session。

## 凭据

| 凭据 | 有效期 | 保存位置 | 用途 |
| --- | --- | --- | --- |
| Access JWT | 15 分钟 | 浏览器 HttpOnly Cookie | Controller 使用 Ed25519 本地验签，不查询数据库 |
| Refresh Token | 30 天 | 浏览器 HttpOnly Cookie；数据库只存 SHA-256 摘要 | 轮换并签发新的 Access JWT |
| CSRF Token | 随 Refresh Token | 浏览器可读 Cookie；数据库和 JWT 只存 SHA-256 摘要 | 保护 Cookie 认证的写请求 |

Access JWT 包含账号 ID、账号状态、账号修订号、角色、Refresh ID、最近认证截止时间和 CSRF 摘要。JWT 使用独立的 Ed25519 密钥签名，并校验固定的 issuer、audience、key ID、签名算法、签发时间和过期时间。

Refresh Token 每次续期都会轮换：旧记录在同一数据库事务中撤销，新记录随后生效。数据库不保存 Access JWT，也不会在普通 API 请求中查询 Refresh Token。

## 撤销语义

退出、修改密码、禁用账号、重置账号和撤销持久登录凭据都会阻止对应 Refresh Token 再次续期。已经签发的 Access JWT 不查数据库，因此仍可使用到自身过期，最长 15 分钟。这是无服务端 Access Session 的明确安全边界。

需要缩短撤销窗口时应调低 Access JWT 有效期；不要在普通 API 请求中重新引入数据库查询。高风险操作继续要求最近十分钟内重新验证密码。

## 浏览器流程

1. 登录成功后，Controller 设置 Access、Refresh 和 CSRF 三个 Secure、SameSite=Strict Cookie。
2. API 返回 401 时，Web 客户端只合并发起一次 `/api/account/refresh` 请求。
3. Controller 校验并轮换 Refresh Token，重新设置三个 Cookie。
4. Web 客户端重试原 API 请求；Refresh 失败则回到登录页。

Refresh Cookie 的 Path 仅允许 `/api/account/refresh`，Access 与 Refresh Cookie 均不可由 JavaScript 读取。响应正文只返回凭据 ID 和过期时间，不返回原始 Token。

## 密钥与迁移

Controller 使用 `/etc/anytty/cloud/pki/account-token-signing-key.pem`，key ID 来自 `ANYTTY_CLOUD_ACCOUNT_TOKEN_KEY_ID`。该密钥不得复用 Edge 配置、artifact 或 daemon binding 的签名密钥。

Schema 10 将 `account_sessions` 改为 `account_refresh_tokens`，删除 Access Token 摘要和 Access 过期时间，保留现有 Refresh Token 摘要。升级后旧 Access Cookie 会失效，但现有 Refresh Cookie 可以轮换为 JWT。
