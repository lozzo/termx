import { expect, test, type Page, type Route } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { Buffer } from 'node:buffer'

const now = '2026-07-27T12:00:00Z'
const periodEnd = '2026-08-27T12:00:00Z'
const provisionSetupCredential = 'P'.repeat(43)
const resetSetupCredential = 'R'.repeat(43)
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
    if (path === '/api/operator/accounts' && route.request().method() === 'GET') return json(route, { accounts: [{ account, roles: ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'], daemon_count: '1', subscription: commerce.subscription, entitlement: commerce.entitlement, usage: commerce.usage }] })
    if (path === '/api/operator/accounts' && route.request().method() === 'POST') return json(route, { account: { account_id: '88888888-8888-4888-8888-888888888888', email: 'new.user@example.com', display_name: '新账号', state: 'ACCOUNT_STATE_PENDING', revision: '1', created_at: now, updated_at: now }, setup_credential: provisionSetupCredential, expires_at: periodEnd }, 201)
    if (path === `/api/operator/accounts/${account.account_id}` && route.request().method() === 'GET') return json(route, { account: { account, roles: ['ACCOUNT_ROLE_USER', 'ACCOUNT_ROLE_ADMIN'], daemon_count: '1', subscription: commerce.subscription, entitlement: commerce.entitlement, usage: commerce.usage } })
    if (path === `/api/operator/accounts/${account.account_id}/reset` && route.request().method() === 'POST') return json(route, { account: { ...account, state: 'ACCOUNT_STATE_PENDING', revision: '2', updated_at: now }, setup_credential: resetSetupCredential, expires_at: periodEnd })
    if (path === `/api/commerce/account/${account.account_id}`) return json(route, commerce)
    if (path === '/api/operator/plans') return json(route, { plans })
    if (path === '/api/operator/subscriptions') return json(route, { subscriptions: [commerce.subscription] })
    if (path === '/api/operator/orders') return json(route, { orders: [pendingOrder] })
    if (path === '/api/operator/usage') return json(route, { accounts: [commerce.usage] })
    if (path === '/api/operator/audit') return json(route, { events: [] })
    return json(route, {})
  })
  return requests
}

async function holdJSONMutation(page: Page, path: string, body: unknown) {
  let release: () => void = () => undefined
  const pending = new Promise<void>((resolve) => { release = () => resolve() })
  await page.route(`**${path}`, async (route) => {
    if (route.request().method() !== 'POST') return route.fallback()
    await pending
    return json(route, body)
  })
  return release
}

async function submitLockedDialog(page: Page, title: string, submitName: string) {
  const dialog = page.getByRole('dialog', { name: title })
  const submit = dialog.locator('footer button').last()
  await expect(submit).toHaveText(submitName)
  await submit.click()
  await expect(submit).toBeDisabled()
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: '关闭', exact: true })).toBeDisabled()

  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await page.locator('.dialog-backdrop').dispatchEvent('mousedown')
  await expect(dialog).toBeVisible()
  return dialog
}

async function finishLockedDialog(page: Page, title: string, submitName: string, resultTitle: string, focusedAction: string, release: () => void) {
  await submitLockedDialog(page, title, submitName)
  release()
  const result = page.getByRole('dialog', { name: resultTitle })
  await expect(result).toBeVisible()
  await expect(result.getByRole('button', { name: focusedAction, exact: true })).toBeFocused()
  expect(await result.evaluate((element) => element.contains(document.activeElement))).toBe(true)
  await expect(result.getByRole('button', { name: '关闭', exact: true })).toBeEnabled()
  await page.keyboard.press('Escape')
  await expect(result).toHaveCount(0)
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

test('从登录或 setup 返回公开首页时恢复 title 与 main focus，query-only 更新不窃取焦点', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page)

  for (const [path, title] of [['/login', '登录 · AnyTTY Cloud'], ['/setup', '设置账号密码 · AnyTTY Cloud']] as const) {
    await page.goto(path)
    await expect(page).toHaveTitle(title)
    await page.getByRole('link', { name: '返回首页', exact: true }).click()

    const main = page.getByRole('main', { name: 'AnyTTY Cloud 公开首页' })
    await expect(page).toHaveTitle('AnyTTY Cloud')
    await expect(main).toBeFocused()

    const loginLink = page.getByRole('link', { name: '登录 Cloud', exact: true })
    await loginLink.focus()
    await page.evaluate(() => {
      window.history.pushState({}, '', '/?source=query-only')
      window.dispatchEvent(new PopStateEvent('popstate'))
    })
    await expect(page).toHaveURL(/\?source=query-only$/)
    await expect(loginLink).toBeFocused()
    await expect(main).not.toBeFocused()
  }
})

