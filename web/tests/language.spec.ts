import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { desktopFixture } from './desktop-fixture'

const fixture = () => JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
async function chooseLanguage(page: Page, value: string) {
  await page.locator('.language-trigger').click()
  await page.locator('.language-menu:popover-open').getByRole('button', {name:value === 'en' ? 'English' : '简体中文',exact:true}).click()
}
async function login(page: Page) {
  await page.goto('/?lang=en')
  await page.getByLabel('Administrator password', { exact: true }).fill(fixture().admin_password)
  await page.getByRole('button', { name: 'Sign in →', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'SSH connections', exact: true })).toBeVisible()
}
async function rightCorner(page: Page) {
  const selector = page.locator('.language-switcher:visible')
  await expect(selector).toHaveCount(1)
  const box = (await selector.boundingBox())!
  const size = page.viewportSize()!
  expect(box.x).toBeGreaterThan(size.width / 2)
  expect(box.x + box.width).toBeLessThanOrEqual(size.width)
  expect(box.y).toBeLessThan(160)
}

test('词典及后端参数保持完整', async () => {
  const zh = JSON.parse(readFileSync('src/locales/zh-CN.json', 'utf8'))
  const en = JSON.parse(readFileSync('src/locales/en.json', 'utf8'))
  expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort())
  for (const key of Object.keys(zh)) {
    expect(en[key], key).toBeTruthy()
    expect([...en[key].matchAll(/\{\d+\}/g)].map(x => x[0]).sort(), key)
      .toEqual([...zh[key].matchAll(/\{\d+\}/g)].map(x => x[0]).sort())
    expect(en[key], key).not.toMatch(/[\u4e00-\u9fff]/)
  }
})

test('首次按系统语言选择，未支持的系统语言回退英文', async ({ browser, baseURL }) => {
  for (const [system, expected] of [['zh-TW', 'zh-CN'], ['en-US', 'en'], ['fr-FR', 'en']]) {
    const context = await browser.newContext({ locale: system })
    const page = await context.newPage()
    await page.goto(baseURL! + '/')
    await expect(page.locator('.language-switcher')).toHaveAttribute('data-locale', expected)
    await expect(page.locator('html')).toHaveAttribute('lang', expected)
    await expect(page).toHaveTitle('SSH Gateway')
    await context.close()
  }
})

test('入口优先级、记忆与同源窗口同步，不重载页面', async ({ page, context }) => {
  await page.goto('/')
  await page.evaluate(() => localStorage.setItem('ssh-gateway:locale', 'zh-CN'))
  await page.goto('/?lang=en&retained=1#section')
  await expect(page.locator('.language-switcher')).toHaveAttribute('data-locale', 'en')
  await page.evaluate(() => { (window as any).languageSentinel = 42 })
  await chooseLanguage(page, 'zh-CN')
  await expect(page).toHaveURL(/lang=zh-CN&retained=1#section/)
  expect(await page.evaluate(() => (window as any).languageSentinel)).toBe(42)
  await page.reload()
  await expect(page.locator('.language-switcher')).toHaveAttribute('data-locale', 'zh-CN')
  const second = await context.newPage()
  await second.goto('/?lang=zh-CN')
  await chooseLanguage(page, 'en')
  await expect(second.locator('.language-switcher')).toHaveAttribute('data-locale', 'en')
  await expect(second).toHaveURL(/lang=en/)
  await second.close()
  await page.goto('/?lang=invalid')
  await expect(page.locator('.language-switcher')).toHaveAttribute('data-locale', 'en')
  await page.locator('.language-trigger').focus()
  await page.keyboard.press('Enter')
  await page.keyboard.press('Tab')
  await expect(page.locator('.language-menu:popover-open').getByRole('button', { name: '简体中文', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.locator('.language-switcher')).toHaveAttribute('data-locale', 'zh-CN')
})

test('存储禁用时仍可切换且保留登录输入及错误', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('blocked', 'SecurityError') } })
  })
  await page.goto('/?lang=en')
  await page.getByLabel('Administrator password', { exact: true }).fill('wrong-password')
  await page.getByRole('button', { name: 'Sign in →' }).click()
  await expect(page.getByRole('alert')).toContainText('Incorrect username or password')
  await chooseLanguage(page, 'zh-CN')
  await expect(page.getByRole('alert')).toContainText('用户名或密码不正确')
  await expect(page.getByLabel('管理员密码', { exact: true })).toHaveValue('wrong-password')
  await chooseLanguage(page, 'en')
  await expect(page.getByRole('alert')).toContainText('Incorrect username or password')
})

