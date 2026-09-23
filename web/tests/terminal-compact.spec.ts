import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('Terminal output and default commands stay visible after resizing', async ({ page }) => {
 const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
 // Other command-management tests may have removed the initial commands.
 const existing = await (await page.request.get('/api/shortcuts?page_size=100')).json()
 for (const command of ['pwd', 'ls -lah', 'df -h', 'free -h', 'uptime']) {
  if (!existing.items.some((item: { command: string; targetId: string }) => item.command === command && item.targetId === '*')) {
   const saved = await page.request.post('/api/shortcuts', { data: { name: command, command, targetId: '*' } })
   expect(saved.ok()).toBe(true)
  }
 }
 await page.goto('/?lang=en&window=1&machine=' + f.target.id)
 await expect(page.locator('.terminal-tab.active i.online')).toBeVisible()
 await expect(page).toHaveTitle(f.target.name + ' — WebSSH')
 const commands = page.locator('.terminal-quick-list')
 for (const command of ['pwd', 'ls -lah', 'df -h', 'free -h', 'uptime']) {
  await expect(commands.getByRole('button', { name: command, exact: true })).toBeVisible()
 }
 const input = page.locator('.machine-terminal:visible .xterm-helper-textarea')
 await input.focus()
 await page.keyboard.type('seq 1 100')
 await page.keyboard.press('Enter')
 for (const [width, height] of [[1280,900],[1000,600],[800,500],[1000,400],[1440,1000]]) {
  await page.setViewportSize({ width, height })
  await expect.poll(async () => {
   const terminal = (await page.locator('.machine-terminal:visible .xterm-screen').boundingBox())!
   const bar = (await page.locator('.terminal-quick-actions').boundingBox())!
   return terminal.y + terminal.height <= bar.y - 5 && bar.y + bar.height <= height
  }).toBe(true)
  await expect(commands.getByRole('button', { name: 'pwd', exact: true })).toBeInViewport()
  await page.screenshot({ path: `test-results/terminal-compact-${width}-${height}.png` })
 }
 await page.locator('.machine-divider').focus()
 for (let i = 0; i < 6; i++) await page.keyboard.press('ArrowDown')
 await page.setViewportSize({ width: 1000, height: 400 })
 await expect.poll(async () => {
  const terminal = (await page.locator('.machine-terminal:visible .xterm-screen').boundingBox())!
  const bar = (await page.locator('.terminal-quick-actions').boundingBox())!
  return terminal.y + terminal.height <= bar.y - 5 && bar.y + bar.height <= 400
 }).toBe(true)
 for (const theme of ['light', 'dark']) {
  await page.evaluate(value => window.dispatchEvent(new StorageEvent('storage', {key:'ssh-gateway:theme',newValue:value})), theme)
  const expected = theme === 'light' ? 'rgb(248, 250, 252)' : 'rgb(13, 13, 13)'
  await expect(page.locator('.machine-terminal:visible')).toHaveCSS('background-color', expected)
  await expect(page.locator('.machine-terminal:visible .xterm-viewport')).toHaveCSS('background-color', expected)
 }
 await commands.getByRole('button', { name: 'pwd', exact: true }).click()
 await expect(input).toBeFocused()
})
