import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('连接列表精简操作，编辑详情内确认删除，取消保留编辑', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  const id = 'compact-connection-actions'
  expect((await page.request.post('/api/targets', { data: { ...fixture.multi_target, id, name: '精简连接验收', tags: ['界面验收'] } })).ok()).toBe(true)
  try {
    await page.reload()
    await page.getByLabel('筛选机器标签').selectOption('界面验收')
    const row = page.getByRole('row').filter({ hasText: '精简连接验收' })
    await expect(row).toBeVisible()
    for (const name of ['连接参数', '禁用', '测试', '删除']) {
      await expect(row.getByRole('button', { name, exact: true })).toHaveCount(0)
    }
    await expect(row.getByRole('combobox')).toBeVisible()
    await page.screenshot({ path: 'test-results/compact-list-light.png' })
    await page.getByRole('button', { name: /切换黑色主题/ }).click()
    await page.screenshot({ path: 'test-results/compact-list-dark.png' })
    await row.getByRole('button', { name: '编辑', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '编辑 SSH 连接' })
    await editor.getByLabel('名称', { exact: true }).fill('未保存草稿')
    const remove = editor.locator('.target-editor-header').getByRole('button', { name: '删除', exact: true })
    await remove.click()
    const confirm = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '删除连接', exact: true }) })
    await confirm.getByRole('button', { name: '取消', exact: true }).click()
    await expect(editor.getByLabel('名称', { exact: true })).toHaveValue('未保存草稿')
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(remove).toBeInViewport()
    await page.screenshot({ path: 'test-results/compact-editor-mobile.png' })
    await remove.click()
    await confirm.getByRole('button', { name: '确认删除', exact: true }).click()
    await expect(editor).not.toBeVisible()
    await expect(row).toHaveCount(0)
  } finally { await page.request.delete(`/api/targets/${id}`, { data: {} }) }
})
