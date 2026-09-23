<script setup lang="ts">
import { msg, t, display } from './i18n'
import { ref } from 'vue'
import { api } from './api'
import { masterPasswordTransportError, type MasterPasswordStatus } from './security-api'
const transportError = masterPasswordTransportError()
const emit = defineEmits<{ change: [status: MasterPasswordStatus] }>()
const dialog = ref<HTMLDialogElement>()
const status = ref<MasterPasswordStatus>({ enabled: false, locked: false })
const mode = ref<'change' | 'disable'>('change')
const adminPassword = ref(''), current = ref(''), password = ref(''), confirm = ref('')
const error = ref(''), notice = ref(''), busy = ref(false), confirmed = ref(false)
function clearPasswords() { adminPassword.value = current.value = password.value = confirm.value = ''; confirmed.value = false }
async function show() {
  clearPasswords(); error.value = notice.value = ''; mode.value = 'change'
  dialog.value?.showModal()
  busy.value = true
  try { status.value = await api<MasterPasswordStatus>('/security') }
  catch (e) { error.value = e instanceof Error ? e.message : msg('text.37af59ecfe92') }
  finally { busy.value = false }
}
function switchMode(value: 'change' | 'disable') { mode.value = value; clearPasswords(); error.value = notice.value = '' }
async function save() {
  error.value = notice.value = ''
  if (transportError) { error.value = transportError; return }
  if (mode.value !== 'disable' && password.value !== confirm.value) { error.value = msg('text.61153d5c7dd3'); return }
  if (mode.value === 'disable' && !confirmed.value) { error.value = msg('text.6a315bf71a81'); return }
  busy.value = true
  const action = mode.value === 'disable' ? 'disable' : status.value.enabled ? 'change' : 'enable'
  try {
    status.value = await api<MasterPasswordStatus>('/security/master-password', 'POST', {
      action, admin_password: adminPassword.value, current: current.value,
      password: action === 'disable' ? '' : password.value,
    })
    clearPasswords(); mode.value = 'change'; emit('change', status.value)
    notice.value = action === 'disable' ? msg('text.be718473bf02') : msg('text.6545d8e9be7f')
  } catch (e) { error.value = e instanceof Error ? e.message : msg('text.6309a3bb5ba4') }
  finally { busy.value = false }
}
defineExpose({ show })
</script>

<template>
  <dialog ref="dialog" class="small-dialog security-dialog" aria-labelledby="security-title" @close="clearPasswords">
    <div class="dialog-heading"><h2 id="security-title">{{ t('settings.encryption') }}</h2><button class="close-button" :aria-label="t('text.a017b8703722')" @click="dialog?.close()">×</button></div>
    <div class="dialog-content">
      <h3>{{ t('text.b7dae45379ba') }} {{ display(status.enabled ? t('text.dfb802238b38') : t('text.f95ea7f4c063')) }}</h3>
      <p class="hint">{{ t('text.a6f18dfb60ff') }}</p>
      <div v-if="status.enabled" class="security-modes"><button :aria-pressed="mode === 'change'" @click="switchMode('change')">{{ t('text.41c964f19c97') }}</button><button :aria-pressed="mode === 'disable'" @click="switchMode('disable')">{{ t('text.2d55fbc80595') }}</button></div>
      <p v-if="transportError" class="error" role="alert">{{ display(transportError) }}</p>
      <form @submit.prevent="save">
        <label>{{ t('text.c19091521d71') }}<input v-model="adminPassword" type="password" autocomplete="current-password" maxlength="72" required /></label>
        <label v-if="status.enabled">{{ t('text.5ee7b0789fb7') }}<input v-model="current" type="password" autocomplete="off" maxlength="1024" required /></label>
        <template v-if="mode !== 'disable'">
          <label>{{ display(status.enabled ? t('text.1cdcd2c700d0') : t('text.0a72ed1550a7')) }}<input v-model="password" type="password" autocomplete="new-password" minlength="12" maxlength="1024" required /></label>
          <label>{{ t('text.12aaa082bc80') }}<input v-model="confirm" type="password" autocomplete="new-password" minlength="12" maxlength="1024" required /></label>
          <p class="hint">{{ t('text.8f987048ac44') }}</p>
          <p class="hint">{{ t('text.54a1083113a9') }}</p>
        </template>
        <label v-else class="checkbox-label"><input v-model="confirmed" type="checkbox" required />{{ t('text.0068cfbf0070') }}</label>
        <p v-if="error" class="error" role="alert">{{ display(error) }}</p><p v-if="notice" class="security-success" role="status">{{ display(notice) }}</p>
        <button class="primary" :disabled="busy || !!transportError">{{ display(busy ? t('text.d3d21191f32e') : mode === 'disable' ? t('text.0d6467129800') : status.enabled ? t('text.1e1b3a17dce9') : t('text.6fe2c998c739')) }}</button>
      </form>
    </div>
  </dialog>
</template>

<style scoped>
.security-dialog{width:min(520px,calc(100vw - 28px))}.security-modes{display:flex;gap:8px;margin:12px 0}.security-modes button[aria-pressed=true]{background:#e8f2ee;color:#087864;border-color:#b8d6c9}.security-success{color:#176448;background:#edf6f1;padding:10px;border-radius:6px;font-size:13px}.checkbox-label{align-items:flex-start}h3{font-size:15px}form>.primary{margin-top:12px}
</style>
