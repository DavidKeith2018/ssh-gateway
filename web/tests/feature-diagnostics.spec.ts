import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
const fixture=()=>JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
test('连接诊断：完整步骤、选择账号及指纹失败',async({page})=>{
 const f=fixture();await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
 f.target={...f.target,id:'feature-diagnostic',name:'诊断专用测试机器',relay_user:'feature-diagnostic-relay'}
 expect((await page.request.post('/api/targets',{data:f.target})).ok()).toBe(true);await page.reload()
 await expect(page.getByRole('button',{name:'连接诊断',exact:true})).toHaveCount(0)
 await page.goto('/?window=1&machine='+encodeURIComponent(f.target.id)+'&connection=server:default')
 await page.getByRole('button',{name:'连接诊断',exact:true}).click();const d=page.getByRole('dialog',{name:'连接诊断'})
 await expect(d).toHaveCSS('background-color','rgb(255, 255, 255)');await expect(d).toHaveCSS('color','rgb(36, 50, 71)');const account=d.getByLabel('诊断账号');await expect(account).toHaveValue('server:default');await expect(account).toHaveCSS('appearance','none');await expect(account).toHaveCSS('border-radius','9px');await account.focus();await expect(account).toBeFocused();await account.selectOption('server:default');await d.getByRole('button',{name:'开始诊断'}).click()
 await expect(d.locator('li').filter({hasText:'身份认证'})).toContainText('通过');await expect(d.getByRole('button',{name:'开始诊断'})).toBeVisible()
 const current=await(await page.request.get('/api/targets/'+f.target.id)).json()
 expect((await page.request.put('/api/targets/'+f.target.id,{data:{...current,host_fingerprint:'SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'}})).ok()).toBeTruthy()
 await d.getByRole('button',{name:'开始诊断'}).click();await expect(d).toContainText('指纹不匹配');await expect(d.locator('li').filter({hasText:'身份认证'})).toContainText('未执行')
 for(const width of [1440,390,320]){await page.setViewportSize({width,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);const box=await account.boundingBox();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(width)}
 await page.evaluate(()=>window.dispatchEvent(new StorageEvent('storage',{key:'ssh-gateway:theme',newValue:'dark'})));await expect(account).toBeVisible();await expect(d).toHaveCSS('background-color','rgb(38, 53, 72)');await expect(d).toHaveCSS('color','rgb(237, 242, 249)');await page.screenshot({path:'test-results/连接诊断-深色选择器.png'});await page.request.delete('/api/targets/'+f.target.id,{data:{}})
})

test('连接诊断：取消任务并可再次执行',async({page})=>{
 const f=fixture();let cancelled=false
 await page.route('**/api/targets/*/diagnostics',route=>route.fulfill({status:202,json:{id:'slow-job'}}))
 await page.route('**/api/diagnostics/slow-job',route=>{if(route.request().method()==='DELETE'){cancelled=true;return route.fulfill({json:{ok:true}})}return route.fulfill({json:{done:false,steps:[{name:'configuration',status:'passed',code:'ok',duration_ms:1}],location:'gateway'}})})
 await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click();await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible();await page.goto('/?window=1&machine='+encodeURIComponent(f.target.id));await page.getByRole('button',{name:'连接诊断',exact:true}).click()
 const d=page.getByRole('dialog',{name:'连接诊断'});await d.getByRole('button',{name:'开始诊断'}).click();await expect(d.locator('li').first()).toContainText('通过');await expect(d.getByLabel('诊断账号')).toBeDisabled();await d.getByRole('button',{name:'取消诊断'}).click();await expect.poll(()=>cancelled).toBe(true);await expect(d.getByLabel('诊断账号')).toBeEnabled();await expect(d.getByRole('button',{name:'开始诊断'})).toBeVisible()
})
