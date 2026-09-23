<script setup lang="ts">
import { msg, t, display, formatDate } from './i18n'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { api, type Target } from './api'
import { theme } from './theme'
import { copyText, fetchRelayAccess, relayCommand, relayInstructions, relayLabel, relayOptions, relayActive, type RelayAccess, loginOptions, type ServerAccess, serverCommand, serverInstructions } from './relay-access'

const props = withDefaults(defineProps<{ target?: Target; card?: boolean; canEdit?: boolean }>(), { canEdit: true })
const emit = defineEmits<{ edit: [target: Target]; selection: [target: Target, value: string]; copied: [message: string] }>()
const current = ref<Target>()
const selected = ref(''), host = ref(''), error = ref(''), note = ref('')
const loading = ref(false), visiblePassword = ref(false)
const data = ref<RelayAccess>()
const serverData = ref<ServerAccess>()
const isServer = computed(() => selected.value.startsWith('server:'))
const logins = computed(() => current.value ? loginOptions(current.value) : [])
const dialog = ref<HTMLDialogElement>()
const fallbackText = ref('')
let generation = 0
const relays = computed(() => current.value ? relayOptions(current.value) : [])
const selectedRelay = computed(() => relays.value.find(relay => relay.id === selected.value))
const instructions = computed(() => isServer.value ? (serverData.value ? serverInstructions(serverData.value) : '') : data.value?.credential.password ? relayInstructions(data.value, host.value) : '')
function chooseTarget(target: Target, username?: string) {
  generation++; data.value = undefined; serverData.value = undefined; error.value = ''; note.value = ''; fallbackText.value = ''
  current.value = target
  selected.value = relayOptions(target).find(item => username ? item.username === username : relayActive(item) && item.login_id === target.default_login_id)?.id
    || relayOptions(target).find(item => relayActive(item))?.id || relayOptions(target)[0]?.id || ''
}
watch(() => props.target, value => { if (value) chooseTarget(value) }, { immediate: true })
async function load() {
  if (!current.value || !selected.value) return false
  const token = ++generation
  data.value = undefined; serverData.value = undefined; fallbackText.value = ''; error.value = ''; note.value = ''; loading.value = true
  try {
    if (isServer.value) {
      const result = await api<ServerAccess>(`/targets/${encodeURIComponent(current.value.id)}/logins/${encodeURIComponent(selected.value.slice(7))}/credentials`, 'POST', {})
      if (token !== generation) return false
      serverData.value = result
      return true
    }
    const result = await fetchRelayAccess(current.value, selected.value)
    if (token !== generation) return false
    data.value = result.data; host.value = result.host
    return true
  } catch (e) {
    if (token === generation) error.value = e instanceof Error ? e.message : msg('text.3064d664f660')
    return false
  } finally { if (token === generation) loading.value = false }
}
async function show(target: Target, username?: string, selection?: string) {
  chooseTarget(target, username)
  if (selection) selected.value = selection
  visiblePassword.value = false
  await nextTick(); dialog.value?.showModal()
  await load()
}
function clear() { generation++; data.value = undefined; serverData.value = undefined; fallbackText.value = ''; visiblePassword.value = false; loading.value = false }
function close() { dialog.value?.close(); clear() }
async function changeSelection() {
  if (current.value) emit('selection', current.value, selected.value)
  clear(); error.value = ''; note.value = ''
  if (dialog.value?.open) await load()
}
async function copySelected(direct = false) {
  if (!direct && isServer.value && !window.confirm(display(msg('text.69ef337cc454')))) return
  if (!await load()) {
    if (direct) { await nextTick(); dialog.value?.showModal() }
    return
  }
  if (!instructions.value) {
    await nextTick(); dialog.value?.showModal()
    return
  }
  const text = instructions.value
  try {
    await copyText(text, dialog.value?.open ? dialog.value : document.body)
    note.value = isServer.value ? msg('text.21f4d2db8ee2') : msg('text.10a45fceac70')
    data.value = undefined; serverData.value = undefined
    if (direct) emit('copied', note.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : msg('text.753d8bb0da99')
    fallbackText.value = text
    await nextTick(); dialog.value?.showModal()
  }
}
async function copyTarget(target: Target, selection?: string) {
  if (selection) {
    chooseTarget(target); selected.value = selection
    await copySelected(true); return
  }
  if (relayOptions(target).filter(item => relayActive(item)).length > 1) { await show(target); return }
  chooseTarget(target)
  await nextTick(); dialog.value?.showModal()
  await copySelected()
}
function edit() { const target = current.value; close(); if (target) emit('edit', target) }
onBeforeUnmount(clear)
defineExpose({ show, close, copyTarget })
</script>
<template>
  <section v-if="card && current" class="relay-access-card" :aria-label="t('text.bdbd8139d9f5')" :data-theme="theme">
    <strong>{{ display(isServer ? t('text.7a342366c31b') : t('text.0546ba31b12c')) }}</strong>
    <label>{{ display(isServer ? t('text.7977db408132') : t('text.925b2ee5ff2b')) }}<select v-model="selected" :disabled="loading" @change="changeSelection"><option v-for="relay in relays" :key="relay.id" :value="display(relay.id)" :disabled="!relayActive(relay)">{{ display(relayLabel(current, relay)) }}</option><option v-if="isServer" :value="display(selected)">{{ t('text.939fd941988b') }} {{ display(logins.find(login => 'server:' + login.id === selected)?.user) }}</option></select></label>
    <button class="primary relay-copy-button" :disabled="loading || (!isServer && (!current.enabled || (!selectedRelay || !relayActive(selectedRelay))))" @click="copySelected()">{{ display(loading ? t('text.86b6d0d63062') : isServer ? t('text.872fdffc7054') : t('text.c4a5b222e09d')) }}</button>
    <small>{{ display(isServer ? t('text.646a2415554e') : t('text.fffc7c0bbdee')) }}</small>
    <p v-if="note" role="status">{{ display(note) }}</p><p v-if="error && !dialog?.open" role="alert">{{ display(error) }}</p>
    <button class="relay-detail-button" @click="show(current, undefined, selected)">{{ t('text.6cb4138e9170') }}</button>
  </section>
  <dialog ref="dialog" class="relay-access-dialog" :aria-label="t('text.95bf1d8cc6e3')" :data-theme="theme" @close="clear">
    <div class="dialog-heading"><h2>{{ display(isServer ? t('text.ab1bb3274bec') : t('text.95bf1d8cc6e3')) }}</h2><button class="close-button" :aria-label="t('text.b07e75b8a70f')" @click="close">×</button></div>
    <div v-if="current" class="dialog-content">
      <p>{{ display(current.name) }} · {{ display(isServer ? t('text.0fd9864d097c') : t('text.f7b202315af6')) }}</p>
      <label>{{ t('text.d2f007f98330') }}<select v-model="selected" :disabled="loading" @change="changeSelection"><optgroup :label="t('text.996fd8697aa1')"><option v-for="relay in relays" :key="relay.id" :value="display(relay.id)" :disabled="!relayActive(relay)">{{ display(relayLabel(current, relay)) }}</option></optgroup><optgroup v-if="canEdit" :label="t('text.6aaa81cb98ae')"><option v-for="login in logins" :key="login.id" :value="display('server:' + login.id)">{{ display(login.user) }} · {{ display(login.auth_type === 'private_key' ? t('text.3ffe935f9d0f') : t('text.a621ab606db2')) }}</option></optgroup></select></label>
      <p v-if="isServer" class="error" role="alert">{{ t('text.831db1be5a89') }}</p>
      <p v-if="loading" role="status">{{ t('text.b71ec8979285') }}</p>
      <p v-if="error" class="error" role="alert">{{ display(error) }} <button :disabled="loading" @click="load">{{ t('text.b8784c8dd563') }}</button></p>
      <p v-if="note" role="status">{{ display(note) }}</p>
      <template v-if="serverData">
        <label>{{ t('text.a7e250a706be') }}<textarea :value="display(serverCommand(serverData))" readonly rows="2" /></label>
        <button @click="visiblePassword = !visiblePassword">{{ display(visiblePassword ? t('text.709dce992974') : t('text.5c1d92665a8e')) }}</button>
        <template v-if="visiblePassword"><label v-if="serverData.credential.auth_type === 'password'">{{ t('text.acc5ab84fc44') }}<input :value="display(serverData.credential.password)" readonly autocomplete="off" /></label><template v-else><label>{{ t('text.1c7154c532cb') }}<textarea :value="display(serverData.credential.private_key)" readonly rows="6" /></label><label v-if="serverData.credential.passphrase">{{ t('text.0550c993e3f9') }}<input :value="display(serverData.credential.passphrase)" readonly autocomplete="off" /></label></template></template>
      </template>
      <template v-if="data">
        <p>{{ t('text.99b697ecb1f5') }}<strong>{{ display(data.login.user) }}</strong>{{ display(data.login.user === 'root' ? t('text.2d9acab25dc5') : t('text.ca2abbdcd726')) }}</p>
        <label>{{ t('text.a7e250a706be') }}<textarea :value="display(relayCommand(data, host))" readonly rows="2" /></label>
        <template v-if="data.credential.password"><label>{{ t('text.5c8a25fb92b0') }}<input :type="visiblePassword ? 'text' : 'password'" :value="display(data.credential.password)" readonly autocomplete="off" /></label><button @click="visiblePassword = !visiblePassword">{{ display(visiblePassword ? t('text.dd1d07bd9ceb') : t('text.ffe21a671190')) }}</button></template>
        <div v-else class="error" role="alert">{{ t('text.37491b228e76') }}{{ display(canEdit ? t('text.c8b939f28654') : t('text.c355017498a6')) }}{{ t('text.f53f94a14014') }}<button v-if="canEdit" @click="edit">{{ t('text.7a2c0d2ad05b') }}</button></div>
        <p class="hint">{{ t('text.bab84e9a667d') }}</p><ul><li v-for="source in data.target.allowed_sources" :key="source">{{ display(source) }}</li></ul>
        <p class="hint">{{ t('text.07706ac29888') }}</p><ul v-if="data.global_ips.length"><li v-for="item in data.global_ips" :key="item.ip">{{ display(item.ip) }} {{ t('text.75a722c002b0') }} {{ display(formatDate(item.expires_at)) }}</li></ul><p v-else>{{ t('text.484d55613910') }}</p>
        <label>{{ t('text.b6622bb3d6a4') }}<textarea :value="display(data.host_fingerprint)" readonly rows="2" /></label>
      </template>
      <label v-if="fallbackText">{{ display(isServer ? t('text.4b5d2f762787') : t('text.33076c4182b2')) }}<textarea :value="display(fallbackText)" readonly rows="12" /></label>
    </div>
    <div class="dialog-footer"><button @click="close">{{ t('text.3fd47edce45b') }}</button><button class="primary relay-copy-button" :disabled="loading || (!isServer && (!current?.enabled || (!selectedRelay || !relayActive(selectedRelay)) || (!!data && !data.credential.password)))" @click="copySelected()">{{ display(isServer ? t('text.872fdffc7054') : t('text.c4a5b222e09d')) }}</button></div>
  </dialog>
</template>
<style>
.relay-access-card,.relay-access-dialog{--relay-bg:#fff;--relay-text:#20312f;--relay-border:#d6e3df;--relay-muted:#607a71;color:var(--relay-text);background:var(--relay-bg);color-scheme:light}
.relay-access-card[data-theme=dark],.relay-access-dialog[data-theme=dark]{--relay-bg:#192a30;--relay-text:#deebe6;--relay-border:#3c585b;--relay-muted:#b1c7bf;color-scheme:dark}
.relay-access-card{padding:16px;border:1px solid #399a82;border-radius:10px;display:flex;flex-direction:column;gap:12px;flex-shrink:0}.relay-access-card>strong{font-size:14px}.relay-access-card label,.relay-access-dialog label{display:flex;flex-direction:column;gap:6px;font-size:12px}.relay-access-card small{color:var(--relay-muted);line-height:1.7}.relay-access-card p{margin:0;overflow-wrap:anywhere;font-size:12px}.relay-access-card .relay-detail-button{color:var(--relay-muted)}
.relay-access-card select,.relay-access-dialog select,.relay-access-dialog input,.relay-access-dialog textarea{width:100%;min-width:0;padding:8px;border:1px solid var(--relay-border);border-radius:6px;background:var(--relay-bg);color:var(--relay-text);font:inherit}.relay-access-dialog textarea{font-family:monospace;resize:vertical}.relay-access-card .relay-copy-button,.relay-access-dialog .relay-copy-button{background:#087864!important;color:#fff!important;border:1px solid #299c85!important;font-weight:650;padding:11px 12px}.relay-access-card .relay-copy-button{width:100%;white-space:normal}
.relay-access-dialog{width:min(640px,calc(100vw - 28px));max-height:90vh;max-height:90dvh;padding:0;overflow:hidden;border:1px solid var(--relay-border)}.relay-access-dialog[open]{display:flex;flex-direction:column}.relay-access-dialog>.dialog-content{overflow:auto;min-height:0}.relay-access-dialog>.dialog-heading,.relay-access-dialog>.dialog-footer{flex-shrink:0;border-color:var(--relay-border);background:var(--relay-bg);gap:10px}.relay-access-dialog .hint{color:var(--relay-muted)}.relay-access-dialog label{margin-bottom:12px}.relay-access-dialog ul{padding-left:20px;overflow-wrap:anywhere}.relay-access-dialog .error button{display:block;margin-top:8px}.relay-access-dialog h2{font-size:18px}.relay-access-dialog .dialog-footer{flex-wrap:wrap}
</style>
