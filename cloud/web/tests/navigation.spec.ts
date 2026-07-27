import { expect, test, type Page, type Route } from '@playwright/test'
import { Buffer } from 'node:buffer'

const now = '2026-07-27T12:00:00Z'
const periodEnd = '2026-08-27T12:00:00Z'
const account = { account_id: '11111111-1111-4111-8111-111111111111', email: 'user@muxvia.com', display_name: '测试用户', state: 'ACCOUNT_STATE_ACTIVE', revision: '1', created_at: now, updated_at: now }
const daemon = { daemon_id: '33333333-3333-4333-8333-333333333333', account_id: account.account_id, account_name: account.display_name, display_name: '开发 Mac', device_id: 'device-1', device_fingerprint: 'fingerprint-1', revision: '1', created_at: now, updated_at: now }
const plans = [{ plan_id: 'starter', version: '1', name: '基础版', description: '适合个人设备的完整 Cloud 连接能力。', state: 'PLAN_STATE_PUBLISHED', billing_period_days: 30, monthly_price: { currency: 'CNY', minor_units: '0' }, yearly_price: { currency: 'CNY', minor_units: '0' }, capability: { managed_p2p_enabled: true, managed_p2p_max_concurrency: 2, relay_enabled: true, relay_max_concurrency: 2, relay_max_bytes_per_period: '5368709120', cloud_daemon_limit: 3 }, revision: '1', created_at: now }, { plan_id: 'professional', version: '1', name: '专业版', description: '适合多设备与高频远程工作的更高配额。', state: 'PLAN_STATE_PUBLISHED', billing_period_days: 30, monthly_price: { currency: 'CNY', minor_units: '3900' }, yearly_price: { currency: 'CNY', minor_units: '39900' }, capability: { managed_p2p_enabled: true, managed_p2p_max_concurrency: 10, relay_enabled: true, relay_max_concurrency: 8, relay_max_bytes_per_period: '1099511627776', cloud_daemon_limit: 20 }, revision: '1', created_at: now }]
const commerce = { subscription: { subscription_id: '55555555-5555-4555-8555-555555555555', account_id: account.account_id, plan_id: 'starter', plan_version: '1', state: 'SUBSCRIPTION_STATE_ACTIVE', revision: '1', period_start: now, period_end: periodEnd }, entitlement: { account_id: account.account_id, state: 'ENTITLEMENT_STATE_ACTIVE', plan_id: 'starter', plan_version: '1', relay_remaining_bytes: '5368707584', capability: plans[0].capability }, orders: [], payment_attempts: [], usage: { account_id: account.account_id, period_start: now, period_end: periodEnd, relay_ingress_bytes: '512', relay_egress_bytes: '1024', relay_total_bytes: '1536', quota_bytes: '5368709120', remaining_bytes: '5368707584', revision: '1' } }
const pendingOrder = { order_id: 'order-pending', account_id: account.account_id, plan_id: 'professional', plan_version: '1', status: 'ORDER_STATUS_PENDING', amount: { currency: 'CNY', minor_units: '3900' }, provider: 'development', idempotency_key: 'pending-checkout', requested_transition: 'SUBSCRIPTION_TRANSITION_UPGRADE', revision: '1', created_at: now }
const pendingAttempt = { payment_attempt_id: 'attempt-pending', order_id: pendingOrder.order_id, account_id: account.account_id, provider: 'development', status: 'PAYMENT_ATTEMPT_STATUS_PENDING', revision: '1', created_at: now, updated_at: now }
const onlineCertificateBinding = { edge_id: '22222222-2222-4222-8222-222222222222', edge_name: 'CN1 Edge', public_endpoint: 'muxvia-cn1.omscd.com:41102', certificate_profile_id: '66666666-6666-4666-8666-666666666666', certificate_profile_name: '中国区 Edge 证书', binding_revision: '1', desired_revision: '2', applied_revision: '2', sync_state: 'CERTIFICATE_SYNC_STATE_APPLIED', applied_at: now, online: true }
const offlineCertificateBinding = { edge_id: '77777777-7777-4777-8777-777777777777', edge_name: '备用 Edge', public_endpoint: 'muxvia-cn2.omscd.com:41102', certificate_profile_id: '66666666-6666-4666-8666-666666666666', certificate_profile_name: '中国区 Edge 证书', binding_revision: '1', desired_revision: '2', applied_revision: '1', sync_state: 'CERTIFICATE_SYNC_STATE_PENDING', online: false }
const certificateProfile = { certificate_profile_id: '66666666-6666-4666-8666-666666666666', name: '中国区 Edge 证书', dns_names: ['*.omscd.com'], sha256_fingerprint: '9D7A0FE21C994AE4B24383E54DB131A25776D2A285F45E733728E474072F7C2A', not_before: now, not_after: periodEnd, revision: '2', created_at: now, updated_at: now, bindings: [onlineCertificateBinding, offlineCertificateBinding] }

