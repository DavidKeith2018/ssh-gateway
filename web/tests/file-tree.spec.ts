import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

// Server-side favorites survive browser contexts; isolate each test's saved state.
test.beforeEach(async ({ request }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await request.post('/api/login', {data:{username:'ssh-admin',password:fixture.admin_password}})
  const endpoint = `/api/targets/${fixture.target.id}/favorites`
  const entries = await (await request.post(endpoint, {data:{op:'read'}})).json()
  expect((await request.post(endpoint, {data:{op:'remove',entries}})).ok()).toBe(true)
})

test('定位深层收藏目录限制并发、复用缓存并停止过期导航', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  const deep = '/' + Array.from({ length: 20 }, (_, i) => '层' + i).join('/')
  const other = '/另一棵树' + deep
  const requests: string[] = []
  let active = 0, peak = 0, rejected = 0
  await page.route(/\/api\/targets\/[^/]+\/files(?:\?|$)/, async route => {
    const body = route.request().postDataJSON()
    if (body.op !== 'list') return route.continue()
    requests.push(body.path)
    active++; peak = Math.max(peak, active)
    try {
      if (active > 16) {
        rejected++
        await route.fulfill({ status: 429, json: { error: '机器操作繁忙，请稍后重试' } })
        return
      }
      await new Promise(resolve => setTimeout(resolve, 40))
      const parent = body.path || '/'
      const children = [...new Set([deep, other].filter(p => p.startsWith(parent === '/' ? '/' : parent + '/'))
        .map(p => p.slice(parent === '/' ? 1 : parent.length + 1).split('/')[0]).filter(Boolean))]
      await route.fulfill({ json: { path: parent, entries: children.map(name => ({ name, path: (parent === '/' ? '' : parent) + '/' + name, kind: 'directory', size: 0, modified: '' })) } })
    } finally { active-- }
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.evaluate(({ id, deep, other }) => localStorage.setItem('ssh-gateway:favorites:' + id, JSON.stringify([
    { name: '深层收藏', path: deep, kind: 'directory' }, { name: '其他收藏', path: other, kind: 'directory' },
  ])), { id: fixture.target.id, deep, other })
  await page.goto('/?window=1&machine=' + fixture.target.id)
  const machine = page.locator('.machine-page')
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  await expect(machine.locator('.tree-row[title="/层0"]')).toBeVisible()
  await machine.locator('.favorites button[title="' + deep + '"]').click()
  // 树节点通过 role 标识，选中目录必须完整定位到叶子。
  const selectedRow = machine.locator('[role="treeitem"][aria-selected="true"] > .tree-row')
  await expect(selectedRow).toHaveAttribute('title', deep)
  expect(rejected).toBe(0)
  expect(peak).toBe(1)
  const before = requests.length
  await machine.locator('.favorites button[title="' + deep + '"]').click()
  await expect.poll(() => requests.length).toBe(before + 1)
  await expect.poll(() => active).toBe(0)
  expect(requests.slice(before)).toEqual([deep])
  await machine.locator('.favorites button[title="' + other + '"]').click()
  await expect.poll(() => requests.includes('/另一棵树/层0')).toBe(true)
  await machine.getByRole('button', { name: '定位系统根目录', exact: true }).click()
  await expect(selectedRow).toHaveAttribute('title', '/')
  await expect.poll(() => active).toBe(0)
  expect(requests).not.toContain('/另一棵树/层0/层1/层2/层3/层4')
  await page.request.post(`/api/targets/${fixture.target.id}/favorites`, {data:{op:'remove',entries:[{path:deep,kind:'directory'},{path:other,kind:'directory'}]}})
})

test('收藏手风琴、目录即时展开与缓存、目录终端操作及行对齐', async ({ page }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  const directory = "/space ' $dir"
  const entry = { name: "space ' $dir", path: directory, kind: 'directory', size: 0, modified: '' }
  let directoryRequests = 0
  let release!: () => void
  const delayed = new Promise<void>(resolve => { release = resolve })
  const inputs: string[][] = []
  page.on('websocket', socket => {
    if (!socket.url().includes('/terminal')) return
    const sent: string[] = []
    inputs.push(sent)
    socket.on('framesent', event => {
      try { const message = JSON.parse(String(event.payload)); if (message.type === 'input') sent.push(message.data) } catch {}
    })
  })
  await page.route(/\/api\/targets\/[^/]+\/files(?:\?|$)/, async route => {
    const body = route.request().postDataJSON()
    if (body.op !== 'list') return route.continue()
    if (body.path === directory) {
      directoryRequests++
      if (directoryRequests === 1) await delayed
      await route.fulfill({ json: { path: directory, entries: [{ name: 'child.txt', path: directory + '/child.txt', kind: 'file', size: 0, modified: '' }, ...(directoryRequests > 1 ? [{ name: 'new.txt', path: directory + '/new.txt', kind: 'file', size: 0, modified: '' }] : [])] } })
    } else await route.fulfill({ json: { path: '/', entries: [entry] } })
  })
  await page.goto('/')
  await page.getByLabel('管理员密码').fill(fixture.admin_password)
  await page.getByRole('button', { name: '进入中转台' }).click()
  await expect(page.getByRole('heading', { name: 'SSH 连接', exact: true })).toBeVisible()
  await page.goto('/?window=1&machine=' + encodeURIComponent(fixture.target.id))
  const machine = page.getByRole('main', { name: '终端机器页面' })
  await expect(machine.locator('.terminal-tab.active i.online')).toBeVisible()
  const tree = machine.getByRole('tree', { name: '远程文件树' })
  const row = tree.locator('.tree-row').filter({ has: page.getByText(entry.name, { exact: true }) })
  await row.click()
  await expect(row.locator('..')).toHaveAttribute('aria-expanded', 'true')
  await expect(row.locator('..')).toHaveAttribute('aria-busy', 'true')
  await expect(tree.getByText('正在加载…', { exact: true })).toBeVisible()
  const loadingTextX=await tree.getByText('正在加载…',{exact:true}).evaluate(node=>{const range=document.createRange();range.selectNodeContents(node);return range.getBoundingClientRect().x})
  await row.getByRole('button', { name: '收起 ' + entry.name, exact: true }).click()
  await row.getByRole('button', { name: '展开 ' + entry.name, exact: true }).click()
  expect(directoryRequests).toBe(1)
  release()
  await expect(tree.getByText('child.txt', { exact: true })).toBeVisible()
  expect(Math.abs((await tree.getByText('child.txt',{exact:true}).boundingBox())!.x-loadingTextX)).toBeLessThanOrEqual(1)
  await row.getByRole('button', { name: '收起 ' + entry.name, exact: true }).click()
  await row.getByRole('button', { name: '展开 ' + entry.name, exact: true }).click()
  await expect(tree.getByText('child.txt', { exact: true })).toBeVisible()
  expect(directoryRequests).toBe(1)
  await row.click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '刷新目录', exact: true }).click()
  await expect.poll(() => directoryRequests).toBe(2)
  await expect(tree.getByText('new.txt', { exact: true })).toBeVisible()
  await expect(machine.getByRole('menu')).toHaveCount(0)
  const child = tree.locator('.tree-row').filter({ has: page.getByText('child.txt', { exact: true }) })
  await machine.getByRole('button', { name: '定位家目录', exact: true }).hover()
  const background = await child.evaluate(element => getComputedStyle(element).backgroundColor)
  await child.hover()
  expect(await child.evaluate(element => getComputedStyle(element).backgroundColor)).not.toBe(background)
  await child.click({ button: 'right' })
  expect((await machine.getByRole('menuitem').allTextContents()).slice(0, 3)).toEqual(['刷新目录', '打开新终端', '终端进入目录'])
  await expect(machine.getByRole('menu').getByRole('separator')).toBeVisible()
  await machine.getByRole('menuitem', { name: '刷新目录', exact: true }).click()
  await expect.poll(() => directoryRequests).toBe(3)
  await child.click()
  const selectedBackground = await child.evaluate(element => getComputedStyle(element).backgroundColor)
  await row.hover()
  expect(await child.evaluate(element => getComputedStyle(element).backgroundColor)).toBe(selectedBackground)
  expect(await child.evaluate(element => getComputedStyle(element).boxShadow)).not.toBe('none')

  await row.click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '加入收藏', exact: true }).click()
  const favorites = machine.locator('.favorites')
  await expect(favorites).toContainText(entry.name)
  const heading = machine.getByRole('button', { name: '文件 / 文件夹收藏', exact: true })
  const beforeHeight = await machine.locator('.tree-scroll').evaluate(element => element.clientHeight)
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await expect(favorites).toBeHidden()
  expect(await machine.locator('.tree-scroll').evaluate(element => element.clientHeight)).toBeGreaterThan(beforeHeight)
  await heading.click()
  await expect(favorites).toBeVisible()
  for (const item of [row, favorites.locator('.favorite-row')]) {
    const icon = (await item.locator('.entry-icon').boundingBox())!
    const text = (await item.locator('.truncate').boundingBox())!
    expect(Math.abs(icon.y + icon.height / 2 - text.y - text.height / 2)).toBeLessThanOrEqual(1)
  }

  const cd = "\u0015cd -- '/space '\\'' $dir'\r"
  await child.click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '终端进入目录', exact: true }).click()
  await expect.poll(() => inputs[0]).toContain(cd)
  await expect(machine.locator('.xterm-helper-textarea:visible')).toBeFocused()
  await child.click({ button: 'right' })
  await machine.getByRole('menuitem', { name: '打开新终端', exact: true }).click()
  await expect(machine.getByRole('tab', { name: '终端 2', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(() => inputs[1]).toContain(cd)
  expect(inputs[0].filter(value => value === cd)).toHaveLength(1)
  await machine.getByRole('button', { name: '关闭终端 2', exact: true }).click()
  await machine.getByRole('button', { name: '关闭终端 1', exact: true }).click()
  await expect(tree).toHaveCount(0)
  await machine.locator('.tree-scroll').click({ button: 'right' })
  await expect(machine.getByRole('menuitem', { name: '终端进入目录', exact: true })).toBeDisabled()
  await expect(machine.getByRole('menuitem', { name: '打开新终端', exact: true })).toBeEnabled()
  await page.keyboard.press('Escape')
  await page.screenshot({ path: 'test-results/file-tree-alignment.png' })
})

for (const language of ['en','zh-CN']) {
 test(`Directory pagination follows child indentation (${language})`, async ({page})=>{
  const f=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
  await page.route(/\/api\/targets\/[^/]+\/files(?:\?|$)/,async route=>{
   const body=route.request().postDataJSON()
   if(body.op!=='list') return route.continue()
   const path=body.path||'/'
   const entries=path==='/' ? [{name:'nested',path:'/nested',kind:'directory',size:0,modified:''}] : Array.from({length:103},(_,i)=>({name:`file-${String(i).padStart(3,'0')}.txt`,path:`/nested/file-${String(i).padStart(3,'0')}.txt`,kind:'file',size:0,modified:''}))
   await route.fulfill({json:{path,entries}})
  })
  await page.request.post('/api/login',{data:{username:'ssh-admin',password:f.admin_password}})
  await page.goto('/?window=1&machine='+f.target.id+'&lang='+language)
  const directory=page.locator('.tree-row[title="/nested"]')
  await directory.click()
  const group=directory.locator('..').locator(':scope > ul')
  const more=group.locator(':scope > .tree-more-row button')
  await expect(more).toBeVisible()
  const last=group.locator(':scope > [role=treeitem] > .tree-row > .truncate').last()
  expect(Math.abs((await more.boundingBox())!.x-(await last.boundingBox())!.x)).toBeLessThanOrEqual(1)
  await more.scrollIntoViewIfNeeded()
  await page.screenshot({path:`test-results/tree-more-indent-${language}.png`})
  await more.focus();await page.keyboard.press('Enter')
  await expect(group.locator(':scope > [role=treeitem]')).toHaveCount(103)
  await expect(more).toHaveCount(0)
 })
}

test('Empty directory text follows child indentation',async({page})=>{
 const f=JSON.parse(readFileSync('.test-fixture/connection.json','utf8'))
 await page.route(/\/api\/targets\/[^/]+\/files(?:\?|$)/,async route=>{
  const body=route.request().postDataJSON()
  if(body.op!=='list') return route.continue()
  const path=body.path||'/'
  await route.fulfill({json:{path,entries:path==='/'?[{name:'empty',path:'/empty',kind:'directory',size:0,modified:''}]:[]}})
 })
 await page.request.post('/api/login',{data:{username:'ssh-admin',password:f.admin_password}})
 await page.goto('/?window=1&machine='+f.target.id)
 const row=page.locator('.tree-row[title="/empty"]')
 await row.click()
 const text=row.locator('..').locator(':scope > ul > .tree-empty')
 await expect(text).toBeVisible()
 const x=await text.evaluate(node=>{const range=document.createRange();range.selectNodeContents(node);return range.getBoundingClientRect().x})
 expect(Math.abs(x-(await row.locator('.truncate').boundingBox())!.x-17)).toBeLessThanOrEqual(1)
})
