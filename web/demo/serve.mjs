import { spawn } from 'node:child_process';
import { readFileSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { request } from '@playwright/test';
import { seedDemo, demoPassword } from './seed.mjs';

const baseURL = 'http://127.0.0.1:19876';
const fixturePath = resolve('.test-fixture/demo-serve.json');
// 固定只监听回环地址；拒绝复用任何已经运行的实例。
try { await fetch(baseURL, { signal: AbortSignal.timeout(1000) }); throw new Error('19876 端口已被占用，请先停止该服务。'); }
catch (error) { if (!error.cause && error.name !== 'TimeoutError') throw error; }
rmSync(fixturePath, { force: true });
const child = spawn('go', ['test', '..', '-run', '^TestBrowserFixture$', '-count=1', '-timeout=0'], {
  env: { ...process.env, GATEWAY_DEMO_FIXTURE: '1', GATEWAY_BROWSER_FIXTURE: fixturePath, GATEWAY_BROWSER_PORT: '19876' }, stdio: 'inherit', detached: true,
});
let stopped = false;
function stop() {
  if (stopped) return;
  stopped = true;
  try { process.kill(-child.pid, 'SIGTERM'); } catch {}
  rmSync(fixturePath, { force: true });
}
process.on('SIGINT', () => { stop(); process.exit(0); });
process.on('SIGTERM', () => { stop(); process.exit(0); });
process.on('exit', stop);
child.on('exit', code => { if (!stopped) { stop(); process.exit(code || 1); } });
try {
  let ready = false;
  for (let attempt = 0; attempt < 120; attempt++) {
    try { const response = await fetch(baseURL, { signal: AbortSignal.timeout(1000) }); if (response.ok) { ready = true; break; } } catch {}
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  if (!ready) throw new Error('演示服务启动超时');
  const fixture = JSON.parse(readFileSync(fixturePath, 'utf8'));
  const api = await request.newContext({ baseURL });
  try { const demo = await seedDemo(api, fixture); console.log('演示数据已就绪：', demo.counts); } finally { await api.dispose(); }
  console.log(`\n演示地址：${baseURL}\n管理员：ssh-admin / ${fixture.admin_password}\n普通用户：demo-reader / ${demoPassword}\n仅供本机演示，每次启动创建全新数据。按 Ctrl+C 退出。`);
} catch (error) { console.error(error); stop(); process.exit(1); }