test('公开落地页在确认视口保留完整首屏产品信号', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  test.setTimeout(60_000)
  await mockAPI(page)
  const viewports = [
    { width: 1710, height: 982 },
    { width: 390, height: 844 },
    { width: 320, height: 568 },
    { width: 844, height: 390 },
    { width: 844, height: 320 },
  ]

  for (const viewport of viewports) {
    await page.setViewportSize(viewport)
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'AnyTTY Cloud', exact: true })).toBeInViewport({ ratio: 1 })
    await expect(page.locator('.hero-copy > p')).toBeInViewport({ ratio: 1 })
    await expect(page.locator('.hero-actions .button')).toBeInViewport({ ratio: 1 })
    await expect(page.locator('.terminal-window')).toBeInViewport({ ratio: 1 })
    await expect(page.locator('.product-band .eyebrow')).toBeInViewport({ ratio: 1 })
    await expect(page.locator('.hero-copy > p')).toContainText('AnyTTY App 不需要账号')
    await expect(page.locator('.hero-copy > p')).toContainText('扫描目标服务生成的配对二维码')
    await assertMinimumHitArea(page, '.landing-header a, .hero-actions a')
    await assertNoHorizontalOverflow(page)
    await assertNoTextClipping(page, '.hero-copy h1, .hero-statement, .hero-copy > p, .hero-actions .button, .route-stage, .terminal-window header, .terminal-window pre, .terminal-window footer, .product-band .eyebrow')
    const geometry = await page.evaluate(() => {
      const bounds = (selector: string) => {
        const rect = document.querySelector(selector)!.getBoundingClientRect()
        return { top: rect.top, bottom: rect.bottom }
      }
      return {
        hero: bounds('.landing-hero'),
        cta: bounds('.hero-actions .button'),
        product: bounds('.hero-product'),
        terminal: bounds('.terminal-window'),
        productBand: bounds('.product-band'),
        heroOverflow: getComputedStyle(document.querySelector('.landing-hero')!).overflow,
      }
    })
    expect(viewport.height - geometry.productBand.top, `${viewport.width}x${viewport.height} 下一节露出高度`).toBeGreaterThanOrEqual(24)
    for (const [name, bounds] of [['主 CTA', geometry.cta], ['产品可视化', geometry.product], ['终端', geometry.terminal]] as const) {
      expect(bounds.top, `${viewport.width}x${viewport.height} ${name} 顶部未被 hero 裁切`).toBeGreaterThanOrEqual(geometry.hero.top - 1)
      expect(bounds.bottom, `${viewport.width}x${viewport.height} ${name} 底部未被 hero 裁切`).toBeLessThanOrEqual(geometry.hero.bottom + 1)
    }
    if (viewport.width === 844 && viewport.height <= 390) expect(geometry.heroOverflow).toBe('visible')
    await page.screenshot({ path: testInfo.outputPath(`landing-first-viewport-${viewport.width}x${viewport.height}.png`) })
  }

  await page.setViewportSize({ width: 1710, height: 982 })
  await page.goto('/')
  await page.getByRole('link', { name: '如何连接', exact: true }).click()
  await expect(page).toHaveURL(/#connect$/)
  const anchorGeometry = await page.evaluate(() => ({
    headerBottom: document.querySelector('.landing-header')!.getBoundingClientRect().bottom,
    sectionTop: document.querySelector('#connect')!.getBoundingClientRect().top,
    headingTop: document.querySelector('#connect h2')!.getBoundingClientRect().top,
  }))
  expect(anchorGeometry.sectionTop).toBeGreaterThanOrEqual(anchorGeometry.headerBottom - 1)
  expect(anchorGeometry.headingTop).toBeGreaterThanOrEqual(anchorGeometry.headerBottom)
})

