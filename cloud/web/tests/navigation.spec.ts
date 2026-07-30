import { expect, test, type Page, type Route } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { Buffer } from 'node:buffer'

const now = '2026-07-27T12:00:00Z'
const periodEnd = '2026-08-27T12:00:00Z'
const account = { account_id: '11111111-1111-4111-8111-111111111111', email: 'user@anytty.com', display_name: '测试用户', state: 'ACCOUNT_STATE_ACTIVE', revision: '1', created_at: now, updated_at: now }
const daemon = { daemon_id: '33333333-3333-4333-8333-333333333333', account_id: account.account_id, account_name: account.display_name, display_name: '开发 Mac', device_id: 'device-1', device_fingerprint: 'fingerprint-1', revision: '1', created_at: now, updated_at: now }
const plans = [{ plan_id: 'starter', version: '1', name: '基础版', description: '适合个人设备的完整 Cloud 连接能力。', state: 'PLAN_STATE_PUBLISHED', billing_period_days: 30, monthly_price: { currency: 'CNY', minor_units: '0' }, yearly_price: { currency: 'CNY', minor_units: '0' }, capability: { managed_p2p_enabled: true, managed_p2p_max_concurrency: 2, relay_enabled: true, relay_max_concurrency: 2, relay_max_bytes_per_period: '5368709120', cloud_daemon_limit: 3 }, revision: '1', created_at: now }, { plan_id: 'professional', version: '1', name: '专业版', description: '适合多设备与高频远程工作的更高配额。', state: 'PLAN_STATE_PUBLISHED', billing_period_days: 30, monthly_price: { currency: 'CNY', minor_units: '3900' }, yearly_price: { currency: 'CNY', minor_units: '39900' }, capability: { managed_p2p_enabled: true, managed_p2p_max_concurrency: 10, relay_enabled: true, relay_max_concurrency: 8, relay_max_bytes_per_period: '1099511627776', cloud_daemon_limit: 20 }, revision: '1', created_at: now }]
const commerce = { subscription: { subscription_id: '55555555-5555-4555-8555-555555555555', account_id: account.account_id, plan_id: 'starter', plan_version: '1', state: 'SUBSCRIPTION_STATE_ACTIVE', revision: '1', period_start: now, period_end: periodEnd }, entitlement: { account_id: account.account_id, state: 'ENTITLEMENT_STATE_ACTIVE', plan_id: 'starter', plan_version: '1', relay_remaining_bytes: '5368707584', capability: plans[0].capability }, orders: [], payment_attempts: [], usage: { account_id: account.account_id, period_start: now, period_end: periodEnd, relay_ingress_bytes: '512', relay_egress_bytes: '1024', relay_total_bytes: '1536', quota_bytes: '5368709120', remaining_bytes: '5368707584', revision: '1' } }
const pendingOrder = { order_id: 'order-pending', account_id: account.account_id, plan_id: 'professional', plan_version: '1', status: 'ORDER_STATUS_PENDING', amount: { currency: 'CNY', minor_units: '3900' }, provider: 'development', idempotency_key: 'pending-checkout', requested_transition: 'SUBSCRIPTION_TRANSITION_UPGRADE', revision: '1', created_at: now }
const pendingAttempt = { payment_attempt_id: 'attempt-pending', order_id: pendingOrder.order_id, account_id: account.account_id, provider: 'development', status: 'PAYMENT_ATTEMPT_STATUS_PENDING', revision: '1', created_at: now, updated_at: now }
const onlineCertificateBinding = { edge_id: '22222222-2222-4222-8222-222222222222', edge_name: 'CN1 Edge', public_endpoint: 'cn1.edge.anytty.com:41102', certificate_profile_id: '66666666-6666-4666-8666-666666666666', certificate_profile_name: '中国区 Edge 证书', binding_revision: '1', desired_revision: '2', applied_revision: '2', sync_state: 'CERTIFICATE_SYNC_STATE_APPLIED', applied_at: now, online: true }
const offlineCertificateBinding = { edge_id: '77777777-7777-4777-8777-777777777777', edge_name: '备用 Edge', public_endpoint: 'cn2.edge.anytty.com:41102', certificate_profile_id: '66666666-6666-4666-8666-666666666666', certificate_profile_name: '中国区 Edge 证书', binding_revision: '1', desired_revision: '2', applied_revision: '1', sync_state: 'CERTIFICATE_SYNC_STATE_PENDING', online: false }
const certificateProfile = { certificate_profile_id: '66666666-6666-4666-8666-666666666666', name: '中国区 Edge 证书', dns_names: ['*.edge.anytty.com'], sha256_fingerprint: '9D7A0FE21C994AE4B24383E54DB131A25776D2A285F45E733728E474072F7C2A', not_before: now, not_after: periodEnd, revision: '2', created_at: now, updated_at: now, bindings: [onlineCertificateBinding, offlineCertificateBinding] }
const themeTokens = {
  light: { '--bg': '#f1f3f6', '--surface': '#ffffff', '--text': '#111418' },
  dark: { '--bg': '#10161d', '--surface': '#171f28', '--text': '#eef2f7' },
} as const

