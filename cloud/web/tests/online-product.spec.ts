import { expect, test, type Page } from '@playwright/test'

const origin = process.env.MUXVIA_CLOUD_ONLINE_ORIGIN

test.describe('Muxvia Cloud 线上普通用户产品', () => {
  test.skip(!origin, '需要显式提供线上地址')
  test.describe.configure({ timeout: 180_000 })

  test('桌面端完成注册、设备命令、订阅支付和权限隔离', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chromium')
    const errors = captureErrors(page)
    const identity = uniqueIdentity(testInfo.project.name)

    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Muxvia Cloud', exact: true })).toBeVisible()
    await expect(page.getByLabel('Muxvia Cloud 产品连接画面')).toContainText('muxvia-cloud-ok')
    await page.getByRole('link', { name: '创建账号' }).first().click()
    await register(page, identity)

    await expect(page).toHaveURL(/\/app\/overview$/)
    await expect(page.getByRole('heading', { name: `你好，${identity.displayName}` })).toBeVisible({ timeout: 30_000 })
    await expect(page.getByRole('navigation', { name: '运营管理' })).toHaveCount(0)

    await page.getByRole('navigation', { name: '用户功能' }).getByRole('link', { name: '我的设备' }).click()
    await page.getByRole('button', { name: '添加设备' }).click()
    await page.getByLabel('设备名称').fill('线上 E2E Mac')
    await page.getByRole('button', { name: '生成命令' }).click()
    const enrollmentDialog = page.getByRole('dialog', { name: '安装命令已生成' })
    await expect(enrollmentDialog).toBeVisible()
    await expect(enrollmentDialog.locator('code')).toContainText('muxvia cloud enroll --controller https://cloud.muxvia.com mxe_')
    await enrollmentDialog.getByRole('button', { name: '完成' }).click()

    await page.getByRole('navigation', { name: '用户功能' }).getByRole('link', { name: '订阅套餐' }).click()
    await page.getByRole('button', { name: '选择专业版' }).click()
    const paymentDialog = page.getByRole('dialog', { name: 'Development 支付确认' })
    await expect(paymentDialog).toContainText('不会发起真实扣款')
    const paymentResponse = page.waitForResponse((response) => response.url().endsWith('/api/commerce/payments/development') && response.request().method() === 'POST', { timeout: 60_000 })
    await paymentDialog.getByRole('button', { name: '确认测试支付' }).click()
    await expect(paymentDialog.getByRole('button', { name: '正在确认' })).toBeDisabled()
    expect((await paymentResponse).ok()).toBe(true)
    await expect(page.locator('.current-subscription')).toContainText('专业版', { timeout: 60_000 })

    await page.getByRole('navigation', { name: '用户功能' }).getByRole('link', { name: '我的订单' }).click()
    await expect(page.getByRole('row').filter({ hasText: 'professional' }).first()).toContainText('已支付', { timeout: 30_000 })
    await page.reload()
    await expect(page).toHaveURL(/\/app\/orders$/)
    await expect(page.getByRole('heading', { name: '我的订单' })).toBeVisible()

    await page.getByRole('navigation', { name: '用户功能' }).getByRole('link', { name: '账号安全' }).click()
    await expect(page.locator('#main-content').getByText(identity.email, { exact: true })).toBeVisible()
    await expect(page.getByText('当前会话', { exact: true })).toBeVisible()

    await page.goto('/app/admin/edges')
    await expect(page).toHaveURL(/\/app\/no-permission$/)
    await expect(page.getByRole('heading', { name: '没有运营管理权限' })).toBeVisible()
    expect(errors).toEqual([])
  })

  test('移动布局注册后使用用户主导航且页面不横向溢出', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'desktop-chromium')
    const errors = captureErrors(page)
    const identity = uniqueIdentity(testInfo.project.name)

    await page.goto('/register')
    await register(page, identity)
    const navigation = page.getByRole('navigation', { name: '手机主导航' })
    await expect(navigation).toBeVisible()
    await navigation.getByRole('link', { name: '我的设备' }).click()
    await expect(page.getByRole('heading', { name: '我的设备' })).toBeVisible()
    await navigation.getByRole('link', { name: '账号安全' }).click()
    await expect(page.locator('#main-content').getByText(identity.email, { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)

    await page.goto('/app/admin/accounts')
    await expect(page).toHaveURL(/\/app\/no-permission$/)
    await expect(page.getByRole('heading', { name: '没有运营管理权限' })).toBeVisible()
    expect(errors).toEqual([])
  })
})

async function register(page: Page, identity: ReturnType<typeof uniqueIdentity>) {
  await page.getByLabel('你的称呼').fill(identity.displayName)
  await page.getByLabel('邮箱').fill(identity.email)
  await page.getByLabel('密码', { exact: true }).fill(identity.password)
  await page.getByRole('button', { name: '创建账号', exact: true }).click()
}

function uniqueIdentity(project: string) {
  const suffix = `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return {
    displayName: project === 'desktop-chromium' ? '桌面验收用户' : '移动验收用户',
    email: `cloud-e2e-${project}-${suffix}@example.com`,
    password: `Muxvia-e2e-${suffix}`,
  }
}

function captureErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}
