import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

async function leaveMachine(page: Page) {
  page.once('dialog', dialog => dialog.accept())
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
}

test('机器页：收藏树、Monaco 多文档、传输、多终端、主题与离开', async ({
  page,
}) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.stack || e.message))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  await expect(machine).toBeVisible()
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  const tree = machine.getByRole('tree', { name: '远程文件树' })
  await expect(tree.locator('.tree-row[title="/"]')).toBeVisible()
  await expect(tree.locator('.tree-row[title="/etc"]')).toBeVisible()
  await expect(tree.locator('.tree-row[title="/var"]')).toBeVisible()
  await expect(
    machine.getByRole('region', { name: '机器硬件信息' }),
  ).toContainText('Fixture CPU')
  await expect(
    machine.getByRole('region', { name: '机器硬件信息' }),
  ).toContainText('测试 Linux 1.0')
  await expect(
    machine.getByRole('region', { name: '机器硬件信息' }),
  ).not.toContainText('中转账号')
  await machine.getByRole('button', { name: '查看 SSH 连接信息' }).click()
  const connection = machine.getByRole('dialog', { name: 'SSH 连接信息' })
  await expect(connection).toContainText(fixture.target.user)
  await expect(connection).not.toContainText(fixture.target.target_password)
  await page.screenshot({ path: 'test-results/machine-connection-info.png' })
  await connection.getByRole('button', { name: '关闭连接信息' }).click()
  await page.screenshot({ path: 'test-results/machine-system-tree.png' })
  await machine.getByRole('button', { name: '定位家目录' }).click()

  await expect(tree.getByText('app.yaml', { exact: true })).toBeVisible()
  const listing = await page.request.post(`/api/targets/${fixture.target.id}/files`, { data: { op: 'list' } })
  expect(listing.ok()).toBe(true)
  const root = (await listing.json()).path
  await tree.getByText('app.yaml', { exact: true }).click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '加入收藏' }).click()
  await expect(machine.locator('.favorites')).toContainText('app.yaml')
  await tree.getByText('app.yaml', { exact: true }).dblclick()
  await expect(machine.getByRole('tab', { name: /app.yaml/ })).toBeVisible()
  const editor = machine.locator('.monaco-editor textarea').first()
  await editor.focus()
  await page.keyboard.press('Control+Home')
  await page.keyboard.type('# local draft\n')
  await expect(machine.locator('.editor-status')).toContainText(
    '1 个文件未保存',
  )
  await tree.getByText('README.md', { exact: true }).dblclick()
  await expect(machine.getByRole('tab', { name: /README.md/ })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type('\nsecond draft')
  await expect(machine.locator('.editor-status')).toContainText(
    '2 个文件未保存',
  )
  await machine.getByRole('tab', { name: /app.yaml/ }).click()
  await expect(machine.locator('.view-lines')).toContainText('# local draft')
  await machine.getByRole('button', { name: '切换主题', exact: true }).click()
  await expect(machine).toHaveAttribute('data-theme', 'dark')
  await expect(machine.locator('.view-lines')).toContainText('# local draft')
  await machine
    .getByRole('button', { name: '保存 Ctrl+S', exact: true })
    .click()
  await expect(machine.locator('.editor-status')).toContainText(
    '1 个文件未保存',
  )
  const read = await page.request.post(
    `/api/targets/${fixture.target.id}/files`,
    { data: { op: 'read', path: `${root}/app.yaml` } },
  )
  expect((await read.json()).content).toContain('# local draft')
  await machine.getByRole('button', { name: '关闭文件 README.md' }).click()
  const close = machine.getByRole('dialog').filter({ hasText: '文件尚未保存' })
  await expect(close).toBeVisible()
  await close.getByRole('button', { name: '取消', exact: true }).click()
  await machine.getByRole('button', { name: '新建终端', exact: true }).click()
  await expect(
    machine.getByRole('tab', { name: '终端 2', exact: true }),
  ).toBeVisible()
  const terminal = machine.locator('.machine-terminal:visible')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await terminal
    .locator('.xterm-helper-textarea')
    .pressSequentially('second-terminal')
  await terminal.locator('.xterm-helper-textarea').press('Enter')
  await expect
    .poll(() => terminal.locator('.xterm-screen').innerText())
    .toContain('second-terminal')
  await machine.getByRole('tab', { name: '终端 1', exact: true }).click()
  await expect(
    machine.locator('.machine-terminal:visible .xterm-screen'),
  ).not.toContainText('second-terminal')
  await expect(machine.locator('.resource-metrics > div').filter({ hasText: 'CPU 使用率' }).locator('dd')).not.toContainText('—', {
    timeout: 15000,
  })
  await machine.getByRole('button', { name: '定位家目录' }).click()
  await tree.getByText('app.yaml', { exact: true }).click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '新建文件', exact: true }).click()
  const create = machine.getByRole('dialog').filter({
    has: page.getByRole('heading', { name: '新建文件', exact: true }),
  })
  await create.getByLabel('名称', { exact: true }).fill('新建.txt')
  await create.getByRole('button', { name: '新建', exact: true }).click()
  await expect(tree.getByText('新建.txt', { exact: true })).toBeVisible()
  await tree.getByText('新建.txt', { exact: true }).click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '上传文件到此目录' }).click()
  await machine.locator('input[type=file]').setInputFiles({
    name: '传输.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('中文文件传输\n'),
  })
  await expect(tree.getByText('传输.txt', { exact: true })).toBeVisible()
  await tree.getByText('传输.txt', { exact: true }).click({ button: 'right' })
  const downloading = page.waitForEvent('download')
  await machine.getByRole('menuitem', { name: '下载文件', exact: true }).click()
  const download = await downloading
  expect(await download.failure()).toBeNull()
  expect(readFileSync((await download.path())!, 'utf8')).toBe('中文文件传输\n')
  await tree.getByText('传输.txt', { exact: true }).click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '删除', exact: true }).click()
  await machine.getByRole('button', { name: '确认删除', exact: true }).click()
  await expect(tree.getByText('传输.txt', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: 'test-results/machine-dark.png' })
  await machine.getByRole('button', { name: '切换主题', exact: true }).click()
  await page.screenshot({ path: 'test-results/machine-light.png' })
  await page.setViewportSize({ width: 900, height: 700 })
  await expect(machine.locator('.machine-header')).toBeInViewport()
  await expect(machine.getByRole('button', { name: '查看 SSH 连接信息' })).toBeInViewport()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect(machine.getByRole('button', { name: '关闭', exact: true })).toHaveCount(0)
  await page.screenshot({ path: 'test-results/machine-narrow.png' })
  const confirming = page.waitForEvent('dialog')
  const reload = page.reload({ timeout: 5000 }).catch(() => null)
  const confirm = await confirming
  expect(confirm.type()).toBe('beforeunload')
  await confirm.dismiss()
  await reload
  await expect(machine.locator('.editor-status')).toContainText('1 个文件未保存')
  await leaveMachine(page)
  await expect(machine).toHaveCount(0)
  expect(errors).toEqual([])
})