test('公开页焦点环在明暗表面均满足非文本对比', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page)
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme })
    await page.goto('/')
    await page.keyboard.press('Tab')
    const colors = await page.evaluate(() => {
      const root = getComputedStyle(document.documentElement)
      const focus = getComputedStyle(document.activeElement!)
      return {
        activeTag: document.activeElement?.tagName,
        outline: focus.outlineColor,
        outlineStyle: focus.outlineStyle,
        surface: root.getPropertyValue('--surface').trim(),
        background: root.getPropertyValue('--bg').trim(),
      }
    })
    expect(colors.activeTag).toBe('A')
    expect(colors.outlineStyle).toBe('solid')
    expect(cssColor(colors.outline).alpha).toBe(1)
    expect(contrastRatio(colors.outline, colors.surface), `${colorScheme} focus ring / surface`).toBeGreaterThanOrEqual(3)
    expect(contrastRatio(colors.outline, colors.background), `${colorScheme} focus ring / background`).toBeGreaterThanOrEqual(3)
  }
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

test('current 500 保留 CloudShell 错误与关联 ID，并可原位重试', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page)
  let available = false
  let attempts = 0
  await page.route('**/api/account/current', async (route) => {
    attempts++
    if (available) return route.fallback()
    return json(route, { code: 'service_unavailable', message: 'private current failure', request_id: 'current-retry-id' }, 500)
  })

  await page.goto('/app/overview')
  const alert = page.getByRole('alert')
  await expect(alert).toContainText('服务暂时不可用，请稍后重试。', { timeout: 10_000 })
  await expect(alert).toContainText('current-retry-id')
  await expect(alert).not.toContainText('private current failure')
  await expect(page).toHaveURL(/\/app\/overview$/)

  available = true
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
  await expect(alert).toHaveCount(0)
  expect(attempts).toBeGreaterThanOrEqual(2)
})

test('logout 失败显示脱敏固定反馈并允许重试', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page)
  let attempts = 0
  let finishFailedRetry: () => void = () => undefined
  let finishSuccessfulRetry: () => void = () => undefined
  const failedRetryPending = new Promise<void>((resolve) => { finishFailedRetry = () => resolve() })
  const successfulRetryPending = new Promise<void>((resolve) => { finishSuccessfulRetry = () => resolve() })
  await page.route('**/api/account/logout', async (route) => {
    attempts++
    if (attempts <= 2) return json(route, { code: 'service_unavailable', message: 'private logout failure', request_id: 'logout-failure-id' }, 500)
    if (attempts === 3) {
      await failedRetryPending
      return json(route, { code: 'service_unavailable', message: 'private retry failure', request_id: 'logout-retry-failure-id' }, 500)
    }
    await successfulRetryPending
    return json(route, {})
  })

  await page.goto('/app/overview')
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '退出登录失败' })
  await expect(dialog).toContainText('无法退出登录，请检查网络后重试。')
  await expect(dialog).not.toContainText('private logout failure')
  await expect(dialog).not.toContainText('logout-failure-id')
  const retry = dialog.getByRole('button', { name: '重试退出', exact: true })
  await expect(retry).toBeFocused()
  expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true)

  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await expect(retry).toBeFocused()

  await retry.click()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: '关闭', exact: true })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: '正在重试', exact: true })).toBeDisabled()
  const pendingStatus = dialog.getByRole('status')
  await expect(pendingStatus).toHaveText('正在重试退出登录。')
  await expect(pendingStatus).toBeFocused()
  expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await page.locator('.dialog-backdrop').dispatchEvent('mousedown')
  await expect(dialog).toBeVisible()

  finishFailedRetry()
  await expect(retry).toBeEnabled()
  await expect(retry).toBeFocused()
  await expect(dialog).not.toContainText('private retry failure')

  await retry.click()
  await expect(pendingStatus).toBeFocused()
  expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true)
  finishSuccessfulRetry()
  await expect(page).toHaveURL(/\/login$/)
  expect(attempts).toBe(4)
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

