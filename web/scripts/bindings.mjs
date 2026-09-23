import { spawnSync } from 'node:child_process'

// 只做静态绑定生成，不依赖本机 GTK、WebKit 或 Xcode。
const result = spawnSync('go', [
  '-C', '../desktop', 'run',
  'github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.20',
  'generate', 'bindings', '-ts', '-i', '-names',
  '-f', '-tags server', '-d', '../web/src/bindings', '.',
], { stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0' } })
if (result.error) console.error(result.error)
process.exit(result.status ?? 1)
