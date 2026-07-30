import { expect, test, type Page } from '@playwright/test'

const origin = process.env.ANYTTY_CLOUD_ONLINE_ORIGIN
const accountLogin = process.env.ANYTTY_CLOUD_E2E_LOGIN
const accountPassword = process.env.ANYTTY_CLOUD_E2E_PASSWORD

test.describe('AnyTTY Cloud 线上普通用户产品', () => {
  test.skip(!origin || !accountLogin || !accountPassword, '需要显式提供线上地址和既有测试账号')
  test.describe.configure({ timeout: 180_000 })

  test('桌面端完成登录、设备命令、订阅支付和权限隔离', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chromium')
    const errors = captureErrors(page)

    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'AnyTTY Cloud', exact: true })).toBeVisible()
    await expect(page.getByLabel('扫码配对后的 AnyTTY Cloud 连接路径')).toContainText('anytty-cloud-ok')
    await page.getByRole('link', { name: '登录 Cloud' }).click()
    await login(page, accountLogin!, accountPassword!)

    await expect(page).toHaveURL(/\/app\/overview$/)
    await expect(page.getByRole('heading', { name: /^你好，/ })).toBeVisible({ timeout: 30_000 })
    await expect(page.getByRole('navigation', { name: '运营管理' })).toHaveCount(0)

    await page.getByRole('navigation', { name: '用户功能' }).getByRole('link', { name: 'Daemon 管理' }).click()
    await page.getByRole('button', { name: '注册 daemon' }).click()
    await page.getByLabel('daemon 名称').fill('线上 E2E Mac')
    await page.getByRole('button', { name: '生成命令' }).click()
    const enrollmentDialog = page.getByRole('dialog', { name: '注册命令已生成' })
    await expect(enrollmentDialog).toBeVisible()
    await expect(enrollmentDialog.locator('code')).toContainText('anytty cloud enroll --controller https://cloud.anytty.com mxe_')
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
    await expect(page.locator('#main-content').getByText(accountLogin!, { exact: true })).toBeVisible()
    await expect(page.getByText('当前会话', { exact: true })).toBeVisible()

    await page.goto('/app/admin/edges')
    await expect(page).toHaveURL(/\/app\/no-permission$/)
    await expect(page.getByRole('heading', { name: '没有运营管理权限' })).toBeVisible()
    expect(errors).toEqual([])
  })

  test('移动布局登录后使用用户主导航且页面不横向溢出', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'desktop-chromium')
    const errors = captureErrors(page)

    await page.goto('/login')
    await login(page, accountLogin!, accountPassword!)
    const navigation = page.getByRole('navigation', { name: '手机主导航' })
    await expect(navigation).toBeVisible()
    await navigation.getByRole('link', { name: 'Daemon 管理' }).click()
    await expect(page.getByRole('heading', { name: 'Daemon 管理' })).toBeVisible()
    await navigation.getByRole('link', { name: '账号安全' }).click()
    await expect(page.locator('#main-content').getByText(accountLogin!, { exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)

    await page.goto('/app/admin/accounts')
    await expect(page).toHaveURL(/\/app\/no-permission$/)
    await expect(page.getByRole('heading', { name: '没有运营管理权限' })).toBeVisible()
    expect(errors).toEqual([])
  })
})

async function login(page: Page, account: string, password: string) {
  await page.getByLabel('邮箱或账号').fill(account)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/app\/overview$/, { timeout: 30_000 })
}

function captureErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}
