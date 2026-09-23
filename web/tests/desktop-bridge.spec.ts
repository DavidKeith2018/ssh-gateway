import { test, expect } from '@playwright/test'
import { readdirSync } from 'node:fs'
import { desktopFixture, desktopEvent } from './desktop-fixture'

test('v3 退出事件支持取消、重新确认和无连接退出', async ({ page }) => {
  let active = 2, confirmed = 0, cancelled = 0, ready = 0
  await desktopFixture(page, {
    Info: () => ({ ready: true, needs_setup: false, active, settings: {}, data_dir: '/测试数据', error: '' }),
    Call: (_method, path) => path === '/security' ? { status: 200, data: { enabled: false, locked: false } } : { status: 401, data: { error: '请先登录' } },
    FrontendReady: () => { ready++ },
    ConfirmQuit: () => { confirmed++ },
    CancelQuit: () => { cancelled++ },
  })
  await page.goto('/')
  await expect.poll(() => ready).toBe(1)
  await desktopEvent(page, 'desktop:confirm-quit')
  const dialog = page.locator('dialog').filter({ has: page.getByRole('heading', { name: '退出 SSH Gateway' }) })
  await expect(dialog).toBeVisible()
  await desktopEvent(page, 'desktop:confirm-quit')
  await expect(page.locator('dialog[open]')).toHaveCount(1)
  await dialog.getByRole('button', { name: '取消' }).click()
  await expect.poll(() => cancelled).toBeGreaterThan(0)
  expect(confirmed).toBe(0)
  await desktopEvent(page, 'desktop:confirm-quit')
  await dialog.getByRole('button', { name: '退出应用', exact: true }).click()
  await expect.poll(() => confirmed).toBe(1)
  active = 0
  await desktopEvent(page, 'desktop:confirm-quit')
  await expect.poll(() => confirmed).toBe(2)
  await page.unrouteAll({ behavior: 'wait' })
})

test('v3 终端与传输事件提取数据并正确取消订阅', async ({ page }) => {
  await desktopFixture(page, { Info: () => ({}), Call: () => ({ status: 401, data: {} }) })
  await page.goto('/')
  const chunk = readdirSync('../desktop/frontend/dist/assets').find(name => name.startsWith('desktop-v3-') && name.endsWith('.js'))!
  const received = await page.evaluate(async chunk => {
    const bridge = await import(`/assets/${chunk}`)
    const events: unknown[] = []
    const off = bridge.onDesktopEvent('terminal', (event: unknown) => events.push(event))
    const offProgress = bridge.onDesktopEvent('file-progress', (event: unknown) => events.push(event))
    const emit = (name: string, data: unknown) => (window as any)._wails.dispatchWailsEvent({ name, data })
    emit('terminal', { id: 't-1', type: 'output', data: '5Lit5paH' })
    emit('file-progress', { id: 'f-1', data: '50' })
    off(); off(); offProgress()
    emit('terminal', { id: 't-1', type: 'closed' })
    emit('file-progress', { id: 'f-1', data: '100' })
    return events
  }, chunk)
  expect(received).toEqual([{ id: 't-1', type: 'output', data: '5Lit5paH' }, { id: 'f-1', data: '50' }])
  await page.unrouteAll({ behavior: 'wait' })
})
