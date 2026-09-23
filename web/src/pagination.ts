import { msg } from './i18n'
import { nextTick, onBeforeUnmount, shallowReactive, watch, type WatchSource, type Ref } from 'vue'
import { api } from './api'

export interface PageResult<T> { items: T[]; total: number; page: number; page_size: number }
export interface PageChange { page: number; pageSize: number }
export function useServerPage<T, R extends PageResult<T> = PageResult<T>>(path: string, query: () => Record<string, string> = () => ({}), accepted?: (result: R) => void) {
  const state = shallowReactive({ items: [] as T[], total: 0, page: 1, pageSize: 20, loading: false, refreshing: false, loaded: false, error: '' })
  let generation = 0, alive = true
  let pending: PageChange | undefined
  async function load(page = state.page, pageSize = state.pageSize, background = false) {
    // 轮询保留分页交互；主动加载优先，并使尚未返回的轮询结果失效。
    if (background && (!state.loaded || state.loading || state.refreshing || state.error)) return
    const token = ++generation
    pending = { page, pageSize }
    state.loading = !background
    state.refreshing = background
    const params = new URLSearchParams({ ...query(), page: String(page), page_size: String(pageSize) })
    try {
      const result = await api<R>(`${path}?${params}`)
      if (!alive || token !== generation) return
      const changed = state.page !== result.page || state.pageSize !== result.page_size
      Object.assign(state, { items: result.items, total: result.total, page: result.page, pageSize: result.page_size, loaded: true, error: '' })
      accepted?.(result)
      pending = undefined
      return { changed }
    } catch (error) {
      if (alive && token === generation) state.error = error instanceof Error ? error.message : msg('text.f5c7d0c38d18')
    } finally { if (alive && token === generation) { state.loading = false; state.refreshing = false } }
  }
  function reset() {
    generation++; pending = undefined
    Object.assign(state, { items: [], total: 0, page: 1, pageSize: 20, loading: false, refreshing: false, loaded: false, error: '' })
  }
  onBeforeUnmount(() => { alive = false; generation++ })
  return Object.assign(state, {
    load, reset,
    refresh: () => load(state.page, state.pageSize, true),
    invalidate() { generation++; state.refreshing = false; pending = { page: 1, pageSize: state.pageSize }; state.loading = true },
    retry: () => load(pending?.page ?? state.page, pending?.pageSize ?? state.pageSize),
    async change(change: PageChange, container?: HTMLElement | null) {
      const result = await load(change.page, change.pageSize)
      if (result && !result.changed) { await nextTick(); scrollPage(container) }
    },
  })
}
// 输入变化立即使旧请求失效，避免防抖期间旧结果回写。
export function watchPageQuery<T>(source: WatchSource<T>, reload: () => void, invalidate: () => void, delay: number | ((value: T, previous: T) => number) = 0) {
  let timer: ReturnType<typeof setTimeout> | undefined
  watch(source, (value, previous) => { invalidate(); clearTimeout(timer); timer = setTimeout(reload, typeof delay === 'number' ? delay : delay(value, previous)) }, { deep: true })
  onBeforeUnmount(() => clearTimeout(timer))
}

function scrollPage(container?: HTMLElement | null) {
  if (!container?.isConnected) return
  container.scrollTo({ top: 0, left: 0 })
  if (container.scrollHeight <= container.clientHeight + 1) container.scrollIntoView({ block: 'start', inline: 'nearest' })
}
export function watchPageScroll(state: { page: number; pageSize: number }, container: Ref<HTMLElement | undefined>) {
  watch(() => [state.page, state.pageSize], () => scrollPage(container.value), { flush: 'post' })
}
