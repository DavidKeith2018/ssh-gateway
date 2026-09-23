import { ref } from 'vue'
const relayNow = ref(Date.now())
setInterval(() => { relayNow.value=Date.now() },1000)
import { msg, display, formatDate } from './i18n'
import { api, type Target, type TargetLogin, type TargetRelay, type RelayCredential, type GlobalIP } from './api'
import { native } from './desktop'

export interface RelayAccess {
  target: Target
  credential: RelayCredential
  login: TargetLogin
  global_ips: GlobalIP[]
  ssh_host?: string
  ssh_port: string
  host_fingerprint: string
}
export function relayOptions(target: Target): TargetRelay[] {
  return target.relays?.length ? target.relays : [{ id: 'default', username: target.relay_user, login_id: 'default', enabled: true, expires_at: target.relay_expires_at }]
}
export function relayActive(relay: TargetRelay) { return relay.enabled && (!relay.expires_at || Date.parse(relay.expires_at) > relayNow.value) }
export function relayLabel(target: Target, relay: TargetRelay) {
  const user = target.logins?.find(login => login.id === relay.login_id)?.user || target.user
  return `${user} · ${relay.username}${!relay.enabled ? msg('text.5b341a32d2a1') : !relayActive(relay) ? ' · ' + msg('feature.expired') : relay.expires_at ? ' · ' + formatDate(relay.expires_at) : ''}`
}
export async function fetchRelayAccess(target: Target, relayID: string) {
  const data = await api<RelayAccess>(`/targets/${encodeURIComponent(target.id)}/relays/${encodeURIComponent(relayID)}/credentials`, 'POST', {})
  const host = data.ssh_host || (native ? (await native.Info()).settings.connect_host : location.hostname)
  return { data, host: host.replace(/^\[|\]$/g, '') }
}
export function shellArg(value: string) { return /^[a-zA-Z0-9_.:@-]+$/.test(value) ? value : `'${value.replace(/'/g, `'"'"'`)}'` }
export function relayCommand(data: RelayAccess, host: string) {
  return `ssh -p ${shellArg(data.ssh_port)} ${shellArg(`${data.credential.username}@${host}`)}`
}
export function relayInstructions(data: RelayAccess, host: string) {
  const targetHost = data.target.host.includes(':') ? `[${data.target.host}]` : data.target.host
  return [
    msg('text.ef424ee5750f', [data.target.name, data.login.user, targetHost, data.target.port]),
    msg('text.d3e4bc7bf99b', [relayCommand(data, host)]),
    msg('text.0633a7464a97', [data.credential.password]),
    msg('feature.validUntil', [relayOptions(data.target).find(r => r.id === data.credential.id)?.expires_at ? new Date(relayOptions(data.target).find(r => r.id === data.credential.id)!.expires_at!).toISOString() : msg('feature.permanent')]),
  ].join('\n')
}
export async function copyText(value: string, container: HTMLElement = document.body) {
  value = display(value)
  try {
    if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(value); return }
  } catch { /* 普通 HTTP 或剪贴板不可用时，尝试浏览器的选择复制。 */ }
  const input = document.createElement('textarea')
  const focused = document.activeElement as HTMLElement | null
  input.value = value
  input.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0'
  container.appendChild(input)
  try {
    input.select()
    if (!document.execCommand('copy')) throw new Error(msg('text.432356bdcd99'))
  } finally { input.remove(); focused?.focus() }
}

export function loginOptions(target: Target): TargetLogin[] {
  return target.logins?.length ? target.logins : [{ id: 'default', user: target.user, auth_type: target.auth_type }]
}
export interface ServerAccess {
  target: Target
  login: TargetLogin
  credential: { auth_type: string; password?: string; private_key?: string; passphrase?: string }
}
export function serverCommand(data: ServerAccess) {
  return `ssh -p ${data.target.port} ${shellArg(`${data.login.user}@${data.target.host}`)}`
}
export function serverInstructions(data: ServerAccess) {
  return [
    msg('text.64b8a66326d0'),
    msg('text.db3d4fc12286', [data.target.name]),
    msg('text.1683bb6d8ead', [data.login.user]),
    msg('text.fb18a5285f61', [serverCommand(data)]),
    msg('text.0066aaf3990b', [data.target.host_fingerprint]),
    ...(data.credential.auth_type === 'private_key'
      ? [msg('text.93dfd125e2c9'), msg('text.46218a138354', [data.credential.private_key]), ...(data.credential.passphrase ? [msg('text.e79c5203df15', [data.credential.passphrase])] : [])]
      : [msg('text.e866705bcc0d', [data.credential.password])]),
    msg('text.fcd7e0671247'),
  ].join('\n')
}
