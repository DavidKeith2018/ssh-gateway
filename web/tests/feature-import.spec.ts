import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
const fixture=()=>JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
test('批量导入：SSH 配置补齐、指纹确认、原子新增与重复跳过',async({page})=>{
 const f=fixture();await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await page.getByRole('button',{name:'系统设置'}).click();await page.getByRole('button',{name:'批量导入机器',exact:true}).click();const d=page.getByRole('dialog',{name:'批量导入机器'})
 await d.getByLabel('配置内容',{exact:true}).fill(`Host 导入测试\n HostName ${f.target.host}\n Port ${f.target.port}\n User import-user\n`)
 await d.getByRole('button',{name:'预览导入'}).click();await expect(d.getByLabel('目标密码',{exact:true})).toBeVisible()
 await d.getByLabel('目标密码',{exact:true}).fill('import-password-123');await d.getByLabel('主机指纹',{exact:true}).fill(f.target.host_fingerprint)
 await d.getByRole('button',{name:'确认导入选中机器'}).click();await expect(d).toBeVisible()
 await d.getByLabel('已通过可信渠道核对该主机指纹').check();await d.getByRole('button',{name:'确认导入选中机器'}).click();await expect(d.getByRole('status')).toHaveText('已导入 1 台机器')
 await d.getByLabel('导入格式').selectOption('json');await d.getByLabel('配置内容',{exact:true}).fill(JSON.stringify([{...f.target,id:'ignored',name:'重复导入',user:'import-user',relay_user:''}]))
 await d.getByRole('button',{name:'预览导入'}).click();await expect(d).toContainText('重复记录，已跳过');await expect(d.locator('input[type=checkbox]').first()).toBeDisabled()
 for(const width of [1440,390,320]){await page.setViewportSize({width,height:900});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)}
 await d.getByRole('button',{name:'重新选择文件'}).click();await d.getByLabel('导入格式').selectOption('ssh_config');await d.getByLabel('配置内容',{exact:true}).fill('Host dangerous\n ProxyCommand touch /tmp/not-allowed')
 await d.getByRole('button',{name:'预览导入'}).click();await expect(d).toContainText('包含不支持的配置');await expect(d.locator('input[type=checkbox]').first()).toBeDisabled()
 await d.getByRole('button',{name:'关闭',exact:true}).click()
})

test('批量导入：切换认证方式清除隐藏凭证并能实际提交',async({page})=>{
 const f=fixture(),ids:string[]=[]
 await page.goto('/');await page.getByLabel('管理员密码',{exact:true}).fill(f.admin_password);await page.getByRole('button',{name:'进入中转台'}).click()
 await page.getByRole('button',{name:'系统设置'}).click();await page.getByRole('button',{name:'批量导入机器',exact:true}).click()
 const d=page.getByRole('dialog',{name:'批量导入机器'})
 try{
  for(const mode of ['password','private_key']){
   await d.getByLabel('配置内容',{exact:true}).fill(`Host auth-switch-${mode}\nHostName = ${f.target.host}\nPort = ${f.target.port}\nUser = import-${mode}\n`)
   await d.getByRole('button',{name:'预览导入'}).click();const auth=d.getByRole('combobox',{name:'认证方式',exact:true})
   if(mode==='password'){
    await auth.selectOption('private_key');await d.locator('input[type=file]').setInputFiles({name:'key.pem',mimeType:'text/plain',buffer:Buffer.from(f.key_targets[0].target_private_key)})
    await d.getByLabel('私钥密码（可选）',{exact:true}).fill('discard-this-passphrase');await auth.selectOption('password');await d.getByLabel('目标密码',{exact:true}).fill('import-switch-password')
   }else{
    await d.getByLabel('目标密码',{exact:true}).fill('discard-this-password');await auth.selectOption('private_key');await d.locator('input[type=file]').setInputFiles({name:'key.pem',mimeType:'text/plain',buffer:Buffer.from(f.key_targets[0].target_private_key)})
   }
   await d.getByLabel('主机指纹',{exact:true}).fill(f.target.host_fingerprint);await d.getByLabel('已通过可信渠道核对该主机指纹').check()
   const sent=page.waitForRequest(r=>r.url().endsWith('/api/import/commit')),response=page.waitForResponse(r=>r.url().endsWith('/api/import/commit'))
   await d.getByRole('button',{name:'确认导入选中机器'}).click()
   const body=(await sent).postDataJSON().rows[0];if(mode==='password'){expect(body.target_private_key).toBe('');expect(body.target_key_passphrase).toBe('')}else{expect(body.target_password).toBe('')}
   const result=await response;expect(result.ok(),await result.text()).toBe(true);ids.push(...(await result.json()).ids);await expect(d.getByRole('status')).toHaveText('已导入 1 台机器')
  }
 }finally{for(const id of ids)await page.request.delete('/api/targets/'+id,{data:{}})}
})
