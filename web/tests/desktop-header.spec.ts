import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { desktopFixture } from './desktop-fixture'

for (const language of ['en', 'zh-CN']) {
 test(`Desktop header groups settings and exit actions (${language})`, async ({ page }) => {
  const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
  let quits = 0
  await desktopFixture(page, {
   Info: () => ({ ready: true, needs_setup: false, active: 0, settings: {}, version: 'test' }),
   CheckUpdate: () => ({ configured: false, available: false }),
   Quit: () => { quits++ },
   Call: async (method: string, path: string, body: string) => {
    const response = await page.request.fetch('/api' + path, { method, ...(method === 'GET' ? {} : { data: JSON.parse(body || '{}') }) })
    return { status: response.status(), data: await response.json() }
   },
  })
  await page.goto('/?lang=' + language)
  const trigger = page.locator('[popovertarget="system-settings-menu"]')
  await expect(trigger).toBeVisible()
  const menu = page.locator('#system-settings-menu')
  for (const width of [1440, 800]) {
   await page.setViewportSize({ width, height: 800 })
   await expect(page.locator('.admin-menu button:visible')).toHaveCount(3)
   const header = (await page.locator('.topbar').boundingBox())!
   const brand = (await page.locator('.sidebar .brand').boundingBox())!
   const logo = (await page.locator('.sidebar .brand-symbol').boundingBox())!
   expect(header.height).toBeLessThanOrEqual(60)
   expect(Math.abs(brand.y + brand.height - header.y - header.height)).toBeLessThanOrEqual(1)
   expect(Math.abs(logo.y + logo.height / 2 - header.y - header.height / 2)).toBeLessThanOrEqual(1)
   await trigger.click()
   await expect(menu.getByRole('button')).toHaveCount(5)
   const box = (await menu.boundingBox())!
   expect(box.x).toBeGreaterThanOrEqual(0)
   expect(box.x + box.width).toBeLessThanOrEqual(width)
   await page.screenshot({ path: `test-results/desktop-header-${language}-${width}.png` })
   await page.keyboard.press('Escape')
   await expect(menu).not.toBeVisible()
  }
  await trigger.click()
  await menu.getByRole('button', { name: language === 'en' ? 'Desktop settings' : '桌面设置', exact: true }).click()
  await expect(page.locator('dialog[open]')).toBeVisible()
  await expect(menu).not.toBeVisible()
  await page.keyboard.press('Escape')
  await trigger.click()
  await menu.getByRole('button', { name: language === 'en' ? 'Quit app' : '退出应用', exact: true }).click()
  await expect.poll(() => quits).toBe(1)
  await trigger.click()
  await menu.getByRole('button', { name: language === 'en' ? 'Sign out' : '退出登录', exact: true }).click()
  await expect(page.locator('.topbar')).not.toBeVisible()
 })
}
