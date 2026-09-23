import { errorMessage, localizeReply } from './i18n'
import { native } from './desktop'

export interface TargetLogin { id: string; user: string; auth_type: 'password' | 'private_key' }
export interface TargetRelay { expires_at?: string | null; id: string; username: string; login_id: string; enabled: boolean }
export interface RelayCredential { id: string; username: string; login_id: string; password: string }
export interface PutResult { target?: Target; id: string; relay_user: string; relay_password: string; credentials?: RelayCredential[] }

export interface Target {
  relay_expires_at?: string | null
  logins?: TargetLogin[]
  relays?: TargetRelay[]
  default_login_id?: string
  tags: string[]
  id: string
  name: string
  host: string
  port: number
  user: string
  auth_type: 'password' | 'private_key'
  host_fingerprint: string
  relay_user: string
  allowed_sources: string[]
  source_mode: string
  enabled: boolean
  revision: number
}

export type TargetOption = Pick<Target, 'id' | 'name' | 'host' | 'tags'>

export interface Event {
  id: number
  time: string
  target: string
  source: string
  message: string
}

export interface Account { id: string; username: string; enabled: boolean; target_ids: string[]; tags: string[] }

export interface Me { id: string; username: string; is_admin: boolean; source_ip: string; ssh_port: string; host_fingerprint: string }

export async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  if (native) {
    const reply = await native.Call(method, path, body === undefined ? '' : JSON.stringify(body))
    if (reply.status >= 400) {
      if (reply.status === 423) window.dispatchEvent(new Event('credentials-locked'))
      if (reply.status === 401 && path !== '/login') window.dispatchEvent(new Event('session-expired'))
      throw new Error(errorMessage(reply.data))
    }
    return localizeReply(reply.data) as T
  }
  const response = await fetch(`/api${path}`, {
    method,
    headers: method === 'GET' ? {} : { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const result = await response.json()
  if (!response.ok) {
    if (response.status === 423) window.dispatchEvent(new Event('credentials-locked'))
    if (response.status === 401 && path !== '/login') window.dispatchEvent(new Event('session-expired'))
    throw new Error(errorMessage(result))
  }
  return localizeReply(result) as T
}

export interface GlobalIP { ip: string; expires_at: string }
