// 先构建并启动静态主页，可通过 GATEWAY_SITE_URL 指向仓库子路径。
import { chromium, expect } from '../../web/node_modules/@playwright/test/index.mjs';
import { readFileSync } from 'node:fs';
const base = process.env.GATEWAY_SITE_URL || 'http://127.0.0.1:8090';
const browser = await chromium.launch({ executablePath: process.env.GATEWAY_CHROMIUM || undefined });
try {
  const page = await browser.newPage({ locale: 'zh-CN', viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(base + '/?lang=zh');
  const manifest = JSON.parse(readFileSync('site/screenshots/manifest.json', 'utf8'));
  const cards = page.locator('.screenshot-grid figure');
  await expect(cards).toHaveCount(manifest.screenshots.length);
  for (const name of manifest.screenshots) {
    const image = page.locator(`#demo-${name} img`);
    await image.scrollIntoViewIfNeeded();
    await expect(image).toBeVisible();
    await expect.poll(() => image.evaluate(node => node.complete && node.naturalWidth === 1920 && node.naturalHeight === 1080)).toBe(true);
    await expect(page.locator(`#demo-${name} a`)).toHaveAttribute('href', `screenshots/${name}.png`);
  }
  for (const language of ['en', 'zh']) {
    await page.locator(`[data-language="${language}"]`).click();
    await expect(page.locator('#demo-workspace img')).toHaveAttribute('alt', language === 'en' ? 'Terminal and files screenshot' : '终端与文件工作台演示截图');
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.locator('#screenshots').scrollIntoViewIfNeeded();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      for (const card of await cards.all()) {
        const box = await card.boundingBox();
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width);
      }
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.locator('.hero a[href="#screenshots"]').click();
  await expect(page).toHaveURL(/#screenshots$/);
  const popupPromise = page.waitForEvent('popup');
  await page.locator('#demo-workspace a').click();
  const popup = await popupPromise;
  await popup.waitForLoadState();
  expect(popup.url()).toContain('/screenshots/workspace.png');
  expect(await popup.evaluate(() => window.opener === null)).toBe(true);
  await popup.close();
  await page.screenshot({ path: '.build/demo-gallery-desktop.png' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('#screenshots').scrollIntoViewIfNeeded();
  await page.screenshot({ path: '.build/demo-gallery-mobile.png' });
  expect(errors).toEqual([]);
  console.log(`通过：${manifest.screenshots.length} 张原图加载、图文对应、双语替代文本、首页锚点、原图访问与三种宽度布局。`);
} finally { await browser.close(); }
