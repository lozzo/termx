import { defineConfig, devices } from '@playwright/test'

const onlineOrigin = process.env.MUXVIA_CLOUD_ONLINE_ORIGIN
const onlineControllerIP = process.env.MUXVIA_CLOUD_ONLINE_CONTROLLER_IP
const resolverArgs = onlineOrigin && onlineControllerIP
  ? [`--host-resolver-rules=MAP ${new URL(onlineOrigin).hostname} ${onlineControllerIP}`]
  : []

export default defineConfig({
  testDir: './tests',
  outputDir: '../../.artifacts/cloud-web-playwright',
  fullyParallel: false,
  reporter: 'line',
  use: {
    baseURL: onlineOrigin ?? 'http://127.0.0.1:4177',
    trace: 'retain-on-failure',
    launchOptions: { args: resolverArgs },
  },
  webServer: onlineOrigin ? undefined : { command: 'npm run dev -- --host 127.0.0.1', port: 4177, reuseExistingServer: false },
  projects: [
    { name: 'desktop-chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'tablet-chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 768, height: 1024 } } },
    { name: 'mobile-chromium', use: { ...devices['Pixel 7'], viewport: { width: 390, height: 844 } } },
  ],
})
