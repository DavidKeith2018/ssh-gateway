import { defineConfig } from '@playwright/test'
import { resolve } from 'node:path'
export default defineConfig({
  outputDir: './test-results/demo-run',
  testDir: './demo', testMatch: '*.spec.ts', workers: 1, timeout: 180_000,
  use: { baseURL: 'http://127.0.0.1:19876', locale: 'zh-CN', viewport: { width: 1920, height: 1080 }, launchOptions: process.env.GATEWAY_CHROMIUM ? { executablePath: process.env.GATEWAY_CHROMIUM } : {} },
  webServer: { command: 'go test .. -run ^TestBrowserFixture$ -count=1 -timeout=15m', url: 'http://127.0.0.1:19876', timeout: 120_000, reuseExistingServer: false, env: { GATEWAY_DEMO_FIXTURE: '1', GATEWAY_BROWSER_FIXTURE: resolve('.test-fixture/demo.json'), GATEWAY_BROWSER_PORT: '19876' } },
})
