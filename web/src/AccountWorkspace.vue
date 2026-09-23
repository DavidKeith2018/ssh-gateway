<script setup lang="ts">
import { msg, t, display } from './i18n'
import { computed, reactive, ref, watch } from 'vue'
import { api, type Account, type TargetOption } from './api'
import Pagination from './Pagination.vue'
import { useServerPage, watchPageScroll } from './pagination'
const props = defineProps<{ targets: TargetOption[]; active: boolean }>()
const list = useServerPage<Account>('/users')
const tableScroll = ref<HTMLElement>()
watchPageScroll(list, tableScroll)
const users = computed(() => list.items)
const dialog = ref<HTMLDialogElement>()
const selected = ref<Account | null>(null)
const error = ref(''), pageError = ref(''), busy = ref(false)
const form = reactive({ username: '', password: '', enabled: true, target_ids: [] as string[], tags: [] as string[] })
const tags = computed(() => [...new Set([...props.targets.flatMap(t => t.tags || []), ...form.tags])].sort())
const targetName = (id: string) => props.targets.find(t => t.id === id)?.name || id
async function refresh() { await list.load() }
function edit(user: Account | null) {
  selected.value = user; error.value = ''
  Object.assign(form, { username: user?.username || '', password: '', enabled: user?.enabled ?? true, target_ids: [...user?.target_ids || []], tags: [...user?.tags || []] })
  dialog.value?.showModal()
}
async function save() {
  busy.value = true; error.value = ''
  try {
    await api(selected.value ? `/users/${selected.value.id}` : '/users', selected.value ? 'PUT' : 'POST', form)
    dialog.value?.close(); form.password = ''; await list.load(selected.value ? list.page : 1)
  } catch (e) { error.value = e instanceof Error ? e.message : msg('text.6309a3bb5ba4') }
  finally { busy.value = false }
}
async function toggle(user: Account) {
  busy.value = true
  try { await api(`/users/${user.id}`, 'PUT', { username: user.username, enabled: !user.enabled, target_ids: user.target_ids, tags: user.tags }); await refresh() }
  catch (e) { pageError.value = e instanceof Error ? e.message : msg('text.ec99e5c45d64') }
  finally { busy.value = false }
}
async function remove(user: Account) {
  if (!window.confirm(display(msg('text.5b81a1dfd7f6', [user.username])))) return
  busy.value = true
  try { await api(`/users/${user.id}`, 'DELETE'); await refresh() }
  catch (e) { pageError.value = e instanceof Error ? e.message : msg('text.c228558cf257') }
  finally { busy.value = false }
}
watch(() => props.active, active => { if (active) void refresh() }, { immediate: true })
</script>
<template>
  <section v-show="active">
    <div class="page-heading"><div><h1>{{ t('text.44d36735fde5') }}</h1><p class="muted">{{ t('text.0b7beb93f661') }}</p></div><button class="primary" @click="edit(null)">{{ t('text.3e7eecaeba23') }}</button></div>
    <p v-if="pageError" class="error" role="alert">{{ display(pageError) }}</p>
    <div class="connection-list"><p v-if="list.error" class="error" role="alert">{{ display(list.error) }} <button @click="list.retry">{{ t('text.b8784c8dd563') }}</button></p><div ref="tableScroll" class="table-scroll"><table><thead><tr><th>{{ t('text.1a3f0617d6de') }}</th><th>{{ t('text.6320b4a8722a') }}</th><th>{{ t('text.38295990e2ba') }}</th><th>{{ t('text.0267279d0704') }}</th><th>{{ t('text.ed31fbb483ee') }}</th></tr></thead><tbody>
      <tr v-for="user in users" :key="user.id"><td>{{ display(user.username) }}</td><td>{{ display(user.enabled ? t('text.dfb802238b38') : t('text.bc5a87a757a5')) }}</td><td>{{ display(user.target_ids.map(targetName).join('、') || t('text.484d55613910')) }}</td><td>{{ display(user.tags.join('、') || t('text.484d55613910')) }}</td><td><div class="row-actions"><button :disabled="busy" @click="edit(user)">{{ t('text.e85e8aa6e889') }}</button><button :disabled="busy" @click="toggle(user)">{{ display(user.enabled ? t('text.7df5c456c765') : t('text.f4f0ead1116b')) }}</button><button :disabled="busy" class="danger-text" @click="remove(user)">{{ t('text.2f9daa828907') }}</button></div></td></tr>
    </tbody></table><p v-if="!users.length" class="empty-state">{{ t('text.8c8ac5e34c7d') }}</p></div><Pagination :label="t('text.2a054d53ac37')" v-bind="list" @change="list.change($event, tableScroll)" /></div>
    <dialog ref="dialog" class="editor-dialog" @close="form.password = ''">
      <form @submit.prevent="save">
        <div class="dialog-heading"><h2>{{ display(selected ? t('text.02a033b76e9f') : t('text.390b7b32fa39')) }}</h2><button type="button" :aria-label="t('text.885ced9590b3')" @click="dialog?.close()">×</button></div>
        <div class="dialog-content">
          <label>{{ t('text.1a3f0617d6de') }}<input v-model="form.username" :disabled="!!selected" required maxlength="64" pattern="[a-zA-Z0-9][a-zA-Z0-9_.\-]{0,63}" autocomplete="off" /></label>
          <label>{{ display(selected ? t('text.d70928debc2e') : t('text.df387080f986')) }}<input v-model="form.password" type="password" autocomplete="new-password" :required="!selected" minlength="12" maxlength="72" :placeholder="display(selected ? t('text.046e993ec43e') : t('text.e50d90e33cb8'))" /></label>
          <label class="checkbox-label"><input v-model="form.enabled" type="checkbox" />{{ t('text.504d0de808ab') }}</label>
          <h3>{{ t('text.38295990e2ba') }}</h3><div class="grant-options"><label v-for="target in targets" :key="target.id" class="checkbox-label"><input v-model="form.target_ids" type="checkbox" :value="display(target.id)" />{{ display(target.name) }}</label></div><p v-if="!targets.length" class="hint">{{ t('text.5fb4df10eca5') }}</p>
          <h3>{{ t('text.31bfad67d301') }}</h3><div class="grant-options"><label v-for="tag in tags" :key="tag" class="checkbox-label"><input v-model="form.tags" type="checkbox" :value="display(tag)" />{{ display(tag) }}</label></div><p v-if="!tags.length" class="hint">{{ t('text.979e91d4868f') }}</p>
          <p class="hint">{{ t('text.32c40a743b0a') }}</p>
          <p v-if="error" class="error" role="alert">{{ display(error) }}</p>
        </div>
        <div class="dialog-footer"><button type="button" @click="dialog?.close()">{{ t('text.2cd0f3be8738') }}</button><button class="primary" :disabled="busy">{{ t('text.c45cbe8e5068') }}</button></div>
      </form>
    </dialog>
  </section>
</template>
<style scoped>
.grant-options { display:flex; flex-wrap:wrap; gap:12px 24px; max-height:240px; overflow:auto }
</style>
