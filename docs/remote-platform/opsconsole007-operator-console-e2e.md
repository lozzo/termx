# OPSCONSOLE007 运营管理后台产品化证据

## 1. 状态

- 当前状态：进行中。
- 产品与交互基线：`docs/remote-platform/operator-console-product-design.md`。
- 本地 Web、Controller 静态深链接和模拟 operator API 已通过；公网 Controller/Web 与双 Edge 重启恢复尚未验收，因此当前不能声明上线完成。

## 2. 用户可观察结果

- 账号中心只保留一个“进入运营管理”入口，不再展示九个指向同一页面的 hash 链接。
- `/operator` 根据后端 `GetOperatorWorkspaceResponse.modules` 规范跳转到第一个获准模块。
- 九个一级模块使用真实路径：
  - `/operator/users`
  - `/operator/agents`
  - `/operator/orders`
  - `/operator/subscriptions`
  - `/operator/plans`
  - `/operator/privileges`
  - `/operator/promotions`
  - `/operator/hubs`
  - `/operator/releases`
- 详情路径覆盖账号、daemon、订单、订阅、catalog、特权账号、优惠码、Hub 和 release；catalog 历史使用 `/operator/catalog/:version`。
- 桌面端左侧管理导航持续存在；移动端使用抽屉，支持遮罩、`Esc`、背景滚动锁定和 `aria-expanded`。
- 运营工作台首次进入默认简体中文，并继续使用独立语言偏好；普通账号语言偏好不受影响。

## 3. 数据边界

- Workspace 权限投影仍是导航可见性的唯一真值，前端没有新增角色或 `isAdmin` 状态。
- 首次只请求当前模块；用户、Agent、订单、订阅、套餐、特权、优惠码、Hub 和版本不再通过一个 `Promise.all` 全量加载。
- 已访问模块保留内存投影，切回时立即显示并在后台刷新，不显示全页 loading。
- mutation、刷新和搜索只重新请求当前模块；账号详情只在特权模块请求 entitlement override。
- 所有业务请求继续使用 generated Proto schema 和现有 `/api/v1/operator/*` API；未新增 Web 私有业务 DTO。

## 4. 九模块闭环

- 用户：搜索、订阅状态筛选、账号详情、账号会话撤销、设备撤销、命令与审计。
- Agent：搜索、freshness 筛选、fresh/stale 显示、Kick、迁移、撤销和 CommandOutbox 四阶段。
- 订单：账号、状态和 provider 筛选；经济快照、payment attempt、事件时间线、人工支付、退款、撤销和 Creem reconciliation。
- 订阅：状态筛选、独立详情、暂停/恢复、赠送/延期/变更套餐和 adjustment 历史。
- 套餐：不可变历史、结构化目录版本/名称/价格/设备/P2P/Relay 字段；完整 Proto JSON 只在高级区；发布仍提交同一 `PlanCatalogContract`。
- 用户特权：账号详情、类型化 override、有效期、状态和带原因撤销。
- 优惠码：创建、经济快照、redemption 列表和带原因停用。
- Hub：创建、编辑、身份批准、drain、迁移、disable/archive、持久 lifecycle 与运行时 readiness 分离显示。
- 版本：签名制品发布、activate、pause/resume、显式 rollback 和审计。

## 5. 本地准入证据

在 `private/cloud/web-controller/web/`：

```bash
npm run typecheck
npm run build
npx playwright test \
  e2e/opsconsole001.spec.ts \
  e2e/opsauth001.spec.ts \
  e2e/opsuser001.spec.ts \
  e2e/opshub001.spec.ts \
  e2e/opsrel001.spec.ts \
  e2e/creem001.spec.ts
```

结果：TypeScript 与 Vite production build 通过；13 条 Playwright 用例通过。

在仓库根目录：

```bash
go test ./private/cloud/controller
git diff --check
```

结果：Controller 测试通过；新增静态资源测试证明 operator 一级与详情深链接均回退到同一 SPA；差异格式检查通过。

## 6. 最终收口待办

- 在真实 PostgreSQL、Controller 和双 Edge 装配上从 Web UI 运行九模块权限、近期认证、mutation、审计和重启恢复矩阵。
- 记录部署提交、Controller/Web 构建身份、线上 URL 和回滚点。
- 公网复测通过后更新本文件与 `workflow.md`，再把 `OPSCONSOLE007` 标记完成。
