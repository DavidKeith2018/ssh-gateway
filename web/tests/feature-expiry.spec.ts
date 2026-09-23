import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
const fixture=()=>JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
test('凭证有效期：单行三选项、持久化、保留原期限及过期续期',async({page})=>{
 const f=fixture();await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await expect(page.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
 const input={...f.multi_target,id:'feature-expiry',name:'有效期测试机器'}
 input.relays=input.relays.map((r:any,i:number)=>({...r,username:'expiry-user-'+i,expires_at:null}))
 const created=await page.request.post('/api/targets',{data:input});expect(created.ok(),await created.text()).toBeTruthy();await page.reload()
 const row=page.getByRole('row').filter({hasText:'有效期测试机器'}),dialog=page.locator('dialog.target-editor'),group=dialog.getByRole('group',{name:'有效期',exact:true}).first()
 async function edit(){await row.getByRole('button',{name:'编辑',exact:true}).click();if(!await group.isVisible())await dialog.locator('[data-relays] .fold-toggle').click()}
 async function save(){await dialog.getByRole('button',{name:/保存/}).last().click();await expect(dialog).not.toBeVisible();return await(await page.request.get('/api/targets/feature-expiry')).json()}
 try{
  await edit();await expect(group.getByRole('button')).toHaveText(['永久','1小时','1天']);await expect(dialog.locator('input[type=datetime-local]')).toHaveCount(0)
  for(const width of [1440,390,320]){await page.setViewportSize({width,height:900});const boxes=await Promise.all((await group.getByRole('button').all()).map(b=>b.boundingBox()));expect(new Set(boxes.map(b=>b!.y)).size).toBe(1);for(const b of boxes){expect(b!.x).toBeGreaterThanOrEqual(0);expect(b!.x+b!.width).toBeLessThanOrEqual(width)}}
  await page.screenshot({path:'test-results/有效期-单行.png'});await page.setViewportSize({width:1440,height:1000})
  for(const [name,hours] of [['1小时',1],['1天',24]] as const){
   const start=Date.now();await group.getByRole('button',{name,exact:true}).click();await expect(group.getByRole('button',{name,exact:true})).toHaveAttribute('aria-pressed','true');const saved=await save();expect(Math.abs(Date.parse(saved.relays[0].expires_at)-start-hours*3600000)).toBeLessThan(65000);await edit()
  }
  const before=await(await page.request.get('/api/targets/feature-expiry')).json();const unchanged=await save();expect(unchanged.relays[0].expires_at).toBe(before.relays[0].expires_at)
  unchanged.relays[0].expires_at='2020-01-01T12:00:00Z';expect((await page.request.put('/api/targets/feature-expiry',{data:unchanged})).ok()).toBe(true);await page.reload();await expect(row).toContainText('已过期')
  const denied=await page.request.post(`/api/targets/feature-expiry/relays/${unchanged.relays[0].id}/credentials`,{data:{}});expect(denied.status()).toBe(409)
  await edit();await group.getByRole('button',{name:'永久',exact:true}).click();const renewed=await save();expect(renewed.relays[0].expires_at??null).toBeNull();await expect(row).not.toContainText('已过期')
  await page.locator('.language-trigger').click();await page.locator('.language-menu:popover-open').getByRole('button',{name:'English',exact:true}).click();await row.getByRole('button',{name:'Edit',exact:true}).click();const english=dialog.getByRole('group',{name:'Expiry',exact:true}).first();if(!await english.isVisible())await dialog.locator('[data-relays] .fold-toggle').click();await expect(english.getByRole('button')).toHaveText(['Never','1 hour','1 day'])
 }finally{await page.request.delete('/api/targets/feature-expiry',{data:{}})}
})
