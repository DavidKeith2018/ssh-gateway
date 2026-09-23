<script setup lang="ts">
import { msg, t, display } from './i18n'
import { computed } from 'vue'
import { native } from './desktop'
import type { MappingView } from './mappings-api'

const props = defineProps<{ mapping: MappingView; localDesktop: boolean }>()
const emit = defineEmits<{ error: [message: string] }>()
const unavailable = computed(() => {
  if (props.mapping.status !== 'running') return msg('text.d81f5a14890c')
  if (props.mapping.scope === 'loopback') {
    if (props.mapping.direction === 'reverse') return msg('text.e455b9302133')
    const host = window.location.hostname
    if (!props.localDesktop && !/^(localhost|127(?:\.\d{1,3}){3}|\[::1\])$/i.test(host)) return msg('text.9bb4bbea048f')
  }
  return ''
})
const href = computed(() => {
  const address = props.mapping.access_addresses[0]
  if (!address) return ''
  try {
    const url = new URL(`http://${address}`)
    if (url.username || url.password || url.pathname !== '/' || url.search || url.hash || !url.hostname) return ''
    return url.href
  } catch { return '' }
})
const reason = computed(() => unavailable.value || (!href.value ? msg('text.2c52232e91c3') : ''))
async function open(event: MouseEvent) {
  if (!native) return
  event.preventDefault()
  try { await native.OpenExternalURL(href.value) }
  catch (e) { emit('error', msg('text.6d7b53e1bd20', [e instanceof Error ? e.message : String(e)])) }
}
</script>

<template>
  <span v-if="reason" class="mapping-open-disabled" :title="display(reason)" tabindex="0" :aria-label="display(t('text.cba02e854f38', [reason]))"><button disabled>{{ t('text.c771248e511f') }}</button></span>
  <a v-else class="mapping-open" :href="href" target="_blank" rel="noopener noreferrer" :title="t('text.2f843df5885b')" @click="open">{{ t('text.e61bfd8f9604') }}</a>
</template>
