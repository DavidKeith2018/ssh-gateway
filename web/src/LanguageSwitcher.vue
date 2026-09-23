<script setup lang="ts">
import { ref, useId } from 'vue'
import { locale, setLocale, t } from './i18n'
const id = useId(), menu = ref<HTMLElement>(), trigger = ref<HTMLButtonElement>()
function position() {
  if (!menu.value || !trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  menu.value.style.top = `${rect.bottom + 6}px`
  menu.value.style.left = `${Math.max(8, Math.min(rect.right - 150, innerWidth - 158))}px`
}
function choose(value: 'zh-CN' | 'en') { setLocale(value); menu.value?.hidePopover(); trigger.value?.focus() }
</script>
<template>
  <div class="language-switcher" :data-locale="locale">
    <button ref="trigger" class="text-button language-trigger menu-trigger" :popovertarget="id" :aria-label="t('language.select')" @click="position"><span>{{ locale === 'zh-CN' ? '简体中文' : 'English' }}</span><svg class="menu-chevron" width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="m4 6 4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg></button>
    <div :id="id" ref="menu" popover class="compact-menu language-menu">
      <button lang="zh-CN" :aria-pressed="locale === 'zh-CN'" @click="choose('zh-CN')">简体中文 <span v-if="locale === 'zh-CN'" aria-hidden="true">✓</span></button>
      <button lang="en" :aria-pressed="locale === 'en'" @click="choose('en')">English <span v-if="locale === 'en'" aria-hidden="true">✓</span></button>
    </div>
  </div>
</template>
<style scoped>
.language-switcher{flex:0 0 auto}.language-trigger{font:inherit;font-size:12px;white-space:nowrap;min-height:32px}.language-menu{width:150px}
</style>