test('管理员创建账号、复制一次性凭据并重置账号', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium' && testInfo.project.name !== 'mobile-320-chromium')
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await mockAPI(page, true)
  await page.goto('/app/admin/accounts')

  await page.getByRole('button', { name: '创建账号', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: '创建账号' })
  await dialog.getByLabel('邮箱').fill('new.user@example.com')
  await dialog.getByLabel('显示名称').fill('新账号')
  await dialog.getByLabel('创建原因').fill('已审批')
  const provisionRequest = page.waitForRequest((request) => request.url().endsWith('/api/operator/accounts') && request.method() === 'POST')
  await dialog.getByRole('button', { name: '创建账号', exact: true }).click()
  expect((await provisionRequest).postDataJSON()).toEqual({ email: 'new.user@example.com', display_name: '新账号', reason: '已审批' })
  dialog = page.getByRole('dialog', { name: '一次性凭据' })
  await expect(dialog).toContainText('仅展示一次')
  await expect(dialog).toContainText(provisionSetupCredential)
  await dialog.getByRole('button', { name: '复制设置链接', exact: true }).click()
  await expect(dialog.getByRole('button', { name: '已复制设置链接', exact: true })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${new URL(page.url()).origin}/setup#${provisionSetupCredential}`)
  await dialog.getByRole('button', { name: '完成', exact: true }).click()

  await page.getByRole('link', { name: '详情', exact: true }).click()
  await page.getByRole('button', { name: '重置凭据', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '重置账号凭据' })
  await expect(dialog).toContainText('旧密码和全部登录会话立即失效')
  await dialog.getByLabel('操作原因').fill('用户遗失密码')
  await dialog.getByRole('button', { name: '重置凭据', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '一次性凭据' })
  await expect(dialog).toContainText(resetSetupCredential)
  await dialog.getByRole('button', { name: '复制设置链接', exact: true }).click()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${new URL(page.url()).origin}/setup#${resetSetupCredential}`)
  await expect(dialog).toContainText('仅展示一次')
  await assertNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('account-lifecycle.png'), fullPage: true })
})

test('一次性结果 mutations pending 时不能关闭，成功后仍可完成关闭', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page, true)

  const finishProvision = await holdJSONMutation(page, '/api/operator/accounts', {
    account: { account_id: 'new-account', email: 'pending@example.com', display_name: 'Pending User', state: 'ACCOUNT_STATE_PENDING', revision: '1' },
    setup_credential: provisionSetupCredential,
    expires_at: periodEnd,
  })
  await page.goto('/app/admin/accounts')
  await page.getByRole('button', { name: '创建账号', exact: true }).click()
  await page.getByLabel('邮箱').fill('pending@example.com')
  await page.getByLabel('显示名称').fill('Pending User')
  await page.getByLabel('创建原因').fill('approved')
  await finishLockedDialog(page, '创建账号', '创建账号', '一次性凭据', '复制设置链接', finishProvision)

  const finishReset = await holdJSONMutation(page, `/api/operator/accounts/${account.account_id}/reset`, {
    account: { ...account, state: 'ACCOUNT_STATE_PENDING', revision: '2' },
    setup_credential: resetSetupCredential,
    expires_at: periodEnd,
  })
  await page.getByRole('link', { name: '详情', exact: true }).click()
  await page.getByRole('button', { name: '重置凭据', exact: true }).click()
  await page.getByLabel('操作原因').fill('lost password')
  await finishLockedDialog(page, '重置账号凭据', '重置凭据', '一次性凭据', '复制设置链接', finishReset)

  const finishUserEnrollment = await holdJSONMutation(page, '/api/daemons/enroll', {
    enrollment_code: 'mxe_user_pending',
    enroll_command: 'anytty cloud enroll mxe_user_pending',
    expires_at: periodEnd,
  })
  await page.goto('/app/devices')
  await page.getByRole('button', { name: '注册 daemon', exact: true }).click()
  await page.getByLabel('daemon 名称').fill('Pending user daemon')
  await finishLockedDialog(page, '注册 daemon', '生成命令', '注册命令已生成', '复制命令', finishUserEnrollment)

  const finishAdminEnrollment = await holdJSONMutation(page, '/api/operator/daemons', {
    account_id: account.account_id,
    enrollment_code: 'mxe_admin_pending',
    enroll_command: 'anytty cloud enroll mxe_admin_pending',
    expires_at: periodEnd,
  })
  await page.goto('/app/admin/daemons')
  await page.getByRole('button', { name: '注册 daemon', exact: true }).click()
  await page.getByLabel('账号 ID').fill(account.account_id)
  await page.getByLabel('daemon 名称').fill('Pending admin daemon')
  await finishLockedDialog(page, '注册 daemon', '生成命令', '注册命令', '复制', finishAdminEnrollment)

  const finishEdge = await holdJSONMutation(page, '/api/operator/edges', {
    install_command: 'anytty edge install --claim edge-pending',
  })
  await page.goto('/app/admin/edges')
  await page.getByRole('button', { name: '添加 Edge', exact: true }).click()
  await page.getByLabel('名称').fill('Pending Edge')
  await page.getByLabel('区域').fill('test-region')
  await page.getByLabel('域名或域名:端口').fill('edge.example.com:443')
  await finishLockedDialog(page, '添加 Edge', '生成安装命令', '安装命令', '复制', finishEdge)
})