test('保存期间的新编辑保留、冲突取消和收藏持久化', async ({ page }) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' }),
    tree = machine.getByRole('tree', { name: '远程文件树' })
  await machine.getByRole('button', { name: '定位家目录' }).click()
  await tree.getByText('app.yaml', { exact: true }).click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '加入收藏' }).click()
  await tree.getByText('app.yaml', { exact: true }).dblclick()
  await expect(machine.getByRole('tab', { name: /app.yaml/ })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  const editor = machine.locator('.monaco-editor textarea').first(),
    path = await machine
      .getByRole('tab', { name: /app.yaml/ })
      .getAttribute('title')
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type('\nfirst-save')
  let submitted!: () => void, release!: () => void
  const submittedPromise = new Promise<void>((r) => (submitted = r)),
    releasePromise = new Promise<void>((r) => (release = r))
  const filesRoute = /\/api\/targets\/[^/]+\/files(?:\?|$)/
  await page.route(filesRoute, async (route) => {
    if (route.request().postDataJSON()?.op === 'write') {
      submitted()
      await releasePromise
    }
    await route.continue()
  })
  await machine
    .getByRole('button', { name: '保存 Ctrl+S', exact: true })
    .click()
  await submittedPromise
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type('\nnew-unsaved')
  release()
  await expect(
    machine.getByRole('button', { name: '保存 Ctrl+S', exact: true }),
  ).toBeEnabled()
  await expect(machine.locator('.editor-status')).toContainText(
    '1 个文件未保存',
  )
  await page.unroute(filesRoute)
  const read = await page.request.post(
    `/api/targets/${fixture.target.id}/files`,
    { data: { op: 'read', path } },
  )
  const remote = await read.json()
  expect(remote.content).not.toContain('new-unsaved')
  await page.request.post(`/api/targets/${fixture.target.id}/files`, {
    data: {
      op: 'write',
      path,
      content: 'external content\n',
      version: remote.version,
    },
  })
  await machine
    .getByRole('button', { name: '保存 Ctrl+S', exact: true })
    .click()
  const conflict = machine
    .getByRole('dialog')
    .filter({ hasText: '文件保存冲突' })
  await expect(conflict).toBeVisible()
  await conflict.getByRole('button', { name: '取消', exact: true }).click()
  await expect(machine.locator('.view-lines')).toContainText('new-unsaved')
  await expect(machine.locator('.editor-status')).toContainText(
    '1 个文件未保存',
  )
  await leaveMachine(page)
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await expect(tree.locator('.tree-row[title="/etc"]')).toBeVisible()
  await expect(machine.locator('.favorites')).toContainText('app.yaml')
  await machine
    .locator('.favorites')
    .getByRole('button', { name: 'app.yaml', exact: true })
    .click()
  await expect(tree.getByText('app.yaml', { exact: true })).toBeVisible()
  await tree.getByText('app.yaml', { exact: true }).dblclick()
  await expect(machine.locator('.view-lines')).toContainText('external content')
  await leaveMachine(page)
})

