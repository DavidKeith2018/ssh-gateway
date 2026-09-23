import { readonly, ref, watch } from 'vue'

type Theme = 'light' | 'dark'
const key = 'ssh-gateway:theme'
const currentTheme = ref<Theme>('light')
try {
  currentTheme.value = localStorage.getItem(key) === 'dark' ? 'dark' : 'light'
} catch {}

watch(currentTheme, value => {
  document.documentElement.dataset.theme = value
}, { immediate: true, flush: 'sync' })

window.addEventListener('storage', event => {
  if (event.key === key || event.key === null) {
    currentTheme.value = event.newValue === 'dark' ? 'dark' : 'light'
  }
})

export const theme = readonly(currentTheme)
export function changeTheme() {
  currentTheme.value = currentTheme.value === 'light' ? 'dark' : 'light'
  try { localStorage.setItem(key, currentTheme.value) } catch {}
}