test('登录页和主界面右上角入口适配浅深主题与窄屏', async ({ page }) => {
  await page.goto('/?lang=en')
  for (const width of [1440, 800, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await rightCorner(page)
  }
  await page.setViewportSize({ width: 1440, height: 1000 })
  await login(page)
  await expect(page.getByLabel('Active connections', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Expand menu', exact: true }).click()
  await expect(page.getByRole('button', { name: /^Current version / })).toBeVisible()
  for (const theme of ['light', 'dark']) {
    if (theme === 'dark') await page.getByRole('button', { name: 'Switch to dark theme', exact: true }).click()
    for (const width of [1440, 800, 390]) {
      await page.setViewportSize({ width, height: 1000 })
      await rightCorner(page)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    }
    await page.setViewportSize({ width: 1440, height: 1000 })
  }
  await page.screenshot({ path: 'test-results/language-dashboard-en.png', fullPage: true })
  await page.getByRole('button', { name: 'Accounts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Accounts', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Port forwarding', exact: true }).click()
  await expect(page.getByRole('heading', { name: /Port forwarding/ })).toBeVisible()
})

test('独立机器页切换保留终端、笔记草稿与原始输出', async ({ page }) => {
  const data = fixture()
  let connections = 0
  page.on('websocket', socket => { if (socket.url().includes('/terminal')) connections++ })
  await login(page)
  await page.goto('/?window=1&machine=' + encodeURIComponent(data.target.id) + '&lang=en')
  const machine = page.locator('.machine-page')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await rightCorner(page)
  const terminal = machine.locator('.machine-terminal:visible')
  await terminal.locator('.xterm-helper-textarea').pressSequentially('locale-session-alive')
  await terminal.locator('.xterm-helper-textarea').press('Enter')
  await expect.poll(() => terminal.locator('.xterm-screen').innerText()).toContain('locale-session-alive')
  await page.evaluate(() => { (window as any).languageSentinel = 42 })
  const editor = machine.locator('.monaco-editor textarea').first()
  await expect(editor).toBeVisible()
  await editor.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type('\ni18n unsaved draft {0}')
  await expect(machine.locator('.editor-status')).toContainText('unsaved')
  const count = connections
  await chooseLanguage(page, 'zh-CN')
  await expect(machine).toHaveAttribute('aria-label', '终端机器页面')
  await expect(machine.locator('.terminal-tab.active')).toContainText('终端 1')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await expect(machine.locator('.view-lines')).toContainText('i18n unsaved draft {0}')
  await expect(machine.locator('.editor-status')).toContainText('未保存')
  await expect.poll(() => terminal.locator('.xterm-screen').innerText()).toContain('locale-session-alive')
  expect(await page.evaluate(() => (window as any).languageSentinel)).toBe(42)
  expect(connections).toBe(count)
  await chooseLanguage(page, 'en')
  await expect(machine.locator('.terminal-tab.active')).toContainText('Terminal 1')
  await expect(page).toHaveTitle(data.target.name + ' · Terminal')
  for (const width of [800, 390]) {
    await page.setViewportSize({ width, height: 1000 })
    await rightCorner(page)
  }
  await page.screenshot({ path: 'test-results/language-machine-en.png', fullPage: true })
})

test('复制说明本地化且用户内容及密码原样保留', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (text: string) => { (window as any).copied = text } } })
  })
  await login(page)
  const row = page.getByRole('row').filter({ hasText: fixture().target.name })
  await row.getByRole('button', { name: 'Copy connection info', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).copied || '')).toContain('Password:')
  const copied = await page.evaluate(() => (window as any).copied)
  expect(copied).toContain(fixture().target.name)
  expect(copied).not.toContain('ssh-gateway:')
  const password = copied.split('Password: ')[1].split('\n')[0]
  expect(copied).toContain('Credential validity: No expiration')
  await chooseLanguage(page, 'zh-CN')
  await row.getByRole('button', { name: '复制连接信息', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).copied || '')).toContain('密码：' + password)
})