test('笔记默认打开、记录持久化、单行操作与刷新保护', async ({ page }) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  const tabs = machine.getByRole('tablist', { name: '文件标签' })
  const local = tabs.getByRole('tab', { name: /笔记/ })
  const refresh = machine.getByRole('button', {
    name: '刷新当前文档',
    exact: true,
  })
  const save = machine.getByRole('button', { name: '保存 Ctrl+S', exact: true })
  await expect(local).toHaveAttribute('aria-selected', 'true')
  await expect(refresh).toBeEnabled()
  await expect(save).toHaveCount(0)
  await expect(
    machine.getByRole('button', { name: '关闭文件 笔记' }),
  ).toHaveCount(0)
  const editor = machine.locator('.monaco-editor textarea').first()
  await editor.focus()
  await page.keyboard.press('Control+A')
  await page.keyboard.type('# Local note persistence')
  await expect(save).toBeVisible()
  const tabBox = (await local.boundingBox())!,
    refreshBox = (await refresh.boundingBox())!,
    saveBox = (await save.boundingBox())!
  expect(Math.abs(tabBox.y - saveBox.y)).toBeLessThan(10)
  expect(refreshBox.x).toBeLessThan(saveBox.x)
  await save.click()
  await expect(save).toHaveCount(0)
  const url = `/api/targets/${fixture.target.id}/notes`
  let stored = await (
    await page.request.post(url, { data: { op: 'read' } })
  ).json()
  expect(stored.content).toBe('# Local note persistence')
  expect(stored.path).toBeUndefined()
  await machine.getByRole('button', { name: '定位家目录' }).click()
  await machine
    .getByRole('tree')
    .getByText('app.yaml', { exact: true })
    .dblclick()
  await expect(tabs.getByRole('tab').nth(0)).toContainText('笔记')
  await expect(tabs.getByRole('tab').nth(1)).toContainText('app.yaml')
  await local.click()
  await expect(machine.locator('.view-lines')).toContainText(
    'Local note persistence',
  )
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type(' unsaved')
  await refresh.click()
  const dialog = machine.getByRole('dialog').filter({ hasText: '刷新当前文档' })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(machine.locator('.view-lines')).toContainText('unsaved')
  await page.request.post(url, {
    data: { op: 'write', content: 'external note', version: stored.version },
  })
  await refresh.click()
  await dialog.getByRole('button', { name: '放弃修改并刷新' }).click()
  await expect(machine.locator('.view-lines')).toContainText('external note')
  await expect(save).toHaveCount(0)
  // 刷新失败保留草稿。
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type(' keep draft')
  await page.route('**/api/targets/*/notes', (route) =>
    route.fulfill({ status: 500, json: { error: '读取失败' } }),
  )
  await refresh.click()
  await dialog.getByRole('button', { name: '放弃修改并刷新' }).click()
  await expect(machine.locator('.file-message.error')).toBeVisible()
  await expect(machine.locator('.view-lines')).toContainText('keep draft')
  await expect(save).toBeVisible()
  await page.unroute('**/api/targets/*/notes')
  await editor.focus()
  await page.keyboard.press('Control+s')
  await expect(save).toHaveCount(0)
  await expect(
    machine.getByRole('button', { name: '保存中…', exact: true }),
  ).toHaveCount(0)
  await machine.getByRole('button', { name: '定位系统根目录' }).click()
  await expect(
    machine.getByRole('tree').locator('.tree-row[title="/etc"]'),
  ).toBeVisible()
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.press('Enter')
  await expect(save).toBeEnabled()
  await page.screenshot({ path: 'test-results/machine-local-note.png' })
  await page.keyboard.press('Control+s')
  await expect(machine.locator('.editor-status')).toContainText('已保存')
  await leaveMachine(page)
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  await expect(local).toHaveAttribute('aria-selected', 'true')
  await expect(machine.locator('.view-lines')).toContainText(
    'external note keep draft',
  )
  await expect(tabs.getByRole('tab')).toHaveCount(1)
  await leaveMachine(page)
})

