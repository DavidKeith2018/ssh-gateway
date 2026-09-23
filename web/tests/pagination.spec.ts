import { test, expect, type Page, type Locator } from '@playwright/test'
import { readFileSync } from 'node:fs'
const fixture = () => JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
async function login(page: Page) {
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture().admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
}

// 延迟一次真实轮询，检查分页不变灰，且旧结果不会覆盖用户的新页面。
async function nextPageDuringRefresh(page: Page, pagination: Locator, targetID = '') {
  let release!: () => void, started!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  const polling = new Promise<void>(resolve => { started = resolve })
  let held = false
  const handler = async (route: import('@playwright/test').Route) => {
    const url = new URL(route.request().url())
    if (!held && url.searchParams.get('target_id') === targetID && url.searchParams.get('page') === '1') {
      held = true
      const response = await route.fetch()
      started()
      await gate
      await route.fulfill({ response })
    } else await route.continue()
  }
  await expect(pagination).toHaveAttribute('aria-busy', 'false')
  await page.route('**/api/mappings?**', handler)
  try {
    await polling
    await expect(pagination).toHaveAttribute('aria-busy', 'false')
    await expect(pagination.getByLabel('每页条数')).toBeEnabled()
    await pagination.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(pagination.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
    const response = page.waitForResponse(r => {
      const url = new URL(r.url())
      return url.pathname === '/api/mappings' && url.searchParams.get('target_id') === targetID && url.searchParams.get('page') === '1'
    })
    release(); await response
    await expect(pagination.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
  } finally { release(); await page.unroute('**/api/mappings?**', handler) }
}

test('连接分页、完整选项、独立终端、请求乱序和失败重试', async ({ page, context }) => {
  test.setTimeout(120_000)
  await login(page)
  const data = fixture()
  const ids: string[] = []
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  try {
    for (let n = 0; n < 61; n++) {
      const id = `paging-${String(n).padStart(3, '0')}`
      const response = await page.request.post('/api/targets', { data: { ...data.target, id, name: `分页机器 ${n}`, relay_user: id, tags: n === 60 ? ['末页标签'] : ['分页测试'] } })
      expect(response.ok(), await response.text()).toBeTruthy(); ids.push(id)
    }
    await page.reload()
    const pagination = page.getByRole('navigation', { name: '连接分页', exact: true })
    const rows = page.locator('.connection-list:visible tbody tr')
    await expect(rows).toHaveCount(20)
    const stats = await (await page.request.get('/api/targets/summary')).json()
    await expect(pagination).toContainText(`共 ${stats.total} 条`)
    await pagination.getByLabel('每页条数').selectOption('10')
    await expect(rows).toHaveCount(10)
    await expect(pagination).toContainText('…')
    const next = page.waitForResponse(r => { const u = new URL(r.url()); return u.pathname === '/api/targets' && u.searchParams.get('page') === '2' && u.searchParams.get('page_size') === '10' })
    await pagination.getByRole('button', { name: '下一页', exact: true }).click()
    expect((await (await next).json()).items).toHaveLength(10)
    await expect(pagination.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await page.getByRole('button', { name: '帐号管理', exact: false }).click()
    await page.getByRole('button', { name: '添加帐号', exact: false }).click()
    const accountDialog = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '添加帐号' }) })
    await expect(accountDialog.getByLabel('分页机器 60', { exact: true })).toBeVisible()
    await accountDialog.getByRole('button', { name: '取消', exact: true }).click()
    await page.getByRole('button', { name: /SSH 连接/ }).click()
    await expect(pagination.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await page.locator('.content').getByLabel('筛选机器标签').selectOption('末页标签')
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('分页机器 60')
    await expect(pagination.getByRole('button', { name: '第 1 页', exact: true })).toHaveAttribute('aria-current', 'page')
    const detached = await context.newPage()
    await detached.goto('/?window=1&machine=paging-060')
    await expect(detached.getByRole('main', { name: '终端机器页面' })).toBeVisible()
    await detached.close()
    await page.locator('.content').getByLabel('筛选机器标签').selectOption('')
    await expect(rows).toHaveCount(10)
    // 搜索较早的响应最后到达，也不能覆盖新筛选结果。
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    await page.route('**/api/targets?**', async route => {
      if (new URL(route.request().url()).searchParams.get('q') === '分页机器 1') { const response = await route.fetch(); await gate; await route.fulfill({ response }) }
      else await route.continue()
    })
    const oldRequest = page.waitForRequest(r => new URL(r.url()).searchParams.get('q') === '分页机器 1')
    await page.getByLabel('搜索连接').fill('分页机器 1'); await oldRequest
    await page.getByLabel('搜索连接').fill('分页机器 60')
    await expect(rows).toHaveCount(1); await expect(rows.first()).toContainText('分页机器 60')
    const oldResponse = page.waitForResponse(r => new URL(r.url()).searchParams.get('q') === '分页机器 1')
    release(); await oldResponse
    await expect(rows.first()).toContainText('分页机器 60')
    await page.unroute('**/api/targets?**')
    await page.getByLabel('搜索连接').fill(''); await expect(rows).toHaveCount(10)
    await page.route('**/api/targets?**', route => route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: '分页读取失败' }) }), { times: 1 })
    await pagination.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('分页读取失败')
    await expect(pagination.getByRole('button', { name: '第 1 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(rows).toHaveCount(10)
    await page.getByRole('alert').getByRole('button', { name: '重试', exact: true }).click()
    await expect(pagination.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await pagination.getByLabel('跳转页码').fill('4')
    await pagination.getByRole('button', { name: '跳转', exact: true }).click()
    await expect(pagination.getByRole('button', { name: '第 4 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await page.setViewportSize({ width: 540, height: 900 })
    await pagination.scrollIntoViewIfNeeded()
    const paginationBox = (await pagination.boundingBox())!
    expect(paginationBox.x).toBeGreaterThanOrEqual(0)
    expect(paginationBox.x + paginationBox.width).toBeLessThanOrEqual(540)
    await page.screenshot({ path: 'test-results/pagination-narrow.png' })
    await expect(pagination.getByRole('button', { name: '跳转', exact: true })).toBeInViewport()
    expect(errors).toEqual([])
  } finally { for (const id of ids) await page.request.delete(`/api/targets/${id}`, { data: {} }) }
})

test('账号、白名单、日志、映射总览和抽屉服务端分页', async ({ page }) => {
  test.setTimeout(120_000)
  await login(page)
  const data = fixture(), userIDs: string[] = [], mappingIDs: string[] = [], ips: string[] = []
  try {
    for (let n = 0; n < 21; n++) {
      const account = await page.request.post('/api/users', { data: { username: `paging-user-${String(n).padStart(2, '0')}`, password: 'paging-password-123', enabled: true, target_ids: [], tags: [] } })
      expect(account.ok(), await account.text()).toBeTruthy(); userIDs.push((await account.json()).id)
      const ip = `192.0.2.${n + 1}`
      expect((await page.request.put('/api/global-ips', { data: { ip, expires_at: new Date(Date.now() + 3600_000).toISOString() } })).ok()).toBeTruthy(); ips.push(ip)
      const mapping = await page.request.post('/api/mappings', { data: { target_id: data.target.id, name: `分页映射 ${n}`, direction: 'local', service_host: '127.0.0.1', service_port: 3000, listen_port: 24000 + n, scope: 'loopback', auto_start: false } })
      expect(mapping.ok(), await mapping.text()).toBeTruthy(); mappingIDs.push((await mapping.json()).id)
    }
    page.on('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: '帐号管理' }).click()
    const accountPage = page.getByRole('navigation', { name: '账号分页', exact: true })
    await accountPage.getByRole('button', { name: '下一页', exact: true }).click()
    const accountRows = page.locator('section:visible .connection-list tbody tr')
    await expect(accountRows).toHaveCount(1)
    await accountRows.getByRole('button', { name: '删除', exact: true }).click()
    await expect(accountPage.getByRole('button', { name: '第 1 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(accountRows).toHaveCount(20)
    await page.getByRole('button', { name: /SSH 连接/ }).click()
    await page.getByRole('button', { name: '全局 IP 白名单', exact: true }).click()
    const ipPage = page.getByRole('navigation', { name: '白名单分页', exact: true })
    await ipPage.getByRole('button', { name: '下一页', exact: true }).click()
    await expect(ipPage.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await page.getByRole('dialog', { name: '全局 IP 白名单', exact: true }).getByRole('button', { name: '关闭', exact: true }).click()
    const logs = await (await page.request.get('/api/events?page=2&page_size=20')).json()
    expect(logs.page).toBe(2)
    expect(logs.items).toHaveLength(20)
    await page.getByRole('button', { name: '端口映射', exact: false }).first().click()
    const overview = page.getByRole('region', { name: '端口映射总览' })
    const mappingPage = page.getByRole('navigation', { name: '映射分页', exact: true })
    await nextPageDuringRefresh(page, mappingPage)
    await expect(overview.locator('tbody tr')).toHaveCount(1)
    await expect(overview.locator('.mapping-stats').first()).toContainText('21')
    await overview.locator('tbody tr').getByRole('button', { name: data.target.name, exact: true }).click()
    const drawer = page.getByRole('dialog', { name: '端口映射配置' })
    const ownPage = drawer.getByRole('navigation', { name: '机器映射分页' })
    await nextPageDuringRefresh(page, ownPage, data.target.id)
    await expect(drawer.locator('.mapping-rule')).toHaveCount(1)
    await drawer.getByRole('button', { name: '关闭端口映射', exact: true }).click()
    await expect(mappingPage.getByRole('button', { name: '第 2 页', exact: true })).toHaveAttribute('aria-current', 'page')
    await overview.locator('tbody tr').getByRole('button', { name: data.target.name, exact: true }).click()
    await ownPage.getByRole('combobox', { name: '每页条数' }).selectOption('10')
    await expect(drawer.locator('.mapping-rule')).toHaveCount(10)
    await page.request.delete(`/api/mappings/${mappingIDs.pop()}`, { data: {} })
    await expect(drawer.getByRole('heading', { name: '映射规则 · 20' })).toBeVisible()
    await expect(ownPage).toBeVisible()
    await page.request.delete(`/api/mappings/${mappingIDs.pop()}`, { data: {} })
    await expect(ownPage).toHaveCount(0)
    await expect(drawer.locator('.mapping-rule')).toHaveCount(19)
  } finally {
    for (const id of mappingIDs) await page.request.delete(`/api/mappings/${id}`, { data: {} })
    for (const id of userIDs) await page.request.delete(`/api/users/${id}`, { data: {} })
    for (const ip of ips) await page.request.delete('/api/global-ips', { data: { ip } })
  }
})

test('快捷命令全局共享、普通用户只读和范围隔离', async ({ page, browser }) => {
  await login(page)
  const data = fixture()
  const accountResponse = await page.request.post('/api/users', { data: { username: 'paging-reader', password: 'paging-reader-password', enabled: true, target_ids: [data.target.id], tags: [] } })
  const account = await accountResponse.json()
  const created = await (await page.request.post('/api/shortcuts', { data: { name: '共享只读命令', command: 'pwd', targetId: '*' } })).json()
  const userContext = await browser.newContext()
  try {
    const user = await userContext.newPage()
    await user.goto('/'); await user.getByLabel('用户名', { exact: true }).fill('paging-reader'); await user.getByLabel('登录密码').fill('paging-reader-password'); await user.getByRole('button', { name: '进入中转台' }).click()
    const opening = user.waitForEvent('popup')
    await user.getByRole('row').filter({ hasText: data.target.name }).getByRole('button', { name: '终端' }).click()
    const terminalPage = await opening
    const quick = terminalPage.getByRole('region', { name: '终端快捷操作', exact: true })
    await expect(quick.getByRole('navigation', { name: '快捷操作分页' })).toHaveCount(0)
    await expect(quick.getByRole('button', { name: '共享只读命令', exact: true })).toBeEnabled()
    await expect(quick.getByRole('button', { name: '管理命令', exact: true })).toHaveCount(0)
    expect((await user.request.put(`/api/shortcuts/${created.id}`, { data: { name: '篡改', command: 'ls', targetId: '*' } })).status()).toBe(403)
    await user.reload()
    const stored = await (await user.request.get('/api/shortcuts?q=' + encodeURIComponent('共享只读命令'))).json()
    expect(stored.total).toBe(1)
  } finally { await userContext.close(); await page.request.delete(`/api/users/${account.id}`, { data: {} }); await page.request.delete(`/api/shortcuts/${created.id}`, { data: {} }) }
})
