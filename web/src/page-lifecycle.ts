import { onMounted, onBeforeUnmount } from 'vue'

// 关闭窗口、刷新和离开文档不保证触发 Vue 卸载；取消离开时不会触发 pagehide。
export function onPageLeave(close: () => void, restore: () => void) {
  const show = (event: PageTransitionEvent) => { if (event.persisted) restore() }
  onMounted(() => {
    window.addEventListener('pagehide', close)
    window.addEventListener('pageshow', show)
  })
  onBeforeUnmount(() => {
    window.removeEventListener('pagehide', close)
    window.removeEventListener('pageshow', show)
  })
}
