import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { randomUUID } from 'node:crypto'

test('WebSSH shows and opens hidden files and directories by default', async ({ page }) => {
 const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 await page.request.post('/api/login', {data:{username:'ssh-admin',password:fixture.admin_password}})
 const endpoint = `/api/targets/${fixture.target.id}/files`
 const home = (await (await page.request.post(endpoint,{data:{op:'list'}})).json()).path
 const directory = home + '/hidden-files-' + randomUUID()
 const create = async (path: string, directory = false) => {
  const response = await page.request.post(endpoint,{data:{op:'create',path,directory}})
  expect(response.ok()).toBe(true)
 }
 await create(directory, true)
 try {
  await create(directory + '/.env')
  await create(directory + '/.config', true)
  await create(directory + '/.config/.settings')
  const listing = await (await page.request.post(endpoint,{data:{op:'list',path:directory}})).json()
  expect(listing.entries.map((entry: {name:string}) => entry.name)).toEqual(['.config','.env'])
  await page.goto('/?lang=en&window=1&machine=' + fixture.target.id)
  await expect(page.locator('.terminal-tab.active i.online')).toBeVisible()
  await page.getByRole('button',{name:'Go to home directory',exact:true}).click()
  await page.locator('.tree-row').filter({hasText:directory.split('/').pop()!}).click()
  const env = page.locator(`.tree-row[title="${directory}/.env"]`)
  const config = page.locator(`.tree-row[title="${directory}/.config"]`)
  await expect(env).toBeVisible()
  await expect(config).toBeVisible()
  await env.dblclick()
  await expect(page.getByRole('tab',{name:/\.env/})).toHaveAttribute('aria-selected','true')
  await config.click()
  await expect(page.locator(`.tree-row[title="${directory}/.config/.settings"]`)).toBeVisible()
  await page.getByRole('button',{name:'Refresh current directory',exact:true}).click()
  await expect(page.locator(`.tree-row[title="${directory}/.config/.settings"]`)).toBeVisible()
 } finally {
  await page.request.post(endpoint,{data:{op:'delete',path:directory}})
 }
})
