import { test, expect, type Locator } from '@playwright/test'
import { readFileSync } from 'node:fs'

async function expectOpaque(locator: Locator) {
  await expect(locator).toBeVisible()
  const style = await locator.evaluate(node => {
    const css = getComputedStyle(node)
    return { background: css.backgroundColor, opacity: css.opacity }
  })
  expect(style.background).toMatch(/^rgb\(/)
  expect(style.opacity).toBe('1')
}

for (const theme of ['light', 'dark']) {
  test(`Shortcut dialogs have opaque surfaces in the ${theme} theme`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('ssh-gateway:theme', value), theme)
    const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
    await page.goto('/?lang=zh-CN')
    await page.getByLabel('管理员密码', { exact: true }).fill(fixture.admin_password)
    await page.getByRole('button', { name: '进入中转台 →', exact: true }).click()
    await page.getByRole('button', { name: '快捷指令', exact: true }).click()
    const manager = page.getByRole('dialog', { name: '管理快捷命令', exact: true })
    await expectOpaque(manager)
    await expectOpaque(manager.getByLabel('搜索快捷命令'))
    await expectOpaque(manager.locator('th').first())
    await page.screenshot({ path: `test-results/shortcut-manager-${theme}.png` })
    await manager.getByRole('button', { name: '＋ 新增命令', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '新增快捷命令', exact: true })
    await expectOpaque(editor)
    await expectOpaque(editor.getByLabel('命令名称', { exact: true }))
    await page.screenshot({ path: `test-results/shortcut-editor-${theme}.png` })
    await editor.getByRole('button', { name: '取消', exact: true }).click()
    await expect(manager).toBeVisible()
    await manager.getByRole('button', { name: '关闭快捷命令管理', exact: true }).click()
    await expect(manager).not.toBeVisible()
  })
}
