<script setup lang="ts">
import { msg, t, display } from './i18n'
import { ref } from 'vue'
import { api } from './api'
defineProps<{ sshPort: string }>()
const dialog = ref<HTMLDialogElement>()
const host = ref(''), port = ref<number | ''>(''), error = ref(''), note = ref('')
const busy = ref(false), loaded = ref(false)
async function show() {
  error.value = ''; note.value = ''; busy.value = true; loaded.value = false
  host.value = ''; port.value = ''; dialog.value?.showModal()
  try { const value = await api<{host: string; port: number}>('/relay-endpoint'); host.value = value.host; port.value = value.port || ''; loaded.value = true }
  catch (e) { error.value = e instanceof Error ? e.message : msg('text.6aa27f14bff1') }
  finally { busy.value = false }
}
async function save() {
  busy.value = true; error.value = ''; note.value = ''
  try {
    const value = await api<{host: string; port: number}>('/relay-endpoint', 'PUT', { host: host.value.trim(), port: port.value === '' ? 0 : Number(port.value) })
    host.value = value.host; port.value = value.port || ''; note.value = msg('text.a47d9ba5ca9f')
  } catch (e) { error.value = e instanceof Error ? e.message : msg('text.6309a3bb5ba4') }
  finally { busy.value = false }
}
defineExpose({ show })
</script>
<template>
  <dialog ref="dialog" :aria-label="t('text.7b83e4b10381')">
    <div class="dialog-heading"><h2>{{ t('text.7b83e4b10381') }}</h2><button class="close-button" :aria-label="t('text.b05b94e74334')" @click="dialog?.close()">×</button></div>
    <form @submit.prevent="save">
      <div class="dialog-content">
        <p class="hint">{{ t('text.f790d1eab63e') }}</p>
        <p class="hint">{{ t('settings.privateRelay') }}</p>
        <label>{{ t('text.02005857cfde') }}<input v-model="host" :disabled="busy" :placeholder="t('text.d8c3f24b3942')" /></label>
        <label>{{ t('text.5ca5388a530a') }}<input v-model="port" :disabled="busy" type="number" min="1" max="65535" step="1" :placeholder="t('text.6d14ed3a92b1')" aria-describedby="relay-ssh-port-hint" /></label>
        <p id="relay-ssh-port-hint" class="hint">{{ t('text.9a6d9d6badd0') }} {{ display(sshPort) }}。</p>
        <p class="hint">{{ t('text.89ed75c6b84f') }}</p>
        <p v-if="error" class="error" role="alert">{{ display(error) }}</p><p v-if="note" role="status">{{ display(note) }}</p>
      </div>
      <div class="dialog-footer"><button type="button" @click="dialog?.close()">{{ t('text.3fd47edce45b') }}</button><button class="primary" :disabled="busy || !loaded">{{ display(busy ? t('text.7281e973958f') : t('text.a3030bf8f16d')) }}</button></div>
    </form>
  </dialog>
</template>
