import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { desktopFixture } from './desktop-fixture'
const fixture = () => JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
const firstPassword = 'browser-first-master-password'
const nextPassword = 'browser-next-master-password'

test('Encryption settings are no longer exposed',async({page})=>{
 await page.request.post('/api/login',{data:{username:'ssh-admin',password:fixture().admin_password}})
 await page.goto('/')
 await page.getByRole('button',{name:'系统设置'}).click()
 await expect(page.getByRole('button',{name:'安全密码加密',exact:true})).toHaveCount(0)
 expect((await page.request.post('/api/security/master-password',{data:{action:'disable'}})).status()).toBe(404)
})

test('重启锁定界面、错误主密码和解锁后登录', async ({ page }) => {
  // 重启与加密由 Go 集成用例验证；这里模拟锁定状态，覆盖前端流程。
  let locked = true
  await page.route('**/api/security', route => route.fulfill({ json: { enabled: true, locked } }))
  await page.route('**/api/unlock', async route => {
    if (route.request().postDataJSON().password !== firstPassword) return route.fulfill({ status: 401, json: { error: '管理员密码不正确或密钥文件已损坏' } })
    locked = false
    await route.fulfill({ json: { enabled: true, locked: false } })
  })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '解锁服务器凭证' })).toBeVisible()
  await page.getByLabel('管理员密码', { exact: true }).fill('wrong-master-password')
  await page.getByRole('button', { name: '解锁凭证' }).click()
  await expect(page.getByRole('alert')).toContainText('管理员密码不正确')
  await expect(page.getByLabel('管理员密码', { exact: true })).toHaveValue('')
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  await page.screenshot({ path: 'test-results/主密码-解锁页.png', fullPage: true })
  await page.getByLabel('管理员密码', { exact: true }).fill(firstPassword)
  await page.getByRole('button', { name: '解锁凭证' }).click()
  await expect(page.getByRole('heading', { name: '登录中转台' })).toBeVisible()
  await page.getByLabel('管理员密码').fill(fixture().admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
})

test('首次桌面设置只需一个管理员密码，无加密开关', async ({ page, baseURL }) => {
  let needsSetup = true, protectedCredentials = false, cookie = ''
  await desktopFixture(page, {
    Info: () => ({ ready: true, needs_setup: needsSetup, data_dir: '/测试数据', listen: '', error: '', active: 0, settings: { external: false, port: 2222, connect_host: '127.0.0.1' }, version: '测试' }),
    Call: async (method: string, path: string, body: string) => {
      if (path === '/security') return { status: 200, data: { enabled: protectedCredentials, locked: false } }
      if (path === '/desktop/setup') {
        protectedCredentials = true
        expect(JSON.parse(body).encryption).toBeUndefined()
        expect(JSON.parse(body).password).toBe(fixture().admin_password)
        expect(JSON.parse(body).master_password).toBeUndefined()
        needsSetup = false
        return { status: 200, data: { ok: true } }
      }
      const response = await fetch(`${baseURL}/api${path}`, { method, headers: { 'Content-Type': 'application/json', ...(cookie ? { Cookie: cookie } : {}) }, body: method === 'GET' ? undefined : body })
      cookie = response.headers.get('set-cookie')?.split(';')[0] || cookie
      return { status: response.status, data: await response.json() }
    },
  })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '设置管理员密码', exact: true })).toBeVisible()
  await expect(page.getByLabel('设置主密码', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('开启安全加密（可选）')).toHaveCount(0)
  await page.getByLabel('设置管理员密码（至少 12 位）').fill(fixture().admin_password)
  await expect(page.locator('input[type=password]')).toHaveCount(1)
  await page.getByLabel('我已妥善保存管理员密码',{exact:true}).check()
  await page.getByRole('button', { name: '设置密码并进入' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  expect(protectedCredentials).toBe(true)
})

test('远程 HTTP 解锁页在发送前阻止提交主密码', async ({ page, baseURL }) => {
  let submitted = 0
  await page.route('http://gateway.invalid/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/security') return route.fulfill({ json: { enabled: true, locked: true } })
    if (route.request().method() === 'POST') { submitted++; return route.fulfill({ json: {} }) }
    const response = await page.request.get(`${baseURL}${url.pathname}${url.search}`)
    await route.fulfill({ response })
  })
  await page.goto('http://gateway.invalid/')
  await expect(page.getByRole('heading', { name: '解锁服务器凭证' })).toBeVisible()
  await expect(page.getByRole('button', { name: '解锁凭证' })).toBeDisabled()
  await expect(page.getByRole('alert')).toContainText('HTTPS')
  await page.getByLabel('管理员密码', { exact: true }).fill(firstPassword)
  await page.locator('form').evaluate((form: HTMLFormElement) => form.requestSubmit())
  await expect(page.getByRole('alert')).toContainText('HTTPS')
  expect(submitted).toBe(0)
})