test('终端快捷操作：当前标签、命令管理与关闭状态', async ({ page }) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  const inputs: string[][] = []
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('websocket', (socket) => {
    if (!socket.url().includes('/terminal')) return
    const sent: string[] = []
    inputs.push(sent)
    socket.on('framesent', (event) => {
      try {
        const message = JSON.parse(String(event.payload))
        if (message.type === 'input') sent.push(message.data)
      } catch {}
    })
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const existing = await (await page.request.get('/api/shortcuts?page_size=100')).json()
  for (const item of existing.items) await page.request.delete(`/api/shortcuts/${item.id}`, { data: {} })
  for (const item of [{ name: '当前目录', command: 'pwd' }, { name: '文件列表', command: 'ls -lah' }, { name: '磁盘空间', command: 'df -h' }]) {
    expect((await page.request.post('/api/shortcuts', { data: { ...item, targetId: '*' } })).ok()).toBeTruthy()
  }
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  const quick = machine.getByRole('region', {
    name: '终端快捷操作',
    exact: true,
  })
  await expect(
    quick.getByRole('button', { name: '当前目录', exact: true }),
  ).toBeEnabled()
  await quick.getByRole('button', { name: '当前目录', exact: true }).click()
  await expect.poll(() => inputs[0]).toContain('pwd')
  expect(inputs[0]).not.toContain('pwd\r')
  await page.keyboard.press('Enter')
  await expect.poll(() => inputs[0].at(-1)).toBe('\r')
  await machine.getByRole('button', { name: '新建终端', exact: true }).click()
  await expect(
    quick.getByRole('button', { name: '文件列表', exact: true }),
  ).toBeEnabled()
  await quick.getByRole('button', { name: '文件列表', exact: true }).click()
  await expect.poll(() => inputs[1]).toContain('ls -lah')
  expect(inputs[1]).not.toContain('ls -lah\r')
  expect(inputs[0]).not.toContain('ls -lah')
  const count = inputs[1].length
  await quick.getByRole('button', { name: '清屏', exact: true }).click()
  expect(inputs[1]).toHaveLength(count)
  await quick.getByRole('button', { name: '管理命令', exact: true }).click()
  const manager = machine.getByRole('dialog', { name: '管理快捷命令' })
  const commandEditor = machine.getByRole('dialog', { name: /^(新增|编辑)快捷命令$/ })
  await manager.getByRole('button', { name: '新增命令' }).click()
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('测试命令')
  await commandEditor.getByLabel('命令内容', { exact: true }).fill('echo quick-test')
  await commandEditor.getByRole('button', { name: '添加命令', exact: true }).click()
  await manager
    .getByRole('button', { name: '编辑快捷命令 测试命令', exact: true })
    .click()
  await commandEditor
    .getByLabel('命令内容', { exact: true })
    .fill('echo quick-edited')
  await commandEditor.getByRole('button', { name: '保存命令', exact: true }).click()
  await manager
    .getByRole('button', { name: '删除快捷命令 磁盘空间', exact: true })
    .click()
  await manager.getByRole('button', { name: '关闭快捷命令管理' }).click()
  await quick.getByRole('button', { name: '测试命令', exact: true }).click()
  await expect.poll(() => inputs[1]).toContain('echo quick-edited')
  expect(inputs[1]).not.toContain('echo quick-edited\r')
  expect(inputs[0]).not.toContain('echo quick-edited')
  await expect(machine.locator('.xterm-helper-textarea:visible')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect.poll(() => inputs[1].at(-1)).toBe('\r')
  await expect(
    quick.getByRole('button', { name: '磁盘空间', exact: true }),
  ).toHaveCount(0)
  await page.screenshot({ path: 'test-results/machine-quick-actions.png' })
  await machine.getByRole('button', { name: '关闭终端 2', exact: true }).click()
  await expect(quick.getByRole('button', { name: '测试命令', exact: true })).toBeEnabled()
  await machine.getByRole('button', { name: '关闭终端 1', exact: true }).click()
  await expect(
    quick.getByRole('button', { name: '测试命令', exact: true }),
  ).toBeDisabled()
  await expect(quick.getByRole('button', { name: '中断', exact: true })).toHaveCount(0)
  await machine.getByRole('button', { name: '新建终端', exact: true }).first().click()
  await expect(
    quick.getByRole('button', { name: '测试命令', exact: true }),
  ).toBeEnabled()
  await leaveMachine(page)
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  await expect(
    quick.getByRole('button', { name: '测试命令', exact: true }),
  ).toHaveAttribute('title', 'echo quick-edited')
  await expect(
    quick.getByRole('button', { name: '磁盘空间', exact: true }),
  ).toHaveCount(0)
  await leaveMachine(page)
  expect(errors).toEqual([])
})

test('快捷命令所属机器、全部机器与服务端持久化', async ({ page }) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'SSH 连接', exact: true }),
  ).toBeVisible()
  const other = {
    ...fixture.target,
    id: 'shortcut-other',
    name: '快捷命令第二机器',
    relay_user: 'shortcut-other',
  }
  const created = await page.request.post('/api/targets', { data: other })
  expect(created.ok(), await created.text()).toBeTruthy()
  const existing = await (await page.request.get('/api/shortcuts?page_size=100')).json()
  for (const item of existing.items) await page.request.delete(`/api/shortcuts/${item.id}`, { data: {} })
  expect((await page.request.post('/api/shortcuts', { data: { name: '机器专属命令', command: 'echo stored', targetId: fixture.target.id } })).ok()).toBeTruthy()
  await page.reload()
  const machine = page.getByRole('main', { name: '终端机器页面' })
  const quick = machine.getByRole('region', {
    name: '终端快捷操作',
    exact: true,
  })
  const manager = machine.getByRole('dialog', { name: '管理快捷命令' })
  const commandEditor = machine.getByRole('dialog', { name: /^(新增|编辑)快捷命令$/ })
  const enter = async (name: string) => {
    await page.goto('/?window=1&machine=' + encodeURIComponent(name === other.name ? other.id : fixture.target.id))
    await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  }
  const leave = async () => {
    await leaveMachine(page)
  }
  await enter(fixture.target.name)
  await expect(
    quick.getByRole('button', { name: '机器专属命令', exact: true }),
  ).toBeVisible()
  await expect(
    quick.getByRole('button', { name: '当前目录', exact: true }),
  ).toHaveCount(0)
  await quick.getByRole('button', { name: '管理命令', exact: true }).click()
  await manager.getByRole('button', { name: '新增命令' }).click()
  await expect(commandEditor.getByLabel('选择机器', { exact: true })).toHaveValue(
    fixture.target.id,
  )
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('仅第二机器')
  await commandEditor.getByLabel('命令内容', { exact: true }).fill('echo other-only')
  await commandEditor.getByLabel('选择机器', { exact: true }).selectOption(other.id)
  await commandEditor.getByRole('button', { name: '添加命令', exact: true }).click()
  await manager.getByRole('button', { name: '新增命令' }).click()
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('通用命令')
  await commandEditor.getByLabel('命令内容', { exact: true }).fill('echo shared')
  await commandEditor.getByRole('radio', { name: '全局', exact: true }).check()
  await commandEditor.getByRole('button', { name: '添加命令', exact: true }).click()
  await manager
    .getByRole('button', { name: '编辑快捷命令 仅第二机器', exact: true })
    .click()
  await expect(commandEditor.getByLabel('选择机器', { exact: true })).toHaveValue(
    other.id,
  )
  await page.screenshot({ path: 'test-results/machine-shortcut-scope.png' })
  await commandEditor.getByRole('button', { name: '取消', exact: true }).click()
  await manager.getByRole('button', { name: '关闭快捷命令管理' }).click()
  await expect(
    quick.getByRole('button', { name: '仅第二机器', exact: true }),
  ).toHaveCount(0)
  await expect(
    quick.getByRole('button', { name: '通用命令', exact: true }),
  ).toBeVisible()
  await leave()
  await enter(other.name)
  await expect(
    quick.getByRole('button', { name: '仅第二机器', exact: true }),
  ).toBeVisible()
  await expect(
    quick.getByRole('button', { name: '通用命令', exact: true }),
  ).toBeVisible()
  await expect(
    quick.getByRole('button', { name: '机器专属命令', exact: true }),
  ).toHaveCount(0)
  await quick.getByRole('button', { name: '管理命令', exact: true }).click()
  await manager
    .getByRole('button', { name: '编辑快捷命令 仅第二机器', exact: true })
    .click()
  await commandEditor
    .getByLabel('选择机器', { exact: true })
    .selectOption(fixture.target.id)
  await commandEditor.getByRole('button', { name: '保存命令', exact: true }).click()
  await manager.getByRole('button', { name: '关闭快捷命令管理' }).click()
  await expect(
    quick.getByRole('button', { name: '仅第二机器', exact: true }),
  ).toHaveCount(0)
  await leave()
  await enter(fixture.target.name)
  await expect(
    quick.getByRole('button', { name: '仅第二机器', exact: true }),
  ).toBeVisible()
  await expect(
    quick.getByRole('button', { name: '机器专属命令', exact: true }),
  ).toBeVisible()
  await leave()
  expect(
    (await page.request.delete(`/api/targets/${other.id}`, { data: {} })).ok(),
  ).toBeTruthy()
})

