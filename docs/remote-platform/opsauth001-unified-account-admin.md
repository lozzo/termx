# OPSAUTH001 统一账号管理员授权证据

## 完成结论

运营后台不再拥有独立登录系统。普通账号 HttpOnly Session 是 Web 唯一登录态；PostgreSQL `commerce_accounts.operator_role` 保存 `none/readonly/admin`，后端在每个运营请求上读取当前角色。账号与登录 Proto、前端状态和 workspace 响应均不包含角色、`isAdmin` 或 `isUser`。

账号页在普通导航下按后端返回的 `OperatorWorkspaceModule` 展示用户、订单、订阅、套餐、Hub、Agent、版本、优惠码和用户特权九个管理入口。普通账号访问 workspace 返回 403；readonly 可以查询但不能 mutation；admin 写操作还需要同源 CSRF 和五分钟内的当前账号密码确认。降级数据库角色后，既有 Session 的下一次请求立即失效。

旧 `OperatorLoginRequest/Response`、部署 access token、独立 operator session/CSRF Cookie、登录/退出 API、登录表单和 development token credentials 已删除。通用 `muxvia_cloud_recent` 只表示五分钟密码确认，不是第二份登录态。

## 准入证据

| 门禁 | 结果 |
| --- | --- |
| Proto generated/descriptor | `./scripts/check-generated-code.sh` 与 `go test ./proto/cloudpb -count=1` PASS |
| PostgreSQL 角色与 HTTP 权限矩阵 | `scripts/with-test-postgres.sh go test ./private/cloud/web-controller ./private/cloud/control-plane/commerce ./private/cloud/control-plane/postgres -count=1` PASS |
| 全量私有 Cloud | `make test-private` PASS |
| Web 类型与生产构建 | `npm run typecheck`、`npm run build` PASS |
| 账号导航与响应式 | `npx playwright test e2e/opsauth001.spec.ts`，普通用户 403、管理员九菜单、密码确认、390/1440 PASS |
| 九模块真实纵向 | `TestOPSE2E001NineModuleOperatorWorkflow`，普通登录、PostgreSQL、Controller、双 Edge、九模块 mutation 和重启恢复 PASS |

## 公网部署

- 2026-07-25 已部署到 `155.94.155.192` 的 Controller/Web；Controller SHA-256 为 `90ee7c6b3eae7fa3c6eefe248e5e7de46ed8598f717eec1f4a792bf30c756b42`。
- `0007_account_operator_role.sql` 已执行，唯一现有账号 `wuyouget@gmail.com` 被精确授予 admin；一次性迁移工具执行后已删除。
- `controller-config.json` 和 `credentials.json` 中的旧 Operator token/id/role 字段已删除；旧 `POST /api/v1/operator/login` 返回 404，未登录 workspace 返回 401，新 Web 资产为 `assets/index-CW4nDSv1.js`。
- 原配置、二进制与 Web 备份位于 `/opt/muxvia/backups/opsauth001-20260725081235`。
- 服务器保存的 bootstrap 密码已不匹配当前账号密码，因此没有擅自重置密码；最终公网人工验证使用用户当前密码登录 `https://muxvia.com/login`，随后账号侧栏应出现九个管理入口。

公网直连检查还观察到 `muxvia.com` 偶发返回证书 SAN 不匹配；本切片通过服务器本机 Nginx/Controller 链路验证应用路由，这个 TLS/DNS 现象不属于账号授权实现，需在独立基础设施问题中处理。
