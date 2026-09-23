import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('普通用户可查看和复制获授权机器的中转密码，撤权后拒绝读取', async ({ page, browser }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const username = 'relay-authorized-reader'
  const accountData = { username, password: 'relay-reader-password', enabled: true, target_ids: [fixture.target.id], tags: [] }
  const created = await page.request.post('/api/users', { data: accountData })
  expect(created.ok()).toBe(true)
  const account = await created.json()
  const userContext = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'], viewport: { width: 1440, height: 1000 } })
  try {
    const userPage = await userContext.newPage()
    await userPage.goto('/')
    await userPage.getByLabel('用户名', { exact: true }).fill(username)
    await userPage.getByLabel('登录密码').fill(accountData.password)
    await userPage.getByRole('button', { name: '进入中转台' }).click()
    const row = userPage.getByRole('row').filter({ hasText: fixture.target.name })
    await row.getByRole('button', { name: '复制连接信息', exact: true }).click()
    const dialog = userPage.getByRole('dialog', { name: '中转连接参数', exact: true })
    await expect(userPage.locator('.copy-toast')).toContainText('连接信息已复制')
    expect(await userPage.evaluate(() => navigator.clipboard.readText())).toContain(`密码：${fixture.target.relay_password}`)
    await expect(dialog).not.toBeVisible()
    const opening = userPage.waitForEvent('popup')
    await row.getByRole('button', { name: '终端', exact: false }).click()
    const terminalPage = await opening
    await expect(terminalPage.getByRole('main', { name: '终端机器页面' })).toBeVisible()
    await expect(terminalPage.getByRole('region', { name: '中转凭证', exact: true })).toHaveCount(0)
    await expect(terminalPage.getByRole('main', { name: '终端机器页面' }).getByRole('button', { name: '复制中转凭证给 AI', exact: true })).toHaveCount(0)
    expect((await userPage.request.post(`/api/targets/${fixture.target.id}/logins/default/credentials`, { data: {} })).status()).toBe(403)
    expect((await page.request.put(`/api/users/${account.id}`, { data: { ...accountData, password: '', target_ids: [] } })).ok()).toBe(true)
    const denied = await userPage.request.post(`/api/targets/${fixture.target.id}/relays/default/credentials`, { data: {} })
    expect(denied.status()).toBe(403)
  } finally {
    await userContext.close()
    await page.request.delete(`/api/users/${account.id}`, { data: {} })
  }
})

test('中转凭证：连接列表可复制，终端页移除凭证区域和弹窗入口', async ({ page, context }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const id = 'copy-relay-accounts'
  const response = await page.request.post('/api/targets', { data: { ...fixture.multi_target, id, name: '复制凭证验收' } })
  expect(response.ok()).toBe(true)
  const result = await response.json()
  const ip = '198.51.100.88'
  expect((await page.request.put('/api/global-ips', { data: { ip, expires_at: new Date(Date.now() + 3600_000).toISOString() } })).ok()).toBe(true)
  try {
    await page.reload()
    const row = page.getByRole('row').filter({ hasText: '复制凭证验收' })
    const dialog = page.getByRole('dialog', { name: '中转连接参数', exact: true })
    const choose = row.getByRole('combobox')
    await expect(choose.locator('option').first()).toContainText('ubuntu · relay-ubuntu-')
    await expect(choose.locator('option').nth(1)).toContainText('root · relay-root-')
    for (const [index, user] of ['ubuntu', 'root'].entries()) {
      await row.getByRole('combobox').selectOption(result.credentials[index].id)
      await row.getByRole('button', { name: '复制连接信息', exact: true }).click()
      await expect(choose).toHaveValue(result.credentials[index].id)
      await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain(result.credentials[index].password)
      await expect(dialog).not.toBeVisible()
      const copied = await page.evaluate(() => navigator.clipboard.readText())
      expect(copied).toContain(`密码：${result.credentials[index].password}`)
      expect(copied).toContain(`（${user}@`)
      expect(copied.split('\n')).toHaveLength(4)
      expect(copied).toContain('凭证有效期：永久有效')
      expect(copied).toContain(result.credentials[index].username)
      expect(copied).toContain('127.0.0.1')
      expect(copied).not.toContain(ip)
      expect(copied).not.toContain('有效至')
      expect(copied).not.toContain(result.credentials[1 - index].password)
      expect(copied).not.toContain(fixture.multi_target.logins[0].target_password)
      expect(copied).not.toContain('BEGIN OPENSSH PRIVATE KEY')
    }
    const popup = page.waitForEvent('popup')
    await row.getByRole('button', { name: '终端', exact: false }).click()
    const machinePage = await popup
    const machine = machinePage.getByRole('main', { name: '终端机器页面' })
    await expect(machine.getByRole('region', { name: '中转凭证', exact: true })).toHaveCount(0)
    await expect(machine.getByRole('button', { name: '复制中转凭证给 AI', exact: true })).toHaveCount(0)
    await expect(machine.getByRole('dialog', { name: '中转连接参数', exact: true })).toHaveCount(0)
    await expect(machine.getByRole('region', { name: '机器硬件信息', exact: true })).toBeVisible()
    await machine.getByRole('button', { name: '查看 SSH 连接信息', exact: true }).click()
    const info = machine.getByRole('dialog', { name: 'SSH 连接信息', exact: true })
    await expect(info).toBeVisible()
    await expect(info.getByRole('button', { name: '复制连接信息' })).toHaveCount(0)
    await info.getByRole('button', { name: '关闭连接信息', exact: true }).click()
    await machinePage.setViewportSize({ width: 390, height: 844 })
    await expect(machine.getByRole('button', { name: '复制中转凭证给 AI', exact: true })).toHaveCount(0)
    await page.screenshot({ path: 'test-results/terminal-without-relay-credentials.png' })
  } finally {
    await page.request.delete(`/api/targets/${id}`, { data: {} })
    await page.request.delete('/api/global-ips', { data: { ip } })
  }
})

test('旧版无法恢复的中转密码不复制空密码，剪贴板失败提供完整手动说明', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  await page.route('**/relays/*/credentials', async route => {
    const response = await route.fetch()
    const data = await response.json()
    data.credential.password = ''
    await route.fulfill({ response, json: data })
  })
  await row.getByRole('button', { name: '复制连接信息', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '中转连接参数', exact: true })
  await expect(dialog.getByRole('alert')).toContainText('无法还原原密码')
  await expect(dialog.getByRole('button', { name: '复制连接信息' })).toBeDisabled()
  await dialog.getByRole('button', { name: '关闭', exact: true }).click()
  await page.unroute('**/relays/*/credentials')
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    document.execCommand = () => false
  })
  await row.getByRole('button', { name: '复制连接信息', exact: true }).click()
  await expect(dialog.getByLabel('完整 AI 连接说明')).toHaveValue(new RegExp(fixture.target.relay_password))
  await expect(dialog.getByRole('alert')).toContainText('无法写入剪贴板')
})
