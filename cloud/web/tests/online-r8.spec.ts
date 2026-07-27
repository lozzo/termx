import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

const origin = process.env.MUXVIA_CLOUD_ONLINE_ORIGIN
const login = process.env.MUXVIA_CLOUD_ONLINE_LOGIN
const password = readCredential()
const certificateFile = process.env.MUXVIA_CLOUD_ONLINE_EDGE_CERTIFICATE_FILE
const privateKeyFile = process.env.MUXVIA_CLOUD_ONLINE_EDGE_PRIVATE_KEY_FILE
const profileName = process.env.MUXVIA_CLOUD_ONLINE_CERTIFICATE_PROFILE ?? 'CN1 Edge 公网证书'
const edgeEndpoint = process.env.MUXVIA_CLOUD_ONLINE_EDGE_ENDPOINT ?? 'muxvia-cn1.omscd.com:41102'

test.describe('R8 线上证书自动更新', () => {
  test.skip(
    !origin || !login || !password || !certificateFile || !privateKeyFile,
    '需要显式提供线上地址、运营账号凭据和 Edge 证书文件',
  )
  test.describe.configure({ timeout: 180_000 })

  test('运营页面上传、绑定并观察在线 Edge 应用证书', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-chromium')
    const errors = captureErrors(page)
    await loginOperator(page)
    const loaded = page.waitForResponse((response) => {
      const url = new URL(response.url())
      return url.pathname === '/api/operator/certificates' && response.request().method() === 'GET'
    })
    await page.goto('/app/admin/certificates')
    expect((await loaded).ok()).toBe(true)
    await expect(page.getByRole('heading', { name: '证书', exact: true }).last()).toBeVisible()

    const existing = page.getByRole('row').filter({ hasText: profileName }).first()
    const replacing = await existing.count() > 0
    if (replacing) {
      await existing.getByRole('button', { name: '替换' }).click()
    } else {
      await page.getByRole('button', { name: '上传证书', exact: true }).click()
    }

    const dialog = page.getByRole('dialog', { name: replacing ? `替换 ${profileName}` : '上传证书' })
    if (!replacing) await dialog.getByLabel('档案名称').fill(profileName)
    await dialog.getByLabel('证书链文件').setInputFiles(certificateFile ?? '')
    await dialog.getByLabel('私钥文件').setInputFiles(privateKeyFile ?? '')
    const uploadResponse = page.waitForResponse((response) => {
      const url = new URL(response.url())
      return url.pathname.startsWith('/api/operator/certificates') && ['POST', 'PUT'].includes(response.request().method())
    })
    await dialog.getByRole('button', { name: replacing ? '替换并自动更新' : '上传证书', exact: true }).click()
    const uploaded = await uploadResponse
    expect(uploaded.ok()).toBe(true)
    expect(await uploaded.text()).not.toMatch(/PRIVATE KEY|certificateChainPem|privateKeyPem/i)
    await expect(dialog).toBeHidden()
    await expect(page.getByRole('row').filter({ hasText: profileName }).first()).toBeVisible()

    const edgeRow = page.getByRole('cell', { name: edgeEndpoint, exact: true }).locator('..')
    const profileSelect = edgeRow.getByRole('combobox')
    await expect(profileSelect).toBeVisible()
    await profileSelect.selectOption({ label: profileName })
    const save = edgeRow.getByRole('button', { name: '保存', exact: true })
    if (await save.isEnabled()) {
      const bindResponse = page.waitForResponse((response) => {
        const url = new URL(response.url())
        return url.pathname.endsWith('/certificate') && response.request().method() === 'POST'
      })
      await save.click()
      const bound = await bindResponse
      expect(bound.ok()).toBe(true)
      expect(await bound.text()).not.toMatch(/PRIVATE KEY|certificateChainPem|privateKeyPem/i)
    }

    await expect.poll(async () => {
      await page.reload()
      const row = page.getByRole('cell', { name: edgeEndpoint, exact: true }).locator('..')
      return await row.innerText()
    }, { timeout: 90_000, intervals: [1_000, 2_000, 3_000] }).toMatch(/已应用[\s\S]*r(\d+)\s*\/\s*r\1/)

    const html = await page.locator('body').innerText()
    expect(html).not.toMatch(/BEGIN (?:RSA |EC )?PRIVATE KEY|certificateChainPem|privateKeyPem/i)
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
    await page.screenshot({ path: testInfo.outputPath('online-r8-certificate-applied.png'), fullPage: true })
    expect(errors).toEqual([])
  })
})

async function loginOperator(page: Page) {
  await page.goto('/login')
  await page.getByLabel('账号').fill(login ?? '')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/app\/overview$/, { timeout: 30_000 })
}

function readCredential(): string {
  const inline = process.env.MUXVIA_CLOUD_ONLINE_PASSWORD
  if (inline) return inline
  const file = process.env.MUXVIA_CLOUD_ONLINE_PASSWORD_FILE
  return file ? readFileSync(file, 'utf8').trim() : ''
}

function captureErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}