function json(route: Route, value: unknown, status = 200) { return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) }) }

async function mockAPI(page: Page, operator = false, failLogin = false, withPendingOrder = false) {
  const requests = new Map<string, number>()
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    requests.set(path, (requests.get(path) ?? 0) + 1)
    if (path === '/api/commerce/plans') return json(route, { plans })
    if (path === '/api/account/current') return json(route, { account, roles: operator ? ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'] : ['ACCOUNT_ROLE_USER'], recent_auth_expires_at: periodEnd })
    if (path === '/api/account/login') return failLogin ? json(route, { code: 'invalid_credentials' }, 401) : json(route, { account, roles: operator ? ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'] : ['ACCOUNT_ROLE_USER'], session: { session_id: 'login-session', access_expires_at: periodEnd, refresh_expires_at: periodEnd } })
    if (path === '/api/account/sessions') return json(route, { sessions: [{ session_id: 'current-session', current: true, created_at: now, access_expires_at: periodEnd, refresh_expires_at: periodEnd, recent_auth_expires_at: periodEnd, revision: '1' }, { session_id: 'other-session', current: false, created_at: now, access_expires_at: periodEnd, refresh_expires_at: periodEnd, revision: '1' }] })
    if (path === '/api/commerce/me') return json(route, withPendingOrder ? { ...commerce, orders: [pendingOrder], payment_attempts: [pendingAttempt] } : commerce)
    if (path === '/api/commerce/orders') return json(route, { order: { order_id: 'order-development', account_id: account.account_id, plan_id: 'professional', plan_version: '1', status: 'ORDER_STATUS_PENDING', amount: { currency: 'CNY', minor_units: '3900' }, provider: 'development', idempotency_key: 'checkout', requested_transition: 'SUBSCRIPTION_TRANSITION_UPGRADE', revision: '1', created_at: now }, payment_attempt: { payment_attempt_id: 'attempt-development', order_id: 'order-development', account_id: account.account_id, provider: 'development', status: 'PAYMENT_ATTEMPT_STATUS_PENDING', revision: '1', created_at: now, updated_at: now } }, 201)
    if (path === '/api/commerce/payments/development') return json(route, { order: { order_id: 'order-development', status: 'ORDER_STATUS_PAID' }, subscription: { ...commerce.subscription, plan_id: 'professional', revision: '2' }, entitlement: { ...commerce.entitlement, plan_id: 'professional' } })
    if (path === '/api/daemons') return json(route, { daemons: [{ daemon, runtime: { online: true, edge_id: '22222222-2222-4222-8222-222222222222', edge_name: 'CN1 Edge', edge_region: 'CN1', edge_public_endpoint: 'cn1.edge.anytty.com:41102', generation: '1' } }] })
    if (path === '/api/daemons/enroll') return json(route, { account_id: account.account_id, enrollment_code: 'mxe_test', expires_at: periodEnd, enroll_command: 'anytty cloud enroll --controller https://cloud.anytty.com mxe_test' })
    if (path === '/api/operator/events') return route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'event: ready\ndata: {"controller_instance_id":"controller-test"}\n\n' })
    if (path === '/api/operator/overview') return json(route, { overview: { edge_total: '1', edge_online: '1', daemon_total: '1', daemon_online: '1', client_session_online: '1', relay_bytes_current_period: '1536', controller_instance_id: 'controller-test', generated_at: now } })
    if (path === '/api/operator/edges') return json(route, { edges: [
      { config: { edge_id: '22222222-2222-4222-8222-222222222222', version: '1', name: 'CN1 Edge', region: 'CN1', capacity: '1000', public_endpoint: 'cn1.edge.anytty.com:41102', enabled: true }, config_revision: '1', runtime: { online: true, software_version: 'dev-cloudp007', agent_count: '1', session_count: '1', last_heartbeat: now }, certificate: onlineCertificateBinding },
      { config: { edge_id: '77777777-7777-4777-8777-777777777777', version: '1', name: '备用 Edge', region: 'CN2', capacity: '1000', public_endpoint: 'cn2.edge.anytty.com:41102', enabled: true }, config_revision: '1', runtime: { online: false, software_version: 'dev-cloudp007', agent_count: '0', session_count: '0', last_heartbeat: now }, certificate: offlineCertificateBinding },
    ] })
    if (path === '/api/operator/certificates' && route.request().method() === 'GET') return json(route, { profiles: [certificateProfile] })
    if (path === '/api/operator/certificates' && route.request().method() === 'POST') {
      if (route.request().postDataJSON()?.name === '无效证书') return json(route, { code: 'conflict', message: '证书与私钥不匹配', request_id: 'certificate-conflict' }, 409)
      return json(route, { profile: certificateProfile })
    }
    if (path.startsWith('/api/operator/certificates/') && route.request().method() === 'PUT') return json(route, { profile: certificateProfile })
    if (path.endsWith('/certificate') && route.request().method() === 'POST') return json(route, { binding: certificateProfile.bindings[0] })
    if (path === '/api/operator/daemons') return json(route, { daemons: [{ daemon, runtime: { online: true, edge_name: 'CN1 Edge', edge_region: 'CN1', edge_public_endpoint: 'cn1.edge.anytty.com:41102', generation: '1' } }] })
    if (path === '/api/operator/connections') return json(route, { sessions: [{ session_id: '44444444-4444-4444-8444-444444444444', account_id: account.account_id, daemon_id: daemon.daemon_id, edge_id: '22222222-2222-4222-8222-222222222222', client_id: 'android-client', product: 'CLIENT_PRODUCT_ANDROID', generation: '1', connected_at: now }] })
    if (path === '/api/operator/accounts') return json(route, { accounts: [{ account, roles: ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'], daemon_count: '1', subscription: commerce.subscription, entitlement: commerce.entitlement, usage: commerce.usage }] })
    if (path === '/api/operator/plans') return json(route, { plans })
    if (path === '/api/operator/subscriptions') return json(route, { subscriptions: [commerce.subscription] })
    if (path === '/api/operator/orders') return json(route, { orders: [] })
    if (path === '/api/operator/usage') return json(route, { accounts: [commerce.usage] })
    if (path === '/api/operator/audit') return json(route, { events: [] })
    return json(route, {})
  })
  return requests
}

test('公开落地页展示真实连接路径和套餐', async ({ page }, testInfo) => {
  await mockAPI(page)
  await page.goto('/')
  const viewport = page.viewportSize()
  expect(viewport).not.toBeNull()
  await expect(page.getByRole('heading', { name: 'AnyTTY Cloud', exact: true })).toBeVisible()
  await expect(page.getByText('随时回到你的电脑。', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Cloud 管理 daemon，App 只接受扫码配对。' })).toBeVisible()
  await expect(page.getByText('Cloud 账号不会填充 App 设备列表。每台手机都必须扫描目标 daemon 或服务生成的配对二维码。')).toBeVisible()
  await expect(page.getByLabel('扫码配对后的 AnyTTY Cloud 连接路径')).toContainText('anytty-cloud-ok')
  await expect(page.getByRole('heading', { name: '基础版' })).toBeVisible()
  await assertMinimumHitArea(page, '.landing-header a, .landing-footer a')
  if (testInfo.project.name === 'mobile-360-chromium') {
    expect(viewport).toEqual({ width: 360, height: 800 })
    const nextSectionTop = await page.locator('.product-band').evaluate((element) => element.getBoundingClientRect().top)
    expect(nextSectionTop).toBeLessThanOrEqual(776)
    expect(800 - nextSectionTop).toBeGreaterThanOrEqual(24)
  }
  if (testInfo.project.name === 'mobile-320-chromium') expect(viewport).toEqual({ width: 320, height: 800 })
  if (testInfo.project.name === 'mobile-landscape-chromium') {
    expect(viewport).toEqual({ width: 844, height: 390 })
    const nextSectionTop = await page.locator('.product-band').evaluate((element) => element.getBoundingClientRect().top)
    const primaryCTA = page.getByRole('link', { name: '登录 Cloud 控制台' }).first()
    await expect(page.locator('.hero-copy > p')).toBeInViewport({ ratio: 1 })
    await expect(primaryCTA).toBeInViewport({ ratio: 1 })
    const ctaBox = await primaryCTA.boundingBox()
    expect(ctaBox).not.toBeNull()
    expect(ctaBox!.y + ctaBox!.height).toBeLessThanOrEqual(nextSectionTop)
    expect(viewport!.height - nextSectionTop).toBeGreaterThanOrEqual(24)
  }
  await assertNoHorizontalOverflow(page)
  const session = await page.context().newCDPSession(page)
  await session.send('Emulation.setPageScaleFactor', { pageScaleFactor: 2 })
  await assertNoHorizontalOverflow(page)
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
  await expect(page.getByLabel('Cloud daemon 管理状态可用')).toContainText('daemon 在线')
  await expect(page.getByText(/每台手机仍需扫描目标服务生成的二维码/)).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('user-overview.png'), fullPage: !compact })
  if (testInfo.project.name.startsWith('mobile-')) {
    const finalAction = page.getByRole('link', { name: '查看套餐' })
    const bottomNavigation = page.getByRole('navigation', { name: '手机主导航' })
    await finalAction.scrollIntoViewIfNeeded()
    const [actionBox, navigationBox] = await Promise.all([finalAction.boundingBox(), bottomNavigation.boundingBox()])
    expect(actionBox && navigationBox ? actionBox.y + actionBox.height <= navigationBox.y : false).toBe(true)
  }
  const deviceLink = compact
    ? page.getByRole('navigation', { name: '手机主导航' }).getByRole('link', { name: 'Daemon 管理' })
    : userNav.getByRole('link', { name: 'Daemon 管理' })
  await deviceLink.click()
  await expect(page).toHaveURL(/\/app\/devices$/)
  await expect(page.getByText('CN1 Edge', { exact: true })).toBeVisible()
  await assertNoHorizontalOverflow(page)
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

test('路由资源首次加载失败后通过页面重载恢复', async ({ page }, testInfo) => {
  test.skip(Boolean(process.env.ANYTTY_CLOUD_ONLINE_ORIGIN), '故障注入仅针对本地 Vite 模块请求')
  test.skip(!['desktop-chromium', 'mobile-320-chromium'].includes(testInfo.project.name))
  await page.emulateMedia({ colorScheme: testInfo.project.name === 'mobile-320-chromium' ? 'dark' : 'light' })
  await mockAPI(page)
  let moduleRequests = 0
  await page.route('**/src/routes/UserRouteGroup.ts', async (route) => {
    moduleRequests += 1
    if (moduleRequests === 1) return route.abort('failed')
    return route.continue()
  })

  await page.goto('/app/devices')
  const alert = page.getByRole('alert')
  const errorTitle = page.getByRole('heading', { name: '页面资源加载失败' })
  const reload = page.getByRole('button', { name: '重新加载页面资源' })
  await expect(alert).toContainText('当前页面资源未能加载')
  await expect(errorTitle).toBeFocused()
  await expect(page).toHaveURL(/\/app\/devices$/)
  await expect(page).toHaveTitle('Daemon 管理 · AnyTTY Cloud')
  await expect(page.locator('#root')).not.toBeEmpty()
  await assertMinimumHitArea(page, '.route-resource-error .button')
  await assertNoHorizontalOverflow(page)

  expect(moduleRequests).toBe(1)
  await reload.click()
  await expect(page.getByRole('heading', { name: 'Daemon 管理', exact: true })).toBeVisible()
  expect(moduleRequests).toBe(2)
  await expect(alert).toHaveCount(0)
  await expect(page).toHaveURL(/\/app\/devices$/)
  await expect(page).toHaveTitle('Daemon 管理 · AnyTTY Cloud')
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
  await expect(page.getByRole('dialog', { name: 'Cloud 导航' })).toHaveCount(0)
  await expect(page.locator('.menu-button')).toBeHidden()
  expect(await navigation.getByRole('link').first().evaluate((element) => getComputedStyle(element).minHeight)).toBe('42px')
  expect(await navigation.getByRole('heading', { level: 2 }).allTextContents()).toEqual(['Infrastructure', 'Account operations', 'Governance'])
  expect(await navigation.getByRole('link').allTextContents()).toEqual(['运营总览', 'Edge 管理', '在线 daemon', '实时连接', '用户与权限', '套餐', '订阅', '订单与交易', '用量与结算', '证书', '审计', '系统'])
  const modules = [['Edge 管理', 'edges'], ['在线 daemon', 'daemons'], ['实时连接', 'connections'], ['用户与权限', 'accounts'], ['套餐', 'plans'], ['订阅', 'subscriptions'], ['订单与交易', 'orders'], ['证书', 'certificates'], ['用量与结算', 'usage'], ['审计', 'audit'], ['系统', 'system']] as const
  for (const [label, path] of modules) {
    await navigation.getByRole('link', { name: label, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/app/admin/${path}$`))
    await expect(page).toHaveTitle(`${label} · AnyTTY Cloud`)
    await expect(page.locator('#main-content')).toBeFocused()
    await expect(page.getByRole('heading', { name: label, exact: true }).last()).toBeVisible()
    await expect(navigation).toBeVisible()
    await expect(page.locator('.boot-shell')).toHaveCount(0)
  }
  await page.screenshot({ path: testInfo.outputPath('admin-shell.png'), fullPage: true })
  expect(errors).toEqual([])
})

test('pathname navigation updates title and main focus without query focus theft', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page, true)
  await page.goto('/app/admin/accounts')
  const main = page.locator('#main-content')
  const accountSearch = page.getByLabel('搜索账号')
  await expect(page).toHaveTitle('用户与权限 · AnyTTY Cloud')
  await expect(main).toBeFocused()
  await accountSearch.focus()
  await expect(accountSearch).toBeFocused()
  expect(await accountSearch.locator('..').evaluate((element) => element.matches(':focus-within'))).toBe(true)
  expect(await accountSearch.locator('..').evaluate((element) => getComputedStyle(element).boxShadow)).not.toBe('none')
  await accountSearch.fill('测试用户')
  await accountSearch.press('Enter')
  await expect(page).toHaveURL(/\/app\/admin\/accounts\?query=/)
  await expect(accountSearch).toBeFocused()
  await expect(main).not.toBeFocused()

  await page.getByRole('navigation', { name: '运营管理' }).getByRole('link', { name: '审计', exact: true }).click()
  await expect(page).toHaveTitle('审计 · AnyTTY Cloud')
  await expect(main).toBeFocused()
  const auditSearch = page.getByLabel('搜索审计记录')
  await auditSearch.focus()
  expect(await auditSearch.locator('..').evaluate((element) => element.matches(':focus-within'))).toBe(true)
})

test('证书页在响应式视口完成对话框生命周期与双文件选择', async ({ page }, testInfo) => {
  await mockAPI(page, true)
  const errors = captureErrors(page)
  await page.goto('/app/admin/certificates')
  await expect(page.getByRole('heading', { name: '证书', exact: true }).last()).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: '中国区 Edge 证书' }).first()).toContainText('待同步')
  await expect(page.getByRole('row').filter({ hasText: 'CN1 Edge' }).last()).toContainText('已应用')
  await expect(page.getByRole('row').filter({ hasText: '备用 Edge' }).last()).toContainText('离线待同步')
  const openUpload = page.getByRole('button', { name: '上传证书', exact: true })
  await openUpload.click()
  let dialog = page.getByRole('dialog', { name: '上传证书' })
  await expect(dialog.getByRole('button', { name: '关闭' })).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(dialog.getByRole('button', { name: '上传证书', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(openUpload).toBeFocused()

  await openUpload.click()
  dialog = page.getByRole('dialog', { name: '上传证书' })
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
  await expect(invalidDialog).toContainText('无法上传证书，请检查证书链与私钥后重试。 关联 ID：certificate-conflict')
  await expect(invalidDialog).not.toContainText('证书与私钥不匹配')
  await invalidDialog.getByRole('button', { name: '取消' }).click()
  await assertNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('certificates.png'), fullPage: testInfo.project.name === 'desktop-chromium' })
  expect(errors.filter((value) => !value.includes('status of 409'))).toEqual([])
  expect(errors.filter((value) => value.includes('status of 409'))).toHaveLength(1)
})

test('query 重试只刷新失败项并保留已打开的证书 dialog 与 draft', async ({ page }) => {
  const requests = await mockAPI(page, true)
  let edgeRequests = 0
  await page.route('**/api/operator/edges', async (route) => {
    edgeRequests++
    if (edgeRequests === 1) return json(route, { code: 'temporary_failure', message: 'private edge query detail', request_id: 'edge-query-retry' }, 401)
    await route.fallback()
  })

  await page.goto('/app/admin/certificates')
  await expect(page.getByRole('alert')).toContainText('edge-query-retry')
  await page.getByRole('button', { name: '上传证书', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '上传证书' })
  await dialog.getByLabel('档案名称').fill('保留中的证书草稿')
  await dialog.getByLabel('证书链文件').setInputFiles({ name: 'kept-fullchain.pem', mimeType: 'application/x-pem-file', buffer: Buffer.from('kept certificate') })
  await dialog.getByLabel('私钥文件').setInputFiles({ name: 'kept-privkey.pem', mimeType: 'application/x-pem-file', buffer: Buffer.from('kept private key') })
  await expect(dialog.getByRole('alert')).toContainText('edge-query-retry')

  const retry = dialog.getByRole('button', { name: '重试', exact: true })
  await retry.focus()
  await expect(retry).toBeFocused()
  await retry.click()

  await expect(page.locator('.error-state')).toHaveCount(0)
  await expect(page.locator('.certificate-workspace')).toContainText('中国区 Edge 证书')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByLabel('档案名称')).toHaveValue('保留中的证书草稿')
  await expect(dialog.getByLabel('证书链文件')).toHaveValue(/kept-fullchain\.pem$/)
  await expect(dialog.getByLabel('私钥文件')).toHaveValue(/kept-privkey\.pem$/)
  expect(edgeRequests).toBe(2)
  expect(requests.get('/api/operator/certificates')).toBe(1)
})

test('query 重试保留账号列表已有的筛选与分页状态', async ({ page }) => {
  const requests = await mockAPI(page, true)
  const accountRequests: { query: string; cursor: string }[] = []
  let secondPageAttempts = 0
  await page.route(/\/api\/operator\/accounts(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url())
    const query = url.searchParams.get('query') ?? ''
    const cursor = url.searchParams.get('cursor') ?? ''
    accountRequests.push({ query, cursor })
    if (cursor === 'accounts-page-2' && ++secondPageAttempts === 1) {
      return json(route, { code: 'temporary_failure', message: 'private accounts query detail', request_id: 'accounts-query-retry' }, 401)
    }
    return json(route, {
      accounts: [{ account, roles: ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'], daemon_count: '1', subscription: commerce.subscription, entitlement: commerce.entitlement, usage: commerce.usage }],
      next_cursor: cursor ? '' : 'accounts-page-2',
    })
  })

  await page.goto('/app/admin/accounts')
  const filter = page.getByLabel('搜索账号')
  await filter.fill('测试用户')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(filter).toHaveValue('测试用户')
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('accounts-query-retry')

  await page.getByRole('button', { name: '重试', exact: true }).click()

  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(filter).toHaveValue('测试用户')
  await expect(page.getByText('第 2 页', { exact: true })).toBeVisible()
  expect(new URL(page.url()).searchParams.get('query')).toBe('测试用户')
  expect(accountRequests).toEqual([
    { query: '', cursor: '' },
    { query: '测试用户', cursor: '' },
    { query: '测试用户', cursor: 'accounts-page-2' },
    { query: '测试用户', cursor: 'accounts-page-2' },
  ])
  expect(requests.get('/api/account/current')).toBe(1)
})

test('@axe 公开页、登录、普通用户 Shell、管理员表格与打开的 dialog 满足 WCAG A/AA', async ({ page }, testInfo) => {
  const colorScheme = testInfo.project.name === 'mobile-320-chromium' ? 'dark' : 'light'
  await page.emulateMedia({ colorScheme })
  await mockAPI(page)

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'AnyTTY Cloud', exact: true })).toBeVisible()
  await assertThemeApplied(page, colorScheme)
  await assertNoAxeViolations(page, '公开页')

  await page.goto('/login')
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
  await assertNoAxeViolations(page, '登录页')

  await page.goto('/app/overview')
  await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
  await assertNoAxeViolations(page, '普通用户 Shell')

  const adminPage = await page.context().newPage()
  await adminPage.emulateMedia({ colorScheme })
  await mockAPI(adminPage, true)
  await adminPage.goto('/app/admin/certificates')
  await expect(adminPage.getByRole('region', { name: '数据表格' }).first()).toBeVisible()
  await assertNoAxeViolations(adminPage, '管理员表格')

  await adminPage.getByRole('button', { name: '上传证书', exact: true }).click()
  await expect(adminPage.getByRole('dialog', { name: '上传证书' })).toBeVisible()
  await assertNoAxeViolations(adminPage, '打开的 dialog')
  await adminPage.close()
})

test('手机底栏与运营抽屉不遮挡内容', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'desktop-chromium')
  await mockAPI(page, true)
  await page.goto('/app/overview')
  const bottom = page.getByRole('navigation', { name: '手机主导航' })
  await bottom.getByRole('link', { name: 'Daemon 管理' }).click()
  await expect(page.getByRole('heading', { name: 'Daemon 管理' })).toBeVisible()
  const menu = page.locator('.menu-button')
  await expect(menu).toHaveAccessibleName('打开导航')
  await expect(menu).toHaveAttribute('aria-controls', 'cloud-mobile-navigation')
  await expect(menu).toHaveAttribute('aria-expanded', 'false')
  if (testInfo.project.name === 'mobile-320-chromium') {
    await assertMinimumHitArea(page, '.menu-button')
    await assertMinimumHitArea(page, '.topbar-account > a')
  }
  await menu.click()
  await expect(menu).toHaveAttribute('aria-expanded', 'true')
  let drawer = page.getByRole('dialog', { name: 'Cloud 导航' })
  await expect(drawer).toHaveAttribute('aria-modal', 'true')
  await expect(drawer.getByRole('button', { name: '关闭导航' })).toBeFocused()
  await expect(page.locator('.workspace')).toHaveAttribute('inert', '')
  expect(await page.evaluate(() => getComputedStyle(document.body).overflow)).toBe('hidden')
  expect(await drawer.getByRole('heading', { level: 2 }).allTextContents()).toEqual(['Infrastructure', 'Account operations', 'Governance'])
  if (testInfo.project.name === 'mobile-320-chromium') {
    await assertMinimumHitArea(page, '.topbar-account > a')
    await assertMinimumHitArea(page, '#cloud-mobile-navigation nav a')
  }
  await page.keyboard.press('Shift+Tab')
  await expect(drawer.getByRole('link', { name: '系统', exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(drawer.getByRole('button', { name: '关闭导航' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(drawer).toHaveCount(0)
  await expect(menu).toHaveAttribute('aria-expanded', 'false')
  await expect(menu).toBeFocused()
  expect(await page.evaluate(() => getComputedStyle(document.body).overflow)).not.toBe('hidden')

  await menu.click()
  drawer = page.getByRole('dialog', { name: 'Cloud 导航' })
  await drawer.getByRole('navigation', { name: '运营管理' }).getByRole('link', { name: '实时连接' }).click()
  await expect(page).toHaveURL(/\/app\/admin\/connections$/)
  await expect(page).toHaveTitle('实时连接 · AnyTTY Cloud')
  await expect(drawer).toHaveCount(0)
  await expect(menu).toHaveAttribute('aria-expanded', 'false')
  await expect(page.locator('#main-content')).toBeFocused()
  await expect(page.getByText('横向滚动查看更多列')).toBeVisible()
  await expect(page.getByRole('region', { name: '数据表格' })).toBeVisible()
  expect(await page.locator('.table-frame th:first-child').evaluate((element) => getComputedStyle(element).position)).toBe('sticky')
  expect(await page.locator('.table-frame th:last-child').evaluate((element) => getComputedStyle(element).position)).toBe('sticky')
  await assertNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('mobile-admin.png'), fullPage: true })
})

test('手机横屏保持导航和主要内容可用', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile-landscape-chromium')
  await mockAPI(page)
  await page.goto('/app/overview')
  expect(page.viewportSize()).toEqual({ width: 844, height: 390 })
  await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: '手机主导航' })).toBeVisible()
  await assertNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('user-overview-landscape.png') })
})

test('统一登录页提供字段约束和明确错误反馈', async ({ page }) => {
  await mockAPI(page, false, true)
  const errors = captureErrors(page)
  await page.goto('/login')
  await assertMinimumHitArea(page, '.auth-panel a')
  const submit = page.getByRole('button', { name: '登录', exact: true })
  await expect(submit).toBeDisabled()
  await page.getByLabel('邮箱或账号').fill('user@anytty.com')
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

async function assertNoHorizontalOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1)
}

async function assertMinimumHitArea(page: Page, selector: string) {
  const boxes = await page.locator(selector).evaluateAll((elements) => elements.flatMap((element) => {
    const style = getComputedStyle(element)
    if (style.display === 'none' || style.visibility === 'hidden') return []
    const box = element.getBoundingClientRect()
    return [{ label: element.getAttribute('aria-label') ?? element.textContent?.trim() ?? element.tagName, width: box.width, height: box.height }]
  }))
  for (const box of boxes) {
    expect(box.width, `${box.label} hit-area width`).toBeGreaterThanOrEqual(44)
    expect(box.height, `${box.label} hit-area height`).toBeGreaterThanOrEqual(44)
  }
}

async function assertNoAxeViolations(page: Page, surface: string) {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()
  const violations = results.violations.map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    targets: violation.nodes.map((node) => node.target),
  }))
  expect(violations, `${surface} 存在 axe violations`).toEqual([])
}

async function assertThemeApplied(page: Page, colorScheme: 'dark' | 'light') {
  const root = page.locator(':root')
  const expected = themeTokens[colorScheme]
  const opposite = themeTokens[colorScheme === 'dark' ? 'light' : 'dark']
  await expect(root).toHaveCSS('color-scheme', colorScheme)
  for (const token of ['--bg', '--surface', '--text'] as const) {
    await expect(root).toHaveCSS(token, expected[token])
    await expect(root).not.toHaveCSS(token, opposite[token])
  }
}
