import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
test('系统设置子菜单、中转入口、语言菜单和导入窄屏布局',async({page})=>{
 const f=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
 await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
 await expect(page.getByRole('row').filter({hasText:f.target.name})).toBeVisible()
 const relay=page.locator('.sidebar-bottom').getByRole('button',{name:'中转访问设置'})
 await expect(relay).toBeVisible();expect(await page.locator('.topbar').getByRole('button',{name:'中转访问设置'}).count()).toBe(0)
 const port=await page.locator('.sidebar-bottom .muted').first().boundingBox();const entry=await relay.boundingBox();expect(entry!.y).toBeGreaterThanOrEqual(port!.y+port!.height)
 await relay.click();await expect(page.getByRole('dialog').getByText('建议不要将中转服务发布到公网',{exact:false})).toBeVisible();await page.keyboard.press('Escape')
 const theme=await page.locator('.theme-toggle').boundingBox(),language=await page.locator('.language-trigger').boundingBox();expect(language!.x).toBeGreaterThan(theme!.x)
 await page.locator('.language-trigger').click();await expect(page.locator('.language-menu:popover-open')).toBeVisible();await page.keyboard.press('Escape');await expect(page.locator('.language-menu:popover-open')).toHaveCount(0)
 for(const width of [1440,390,320]){
  await page.setViewportSize({width,height:900})
  await page.getByRole('button',{name:'系统设置'}).click()
  const menu=page.locator('#system-settings-menu');await expect(menu.getByRole('button')).toHaveCount(3)
  const box=await menu.boundingBox();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(width)
  await menu.getByRole('button',{name:'批量导入机器'}).click()
  const d=page.getByRole('dialog',{name:'批量导入机器'});await expect(d).toBeVisible();await expect(menu).not.toBeVisible()
  for(const control of await d.locator('input,textarea,select').all()){const b=await control.boundingBox();expect(b!.x).toBeGreaterThanOrEqual(0);expect(b!.x+b!.width).toBeLessThanOrEqual(width)}
  await page.screenshot({path:`test-results/系统设置-导入-${width}.png`})
  await page.keyboard.press('Escape')
 }
 await page.setViewportSize({width:1440,height:900});await page.locator('.theme-toggle').click();await page.getByRole('button',{name:'系统设置'}).click();await page.getByRole('button',{name:'批量导入机器',exact:true}).click();await page.screenshot({path:'test-results/系统设置-导入-深色.png'})
})

test('菜单箭头垂直居中，新增设置提供完整英文',async({page})=>{
 const f=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
 await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
 await expect(page.getByRole('row').filter({hasText:f.target.name})).toBeVisible()
 for(const lang of ['zh-CN','en']){
  if(lang==='en'){await page.locator('.language-trigger').click();await page.locator('.language-menu:popover-open').getByRole('button',{name:'English',exact:true}).click()}
  for(const width of [1440,390]){
   await page.setViewportSize({width,height:900})
   for(const trigger of await page.locator('.menu-trigger').all()){
    const label=await trigger.locator(':scope > span').boundingBox(),arrow=await trigger.locator('svg').boundingBox()
    expect(Math.abs(label!.y+label!.height/2-arrow!.y-arrow!.height/2)).toBeLessThanOrEqual(1)
   }
  }
 }
 await page.setViewportSize({width:1440,height:900})
 for(const title of ['Password encryption','Import machines','Backup and restore']){
  await page.getByRole('button',{name:'System settings'}).click()
  const menu=page.locator('#system-settings-menu');await expect(menu).not.toContainText(/[\u4e00-\u9fff]/)
  await menu.getByRole('button',{name:title,exact:true}).click()
  const dialog=page.getByRole('dialog');await expect(dialog.getByRole('heading',{name:title,exact:true})).toBeVisible();await expect(dialog).not.toContainText(/[\u4e00-\u9fff]/)
  await page.keyboard.press('Escape')
 }
 await page.locator('.sidebar-bottom .relay-settings-link').click()
 await expect(page.getByRole('dialog')).toContainText('Avoid exposing the relay service to the public internet.')
 await expect(page.getByRole('dialog')).not.toContainText(/[\u4e00-\u9fff]/)
 await page.keyboard.press('Escape')
 await page.screenshot({path:'test-results/菜单箭头居中-英文.png'})
})
