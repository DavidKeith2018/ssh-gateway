import * as monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker.js?worker'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/shell/shell.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/javascript/javascript.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/python/python.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/go/go.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution.js'
import 'monaco-editor/esm/vs/basic-languages/ini/ini.contribution.js'
;(
  self as typeof self & { MonacoEnvironment: { getWorker: () => Worker } }
).MonacoEnvironment = { getWorker: () => new EditorWorker() }
export { monaco }
export function language(path: string) {
  return (
    (
      {
        yaml: 'yaml',
        yml: 'yaml',
        sh: 'shell',
        bash: 'shell',
        js: 'javascript',
        ts: 'typescript',
        py: 'python',
        go: 'go',
        md: 'markdown',
        conf: 'ini',
        ini: 'ini',
        json: 'plaintext',
      } as Record<string, string>
    )[path.split('.').pop() || ''] || 'plaintext'
  )
}
