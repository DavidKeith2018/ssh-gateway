import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('关闭机器页面释放全部中转和直连终端，缓存恢复可重新连接', async ({ page, context }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const active = async () => (await (await page.request.get('/api/targets/summary')).json()).active
  await expect.poll(active).toBe(0)
  for (const selection of ['default', 'server:default']) {
    const machine = await context.newPage()
    await machine.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id) + '&connection=' + selection)
    await expect(machine.locator('.terminal-tab i.online')).toHaveCount(1)
    await machine.getByRole('button', { name: '新建终端', exact: true }).click()
    await expect(machine.locator('.terminal-tab i.online')).toHaveCount(2)
    await expect.poll(active).toBe(2)
    await machine.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true })))
    await expect.poll(active).toBe(0)
    await machine.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })))
    await expect.poll(active).toBe(2)
    await machine.close()
    await expect.poll(active).toBe(0)
  }
})
