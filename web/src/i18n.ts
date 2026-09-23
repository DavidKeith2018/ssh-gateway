import { readonly, ref, watch } from 'vue'
import zh from './locales/zh-CN.json'
import en from './locales/en.json'

export type Locale = 'zh-CN' | 'en'
export type MessageKey = keyof typeof zh
export interface LocalizedMessage {
  message_key?: string
  message_params?: unknown[]
  error?: string
  message?: string
}
const dictionaries: Record<Locale, Record<MessageKey, string>> = { 'zh-CN': zh, en }
const storageKey = 'ssh-gateway:locale'
export function supportedLocale(value: unknown): Locale | undefined {
  return value === 'zh-CN' || value === 'en' ? value : undefined
}
function systemLocale(): Locale {
  return /^zh(?:-|$)/i.test(navigator.languages?.[0] || navigator.language) ? 'zh-CN' : 'en'
}
function savedLocale() {
  try { return supportedLocale(localStorage.getItem(storageKey)) } catch { return undefined }
}
const current = ref<Locale>(supportedLocale(new URL(location.href).searchParams.get('lang')) || savedLocale() || systemLocale())
export const locale = readonly(current)

function updateURL(value: Locale) {
  const url = new URL(location.href)
  if (!url.searchParams.has('lang')) return
  url.searchParams.set('lang', value)
  try { history.replaceState(history.state, '', url) } catch { /* 原生入口可能不允许更新 URL。 */ }
}
export function setLocale(value: Locale) {
  if (!supportedLocale(value)) return
  current.value = value
  try { localStorage.setItem(storageKey, value) } catch { /* 禁用存储时仍可切换。 */ }
  updateURL(value)
}
window.addEventListener('storage', event => {
  if (event.key !== storageKey && event.key !== null) return
  current.value = supportedLocale(event.newValue) || savedLocale() || systemLocale()
  updateURL(current.value)
})
watch(current, value => { document.documentElement.lang = value; document.title = t('text.267e3e1a658d', [], value) }, { immediate: true, flush: 'sync' })

// 消息引用保持既有字符串状态/事件接口；只在渲染、原生对话框和复制边界解析。
// 用户文本永不按中文内容匹配翻译，参数也不参与模板替换。
const start = '\uFFF9ssh-gateway:'
const end = '\uFFFB'
export function msg(key: MessageKey, params: unknown[] = []): string {
  return start + encodeURIComponent(JSON.stringify([key, params])) + end
}
export function t(key: MessageKey, params: unknown[] = [], language: Locale = current.value): string {
  const template = dictionaries[language][key] ?? zh[key] ?? key
  return template.replace(/\{(\d+)\}/g, (placeholder, index) => index < params.length ? String(display(params[index], language) ?? '') : placeholder)
}
export function display<T>(value: T, language: Locale = current.value): T {
  if (typeof value !== 'string' || !value.includes(start)) return value
  return value.replace(/\uFFF9ssh-gateway:([^\uFFFB]*)\uFFFB/g, (original, encoded) => {
    try {
      const [key, params] = JSON.parse(decodeURIComponent(encoded))
      return typeof key === 'string' && Object.hasOwn(zh, key) && Array.isArray(params) ? t(key as MessageKey, params, language) : original
    } catch { return original }
  }) as T
}
export function errorMessage(value: unknown): string {
  if (value instanceof Error && value.cause) return errorMessage(value.cause)
  if (value && typeof value === 'object') {
    const info = value as LocalizedMessage
    if (typeof info.message_key === 'string' && Object.hasOwn(zh, info.message_key)) {
      return msg(info.message_key as MessageKey, (Array.isArray(info.message_params) ? info.message_params : []).map(param =>
        param && typeof param === 'object' && 'message_key' in param ? errorMessage(param) : param))
    }
    const raw = info.error || info.message
    if (typeof raw === 'string' && raw) return raw.includes(start) ? raw : msg('error.unknown', [raw])
  }
  const raw = String(value ?? '')
  return raw.includes(start) ? raw : msg('error.unknown', [raw])
}
export function formatDate(value: string | number | Date): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat(current.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}
export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(current.value, options).format(value)
}

// 仅处理状态字段；用户内容、终端字节和凭证保持原值。
export function localizeReply<T>(value: T): T {
  if (Array.isArray(value)) return value.map(item => localizeReply(item)) as T
  if (!value || typeof value !== 'object') return value
  const source = value as Record<string, unknown>
  const result = { ...source }
  for (const field of ['error', 'last_error', 'warning']) {
    if (typeof source[field] === 'string' && source[field]) {
      const descriptor = source[field + '_i18n'] || (field === 'error' ? source : undefined)
      result[field] = errorMessage(descriptor && typeof descriptor === 'object'
        ? { ...descriptor, error: source[field] } : { error: source[field] })
    }
  }
  if (Array.isArray(source.items)) result.items = source.items.map(item => localizeReply(item))
  return result as T
}
