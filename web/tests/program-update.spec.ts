import { test, expect } from '@playwright/test'
import { desktopFixture } from './desktop-fixture'
import { readFileSync } from 'node:fs'
const base = {
  Info: () => ({ ready: true, needs_setup: false, active: 0, settings: {}, data_dir: '/测试数据', version: '0.2.0', error: '' }),
  Call: (_method: string, path: string) => path === '/security' ? { status: 200, data: { enabled: false, locked: false } } : { status: 401, data: { error: '请先登录' } },
}
const release = { configured: true, current_version: '0.2.0', version: '0.3.0', available: true, url: 'https://github.com/owner/repo/releases/tag/v0.3.0', notes: '<script>危险内容</script>\n修复连接问题' }

test('桌面常驻显示版本，只提示新版本并由用户打开发布页', async ({ page }) => {
  const opened: string[] = []
  let checks = 0
  await desktopFixture(page, {
    ...base,
    CheckUpdate: () => { checks++; return release },
    OpenExternalURL: (url: string) => { opened.push(url) },
  })
  await page.goto('/')
  await expect(page.getByRole('button', { name: '当前版本 0.2.0', exact: true })).toBeInViewport()
  await expect(page.getByRole('button', { name: '发现新版本 0.3.0' })).toBeInViewport()
  expect(checks).toBe(1); expect(opened).toEqual([])
  await page.getByRole('button', { name: '发现新版本 0.3.0' }).click()
  const dialog = page.locator('dialog[open]')
  await expect(dialog).toContainText('当前版本 0.2.0')
  await expect(dialog).toContainText('<script>危险内容</script>')
  await expect(dialog.locator('script')).toHaveCount(0)
  await expect(dialog).toContainText('手动升级')
  await expect(dialog.getByRole('button', { name: /下载更新|打开安装包/ })).toHaveCount(0)
  await dialog.getByRole('link', { name: /查看 GitHub 发布页/ }).click()
  await expect.poll(() => opened).toEqual([release.url])
  await dialog.getByRole('button', { name: '稍后处理' }).click()
  await expect(dialog).toHaveCount(0)
  await page.unrouteAll({ behavior: 'wait' })
})

test('未配置发布仓库时仍显示版本且支持手动检查', async ({ page }) => {
  await desktopFixture(page, { ...base, CheckUpdate: () => ({ configured: false, current_version: '0.2.0', available: false }) })
  await page.goto('/')
  await expect(page.getByRole('button', { name: '当前版本 0.2.0', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /发现新版本/ })).toHaveCount(0)
  await page.getByRole('button', { name: '当前版本 0.2.0', exact: true }).click()
  await expect(page.locator('dialog[open]').getByRole('status')).toContainText('暂未配置更新来源')
  await page.unrouteAll({ behavior: 'wait' })
})

test('浏览器页面显示服务端版本，登录后提示新版本和手动升级链接', async ({ page }) => {
  await page.route('**/api/version', route => route.fulfill({ json: { version: '0.2.0' } }))
  await page.route('**/api/update', route => route.fulfill({ json: release }))
  await page.goto('/')
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await expect(page.getByRole('button', { name: '当前版本 0.2.0', exact: true })).toBeInViewport()
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await page.getByRole('button', { name: '展开菜单', exact: true }).click()
  await expect(page.getByRole('button', { name: '当前版本 0.2.0', exact: true })).toBeInViewport()
  await page.getByRole('button', { name: '发现新版本 0.3.0' }).click()
  const link = page.locator('dialog[open]').getByRole('link', { name: /查看 GitHub 发布页/ })
  await expect(link).toHaveAttribute('href', release.url)
  await expect(link).toHaveAttribute('target', '_blank')
})

test('Long preview version stays within the expanded sidebar', async ({ page }) => {
  const version = '0.1.1-preview.20260923.14'
  await page.route('**/api/version', route => route.fulfill({ json: { version } }))
  await page.route('**/api/update', route => route.fulfill({ json: { configured: false, current_version: version, available: false } }))
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.request.post('/api/login', { data: { username: 'ssh-admin', password: fixture.admin_password } })
  for (const language of ['zh-CN', 'en']) {
    await page.goto('/?lang=' + language)
    await expect(page.locator('#workspace-sidebar')).toBeVisible()
    const expand = page.getByRole('button', { name: language === 'en' ? 'Expand menu' : '展开菜单', exact: true })
    if (await expand.isVisible()) await expand.click()
    for (const width of [1440, 800]) {
      await page.setViewportSize({ width, height: 800 })
      const label = page.locator('.sidebar-version .version-label')
      const number = page.locator('.sidebar-version .version-number')
      await expect(number).toHaveAttribute('title', 'v' + version)
      const labelBox = (await label.boundingBox())!
      const numberBox = (await number.boundingBox())!
      const sidebar = (await page.locator('#workspace-sidebar').boundingBox())!
      expect(labelBox.height).toBeLessThan(25)
      expect(numberBox.y).toBeGreaterThanOrEqual(labelBox.y + labelBox.height)
      expect(numberBox.x + numberBox.width).toBeLessThanOrEqual(sidebar.x + sidebar.width)
      await page.screenshot({ path: `test-results/sidebar-version-${language}-${width}.png` })
    }
  }
})
