import { t } from './i18n'
import { createApp, h } from 'vue'
import LanguageSwitcher from './LanguageSwitcher.vue'
import { initialiseDesktop } from './desktop'
import './style.css'
import './theme.css'
import './compact.css'

async function start() {
  await initialiseDesktop()
  const { default: App } = await import('./App.vue')
  createApp(App).mount('#app')
}
void start().catch(error => {
  console.error(error)
  createApp({ setup: () => () => h('main', { class: 'loading' }, [
    h('div', { class: 'entry-language' }, [h(LanguageSwitcher)]),
    h('p', { role: 'alert' }, t('text.865a6e217f9f')),
  ]) }).mount('#app')
})
