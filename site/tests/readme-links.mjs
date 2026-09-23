// 验证 README 官网入口的语言与栏目；GATEWAY_SITE_URL 可指定本地子路径或线上官网。
import { chromium, expect } from '../../web/node_modules/@playwright/test/index.mjs';
import { readFileSync } from 'node:fs';

const readme = readFileSync(new URL('../../README.md', import.meta.url), 'utf8');
const links = [...readme.matchAll(/\]\(([^)]+)\)/g)].map(match => match[1]);
expect(links.filter(link => /\.md(?:#|$)/.test(link))).toEqual([]);
const website = links.filter(link => link.startsWith('https://'));
expect(website.length).toBeGreaterThan(0);
const base = (process.env.GATEWAY_SITE_URL || 'http://127.0.0.1:8090').replace(/\/$/, '');
const browser = await chromium.launch({ executablePath: process.env.GATEWAY_CHROMIUM || undefined });
try {
  const page = await browser.newPage({ locale: 'en-US' });
  for (const link of website) {
    const url = new URL(link);
    expect(url.hostname).toBe('davidkeith2018.github.io');
    expect(url.pathname).toBe('/ssh-gateway/');
    const response = await page.goto(base + '/' + url.search + url.hash);
    if (response) expect(response.ok()).toBe(true);
    const language = url.searchParams.get('lang');
    if (language) await expect(page.locator('html')).toHaveAttribute('lang', language === 'zh' ? 'zh-CN' : 'en');
    if (url.hash) {
      await expect(page.locator(url.hash)).toBeVisible();
      expect(new URL(page.url()).hash).toBe(url.hash);
    }
  }
  console.log(`通过：README 的 ${website.length} 个官网入口可访问，语言与栏目锚点正确，无 Markdown 文档链接。`);
} finally {
  await browser.close();
}
