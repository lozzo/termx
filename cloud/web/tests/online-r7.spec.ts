import { expect, test, type Page } from '@playwright/test'

const origin = process.env.MUXVIA_CLOUD_ONLINE_ORIGIN
const login = process.env.MUXVIA_CLOUD_ONLINE_LOGIN
const password = process.env.MUXVIA_CLOUD_ONLINE_PASSWORD

test.describe('R7 线上运营后台', () => {
  test.skip(!origin || !login || !password, '需要显式提供线上地址和运营账号凭据')

  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('账号').fill(login ?? '')
    await page.getByLabel('密码').fill(password ?? '')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(/\/overview$/)
    await expect(page.getByRole('heading', { name: '总览', exact: true }).last()).toBeVisible()
  })

  test('桌面端固定侧栏连接所有真实管理模块', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chromium')
    const errors = captureErrors(page)
    const navigation = page.getByRole('navigation', { name: '运营模块' })
    await expect(navigation).toBeVisible()

    const modules = [
      ['Edge 管理', '/edges'],
      ['在线 daemon', '/daemons'],
      ['实时连接', '/connections'],
      ['用户与权限', '/accounts'],
      ['套餐', '/plans'],
      ['订阅', '/subscriptions'],
      ['订单与交易', '/orders'],
      ['证书', '/certificates'],
      ['用量与结算', '/usage'],
      ['审计', '/audit'],
      ['系统', '/system'],
    ] as const

    for (const [label, path] of modules) {
      await navigation.getByRole('link', { name: label, exact: true }).click()
      await expect(page).toHaveURL(new RegExp(`${path}$`))
      await expect(page.getByRole('heading', { name: label, exact: true }).last()).toBeVisible()
      await expect(navigation).toBeVisible()
    }

    await navigation.getByRole('link', { name: 'Edge 管理', exact: true }).click()
    const edgeRow = page.getByRole('row').filter({ hasText: 'muxvia-cn1.omscd.com:41102' })
    await expect(edgeRow).toContainText('CN1 Edge')
    await expect(edgeRow).toContainText('CN1')
    await expect(edgeRow).toContainText('在线', { timeout: 30_000 })
    await edgeRow.getByRole('link', { name: '查看' }).click()
    await expect(page.getByRole('heading', { name: 'CN1 Edge', exact: true })).toBeVisible()
    await expect(page.getByText('muxvia-cn1.omscd.com:41102', { exact: true })).toBeVisible()
    await expect(page.getByRole('navigation', { name: '运营模块' })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('online-r7-desktop-edge.png'), fullPage: true })
    expect(errors).toEqual([])
  })

  test('手机端从抽屉逐项进入真实管理模块', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile-chromium')
    const errors = captureErrors(page)
    const modules = [
      ['Edge 管理', '/edges'],
      ['在线 daemon', '/daemons'],
      ['实时连接', '/connections'],
      ['用户与权限', '/accounts'],
    ] as const

    for (const [label, path] of modules) {
      await page.getByRole('button', { name: '打开导航' }).click()
      const navigation = page.getByRole('navigation', { name: '运营模块' })
      await expect(navigation).toBeVisible()
      await navigation.getByRole('link', { name: label, exact: true }).click()
      await expect(page).toHaveURL(new RegExp(`${path}$`))
      await expect(page.getByRole('heading', { name: label, exact: true }).last()).toBeVisible()
      await expect.poll(async () => {
        const box = await page.locator('.sidebar').boundingBox()
        return box ? box.x + box.width : 0
      }).toBeLessThanOrEqual(0)
    }

    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
    await expect(page.getByRole('cell', { name: /^系统管理员/ })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('online-r7-mobile-accounts.png'), fullPage: true })
    expect(errors).toEqual([])
  })
})

function captureErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}
