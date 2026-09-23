import { chromium, expect } from '../../web/node_modules/@playwright/test/index.mjs';

const base = process.env.GATEWAY_SITE_URL || 'http://127.0.0.1:8090';
const browser = await chromium.launch({ executablePath: process.env.GATEWAY_CHROMIUM || undefined });
try {
  const page = await browser.newPage({ locale: 'zh-CN' });
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async text => { window.copiedCommand = text; } } });
  });
  await page.goto(base + '/?lang=zh');
  const repository = await page.evaluate(() => window.SSH_GATEWAY_REPOSITORY);
  expect(repository).toMatch(/^https:\/\/github\.com\/[^/]+\/[^/]+$/);
  const assets = [
    'ssh-gateway-server-linux-amd64.tar.gz', 'ssh-gateway-server-linux-arm64.tar.gz',
    'ssh-gateway-desktop-windows-x64-setup.exe', 'ssh-gateway-desktop-windows-x64-portable.zip', 'ssh-gateway-desktop-linux-amd64.deb',
    'ssh-gateway-desktop-macos-arm64.dmg', 'ssh-gateway-desktop-macos-amd64.dmg',
  ];
  await page.locator('.hero .primary').click();
  await expect(page).toHaveURL(/#downloads$/);
  for (const language of ['en', 'zh']) {
    await page.locator(`[data-language="${language}"]`).click();
    for (const asset of assets) {
      const link = page.locator(`#downloads a[data-repo-path$="/${asset}"]`);
      await expect(link).toBeVisible();
      await expect(link).toHaveAttribute('href', `${repository}/releases/latest/download/${asset}`);
    }
    await expect(page.locator('#downloads a[data-repo-path$="-portable.zip"]')).toContainText(language === 'en' ? 'portable' : '免安装');
    await expect(page.locator('#commands')).toHaveText('./ssh-gateway-server serve');
    await expect(page.locator('#start')).not.toContainText('make build');
    await page.locator('#copy').click();
    expect(await page.evaluate(() => window.copiedCommand)).toBe('./ssh-gateway-server serve');
    await page.locator('#start .text-link').click();
    await expect(page).toHaveURL(/#downloads$/);
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    }
  }
  // 模拟附件响应以验证点击能启动下载；真实发布包另在线上校验。
  for (const asset of [assets[0], 'ssh-gateway-desktop-windows-x64-portable.zip']) {
  const downloadURL = `${repository}/releases/latest/download/${asset}`;
  await page.route(downloadURL, route => route.fulfill({
    status: 200, contentType: 'application/octet-stream',
    headers: { 'Content-Disposition': `attachment; filename="${asset}"` }, body: 'test package',
  }));
  const downloadPromise = page.waitForEvent('download');
  await page.locator(`#downloads a[data-repo-path$="/${asset}"]`).click();
  expect((await downloadPromise).suggestedFilename()).toBe(asset);
  }
  console.log('通过：七种安装及免安装包入口、双语下载指引、解压后启动命令与复制、下载交互及响应式布局。');
} finally { await browser.close(); }