test('大量快捷命令：紧凑列表、搜索筛选、分页与按需编辑', async ({ page }) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'SSH 连接', exact: true }),
  ).toBeVisible()
  const existing = await (await page.request.get('/api/shortcuts?page_size=100')).json()
  for (const item of existing.items) await page.request.delete(`/api/shortcuts/${item.id}`, { data: {} })
  for (let index = 0; index < 95; index++) {
    const response = await page.request.post('/api/shortcuts', { data: {
      name: `运维命令 ${String(index + 1).padStart(3, '0')}`,
      command: `echo task-${index + 1}${index === 94 ? ' ' + 'long-command-'.repeat(40) : ''}`,
      targetId: index % 2 === 0 ? fixture.target.id : '*',
    } })
    expect(response.ok(), await response.text()).toBeTruthy()
  }
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  await machine.getByRole('button', { name: '管理命令', exact: true }).click()
  const manager = machine.getByRole('dialog', { name: '管理快捷命令' })
  const commandEditor = machine.getByRole('dialog', { name: /^(新增|编辑)快捷命令$/ })
  const rows = manager
    .getByRole('table', { name: '快捷命令列表' })
    .locator('tbody tr')
  await expect(commandEditor.getByLabel('命令名称', { exact: true })).toHaveCount(0)
  await expect(rows).toHaveCount(20)
  const box = (await rows.first().boundingBox())!
  expect(box.height).toBeLessThanOrEqual(40)
  await page.screenshot({ path: 'test-results/machine-shortcut-compact.png' })
  await manager.getByRole('button', { name: '下一页' }).click()
  await expect(rows.first()).toContainText('运维命令 021')
  await manager.getByLabel('搜索快捷命令').fill('task-95')
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText('运维命令 095')
  await expect(
    manager.getByRole('button', { name: '上一页' }),
  ).toBeDisabled()
  await manager
    .getByRole('button', { name: '编辑快捷命令 运维命令 095', exact: true })
    .click()
  await expect(commandEditor.getByLabel('命令内容', { exact: true })).toHaveValue(
    /long-command-/,
  )
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('已修改的命令')
  await commandEditor.getByRole('button', { name: '保存命令', exact: true }).click()
  await expect(commandEditor.getByLabel('命令名称', { exact: true })).toHaveCount(0)
  await expect(rows.first()).toContainText('已修改的命令')
  await manager.getByLabel('搜索快捷命令').fill('')
  await manager.getByLabel('筛选适用范围').selectOption('target:*')
  await expect(manager.getByRole('navigation', { name: '快捷命令分页' })).toContainText(
    '共 47 条',
  )
  await expect(rows.first()).toContainText('运维命令 002')
  await manager.getByLabel('搜索快捷命令').fill('不存在的命令')
  await expect(
    manager.getByText('没有匹配的命令', { exact: true }),
  ).toBeVisible()
  await manager.getByLabel('搜索快捷命令').fill('')
  await manager.getByLabel('筛选适用范围').selectOption('')
  const pagination = manager.getByRole('navigation', { name: '快捷命令分页' })
  await expect(pagination).toContainText('共 95 条')
  await pagination.getByRole('button', { name: '第 5 页', exact: true }).click()
  await expect(rows).toHaveCount(15)
  for (let n = 81; n <= 94; n++) {
    await manager.getByRole('button', { name: `删除快捷命令 运维命令 ${String(n).padStart(3, '0')}`, exact: true }).click()
    await expect(rows).toHaveCount(95 - n)
  }
  await manager.getByRole('button', { name: '删除快捷命令 已修改的命令', exact: true }).click()
  await expect(rows).toHaveCount(20)
  await expect(pagination.getByRole('button', { name: '第 4 页', exact: true })).toHaveAttribute('aria-current', 'page')
  await page.setViewportSize({ width: 620, height: 700 })
  await manager.getByRole('button', { name: '新增命令' }).click()
  await expect(
    commandEditor.getByRole('button', { name: '添加命令', exact: true }),
  ).toBeInViewport()
  const dialogBox = (await commandEditor.boundingBox())!
  expect(dialogBox.width).toBeLessThanOrEqual(620)
  expect(dialogBox.height).toBeLessThanOrEqual(700)
  await page.screenshot({
    path: 'test-results/machine-shortcut-compact-narrow.png',
  })
  await commandEditor.getByRole('button', { name: '取消', exact: true }).click()
  await expect(commandEditor.getByLabel('命令名称', { exact: true })).toHaveCount(0)
  await manager.getByRole('button', { name: '关闭快捷命令管理' }).click()
  await leaveMachine(page)
})

