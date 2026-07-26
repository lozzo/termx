import { expect, test, type Page, type Route } from '@playwright/test'

const now = '2026-07-26T12:00:00Z'
const account = { account_id: '11111111-1111-4111-8111-111111111111', email: 'operator@muxvia.com', display_name: '测试运营员', state: 'ACCOUNT_STATE_ACTIVE', revision: '1', created_at: now, updated_at: now }

function json(route: Route, value: unknown) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(value) })
}

async function mockAPI(page: Page) {
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/account/current') return json(route, { account, roles: ['ACCOUNT_ROLE_OPERATOR'], recent_auth_expires_at: '2026-07-26T13:00:00Z' })
    if (path === '/api/operator/events') return route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'event: ready\ndata: {"controller_instance_id":"controller-test"}\n\n' })
    if (path === '/api/operator/overview') return json(route, { overview: { edge_total: '1', edge_online: '1', daemon_total: '1', daemon_online: '1', client_session_online: '1', p2p_session_online: '1', relay_session_online: '0', relay_bytes_current_period: '1536', controller_instance_id: 'controller-test', generated_at: now } })
    if (path === '/api/operator/edges') return json(route, { edges: [{ config: { edge_id: '22222222-2222-4222-8222-222222222222', version: '1', name: '上海测试 Edge', region: 'cn-east-1', capacity: '1000', public_endpoint: 'muxvia-cn1.omscd.com:41102', enabled: true }, config_revision: '1', runtime: { online: true, software_version: 'dev-r7', agent_count: '1', session_count: '1', relay_allocation_count: '0', last_heartbeat: now } }] })
    if (path === '/api/operator/daemons') return json(route, { daemons: [{ daemon: { daemon_id: '33333333-3333-4333-8333-333333333333', account_id: account.account_id, account_name: '测试账号', display_name: '开发机', device_id: 'device-1', revision: '1', created_at: now, updated_at: now }, runtime: { online: true, edge_id: '22222222-2222-4222-8222-222222222222', edge_name: '上海测试 Edge', edge_region: 'cn-east-1', edge_public_endpoint: 'muxvia-cn1.omscd.com:41102', generation: '1' } }] })
    if (path === '/api/operator/connections') return json(route, { sessions: [{ session_id: '44444444-4444-4444-8444-444444444444', account_id: account.account_id, daemon_id: '33333333-3333-4333-8333-333333333333', edge_id: '22222222-2222-4222-8222-222222222222', client_id: 'android-client', product: 'CLIENT_PRODUCT_ANDROID', relay: false, generation: '1', connected_at: now }] })
    if (path === '/api/operator/accounts') return json(route, { accounts: [{ account, roles: ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_OPERATOR'], daemon_count: '1', subscription: { subscription_id: 'sub-1', account_id: account.account_id, plan_id: 'starter', plan_version: '1', state: 'SUBSCRIPTION_STATE_ACTIVE', revision: '1', period_start: now, period_end: '2026-08-26T12:00:00Z' }, entitlement: { account_id: account.account_id, state: 'ENTITLEMENT_STATE_ACTIVE', plan_id: 'starter', plan_version: '1', relay_remaining_bytes: '1048576' }, usage: { account_id: account.account_id, relay_total_bytes: '1536', remaining_bytes: '1048576' } }], next_cursor: url.searchParams.has('cursor') ? '' : 'account-page-2' })
    if (path === '/api/operator/plans') return json(route, { plans: [{ plan_id: 'starter', version: '1', name: '入门版', state: 'PLAN_STATE_PUBLISHED', billing_period_days: 30, monthly_price: { currency: 'CNY', minor_units: '0' }, yearly_price: { currency: 'CNY', minor_units: '0' }, capability: { managed_p2p_enabled: true, managed_p2p_max_concurrency: 1, cloud_daemon_limit: 1 }, revision: '1', created_at: now }] })
    if (path === '/api/operator/subscriptions') return json(route, { subscriptions: [{ subscription_id: '55555555-5555-4555-8555-555555555555', account_id: account.account_id, plan_id: 'starter', plan_version: '1', state: 'SUBSCRIPTION_STATE_ACTIVE', revision: '1', period_start: now, period_end: '2026-08-26T12:00:00Z' }] })
    if (path === '/api/operator/orders') return json(route, { orders: [{ order_id: '66666666-6666-4666-8666-666666666666', account_id: account.account_id, plan_id: 'starter', plan_version: '1', status: 'ORDER_STATUS_PAID', amount: { currency: 'CNY', minor_units: '0' }, provider: 'manual', idempotency_key: 'e2e', revision: '1', created_at: now }] })
    if (path === '/api/operator/usage') return json(route, { accounts: [{ account_id: account.account_id, period_start: now, period_end: '2026-08-26T12:00:00Z', relay_ingress_bytes: '512', relay_egress_bytes: '1024', relay_total_bytes: '1536', quota_bytes: '1048576', remaining_bytes: '1047040', revision: '1' }], edges: [] })
    if (path === '/api/operator/audit') return json(route, { events: [{ audit_id: '77777777-7777-4777-8777-777777777777', actor_account_id: account.account_id, actor_display_name: account.display_name, action: 'account.login', resource_type: 'account', resource_id: account.account_id, result: 'applied', occurred_at: now }] })
    return json(route, {})
  })
}

test.beforeEach(async ({ page }) => { await mockAPI(page) })

test('桌面端固定侧栏逐项进入不同管理模块', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/overview')
  const nav = page.getByRole('navigation', { name: '运营模块' })
  await expect(nav).toBeVisible()
  const modules = [['Edge 管理', '/edges'], ['在线 daemon', '/daemons'], ['实时连接', '/connections'], ['用户与权限', '/accounts'], ['套餐', '/plans'], ['订阅', '/subscriptions'], ['订单与交易', '/orders'], ['证书', '/certificates'], ['用量与结算', '/usage'], ['审计', '/audit'], ['系统', '/system']] as const
  for (const [label, path] of modules) {
    await nav.getByRole('link', { name: label, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(page.getByRole('heading', { name: label, exact: true }).last()).toBeVisible()
    await expect(nav).toBeVisible()
    await expect(page.locator('.boot-shell')).toHaveCount(0)
  }
  await nav.getByRole('link', { name: '用户与权限', exact: true }).click()
  await expect(page.getByText('用户、运营', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('第 2 页', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '上一页' })).toBeEnabled()
  await page.screenshot({ path: testInfo.outputPath('desktop-system.png'), fullPage: true })
  expect(errors).toEqual([])
})

test('手机端通过菜单进入模块且内容不重叠', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile-chromium')
  await page.goto('/overview')
  for (const [label, path] of [['Edge 管理', '/edges'], ['在线 daemon', '/daemons'], ['实时连接', '/connections']] as const) {
    await page.getByRole('button', { name: '打开导航' }).click()
    const nav = page.getByRole('navigation', { name: '运营模块' })
    await expect(nav).toBeVisible()
    await nav.getByRole('link', { name: label, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(page.getByRole('heading', { name: label, exact: true }).last()).toBeVisible()
  }
  await expect(page.getByText('Android', { exact: true })).toBeVisible()
  await expect.poll(async () => { const box = await page.locator('.sidebar').boundingBox(); return box ? box.x + box.width : 0 }).toBeLessThanOrEqual(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('mobile-connections.png'), fullPage: true })
})
