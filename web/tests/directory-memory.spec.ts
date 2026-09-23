import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

test('Directory restoration, refresh and deleted ancestor fallback', async ({ page }) => {
 const f = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
 const directories = new Map<string, string[]>([['/', ['saved']], ['/saved', ['child']], ['/saved/child', []]])
 const requests: string[] = []
 await page.route(/\/api\/targets\/[^/]+\/files(?:\?|$)/, async route => {
  const body = route.request().postDataJSON()
  if (body.op !== 'list') return route.continue()
  const path = body.path || '/'
  requests.push(path)
  if (!directories.has(path)) return route.fulfill({ status: 404, json: { error: 'No such file or directory' } })
  return route.fulfill({ json: { path, entries: directories.get(path)!.map(name => ({ name, path: (path === '/' ? '' : path) + '/' + name, kind: 'directory', size: 0, modified: '' })) } })
 })
 await page.request.post('/api/login', { data: { username: 'ssh-admin', password: f.admin_password } })
 await page.goto('/?lang=en&window=1&machine=' + f.target.id)
 const selected = page.locator('.tree-row.selected')
 await page.locator('.tree-row[title="/saved"]').click()
 await page.locator('.tree-row[title="/saved/child"]').click()
 await expect(selected).toHaveAttribute('title', '/saved/child')
 // Cookie storage also restores the path when the desktop loopback port changes.
 await page.evaluate(() => localStorage.clear())
 await page.reload()
 await expect(selected).toHaveAttribute('title', '/saved/child')
 directories.set('/saved/child', ['new-folder'])
 directories.set('/saved/child/new-folder', [])
 const refresh = page.getByRole('button', { name: 'Refresh current directory', exact: true })
 await refresh.click()
 await expect(page.locator('.tree-row[title="/saved/child/new-folder"]')).toBeVisible()
 // Collapsing another directory must not redirect refresh away from the last opened directory.
 await page.locator('.tree-row[title="/saved"]').click()
 const beforeRefresh = requests.length
 await refresh.click()
 expect(requests[beforeRefresh]).toBe('/saved/child')
 await expect(selected).toHaveAttribute('title', '/saved/child')
 directories.delete('/saved/child')
 directories.set('/saved', [])
 await refresh.click()
 await expect(selected).toHaveAttribute('title', '/saved')
 await expect(page.locator('.tree-row[title="/saved/child"]')).toHaveCount(0)
 // Missing saved paths also fall back when opening WebSSH again.
 directories.delete('/saved')
 directories.set('/', [])
 await page.reload()
 await expect(selected).toHaveAttribute('title', '/')
 await expect.poll(() => page.evaluate(id => localStorage.getItem('ssh-gateway:last-directory:' + id), f.target.id)).toBe('/')
 expect(requests).toContain('/saved/child')
 await page.screenshot({ path: 'test-results/directory-refresh-fallback.png' })
})