test('独立窗口：登录复用、会话隔离和退出保护', async ({ page }) => {
  const fixture = JSON.parse(
    readFileSync('.test-fixture/connection.json', 'utf8'),
  )
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  const firstOpening = page.waitForEvent('popup')
  await row.getByRole('button', { name: '终端' }).click()
  const firstWindow = await firstOpening
  const machine = firstWindow.getByRole('main', { name: '终端机器页面' })
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  const opening = page.waitForEvent('popup')
  await row.getByRole('button', { name: '终端' }).click()
  const popup = await opening
  await popup.waitForLoadState()
  const detached = popup.getByRole('main', { name: '终端机器页面' })
  await expect(detached).toBeVisible()
  await expect(popup).toHaveTitle(new RegExp(fixture.target.name))
  expect(await popup.evaluate(() => window.opener === null)).toBeTruthy()
  await expect(
    detached.getByRole('button', { name: '关闭', exact: true }),
  ).toHaveCount(0)
  await expect(
    detached.getByRole('button', { name: '返回', exact: true }),
  ).toHaveCount(0)
  await expect(
    detached.getByRole('button', { name: '独立窗口打开', exact: true }),
  ).toHaveCount(0)
  await expect(
    detached.getByRole('button', { name: '切换主题', exact: true }),
  ).toBeVisible()
  await expect(detached.locator('.terminal-tab.active i.online')).toBeVisible()
  await detached
    .locator('.xterm-helper-textarea')
    .pressSequentially('popup-only-session')
  await detached.locator('.xterm-helper-textarea').press('Enter')
  await expect(detached.locator('.xterm-screen')).toContainText(
    'popup-only-session',
  )
  await expect(machine.locator('.xterm-screen')).not.toContainText(
    'popup-only-session',
  )
  const editor = detached.locator('.monaco-editor textarea').first()
  await editor.focus()
  await popup.keyboard.press('Control+End')
  await popup.keyboard.type(' unsaved-popup-note')
  await expect(detached.locator('.editor-status')).toContainText(
    '1 个文件未保存',
  )
  await expect(machine.locator('.view-lines')).not.toContainText(
    'unsaved-popup-note',
  )
  await popup.screenshot({
    path: 'test-results/machine-independent-window.png',
  })
  const confirming = popup.waitForEvent('dialog')
  const reload = popup.reload({ timeout: 5000 }).catch(() => null)
  const confirm = await confirming
  expect(confirm.type()).toBe('beforeunload')
  await confirm.dismiss()
  await reload
  await expect(detached.locator('.view-lines')).toContainText(
    'unsaved-popup-note',
  )
  const closing = popup.waitForEvent('close')
  popup.once('dialog', (dialog) => dialog.accept())
  await popup.close({ runBeforeUnload: true })
  await closing
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await page.evaluate(() => { window.open = () => null })
  await row.getByRole('button', { name: '终端' }).click()
  await expect(page.locator('.notice-error')).toContainText('允许本站弹出窗口')
  await firstWindow.close()
  await expect(page.getByRole('main', { name: '终端机器页面' })).toHaveCount(0)
})

