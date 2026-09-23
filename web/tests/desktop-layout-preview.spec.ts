import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

const fixture = () => JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
test('Desktop SSH list fits smaller windows and directory width persists', async ({ page }) => {
 const f = fixture()
 await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
 await page.goto('/?lang=en')
 await expect(page.getByRole('heading', { name: 'SSH connections', exact: true })).toBeVisible()
 for (const width of [1440, 1024, 900, 800]) {
  await page.setViewportSize({width,height:800})
  expect(await page.locator('.connection-list .table-scroll:visible').evaluate(el => el.scrollWidth <= el.clientWidth+1)).toBe(true)
  if (width === 1024 || width === 800) await page.screenshot({path:`test-results/compact-list-${width}.png`})
 }
 await page.setViewportSize({width:1440,height:1000})
 const url = '/?lang=en&window=1&machine='+f.target.id
 await page.goto(url)
 const handle = page.getByRole('separator',{name:'Resize directory width'})
 await expect(handle).toBeVisible()
 const box = (await handle.boundingBox())!
 await page.mouse.move(box.x+3,box.y+80); await page.mouse.down(); await page.mouse.move(box.x+103,box.y+80); await page.mouse.up()
 await expect(handle).toHaveAttribute('aria-valuenow','360')
 await expect(page.locator('.files-left')).toHaveCSS('width','360px')
 await handle.focus(); await page.keyboard.press('ArrowRight')
 await expect(handle).toHaveAttribute('aria-valuenow','380')
 page.on('dialog', dialog => dialog.accept())
 await page.reload()
 await expect(page.locator('.files-left')).toHaveCSS('width','380px')
})

test('WebSSH previews images, PDF, audio and video without losing the text editor', async ({ page }) => {
 const f=fixture()
 await page.request.post('/api/login',{data:{username:'ssh-admin',password:f.admin_password}})
 const root = (await (await page.request.post(`/api/targets/${f.target.id}/files`,{data:{op:'list'}})).json()).path
 const previews = [ ['preview.png', '../packaging/icon.png', 'img'], ['preview.pdf', 'tests/fixtures/preview/document.pdf', 'iframe'], ['preview.wav', 'tests/fixtures/preview/sound.wav', 'audio'], ['preview.mp4', 'tests/fixtures/preview/clip.mp4', 'video'] ]
 for (const [name,source] of previews) {
  const response=await page.request.put(`/api/targets/${f.target.id}/transfer?overwrite=true&path=${encodeURIComponent(root+'/'+name)}`,{data:readFileSync(source!)})
  expect(response.ok(),await response.text()).toBe(true)
 }
 await page.goto('/?lang=en&window=1&machine='+f.target.id)
 await expect(page.locator('.terminal-tab.active i.online')).toBeVisible()
 await page.getByRole('button',{name:'Go to home directory',exact:true}).click()
 for(const [name,,selector] of previews) {
  await page.locator(`.tree-row[title="${root}/${name}"]`).dblclick()
  const preview=page.getByRole('region',{name:'File preview'}).filter({ visible: true })
  await expect(preview.locator(selector!)).toHaveAttribute('src',/^blob:/)
  if (selector === 'img') await expect.poll(() => preview.locator('img').evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0)
  if (selector === 'img') {
   const image = preview.locator('img')
   await preview.getByRole('button', { name: 'Actual size', exact: true }).click()
   const original = (await image.boundingBox())!.width
   await preview.locator('.preview-content').hover()
   await page.mouse.wheel(0, -100)
   await expect.poll(async () => (await image.boundingBox())!.width).toBeGreaterThan(original)
   await page.mouse.wheel(0, 100)
   await expect.poll(async () => Math.abs((await image.boundingBox())!.width - original)).toBeLessThan(1)
   for (let i = 0; i < 8; i++) await preview.getByRole('button', { name: 'Zoom in', exact: true }).click()
   expect(await preview.locator('.preview-content').evaluate(el => el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight)).toBe(true)
   await preview.getByRole('button', { name: 'Fit', exact: true }).click()
   await expect.poll(() => preview.locator('.preview-content').evaluate(el => el.scrollWidth <= el.clientWidth + 1 && el.scrollHeight <= el.clientHeight + 1)).toBe(true)
  }
  if (selector === 'audio' || selector === 'video') await expect.poll(() => preview.locator(selector).evaluate((el: HTMLMediaElement) => el.readyState)).toBeGreaterThan(0)
  await expect(preview.getByRole('button',{name:'Download file'})).toBeVisible()
  if (selector === 'img' || selector === 'iframe') await page.screenshot({path:`test-results/preview-${selector}.png`})
  await expect(page.getByRole('tab', { name: name!, exact: true })).toHaveAttribute('aria-selected', 'true')
 }
 const tabs = page.locator('.file-tabs')
 await expect(tabs.getByRole('tab')).toHaveCount(5)
 await tabs.getByRole('tab', {name:'preview.png',exact:true}).click()
 await expect(page.locator('.file-preview:visible img')).toBeVisible()
 await page.locator(`.tree-row[title="${root}/preview.png"]`).dblclick()
 await expect(tabs.getByRole('tab')).toHaveCount(5)
 await tabs.getByRole('tab').first().click()
 await expect(page.locator('.monaco-host')).toBeVisible()
 await tabs.getByRole('tab', {name:'preview.pdf',exact:true}).click()
 await expect(page.locator('.file-preview:visible iframe')).toBeVisible()
 await tabs.locator('.file-tab.active').getByRole('button', {name:'Close preview',exact:true}).click()
 await expect(tabs.getByRole('tab', {name:'preview.pdf',exact:true})).toHaveCount(0)
 await expect(page.locator('.file-preview:visible audio')).toBeVisible()
 for (let remaining = 3; remaining > 0; remaining--) {
  await tabs.getByRole('button', {name:'Close preview',exact:true}).first().click()
 }
 await expect(page.locator('.monaco-host')).toBeVisible()
})
