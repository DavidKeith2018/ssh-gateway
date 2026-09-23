import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

const fixture = () => JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))

test('Favorites migrate to the server and survive a clean browser session', async ({ page, browser }) => {
 const f = fixture()
 const endpoint = `/api/targets/${f.target.id}/favorites`
 await page.request.post('/api/login', {data:{username:'ssh-admin',password:f.admin_password}})
 await page.goto('/?lang=en')
 const legacy = [{path:'/favorite-demo/file.txt',name:'file.txt',kind:'file'}, {path:'/favorite-demo/folder',name:'folder',kind:'directory'}]
 await page.evaluate(({id, legacy}) => localStorage.setItem('ssh-gateway:favorites:'+id, JSON.stringify(legacy)), {id:f.target.id,legacy})
 await page.goto('/?lang=en&window=1&machine='+f.target.id)
 for (const entry of legacy) await expect(page.locator('.favorites').getByRole('button',{name:entry.name,exact:true})).toBeVisible()
 await expect.poll(() => page.evaluate(id => localStorage.getItem('ssh-gateway:favorites:'+id),f.target.id)).toBeNull()
 const context = await browser.newContext({baseURL:new URL(page.url()).origin})
 try {
  const fresh = await context.newPage()
  await fresh.request.post('/api/login',{data:{username:'ssh-admin',password:f.admin_password}})
  await fresh.goto('/?lang=en&window=1&machine='+f.target.id)
  for (const entry of legacy) await expect(fresh.locator('.favorites').getByRole('button',{name:entry.name,exact:true})).toBeVisible()
  const row = fresh.locator('.favorite-row').filter({has: fresh.getByRole('button',{name:'file.txt',exact:true})})
  await row.getByRole('button').last().click()
  await expect(row).toHaveCount(0)
  await page.reload()
  await expect(page.locator('.favorites').getByRole('button',{name:'folder',exact:true})).toBeVisible()
  await expect(page.locator('.favorites').getByRole('button',{name:'file.txt',exact:true})).toHaveCount(0)
 } finally { await context.close(); await page.request.post(endpoint,{data:{op:'remove',entries:legacy}}) }
})

test('Failed migration retains the local copy and retries without duplicates', async ({ page }) => {
 const f = fixture()
 await page.request.post('/api/login',{data:{username:'ssh-admin',password:f.admin_password}})
 await page.goto('/?lang=en')
 const entry = {path:'/retry-favorite.txt',name:'retry-favorite.txt',kind:'file'}
 await page.evaluate(({id,entry})=>localStorage.setItem('ssh-gateway:favorites:'+id,JSON.stringify([entry])),{id:f.target.id,entry})
 await page.route('**/favorites', route => route.request().postDataJSON().op === 'import' ? route.fulfill({status:503,json:{error:'Temporary failure'}}) : route.continue())
 await page.goto('/?lang=en&window=1&machine='+f.target.id)
 await expect(page.locator('.file-message.error')).toContainText('Temporary failure')
 expect(await page.evaluate(id=>localStorage.getItem('ssh-gateway:favorites:'+id),f.target.id)).not.toBeNull()
 await page.unroute('**/favorites')
 await page.reload()
 await expect(page.locator('.favorites').getByRole('button',{name:entry.name,exact:true})).toHaveCount(1)
 await expect.poll(()=>page.evaluate(id=>localStorage.getItem('ssh-gateway:favorites:'+id),f.target.id)).toBeNull()
 await page.request.post(`/api/targets/${f.target.id}/favorites`,{data:{op:'remove',entries:[entry]}})
})
