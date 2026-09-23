import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { resolve, extname } from 'node:path'
import { desktopFixture } from './desktop-fixture'

const fixture = () => JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))

for (const desktop of [false, true]) {
  test(`${desktop ? '桌面桥接' : '网页'}免费版本超过十台仍可添加机器且没有收费入口`, async ({ page }) => {
    const f = fixture()
    const prefix = desktop ? 'free-desktop' : 'free-web'
    const ids = Array.from({ length: 11 }, (_, i) => `${prefix}-${i}`)
    const requests: string[] = []
    page.on('request', request => {
      if (/\/api\/license|lemonsqueezy|gumroad/.test(request.url())) requests.push(request.url())
    })
    expect((await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })).ok()).toBeTruthy()
    if (desktop) {
      await desktopFixture(page, {
        Info: () => ({ ready: true, needs_setup: false, active: 0, settings: { port: 2222, external: false, connect_host: '127.0.0.1' }, data_dir: '/测试数据', error: '', version: '0.2.0' }),
        CheckUpdate: () => ({ configured: false, current_version: '0.2.0', available: false }),
        Call: async (method, path, body) => {
          const response = await page.request.fetch('/api' + path, { method, ...(method === 'GET' ? {} : { data: JSON.parse(body || '{}') }) })
          return { status: response.status(), data: await response.json() }
        },
      })
    }
    try {
      for (const id of ids.slice(0, 10)) {
        const response = await page.request.post('/api/targets', { data: { ...f.target, id, name: id, relay_user: id } })
        expect(response.ok(), await response.text()).toBeTruthy()
      }
      await page.goto('/?lang=zh-CN')
      await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: '版本与授权', exact: true })).toHaveCount(0)
      await page.getByRole('button', { name: '＋ 添加 SSH', exact: true }).click()
      const editor = page.locator('.target-editor')
      await expect(editor).toBeVisible()
      await editor.getByLabel('名称', { exact: true }).fill(ids[10]!)
      await editor.getByLabel('主机 IP / 域名').fill(f.target.host)
      await editor.getByLabel('SSH 端口').fill(String(f.target.port))
      await editor.getByLabel('目标用户名', { exact: true }).fill(f.target.user)
      await editor.getByLabel('目标密码', { exact: true }).fill(f.target.target_password)
      await editor.getByRole('button', { name: '读取指纹' }).click()
      await expect(editor.getByLabel('目标主机指纹')).toHaveValue(f.target.host_fingerprint)
      await editor.getByRole('button', { name: '保存连接' }).click()
      await expect(editor).not.toBeVisible()
      const targets = await (await page.request.get('/api/targets?page_size=100')).json()
      const added = targets.items.find((target: { name: string }) => target.name === ids[10])
      expect(added).toBeTruthy()
      ids.push(added.id)
      expect(targets.total).toBeGreaterThan(10)
      expect(requests).toEqual([])
    } finally {
      for (const id of ids) await page.request.delete(`/api/targets/${id}`, { data: {} })
      await page.unrouteAll({ behavior: 'wait' })
    }
  })
}

test('官网中英文页面没有定价和购买入口', async ({ page }) => {
  await page.route('https://site.test/**', route => {
    const name = new URL(route.request().url()).pathname.slice(1) || 'index.html'
    const types: Record<string, string> = { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }
    const body = name === 'config.js' ? 'window.SSH_GATEWAY_REPOSITORY = "";' : readFileSync(resolve('../site', name))
    return route.fulfill({ body, contentType: types[extname(name)] || 'application/octet-stream' })
  })
  await page.goto('https://site.test/?lang=zh')
  for (const language of ['zh', 'en']) {
    await page.locator(`[data-language="${language}"]`).click()
    await expect(page.locator('h1')).toBeVisible()
    await expect(page.locator('#pricing, #purchase-link')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText(/购买专业版|Buy Pro|\$29|\$49/)
  }
})
