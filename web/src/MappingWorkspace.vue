<script setup lang="ts">
import { msg, t, display } from './i18n'
import { computed, onBeforeUnmount, onMounted, reactive, ref, useId, watch } from 'vue'
import { api, type TargetOption } from './api'
import MappingOpenLink from './MappingOpenLink.vue'
import MappingHelp from './MappingHelp.vue'
import Pagination from './Pagination.vue'
import { useServerPage, watchPageQuery, watchPageScroll } from './pagination'
import { mappingPath, type Mapping, type MappingView, type MappingList, type MappingSummary } from './mappings-api'
const props = withDefaults(defineProps<{ targets: TargetOption[]; overview?: boolean; summaryActive?: boolean; theme?: string }>(), { overview: false, theme: 'light' })
const emit = defineEmits<{ change: [summary: MappingSummary] }>()
const editorID = useId()
const localDesktop = ref(false)
const summary = ref<MappingSummary>({ total: 0, running: 0, stopped: 0, error: 0, by_target: {} })
function accept(result: MappingList) { localDesktop.value = result.local_desktop; summary.value = result.summary; emit('change', result.summary) }
const list = useServerPage<MappingView, MappingList>('/mappings', () => ({ target_id: targetFilter.value, direction: directionFilter.value, scope: scopeFilter.value, status: statusFilter.value }), accept)
const ownList = useServerPage<MappingView, MappingList>('/mappings', () => ({ target_id: currentTarget.value }), accept)
const items = computed(() => list.items)
const loaded = computed(() => list.loaded || ownList.loaded)
const tableScroll = ref<HTMLElement>(), drawerScroll = ref<HTMLElement>()
watchPageScroll(list, tableScroll)
watchPageScroll(ownList, drawerScroll)
watch(() => [ownList.total, ownList.pageSize, ownList.loading], () => {
  // 隐藏分页前确保少于 20 条的规则都在同一页，包括曾选每页 10 条的情况。
  if (ownList.loaded && !ownList.loading && !ownList.error && ownList.total < 20 && ownList.pageSize < 20) {
    void ownList.change({ page: 1, pageSize: 20 }, drawerScroll.value)
  }
})
const loadError = computed(() => list.error || ownList.error)
const notice = ref(''), formError = ref(''), busy = ref('')
const targetFilter = ref(''), directionFilter = ref(''), scopeFilter = ref(''), statusFilter = ref('')
const drawer = ref<HTMLDialogElement>(), editing = ref(false), currentTarget = ref(''), globalCreate = ref(false)
const form = reactive<Mapping>({ id: '', target_id: '', name: '', direction: 'local', service_host: '127.0.0.1', service_port: 3000, listen_port: 3000, scope: 'loopback', auto_start: false, revision: 0 })
const localSide = computed(() => localDesktop.value ? msg('text.37a2af4ddf5d') : msg('text.b6ad41550214'))
const targetName = (id: string) => props.targets.find(t => t.id === id)?.name || id
const directionName = (r: Mapping) => r.direction === 'local' ? msg('text.e8064639f37e') : msg('text.57e1e4c2dde5')
const directionHelp = (direction: string) => direction === 'local' ? msg('text.9427b896337a', [localSide.value]) : msg('text.4a42e44d8019', [localSide.value])
const listenSide = (r: Mapping) => r.direction === 'local' ? localSide.value : msg('text.9beed95cb3cf')
const serviceSide = (r: Mapping) => r.direction === 'local' ? msg('text.9beed95cb3cf') : localSide.value
const scopeName = (r: Mapping) => r.scope === 'shared' ? msg('text.a94246aafadf') : msg('text.13f8c0fad5d9', [listenSide(r)])
const endpoint = (host: string, port: number) => `${host.includes(':') ? '[' + host + ']' : host}:${port}`
const labels = { stopped: msg('text.f006455e3baf'), connecting: msg('text.a898478be181'), running: msg('text.1f0eb99b7ed0'), error: msg('text.0cf642cf998c') }
const filtered = computed(() => list.items)
const own = computed(() => ownList.items)
const stats = computed(() => [[msg('text.de8184da1ef8'), summary.value.total], [msg('text.1f0eb99b7ed0'), summary.value.running], [msg('text.f006455e3baf'), summary.value.stopped], [msg('text.428fb8bfeecf'), summary.value.error]])
const counts = (target: string) => { const c = summary.value.by_target[target]; return `${c?.running || 0} / ${c?.total || 0}` }
function resetFilters() { targetFilter.value = directionFilter.value = scopeFilter.value = statusFilter.value = '' }
function message(e: unknown) { return e instanceof Error ? e.message : msg('text.e113c7d13e87') }
let timer: ReturnType<typeof setInterval> | undefined
const listenEdited = ref(false)
async function refresh(background = false) {
  await Promise.all([
    props.overview ? (background ? list.refresh() : list.load()) : Promise.resolve(),
    !props.overview && props.summaryActive && !drawer.value?.open ? refreshSummary() : Promise.resolve(),
    drawer.value?.open && currentTarget.value ? (background ? ownList.refresh() : ownList.load()) : Promise.resolve(),
  ])
}
let summaryGeneration = 0, summaryFetching = false
async function refreshSummary() {
  if (summaryFetching) return
  summaryFetching = true
  const token = ++summaryGeneration
  try { const result = await api<MappingSummary>('/mappings/summary'); if (token === summaryGeneration) { summary.value = result; emit('change', result) } }
  catch { /* 下次可见时继续刷新汇总；列表请求独立显示错误。 */ }
  finally { summaryFetching = false }
}
async function refreshAfterWrite(firstPage = false) {
  await Promise.all([
    props.overview ? list.load(firstPage ? 1 : list.page) : Promise.resolve(),
    drawer.value?.open && currentTarget.value ? ownList.load(firstPage ? 1 : ownList.page) : Promise.resolve(),
    !props.overview && !drawer.value?.open ? refreshSummary() : Promise.resolve(),
  ])
}
function show(target: Pick<TargetOption, 'id'>) { if (currentTarget.value !== target.id) ownList.reset(); currentTarget.value = target.id; editing.value = false; globalCreate.value = false; notice.value = ''; formError.value = ''; drawer.value?.showModal(); void ownList.load() }

