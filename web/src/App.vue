<script setup lang="ts">
import { msg, t, display, locale, formatDate } from './i18n'
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, type Target, type TargetOption, type GlobalIP, type Me, type PutResult } from './api'
const MachinePage = defineAsyncComponent(() => import('./MachinePage.vue'))
import { native, onDesktopEvent, type DesktopInfo } from './desktop'
import TerminalQuickActions from './TerminalQuickActions.vue'
const shortcutManager = ref<InstanceType<typeof TerminalQuickActions>>()
import AccountWorkspace from './AccountWorkspace.vue'
import { theme, changeTheme } from './theme'
import { openMachineWindow } from './open-machine-window'
import SecuritySettings from './SecuritySettings.vue'
import BackupPanel from './BackupPanel.vue'
import ImportTargets from './ImportTargets.vue'
import ConnectionDiagnostic from './ConnectionDiagnostic.vue'
const importTargets=ref<InstanceType<typeof ImportTargets>>()
const diagnostic=ref<InstanceType<typeof ConnectionDiagnostic>>()
import ProgramUpdate from './ProgramUpdate.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'
const programUpdate = ref<InstanceType<typeof ProgramUpdate>>()
import RelayCredentials from './RelayCredentials.vue'
import NoteDialog from './NoteDialog.vue'
const noteDialog = ref<InstanceType<typeof NoteDialog>>()
const sourcesDialog = ref<HTMLDialogElement>()
const sourcesTarget = ref<Target>()
async function showSources(target: Target) {
  sourcesTarget.value = target
  await nextTick()
  sourcesDialog.value?.showModal()
}
const copyNotice = ref('')
let copyNoticeTimer: ReturnType<typeof setTimeout> | undefined
function showCopyNotice(message: string) {
  clearTimeout(copyNoticeTimer)
  copyNotice.value = message
  copyNoticeTimer = setTimeout(() => { copyNotice.value = '' }, 3000)
}

import RelayEndpointSettings from './RelayEndpointSettings.vue'
import { loginOptions, relayOptions, relayLabel, relayActive } from './relay-access'
const connectionSelections = reactive<Record<string, string>>({})
function currentConnection(target: Target) {
  const chosen = connectionSelections[target.id]
  const valid = chosen?.startsWith('server:') ? !!me.value?.is_admin && loginOptions(target).some(login => 'server:' + login.id === chosen) : relayOptions(target).some(relay => relay.id === chosen && relayActive(relay))
  return valid ? chosen! : relayOptions(target).find(relay => relayActive(relay))?.id || relayOptions(target)[0]?.id || ''
}
function selectConnection(target: Target, value: string) { connectionSelections[target.id] = value }
const relayEndpointSettings = ref<InstanceType<typeof RelayEndpointSettings>>()
import { masterPasswordTransportError, type MasterPasswordStatus } from './security-api'
const masterTransportError = masterPasswordTransportError()
import TargetEditor from './TargetEditor.vue'
import MappingWorkspace from './MappingWorkspace.vue'
import type { MappingSummary } from './mappings-api'
import Pagination from './Pagination.vue'
import { useServerPage, watchPageQuery, watchPageScroll } from './pagination'
const mappings = ref<InstanceType<typeof MappingWorkspace>>()
const mappingSummary = ref<MappingSummary>({ total: 0, running: 0, stopped: 0, error: 0, by_target: {} })
const mappingCount = (id: string) => { const count = mappingSummary.value.by_target[id]; return `${count?.running || 0} / ${count?.total || 0}` }

const params = new URLSearchParams(window.location.search)
const detachedTargetID = params.get('window') === '1' ? params.get('machine') : null
const detached = !!detachedTargetID
const detachedError = ref('')
const me = ref<Me | null>(null)
const desktopInfo = ref<DesktopInfo | null>(null)
function positionSettings(event: MouseEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  if (systemMenu.value) { systemMenu.value.style.top = `${rect.bottom + 6}px`; systemMenu.value.style.left = `${Math.max(8, Math.min(rect.right - 190, innerWidth - 198))}px` }
}
const systemMenu = ref<HTMLElement>()
const backupDialog = ref<HTMLDialogElement>()
const backupPanel = ref<InstanceType<typeof BackupPanel>>()
const securitySettings = ref<InstanceType<typeof SecuritySettings>>()
const vault = ref<MasterPasswordStatus>({ enabled: false, locked: false })
const unlockPassword = ref(''), unlockError = ref('')
const setupProtection = ref(false), setupMaster = ref(''), setupMasterConfirm = ref('')
async function unlockCredentials() {
  busy.value = 'unlock'; unlockError.value = ''
  try { if (masterTransportError) throw new Error(masterTransportError); vault.value = await api<MasterPasswordStatus>('/unlock', 'POST', { password: unlockPassword.value }); unlockPassword.value = ''; await restore() }
  catch (e) { unlockError.value = message(e); unlockPassword.value = '' }
  finally { busy.value = '' }
}
function credentialsLocked() { vault.value.locked = true; clearSession() }

