import type { Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { resolve, extname } from 'node:path'

// 使用真正的桌面构建、生成绑定与 v3 运行时，仅替换原生 HTTP 调用端点。
export async function desktopFixture(page: Page, methods: Record<string, (...args: any[]) => unknown>) {
  const root = resolve('../desktop/frontend/dist')
  await page.route('**/*', async route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === '/wails/runtime') {
      const { args } = route.request().postDataJSON()
      const method = args?.methodName?.replace('main.App.', '')
      const result = methods[method]?.(...(args?.args ?? []))
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify((await result) ?? null) })
    }
    if (pathname === '/wails/custom.js') return route.fulfill({ contentType: 'text/javascript', body: '' })
    if (pathname === '/' || pathname.startsWith('/assets/')) {
      const file = resolve(root, '.' + (pathname === '/' ? '/index.html' : pathname))
      if (!file.startsWith(root + '/')) return route.abort()
      const types: Record<string, string> = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.ttf': 'font/ttf' }
      return route.fulfill({ contentType: types[extname(file)] ?? 'application/octet-stream', body: readFileSync(file) })
    }
    return route.continue()
  })
}

export async function desktopEvent(page: Page, name: string, data: unknown = null) {
  await page.evaluate(({ name, data }) => (window as any)._wails.dispatchWailsEvent({ name, data }), { name, data })
}
