import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('普通 HTTP 地址可以打开终端、笔记和文件树', async ({ page, baseURL }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.request.post(`${baseURL}/api/login`, { data: { username: 'ssh-admin', password: fixture.admin_password } })
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('http://gateway.test/**', async route => {
    const url = new URL(route.request().url())
    // 此用例模拟终端握手，文件请求仍交给真实测试服务验证。
    url.searchParams.delete('shared')
    const response = await page.request.fetch(`${baseURL}${url.pathname}${url.search}`, {
      method: route.request().method(),
      data: route.request().postDataBuffer() ?? undefined,
      headers: { 'Content-Type': 'application/json' },
    })
    await route.fulfill({ response })
  })
  await page.routeWebSocket('ws://gateway.test/**', socket => {
    socket.send(JSON.stringify({ type: 'connection', message: JSON.stringify({ shared_id: 'http-fixture', mode: 'server', user: fixture.target.user, host: fixture.target.host, port: String(fixture.target.port) }) }))
    socket.send(JSON.stringify({ type: 'ready', message: '已连接' }))
  })
  await page.goto(`http://gateway.test/?window=1&machine=${fixture.target.id}`)
  expect(await page.evaluate(() => isSecureContext)).toBe(false)
  expect(await page.evaluate(() => typeof crypto.randomUUID)).toBe('undefined')
  await expect(page.locator('.terminal-tab.active i.online')).toBeVisible()
  await expect(page.getByRole('tree', { name: '远程文件树' }).locator('.tree-row[title="/"]')).toBeVisible()
  await expect(page.locator('.monaco-editor textarea')).toBeAttached()
  await expect(page.getByText('crypto.randomUUID is not a function', { exact: false })).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: '快捷操作分页' })).toHaveCount(0)
  await page.getByRole('button', { name: '管理命令', exact: true }).click()
  const manager = page.getByRole('dialog', { name: '管理快捷命令' })
  const commandEditor = page.getByRole('dialog', { name: '新增快捷命令', exact: true })
  await manager.getByRole('button', { name: '新增命令' }).click()
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('普通 HTTP 命令')
  await commandEditor.getByLabel('命令内容', { exact: true }).fill('pwd')
  await commandEditor.getByRole('button', { name: '添加命令', exact: true }).click()
  await expect(commandEditor.getByLabel('命令名称', { exact: true })).toHaveCount(0)
  await manager.getByLabel('搜索快捷命令').fill('普通 HTTP 命令')
  await expect(manager.getByRole('table')).toContainText('普通 HTTP 命令')
  await manager.getByRole('button', { name: '删除快捷命令 普通 HTTP 命令', exact: true }).click()
  await expect(manager.getByRole('table')).not.toContainText('普通 HTTP 命令')
  expect(errors).toEqual([])
})
