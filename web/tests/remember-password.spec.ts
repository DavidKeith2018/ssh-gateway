import {test,expect} from '@playwright/test'
import {readFileSync} from 'node:fs'
import {desktopFixture} from './desktop-fixture'

for(const language of ['en','zh-CN']) {
 test(`Desktop remembers login only with consent (${language})`,async({page,baseURL})=>{
  const f=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
  let saved={username:'',password:''},cookie='',writes=0,deletes=0
  await desktopFixture(page,{
   Info:()=>({ready:true,needs_setup:false,data_dir:'/fictional-data',settings:{},version:'test'}),
   RememberedLogin:()=>saved,
   SaveRememberedLogin:(username:string,password:string)=>{saved={username,password};writes++},
   ForgetRememberedLogin:()=>{saved={username:'',password:''};deletes++},
   Call:async(method:string,path:string,body:string)=>{
    const response=await fetch(`${baseURL}/api${path}`,{method,headers:{'Content-Type':'application/json',...(cookie?{Cookie:cookie}:{})},body:method==='GET'?undefined:body})
    cookie=response.headers.get('set-cookie')?.split(';')[0]||cookie
    return {status:response.status,data:await response.json()}
   },
  })
  await page.goto('/?lang='+language)
  const remember=page.getByLabel(language==='en'?'Remember password on this computer':'在本机记住密码',{exact:true})
  await expect(remember).not.toBeChecked()
  await page.locator('#admin-password').fill(f.admin_password)
  await remember.check()
  await page.locator('.login-button').click()
  await expect(page.locator('.app-layout')).toBeVisible()
  await expect.poll(()=>writes).toBe(1)
  expect(saved.password).toBe(f.admin_password)
  expect(await page.evaluate(()=>JSON.stringify({...localStorage}))).not.toContain(f.admin_password)
  cookie=''
  await page.reload()
  await expect(page.locator('#admin-password')).toHaveValue(f.admin_password)
  await expect(remember).toBeChecked()
  await remember.uncheck()
  await expect.poll(()=>deletes).toBe(1)
  await page.reload()
  await expect(page.locator('#admin-password')).toHaveValue('')
  await expect(remember).not.toBeChecked()
 })
}

test('First setup requires password preservation confirmation',async({page})=>{
 let setupCalls=0
 await desktopFixture(page,{
  Info:()=>({ready:true,needs_setup:true,data_dir:'/fictional-setup',settings:{},version:'test'}),
  Call:(_method:string,path:string)=>{
   if(path==='/security') return {status:200,data:{enabled:false,locked:false}}
   if(path==='/desktop/setup') {setupCalls++;return {status:200,data:{ok:true}}}
   return {status:401,data:{error:'Test session'}}
  },
 })
 await page.goto('/?lang=zh-CN')
 await page.locator('#admin-password').fill('setup-test-password')
 await expect(page.getByText(/请务必将管理员密码保存在安全的地方/)).toBeVisible()
 await page.locator('.login-button').click()
 expect(setupCalls).toBe(0)
 await page.getByLabel('我已妥善保存管理员密码',{exact:true}).check()
 await page.locator('.login-button').click()
 await expect.poll(()=>setupCalls).toBe(1)
})

test('Restart requires manual password even when login is remembered',async({page})=>{
 let locked=true,unlocks=0,logins=0
 await desktopFixture(page,{
  Info:()=>({ready:true,needs_setup:false,data_dir:'/fictional-locked',settings:{},version:'test'}),
  RememberedLogin:()=>({username:'ssh-admin',password:'remembered-unlock-password'}),
  Call:(_method:string,path:string)=>{
   if(path==='/security')return {status:200,data:{enabled:true,locked}}
   if(path==='/unlock'){unlocks++;locked=false;return {status:200,data:{enabled:true,locked:false}}}
   if(path==='/login')logins++
   return {status:401,data:{error:'Sign in'}}
  },
 })
 await page.goto('/?lang=zh-CN')
 await expect(page.getByRole('heading',{name:'解锁服务器凭证'})).toBeVisible()
 await expect(page.getByLabel('管理员密码',{exact:true})).toHaveValue('')
 await expect(page.getByLabel('在本机记住密码',{exact:true})).toHaveCount(0)
 await page.getByLabel('管理员密码',{exact:true}).fill('manually-entered-password')
 expect(unlocks).toBe(0);expect(logins).toBe(0)
 await page.getByRole('button',{name:'解锁凭证 →',exact:true}).click()
 await expect(page.locator('#admin-password')).toHaveValue('manually-entered-password')
 expect(unlocks).toBe(1);expect(logins).toBe(0)
})

test('Rejected remembered login can be forgotten',async({page})=>{
 let forgotten=0
 await desktopFixture(page,{
  Info:()=>({ready:true,needs_setup:false,data_dir:'/fictional-stale',settings:{},version:'test'}),
  RememberedLogin:()=>({username:'ssh-admin',password:'stale-saved-password'}),
  ForgetRememberedLogin:()=>{forgotten++},
  Call:(_method:string,path:string)=>path==='/security'?{status:200,data:{enabled:false,locked:false}}:{status:401,data:{error:'Incorrect password'}},
 })
 await page.goto('/?lang=en')
 await expect(page.locator('#admin-password')).toHaveValue('stale-saved-password')
 await page.locator('.login-button').click()
 await expect.poll(()=>forgotten).toBe(1)
 await expect(page.getByLabel('Remember password on this computer',{exact:true})).not.toBeChecked()
})
