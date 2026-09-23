import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('机器连接错误统一弹窗且持续失败不重复弹出', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.route(/\/api\/targets\/[^/]+\/(files|hardware|resources)(\?|$)/, route => route.fulfill({ status: 502, json: { error: '测试连接失败' } }))
  await page.routeWebSocket(/\/terminal/, socket => {
    socket.send(JSON.stringify({ type: 'error', message: '测试终端失败' }))
  })
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const dialog = page.locator('dialog[open]').filter({ hasText: '连接失败' })
  await expect(dialog).toHaveCount(1)
  await expect(page.locator('.tree-error, .hardware-error')).toHaveCount(0)
  await expect(page.locator('.machine-connection')).toContainText('连接失败')
  await dialog.getByRole('button', { name: '知道了' }).click()
  // 跨过资源轮询周期，持续失败仍不重新弹窗。
  await page.waitForTimeout(5500)
  await expect(dialog).toHaveCount(0)
  await page.getByRole('button', { name: '新建终端', exact: true }).click()
  await expect(dialog).toHaveCount(1)
})
