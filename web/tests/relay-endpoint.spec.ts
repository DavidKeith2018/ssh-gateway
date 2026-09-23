import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('全局中转地址入口、监听端口提示、保存及恢复自动识别', async ({ page }, testInfo) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  const original = await (await page.request.get('/api/relay-endpoint')).json()
  try {
    await page.getByRole('button', { name: '中转访问设置', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '中转访问设置', exact: true })
    await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
    const { ssh_port: sshPort } = await (await page.request.get('/api/me')).json()
    const listenHint = dialog.getByText(`当前服务监听 SSH 端口为 ${sshPort}。`, { exact: true })
    await expect(listenHint).toBeVisible()
    await expect(dialog).toContainText('用于通过中转服务连接目标服务器')
    await expect(dialog).not.toContainText('复制给 AI')
    await dialog.getByLabel('中转访问域名/IP').fill('ssh.example.com')
    await dialog.getByLabel('中转访问端口').fill('443')
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect(dialog.getByRole('status')).toContainText('已保存')
    await expect(listenHint).toBeVisible()
    await page.reload()
    const sidebarEntry = page.getByRole('button', { name: '中转访问设置', exact: true })
    await sidebarEntry.click()
    await expect(dialog.getByLabel('中转访问域名/IP')).toHaveValue('ssh.example.com')
    await expect(dialog.getByLabel('中转访问端口')).toHaveValue('443')
    await expect(listenHint).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('relay-settings-light.png') })
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('button', { name: '切换黑色主题', exact: true }).click()
    for (const key of ['Enter', 'Space']) {
      await sidebarEntry.focus()
      await expect(sidebarEntry).toBeFocused()
      await sidebarEntry.press(key)
      await expect(listenHint).toBeVisible()
      await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
      await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    }
    await sidebarEntry.hover()
    await page.screenshot({ path: testInfo.outputPath('relay-entry-dark.png') })
    await sidebarEntry.click()
    await expect(listenHint).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('relay-settings-dark.png') })
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await sidebarEntry.click()
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(sidebarEntry).toBeHidden()
    await expect(listenHint).toBeInViewport()
    await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeInViewport()
    await page.screenshot({ path: testInfo.outputPath('relay-settings-mobile.png') })
    await dialog.getByLabel('中转访问域名/IP').fill('')
    await dialog.getByLabel('中转访问端口').fill('')
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect(dialog.getByRole('status')).toContainText('已保存')
    await expect(listenHint).toBeVisible()
  } finally {
    await page.request.put('/api/relay-endpoint', { data: original })
  }
})

test('普通用户的监听端口仅显示文本', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.route('**/api/me', async route => {
    const response = await route.fetch()
    if (response.ok()) {
      const me = await response.json()
      await route.fulfill({ response, json: { ...me, id: 'readonly-user', username: '普通用户', is_admin: false } })
    } else {
      await route.fulfill({ response })
    }
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.locator('.sidebar-bottom').getByText(/^SSH 监听端口 \d+$/)).toBeVisible()
  await expect(page.getByRole('button', { name: /^SSH 监听端口/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '中转访问设置', exact: true })).toHaveCount(0)
  await expect(page.getByRole('dialog', { name: '中转访问设置', includeHidden: true })).toHaveCount(0)
})

// Keep navigation visible for these workflows; default collapse is covered by compact-ui.spec.ts.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (localStorage.getItem('ssh-gateway:sidebar-collapsed') === null)
      localStorage.setItem('ssh-gateway:sidebar-collapsed', 'false')
  })
})
