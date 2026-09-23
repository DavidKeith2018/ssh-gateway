import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { desktopFixture } from './desktop-fixture'
const fixture=()=>JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
test('备份：密码确认、验证失败和加密下载',async({page})=>{
 await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(fixture().admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await page.getByRole('button',{name:'系统设置'}).click();await page.getByRole('button',{name:'备份与恢复',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'备份与恢复',exact:true})
 await dialog.getByLabel('备份验证：管理员密码').fill('wrong-password')
 await dialog.getByLabel('备份密码',{exact:true}).fill('backup-password-test')
 await dialog.getByLabel('确认备份密码').fill('different-password')
 await dialog.getByRole('button',{name:'导出加密备份'}).click();await expect(dialog.getByRole('alert')).toContainText('两次输入')
 await dialog.getByLabel('确认备份密码').fill('backup-password-test');await dialog.getByRole('button',{name:'导出加密备份'}).click();await expect(dialog.getByRole('alert')).toContainText('管理员密码不正确')
 await dialog.getByLabel('备份验证：管理员密码').fill(fixture().admin_password)
 await dialog.getByLabel('备份密码',{exact:true}).fill('backup-password-test');await dialog.getByLabel('确认备份密码').fill('backup-password-test')
 const download=page.waitForEvent('download');await dialog.getByRole('button',{name:'导出加密备份'}).click();const file=await download
 const bytes=readFileSync((await file.path())!);expect(bytes.subarray(0,8).toString()).toBe('SGBACK01');expect(bytes.includes(Buffer.from(fixture().target.target_password))).toBe(false)
 await expect(dialog.getByLabel('备份密码',{exact:true})).toHaveValue('')
 for(const width of [1440,390,320]){await page.setViewportSize({width,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
 await dialog.getByRole('button',{name:'关闭'}).click()
})

test('备份：英文界面与普通用户权限',async({page,browser})=>{
 const f=fixture();await page.goto('/?lang=en');await page.getByLabel('Administrator password',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'Sign in →',exact:true}).click()
 await page.getByRole('button',{name:'System settings'}).click();await page.getByRole('button',{name:'Backup and restore',exact:true}).click();const dialog=page.getByRole('dialog',{name:'Backup and restore',exact:true});await expect(dialog.getByRole('heading',{name:'Backup and restore'})).toBeVisible()
 const created=await page.request.post('/api/users',{data:{username:'feature-backup-reader',password:'reader-password-test',enabled:true,target_ids:[f.target.id],tags:[]}});expect(created.ok()).toBe(true)
 const user=await created.json();const context=await browser.newContext();try{const p=await context.newPage();await p.goto('/?lang=zh-CN');await p.getByLabel('用户名',{exact:true}).fill('feature-backup-reader');await p.getByLabel('登录密码').fill('reader-password-test');await p.getByRole('button',{name:'进入中转台'}).click();await expect(p.getByRole('heading',{name:'SSH 连接',exact:true})).toBeVisible()
 for(const name of ['系统设置','安全密码加密','批量导入机器','备份与恢复','连接诊断','编辑']){await expect(p.getByRole('button',{name,exact:true})).toHaveCount(0)}
 expect((await p.request.post('/api/import/commit',{data:{rows:[]}})).status()).toBe(403)
 expect((await p.request.get('/api/diagnostics/permission-test')).status()).toBe(403)
 expect((await p.request.delete('/api/diagnostics/permission-test',{data:{}})).status()).toBe(403)
 expect((await p.request.put('/api/targets/'+f.target.id,{data:{...f.target,relay_expires_at:'2099-01-01T00:00:00Z'}})).status()).toBe(403)
 expect((await p.request.post('/api/security/master-password',{data:{action:'enable'}})).status()).toBe(403)
 expect((await p.request.post('/api/backups',{data:{password:'backup-password-test',admin_password:f.admin_password}})).status()).toBe(403)
 expect((await p.request.post('/api/import/preview',{data:{format:'json',content:'[]'}})).status()).toBe(403)
 expect((await p.request.post('/api/targets/'+f.target.id+'/diagnostics',{data:{connection:'server:default'}})).status()).toBe(403)
 }finally{await context.close();await page.request.delete('/api/users/'+user.id,{data:{}})}
})


test('桌面备份：调用保存桥接、取消与成功后清除密码',async({page})=>{
 const f=fixture();let saves=0,accept=false
 await desktopFixture(page,{
  Info:()=>({ready:true,needs_setup:false,active:0,settings:{external:false,port:2222,connect_host:'127.0.0.1'},data_dir:'/测试',error:''}),
  Call:async(method:string,path:string,body:string)=>{const response=await page.request.fetch('/api'+path,{method,data:body?JSON.parse(body):undefined});return {status:response.status(),data:await response.json()}},
  SaveBackup:(data:string)=>{expect(Buffer.from(data,'base64').subarray(0,8).toString()).toBe('SGBACK01');saves++;return accept},
 })
 await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click();await page.getByRole('button',{name:'系统设置'}).click();await page.getByRole('button',{name:'备份与恢复',exact:true}).click()
 const d=page.getByRole('dialog',{name:'备份与恢复',exact:true})
 for(const accepted of [false,true]){
  accept=accepted;await d.getByLabel('备份验证：管理员密码').fill(f.admin_password);await d.getByLabel('备份密码',{exact:true}).fill('desktop-backup-password');await d.getByLabel('确认备份密码').fill('desktop-backup-password');await d.getByRole('button',{name:'导出加密备份'}).click();await expect(d.getByRole('status')).toContainText(accepted?'已生成加密备份':'已取消');await expect(d.getByLabel('备份密码',{exact:true})).toHaveValue('')
 }
 expect(saves).toBe(2)
})