function configure(r?: MappingView, global = false) {
  if (r) currentTarget.value = r.target_id
  globalCreate.value = global
  Object.assign(form, { id: r?.id || '', target_id: r?.target_id || currentTarget.value || targetFilter.value || props.targets[0]?.id || '', name: r?.name || '', direction: r?.direction || (directionFilter.value === 'reverse' ? 'reverse' : 'local'), service_host: r?.service_host || '127.0.0.1', service_port: r?.service_port || 3000, listen_port: r?.listen_port || 3000, scope: r?.scope || 'loopback', auto_start: r?.auto_start || false, revision: r?.revision || 0 })
  listenEdited.value = !!r; editing.value = true; formError.value = ''; notice.value = ''; drawer.value?.showModal(); if (currentTarget.value) void ownList.load()
}
function addGlobal() { currentTarget.value = ''; configure(undefined, true) }
function close() { if (busy.value) return; editing.value = false; drawer.value?.close() }
function cancelEdit() { editing.value = false; formError.value = ''; if(globalCreate.value) drawer.value?.close() }
function changeDirection() { form.scope = 'loopback'; formError.value = '' }
function servicePortInput() { if(!listenEdited.value) form.listen_port = form.service_port }
async function save(start: boolean) {
  if (busy.value) return
  const creating = !form.id
  const old = [...items.value, ...ownList.items].find(r => r.id === form.id)
  if (old && ['running','connecting'].includes(old.status) && !window.confirm(display(msg('text.9b8f4cb64976')))) return
  busy.value = 'save'; formError.value = ''
  try {
    const saved = await api<Mapping>(form.id ? mappingPath(form.id) : '/mappings', form.id ? 'PUT' : 'POST', { ...form, name: form.name.trim(), service_host: form.service_host.trim(), service_port: Number(form.service_port), listen_port: Number(form.listen_port) })
    Object.assign(form, saved)

    if(start) { try { await api(mappingPath(saved.id) + '/start', 'POST', {}) } catch(e) { await refreshAfterWrite(creating); formError.value = msg('text.17c65f1e5cd4', [message(e)]); return } }
    editing.value = false; if(globalCreate.value) drawer.value?.close()
    await refreshAfterWrite(creating); notice.value = start ? msg('text.1ab746e88d1f') : msg('text.5e4641834340')
  } catch(e) { formError.value = message(e) }
  finally { busy.value = '' }
}
async function action(r: MappingView, op: 'start' | 'stop' | 'delete') {
  if(busy.value) return
  if(op === 'delete' && !window.confirm(display(msg('text.132cb70421e8', [r.name])))) return
  busy.value = r.id; notice.value = ''; formError.value = ''
  try { await api(mappingPath(r.id) + (op === 'delete' ? '' : '/' + op), op === 'delete' ? 'DELETE' : 'POST', {}); await refreshAfterWrite(); notice.value = op === 'start' ? msg('text.4d37c21d40d5') : op === 'stop' ? msg('text.9713081c05ca') : msg('text.2d57dd6a67cc') }
  catch(e) { notice.value = message(e) }
  finally { busy.value = '' }
}
async function copy(address: string, r: Mapping) { try { await navigator.clipboard.writeText(address); notice.value = msg('text.39007f022326', [listenSide(r), address]) } catch { notice.value = msg('text.933a359bc278', [address]) } }
watchPageQuery(() => [targetFilter.value, directionFilter.value, scopeFilter.value, statusFilter.value], () => { if (props.overview) void list.load(1) }, list.invalidate)
watch(() => props.overview, value => { if (value) void list.load() })
onMounted(() => { void refresh(); timer = setInterval(() => { if (document.visibilityState !== 'hidden' && !list.loading && !ownList.loading && !list.error && !ownList.error) void refresh(true) }, 1500) })
onBeforeUnmount(() => { summaryGeneration++; clearInterval(timer); drawer.value?.close() })
defineExpose({ show, refresh, counts })
</script>
<template>
  <section v-if="overview" class="mapping-ui" :data-theme="theme" :aria-label="t('text.ec60cd7c6ce1')">
    <header class="page-heading mapping-page-heading"><div><h1>{{ t('text.c263674112e4') }}</h1><div class="mapping-description muted"><span>{{ t('text.54d679dc7d91') }}</span><MappingHelp inline :local-side="localSide" :local-desktop="localDesktop" /></div></div><div class="page-heading-actions"><button class="primary" :disabled="!loaded || !targets.length" @click="addGlobal">{{ t('text.9ea48f1f6488') }}</button></div></header>
    <div class="mapping-stats"><div v-for="[title, count] in stats" :key="title"><small>{{ display(title) }}</small><strong>{{ display(count) }}<small>{{ t('text.f4b3a5c9d858') }}</small></strong></div></div>
    <p v-if="list.error" class="mapping-error" role="alert">{{ display(list.error) }} <button @click="list.retry">{{ t('text.b8784c8dd563') }}</button></p><p v-if="notice" class="mapping-notice" role="status">{{ display(notice) }}</p>
    <div class="mapping-panel"><div class="mapping-filters"><select v-model="targetFilter" :aria-label="t('text.393372f9ebef')"><option value="">{{ t('text.29d9802b8fac') }}</option><option v-for="t in targets" :key="t.id" :value="display(t.id)">{{ display(t.name) }}</option></select><select v-model="directionFilter" :aria-label="t('text.f90bdae76f84')"><option value="">{{ t('text.8203eb23c5f9') }}</option><option value="local">{{ t('text.e8064639f37e') }}</option><option value="reverse">{{ t('text.57e1e4c2dde5') }}</option></select><select v-model="scopeFilter" :aria-label="t('text.0183de71bee7')"><option value="">{{ t('text.6eca2f053bd8') }}</option><option value="loopback">{{ t('text.e2d4c5024c7e') }}</option><option value="shared">{{ t('text.aecac45b6906') }}</option></select><select v-model="statusFilter" :aria-label="t('text.7d271766076e')"><option value="">{{ t('text.0a379c1e7398') }}</option><option value="running">{{ t('text.1f0eb99b7ed0') }}</option><option value="stopped">{{ t('text.f006455e3baf') }}</option><option value="connecting">{{ t('text.a898478be181') }}</option><option value="error">{{ t('text.428fb8bfeecf') }}</option></select><button v-if="targetFilter || directionFilter || scopeFilter || statusFilter" @click="resetFilters">{{ t('text.657d9cbf45ec') }}</button></div>
      <div ref="tableScroll" class="mapping-table-scroll"><table class="mapping-table"><thead><tr><th>{{ t('text.c9e633b49d9e') }}</th><th>{{ t('text.d6ccc764e2a7') }}</th><th>{{ t('text.1121471a0ff4') }}</th><th>{{ t('text.ca757045f8c1') }}</th><th>{{ t('text.dd4f47c87627') }}</th><th>{{ t('text.6320b4a8722a') }}</th><th>{{ t('text.ed31fbb483ee') }}</th></tr></thead><tbody><tr v-for="r in filtered" :key="r.id"><td><strong>{{ display(r.name) }}</strong><small>{{ display(r.auto_start ? t('text.bcd8ce25f848') : t('text.465cf4bc77fc')) }}</small></td><td><button class="mapping-link" @click="show({ id: r.target_id })">{{ display(targetName(r.target_id)) }}</button></td><td :title="display(directionHelp(r.direction))">{{ display(directionName(r)) }}</td><td><code>{{ display(endpoint(r.service_host, r.service_port)) }}</code><small>{{ display(serviceSide(r)) }}</small></td><td><strong class="mapping-address">→ {{ display(r.access_addresses[0] || t('text.d257d5fabc09')) }}</strong><small>{{ display(listenSide(r)) }}{{ t('text.ca5c2325bd56') }} {{ display(r.bind_address) }}</small><span class="mapping-scope" :class="{ shared: r.scope === 'shared' }">{{ display(scopeName(r)) }}</span></td><td><span class="mapping-status" :class="r.status">● {{ display(labels[r.status]) }}</span><small v-if="r.connections">{{ display(r.connections) }} {{ t('text.f049a804462a') }}</small><small v-if="r.error" class="mapping-error-text">{{ display(r.error) }}</small><small v-if="r.warning" class="mapping-warning">{{ display(r.warning) }}</small><small v-if="r.last_error" class="mapping-error-text">{{ display(r.last_error) }}</small></td><td><div class="mapping-actions"><MappingOpenLink :mapping="r" :local-desktop="localDesktop" @error="notice = $event" /><button :disabled="!r.access_addresses.length" @click="copy(r.access_addresses[0]!, r)">{{ t('text.4c6de01bbe86') }}</button><button :disabled="!!busy" @click="configure(r)">{{ t('text.051836569928') }}</button><button :disabled="!!busy" @click="action(r, ['running','connecting'].includes(r.status) ? 'stop' : 'start')">{{ display(['running','connecting'].includes(r.status) ? t('text.ca4d973c0b00') : r.status === 'error' ? t('text.b8784c8dd563') : t('text.56410fc65314')) }}</button></div></td></tr></tbody></table></div>
      <div v-if="loaded && !filtered.length" class="mapping-empty">{{ display(summary.total ? t('text.8edce10e7400') : t('text.fc144b45fb53')) }}</div><div v-if="!loaded" class="mapping-empty">{{ display(loadError ? t('text.1c1966d71842') : t('text.ff9620fafb3d')) }}</div><Pagination :label="t('text.d64c99930a61')" v-bind="list" @change="list.change($event, tableScroll)" />
    </div>
  </section>
  <Teleport to="body"><dialog ref="drawer" class="mapping-ui mapping-dialog" :data-theme="theme" :aria-label="t('text.9980a68dde86')" @cancel.prevent="close">
    <header class="mapping-dialog-heading"><div><h2>{{ display(editing ? (form.id ? t('text.4afbbdfed7e0') : t('text.e250746cb10e')) : t('text.c263674112e4')) }}</h2><small>{{ display(globalCreate ? t('text.d74277f98f8b') : targetName(currentTarget)) }}</small></div><button :disabled="!!busy" :aria-label="t('text.d1794b83ea2d')" @click="close">×</button></header>
    <div ref="drawerScroll" class="mapping-dialog-body"><MappingHelp :local-side="localSide" :local-desktop="localDesktop" /><p v-if="ownList.error" class="mapping-error" role="alert">{{ display(ownList.error) }} <button @click="ownList.retry">{{ t('text.b8784c8dd563') }}</button></p><p v-if="notice" class="mapping-notice" role="status">{{ display(notice) }}</p>
      <template v-if="!editing"><div class="mapping-heading"><h3>{{ t('text.88bd49dad258') }} {{ display(ownList.total) }}</h3><button class="primary" :disabled="!!busy || !loaded" @click="configure()">{{ t('text.9ea48f1f6488') }}</button></div><p v-if="!own.length" class="mapping-empty">{{ t('text.c82318eb700e') }}</p>
        <article v-for="r in own" :key="r.id" class="mapping-rule"><header><div><strong>{{ display(r.name) }}</strong><small>{{ display(directionName(r)) }} · {{ display(scopeName(r)) }}</small></div><span class="mapping-status" :class="r.status">● {{ display(labels[r.status]) }}</span></header><div class="mapping-route"><div><small>{{ t('text.6631d98044d0') }} {{ display(serviceSide(r)) }}</small><code>{{ display(endpoint(r.service_host, r.service_port)) }}</code></div><span>→</span><div><small>{{ t('text.12655bbe7042') }} {{ display(listenSide(r)) }}</small><code>{{ display(r.access_addresses[0] || r.bind_address) }}</code><small>{{ t('text.faf9aa976106') }} {{ display(r.bind_address) }}</small></div></div><p v-if="r.error" class="mapping-error">{{ display(r.error) }}</p><p v-if="r.warning" class="mapping-warning">{{ display(r.warning) }}</p><p v-if="r.last_error" class="mapping-error">{{ display(r.last_error) }}</p><div class="mapping-rule-footer"><small>{{ display(r.auto_start ? t('text.bcd8ce25f848') : t('text.465cf4bc77fc')) }} · {{ display(r.connections) }} {{ t('text.f049a804462a') }}</small><div class="mapping-actions"><MappingOpenLink :mapping="r" :local-desktop="localDesktop" @error="notice = $event" /><button :disabled="!r.access_addresses.length" @click="copy(r.access_addresses[0]!, r)">{{ t('text.4c6de01bbe86') }}</button><button :disabled="!!busy" @click="configure(r)">{{ t('text.051836569928') }}</button><button :disabled="!!busy" @click="action(r, ['running','connecting'].includes(r.status) ? 'stop' : 'start')">{{ display(['running','connecting'].includes(r.status) ? t('text.ca4d973c0b00') : t('text.56410fc65314')) }}</button><button class="mapping-delete" :disabled="!!busy" @click="action(r, 'delete')">{{ t('text.2f9daa828907') }}</button></div></div></article>
        <Pagination v-if="ownList.total >= 20" :label="t('text.26dd57acef37')" v-bind="ownList" @change="ownList.change($event, drawerScroll)" />
      </template>
      <form v-else :id="editorID" class="mapping-editor" @submit.prevent="save(($event as SubmitEvent).submitter?.getAttribute('value') === 'start')"><fieldset :disabled="!!busy"><div class="mapping-section-title">{{ t('text.e8df05872569') }}<small>{{ t('text.526a208d2801') }}</small></div><label v-if="globalCreate">{{ t('text.d6ccc764e2a7') }}<select v-model="form.target_id" required><option value="" disabled>{{ t('text.cbf02cab259f') }}</option><option v-for="t in targets" :key="t.id" :value="display(t.id)">{{ display(t.name) }} · {{ display(t.host) }}</option></select></label><label>{{ t('text.d44e9b3d3b31') }}<input v-model="form.name" required maxlength="60" :placeholder="t('text.e0f36dee5346')"></label><div class="mapping-directions"><label><input v-model="form.direction" type="radio" value="local" @change="changeDirection">{{ t('text.e8064639f37e') }}<small>{{ t('text.04e8168400b0') }} {{ display(localSide) }}</small></label><label><input v-model="form.direction" type="radio" value="reverse" @change="changeDirection">{{ t('text.57e1e4c2dde5') }}<small>{{ display(localSide) }}{{ t('text.5b5cee8f399e') }}</small></label></div>
        <div class="mapping-endpoints"><section class="mapping-endpoint"><h4>{{ t('text.ca757045f8c1') }} <span>{{ display(serviceSide(form)) }}</span></h4><div class="mapping-grid"><label>{{ display(form.direction === 'local' ? t('text.b9f974bff9f0') : t('text.de948cb2b193')) }}<input v-model="form.service_host" required maxlength="253"></label><label>{{ display(form.direction === 'local' ? t('text.bf16658de34f') : t('text.0da6457e7638')) }}<input v-model.number="form.service_port" type="number" required min="1" max="65535" step="1" @input="servicePortInput"></label></div><p class="mapping-hint">{{ t('text.2ad87071ba1c') }}{{ display(serviceSide(form)) }}{{ t('text.5e80b19c7ab0') }}</p>
        </section><section class="mapping-endpoint mapping-entry"><h4>{{ t('text.09b1f6d310c0') }} <span>{{ display(listenSide(form)) }}</span></h4><div class="mapping-grid"><label>{{ display(form.direction === 'local' ? t('text.b932aaccfb51') : t('text.178f4da1fab0')) }}<input v-model.number="form.listen_port" type="number" required min="1" max="65535" step="1" @input="listenEdited = true"></label><label>{{ t('text.b3e8912ac6e1') }}<select v-model="form.scope" :aria-label="t('text.b3e8912ac6e1')"><option value="loopback">{{ t('text.8c8e27463551') }}{{ display(listenSide(form)) }}{{ t('text.274675465b32') }}</option><option value="shared">{{ t('text.a94246aafadf') }}</option></select></label></div><p class="mapping-hint">{{ display(form.scope === 'loopback' ? t('text.70db2f84d080') : t('text.e22e5768cc8a')) }}</p><p v-if="form.direction === 'reverse'" class="mapping-warning">{{ t('text.362a180719e0') }}</p></section></div><label class="mapping-check"><input v-model="form.auto_start" type="checkbox">{{ display(localDesktop ? t('text.63c73c4730f4') : t('text.d09ccaa70b42')) }}{{ t('text.fff9daf699f3') }}</label><p class="mapping-preview"><strong>{{ t('text.c5ec907524b0') }}</strong>{{ display(serviceSide(form)) }} {{ display(endpoint(form.service_host, form.service_port)) }} → {{ display(listenSide(form)) }}{{ t('text.e71ac32b544b') }} {{ display(form.listen_port) }}<small>{{ display(scopeName(form)) }}</small></p><p v-if="formError" class="mapping-error" role="alert">{{ display(formError) }}</p></fieldset></form>
    </div><footer v-if="editing" class="mapping-form-footer mapping-dialog-actions"><button :disabled="!!busy" type="button" @click="cancelEdit">{{ t('text.2cd0f3be8738') }}</button><button :disabled="!!busy" type="submit" :form="editorID" value="save">{{ t('text.0adada7d466c') }}</button><button :disabled="!!busy" type="submit" :form="editorID" value="start" class="primary">{{ display(busy === 'save' ? t('text.ff509c9ba052') : t('text.fc990e978179')) }}</button></footer><footer class="mapping-dialog-footer">{{ t('text.8f6f5d8e09d6') }}{{ display(localDesktop ? t('text.b0d269c8707c') : t('text.a619969b3ce3')) }}{{ t('text.fb1ae6229f9a') }}</footer>
  </dialog></Teleport>