const desktopDialog = ref<HTMLDialogElement>()
const quitDialog = ref<HTMLDialogElement>()
let removeQuitListener: (() => void) | undefined
const desktopSettings = reactive({ external: false, port: 2222, connect_host: '127.0.0.1' })
const currentAdmin = ref('')
const newAdmin = ref('')
const desktopError = ref('')
async function updateDesktop() { if (native) desktopInfo.value = await native.Info() }
async function chooseDirectory() {
  try { await native!.ChooseDataDir(); await updateDesktop() }
  catch (error) { loginError.value = message(error) }
}
async function openDesktopSettings() {
  await updateDesktop(); Object.assign(desktopSettings, desktopInfo.value!.settings)
  desktopError.value = ''; currentAdmin.value = ''; newAdmin.value = ''
  desktopDialog.value?.showModal()
}
async function saveDesktopSettings() {
  busy.value = 'desktop'
  try {
    await updateDesktop()
    const confirmed = !desktopInfo.value!.active || window.confirm(display(msg('text.8f802af71f31')))
    if (!confirmed) return
    await native!.SaveSettings({ ...desktopSettings, port: Number(desktopSettings.port) }, true)
    await updateDesktop(); me.value = await api<Me>('/me')
    desktopError.value = desktopInfo.value!.error
    if (!desktopError.value) notify(msg('text.e1073f0d6170'))
  } catch (error) { desktopError.value = message(error) }
  finally { busy.value = '' }
}
async function resetAdmin() {
  try {
    await api('/desktop/password', 'POST', { current: currentAdmin.value, password: newAdmin.value })
    currentAdmin.value = ''; newAdmin.value = ''; desktopDialog.value?.close(); clearSession()
  } catch (error) { desktopError.value = message(error) }
}

