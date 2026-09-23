import { defineConfig } from '@playwright/test'
import { resolve } from 'node:path'
const baseURL = `http://127.0.0.1:${process.env.GATEWAY_BROWSER_PORT || '18080'}`

export default defineConfig({
  testDir: './tests',
  timeout: 60_000,
  workers: 1,
  use: {
    locale: 'zh-CN',
    baseURL,
    viewport: { width: 1440, height: 1000 },
    launchOptions: process.env.GATEWAY_CHROMIUM ? { executablePath: process.env.GATEWAY_CHROMIUM } : {},
  },
  webServer: {
    command: 'go test .. -run ^TestBrowserFixture$ -count=1 -timeout=10m',
    url: baseURL,
    timeout: 120_000,
    reuseExistingServer: false,
    env: { GATEWAY_BROWSER_FIXTURE: resolve('.test-fixture/connection.json') },
  },
})
