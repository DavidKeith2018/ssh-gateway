<script setup lang="ts">
import { msg, t, display } from './i18n'
import { computed, onBeforeUnmount, ref } from 'vue'
import type { Target } from './api'
import { errorText, machineAPI, MachineError } from './machine-api'

const dialog = ref<HTMLDialogElement>()
const target = ref<Target>()
const content = ref(''), saved = ref(''), version = ref('')
const busy = ref(false), loaded = ref(false), error = ref(''), notice = ref('')
const dirty = computed(() => loaded.value && content.value !== saved.value)
let controller: AbortController | undefined

async function read() {
  if (!target.value || busy.value) return
  if (dirty.value && !window.confirm(display(msg('text.9d46f70819ef')))) return
  busy.value = true; error.value = ''; notice.value = ''
  controller = new AbortController()
  try {
    const data = await machineAPI<{ content: string; version: string }>(target.value.id, 'notes', { op: 'read' }, controller.signal)
    content.value = saved.value = data.content; version.value = data.version; loaded.value = true
  } catch (e) { error.value = errorText(e) }
  finally { busy.value = false }
}
function show(value: Target) {
  target.value = value; content.value = saved.value = version.value = ''; loaded.value = false
  dialog.value?.showModal()
  void read()
}
async function save() {
  if (!target.value || busy.value || !loaded.value) return
  busy.value = true; error.value = ''; notice.value = ''
  controller = new AbortController()
  try {
    const data = await machineAPI<{ version: string }>(target.value.id, 'notes', { op: 'write', content: content.value, version: version.value }, controller.signal)
    saved.value = content.value; version.value = data.version; notice.value = msg('text.6d1bf9f0107b')
  } catch (e) {
    error.value = e instanceof MachineError && e.status === 409
      ? msg('text.92b9fb1a6de7')
      : errorText(e)
  } finally { busy.value = false }
}
function close() {
  if (busy.value) return
  if (dirty.value && !window.confirm(display(msg('text.46a78c4e0039')))) return
  dialog.value?.close()
}
onBeforeUnmount(() => controller?.abort())
defineExpose({ show })
</script>

<template>
  <dialog ref="dialog" class="note-dialog" aria-labelledby="note-dialog-title" @cancel.prevent="close" @keydown.ctrl.s.prevent="save" @keydown.meta.s.prevent="save">
    <div class="dialog-heading"><h2 id="note-dialog-title">{{ display(target?.name) }} {{ t('text.c06926767460') }}</h2><button class="close-button" :aria-label="t('text.94f7e2f52793')" :disabled="busy" @click="close">×</button></div>
    <div class="dialog-content">
      <p class="hint">{{ t('text.4347435b1507') }}</p>
      <label>{{ t('text.70470d8208e6') }}<textarea v-model="content" :disabled="busy || !loaded" spellcheck="false" @input="notice = ''"></textarea></label>
      <p v-if="busy" role="status">{{ t('text.574ec7517de1') }}</p><p v-if="error" class="error" role="alert">{{ display(error) }}</p><p v-if="notice" role="status">{{ display(notice) }}</p>
    </div>
    <div class="dialog-footer"><button :disabled="busy" @click="read">{{ t('text.df7392cc96be') }}</button><button :disabled="busy" @click="close">{{ t('text.3fd47edce45b') }}</button><button class="primary" :disabled="busy || !loaded || !dirty" @click="save">{{ t('text.b3e264887d50') }}</button></div>
  </dialog>
</template>

<style scoped>
.note-dialog{width:min(800px,calc(100vw - 28px))}
textarea{height:45vh;min-height:160px;resize:vertical;font-family:monospace;line-height:1.6}
</style>
