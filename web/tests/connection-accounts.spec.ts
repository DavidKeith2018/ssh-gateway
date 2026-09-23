import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('列表选择账号后直接复制，来源位于账号上方，右侧笔记保存', async ({ page, context }) => {
 const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await context.grantPermissions(['clipboard-read', 'clipboard-write'])
 await page.goto('/')
 await page.getByLabel('管理员密码').fill(fixture.admin_password)
 await page.getByRole('button', { name: '进入中转台' }).click()
 const row = page.getByRole('row').filter({ hasText: fixture.target.name })
 const choose = row.getByRole('combobox')
 await expect(choose.locator('optgroup')).toHaveCount(0)
 await expect(row.locator('.target-address')).not.toContainText('@')
 const source = (await row.locator('.source-preview').boundingBox())!
 const account = (await choose.boundingBox())!
 expect(source.y + source.height).toBeLessThanOrEqual(account.y)
 for (const [selection, label, password, other] of [
   ['server:default', '复制服务器凭证', fixture.target.target_password, fixture.target.relay_password],
   ['default', '复制连接信息', fixture.target.relay_password, fixture.target.target_password],
 ]) {
   await choose.selectOption(selection)
   await row.getByRole('button', { name: label, exact: true }).click()
   await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain(password)
   expect(await page.evaluate(() => navigator.clipboard.readText())).not.toContain(other)
   await expect(page.locator('dialog[open]')).toHaveCount(0)
   await expect(page.locator('.copy-toast')).toBeVisible()
   await expect(page.locator('.notice').filter({ hasText: '已复制' })).toHaveCount(0)
   if (selection === 'default') {
     const copied = await page.evaluate(() => navigator.clipboard.readText())
     const access = await (await page.request.post(`/api/targets/${fixture.target.id}/relays/default/credentials`, {data:{}})).json()
     expect(copied).toBe(`目标：${fixture.target.name}（${fixture.target.user}@${fixture.target.host}:${fixture.target.port}）\n连接：ssh -p ${access.ssh_port} ${access.credential.username}@${access.ssh_host || '127.0.0.1'}\n密码：${password}\n凭证有效期：永久有效`)
   }
 }
 await row.getByRole('button', { name: '笔记', exact: true }).click()
 const note = page.getByRole('dialog', { name: `${fixture.target.name} · 笔记` })
 await note.getByLabel('笔记内容').fill('列表右侧笔记验收')
 await note.getByRole('button', { name: '保存笔记' }).click()
 await expect(note.getByRole('status')).toHaveText('笔记已保存')
 await note.getByRole('button', { name: '关闭', exact: true }).click()
 await row.getByRole('button', { name: '笔记', exact: true }).click()
 await expect(note.getByLabel('笔记内容')).toHaveValue('列表右侧笔记验收')
})

test('全局白名单卡片统计有效 IP，点击管理并同步删除', async ({ page }) => {
 const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await page.goto('/')
 await page.getByLabel('管理员密码').fill(fixture.admin_password)
 await page.getByRole('button', { name: '进入中转台' }).click()
 await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
 // 提供跨两页数据，验证卡片统计全部有效项而非仅当前页。
 const ips = [
   { ip: '192.0.2.1', expires_at: new Date(Date.now()-60000).toISOString() },
   { ip: '192.0.2.2', expires_at: new Date(Date.now()+3600000).toISOString() },
   { ip: '192.0.2.3', expires_at: new Date(Date.now()+3600000).toISOString() },
 ]
 await page.route('**/api/global-ips?**', async route => {
   const p = new URL(route.request().url()).searchParams
   const items = p.get('page_size') === '100' ? (p.get('page') === '1' ? ips.slice(0,2) : ips.slice(2)) : ips
   await route.fulfill({ json: { items, total: ips.length, page: Number(p.get('page')), page_size: Number(p.get('page_size')) } })
 })
 await page.route('**/api/global-ips', async route => {
   if (route.request().method() === 'DELETE') { ips.splice(ips.findIndex(x=>x.ip===route.request().postDataJSON().ip),1); await route.fulfill({json:{}}) }
   else await route.continue()
 })
 await page.reload()
 const card = page.getByRole('button', { name: '全局 IP 白名单', exact: true })
 await expect(card).toHaveCount(1)
 await expect(card).toContainText('2 个有效')
 await expect(card).toContainText('192.0.2.2')
 await expect(card).not.toContainText('192.0.2.1')
 await page.screenshot({ path: '/tmp/ssh-list-revised.png' })
 await card.click()
 const dialog = page.getByRole('dialog', { name: '全局 IP 白名单', exact: true })
 await dialog.getByRole('row').filter({hasText:'192.0.2.2'}).getByRole('button',{name:'删除',exact:true}).click()
 await expect(card).toContainText('1 个有效')
 await expect(card).toContainText('192.0.2.3')
 await dialog.getByRole('button',{name:'关闭',exact:true}).click()
 await page.setViewportSize({ width:390,height:844 })
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({ path: '/tmp/ssh-list-revised-mobile.png' })
})


test('来源列表显示全部 IP，长列表可滚动且关闭按钮可见', async ({ page }) => {
 const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await page.goto('/')
 await page.getByLabel('管理员密码').fill(fixture.admin_password)
 await page.getByRole('button', {name:'进入中转台'}).click()
 await expect(page.getByRole('heading', {name:'SSH 连接', exact:true})).toBeVisible()
 const id = 'source-list-dialog-test'
 const sources = Array.from({length:40}, (_,i)=>`192.0.2.${i+1}`)
 const created = await page.request.post('/api/targets', {data:{...fixture.multi_target,id,name:'来源列表验收',allowed_sources:sources,source_mode:'custom'}})
 expect(created.ok()).toBe(true)
 try {
   await page.reload()
   const row=page.getByRole('row').filter({hasText:'来源列表验收'})
   await expect(row.locator('.source-preview')).toHaveText(sources[0])
   await row.getByRole('button',{name:'来源列表验收 查看允许来源 IP'}).click()
   const dialog=page.getByRole('dialog',{name:'允许来源 IP',exact:true})
   const content = dialog.getByRole('textbox', {name:'全部允许来源 IP'})
   await expect(content).toHaveValue(sources.join('\n'))
   await expect(content).toHaveAttribute('readonly', '')
   await content.evaluate(element => { element.scrollTop = element.scrollHeight })
   expect(await content.evaluate(element => element.scrollTop > 0)).toBe(true)
   await expect(dialog.getByRole('button',{name:'关闭',exact:true})).toBeInViewport()
   await page.setViewportSize({width:390,height:844})
   await expect(dialog.getByRole('button',{name:'关闭',exact:true})).toBeInViewport()
   await page.screenshot({path:'/tmp/ssh-source-list-mobile.png'})
   await dialog.getByRole('button',{name:'关闭',exact:true}).click()
   await expect(dialog).not.toBeVisible()
 } finally {await page.request.delete(`/api/targets/${id}`,{data:{}})}
})
