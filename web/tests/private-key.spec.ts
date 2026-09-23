import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

for (const encrypted of [false, true]) {
  test(`私钥认证：${encrypted ? '加密私钥文件导入' : '无密码私钥粘贴'}、连接与编辑保留`, async ({ page }) => {
    const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
    const target = fixture.key_targets[encrypted ? 1 : 0]
    const id = encrypted ? 'encrypted-key' : 'plain-key'
    await page.goto('/')
    await page.getByLabel('管理员密码').fill(fixture.admin_password)
    await page.getByRole('button', { name: '进入中转台' }).click()
    await expect(page.getByRole('row').filter({ hasText: fixture.target.name })).toBeVisible()
    await page.getByRole('button', { name: '＋ 添加 SSH', exact: true }).click()
    const editor = page.locator('.target-editor')
    await editor.getByLabel('名称', { exact: true }).fill(id)
    await editor.getByLabel('主机 IP / 域名').fill(target.host)
    await editor.getByLabel('SSH 端口').fill(String(target.port))
    await editor.getByLabel('目标用户名', { exact: true }).fill(target.user)
    await editor.getByRole('combobox', { name: '认证方式', exact: true }).selectOption('private_key')
    await expect(editor.getByLabel('目标密码', { exact: true })).toHaveCount(0)
    if (encrypted) {
      await editor.getByLabel('导入私钥文件').setInputFiles({ name: 'id_ed25519', mimeType: 'text/plain', buffer: Buffer.from(target.target_private_key) })
    } else {
      await editor.getByLabel('目标私钥', { exact: true }).fill(target.target_private_key)
    }
    await expect(editor.getByLabel('目标私钥', { exact: true })).toHaveValue(target.target_private_key)
    await editor.getByRole('button', { name: /^主机指纹/ }).click()
    await editor.getByLabel('目标主机指纹').fill(target.host_fingerprint)
    await editor.getByRole('button', { name: /^中转凭证/ }).click()
    await editor.getByLabel('自动创建中转账号和密码').uncheck()
    await editor.getByLabel('中转用户名').fill(id)
    await editor.getByLabel('中转密码', { exact: true }).fill('browser-relay-password')
    if (await editor.getByRole('button', { name: /^访问范围/ }).getAttribute('aria-expanded') === 'false') await editor.getByRole('button', { name: /^访问范围/ }).click()
    await editor.getByLabel('允许的来源 IP / 网段').fill('127.0.0.1')
    if (encrypted) {
      await editor.getByLabel('私钥密码', { exact: true }).fill('wrong-passphrase')
      await editor.getByRole('button', { name: '保存连接' }).click()
      await expect(editor.getByRole('alert')).toContainText('无法解密私钥')
      await editor.getByLabel('私钥密码', { exact: true }).fill(target.target_key_passphrase)
    }
    await editor.getByRole('button', { name: '保存连接' }).click()
    const connection = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '中转连接参数' }) })
    await expect(connection).toBeVisible()
    await connection.getByRole('button', { name: '关闭', exact: true }).click()
    const row = page.getByRole('row').filter({ hasText: id })
    await row.getByRole('button', { name: '编辑', exact: true }).click()
    await expect(editor.getByRole('combobox', { name: '认证方式', exact: true })).toHaveValue('private_key')
    await expect(editor.getByLabel('目标私钥', { exact: true })).toHaveValue('')
    await expect(editor.getByLabel('私钥密码', { exact: true })).toHaveValue('')
    await editor.getByRole('button', { name: '保存连接' }).click()
    await expect(editor).not.toBeVisible()
    const opening = page.waitForEvent('popup')
    await row.getByRole('button', { name: '终端' }).click()
    const terminalPage = await opening
    const machine = terminalPage.getByRole('main', { name: '终端机器页面' })
    await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
    await machine.getByRole('button', { name: '查看 SSH 连接信息' }).click()
    const info = machine.getByRole('dialog', { name: 'SSH 连接信息' })
    await expect(info).toContainText('私钥认证')
    await expect(info).not.toContainText(target.target_private_key)
    const response = await page.request.get('/api/targets')
    const text = await response.text()
    expect(text).not.toContain('target_private_key')
    expect(text).not.toContain('target_key_passphrase')
    await info.getByRole('button', { name: '关闭连接信息' }).click()
    await terminalPage.close()
    await row.getByRole('button', { name: '编辑', exact: true }).click()
    await editor.locator('.target-editor-header').getByRole('button', { name: '删除', exact: true }).click()
    await page.getByRole('button', { name: '确认删除' }).click()
    await expect(row).toHaveCount(0)
  })
}
