import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { connect } from 'node:net';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';

const docker = (...args) => execFileSync('docker', args, { encoding: 'utf8', timeout: 180_000 }).trim();

// 只向本机随机端口暴露 SSH；不挂载宿主机目录，退出时删除本次容器。
export async function startDemoSSH() {
  const image = 'ssh-gateway-demo:local';
  const source = createHash('sha256').update(readFileSync(new URL('./ssh/Dockerfile', import.meta.url))).digest('hex');
  let cached = '';
  try { cached = docker('inspect', '--type=image', '--format', '{{index .Config.Labels "org.ssh-gateway.demo-source"}}', image); } catch { /* 首次构建。 */ }
  if (cached !== source) execFileSync('docker', ['build', '--label', `org.ssh-gateway.demo-source=${source}`, '-t', image, fileURLToPath(new URL('./ssh/', import.meta.url))], { timeout: 600_000, stdio: ['ignore', 'ignore', 'inherit'] });
  const id = docker('run', '-d', '--rm', '--hostname', 'gateway-demo', '--memory', '256m', '--cpus', '1', '--pids-limit', '128', '-p', '127.0.0.1::22', image);
  const stop = () => docker('rm', '-f', id);
  try {
    const bindings = JSON.parse(docker('inspect', '--format', '{{json .NetworkSettings.Ports}}', id));
    const port = Number(bindings['22/tcp'][0].HostPort);
    let ready = false;
    for (let attempt = 0; attempt < 60; attempt++) {
      ready = await new Promise(resolve => {
        const socket = connect(port, '127.0.0.1');
        socket.setTimeout(1000);
        const done = value => { socket.destroy(); resolve(value); };
        socket.once('data', data => done(data.toString().startsWith('SSH-2.0-')));
        socket.once('error', () => done(false));
        socket.once('timeout', () => done(false));
        socket.once('end', () => done(false));
      });
      if (ready) break;
      await new Promise(resolve => setTimeout(resolve, 200));
    }
    if (!ready) throw new Error('演示 SSH 容器启动超时');
    const fingerprint = docker('exec', id, 'ssh-keygen', '-lf', '/etc/ssh/ssh_host_ed25519_key.pub').split(/\s+/)[1];
    // 文件树和终端从同一个应用目录开始，截图中的内容均可通过 SFTP 读取。
    docker('exec', id, 'usermod', '-d', '/srv/app', 'demo');
    docker('exec', id, 'cp', '/home/demo/.bashrc', '/srv/app/.bashrc');
    docker('exec', id, 'cp', '/home/demo/.profile', '/srv/app/.profile');
    return { host: '127.0.0.1', port, user: 'demo', auth_type: 'password', target_password: 'demo-ssh-password', host_fingerprint: fingerprint, stop };
  } catch (error) { stop(); throw error; }
}
