import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('终端打开独立窗口，列表和筛选保留，拦截时提示', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await page.getByLabel('搜索连接').fill(fixture.target.name)
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  const popupPromise = page.waitForEvent('popup')
  await row.getByRole('button', { name: '连接终端', exact:true }).click()
  const popup = await popupPromise
  try {
    await expect(popup).toHaveURL(new RegExp(`machine=${fixture.target.id}.*window=1`))
    const machine = popup.getByRole('main', { name: '终端机器页面' })
    await expect(machine).toBeVisible()
    await expect(popup).toHaveTitle(fixture.target.name + ' — WebSSH')
    await expect(page).toHaveTitle('SSH Gateway')
    await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
    expect(await popup.evaluate(() => window.opener === null)).toBe(true)
    await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
    await expect(page.getByLabel('搜索连接')).toHaveValue(fixture.target.name)
    await expect(page.getByRole('main', { name: '终端机器页面' })).toHaveCount(0)
  } finally { await popup.close() }
  await expect(row).toBeVisible()
  await page.evaluate(() => { window.open = () => null })
  await row.getByRole('button', { name: '连接终端', exact:true }).click()
  await expect(page.locator('.notice-error')).toContainText('允许本站弹出窗口')
  await expect(page.getByRole('main', { name: '终端机器页面' })).toHaveCount(0)
})

test('旧机器链接未指定账号时选择首个启用的中转账号', async ({ page, context }) => {
 const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await page.goto('/')
 await page.getByLabel('管理员密码').fill(fixture.admin_password)
 await page.getByRole('button', { name: '进入中转台' }).click()
 await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
 const id = 'default-terminal-account'
 const input = { ...fixture.multi_target, id, name: '默认账号回归', default_login_id: 'root-login' }
 const created = await page.request.post('/api/targets', { data: input })
 expect(created.ok()).toBe(true)
 try {
  for (const user of ['ubuntu', 'root']) {
   const machinePage = await context.newPage()
   try {
    await machinePage.goto('/?window=1&machine=' + id)
    await expect(machinePage.locator('.terminal-tab.active i.online')).toBeVisible()
    await expect(machinePage.getByRole('button', { name: '查看 SSH 连接信息' })).toContainText(user + '@')
    await expect.poll(() => machinePage.locator('.xterm-screen').innerText()).toContain(user)
   } finally { await machinePage.close() }
   if (user === 'ubuntu') {
    const saved = await (await page.request.get('/api/targets/' + id)).json()
    saved.relays[0].enabled = false
    const changed = await page.request.put('/api/targets/' + id, { data: saved })
    expect(changed.ok()).toBe(true)
   }
  }
 } finally { await page.request.delete('/api/targets/' + id) }
})


test('所选中转与服务器账号使用对应密码和私钥连接', async ({page}) => {
 const fixture=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
 await page.goto('/')
 await page.getByLabel('管理员密码').fill(fixture.admin_password)
 await page.getByRole('button',{name:'进入中转台'}).click()
 await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
 const id='selected-terminal-account'
 const created=await page.request.post('/api/targets',{data:{...fixture.multi_target,id,name:'选定账号终端验收'}})
 expect(created.ok()).toBe(true)
 try {
  await page.reload()
  const row=page.getByRole('row').filter({hasText:'选定账号终端验收'})
  for(const [selection,user] of [['ubuntu-relay','ubuntu'],['root-relay','root'],['server:root-login','root']]) {
   await row.getByRole('combobox').selectOption(selection)
   const opened=page.waitForEvent('popup')
   await row.getByRole('button',{name:'连接终端',exact:true}).click()
   const popup=await opened
   try {
    await expect(popup).toHaveURL(new RegExp('connection='+encodeURIComponent(selection)))
    const machine=popup.getByRole('main',{name:'终端机器页面'})
    await expect(machine.getByRole('button',{name:'查看 SSH 连接信息'})).toContainText(user+'@')
    await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
    await expect.poll(()=>machine.locator('.xterm-screen').innerText()).toContain(user)
    await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
   } finally { await popup.close() }
  }
 } finally {await page.request.delete(`/api/targets/${id}`,{data:{}})}
})
