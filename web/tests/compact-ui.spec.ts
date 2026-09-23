import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

for (const theme of ['light', 'dark']) {
 test(`Global compact layout keeps headers, statistics and menus usable (${theme})`, async ({ page }) => {
  const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.addInitScript(value => localStorage.setItem('ssh-gateway:theme', value), theme)
  await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
  for (const language of ['en','zh-CN']) {
   await page.goto('/?lang='+language)
   await expect(page.locator('.stats')).toBeVisible()
   for (const width of [1440,1024,800]) {
    await page.setViewportSize({width,height:800})
    const header = (await page.locator('.topbar').boundingBox())!
    expect(header.height).toBeLessThanOrEqual(60)
    const cards = await page.locator('.stats .stat-card').evaluateAll(nodes => nodes.map(node => node.getBoundingClientRect().height))
    expect(Math.max(...cards)).toBeLessThanOrEqual(55)
    const table = page.locator('.connection-list .table-scroll:visible')
    expect((await table.boundingBox())!.y).toBeLessThan(290)
    expect(await table.evaluate(node => node.scrollWidth <= node.clientWidth + 1), `Table should fit ${width}px viewport (${language})`).toBe(true)
    await page.locator('[popovertarget="system-settings-menu"]').click()
    await expect(page.locator('#system-settings-menu')).toBeVisible()
    const menu = (await page.locator('#system-settings-menu').boundingBox())!
    expect(menu.x).toBeGreaterThanOrEqual(0)
    expect(menu.x+menu.width).toBeLessThanOrEqual(width)
    await page.keyboard.press('Escape')
    if(language === 'zh-CN') await page.screenshot({path:`test-results/compact-ui-${theme}-${width}.png`})
   }
  }
 })
}

for (const language of ['en','zh-CN']) {
 test(`Combined status column and persistent collapsible sidebar (${language})`, async ({ page }) => {
  const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.request.post('/api/login', {data:{username:'ssh-admin',password:f.admin_password}})
  await page.setViewportSize({width:800,height:800})
  await page.goto('/?lang='+language)
  const table=page.locator('.ssh-connections-table')
  await expect(table.locator('thead th')).toHaveCount(5)
  await expect(table.getByRole('columnheader',{name:language==='en'?'Status / forwarding':'状态 / 映射',exact:true})).toBeVisible()
  const sources=table.locator('.source-preview').first()
  await expect(sources.locator('code')).toBeVisible()
  await expect(sources.locator('small')).toHaveCount(0)
  const account = table.locator('tbody tr').first().locator('.connection-account-select')
  expect((await account.boundingBox())!.height).toBeLessThanOrEqual(25)
  expect((await sources.boundingBox())!.y + (await sources.boundingBox())!.height).toBeLessThanOrEqual((await account.boundingBox())!.y)
  await sources.click()
  await expect(page.locator('.source-details')).toBeVisible()
  await page.keyboard.press('Escape')
  const status=table.locator('tbody tr').first().locator('td').nth(2)
  await expect(status.locator('.status-pill')).toBeVisible()
  await status.getByRole('button').click()
  await expect(page.locator('.mapping-dialog[open]')).toBeVisible()
  await page.keyboard.press('Escape')
  const collapse=language==='en'?'Collapse menu':'收起菜单'
  const expand=language==='en'?'Expand menu':'展开菜单'
  await expect(page.locator('#workspace-sidebar')).toBeVisible()
  await expect(page.locator('#workspace-sidebar .nav-text').first()).not.toBeVisible()
  expect((await page.locator('#workspace-sidebar').boundingBox())!.width).toBe(52)
  await expect(page.getByRole('button',{name:expand,exact:true})).toHaveAttribute('aria-expanded','false')
  await page.getByRole('button',{name:expand,exact:true}).click()
  await expect(page.locator('#workspace-sidebar .nav-text').first()).toBeVisible()
  const before=(await page.locator('.workspace').boundingBox())!.width
  await page.getByRole('button',{name:collapse,exact:true}).click()
  await expect(page.locator('#workspace-sidebar')).toBeVisible()
  await expect(page.locator('#workspace-sidebar .nav-text').first()).not.toBeVisible()
  expect((await page.locator('#workspace-sidebar').boundingBox())!.width).toBe(52)
  await expect(page.getByRole('button',{name:expand,exact:true})).toHaveAttribute('aria-expanded','false')
  expect((await page.locator('.workspace').boundingBox())!.width).toBeGreaterThan(before)
  const nav=page.locator('#workspace-sidebar .nav-item')
  await nav.nth(1).click()
  await expect(nav.nth(1)).toHaveClass(/active/)
  await nav.first().click()
  await expect(nav.first()).toHaveClass(/active/)
  await page.reload()
  await expect(page.locator('#workspace-sidebar')).toBeVisible()
  await expect(page.locator('#workspace-sidebar .nav-text').first()).not.toBeVisible()
  expect((await page.locator('#workspace-sidebar').boundingBox())!.width).toBe(52)
  await page.screenshot({path:`test-results/sidebar-collapsed-${language}.png`})
  if(language === 'zh-CN') {
   await page.setViewportSize({width:1280,height:800})
   await page.screenshot({path:'test-results/sidebar-default-collapsed-desktop.png'})
  }
  await page.getByRole('button',{name:expand,exact:true}).focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('#workspace-sidebar')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('button',{name:collapse,exact:true})).toHaveAttribute('aria-expanded','true')
 })
}

for (const theme of ['light', 'dark']) {
 test(`Compact statistics across pages (${theme})`, async ({ page }) => {
  const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.addInitScript(value => localStorage.setItem('ssh-gateway:theme', value), theme)
  await page.request.post('/api/login', {data:{username:'ssh-admin',password:f.admin_password}})
  for (const language of ['en','zh-CN']) {
   await page.goto('/?lang='+language)
   for (const width of [1280,800,390]) {
    await page.setViewportSize({width,height:800})
    await page.locator('.nav-item').first().click()
    await expect(page.locator('.stats')).toBeVisible()
    const home=page.locator('.stats .stat-card')
    expect(Math.max(...await home.evaluateAll(nodes=>nodes.map(n=>n.getBoundingClientRect().height)))).toBeLessThanOrEqual(65)
    if(width===1280 && language==='zh-CN') await page.screenshot({path:`test-results/compact-stats-home-${theme}.png`})
    await page.locator('.nav-item').nth(1).click()
    await expect(page.locator('.mapping-stats')).toBeVisible()
    expect(Math.max(...await page.locator('.mapping-stats > div').evaluateAll(nodes=>nodes.map(n=>n.getBoundingClientRect().height)))).toBeLessThanOrEqual(50)
    if(width===1280 && language==='zh-CN') await page.screenshot({path:`test-results/compact-stats-mappings-${theme}.png`})
   }
  }
 })
}
