import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { desktopFixture } from './desktop-fixture'

test('Desktop WebSSH uses the native window bridge without opening a browser tab', async ({ page, context }) => {
  const fixture = JSON.parse(readFileSync('.test-fixture/connection.json', 'utf8'))
  await page.request.post('/api/login', { data: { username: 'ssh-admin', password: fixture.admin_password } })
  const opened: { target: string; selection: string }[] = []
  await desktopFixture(page, {
    Info: () => ({ ready: true, needs_setup: false, active: 0, settings: {}, version: 'test' }),
    CheckUpdate: () => ({ configured: false, available: false }),
    OpenMachineWindow: (target: string) => { opened.push({ target, selection: '' }) },
    OpenMachineConnectionWindow: (target: string, selection: string) => { opened.push({ target, selection }) },
    Call: async (method: string, path: string, body: string) => {
      const response = await page.request.fetch('/api' + path, { method, ...(method === 'GET' ? {} : { data: JSON.parse(body || '{}') }) })
      return { status: response.status(), data: await response.json() }
    },
  })
  await page.goto('/?lang=en')
  const row = page.getByRole('row').filter({ hasText: fixture.target.name })
  await row.getByRole('button', { name: 'Open terminal', exact: true }).click()
  await expect.poll(() => opened.length).toBe(1)
  expect(opened[0]?.target).toBe(fixture.target.id)
  expect(context.pages()).toHaveLength(1)
  await expect(page.getByRole('heading', { name: 'SSH connections', exact: true })).toBeVisible()
})
