import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

for (const lang of ['en', 'zh-CN']) {
 test(`Administrator reveals passwords without changing saved credentials (${lang})`, async ({ page }) => {
  const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
  await page.goto('/?lang=' + lang)
  const edit = page.getByRole('row').filter({ hasText: f.target.name }).getByRole('button', { name: lang === 'en' ? 'Edit' : '编辑', exact: true })
  await edit.click()
  const editor = page.locator('.target-editor[open]')
  const password = editor.locator('.editor-password-row input')
  const show = () => editor.getByRole('button', { name: lang === 'en' ? 'Show password' : '显示密码', exact: true })
  const hide = () => editor.getByRole('button', { name: lang === 'en' ? 'Hide password' : '隐藏密码', exact: true })
  await expect(password).toHaveAttribute('type', 'password')
  await expect(password).toHaveValue('')
  await show().click()
  await expect(password).toHaveAttribute('type', 'text')
  await expect(password).toHaveValue(f.target.target_password)
  await hide().click()
  await expect(password).toHaveAttribute('type', 'password')
  await expect(password).toHaveValue('')
  await password.fill('unsaved-demo-password')
  await show().click()
  await expect(password).toHaveAttribute('type', 'text')
  await expect(password).toHaveValue('unsaved-demo-password')
  await hide().click()
  await expect(password).toHaveValue('unsaved-demo-password')
  await page.keyboard.press('Escape')
  await edit.click()
  await expect(password).toHaveAttribute('type', 'password')
  await expect(password).toHaveValue('')
  await show().click()
  let retainedSavedPassword = false
  await page.route('**/api/targets/' + f.target.id, async route => {
   if (route.request().method() !== 'PUT') return route.continue()
   retainedSavedPassword = route.request().postDataJSON().logins.every((login: { target_password: string }) => login.target_password === '')
   await route.fulfill({ status: 409, json: { error: 'Demo save conflict' } })
  })
  await editor.getByRole('button', { name: lang === 'en' ? 'Save connection' : '保存连接', exact: true }).click()
  await expect.poll(() => retainedSavedPassword).toBe(true)
  await hide().click()
  await page.setViewportSize({ width: 800, height: 600 })
  await expect(show()).toBeInViewport()
  await page.screenshot({ path: `test-results/editor-show-password-${lang}.png` })
 })
}

test('Ordinary users cannot open the editor or retrieve original passwords', async ({ page, browser }) => {
 const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
 const created = await page.request.post('/api/users', { data: { username: 'copy-password-reader', password: 'reader-demo-password', enabled: true, target_ids: [f.target.id], tags: [] } })
 expect(created.ok()).toBe(true)
 const user = await created.json()
 const context = await browser.newContext()
 try {
  const p = await context.newPage()
  expect((await p.request.post('/api/login', { data: { username: 'copy-password-reader', password: 'reader-demo-password' } })).ok()).toBe(true)
  await p.goto('/?lang=en')
  await expect(p.locator('.ssh-connections-table')).toBeVisible()
  await expect(p.locator('.target-editor')).toHaveCount(0)
  await expect(p.getByRole('button', { name: 'Show password', exact: true })).toHaveCount(0)
  expect((await p.request.post(`/api/targets/${f.target.id}/logins/default/credentials`, { data: {} })).status()).toBe(403)
 } finally {
  await context.close()
  expect((await page.request.delete('/api/users/' + user.id, {data:{}})).ok()).toBe(true)
 }
})