</template>
<style>
.mapping-ui{--mp-bg:#f6f8f7;--mp-card:#fff;--mp-text:#20312f;--mp-muted:#75877f;--mp-border:#dde7e1;--mp-tint:#edf6f1;color:var(--mp-text);font-size:13px;line-height:1.6}.mapping-ui[data-theme=dark]{--mp-bg:#17232b;--mp-card:#20303a;--mp-text:#deebe6;--mp-muted:#9eafa7;--mp-border:#36484d;--mp-tint:#243e38}.mapping-ui button,.mapping-ui input,.mapping-ui select{font:inherit;color:var(--mp-text);background:var(--mp-card);border:1px solid var(--mp-border);border-radius:7px;padding:8px 11px}.mapping-ui button.primary{color:white;background:#0c796d;border-color:#0c796d}.mapping-ui button:disabled{opacity:.5}.mapping-ui input,.mapping-ui select{width:100%;min-width:0}.mapping-ui label{display:flex;flex-direction:column;gap:6px;font-size:12px}.mapping-ui small{color:var(--mp-muted);font-size:11px}.mapping-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:22px}.mapping-page-heading{font-size:14px;line-height:1.55}.mapping-page-heading .primary{font-weight:600}.mapping-heading p{margin:0;color:var(--mp-muted)}.mapping-heading button{white-space:nowrap}.mapping-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:15px;margin-bottom:20px}.mapping-stats>div{padding:18px 20px;border:1px solid var(--mp-border);background:var(--mp-card);border-radius:9px}.mapping-stats strong{display:block;font-size:27px}.mapping-stats strong small{margin-left:10px;font-weight:400}.mapping-panel{border:1px solid var(--mp-border);border-radius:10px;background:var(--mp-card);overflow:hidden}.mapping-filters{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:9px;padding:16px}.mapping-filters select,.mapping-filters button{height:32px;font-size:12px;line-height:1.5;padding-top:6px;padding-bottom:6px}.mapping-filters select{width:130px}.mapping-table-scroll{overflow:auto}.mapping-table{min-width:1100px;width:100%;border-collapse:collapse;white-space:normal}.mapping-table th,.mapping-table td{padding:16px 13px;border-bottom:1px solid var(--mp-border);background:var(--mp-card);font-size:12px}.mapping-table th{background:var(--mp-bg)}.mapping-table strong,.mapping-table small{display:block;overflow-wrap:anywhere}.mapping-table td:nth-child(1){max-width:160px}.mapping-table td:nth-child(6){max-width:190px}.mapping-ui code{font-family:ui-monospace,monospace;font-size:12px;color:var(--mp-text);overflow-wrap:anywhere}.mapping-address{color:#0c8b79;font-family:ui-monospace,monospace;white-space:nowrap}.mapping-ui .mapping-link{padding:0;border:0;background:transparent;color:#0c8b79;text-align:left}.mapping-scope{display:inline-block;border-radius:4px;padding:2px 6px;background:var(--mp-tint);font-size:10px;margin-top:4px}.mapping-scope.shared{background:#fff0d8;color:#94602a}.mapping-status{display:inline-block;white-space:nowrap;background:var(--mp-bg);color:var(--mp-muted);font-size:11px;border-radius:5px;padding:3px 7px}.mapping-status.running{background:#e9f7ee;color:#207c52}.mapping-status.error{background:#ffede7;color:#b54e38}.mapping-status.connecting{background:#fff2d9;color:#8c6829}.mapping-actions{display:flex;gap:6px;flex-wrap:wrap}.mapping-actions .mapping-open{border:1px solid var(--mp-border);border-radius:7px;background:var(--mp-card);color:#0c8b79;text-decoration:none}.mapping-actions .mapping-open:hover{background:var(--mp-tint)}.mapping-open-disabled{display:inline-flex;cursor:help}.mapping-open-disabled button{pointer-events:none}.mapping-actions button,.mapping-actions .mapping-open{padding:5px 8px;white-space:nowrap;font-size:11px}.mapping-ui .mapping-error-text{color:#b44d3d}.mapping-error{padding:10px 12px;background:#fff0eb;border-radius:7px;color:#a34434}.mapping-warning{font-size:12px;color:#9d7037}.mapping-notice{padding:10px 12px;background:var(--mp-tint);border-radius:7px;overflow-wrap:anywhere}.mapping-empty{text-align:center;padding:35px 16px;color:var(--mp-muted)}.mapping-count{padding:12px 16px;color:var(--mp-muted);font-size:11px}.mapping-hint{font-size:12px;color:var(--mp-muted);margin:10px 0 18px}.mapping-dialog{border:1px solid var(--mp-border);border-radius:14px;margin:auto;padding:0;width:860px;max-width:calc(100vw - 32px);height:auto;max-height:90vh;max-height:90dvh;overflow:hidden;background:var(--mp-bg);box-shadow:0 24px 80px #10251f33}.mapping-dialog[open]{display:flex;flex-direction:column}.mapping-dialog::backdrop{background:#152e2855}.mapping-dialog-heading{flex-shrink:0;padding:22px 26px;display:flex;justify-content:space-between;gap:15px;border-bottom:1px solid var(--mp-border);background:var(--mp-card)}.mapping-dialog-heading small{display:block;margin-top:4px}.mapping-dialog-heading button{font-size:23px;border:0;padding:0 8px}.mapping-dialog-body{min-height:0;overflow:auto;padding:22px 26px;flex:1}.mapping-dialog-footer{flex-shrink:0;padding:13px 26px;background:var(--mp-card);border-top:1px solid var(--mp-border);font-size:11px;color:var(--mp-muted)}.mapping-rule,.mapping-editor{background:var(--mp-card);border:1px solid var(--mp-border);border-radius:9px;padding:18px;margin-bottom:13px}.mapping-rule header{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.mapping-rule header small{display:block;margin-top:4px}.mapping-route{display:grid;grid-template-columns:1fr auto 1fr;gap:12px;margin:17px 0;align-items:center}.mapping-route small{display:block}.mapping-rule-footer{border-top:1px solid var(--mp-border);padding-top:12px;display:flex;justify-content:space-between;gap:10px;align-items:center}.mapping-ui .mapping-delete{color:#b14b3d}.mapping-editor fieldset{border:0;padding:0;margin:0;min-width:0}.mapping-editor label{margin:12px 0}.mapping-editor h3{margin:0 0 12px}.mapping-editor h4{margin:16px 0 10px;border-top:1px solid var(--mp-border);padding-top:13px;font-size:13px}.mapping-directions{display:grid;grid-template-columns:1fr 1fr;gap:12px}.mapping-directions label{display:block;border:1px solid var(--mp-border);border-radius:7px;padding:12px;cursor:pointer}.mapping-directions label:has(input:checked){background:var(--mp-tint);border-color:#0c796d}.mapping-directions small{display:block;margin-top:5px}.mapping-ui input[type=radio],.mapping-ui input[type=checkbox]{width:auto;accent-color:#0c796d}.mapping-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.mapping-grid label{margin:0}.mapping-editor .mapping-check{flex-direction:row;align-items:center;font-weight:400}.mapping-preview{padding:12px;background:var(--mp-tint);border-radius:7px;overflow-wrap:anywhere}.mapping-preview small{display:block}.mapping-form-footer{display:flex;justify-content:flex-end;gap:8px}.mapping-ui h2{color:var(--mp-text)}@media(max-width:800px){.mapping-stats{grid-template-columns:1fr 1fr;gap:10px}.mapping-stats>div{padding:13px}.mapping-dialog-heading,.mapping-dialog-body{padding:18px}.mapping-dialog-footer{flex-shrink:0;padding:12px 18px}.mapping-rule-footer{align-items:flex-start;flex-direction:column}.mapping-filters select{flex:1;min-width:130px}.mapping-directions{gap:8px}.mapping-directions label{padding:10px}.mapping-route{gap:8px}}

.mapping-dialog-heading h2{font-size:20px;font-weight:650}
.mapping-editor{padding:22px;margin-bottom:0}
.mapping-section-title{font-size:14px;font-weight:650;margin-bottom:14px}.mapping-section-title small{display:block;font-weight:400;margin-top:3px}
.mapping-directions{margin:18px 0 22px}.mapping-directions label{font-weight:600;margin:0}.mapping-directions label:has(input:checked){box-shadow:inset 0 0 0 1px #0c796d}.mapping-directions small{font-weight:400}
.mapping-endpoints{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:18px}.mapping-endpoint{min-width:0;border:1px solid var(--mp-border);border-radius:9px;padding:16px;background:var(--mp-bg)}.mapping-entry{border-top:3px solid #438f7e;padding-top:14px}
.mapping-editor .mapping-endpoint h4{display:flex;align-items:center;justify-content:space-between;gap:8px;border:0;padding:0;margin:0 0 16px;font-size:14px}.mapping-endpoint h4 span{font-size:11px;font-weight:500;color:var(--mp-muted)}
.mapping-endpoint .mapping-grid{grid-template-columns:1fr}.mapping-endpoint .mapping-hint{margin-bottom:0}.mapping-entry input{font-weight:600}.mapping-endpoint .mapping-warning{margin:10px 0 0}
.mapping-preview{border-left:3px solid #438f7e;padding:13px 15px;line-height:1.8}.mapping-preview>strong{display:block;font-size:12px;margin-bottom:3px}.mapping-form-footer{border-top:1px solid var(--mp-border);padding-top:16px;margin-top:18px}
.mapping-dialog-actions{flex-shrink:0;margin:0;padding:14px 26px;background:var(--mp-card)}
@media(max-width:600px){.mapping-dialog-actions{padding:12px 16px}.mapping-dialog{max-width:calc(100vw - 20px);max-height:94vh;max-height:94dvh;border-radius:12px}.mapping-dialog-heading,.mapping-dialog-body{padding:16px}.mapping-editor{padding:16px}.mapping-endpoints{grid-template-columns:1fr}.mapping-form-footer{flex-wrap:wrap}.mapping-directions{gap:8px}}
</style>

<style scoped>
.mapping-description{display:flex;align-items:baseline;flex-wrap:wrap;gap:4px 8px}
</style>
