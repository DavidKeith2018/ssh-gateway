// 仅接受由本机测试夹具生成的地址和凭证；不操作用户的正式数据库。
export const demoPassword = 'demo-reader-password';
export async function seedDemo(api, fixture) {
  async function call(path, method = 'GET', data) {
    const response = await api.fetch(`/api${path}`, { method, data });
    if (!response.ok()) throw new Error(`演示数据 ${method} ${path}：${response.status()} ${await response.text()}`);
    return response.json();
  }
  await call('/login', 'POST', { username: 'ssh-admin', password: fixture.admin_password });
  const primary = { ...fixture.target, name: '生产 · 应用主节点', tags: ['生产', '华东', '应用'], allowed_sources: ['127.0.0.1', '192.0.2.0/24'] };
  primary.revision = (await call(`/targets/${primary.id}`)).revision;
  await call(`/targets/${primary.id}`, 'PUT', primary);
  const targets = [primary];
  const roles = ['应用', '数据库', '缓存', '监控', '构建', '网关'];
  for (let i = 1; i <= 33; i++) {
    const environment = ['生产', '预发布', '开发'][Math.floor((i - 1) / 6) % 3];
    const target = { ...fixture.target, id: `demo-server-${i}`, name: `${environment} · ${roles[i % 6]}节点 ${String(i).padStart(2, '0')}`, tags: [environment, i % 2 ? '华北' : '华东', roles[i % 6]], host: `192.0.2.${i + 10}`, port: 22, relay_user: `demo-relay-${i}`, relay_password: 'demo-relay-password', enabled: i % 8 !== 0, allowed_sources: ['192.0.2.0/24', '198.51.100.16'] };
    await call('/targets', 'POST', target); targets.push(target);
  }
  const multi = { ...fixture.multi_target, id: 'demo-multi', name: '预发布 · 多账号堡垒机', tags: ['预发布', '安全'], relay_user: '' };
  await call('/targets', 'POST', multi); targets.push(multi);
  const mapping = { ...fixture.mapping_target, name: '开发 · 服务转发节点', tags: ['开发', '转发'] };
  await call('/targets', 'POST', mapping); targets.push(mapping);
  for (const [i, username] of ['demo-reader', 'ops-east', 'ops-north', 'developer', 'release-bot', 'auditor', 'contractor', 'paused-user'].entries()) {
    await call('/users', 'POST', { username, password: demoPassword, enabled: i !== 7, target_ids: i % 2 === 0 ? [primary.id] : [], tags: [['生产'], ['华东'], ['华北'], ['开发'], ['预发布'], ['监控'], ['转发'], []][i] });
  }
  for (let i = 0; i < 8; i++) await call('/global-ips', 'PUT', { ip: `198.51.100.${20 + i}`, expires_at: new Date(Date.now() + (i + 1) * 86400000).toISOString() });
  const shortcuts = [['查看系统负载', 'uptime'], ['磁盘使用情况', 'df -h'], ['内存使用情况', 'free -h'], ['最近服务日志', 'journalctl -u app -n 50'], ['容器状态', 'docker ps'], ['监听端口', 'ss -lnt'], ['当前登录用户', 'who'], ['系统版本', 'cat /etc/os-release'], ['服务健康检查', 'curl -I http://127.0.0.1:8080'], ['进程概览', 'ps aux'], ['应用目录', 'ls -lah /srv/app'], ['网络地址', 'ip addr']];
  for (const [i, [name, command]] of shortcuts.entries()) await call('/shortcuts', 'POST', { name, command, targetId: i < 6 ? '*' : i < 9 ? primary.id : '', tags: i >= 9 ? ['生产'] : [] });
  for (let i = 0; i < 12; i++) await call('/mappings', 'POST', { target_id: mapping.id, name: `${['应用预览', '数据库隧道', '监控面板', '回调调试'][i % 4]} ${i + 1}`, direction: i % 2 ? 'reverse' : 'local', service_host: '127.0.0.1', service_port: [8080, 5432, 9090, 3000][i % 4], listen_port: 24000 + i, scope: i % 2 && i % 3 === 0 ? 'shared' : 'loopback', auto_start: false });
  for (const target of targets) {
    const path = `/targets/${target.id}/notes`;
    const note = await call(path, 'POST', { op: 'read' });
    await call(path, 'POST', { op: 'write', version: note.version, content: `# ${target.name}\n\n## 用途\n用于演示 SSH Gateway的服务器管理与协作流程。\n\n## 运维记录\n- 所属团队：平台工程\n- 环境标签：${target.tags.join(' / ')}\n- 应用目录：/srv/app\n- 日志目录：/var/log/app\n\n## 交接清单\n1. 核对主机指纹和来源范围\n2. 使用独立中转凭证接入\n3. 完成排查后撤销临时授权\n\n所有内容均为虚构演示数据。` });
  }
  const filesPath = `/targets/${primary.id}/files`;
  const home = await call(filesPath, 'POST', { op: 'list', path: '' });
  const files = { 'app.yaml': 'app:\n  name: gateway-demo\n  environment: staging\nserver:\n  host: 127.0.0.1\n  port: 8080\nlogging:\n  level: info\n  retention_days: 7\nhealth:\n  path: /healthz\n', 'README.md': '# 演示应用\n\n此目录由本机 SSH 测试服务提供。\n支持文件浏览、编辑、上传与下载。\n', 'deploy.sh': '#!/bin/sh\n# 演示部署步骤，不执行实际发布\nprintf "检查配置完成\\n"\n', 'access.log': '2026-09-22T09:00:00 GET /healthz 200 2ms\n2026-09-22T09:01:00 GET /api/status 200 8ms\n', 'compose.yaml': 'services:\n  app:\n    image: example.invalid/demo/app:1.0\n    ports:\n      - "8080:8080"\n', 'metrics.json': '{"requests":12840,"error_rate":0.002,"latency_ms":18}\n' };
  for (const [name, content] of Object.entries(files)) {
    const path = `${home.path}/${name}`;
    if (!home.entries.some(entry => entry.name === name)) await call(filesPath, 'POST', { op: 'create', path, directory: false });
    const file = await call(filesPath, 'POST', { op: 'read', path });
    await call(filesPath, 'POST', { op: 'write', path, content, version: file.version });
  }
  return { primary, multi, mapping, counts: { targets: 36, accounts: 8, mappings: 12, globalIPs: 8, shortcuts: (await call('/shortcuts?page_size=100')).total, notes: 36, files: 6 } };
}
