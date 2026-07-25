# OPSCONSOLE008 登录后统一控制台壳证据

## 状态

- 当前状态：实现与本地准入通过，待公网部署复核。
- 触发原因：账号中心只提供单一运营入口、`/operator/*` 再渲染独立全屏壳，运营人员无法在登录后的左栏直接切换管理项。

## 结构收口

- `ConsolePage` 是登录后唯一壳层，拥有品牌头、账号导航、Workspace 模块可见性、移动抽屉、语言偏好切换和退出入口。
- `AccountPage` 只呈现概览、设备、套餐和账号右侧内容。
- `OperatorPage` 只呈现当前运营模块的表格、详情、表单、近期认证和局部加载状态。
- 已删除 `AccountPage` 与 `OperatorPage` 原有的两套侧栏、品牌头、语言和 Workspace 加载；没有通过 `embedded` 开关继续维护旧壳。

## 用户路径

1. 管理员以普通账号 Session 登录并进入账号概览。
2. 左侧栏直接展示后端 Workspace 允许的用户、Agent、订单、订阅、套餐、特权、优惠码、Hub 和版本模块。
3. 点击任一管理项只更新 URL 与右侧内容，左栏不卸载，页面不整页刷新。
4. `/operator` 和详情深链接仍可刷新、复制及前进后退；无权限模块仍由 Workspace 导航过滤和后端 API 双重拒绝。

## 本地准入

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
go test ./private/cloud/controller
git diff --check
```

结果：14 条 Playwright 用例、Web typecheck/build、Controller Go 测试和差异格式检查全部通过。