function json(route: Route, value: unknown, status = 200) { return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) }) }

async function mockAPI(page: Page, operator = false, failLogin = false, withPendingOrder = false) {
  const requests = new Map<string, number>()
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    requests.set(path, (requests.get(path) ?? 0) + 1)
    if (path === '/api/commerce/plans') return json(route, { plans })
    if (path === '/api/account/current') return json(route, { account, roles: operator ? ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'] : ['ACCOUNT_ROLE_USER'], recent_auth_expires_at: periodEnd })
    if (path === '/api/account/register') return json(route, { account, session: { session_id: 'register-session', access_expires_at: periodEnd, refresh_expires_at: periodEnd } }, 201)
    if (path === '/api/account/login') return failLogin ? json(route, { code: 'invalid_credentials' }, 401) : json(route, { account, roles: operator ? ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'] : ['ACCOUNT_ROLE_USER'], session: { session_id: 'login-session', access_expires_at: periodEnd, refresh_expires_at: periodEnd } })
    if (path === '/api/account/sessions') return json(route, { sessions: [{ session_id: 'current-session', current: true, created_at: now, access_expires_at: periodEnd, refresh_expires_at: periodEnd, recent_auth_expires_at: periodEnd, revision: '1' }, { session_id: 'other-session', current: false, created_at: now, access_expires_at: periodEnd, refresh_expires_at: periodEnd, revision: '1' }] })
    if (path === '/api/commerce/me') return json(route, withPendingOrder ? { ...commerce, orders: [pendingOrder], payment_attempts: [pendingAttempt] } : commerce)
    if (path === '/api/commerce/orders') return json(route, { order: { order_id: 'order-development', account_id: account.account_id, plan_id: 'professional', plan_version: '1', status: 'ORDER_STATUS_PENDING', amount: { currency: 'CNY', minor_units: '3900' }, provider: 'development', idempotency_key: 'checkout', requested_transition: 'SUBSCRIPTION_TRANSITION_UPGRADE', revision: '1', created_at: now }, payment_attempt: { payment_attempt_id: 'attempt-development', order_id: 'order-development', account_id: account.account_id, provider: 'development', status: 'PAYMENT_ATTEMPT_STATUS_PENDING', revision: '1', created_at: now, updated_at: now } }, 201)
    if (path === '/api/commerce/payments/development') return json(route, { order: { order_id: 'order-development', status: 'ORDER_STATUS_PAID' }, subscription: { ...commerce.subscription, plan_id: 'professional', revision: '2' }, entitlement: { ...commerce.entitlement, plan_id: 'professional' } })
    if (path === '/api/daemons') return json(route, { daemons: [{ daemon, runtime: { online: true, edge_id: '22222222-2222-4222-8222-222222222222', edge_name: 'CN1 Edge', edge_region: 'CN1', edge_public_endpoint: 'muxvia-cn1.omscd.com:41102', generation: '1' } }] })
    if (path === '/api/daemons/enroll') return json(route, { account_id: account.account_id, enrollment_code: 'mxe_test', expires_at: periodEnd, enroll_command: 'muxvia cloud enroll --controller https://cloud.muxvia.com mxe_test' })
    if (path === '/api/operator/events') return route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'event: ready\ndata: {"controller_instance_id":"controller-test"}\n\n' })
    if (path === '/api/operator/overview') return json(route, { overview: { edge_total: '1', edge_online: '1', daemon_total: '1', daemon_online: '1', client_session_online: '1', p2p_session_online: '1', relay_session_online: '0', relay_bytes_current_period: '1536', controller_instance_id: 'controller-test', generated_at: now } })
    if (path === '/api/operator/edges') return json(route, { edges: [
      { config: { edge_id: '22222222-2222-4222-8222-222222222222', version: '1', name: 'CN1 Edge', region: 'CN1', capacity: '1000', public_endpoint: 'muxvia-cn1.omscd.com:41102', enabled: true }, config_revision: '1', runtime: { online: true, software_version: 'dev-cloudp007', agent_count: '1', session_count: '1', relay_allocation_count: '0', last_heartbeat: now }, certificate: onlineCertificateBinding },
      { config: { edge_id: '77777777-7777-4777-8777-777777777777', version: '1', name: '备用 Edge', region: 'CN2', capacity: '1000', public_endpoint: 'muxvia-cn2.omscd.com:41102', enabled: true }, config_revision: '1', runtime: { online: false, software_version: 'dev-cloudp007', agent_count: '0', session_count: '0', relay_allocation_count: '0', last_heartbeat: now }, certificate: offlineCertificateBinding },
    ] })
    if (path === '/api/operator/certificates' && route.request().method() === 'GET') return json(route, { profiles: [certificateProfile] })
    if (path === '/api/operator/certificates' && route.request().method() === 'POST') {
      if (route.request().postDataJSON()?.name === '无效证书') return json(route, { error: '证书与私钥不匹配' }, 409)
      return json(route, { profile: certificateProfile })
    }
    if (path.startsWith('/api/operator/certificates/') && route.request().method() === 'PUT') return json(route, { profile: certificateProfile })
    if (path.endsWith('/certificate') && route.request().method() === 'POST') return json(route, { binding: certificateProfile.bindings[0] })
    if (path === '/api/operator/daemons') return json(route, { daemons: [{ daemon, runtime: { online: true, edge_name: 'CN1 Edge', edge_region: 'CN1', edge_public_endpoint: 'muxvia-cn1.omscd.com:41102', generation: '1' } }] })
    if (path === '/api/operator/connections') return json(route, { sessions: [{ session_id: '44444444-4444-4444-8444-444444444444', account_id: account.account_id, daemon_id: daemon.daemon_id, edge_id: '22222222-2222-4222-8222-222222222222', client_id: 'android-client', product: 'CLIENT_PRODUCT_ANDROID', relay: false, generation: '1', connected_at: now }] })
    if (path === '/api/operator/accounts') return json(route, { accounts: [{ account, roles: ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'], daemon_count: '1', subscription: commerce.subscription, entitlement: commerce.entitlement, usage: commerce.usage }] })
    if (path === '/api/operator/plans') return json(route, { plans })
    if (path === '/api/operator/subscriptions') return json(route, { subscriptions: [commerce.subscription] })
    if (path === '/api/operator/orders') return json(route, { orders: [] })
    if (path === '/api/operator/usage') return json(route, { accounts: [commerce.usage], edges: [] })
    if (path === '/api/operator/audit') return json(route, { events: [] })
    return json(route, {})
  })
  return requests
}

test('公开落地页展示真实连接路径和套餐', async ({ page }, testInfo) => {
  await mockAPI(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Muxvia Cloud', exact: true })).toBeVisible()
  await expect(page.getByText('随时回到你的电脑。', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '把一台电脑放进你的设备列表。' })).toBeVisible()
  await expect(page.getByLabel('Muxvia Cloud 产品连接画面')).toContainText('muxvia-cloud-ok')
  await expect(page.getByRole('heading', { name: '基础版' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('landing.png'), fullPage: true })
})

test('普通用户共享 Shell、复用页面缓存且不能进入运营页面', async ({ page }, testInfo) => {
  const requests = await mockAPI(page)
  await page.goto('/app/overview')
  const userNav = page.getByRole('navigation', { name: '用户功能' })
  const compact = testInfo.project.name !== 'desktop-chromium'
  if (compact) await expect(userNav).toBeHidden()
  else await expect(userNav).toBeVisible()
  await expect(page.getByRole('navigation', { name: '运营管理' })).toHaveCount(0)
  await expect(page.getByLabel('Cloud 设备连接可用')).toContainText('连接可用')
  await page.screenshot({ path: testInfo.outputPath('user-overview.png'), fullPage: !compact })
  if (testInfo.project.name === 'mobile-chromium') {
    const finalAction = page.getByRole('link', { name: '查看套餐' })
    const bottomNavigation = page.getByRole('navigation', { name: '手机主导航' })
    await finalAction.scrollIntoViewIfNeeded()
    const [actionBox, navigationBox] = await Promise.all([finalAction.boundingBox(), bottomNavigation.boundingBox()])
    expect(actionBox && navigationBox ? actionBox.y + actionBox.height <= navigationBox.y : false).toBe(true)
  }
  const deviceLink = compact
    ? page.getByRole('navigation', { name: '手机主导航' }).getByRole('link', { name: '我的设备' })
    : userNav.getByRole('link', { name: '我的设备' })
  await deviceLink.click()
  await expect(page).toHaveURL(/\/app\/devices$/)
  await expect(page.getByText('CN1 Edge', { exact: true })).toBeVisible()
  if (!compact) {
    await userNav.getByRole('link', { name: '概览' }).click()
    await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
    await expect(page.locator('.skeleton-list')).toHaveCount(0)
    expect(requests.get('/api/commerce/me')).toBe(1)
    await page.goto('/app/devices')
    await page.reload()
    await expect(page).toHaveURL(/\/app\/devices$/)
    await expect(page.getByText('CN1 Edge', { exact: true })).toBeVisible()
  }
  await page.screenshot({ path: testInfo.outputPath('user-devices.png'), fullPage: !compact })
  await page.goto('/app/admin/edges')
  await expect(page).toHaveURL(/\/app\/no-permission$/)
  await expect(page.getByRole('heading', { name: '没有运营管理权限' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('user-denied.png'), fullPage: true })
})

test('用户创建订单并完成 Development 支付', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/app/subscription')
  await page.getByRole('button', { name: '选择专业版' }).click()
  const paymentDialog = page.getByRole('dialog', { name: 'Development 支付确认' })
  await expect(paymentDialog).toBeVisible()
  await expect(paymentDialog.getByText('¥39.00', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '确认测试支付' }).click()
  await expect(page.getByRole('dialog', { name: 'Development 支付确认' })).toHaveCount(0)
})

test('用户可以确认取消续订并继续完成待支付订单', async ({ page }) => {
  await mockAPI(page, false, false, true)
  await page.goto('/app/subscription')
  await page.getByRole('button', { name: '到期后取消' }).click()
  const cancelDialog = page.getByRole('dialog', { name: '取消自动续订' })
  await expect(cancelDialog).toContainText('不会立即删除设备或中断现有连接')
  await page.keyboard.press('Escape')
  await expect(cancelDialog).toHaveCount(0)

  await page.goto('/app/orders')
  await page.getByRole('button', { name: '继续支付' }).click()
  const paymentDialog = page.getByRole('dialog', { name: '继续完成订单' })
  await expect(paymentDialog).toContainText('¥39.00')
  await paymentDialog.getByRole('button', { name: '确认测试支付' }).click()
  await expect(paymentDialog).toHaveCount(0)
})

test('管理员在同一 Shell 进入全部运营模块', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page, true)
  const errors = captureErrors(page)
  await page.goto('/app/admin/overview')
  const navigation = page.getByRole('navigation', { name: '运营管理' })
  const modules = [['Edge 管理', 'edges'], ['在线 daemon', 'daemons'], ['实时连接', 'connections'], ['用户与权限', 'accounts'], ['套餐', 'plans'], ['订阅', 'subscriptions'], ['订单与交易', 'orders'], ['证书', 'certificates'], ['用量与结算', 'usage'], ['审计', 'audit'], ['系统', 'system']] as const
  for (const [label, path] of modules) {
    await navigation.getByRole('link', { name: label, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/app/admin/${path}$`))
    await expect(page.getByRole('heading', { name: label, exact: true }).last()).toBeVisible()
    await expect(navigation).toBeVisible()
    await expect(page.locator('.boot-shell')).toHaveCount(0)
  }
  await page.screenshot({ path: testInfo.outputPath('admin-shell.png'), fullPage: true })
  expect(errors).toEqual([])
})

test('证书页在三种视口完成双文件选择并展示自动同步状态', async ({ page }, testInfo) => {
  await mockAPI(page, true)
  const errors = captureErrors(page)
  await page.goto('/app/admin/certificates')
  await expect(page.getByRole('heading', { name: '证书', exact: true }).last()).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: '中国区 Edge 证书' }).first()).toContainText('待同步')
  await expect(page.getByRole('row').filter({ hasText: 'CN1 Edge' }).last()).toContainText('已应用')
  await expect(page.getByRole('row').filter({ hasText: '备用 Edge' }).last()).toContainText('离线待同步')
  await page.getByRole('button', { name: '上传证书', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '上传证书' })
  await dialog.getByLabel('档案名称').fill('海外 Edge 证书')
  await dialog.getByLabel('证书链文件').setInputFiles({ name: 'fullchain.pem', mimeType: 'application/x-pem-file', buffer: Buffer.from('-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n') })
  await dialog.getByLabel('私钥文件').setInputFiles({ name: 'privkey.pem', mimeType: 'application/x-pem-file', buffer: Buffer.from('-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n') })
  await expect(dialog.getByLabel('证书链文件')).toHaveValue(/fullchain\.pem$/)
  await expect(dialog.getByLabel('私钥文件')).toHaveValue(/privkey\.pem$/)
  await dialog.getByRole('button', { name: '上传证书', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: '上传证书', exact: true }).click()
  const invalidDialog = page.getByRole('dialog', { name: '上传证书' })
  await invalidDialog.getByLabel('档案名称').fill('无效证书')
  await invalidDialog.getByLabel('证书链文件').setInputFiles({ name: 'fullchain.pem', mimeType: 'application/x-pem-file', buffer: Buffer.from('-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n') })
  await invalidDialog.getByLabel('私钥文件').setInputFiles({ name: 'privkey.pem', mimeType: 'application/x-pem-file', buffer: Buffer.from('-----BEGIN PRIVATE KEY-----\nwrong\n-----END PRIVATE KEY-----\n') })
  await invalidDialog.getByRole('button', { name: '上传证书', exact: true }).click()
  await expect(invalidDialog).toContainText('证书与私钥不匹配')
  await invalidDialog.getByRole('button', { name: '取消' }).click()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('certificates.png'), fullPage: testInfo.project.name === 'desktop-chromium' })
  expect(errors.filter((value) => !value.includes('status of 409'))).toEqual([])
  expect(errors.filter((value) => value.includes('status of 409'))).toHaveLength(1)
})

test('手机底栏与运营抽屉不遮挡内容', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'desktop-chromium')
  await mockAPI(page, true)
  await page.goto('/app/overview')
  const bottom = page.getByRole('navigation', { name: '手机主导航' })
  await bottom.getByRole('link', { name: '我的设备' }).click()
  await expect(page.getByRole('heading', { name: '我的设备' })).toBeVisible()
  await page.getByRole('button', { name: '打开导航' }).click()
  await page.getByRole('navigation', { name: '运营管理' }).getByRole('link', { name: '实时连接' }).click()
  await expect(page).toHaveURL(/\/app\/admin\/connections$/)
  await expect.poll(async () => { const box = await page.locator('.sidebar').boundingBox(); return box ? box.x + box.width : 0 }).toBeLessThanOrEqual(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('mobile-admin.png'), fullPage: true })
})

test('注册后直接进入普通用户概览', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/register')
  await page.getByLabel('你的称呼').fill('测试用户')
  await page.getByLabel('邮箱').fill('user@muxvia.com')
  await page.getByLabel('密码', { exact: true }).fill('muxvia-test-password')
  await page.getByRole('button', { name: '创建账号' }).click()
  await expect(page).toHaveURL(/\/app\/overview$/)
  await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
})

test('统一登录页提供字段约束和明确错误反馈', async ({ page }) => {
  await mockAPI(page, false, true)
  const errors = captureErrors(page)
  await page.goto('/login')
  const submit = page.getByRole('button', { name: '登录', exact: true })
  await expect(submit).toBeDisabled()
  await page.getByLabel('邮箱或账号').fill('user@muxvia.com')
  await page.getByLabel('密码', { exact: true }).fill('wrong-password')
  await submit.click()
  await expect(page.getByRole('alert')).toHaveText('账号或密码不正确。请检查后重新输入。')
  await expect(page).toHaveURL(/\/login$/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
  expect(errors.filter((value) => !value.includes('status of 401'))).toEqual([])
})

function captureErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}
