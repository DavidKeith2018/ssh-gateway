import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('在线连接卡片随终端连接和断开自动更新', async ({page}) => {
 const fixture=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
 await page.goto('/')
 await page.getByLabel('管理员密码').fill(fixture.admin_password)
 await page.getByRole('button',{name:'进入中转台'}).click()
 const value=page.locator('[aria-label="当前在线连接"] strong')
 await expect(value).toHaveText('0个 SSH 会话')
 const opened=page.waitForEvent('popup')
 await page.getByRole('row').filter({hasText:fixture.target.name}).getByRole('button',{name:'连接终端',exact:true}).click()
 const popup=await opened
 try {
  await expect(popup.locator('.terminal-tab.active i.online')).toBeVisible()
  await page.bringToFront()
  await expect(value).toHaveText('1个 SSH 会话',{timeout:10000})
 } finally {await popup.close()}
 await page.bringToFront()
 await expect(value).toHaveText('0个 SSH 会话',{timeout:10000})
})
