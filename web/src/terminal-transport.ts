import { msg, errorMessage } from './i18n'
import { randomID } from './random-id'
import { native, onDesktopEvent } from './desktop'

export interface TerminalConnectionInfo { shared_id?: string; mode: 'server' | 'relay'; user: string; host: string; port: string }

export function connectTerminal(target: string, output: (data: Uint8Array, ack: () => void) => void, status: (type: string, message: string) => void, selection = '') {
  let closed = false
  if (native) {
    const id = `t-${randomID()}`
    const unsubscribe = onDesktopEvent('terminal', event => {
      if (event.id !== id || closed) return
      if (event.type === 'output') {
        output(Uint8Array.from(atob(event.data!), char => char.charCodeAt(0)), () => { void native!.AckTerminal(id) })
      } else status(event.type, ['error', 'closed', 'ready'].includes(event.type) && event.message_key ? errorMessage(event) : event.message || '')
    })
    void native!.OpenTerminalConnection(id, target, selection).catch(error => { if (!closed) status('error', String(error)) })
    return {
      input(data: string) { void native!.TerminalInput(id, data, 0, 0).catch(error => status('error', String(error))) },
      resize(columns: number, rows: number) { void native!.TerminalInput(id, '', columns, rows).catch(error => status('error', String(error))) },
      close() { if (closed) return; closed = true; unsubscribe(); void native!.CloseTerminal(id).catch(() => {}) },
    }
  }
  const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/targets/${encodeURIComponent(target)}/terminal${selection ? `?connection=${encodeURIComponent(selection)}` : ''}`)
  socket.binaryType = 'arraybuffer'
  let finalMessage = false
  socket.onmessage = event => {
    if (closed) return
    if (event.data instanceof ArrayBuffer) output(new Uint8Array(event.data), () => {})
    else {
      const message = JSON.parse(event.data)
      if (message.type !== 'ready' && message.type !== 'connection') finalMessage = true
      status(message.type, ['error', 'closed', 'ready'].includes(message.type) && message.message_key ? errorMessage(message) : message.message)
    }
  }
  socket.onerror = () => { finalMessage = true; if (!closed) status('error', msg('text.2c7ace236424')) }
  socket.onclose = () => { if (!closed && !finalMessage) status('closed', msg('text.2ab7d89557e7')) }
  const send = (data: unknown) => { if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(data)) }
  return {
    input(data: string) { send({ type: 'input', data }) },
    resize(columns: number, rows: number) { send({ type: 'resize', columns, rows }) },
    close() { if (closed) return; closed = true; socket.close() },
  }
}
