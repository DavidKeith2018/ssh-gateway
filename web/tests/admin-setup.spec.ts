import { test, expect } from '@playwright/test'
import { desktopFixture } from './desktop-fixture'

for (const language of ['en', 'zh-CN']) {
  test(`Administrator setup validates password length before submission (${language})`, async ({ page }) => {
    const submitted: string[] = []
    await desktopFixture(page, {
      Info: () => ({ ready: true, needs_setup: true, settings: {}, version: 'test' }),
      Call: (_method: string, path: string, body: string) => {
        if (path === '/security') return { status: 200, data: { enabled: false, locked: false } }
        if (path === '/desktop/setup') {
          submitted.push(JSON.parse(body).password)
          return { status: 200, data: { ok: true } }
        }
        return { status: 401, data: { error: 'Test session is not authenticated' } }
      },
    })
    await page.goto(`/?lang=${language}`)
    const password = page.locator('#admin-password')
    const submit = page.locator('.login-button')
    const alert = page.locator('.login-card [role="alert"]')
    await page.getByLabel(language==='en'?'I have saved my administrator password safely':'我已妥善保存管理员密码',{exact:true}).check()
    await expect(password).toHaveAttribute('autocomplete', 'new-password')
    await password.fill('a'.repeat(11))
    await submit.click()
    await expect(alert).toContainText(language === 'en' ? 'Administrator password is too short' : '管理员密码过短')
    await expect(alert).toContainText('12')
    await expect(password).toHaveValue('a'.repeat(11))
    await expect(submit).toBeEnabled()
    expect(submitted).toEqual([])

    await password.fill('a'.repeat(73))
    await password.press('Enter')
    await expect(alert).toContainText('72')
    expect(submitted).toEqual([])

    // Keep UTF-8 limits consistent with the backend, including non-ASCII passwords.
    for (const value of ['a'.repeat(12), 'a'.repeat(72), '测'.repeat(4)]) {
      await password.fill(value)
      await password.press('Enter')
      await expect.poll(() => submitted.length).toBeGreaterThan(0)
      await expect(submit).toBeEnabled()
      expect(submitted.pop()).toBe(value)
    }
  })
}
