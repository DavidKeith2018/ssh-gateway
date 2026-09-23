<script setup lang="ts">
import { msg, t } from './i18n'
import { onPageLeave } from './page-lifecycle'
import { inject } from 'vue'
import { connectionErrorsKey } from './connection-errors'
import { onMounted, onBeforeUnmount, ref, watch, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import type { Target } from './api'
import { connectTerminal, type TerminalConnectionInfo } from './terminal-transport'
const props = defineProps<{
  target: Target
  selection?: string
  active?: boolean
  theme?: string
  initialDirectory?: string
}>()
const emit = defineEmits<{
  connection: [info: TerminalConnectionInfo]
  close: []
  status: [message: string, connected: boolean]
}>()
const errors = inject(connectionErrorsKey)
const errorSource = `terminal:${Math.random()}`
const element = ref<HTMLElement>()
const status = ref(msg('text.8be0ea36a9bb')),
  connected = ref(false)
let terminal: Terminal,
  fit: FitAddon,
  connection: ReturnType<typeof connectTerminal> | undefined,
  observer: ResizeObserver
let disposed = false,
  generation = 0
function setStatus(message: string, online = false) {
  status.value = message
  connected.value = online
  emit('status', message, online)
}
function resize() {
  if (disposed || !element.value?.clientWidth || !element.value.clientHeight)
    return
  fit?.fit()
  if (connected.value) connection?.resize(terminal.cols, terminal.rows)
}
function theme() {
  if (terminal)
    terminal.options.theme =
      props.theme === 'light'
        ? { background: '#f8fafc', foreground: '#243247', cursor: '#3674ef' }
        : { background: '#0d0d0d', foreground: '#d6deeb', cursor: '#72d6bb' }
}
function connect() {
  connection?.close()
  const current = ++generation
  errors?.recover(errorSource)
  setStatus(msg('text.8be0ea36a9bb'))
  connection = connectTerminal(
    props.target.id,
    (data, ack) => {
      if (!disposed && current === generation) terminal.write(data, ack)
    },
    (type, message) => {
      if (disposed || current !== generation) return
      if (type === 'connection') { emit('connection', JSON.parse(message)); return }
      if (type === 'error' || type === 'closed') errors?.report(errorSource, message || msg('text.2ab7d89557e7'))
      if (type === 'ready') errors?.recover(errorSource)
      setStatus(type === 'error' ? msg('text.89c4766afc85') : type === 'closed' ? msg('text.1f0ac6953e04') : message, type === 'ready')
      if (connected.value) {
        resize()
        if (type === 'ready' && props.initialDirectory) changeDirectory(props.initialDirectory)
        if (props.active !== false) terminal.focus()
      }
    },
    props.selection,
  )
}
onPageLeave(() => {
  ++generation
  connection?.close()
  setStatus(msg('text.1f0ac6953e04'))
}, connect)
function sendCommand(command: string) {
  if (!connected.value || props.active === false) return
  connection?.input(command)
  terminal?.focus()
}
function changeDirectory(path: string) {
  // 清除未提交的输入，并将路径作为一个 shell 参数，避免空格或引号改变命令含义。
  const quoted = "'" + path.replaceAll("'", "'\\''") + "'"
  connection?.input('\x15cd -- ' + quoted + '\r')
}
function enterDirectory(path: string) {
  if (!connected.value || props.active === false) return
  changeDirectory(path)
  terminal?.focus()
}
function clear() {
  terminal?.clear()
  terminal?.focus()
}
defineExpose({ sendCommand, enterDirectory, clear, reconnect: connect })
onMounted(() => {
  terminal = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: '"SFMono-Regular", Consolas, monospace',
    scrollback: 3000,
  })
  theme()
  fit = new FitAddon()
  terminal.loadAddon(fit)
  terminal.open(element.value!)
  resize()
  observer = new ResizeObserver(resize)
  observer.observe(element.value!)
  terminal.onData((data) => {
    if (connected.value) connection?.input(data)
  })
  connect()
})
watch(
  () => props.active,
  async (active) => {
    if (active) {
      await nextTick()
      resize()
      terminal?.focus()
    }
  },
)
watch(() => props.theme, theme)
onBeforeUnmount(() => {
  disposed = true
  ++generation
  observer?.disconnect()
  connection?.close()
  terminal?.dispose()
})
</script>
<template>
  <section class="machine-terminal" :aria-label="t('text.38933f67f2f0')">
    <div ref="element" class="machine-terminal-body"></div>
  </section>
</template>
