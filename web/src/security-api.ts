import { msg } from './i18n'
import { native } from './desktop'
export interface MasterPasswordStatus { enabled: boolean; locked: boolean }

// 在发出请求前阻止主密码经远程 HTTP 明文传输。
export function masterPasswordTransportError(): string {
  const host = location.hostname
  if (native || location.protocol === 'https:' || host === 'localhost' || host === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(host)) return ''
  return msg('text.613d8d843a53')
}