test('快捷命令按机器标签匹配、编辑回显与紧凑表单', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  const sent: string[] = []
  page.on('websocket', socket => {
    if (!socket.url().includes('/terminal')) return
    socket.on('framesent', event => {
      try {
        const message = JSON.parse(String(event.payload))
        if (message.type === 'input') sent.push(message.data)
      } catch {}
    })
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const target = { ...fixture.target, id: 'shortcut-tags', name: '标签快捷测试机器', relay_user: 'shortcut-tags', tags: ['生产环境', '数据库'] }
  const response = await page.request.post('/api/targets', { data: target })
  expect(response.ok(), await response.text()).toBeTruthy()
  const existing = await (await page.request.get('/api/shortcuts?page_size=100')).json()
  for (const item of existing.items) await page.request.delete(`/api/shortcuts/${item.id}`, { data: {} })
  await page.reload()
  await page.getByLabel('搜索连接', { exact: true }).fill(target.name)
  await page.goto('/?window=1&machine=' + encodeURIComponent(target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  const quick = machine.getByRole('region', { name: '终端快捷操作', exact: true })
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await quick.getByRole('button', { name: '管理命令', exact: true }).click()
  const manager = machine.getByRole('dialog', { name: '管理快捷命令' })
  const commandEditor = machine.getByRole('dialog', { name: /^(新增|编辑)快捷命令$/ })
  await manager.getByRole('button', { name: '新增命令' }).click()
  await expect(commandEditor.getByLabel('选择机器', { exact: true })).toHaveValue(target.id)
  await expect(commandEditor).toHaveAccessibleName('新增快捷命令')
  await expect(commandEditor.getByLabel('命令名称', { exact: true })).toBeFocused()
  expect(await commandEditor.evaluate(element => element.matches(':modal'))).toBeTruthy()
  const nameBox = (await commandEditor.getByLabel('命令名称', { exact: true }).boundingBox())!
  const commandBox = (await commandEditor.getByLabel('命令内容', { exact: true }).boundingBox())!
  const scopeBox = (await commandEditor.getByRole('group', { name: '适用范围', exact: true }).boundingBox())!
  expect(nameBox.y).toBeLessThan(commandBox.y)
  expect(commandBox.y).toBeLessThan(scopeBox.y)
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('标签巡检')
  await commandEditor.getByLabel('命令内容', { exact: true }).fill('echo tagged-command')
  await commandEditor.getByRole('radio', { name: '标签', exact: true }).check()
  await commandEditor.getByRole('button', { name: '添加命令', exact: true }).click()
  await expect(commandEditor.getByRole('alert')).toContainText('请至少选择一个标签')
  const options = commandEditor.getByRole('group', { name: '适用机器标签' })
  await commandEditor.getByLabel('搜索可选标签').fill('生产')
  await expect(options.getByRole('button')).toHaveCount(1)
  await options.getByRole('button', { name: '生产环境' }).click()
  await commandEditor.getByLabel('搜索可选标签').fill('')
  await options.getByRole('button', { name: '数据库' }).click()
  await commandEditor.getByRole('button', { name: '添加命令', exact: true }).click()
  const row = manager.getByRole('row').filter({ hasText: '标签巡检' })
  await expect(row).toContainText('标签：数据库、生产环境')
  await manager.getByLabel('筛选适用范围').selectOption('tags')
  await manager.getByLabel('筛选快捷命令标签', { exact: true }).selectOption('数据库')
  await expect(row).toBeVisible()
  await manager.getByLabel('搜索快捷命令').fill('生产环境')
  await expect(row).toBeVisible()
  await manager.getByRole('button', { name: '编辑快捷命令 标签巡检', exact: true }).click()
  await expect(commandEditor).toHaveAccessibleName('编辑快捷命令')
  await commandEditor.getByLabel('命令名称', { exact: true }).fill('不保存的修改')
  await page.keyboard.press('Escape')
  await expect(commandEditor).toHaveCount(0)
  await expect(manager.getByRole('button', { name: '编辑快捷命令 标签巡检', exact: true })).toBeFocused()
  await manager.getByRole('button', { name: '编辑快捷命令 标签巡检', exact: true }).click()
  await expect(commandEditor.getByLabel('命令名称', { exact: true })).toHaveValue('标签巡检')
  await expect(commandEditor.getByRole('radio', { name: '标签', exact: true })).toBeChecked()
  await expect(options.getByRole('button', { name: /生产环境/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(options.getByRole('button', { name: /数据库/ })).toHaveAttribute('aria-pressed', 'true')
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 620, height: 700 }, { width: 620, height: 420 }]) {
    await page.setViewportSize(viewport)
    const save = commandEditor.getByRole('button', { name: '保存命令', exact: true })
    await save.scrollIntoViewIfNeeded()
    await expect(save).toBeInViewport()
    const box = (await commandEditor.boundingBox())!
    expect(box.width).toBeLessThanOrEqual(Math.min(560, viewport.width))
    expect(box.height).toBeLessThanOrEqual(viewport.height - 32)
    if (viewport.width === 1440) expect(box.height).toBeLessThan(600)
    await page.screenshot({ path: `test-results/shortcut-tags-${viewport.width}-${viewport.height}.png` })
  }
  await commandEditor.getByRole('button', { name: '保存命令', exact: true }).click()
  await manager.getByRole('button', { name: '关闭快捷命令管理' }).click()
  await page.setViewportSize({ width: 1440, height: 1000 })
  const button = quick.getByRole('button', { name: '标签巡检', exact: true })
  await expect(button).toHaveCount(1)
  await button.click()
  await expect.poll(() => sent).toContain('echo tagged-command')
  expect(sent.join('')).not.toContain('echo tagged-command\r')
  await page.keyboard.press('Enter')
  await expect.poll(() => sent.join('')).toContain('echo tagged-command\r')
  // 关闭最后一个终端后，命令仍显示但不可发送。
  await machine.getByRole('button', { name: '关闭终端 1', exact: true }).click()
  await expect(button).toBeDisabled()
  await leaveMachine(page)
  const commands = await (await page.request.get('/api/shortcuts?scope=tags')).json()
  for (const item of commands.items) await page.request.delete(`/api/shortcuts/${item.id}`, { data: {} })
  await page.request.delete(`/api/targets/${target.id}`, { data: {} })
})

test('终端快捷命令全部载入、单行横向滚动且无分页', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  const commands = Array.from({ length: 105 }, (_, index) => ({
    id: 'horizontal-' + index, name: '横向命令 ' + (index + 1), command: 'echo ' + index, targetId: '*', tags: [],
  }))
  await page.route('**/api/shortcuts?**', async route => {
    const url = new URL(route.request().url())
    if (!url.searchParams.has('applicable_to')) return route.continue()
    const pageNumber = Number(url.searchParams.get('page') || 1)
    const pageSize = Number(url.searchParams.get('page_size') || 20)
    await route.fulfill({ json: { items: commands.slice((pageNumber - 1) * pageSize, pageNumber * pageSize), total: commands.length, page: pageNumber, page_size: pageSize } })
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const quick = page.getByRole('region', { name: '终端快捷操作', exact: true })
  const strip = quick.getByLabel('快捷命令列表，横向滚动', { exact: true })
  await expect(strip.getByRole('button')).toHaveCount(105)
  await expect(quick.getByRole('navigation')).toHaveCount(0)
  await page.setViewportSize({ width: 620, height: 700 })
  expect(await strip.evaluate(element => element.scrollWidth > element.clientWidth)).toBeTruthy()
  const first = (await strip.getByRole('button').first().boundingBox())!
  const last = (await strip.getByRole('button').last().boundingBox())!
  expect(last.y).toBe(first.y)
  await expect(quick.getByText('点击填入，回车执行', { exact: true })).toHaveCount(0)
  const left = quick.getByText('快捷操作', { exact: true })
  const right = quick.getByRole('button', { name: '管理命令', exact: true })
  const leftBefore = (await left.boundingBox())!
  const rightBefore = (await right.boundingBox())!
  const stripBox = (await strip.boundingBox())!
  expect(stripBox.x).toBeGreaterThanOrEqual(leftBefore.x + leftBefore.width)
  expect(stripBox.x + stripBox.width).toBeLessThan(rightBefore.x)
  expect(stripBox.y).toBeLessThan(leftBefore.y + leftBefore.height)
  expect(stripBox.y + stripBox.height).toBeGreaterThan(leftBefore.y)
  await strip.hover()
  await page.mouse.wheel(0, 200)
  await expect.poll(() => strip.evaluate(element => element.scrollLeft)).toBeGreaterThan(0)
  await strip.getByRole('button').last().scrollIntoViewIfNeeded()
  await expect(strip.getByRole('button', { name: '横向命令 105', exact: true })).toBeInViewport()
  expect(await left.boundingBox()).toEqual(leftBefore)
  expect(await right.boundingBox()).toEqual(rightBefore)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(left).toBeInViewport()
  await expect(right).toBeInViewport()
  await expect(quick.getByRole('button', { name: '中断', exact: true })).toHaveCount(0)
  await expect(quick.getByRole('button', { name: /快捷操作/ })).toHaveCount(0)
  await expect(quick.getByRole('button', { name: '清屏', exact: true })).toBeInViewport()
  await page.screenshot({ path: 'test-results/shortcut-horizontal.png' })
})

test('SSH 连接页管理快捷指令，无需连接终端', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '连接日志' })).toHaveCount(0)
  await expect(page.locator('.sidebar').getByRole('button', { name: '快捷指令' })).toHaveCount(0)
  const actions = page.locator('.page-heading-actions')
  await expect(page.getByRole('button', { name: '全局 IP 白名单', exact: true })).toBeVisible()
  await actions.getByRole('button', { name: '快捷指令', exact: true }).click()
  const manager = page.getByRole('dialog', { name: '管理快捷命令', exact: true })
  await expect(manager).toBeVisible()
  const size = await manager.boundingBox()
  expect(size?.width).toBe(880)
  expect(size?.height).toBe(600)
  await manager.getByRole('button', { name: '新增命令' }).click()
  const editor = page.getByRole('dialog', { name: '新增快捷命令', exact: true })
  await expect(editor.getByRole('radio', { name: '全局', exact: true })).toBeChecked()
  await editor.getByLabel('命令名称').fill('首页快捷测试')
  await editor.getByLabel('命令内容').fill('echo homepage')
  await editor.getByRole('radio', { name: '机器', exact: true }).check()
  await editor.getByLabel('选择机器').selectOption(fixture.target.id)
  await editor.getByRole('button', { name: '添加命令', exact: true }).click()
  await expect(editor).not.toBeVisible()
  await manager.getByLabel('搜索快捷命令').fill('首页快捷测试')
  const row = manager.getByRole('row').filter({ hasText: '首页快捷测试' })
  await expect(row).toContainText(fixture.target.name)
  await row.getByRole('button', { name: '编辑快捷命令 首页快捷测试' }).click()
  const editing = page.getByRole('dialog', { name: '编辑快捷命令', exact: true })
  await expect(editing.getByLabel('命令内容')).toHaveValue('echo homepage')
  await editing.getByRole('radio', { name: '全局', exact: true }).check()
  await editing.getByRole('button', { name: '保存命令', exact: true }).click()
  await expect(row).toContainText('全部机器')
  await row.getByRole('button', { name: '删除快捷命令 首页快捷测试' }).click()
  await expect(row).toHaveCount(0)
  await manager.getByRole('button', { name: '关闭快捷命令管理' }).click()
  await expect(manager).not.toBeVisible()
  await expect(page.getByRole('main', { name: '终端机器页面' })).toHaveCount(0)
})

