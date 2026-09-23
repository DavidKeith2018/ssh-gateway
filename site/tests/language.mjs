// 在根目录启动主页预览后执行：node site/tests/language.mjs
import { chromium, expect } from '../../web/node_modules/@playwright/test/index.mjs';
import { existsSync, readFileSync } from 'node:fs';
const browser = await chromium.launch({
  executablePath: process.env.GATEWAY_CHROMIUM || undefined,
  headless: true
});
const base = process.env.GATEWAY_SITE_URL || 'http://127.0.0.1:8090';
try {
  const context = await browser.newContext({ locale: 'en-US' });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(base);
  // 官网页面的仓库文档链接必须指向仍存在的文件和显式锚点。
  for (const path of await page.locator('[data-repo-path^="blob/HEAD/"]').evaluateAll(nodes => nodes.map(node => node.dataset.repoPath))) {
    const [file, anchor] = path.slice('blob/HEAD/'.length).split('#');
    const target = new URL('../../' + file, import.meta.url);
    expect(existsSync(target), `文档不存在：${file}`).toBe(true);
    if (anchor === 'demo') expect(readFileSync(target, 'utf8')).toContain('id="demo"');
  }
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page).toHaveTitle(/SSH Gateway/);
  const untranslated = await page.evaluate(() => {
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const found = [];
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (!node.parentElement.closest('[data-language]') && /[\u4e00-\u9fff]/.test(node.textContent)) found.push(node.textContent);
    }
    return found;
  });
  expect(untranslated).toEqual([]);
  expect(await page.evaluate(() => Object.keys(window.SSH_GATEWAY_TRANSLATIONS.en.content).filter(selector => !document.querySelector(selector)))).toEqual([]);
  for (const language of ['en', 'zh']) {
    await page.locator(`[data-language="${language}"]`).click();
    await expect(page.locator(`[data-language="${language}"]`)).toHaveAttribute('aria-pressed', 'true');
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
    await page.locator('#copy').click();
    await expect(page.locator('#copy-status')).toContainText(language === 'en' ? /copied|Copy unavailable/ : /已复制|无法自动复制/);
  }
  await page.goto(base);
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await page.goto(base + '/?lang=en#platforms');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await page.locator('[data-language="zh"]').click();
  expect(new URL(page.url()).hash).toBe('#platforms');
  expect(new URL(page.url()).searchParams.get('lang')).toBe('zh');
  await page.goto(base + '/?lang=en');
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({ path: '.build/homepage-en-desktop.png', fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: '.build/homepage-en-mobile.png', fullPage: true });
  const privateContext = await browser.newContext({ locale: 'zh-CN' });
  await privateContext.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new Error('存储已禁用'); } });
  });
  const privatePage = await privateContext.newPage();
  privatePage.on('pageerror', error => errors.push(error.message));
  await privatePage.goto(base);
  await expect(privatePage.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await privatePage.locator('[data-language="en"]').click();
  await expect(privatePage.locator('html')).toHaveAttribute('lang', 'en');
  expect(errors).toEqual([]);
  console.log('通过：中英文完整性、浏览器语言、记忆设置、分享参数、存储禁用、复制提示与多尺寸布局');
} finally {
  await browser.close();
}
