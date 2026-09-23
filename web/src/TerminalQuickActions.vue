<script setup lang="ts">
import { msg, t, display } from './i18n'
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { api, type TargetOption } from './api'
import Pagination from './Pagination.vue'
import './machine.css'
import { useServerPage, watchPageQuery, watchPageScroll, type PageResult } from './pagination'
const props = defineProps<{ targetId: string; targetName: string; connected: boolean; hasTerminal: boolean; canManage: boolean; managerOnly?: boolean }>()
const emit = defineEmits<{ send: [command: string]; clear: [] }>()
interface Shortcut { id: string; name: string; command: string; targetId: string; tags: string[] }
const tagFilter = ref(''), tagSearch = ref(''), selectedTags = ref<string[]>([])
const search = ref(''), scopeFilter = ref(''), formOpen = ref(false)
const list = useServerPage<Shortcut>('/shortcuts', () => ({ q: search.value.trim(), target_id: scopeFilter.value.startsWith('target:') ? scopeFilter.value.slice(7) : '', scope: scopeFilter.value === 'tags' ? 'tags' : '', tag: tagFilter.value }))
const visibleShortcuts = ref<Shortcut[]>([]), buttonsError = ref('')
let buttonsGeneration = 0
async function loadButtons() {
  if (props.managerOnly) return
  const generation = ++buttonsGeneration
  const items = new Map<string, Shortcut>()
  try {
    for (let page = 1; ; page++) {
      const result = await api<PageResult<Shortcut>>(`/shortcuts?applicable_to=${encodeURIComponent(props.targetId)}&page=${page}&page_size=100`)
      if (generation !== buttonsGeneration) return
      for (const item of result.items) items.set(item.id, item)
      if (result.page * result.page_size >= result.total) break
    }
    visibleShortcuts.value = [...items.values()]
    buttonsError.value = ''
  } catch (error) {
    if (generation !== buttonsGeneration) return
    visibleShortcuts.value = []
    buttonsError.value = error instanceof Error ? error.message : msg('text.3a459a468cf1')
  }
}
function scrollCommands(event: WheelEvent) {
  const element = buttonScroll.value
  if (!element || Math.abs(event.deltaX) >= Math.abs(event.deltaY)) return
  const before = element.scrollLeft
  element.scrollLeft += event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? element.clientWidth : 1)
  if (element.scrollLeft !== before) event.preventDefault()
}
const pageItems = computed(() => list.items)
const tableScroll = ref<HTMLElement>(), buttonScroll = ref<HTMLElement>()
watchPageScroll(list, tableScroll)
const selectedTarget = ref(props.targetId)
const scopeType = ref<'machine' | 'tags' | 'global'>('machine')
const editorDialog = ref<HTMLDialogElement>(), formError = ref('')
function closeEditor() { if (!busy.value) editorDialog.value?.close() }
const targets = ref<TargetOption[]>([])
const tagOptions = computed(() => [...new Set(targets.value.flatMap(target => target.tags || []))].sort())
const editTagOptions = computed(() => [...new Set([...tagOptions.value, ...selectedTags.value])].filter(tag => tag.toLocaleLowerCase().includes(tagSearch.value.trim().toLocaleLowerCase())))
function toggleTag(tag: string) { selectedTags.value = selectedTags.value.includes(tag) ? selectedTags.value.filter(value => value !== tag) : [...selectedTags.value, tag] }
function itemScope(item: Shortcut) { return item.targetId === '' ? msg('text.462ff3d23d59') + item.tags.join('、') : scopeName(item.targetId) }
const filterTargets = computed(() => targets.value.map(target => target.id))
const targetsError = ref(''), loadingTargets = ref(false)
const dialog = ref<HTMLDialogElement>()
const editing = ref(''), name = ref(''), command = ref(''), error = ref(''), busy = ref(false)
function scopeName(id: string) { if (id === '*') return msg('text.7569582505bd'); return `${targets.value.find(t => t.id === id)?.name || (id === props.targetId ? props.targetName : id)}（${id}）` }
async function edit(item?: Shortcut) {
  if (!props.canManage) return
  formOpen.value = true; editing.value = item?.id || ''; name.value = item?.name || ''; command.value = item?.command || ''; selectedTarget.value = item?.targetId ?? props.targetId; selectedTags.value = [...(item?.tags || [])]; tagSearch.value = ''; formError.value = ''; scopeType.value = (item?.targetId === '*' || (!item && !props.targetId)) ? 'global' : item?.targetId === '' ? 'tags' : 'machine'
  await nextTick(); editorDialog.value?.showModal(); editorDialog.value?.querySelector<HTMLInputElement>('input[data-command-name]')?.focus()
}
async function save() {
  if (busy.value || !props.canManage) return
  if (scopeType.value === 'tags' && !selectedTags.value.length) { formError.value = msg('text.025e2de5cc57'); return }
  busy.value = true; formError.value = ''
  try {
    const updating = !!editing.value
    await api(updating ? `/shortcuts/${encodeURIComponent(editing.value)}` : '/shortcuts', updating ? 'PUT' : 'POST', { name: name.value.trim(), command: command.value.trim(), targetId: scopeType.value === 'global' ? '*' : scopeType.value === 'tags' ? '' : selectedTarget.value, tags: scopeType.value === 'tags' ? selectedTags.value : [] })
    editorDialog.value?.close()
    await Promise.all([list.load(updating ? list.page : 1), loadButtons()])
  } catch (e) { formError.value = e instanceof Error ? e.message : msg('text.c4699a549596') }
  finally { busy.value = false }
}
async function remove(item: Shortcut) {
  if (busy.value || !props.canManage) return
  busy.value = true; error.value = ''
  try {
    await api(`/shortcuts/${encodeURIComponent(item.id)}`, 'DELETE')
    if (editing.value === item.id) formOpen.value = false
    await Promise.all([list.load(), loadButtons()])
  } catch (e) { error.value = e instanceof Error ? e.message : msg('text.630704b848c4') }
  finally { busy.value = false }
}
async function manage() { if (!props.canManage) return; formOpen.value = false; dialog.value?.showModal(); await Promise.all([list.load(), loadTargets()]) }
async function loadTargets() {
  targetsError.value = ''; loadingTargets.value = true
  try { targets.value = await api<TargetOption[]>('/targets/options') }
  catch { targetsError.value = msg('text.806e41f4287a') }
  finally { loadingTargets.value = false }
}
function send(item: Shortcut) { if (props.connected && visibleShortcuts.value.some(visible => visible.id === item.id)) emit('send', item.command) }
function refreshVisible() { if (document.visibilityState === 'hidden') return; void loadButtons(); if (dialog.value?.open) void list.load() }
watchPageQuery(() => [search.value, scopeFilter.value, tagFilter.value], () => { if (dialog.value?.open) void list.load(1) }, list.invalidate, (value, previous) => value[0] !== previous[0] ? 300 : 0)
watch(scopeType, scope => { if (scope === 'machine' && (!selectedTarget.value || selectedTarget.value === '*')) selectedTarget.value = props.targetId || targets.value[0]?.id || '' })
watch(() => props.targetId, () => { visibleShortcuts.value = []; if (buttonScroll.value) buttonScroll.value.scrollLeft = 0; void loadButtons() })
onMounted(() => { void loadButtons(); window.addEventListener('focus', refreshVisible) })
onBeforeUnmount(() => { buttonsGeneration++; window.removeEventListener('focus', refreshVisible) })
defineExpose({ manage })
</script>
<template>
  <div :class="managerOnly ? 'shortcut-manager-host' : 'terminal-quick-actions'" :role="managerOnly ? undefined : 'region'" :aria-label="display(managerOnly ? undefined : t('text.8557ed01c44d'))">
    <template v-if="!managerOnly">
    <div class="terminal-quick-heading">
      <span class="terminal-quick-title">{{ t('text.899432d83d98') }}</span>
      <div ref="buttonScroll" class="terminal-quick-list" tabindex="0" :aria-label="t('text.1d54908f7ed0')" @wheel="scrollCommands">
        <button
          v-for="item in visibleShortcuts"
          :key="item.id"
          :title="display(item.command)"
          :disabled="!connected"
          @click="send(item)"
        >
          {{ display(item.name) }}
        </button>
        <span v-if="!visibleShortcuts.length" class="empty-hint"
          >{{ display(canManage ? t('text.d32e3ed9e6dc') : t('text.c363cbf0be3f')) }}</span
        >
      </div>
      <button
        :title="t('text.cbb1ee8019f7')"
        :disabled="!hasTerminal"
        @click="emit('clear')"
      >
        {{ t('text.9f695490d448') }}
      </button>
      <button v-if="canManage" @click="manage">{{ t('text.507d34e2efed') }}</button>
    </div>
    <p v-if="buttonsError" class="tree-error" role="alert">{{ display(buttonsError) }} <button @click="loadButtons">{{ t('text.b8784c8dd563') }}</button></p>
    <p v-if="error && !dialog?.open" class="tree-error" role="alert">
      {{ display(error) }}
    </p>
    </template>
    <dialog
      ref="dialog"
      class="machine-dialog terminal-shortcut-dialog"
      :aria-label="t('text.c0e0c06b6177')"
    >
      <header>
        <h3>{{ t('text.c0e0c06b6177') }}</h3>
        <button :aria-label="t('text.daeee57ed6d1')" @click="dialog?.close()">
          ×
        </button>
      </header>
      <div class="shortcut-filters">
        <input
          v-model="search"
          :aria-label="t('text.4c9074ec4bd9')"
          :placeholder="t('text.d2ec85d48cbb')"
        />
        <select v-model="scopeFilter" :aria-label="t('text.041d45a2ad0c')">
          <option value="">{{ t('text.5281db22b254') }}</option>
          <option value="target:*">{{ t('text.58ae07f79f4e') }}</option><option value="tags">{{ t('text.ab38586d9819') }}</option>
          <option v-for="id in filterTargets" :key="id" :value="display(`target:${id}`)">
            {{ display(scopeName(id)) }}
          </option>
        </select>
        <select v-model="tagFilter" :aria-label="t('text.ad8f68fce3f4')"><option value="">{{ t('text.b709cb12f0ab') }}</option><option v-for="tag in tagOptions" :key="tag" :value="display(tag)">{{ display(tag) }}</option></select>
        <button class="primary" @click="edit()">{{ t('text.8c9ab187bdd5') }}</button>
      </div>
      <p v-if="targetsError" class="tree-error" role="alert">
        {{ display(targetsError) }}
        <button type="button" @click="loadTargets">{{ t('text.b8784c8dd563') }}</button>
      </p>
      <p v-if="error" class="tree-error" role="alert">{{ display(error) }}</p><p v-if="list.error" class="tree-error" role="alert">{{ display(list.error) }} <button @click="list.retry">{{ t('text.b8784c8dd563') }}</button></p>
      <div ref="tableScroll" class="shortcut-table-scroll">
        <table class="shortcut-table" :aria-label="t('text.7c29bb085442')">
          <colgroup>
            <col class="shortcut-name-col" />
            <col />
            <col class="shortcut-scope-col" />
            <col class="shortcut-actions-col" />
          </colgroup>
          <thead>
            <tr>
              <th>{{ t('text.d44e9b3d3b31') }}</th>
              <th>{{ t('text.928f87d4507b') }}</th>
              <th>{{ t('text.40781b5ae18a') }}</th>
              <th>{{ t('text.ed31fbb483ee') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="item in pageItems"
              :key="item.id"
              :class="{ 'is-editing': formOpen && editing === item.id }"
            >
              <td :title="display(item.name)">{{ display(item.name) }}</td>
              <td :title="display(item.command)">
                <code>{{ display(item.command) }}</code>
              </td>
              <td :title="display(itemScope(item))">
                {{ display(itemScope(item)) }}
              </td>
              <td class="shortcut-row-actions">
                <button
                  :aria-label="display(t('text.2d308436ce5f', [item.name]))"
                  :disabled="busy" @click="edit(item)"
                >
                  {{ t('text.051836569928') }}</button
                ><button
                  :aria-label="display(t('text.d2fa0bfde601', [item.name]))"
                  :disabled="busy" @click="remove(item)"
                >
                  {{ t('text.2f9daa828907') }}
                </button>
              </td>
            </tr>
            <tr v-if="!list.items.length">
              <td colspan="4" class="shortcut-no-results">
                {{
                  display(search || scopeFilter || tagFilter ? t('text.556513743902') : t('text.018b8ef076a5'))
                }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination :label="t('text.2b1c425d7d7f')" v-bind="list" @change="list.change($event, tableScroll)" />
    </dialog>
    <dialog
      ref="editorDialog"
      class="machine-dialog shortcut-editor-dialog"
      :aria-label="display(editing ? t('text.f9de3d7740bc') : t('text.37f3d43b2db6'))"
      @close="formOpen = false"
      @cancel="busy && $event.preventDefault()"
    >
      <form v-if="formOpen" class="shortcut-edit-form" @submit.prevent="save">
        <header class="shortcut-form-heading">
          <h3>{{ display(editing ? t('text.9ea7f032d0f3') : t('text.ec4f1ea3c569')) }}</h3>
          <button type="button" :aria-label="t('text.44677cc687bb')" :disabled="busy" @click="closeEditor">×</button>
        </header>
        <div class="shortcut-editor-fields">
          <label>{{ t('text.d44e9b3d3b31') }}<input v-model="name" data-command-name :aria-label="t('text.838910be8554')" maxlength="24" required :disabled="busy" /></label>
          <label>{{ t('text.928f87d4507b') }}<input v-model="command" :aria-label="t('text.04bf2a9f2a47')" maxlength="2000" required :placeholder="t('text.d6e913bf8d46')" :disabled="busy" /></label>
          <fieldset class="shortcut-scope-picker" :disabled="busy">
            <legend>{{ t('text.40781b5ae18a') }}</legend>
            <div class="shortcut-scope-types">
              <label><input v-model="scopeType" type="radio" value="machine" name="shortcut-scope" />{{ t('text.ece969c3f881') }}</label>
              <label><input v-model="scopeType" type="radio" value="tags" name="shortcut-scope" />{{ t('text.1d0fd5f9336d') }}</label>
              <label><input v-model="scopeType" type="radio" value="global" name="shortcut-scope" />{{ t('text.63d6b47116de') }}</label>
            </div>
            <select v-if="scopeType === 'machine'" v-model="selectedTarget" :aria-label="t('text.ffe8d9cf30a7')" required :disabled="loadingTargets">
              <option v-for="target in targets" :key="target.id" :value="display(target.id)">{{ display(target.name) }}（{{ display(target.id) }}）{{ display(target.id === targetId ? t('text.bbfaded05415') : '') }}</option>
              <option v-if="selectedTarget && !targets.some(target => target.id === selectedTarget)" :value="display(selectedTarget)">{{ t('text.eb5bb50951fe') }}{{ display(selectedTarget) }}）</option>
            </select>
            <div v-else-if="scopeType === 'tags'" class="shortcut-tag-field">
              <input v-model="tagSearch" :aria-label="t('text.c705d188e4d0')" :placeholder="t('text.25498b867893')" />
              <div class="shortcut-tag-options" role="group" :aria-label="t('text.17d46799e687')">
                <button v-for="tag in editTagOptions" :key="tag" type="button" class="tag-choice" :aria-pressed="selectedTags.includes(tag)" @click="toggleTag(tag)">{{ display(selectedTags.includes(tag) ? '✓ ' : '') }}{{ display(tag) }}</button>
                <small v-if="!editTagOptions.length">{{ display(tagOptions.length ? t('text.f6ebb7f7944b') : t('text.f689e048e692')) }}</small>
              </div>
              <small>{{ t('text.eed27fb48ad0') }}<span v-if="selectedTags.length"> {{ t('text.5f48778c33be') }} {{ display(selectedTags.length) }} {{ t('text.49ccde43a154') }}</span></small>
            </div>
            <small v-else class="shortcut-scope-hint">{{ t('text.0b9c522fef7f') }}</small>
          </fieldset>
          <p v-if="targetsError && scopeType !== 'global'" class="tree-error" role="alert">{{ display(targetsError) }} <button type="button" :disabled="busy" @click="loadTargets">{{ t('text.b8784c8dd563') }}</button></p>
          <p v-if="formError" class="tree-error" role="alert">{{ display(formError) }}</p>
        </div>
        <footer>
          <button type="button" :disabled="busy" @click="closeEditor">{{ t('text.2cd0f3be8738') }}</button>
          <button class="primary" type="submit" :disabled="busy">{{ display(busy ? t('text.ff509c9ba052') : editing ? t('text.937740052e90') : t('text.d5fb4378c479')) }}</button>
        </footer>
      </form>
    </dialog>
  </div>
</template>