test('右侧资源显示三个实时值', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const resources = page.getByRole('region', { name: '实时资源使用率' })
  await expect(resources.locator('dd')).toHaveCount(3)
  await expect(resources.locator('dd').nth(0)).toHaveText(/33\.\d%/, { timeout: 15000 })
  await expect(resources.locator('dd').nth(1)).toHaveText('37.5%')
  await expect(resources.locator('dd').nth(2)).toHaveText('42.0%')
  await expect(resources.locator('svg')).toHaveCount(0)
})

test('机器标题显示 IP、多终端会话隔离及关闭即断开', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  const closed: boolean[] = []
  page.on('websocket', socket => {
    if (!socket.url().includes('/terminal')) return
    const index = closed.length
    closed.push(false)
    socket.on('close', () => { closed[index] = true })
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  const bar = machine.locator('.terminal-tabs-bar')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await expect(machine.locator('.machine-address')).toContainText(fixture.target.host)
  await expect(machine.locator('.machine-terminal-toolbar')).toHaveCount(0)
  await expect(machine.getByRole('button', { name: '断开', exact: true })).toHaveCount(0)
  await machine.getByRole('button', { name: '新建终端', exact: true }).click()
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await machine.getByRole('button', { name: '关闭终端 2', exact: true }).click()
  await machine.getByRole('button', { name: '新建终端', exact: true }).click()
  await expect.poll(() => closed.length).toBe(3)
  await expect.poll(() => closed[1]).toBe(true)
  expect(closed[0]).toBe(false)
  await expect(machine.getByRole('tab', { name: '终端 3', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await page.setViewportSize({ width: 620, height: 700 })
  await expect(machine.locator('.machine-address')).toBeInViewport()
  await expect(bar.getByRole('button', { name: '新建终端', exact: true })).toBeInViewport()
  await page.screenshot({ path: 'test-results/terminal-tabs-controls.png' })
  await machine.getByRole('button', { name: '关闭终端 3', exact: true }).click()
  await expect.poll(() => closed[2]).toBe(true)
  expect(closed[0]).toBe(false)
  await expect(machine.getByRole('tab', { name: '终端 1', exact: true })).toHaveAttribute('aria-selected', 'true')
  await machine.getByRole('button', { name: '关闭终端 1', exact: true }).click()
  await expect.poll(() => closed[0]).toBe(true)
  await expect(bar.getByRole('tab')).toHaveCount(0)
  await expect(machine.locator('.machine-terminal')).toHaveCount(0)
})