const loading = ref(true)
const username = ref('ssh-admin')
const password = ref('')
const loginError = ref('')
const targetOptions = ref<TargetOption[]>([])
const targetStats = ref({ total: 0, enabled: 0 })
const targetList = useServerPage<Target>('/targets', () => ({ q: search.value.trim(), tag: tagFilter.value }))
const targets = computed(() => targetList.items)
const targetScroll = ref<HTMLElement>(), globalScroll = ref<HTMLElement>()
const tab = ref<'targets' | 'mappings' | 'users'>('targets')
const globalList = useServerPage<GlobalIP>('/global-ips')
const globalIPs = computed(() => globalList.items)
watchPageScroll(targetList, targetScroll)
watchPageScroll(globalList, globalScroll)
const globalDialog = ref<HTMLDialogElement>()
const globalIPInput = ref<HTMLInputElement>()
const globalNotice = ref(''), globalError = ref('')
const globalIP = ref('')
const globalExpiry = ref('')
const globalNow = ref(Date.now())
const globalSummary = ref<GlobalIP[]>([])
const activeGlobalIPs = computed(() => globalSummary.value.filter(item => new Date(item.expires_at).getTime() > globalNow.value))
async function refreshGlobalSummary() {
  if (!me.value?.is_admin) return
  const session = me.value
  const items: GlobalIP[] = []
  let page = 1
  while (true) {
    const result = await api<{ items: GlobalIP[]; total: number }>(`/global-ips?page=${page++}&page_size=100`)
    if (me.value !== session) return
    items.push(...result.items)
    if (items.length >= result.total || !result.items.length) break
  }
  globalSummary.value = items
}
let globalClock: ReturnType<typeof setInterval> | undefined
function localTime(time: number) {
  const date = new Date(time)
  return new Date(time - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}
async function showGlobalIPs() {
  globalNow.value = Date.now()
  globalIP.value = ''; globalExpiry.value = localTime(globalNow.value + 86400000)
  globalNotice.value = ''; globalError.value = ''
  globalDialog.value?.showModal()
  await Promise.all([globalList.load(), refreshGlobalSummary()])
}
function renewGlobalIP(ip: string) {
  globalIP.value = ip; globalExpiry.value = localTime(Date.now() + 86400000)
  globalIPInput.value?.focus()
}
async function saveGlobalIP() {
  busy.value = 'global'; globalNotice.value = ''; globalError.value = ''
  try {
    await api('/global-ips', 'PUT', { ip: globalIP.value.trim(), expires_at: new Date(globalExpiry.value).toISOString() })
    globalIP.value = ''; await globalList.load(1); await refreshGlobalSummary(); globalNotice.value = msg('text.b6b7e3e9b180')
  } catch (error) { globalError.value = message(error) }
  finally { busy.value = '' }
}
async function deleteGlobalIP(ip: string) {
  busy.value = 'global'; globalNotice.value = ''; globalError.value = ''
  try {
    await api('/global-ips', 'DELETE', { ip }); await globalList.load(); await refreshGlobalSummary()
    if (globalIP.value === ip) globalIP.value = ''
    globalNotice.value = msg('text.7ce07540f8ba')
  } catch (error) { globalError.value = message(error) }
  finally { busy.value = '' }
}
const search = ref('')
const tagFilter = ref('')
const availableTags = computed(() => [...new Set(targetOptions.value.flatMap(t => t.tags || []))].sort())
const notice = ref('')
const noticeError = ref(false)
const busy = ref('')
const terminal = ref<Target | null>(null)
const machinePage = ref<InstanceType<typeof MachinePage>>()
function editFromMachine() { const target = terminal.value; if (!detached) terminal.value = null; if (target) void openEditor(target) }
const editor = ref<InstanceType<typeof TargetEditor>>()
const connectionDialog = ref<InstanceType<typeof RelayCredentials>>()
const deleteDialog = ref<HTMLDialogElement>()
const pendingDelete = ref<Target | null>(null)
const filtered = computed(() => targetList.items)
const activeConnections = ref<number | null>(null)
let activeClock: ReturnType<typeof setInterval> | undefined
let activeLoading = false
async function refreshActiveConnections() {
 if (!me.value || detached || tab.value !== 'targets' || document.hidden || activeLoading) return
 const session = me.value
 activeLoading = true
 try {
  const stats = await api<{ active: number }>('/targets/summary')
  if (me.value === session) activeConnections.value = stats.active
 } catch { if (me.value === session) activeConnections.value = null }
 finally { activeLoading = false }
}
watchPageQuery(() => [search.value, tagFilter.value], () => { if (me.value) void targetList.load(1) }, targetList.invalidate, (value, previous) => value[0] !== previous[0] ? 300 : 0)
watch(tab, value => { if (value === 'targets' && me.value) void refresh() })

function notify(message: string, error = false) { notice.value = message; noticeError.value = error }
function message(error: unknown) { return error instanceof Error ? error.message : msg('text.e113c7d13e87') }

let contextGeneration = 0
async function refresh(page = targetList.page) {
  const token = ++contextGeneration
  await Promise.all([
    targetList.load(page),
    refreshGlobalSummary(),
    (async () => {
      const [options, stats, summary] = await Promise.all([
        api<TargetOption[]>('/targets/options'), api<{ total: number; enabled: number; active: number }>('/targets/summary'),
        me.value?.is_admin ? api<MappingSummary>('/mappings/summary') : Promise.resolve(null),
      ])
      if (token !== contextGeneration || !me.value) return
      targetOptions.value = options; targetStats.value = stats; activeConnections.value = stats.active
      if (summary) mappingSummary.value = summary
    })(),
  ])
}
let creatingTarget = false

async function restore() {
  try {
    await updateDesktop()
    vault.value = await api<MasterPasswordStatus>('/security')
    if (vault.value.locked) { me.value = null; return }
    if (desktopInfo.value && (!desktopInfo.value.ready || desktopInfo.value.needs_setup)) return
    me.value = await api<Me>('/me')
    await refresh()
    if (detached) {
      terminal.value = await api<Target>(`/targets/${encodeURIComponent(detachedTargetID!)}`)
      detachedError.value = terminal.value ? '' : msg('text.39ef153fb9f5')

    }
  } catch (error) {
    if (detached) detachedError.value = message(error)
    if (me.value) notify(message(error), true)
  } finally { loading.value = false }
}

async function login() {
  busy.value = 'login'
  loginError.value = ''
  try {
    if (desktopInfo.value?.needs_setup) {
      const passwordBytes = new TextEncoder().encode(password.value).length
      if (passwordBytes < 12) throw new Error(msg('setup.adminPasswordTooShort'))
      if (passwordBytes > 72) throw new Error(msg('backend.2b1431756f24'))
      if (setupProtection.value && setupMaster.value !== setupMasterConfirm.value) throw new Error(msg('text.61153d5c7dd3'))
      await api('/desktop/setup', 'POST', { password: password.value, master_password: setupProtection.value ? setupMaster.value : '' })
      setupMaster.value = setupMasterConfirm.value = ''; await updateDesktop()
    }
    await api('/login', 'POST', { username: desktopInfo.value?.needs_setup ? 'ssh-admin' : username.value, password: password.value })
    password.value = ''
    await restore()
  } catch (error) { loginError.value = message(error) }
  finally { busy.value = '' }
}

function clearSession() {
  setupMaster.value = setupMasterConfirm.value = unlockPassword.value = ''
  terminal.value = null
  desktopDialog.value?.close()
  editor.value?.close()
  connectionDialog.value?.close()
  deleteDialog.value?.close()
  sourcesDialog.value?.close()
  copyNotice.value = ''
  globalDialog.value?.close()
  globalNotice.value = ''; globalError.value = ''
  me.value = null
  activeConnections.value = null
  globalSummary.value = []
  tab.value = 'targets'; mappingSummary.value = { total: 0, running: 0, stopped: 0, error: 0, by_target: {} }; search.value = ''; tagFilter.value = ''
  contextGeneration++; targetList.reset(); globalList.reset(); targetOptions.value = []; targetStats.value = { total: 0, enabled: 0 }; globalIP.value = ''; globalExpiry.value = ''
}

async function logout() {
  try { await api('/logout', 'POST', {}); clearSession() }
  catch (error) { notify(message(error), true) }
}

async function openTerminal(target: Target) {
  try { await openMachineWindow(target.id, currentConnection(target)) }
  catch (error) { notify(message(error), true) }
}

async function openEditor(target?: Target) {
  creatingTarget = !target
  if (me.value?.is_admin) await editor.value?.show(target)
}
async function savedTarget(result: PutResult) {
  try {
    await refresh(creatingTarget ? 1 : targetList.page)
    notify(msg('text.dc1b93ccc710'))
  } catch (error) { notify(message(error), true) }
  const target = result.target || targets.value.find(item => item.id === result.id)
  if (target && (result.credentials?.length || result.relay_password)) await showConnection(target, result.credentials?.[0]?.username)
}

async function showConnection(target: Target, username?: string) {
  await connectionDialog.value?.show(target, username, username ? undefined : currentConnection(target))
}
async function editRelayCredentials(target: Target) {
  if (me.value?.is_admin) await editor.value?.show(target, true)
}

async function askDelete(target: Target) {
  pendingDelete.value = target
  await nextTick()
  deleteDialog.value?.showModal()
}

async function remove() {
  if (!pendingDelete.value) return
  busy.value = 'delete'
  try {
    await api(`/targets/${encodeURIComponent(pendingDelete.value.id)}`, 'DELETE')
    deleteDialog.value?.close()
    editor.value?.close()
  sourcesDialog.value?.close()
  copyNotice.value = ''
  globalDialog.value?.close()
  globalNotice.value = ''; globalError.value = ''
    await refresh()
    notify(msg('text.12e9a350df94'))
  } catch (error) { notify(message(error), true) }
  finally { busy.value = '' }
}

onMounted(() => {
  activeClock = setInterval(() => { void refreshActiveConnections() }, 3000)
  globalClock = setInterval(() => { globalNow.value = Date.now() }, 1000)
  window.addEventListener('session-expired', clearSession)
  window.addEventListener('credentials-locked', credentialsLocked)
  if (native) {
    removeQuitListener = onDesktopEvent('desktop:confirm-quit', async () => {
      try {
        if (machinePage.value && !await machinePage.value.leave()) { await native!.CancelQuit(); return }
        await updateDesktop()
        if (!desktopInfo.value?.active) { await native!.ConfirmQuit(); return }
        if (!quitDialog.value?.open) quitDialog.value?.showModal()
      } catch (error) {
        await native!.CancelQuit()
        notify(message(error), true)
      }
    })
    void native.FrontendReady().catch(error => notify(message(error), true))
  }
  restore()
})
onBeforeUnmount(() => { clearTimeout(copyNoticeTimer); clearInterval(activeClock); clearInterval(globalClock); window.removeEventListener('session-expired', clearSession); window.removeEventListener('credentials-locked', credentialsLocked); removeQuitListener?.() })
watch([locale, terminal], () => { document.title = terminal.value ? t('text.076fcd4ebc2c', [terminal.value.name]) : t('text.267e3e1a658d') }, { immediate: true })
</script>

<template>
  <div v-if="!(me && terminal) && (loading || vault.locked || !me || detached)" class="entry-language"><LanguageSwitcher /></div>
  <div v-if="loading" class="loading">{{ t('text.70b429a7bba7') }}</div>
  <main v-else-if="vault.locked" class="login-page">
    <div class="login-brand"><div class="brand-symbol">&gt;_</div><span>{{ t('text.267e3e1a658d') }}</span></div>
    <form class="login-card" @submit.prevent="unlockCredentials">
      <p class="eyebrow">{{ t('text.10b3510938a3') }}</p><h1>{{ t('text.d23d6c9d746a') }}</h1>
      <p class="muted">{{ t('text.771e95da8ac0') }}</p>
      <label>{{ t('text.f36f5f11edef') }}<input v-model="unlockPassword" type="password" autocomplete="off" maxlength="1024" required autofocus /></label>
      <p v-if="unlockError || masterTransportError" class="error" role="alert">{{ display(unlockError || masterTransportError) }}</p>
      <button class="primary login-button" :disabled="!!busy || !!masterTransportError">{{ display(busy ? t('text.671b588e0774') : t('text.5f567907614e')) }}</button>
      <p class="hint">{{ t('text.cb4d3dfae5c5') }}</p>
    </form>
  </main>
  <main v-else-if="!me" class="login-page">
    <div class="login-brand"><div class="brand-symbol">&gt;_</div><span>{{ t('text.267e3e1a658d') }}</span></div>
    <div class="login-layout">
    <section class="login-intro" aria-labelledby="ai-gateway-title">
      <p class="eyebrow">{{ t('text.460758a7a011') }}</p>
      <h1 id="ai-gateway-title">{{ t('text.f7b975ba1d6c') }}<br><em>{{ t('text.5eda8dcda462') }}</em></h1>
      <p class="intro-lead">{{ t('text.cdb9f76e5eb5') }}</p>
      <ul class="intro-benefits"><li>{{ t('text.763e1af17189') }}</li><li>{{ t('text.f852fa161e1e') }}</li><li>{{ t('text.920a2071042d') }}</li></ul>
      <div class="isolation-route" :aria-label="t('text.0ef3ec02e00c')"><span>{{ t('text.c42809a87c05') }}<small>{{ t('text.038a2c5f785e') }}</small></span><b aria-hidden="true">→</b><span class="relay-node">{{ t('text.c248369eb6de') }}<small>{{ t('text.a8e1dd2890a8') }}</small></span><b aria-hidden="true">→</b><span>{{ t('text.9d39d0195c94') }}<small>{{ t('text.310f4e66ef27') }}</small></span></div>
      <p class="isolation-note"><strong>{{ t('text.463f11f09ae6') }}</strong>{{ t('text.29c965bd33c9') }}</p>
    </section>
    <form class="login-card" @submit.prevent="login">
      <p class="eyebrow">{{ t('text.267e3e1a658d') }}</p><h2>{{ display(desktopInfo?.needs_setup ? t('text.f148eb4bd82e') : t('text.8f9e1c2781ed')) }}</h2>
      <p class="muted">{{ t('text.ebbc334fde39') }}</p>
      <label v-if="!desktopInfo?.needs_setup">{{ t('text.1a3f0617d6de') }}<input v-model="username" autocomplete="username" required :placeholder="t('text.201e3f1c1245')" /></label>
      <p v-if="desktopInfo?.error" class="error" role="alert">{{ display(desktopInfo.error) }}</p><label for="admin-password">{{ display(desktopInfo?.needs_setup ? t('text.b5cbe49121fc') : username === 'ssh-admin' ? t('text.c19091521d71') : t('text.fb5bbea8d049')) }}</label>
      <input id="admin-password" v-model="password" type="password" :autocomplete="desktopInfo?.needs_setup ? 'new-password' : 'current-password'" :placeholder="t('text.739d1cc26c30')" required autofocus />
      <template v-if="desktopInfo?.needs_setup && !vault.enabled">
        <label class="checkbox-label"><input v-model="setupProtection" type="checkbox" @change="setupMaster = setupMasterConfirm = ''" />{{ t('text.c4ad8dda3ddd') }}</label>
        <template v-if="setupProtection">
          <label>{{ t('text.0a72ed1550a7') }}<input v-model="setupMaster" type="password" autocomplete="new-password" minlength="12" maxlength="1024" required /></label>
          <label>{{ t('text.12aaa082bc80') }}<input v-model="setupMasterConfirm" type="password" autocomplete="new-password" minlength="12" maxlength="1024" required /></label>
          <p class="hint">{{ t('text.0cd58a85de27') }}</p>
        </template>
      </template>
      <p v-if="loginError" class="error" role="alert">{{ display(loginError) }}</p>
      <button class="primary login-button" :disabled="!!busy || (!!desktopInfo && !desktopInfo.ready)">{{ display(busy ? t('text.7281e973958f') : desktopInfo?.needs_setup ? t('text.abf777496746') : t('text.6f0c999a5a8a')) }}</button>
      <p v-if="!native" class="hint">{{ t('text.406095a22a9e') }}</p><template v-else><p class="hint">{{ t('text.d21682ed9a10') }}{{ display(desktopInfo?.data_dir) }}</p><button v-if="!desktopInfo?.ready || desktopInfo?.needs_setup" type="button" @click="chooseDirectory">{{ t('text.8c1692e1084b') }}</button></template>
    </form>
    </div>
    <p class="login-footer">{{ t('text.b21dac507104') }}</p>
  </main>

  <main v-else-if="detached" class="loading"><p v-if="detachedError" role="alert">{{ display(detachedError) }}</p><p v-else>{{ t('text.75b2d2120793') }}</p></main>
  <div v-else class="app-layout" :inert="!!terminal">
    <aside class="sidebar">
      <div class="brand"><div class="brand-symbol">&gt;_</div><div><strong>{{ t('text.267e3e1a658d') }}</strong><small>{{ t('text.66c649904a85') }}</small></div></div>
      <div class="nav-label">{{ t('text.6fed7e860168') }}</div>
      <button class="nav-item" :class="{ active: tab === 'targets' }" @click="tab = 'targets'"><span>▤</span> {{ t('text.7f4405d07770') }} <span class="nav-count">{{ display(targetStats.total) }}</span></button>
      <button v-if="me.is_admin" class="nav-item" :class="{ active: tab === 'mappings' }" @click="tab = 'mappings'"><span>⇄</span> {{ t('text.c263674112e4') }}</button>
      <button v-if="me.is_admin" class="nav-item" :class="{ active: tab === 'users' }" @click="tab = 'users'">{{ t('text.50112fbf8b9e') }}</button>
      <div class="sidebar-bottom">
        <div :title="display(native ? t('text.76274cc3270b') : t('text.3b7c86631692'))"><span class="status-dot"></span> {{ t('text.d09ccaa70b42') }}
          <div class="muted">{{ t('text.f9e17bfdd423') }} {{ display(me.ssh_port) }}</div>
          <button v-if="me.is_admin" class="muted relay-settings-link" @click="relayEndpointSettings?.show()">{{ t('text.7b83e4b10381') }}</button>
        </div>
        <ProgramUpdate ref="programUpdate" :enabled="true" inline />
      </div>
    </aside>
    <div class="workspace">
      <header class="topbar"><span>{{ t('text.6fed7e860168') }} <span class="separator">/</span> {{ display(tab === 'targets' ? t('text.7f4405d07770') : tab === 'mappings' ? t('text.c263674112e4') : t('text.44d36735fde5')) }}</span><div class="admin-menu"><div class="language-theme-controls"><button class="text-button theme-toggle" :aria-label="display(theme === 'light' ? t('text.54e46b7d4249') : t('text.4c3a133c2cf9'))" :title="display(theme === 'light' ? t('text.54e46b7d4249') : t('text.4c3a133c2cf9'))" @click="changeTheme"><span aria-hidden="true">{{ display(theme === 'light' ? '☾' : '☀') }}</span> {{ display(theme === 'light' ? t('text.ed7d2c54184b') : t('text.f56e7eff58bf')) }}</button><LanguageSwitcher v-if="!terminal" /></div><span class="avatar">{{ display(me.is_admin ? t('text.0f9effe3a253') : t('text.04e45efb3be8')) }}</span><span>{{ display(me.is_admin ? t('text.e19796712f1c') : me.username) }}</span><div v-if="me.is_admin" class="settings-menu">
<button class="text-button menu-trigger" popovertarget="system-settings-menu" @click="positionSettings"><span>{{ t('settings.system') }}</span><svg class="menu-chevron" width="12" height="12" viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="m4 6 4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg></button>
<div id="system-settings-menu" ref="systemMenu" popover class="compact-menu" @click="systemMenu?.hidePopover()">
<button @click="securitySettings?.show()">{{ t('settings.encryption') }}</button>
<button @click="importTargets?.show()">{{ t('feature.import') }}</button>
<button @click="backupDialog?.showModal()">{{ t('feature.backup') }}</button>
</div></div><button v-if="native && me.is_admin" class="text-button" @click="openDesktopSettings">{{ t('text.108498328f97') }}</button><button class="text-button" @click="logout">{{ display(native ? t('text.3ab8cc15939f') : t('text.498e1d59b4d7')) }}</button><button v-if="native" class="text-button" @click="native.Quit()">{{ t('text.b0d269c8707c') }}</button></div></header>
      <div class="workspace-scroll">
      <main class="content"><div v-if="desktopInfo?.error" class="notice notice-error" role="alert">{{ display(desktopInfo.error) }}<button v-if="me.is_admin" @click="openDesktopSettings">{{ t('text.06af026e91b8') }}</button></div>
        <div v-if="notice" class="notice" :class="{ 'notice-error': noticeError }" role="status"><span>{{ display(notice) }}</span><button :aria-label="t('text.d301bc125833')" @click="notice = ''">×</button></div>
        <AccountWorkspace v-if="me.is_admin" :active="tab === 'users'" :targets="targetOptions" />
        <MappingWorkspace v-if="me.is_admin" ref="mappings" :theme="theme" :targets="targetOptions" :overview="tab === 'mappings' && !terminal" :summary-active="tab === 'targets' && !terminal" @change="mappingSummary = $event" />
        <div v-if="tab === 'targets'" class="page-heading"><div><h1>{{ t('text.7f4405d07770') }}</h1><p class="muted">{{ t('text.85dc6c6b1f86') }}</p></div><div v-if="me.is_admin" class="page-heading-actions"><button @click="shortcutManager?.manage()">{{ t('text.c813e695f267') }}</button><button class="primary" @click="openEditor()">{{ t('text.2715b6599085') }}</button></div></div>

        <template v-if="tab === 'targets'">
          <div class="stats"><div class="stat-card"><span>{{ t('text.431a427a0e5c') }}</span><strong>{{ display(targetStats.total) }}<small>{{ t('text.f878706308d4') }}</small></strong></div><div class="stat-card" :aria-label="t('text.d7d7735ec4cc')"><span>{{ t('text.d7d7735ec4cc') }}</span><strong>{{ display(activeConnections ?? '—') }}<small>{{ t('text.c807ed9c95f9') }}</small></strong></div><button v-if="me.is_admin" class="stat-card global-summary-card" :aria-label="t('text.e57488851025')" @click="showGlobalIPs"><span>{{ t('text.d60069ce2b44') }} <span class="badge">{{ display(activeGlobalIPs.length) }} {{ t('text.2caa7b2f2d6c') }}</span></span><strong>{{ display(activeGlobalIPs[0]?.ip || t('text.a1c2fd2c6266')) }}</strong><small>{{ t('text.71b11c26ac9c') }}</small></button></div>
          <section class="connection-list">
            <div class="list-toolbar"><div><strong>{{ t('text.dbb2c7b44300') }}</strong><span class="badge">{{ display(targetStats.total) }}</span></div><div class="connection-filters"><span class="polished-select tag-filter-select"><select v-model="tagFilter" :aria-label="t('text.99b09ae73155')"><option value="">{{ t('text.b709cb12f0ab') }}</option><option v-for="tag in availableTags" :key="tag" :value="display(tag)">{{ display(tag) }}</option></select></span><input v-model="search" class="search" :aria-label="t('text.74b5a598118b')" :placeholder="t('text.7086c3735b32')" /></div></div>
            <div v-if="targetStats.total === 0 && targetList.loaded" class="empty-state"><div class="empty-icon">&gt;_</div><h2>{{ display(me.is_admin ? t('text.f2343b3efc72') : t('text.f2e680c6c362')) }}</h2><p class="muted">{{ display(me.is_admin ? t('text.e381a908b5ba') : t('text.35170ca96dbd')) }}</p><button v-if="me.is_admin" @click="openEditor()">{{ t('text.2715b6599085') }}</button></div>
            <div v-else-if="filtered.length === 0" class="empty-state"><p>{{ t('text.7b0272977a09') }}</p><button @click="search = ''; tagFilter = ''">{{ t('text.ee32f25f7050') }}</button></div>
            <div v-else ref="targetScroll" class="table-scroll"><table><thead><tr><th>{{ t('text.9d39d0195c94') }}</th><th>{{ t('text.1d0fd5f9336d') }}</th><th>{{ t('text.6320b4a8722a') }}</th><th v-if="me.is_admin">{{ t('text.c263674112e4') }}</th><th>{{ t('text.0e99a5a8cf79') }}</th><th>{{ t('text.7977db408132') }}</th><th class="actions-heading">{{ t('text.ed31fbb483ee') }}</th></tr></thead><tbody><tr v-for="target in filtered" :key="target.id"><td><div class="target-title"><span class="server-icon">▤</span><div><strong>{{ display(target.name) }}</strong><small class="target-address">{{ display(target.host) }}<span>:{{ display(target.port) }}</span></small></div></div></td><td><div v-if="target.tags?.length" class="target-tags"><span v-for="tag in target.tags" :key="tag" class="badge">{{ display(tag) }}</span></div><span v-else class="muted">—</span></td><td><span class="status-pill" :class="{ disabled: !target.enabled }"><i></i>{{ display(target.enabled ? t('text.8a4ef3e48e4e') : t('text.bc5a87a757a5')) }}</span></td><td v-if="me.is_admin"><button class="inline-link" @click="mappings?.show(target)">{{ t('text.1e01737090c9') }} {{ display(mappingCount(target.id)) }}</button></td><td><button class="source-preview" :aria-label="display(target.name + t('text.944ed90254ca'))" @click="showSources(target)"><code>{{ display(target.allowed_sources[0] || t('text.2f5f1d6fbfb0')) }}</code><small>{{ display(target.allowed_sources.length) }} {{ t('text.27e1f477b32a') }}</small></button></td><td><span class="polished-select account-select"><select class="connection-account-select" :aria-label="display(target.name + t('text.79c3a049f169'))" :value="display(currentConnection(target))" @change="selectConnection(target, ($event.target as HTMLSelectElement).value)"><option v-for="relay in relayOptions(target)" :key="relay.id" :value="display(relay.id)" :disabled="!relayActive(relay)">{{ t('text.dc4b05496399') }} {{ display(relayLabel(target, relay)) }}</option><template v-if="me.is_admin"><option v-for="login in loginOptions(target)" :key="login.id" :value="display('server:' + login.id)">{{ t('text.939fd941988b') }} {{ display(login.user) }}</option></template></select></span><small v-if="currentConnection(target).startsWith('server:')" class="server-account-warning">{{ t('text.6d3dd5ccccf6') }}</small></td><td><div class="row-actions"><button class="primary" :disabled="!currentConnection(target).startsWith('server:') && (!target.enabled || !relayOptions(target).some(r => r.id === currentConnection(target) && relayActive(r)))" @click="connectionDialog?.copyTarget(target, currentConnection(target))">{{ display(currentConnection(target).startsWith('server:') ? t('text.872fdffc7054') : t('text.c4a5b222e09d')) }}</button><button @click="noteDialog?.show(target)">{{ t('text.b778e307964f') }}</button><button v-if="me.is_admin" @click="diagnostic?.show(target,currentConnection(target))">{{ t('feature.diagnostics') }}</button><button v-if="me.is_admin" :disabled="!!busy" @click="openEditor(target)">{{ t('text.051836569928') }}</button><button class="terminal-button" @click="openTerminal(target)">{{ t('text.aac0c509aacb') }}</button></div></td></tr></tbody></table></div>
            <p v-if="targetList.error" class="error" role="alert">{{ display(targetList.error) }} <button @click="targetList.retry">{{ t('text.b8784c8dd563') }}</button></p>
            <Pagination :label="t('text.ded17f7f73fb')" v-bind="targetList" @change="targetList.change($event, targetScroll)" />
            <div class="list-footer"><span class="status-dot"></span> {{ t('text.2ec469ec1e8b') }}</div>
          </section>
        </template>
        <footer class="workspace-footer">{{ t('text.267e3e1a658d') }} <span>{{ t('text.eac9ab31d4ea') }}</span></footer>
      </main>
      </div>
    </div>

    <TerminalQuickActions v-if="me.is_admin" ref="shortcutManager" manager-only target-id="" target-name="" :connected="false" :has-terminal="false" :can-manage="true" />
    <dialog v-if="me.is_admin" ref="globalDialog" class="global-ip-dialog" aria-labelledby="global-ip-heading">
      <div class="dialog-heading"><h2 id="global-ip-heading">{{ t('text.e57488851025') }}</h2><button class="close-button" :aria-label="t('text.6709e98b7aab')" @click="globalDialog?.close()">×</button></div>
      <div class="global-ip-body">
          <form class="dialog-content" @submit.prevent="saveGlobalIP">
            <p class="hint">{{ t('text.58429b59b235') }}</p>
            <div class="current-source-action"><span>{{ t('text.91e6675ea982') }} <code>{{ display(me.source_ip || t('text.b336a174cd1f')) }}</code></span><button type="button" :disabled="!!busy || !me.source_ip" @click="globalIP = me.source_ip; globalIPInput?.focus()">{{ t('text.58b758bcad15') }}</button></div>
            <div class="form-grid"><label>{{ t('text.4bf0843a4518') }}<input ref="globalIPInput" v-model="globalIP" required :placeholder="t('text.a6cb15d84a5d')" /></label><label>{{ t('text.ea5c831b9781') }}<input v-model="globalExpiry" type="datetime-local" required :min="localTime(globalNow + 60000)" :max="localTime(globalNow + 30 * 86400000)" /></label></div>
            <p class="hint">{{ t('text.2ec9a379c910') }}</p>
            <p v-if="globalError" class="error" role="alert">{{ display(globalError) }}</p>
            <p v-if="globalNotice" class="notice" role="status">{{ display(globalNotice) }}</p>
            <button class="primary" :disabled="!!busy">{{ t('text.b3e23c7b94c0') }}</button>
          </form>
          <p v-if="globalList.loading" class="hint global-ip-loading" role="status">{{ t('text.2d978c09b169') }}</p>
          <div ref="globalScroll" class="table-scroll"><table><thead><tr><th>{{ t('text.4bf0843a4518') }}</th><th>{{ t('text.a8e5f1716600') }}</th><th>{{ t('text.6320b4a8722a') }}</th><th>{{ t('text.ed31fbb483ee') }}</th></tr></thead><tbody><tr v-for="item in globalIPs" :key="item.ip"><td><code>{{ display(item.ip) }}</code></td><td>{{ display(formatDate(item.expires_at)) }}</td><td>{{ display(new Date(item.expires_at).getTime() > globalNow ? t('text.11afd2a53439') : t('text.2fe0e3339ac4')) }}</td><td><div class="row-actions"><button :disabled="!!busy" @click="renewGlobalIP(item.ip)">{{ t('text.4d28f8979a02') }}</button><button class="danger-text" :disabled="!!busy" @click="deleteGlobalIP(item.ip)">{{ t('text.2f9daa828907') }}</button></div></td></tr></tbody></table></div>
          <div v-if="globalList.loaded && !globalList.loading && !globalList.error && globalIPs.length === 0" class="empty-state">{{ t('text.d3eb82ac4cac') }}</div>
        <p v-if="globalList.error" class="error" role="alert">{{ display(globalList.error) }} <button @click="globalList.retry">{{ t('text.b8784c8dd563') }}</button></p><Pagination :label="t('text.f501a739b7ec')" v-bind="globalList" @change="globalList.change($event, globalScroll)" />
      </div>
      <div class="dialog-footer"><button :disabled="globalList.loading || !!busy" @click="globalList.load()">{{ t('text.42cd98937e2d') }}</button><button @click="globalDialog?.close()">{{ t('text.3fd47edce45b') }}</button></div>
    </dialog>

    <dialog v-if="me.is_admin" ref="backupDialog" class="small-dialog" :aria-label="t('feature.backup')" @close="backupPanel?.clear()">
<div class="dialog-heading"><h2>{{ t('feature.backup') }}</h2><button class="close-button" :aria-label="t('feature.close')" @click="backupDialog?.close()">×</button></div>
<div class="dialog-content"><BackupPanel ref="backupPanel" /></div>
</dialog>
<SecuritySettings v-if="me.is_admin" ref="securitySettings" @change="vault = $event" />
    <ImportTargets v-if="me.is_admin" ref="importTargets" @saved="refresh()" />
    <ConnectionDiagnostic v-if="me.is_admin" ref="diagnostic" />
    <TargetEditor ref="editor" :tags="availableTags" :source-i-p="me.source_ip" @saved="savedTarget" @delete="askDelete" />

    <RelayEndpointSettings v-if="me.is_admin" ref="relayEndpointSettings" :ssh-port="me.ssh_port" />
    <div v-if="copyNotice" class="copy-toast" role="status"><span aria-hidden="true">✓</span>{{ display(copyNotice) }}<button :aria-label="t('text.cebbe5163fcd')" @click="copyNotice = ''">×</button></div>
    <dialog ref="sourcesDialog" class="small-dialog sources-dialog" aria-labelledby="sources-heading" @close="sourcesTarget = undefined">
      <div class="dialog-heading"><div><h2 id="sources-heading">{{ t('text.0e99a5a8cf79') }}</h2><p class="hint">{{ display(sourcesTarget?.name) }} · {{ display(sourcesTarget?.allowed_sources.length || 0) }} {{ t('text.49ccde43a154') }}</p></div><button class="close-button" :aria-label="t('text.29117a3e84f4')" @click="sourcesDialog?.close()">×</button></div>
      <div class="dialog-content"><textarea class="source-details" :aria-label="t('text.685d38b63526')" :value="display(sourcesTarget?.allowed_sources.join('\n') || '')" readonly spellcheck="false" rows="10" :placeholder="t('text.99a6ec10a500')" /></div>
      <div class="dialog-footer"><button @click="sourcesDialog?.close()">{{ t('text.3fd47edce45b') }}</button></div>
    </dialog>
    <NoteDialog ref="noteDialog" />
    <RelayCredentials @copied="showCopyNotice" ref="connectionDialog" :can-edit="me.is_admin" @selection="selectConnection" @edit="editRelayCredentials" />


    <dialog v-if="native && me.is_admin" ref="desktopDialog" class="small-dialog" @close="currentAdmin = ''; newAdmin = ''">
      <div class="dialog-heading"><h2>{{ t('text.108498328f97') }}</h2><button class="close-button" :aria-label="t('text.dba0ba2ff38e')" @click="desktopDialog?.close()">×</button></div>
      <div class="dialog-content">
        <p class="hint">{{ t('text.5f76b2bf82dd') }} {{ display(desktopInfo?.version) }} {{ t('text.5873159750ac') }}{{ display(desktopInfo?.data_dir) }}</p>
        <button @click="programUpdate?.show()">{{ t('text.7f68ebad19ba') }}</button>
        <p>{{ t('text.62b258813eae') }}{{ display(desktopInfo?.listen || t('text.e6fc5eb8c3b8')) }} {{ t('text.660145f0a98c') }} {{ display(desktopInfo?.active) }}</p>
        <form @submit.prevent="saveDesktopSettings">
          <label class="checkbox-label"><input v-model="desktopSettings.external" type="checkbox" />{{ t('text.094c6ed0493b') }}</label>
          <label>{{ t('text.f9e17bfdd423') }}<input v-model.number="desktopSettings.port" type="number" min="1" max="65535" required /></label>
          <label>{{ t('text.af3bff01ccad') }}<input v-model="desktopSettings.connect_host" required :placeholder="t('text.aa6b24f0b783')" /></label>
          <p class="hint">{{ t('text.a7d58b2bf928') }}</p>
          <button class="primary" :disabled="!!busy">{{ t('text.f4d8d1fb0cb8') }}</button>
        </form>
        <h3>{{ t('text.57e1d2c6d23c') }}</h3>
        <form @submit.prevent="resetAdmin"><label>{{ t('text.6c047bb95093') }}<input v-model="currentAdmin" type="password" autocomplete="current-password" required /></label><label>{{ t('text.f1bf74cddc8f') }}<input v-model="newAdmin" type="password" autocomplete="new-password" minlength="12" maxlength="72" required /></label><button>{{ t('text.d9e4750fbbee') }}</button></form>
        <p v-if="desktopError" class="error" role="alert">{{ display(desktopError) }}</p>
      </div>
    </dialog>
    <dialog ref="deleteDialog" class="small-dialog" @close="pendingDelete = null"><div class="dialog-heading"><h2>{{ t('text.92e11347ed70') }}</h2></div><div class="dialog-content"><p>{{ t('text.9fad3beb4471') }}{{ display(pendingDelete?.name) }}{{ t('text.18f62bfaba71') }}</p></div><div class="dialog-footer"><button @click="deleteDialog?.close()">{{ t('text.2cd0f3be8738') }}</button><button class="danger" :disabled="!!busy" @click="remove">{{ t('text.a3ea3c17b401') }}</button></div></dialog>
  </div>
  <MachinePage v-if="me && terminal" ref="machinePage" :key="terminal.id" :target="terminal" :detached="detached" :selection="params.get('connection') || ''" :can-manage="me.is_admin" @close="terminal = null" @edit="editFromMachine" />
  <ProgramUpdate v-if="!detached && (loading || !me || vault.locked)" ref="programUpdate" :enabled="!!native || !!me" />
  <dialog v-if="native" ref="quitDialog" class="small-dialog" @cancel="native.CancelQuit()" @close="native.CancelQuit()">
    <div class="dialog-heading"><h2>{{ t('text.3f2097b8e990') }}</h2></div>
    <div class="dialog-content">{{ t('text.3e9f77854cb0') }}</div>
    <div class="dialog-footer"><button @click="quitDialog?.close()">{{ t('text.2cd0f3be8738') }}</button><button class="danger" @click="native.ConfirmQuit()">{{ t('text.b0d269c8707c') }}</button></div>
  </dialog>
</template>