test('桌面桥接同步语言', async ({ page }) => {
  const languages: string[] = []
  await desktopFixture(page, {
    SetLocale: (value: string) => { languages.push(value) },
    Info: () => ({ ready: true, needs_setup: true, settings: {}, version: '1.0.0' }),
    Call: (_method: string, path: string) => path === '/security'
      ? { status: 200, data: { enabled: false, locked: false } }
      : { status: 401, data: { error: '请先登录', message_key: 'backend.48144f96c150' } },
  })
  await page.goto('http://wails.localhost/?lang=en')
  await expect(page.locator('.language-switcher')).toHaveAttribute('data-locale', 'en')
  await expect.poll(() => languages).toEqual(['en'])
  await chooseLanguage(page, 'zh-CN')
  await expect.poll(() => languages).toEqual(['en', 'zh-CN'])
})

test('桌面初始化失败仍显示可切换的右上角入口', async ({ page }) => {
  await desktopFixture(page, { SetLocale: () => { throw new Error('init failure') } })
  await page.route('**/wails/runtime', route => route.fulfill({ status: 500, json: { message: 'init failure' } }))
  await page.goto('http://wails.localhost/?lang=en')
  await expect(page.getByRole('alert')).toHaveText('Failed to start. Please quit the app and try again.')
  await rightCorner(page)
  await chooseLanguage(page, 'zh-CN')
  await expect(page.getByRole('alert')).toHaveText('启动失败，请退出应用后重试。')
})


test('新终端窗口继承当前语言', async ({ page }) => {
  await login(page)
  const opened = page.waitForEvent('popup')
  await page.getByRole('row').filter({ hasText: fixture().target.name }).getByRole('button', { name: 'Open terminal', exact: true }).click()
  const popup = await opened
  try {
    await expect(popup.locator('.language-switcher')).toHaveAttribute('data-locale', 'en')
    await expect(popup.locator('.terminal-tab.active i.online')).toBeVisible()
    await expect(popup).toHaveURL(/lang=en/)
  } finally { await popup.close() }
})

test('主密码解锁页可切换且保留输入', async ({ page }) => {
  await page.route('**/api/security', route => route.fulfill({ json: { enabled: true, locked: true } }))
  await page.goto('/?lang=en')
  await expect(page.getByRole('heading', { name: 'Unlock server credentials' })).toBeVisible()
  await page.getByLabel('Administrator password', { exact: true }).fill('draft-master-password')
  await chooseLanguage(page, 'zh-CN')
  await expect(page.getByRole('heading', { name: '解锁服务器凭证' })).toBeVisible()
  await expect(page.getByLabel('管理员密码', { exact: true })).toHaveValue('draft-master-password')
  await rightCorner(page)
})

test('切换语言保留正在上传的请求', async ({ page }) => {
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  let uploads = 0
  await page.route(/\/api\/targets\/[^/]+\/transfer\?/, async route => {
    uploads++
    await gate
    await route.fulfill({ json: {} })
  })
  await login(page)
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture().target.id) + '&lang=en')
  const machine = page.locator('.machine-page')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  const tree = machine.getByRole('tree', { name: 'Remote file tree' })
  await expect(tree).toBeVisible()
  await tree.locator('.tree-row').first().click({ button: 'right' })
  const chooser = page.waitForEvent('filechooser')
  await machine.getByRole('menuitem', { name: 'Upload file to this directory' }).click()
  await (await chooser).setFiles({ name: 'language-transfer.txt', mimeType: 'text/plain', buffer: Buffer.from('原始内容 {0}') })
  try {
    await expect.poll(() => uploads).toBe(1)
    await expect(machine.getByText('Uploading language-transfer.txt', { exact: false })).toBeVisible()
    await chooseLanguage(page, 'zh-CN')
    await expect(machine.getByText('上传 language-transfer.txt', { exact: false })).toBeVisible()
    expect(uploads).toBe(1)
  } finally { release() }
  await expect(machine.locator('.file-message[role=status]')).toContainText('上传完成')
})