test('套餐与订单 create deferred 失败后关闭复开会清理旧状态', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await mockAPI(page, true)

  let finishPlanFailure: () => void = () => undefined
  const planFailurePending = new Promise<void>((resolve) => { finishPlanFailure = () => resolve() })
  let planAttempts = 0
  await page.route('**/api/operator/plans', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback()
    planAttempts++
    await planFailurePending
    return json(route, { code: 'service_unavailable', message: 'private plan failure', request_id: 'plan-failure-id' }, 503)
  })
  await page.goto('/app/admin/plans')
  await page.getByRole('button', { name: '创建套餐版本', exact: true }).click()
  await page.getByLabel('套餐 ID').fill('slow-plan')
  await page.getByLabel('显示名称').fill('Slow Plan')
  const planDialog = await submitLockedDialog(page, '创建套餐版本', '创建草稿')
  finishPlanFailure()
  await expect(planDialog.getByRole('alert')).toContainText('plan-failure-id')
  await expect(planDialog).not.toContainText('private plan failure')
  await planDialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(planDialog).toHaveCount(0)
  await page.getByRole('button', { name: '创建套餐版本', exact: true }).click()
  const reopenedPlan = page.getByRole('dialog', { name: '创建套餐版本' })
  await expect(reopenedPlan.getByRole('alert')).toHaveCount(0)
  await reopenedPlan.getByRole('button', { name: '取消', exact: true }).click()
  expect(planAttempts).toBe(1)

  let finishOrderFailure: () => void = () => undefined
  const orderFailurePending = new Promise<void>((resolve) => { finishOrderFailure = () => resolve() })
  let orderAttempts = 0
  await page.route('**/api/commerce/order', async (route) => {
    orderAttempts++
    if (orderAttempts === 1) {
      await orderFailurePending
      return json(route, { code: 'service_unavailable', message: 'private order failure', request_id: 'order-failure-id' }, 503)
    }
    const suffix = orderAttempts === 2 ? 'closed' : 'transferred'
    return json(route, {
      order: { order_id: `${suffix}-order`, account_id: account.account_id, plan_id: 'professional', plan_version: '1', status: 'ORDER_STATUS_PENDING', amount: { currency: 'CNY', minor_units: '3900' }, provider: 'manual', idempotency_key: 'slow-order-create', requested_transition: 'SUBSCRIPTION_TRANSITION_ACTIVATE', revision: '1', created_at: now },
      payment_attempt: { payment_attempt_id: `${suffix}-attempt`, order_id: `${suffix}-order`, account_id: account.account_id, provider: 'manual', status: 'PAYMENT_ATTEMPT_STATUS_PENDING', revision: '1', created_at: now, updated_at: now },
    })
  })
  await page.goto('/app/admin/orders')
  await page.getByRole('button', { name: '创建订单', exact: true }).click()
  await page.getByLabel('账号 ID').fill(account.account_id)
  await page.getByLabel('套餐 ID').fill('professional')
  await page.getByLabel('幂等键').fill('slow-order-create')
  const orderDialog = await submitLockedDialog(page, '创建订单', '创建')
  finishOrderFailure()
  await expect(orderDialog.getByRole('alert')).toContainText('order-failure-id')
  await expect(orderDialog).not.toContainText('private order failure')
  await orderDialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '创建订单', exact: true }).click()
  const reopenedOrder = page.getByRole('dialog', { name: '创建订单' })
  await expect(reopenedOrder.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('dialog', { name: '订单已创建' })).toHaveCount(0)

  await reopenedOrder.getByRole('button', { name: '创建', exact: true }).click()
  const created = page.getByRole('dialog', { name: '订单已创建' })
  await expect(created).toContainText('closed-order')
  await expect(created.getByRole('button', { name: '关闭', exact: true })).toBeEnabled()
  await expect(created.getByRole('button', { name: '录入支付结果', exact: true })).toBeEnabled()
  expect(await created.evaluate((element) => element.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(created).toHaveCount(0)

  await page.getByRole('button', { name: '创建订单', exact: true }).click()
  const cleanOrder = page.getByRole('dialog', { name: '创建订单' })
  await expect(cleanOrder).not.toContainText('closed-order')
  await cleanOrder.getByRole('button', { name: '创建', exact: true }).click()
  const transferred = page.getByRole('dialog', { name: '订单已创建' })
  await expect(transferred).toContainText('transferred-order')
  await transferred.getByRole('button', { name: '录入支付结果', exact: true }).click()
  const paymentDialog = page.getByRole('dialog', { name: '录入支付事件' })
  await expect(paymentDialog.getByLabel('订单 ID')).toHaveValue('transferred-order')
  await expect(paymentDialog.getByLabel('支付尝试 ID')).toHaveValue('transferred-attempt')
  await expect(transferred).toHaveCount(0)
  expect(orderAttempts).toBe(3)
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

test('@axe 公开页、Cloud Shell、订单搜索、表格控件与打开的 dialog 满足 WCAG A/AA', async ({ page }, testInfo) => {
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

  await page.goto('/setup')
  await expect(page.getByRole('heading', { name: '设置账号密码' })).toBeVisible()
  await assertNoAxeViolations(page, '账号 setup 页')

  await page.goto('/app/overview')
  await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
  await assertNoAxeViolations(page, '普通用户 Shell')

  await page.goto('/app/subscription')
  await expect(page.getByRole('group', { name: '计费周期' })).toBeVisible()
  await assertMinimumHitArea(page, '.segmented button')
  await assertNoHorizontalOverflow(page)
  await assertNoAxeViolations(page, '订阅计费周期')

  const adminPage = await page.context().newPage()
  await adminPage.emulateMedia({ colorScheme })
  await mockAPI(adminPage, true)

  await adminPage.goto('/app/admin/orders')
  const orderSearch = adminPage.getByLabel('搜索订单')
  await expect(orderSearch).toHaveAttribute('id', 'order-search')
  const searchResponse = adminPage.waitForResponse((response) => {
    const url = new URL(response.url())
    return url.pathname === '/api/operator/orders' && url.searchParams.get('query') === 'development'
  })
  await orderSearch.fill('development')
  await adminPage.getByRole('button', { name: '查询', exact: true }).click()
  expect((await searchResponse).ok()).toBe(true)
  await expect(adminPage.getByRole('region', { name: '数据表格' })).toBeVisible()
  await assertMinimumHitArea(adminPage, '.table-link')
  await assertNoHorizontalOverflow(adminPage)
  await assertNoAxeViolations(adminPage, '管理员订单搜索与表格链接')

  await adminPage.goto('/app/admin/certificates')
  await expect(adminPage.getByRole('region', { name: '数据表格' }).first()).toBeVisible()
  await assertMinimumHitArea(adminPage, '.table-button, .table-select')
  await assertNoHorizontalOverflow(adminPage)
  await assertNoAxeViolations(adminPage, '管理员表格')

  await adminPage.getByRole('button', { name: '上传证书', exact: true }).click()
  await expect(adminPage.getByRole('dialog', { name: '上传证书' })).toBeVisible()
  await assertNoAxeViolations(adminPage, '打开的 dialog')
  await adminPage.keyboard.press('Escape')
  await adminPage.goto('/app/admin/accounts')
  const shortTableLink = adminPage.getByRole('link', { name: '详情', exact: true })
  await expect(shortTableLink).toBeVisible()
  const shortTableLinkRow = adminPage.getByRole('row').filter({ has: shortTableLink })
  const [shortTableLinkBox, shortTableLinkRowBox] = await Promise.all([shortTableLink.boundingBox(), shortTableLinkRow.boundingBox()])
  expect(shortTableLinkBox).not.toBeNull()
  expect(shortTableLinkBox!.width, '详情链接命中区域宽度').toBeGreaterThanOrEqual(44)
  expect(shortTableLinkBox!.height, '详情链接命中区域高度').toBeGreaterThanOrEqual(44)
  expect(shortTableLinkRowBox).not.toBeNull()
  expect(shortTableLinkRowBox!.height, '详情链接所在表格行高度').toBeLessThanOrEqual(64)
  await shortTableLink.focus()
  await expect(shortTableLink).toBeFocused()
  await assertNoHorizontalOverflow(adminPage)
  await assertNoAxeViolations(adminPage, '管理员账号短表格链接')
  await adminPage.getByRole('button', { name: '创建账号', exact: true }).click()
  await expect(adminPage.getByRole('dialog', { name: '创建账号' })).toBeVisible()
  await assertNoAxeViolations(adminPage, '创建账号 dialog')
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

test('公开 setup 失败后保留输入并可成功重试', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium' && testInfo.project.name !== 'mobile-320-chromium')
  await mockAPI(page)
  let attempts = 0
  await page.route('**/api/account/setup/redeem', async (route) => {
    attempts++
    const request = route.request().postDataJSON()
    expect(request).toEqual({ setup_credential: 'A'.repeat(43), new_password: 'replacement-password' })
    if (attempts === 1) return json(route, { code: 'setup_invalid', message: '一次性凭据无效或已过期。', request_id: 'setup-retry-id' }, 400)
    return json(route, { account: { ...account, state: 'ACCOUNT_STATE_ACTIVE', revision: '2' }, roles: ['ACCOUNT_ROLE_USER'], session: { session_id: 'setup-session', access_expires_at: periodEnd, refresh_expires_at: periodEnd } })
  })
  const setupCredential = 'A'.repeat(43)
  await page.goto(`/setup#${setupCredential}`)
  await expect(page.getByLabel('一次性凭据')).toHaveValue(setupCredential)
  await expect.poll(() => page.evaluate(() => window.location.hash)).toBe('')
  await page.getByLabel('新密码', { exact: true }).fill('replacement-password')
  await page.getByLabel('确认新密码').fill('replacement-password')
  await page.getByRole('button', { name: '设置密码', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('请向管理员申请重置')
  await expect(page.getByRole('alert')).toContainText('setup-retry-id')
  await expect(page.getByLabel('一次性凭据')).toHaveValue('A'.repeat(43))
  await expect(page.getByLabel('新密码', { exact: true })).toHaveValue('replacement-password')
  await page.getByRole('button', { name: '设置密码', exact: true }).click()
  await expect(page).toHaveURL(/\/app\/overview$/)
  await expect(page.getByRole('heading', { name: '你好，测试用户' })).toBeVisible()
  expect(attempts).toBe(2)
  await assertNoHorizontalOverflow(page)
  await page.screenshot({ path: testInfo.outputPath('setup-success.png'), fullPage: true })
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

async function assertNoTextClipping(page: Page, selector: string) {
  const clipped = await page.locator(selector).evaluateAll((elements) => elements.flatMap((element) => {
    const style = getComputedStyle(element)
    const horizontal = ['hidden', 'clip'].includes(style.overflowX) && element.scrollWidth > element.clientWidth + 1
    const vertical = ['hidden', 'clip'].includes(style.overflowY) && element.scrollHeight > element.clientHeight + 1
    return horizontal || vertical ? [element.className || element.tagName] : []
  }))
  expect(clipped, '首屏文本被容器裁切').toEqual([])
}

function cssColor(value: string) {
  const hex = value.match(/^#([\da-f]{6})$/i)?.[1]
  if (hex) return { channels: [0, 2, 4].map((offset) => Number.parseInt(hex.slice(offset, offset + 2), 16)), alpha: 1 }
  const components = value.match(/[\d.]+/g)?.map(Number) ?? []
  if (components.length < 3) throw new Error(`Unsupported CSS color: ${value}`)
  return { channels: components.slice(0, 3), alpha: components[3] ?? 1 }
}

function contrastRatio(foreground: string, background: string) {
  const luminance = (value: string) => cssColor(value).channels
    .map((channel) => channel / 255)
    .map((channel) => channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4)
    .reduce((sum, channel, index) => sum + channel * [.2126, .7152, .0722][index], 0)
  const values = [luminance(foreground), luminance(background)].sort((left, right) => right - left)
  return (values[0] + .05) / (values[1] + .05)
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
