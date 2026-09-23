import { msg, errorMessage } from './i18n'
import { randomID } from './random-id'
import { native } from './desktop'
import { connectionQuery } from './connection-query'
export interface FileEntry {
  link?: boolean
  name: string
  path: string
  kind: 'directory' | 'file' | 'link'
  size: number
  modified: string
}
export interface Directory {
  path: string
  entries: FileEntry[]
}
export interface FileContent {
  path: string
  content: string
  version: string
}
export interface Sample {
  time: string
  cpu_total: number
  cpu_idle: number
  cpu_ready: boolean
  memory_total: number
  memory_available: number
  memory_ready: boolean
  disk_usage: number
  disk_ready: boolean
}
export class MachineError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message)
  }
}
export async function machineAPI<T>(
  target: string,
  operation: 'files' | 'notes' | 'resources' | 'hardware',
  body?: unknown,
  signal?: AbortSignal,
  shared?: string,
): Promise<T> {
  const id = randomID()
  const cancel = () => {
    void native?.CancelWork(id)
  }
  if (signal?.aborted) throw new DOMException(msg('text.9596d1ac92cd'), 'AbortError')
  signal?.addEventListener('abort', cancel, { once: true })
  try {
    let status: number, data: any
    if (native) {
      const reply = shared === undefined
        ? await native.MachineCall(id, target, operation, JSON.stringify(body ?? {}))
        : await native.MachineCallConnection(id, target, operation, JSON.stringify(body ?? {}), connectionQuery(target, '', shared))
      status = reply.status
      data = reply.data
    } else {
      const response = await fetch(
        `/api/targets/${encodeURIComponent(target)}/${operation}${connectionQuery(target, '?', shared)}`,
        {
          method:
            operation === 'files' || operation === 'notes' ? 'POST' : 'GET',
          headers: { 'Content-Type': 'application/json' },
          body:
            operation === 'files' || operation === 'notes'
              ? JSON.stringify(body)
              : undefined,
          signal,
        },
      )
      status = response.status
      data = await response.json()
    }
    if (status === 401) window.dispatchEvent(new Event('session-expired'))
    if (status >= 400) throw new MachineError(errorMessage(data), status)
    if (signal?.aborted) throw new DOMException(msg('text.9596d1ac92cd'), 'AbortError')
    return data as T
  } finally {
    signal?.removeEventListener('abort', cancel)
  }
}
export function parentPath(path: string) {
  return path.slice(0, path.lastIndexOf('/')) || '/'
}
export function joinPath(parent: string, name: string) {
  return `${parent === '/' ? '' : parent}/${name}`
}
export function baseName(path: string) {
  return path.split('/').pop() || '/'
}
export function errorText(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

export interface HardwareInfo {
  disk_total?: number
  hostname: string
  os: string
  kernel: string
  architecture: string
  cpu_model: string
  logical_cpus: number
  memory_total: number
  product: string
}
