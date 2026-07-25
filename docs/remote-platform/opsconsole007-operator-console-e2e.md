# OPSCONSOLE007 运营管理后台产品化证据

## 1. 状态

- 当前状态：已完成并部署。
- 产品与交互基线：`docs/remote-platform/operator-console-product-design.md`。
- 本地 Web、Controller 静态深链接和模拟 operator API 已通过；公网 Controller/Web 已部署，US/CN 双 Edge 已依次重启并恢复健康。

## 2. 用户可观察结果

- 登录后账号项与获准运营模块共用唯一左侧栏；运营人员点击模块后只切换右侧内容，不再经过“进入运营管理”的独立全屏页面。
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
- 桌面端统一左栏同时展示账号项和管理项；移动端使用同一抽屉，支持遮罩、`Esc`、背景滚动锁定和 `aria-expanded`。
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

结果：TypeScript 与 Vite production build 通过；新增 768px/150% 缩放准入后，14 条 Playwright 用例通过。

在仓库根目录：

```bash
go test ./private/cloud/controller
git diff --check
```

结果：Controller 测试通过；新增静态资源测试证明 operator 一级与详情深链接均回退到同一 SPA；差异格式检查通过。

## 6. 公网部署

- Git 提交：`0f19d846`，已推送到 `origin/master`。
- bundle SHA-256：`98190f43cc797cc8d6b67b8add5d1a0b56ae7b38a31401361f0afcf98261ee41`。
- Controller SHA-256：`63cc5998f484e0c750cbc5d8814e8f118efdcdeb2b572ddc4f084d77d63469da`。
- Web `index.html` SHA-256：`f0ebe5e44539f2fb566e8ce190aacb76baa0b6eb79939aa0265f059d39de640e`；主 JS SHA-256：`214c3ca67de4db6766f1f8cdfdfea1fa5bd9e6e299e89691750ac44b61b906f9`。
- 线上入口：`https://muxvia.com/operator/users`；九个一级路由全部返回 200，线上主 JS 与本地构建摘要一致。
- Controller 与 US Edge 公网 health 均为 204；CN Edge 按当前 deployment truth `https://muxvia-cn1.omscd.com:41102/healthz` 返回 204。旧文档中的 `cn1.edge.muxvia.com` 不是当前 Edge 配置真值。
- US/CN Edge 使用原二进制依次重启，均先经历短暂未就绪，再恢复本机及公网 health；Controller、PostgreSQL、Hub projection 和运行时 owner 未改变。
- 回滚目录：`/opt/muxvia/rollback/pre-opsconsole-0f19d846-20260725-230947/`。
- 九模块真实 PostgreSQL、Controller、双 Edge mutation、审计与重启业务矩阵沿用 `OPSE2E001` 已通过证据；本切片新增测试证明重构后的真实路由、请求隔离、近期认证、响应式和同一 generated API consumer，不通过重复创建线上套餐、Hub 或签名版本冒充新能力。

## 7. 统一壳修正

- 用户反馈证明“账号页单一入口 + 独立运营壳”不符合真实操作习惯；`OPSCONSOLE008` 将登录后页面收敛为唯一 `ConsolePage`。
- `AccountPage` 与 `OperatorPage` 已删除各自的侧栏和壳层职责，只渲染右侧业务内容；Workspace、语言域切换、移动抽屉、退出和 URL 导航只保留一个 owner。
- 从账号概览点击左侧“用户管理”直接进入 `/operator/users`，浏览器不整页重载；九个模块继续只加载当前模块，开发环境严格挂载下的同模块并发请求也已去重。
- Web typecheck/build、Controller 测试和 14 条运营后台 Playwright 回归通过；1440px 截图确认统一左栏，390px 抽屉与无横向溢出通过。
