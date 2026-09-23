<script setup lang="ts">
import { msg, t, display } from './i18n'
import ChevronIcon from './ChevronIcon.vue'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { api, type Target, type TargetLogin, type TargetRelay, type PutResult } from './api'
import { randomID } from './random-id'

const props = defineProps<{ tags: string[]; sourceIP: string }>()
const emit = defineEmits<{ saved: [result: PutResult]; delete: [target: Target] }>()
type Login = TargetLogin & { passwordVisible: boolean; revealedPassword: string; keyVisible: boolean; revealedKey: string; passphraseVisible: boolean; revealedPassphrase: string; target_password: string; target_private_key: string; target_key_passphrase: string; saved_type: string; importVersion: number }
type Relay = TargetRelay & { password: string; automatic: boolean; saved: boolean; expires: string; expiryPreset: number | null }
const dialog = ref<HTMLDialogElement>()
const revealingPassword = ref(false)
let revealVersion = 0
type SecretKind = 'password' | 'key' | 'passphrase'
const secretFields = {
 password: { visible: 'passwordVisible', revealed: 'revealedPassword', draft: 'target_password', response: 'password' },
 key: { visible: 'keyVisible', revealed: 'revealedKey', draft: 'target_private_key', response: 'private_key' },
 passphrase: { visible: 'passphraseVisible', revealed: 'revealedPassphrase', draft: 'target_key_passphrase', response: 'passphrase' },
} as const
async function toggleSecret(login: Login, kind: SecretKind) {
 const field = secretFields[kind]
 if (login[field.visible]) {
  login[field.visible] = false; login[field.revealed] = ''
  return
 }
 const version = ++revealVersion
 revealingPassword.value = true; error.value = ''
 try {
  let value = login[field.draft]
  const savedType = kind === 'password' ? 'password' : 'private_key'
  if (!value && editing.value && login.saved_type === savedType) {
   const result = await api<{ credential: Record<string, string> }>(`/targets/${encodeURIComponent(form.id)}/logins/${encodeURIComponent(login.id)}/credentials`, 'POST', {})
   value = result.credential[field.response] || ''
  }
  if (version !== revealVersion || !dialog.value?.open) return
  if (!login[field.draft]) login[field.revealed] = value
  login[field.visible] = true
 } catch {
  if (version === revealVersion) error.value = msg('editor.showSecretFailed')
 } finally {
  if (version === revealVersion) revealingPassword.value = false
 }
}
function hideSecrets(login: Login) {
 login.passwordVisible = login.keyVisible = login.passphraseVisible = false
 login.revealedPassword = login.revealedKey = login.revealedPassphrase = ''
}

const formElement = ref<HTMLFormElement>()
const originalTarget = ref<Target>()
const editing = ref(false), busy = ref(false), probing = ref(false), error = ref('')
const tagInput = ref(''), pickingTags = ref(false), activeLogin = ref('')
const relaySection = ref(false), sourceSection = ref(true), fingerprintSection = ref(false)
const form = reactive({ id: '', revision: 0, name: '', host: '', port: 22, enabled: true, tags: [] as string[], logins: [] as Login[], relays: [] as Relay[], default_login_id: '', host_fingerprint: '', sources: '' })
watch(activeLogin, () => {
 revealVersion++; revealingPassword.value = false
 form.logins.forEach(hideSecrets)
})
const privateSources = '10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16\n127.0.0.0/8'
const selectedDefault = computed(() => form.logins.find(login => login.id === form.default_login_id))
const tagOptions = computed(() => [...new Set([...props.tags, ...form.tags])].filter(tag => !tagInput.value || tag.includes(tagInput.value)))
const validRelays = computed(() => form.relays.filter(relay => relay.enabled).length)
function makeLogin(login?: TargetLogin): Login {
  return { passwordVisible: false, revealedPassword: '', keyVisible: false, revealedKey: '', passphraseVisible: false, revealedPassphrase: '', id: login?.id || randomID(), user: login?.user || '', auth_type: login?.auth_type || 'password', target_password: '', target_private_key: '', target_key_passphrase: '', saved_type: login?.auth_type || '', importVersion: 0 }
}
function setExpiry(relay: Relay, hours: number) { relay.expiryPreset = hours; relay.expires = hours ? new Date(Date.now()+hours*3600000).toISOString() : '' }
function makeRelay(loginID: string, relay?: TargetRelay): Relay {
  return { id: relay?.id || randomID(), username: relay?.username || '', login_id: relay?.login_id || loginID, enabled: relay?.enabled ?? true, password: '', automatic: !relay, saved: !!relay, expires: relay?.expires_at || '', expiryPreset: relay?.expires_at ? null : 0 }
}
async function show(target?: Target, showRelays = false) {
  clearSecrets()
  originalTarget.value = target
  editing.value = !!target
  const logins = (target?.logins?.length ? target.logins : target ? [{ id: 'default', user: target.user, auth_type: target.auth_type }] : []).map(makeLogin)
  if (!logins.length) logins.push(makeLogin())
  const defaultID = target?.default_login_id || logins[0]!.id
  const relays = target?.relays?.length ? target.relays.map(relay => makeRelay(defaultID, relay)) : [makeRelay(defaultID, target ? { id: 'default', username: target.relay_user, login_id: defaultID, enabled: true, expires_at: target.relay_expires_at } : undefined)]
  Object.assign(form, { id: target?.id || '', revision: target?.revision || 0, name: target?.name || '', host: target?.host || '', port: target?.port || 22, enabled: target?.enabled ?? true, tags: [...target?.tags || []], logins, relays, default_login_id: defaultID, host_fingerprint: target?.host_fingerprint || '', sources: target ? target.allowed_sources.join('\n') : privateSources })
  activeLogin.value = logins[0]!.id
  tagInput.value = ''; pickingTags.value = false; error.value = ''
  sourceSection.value = true
  fingerprintSection.value = false
  relaySection.value = showRelays
  await nextTick(); dialog.value?.showModal()
}
function clearLogin(login: Login) { revealVersion++; revealingPassword.value = false; hideSecrets(login); login.importVersion++; login.target_password = ''; login.target_private_key = ''; login.target_key_passphrase = '' }
function clearSecrets() { form.logins.forEach(clearLogin); form.relays.forEach(relay => { relay.password = '' }) }
function close() { if (!busy.value) { dialog.value?.close(); clearSecrets() } }
function addLogin() {
  const login = makeLogin(); form.logins.push(login); activeLogin.value = login.id
}
function removeLogin(login: Login) {
  if (form.relays.some(relay => relay.login_id === login.id)) {
    relaySection.value = true; error.value = msg('text.8781cd68e08b'); return
  }
  clearLogin(login); form.logins = form.logins.filter(item => item.id !== login.id)
  if (form.default_login_id === login.id) form.default_login_id = form.logins[0]!.id
  activeLogin.value = form.logins[0]!.id; error.value = ''
}
const hasCurrentSource = computed(() => form.sources.split(/[\s,，]+/).includes(props.sourceIP))
function addCurrentSource() {
  if (!props.sourceIP || hasCurrentSource.value) return
  form.sources = [props.sourceIP, ...form.sources.split(/[\s,，]+/).filter(Boolean)].join('\n')
}
function toggleTag(tag: string) { form.tags = form.tags.includes(tag) ? form.tags.filter(item => item !== tag) : [...form.tags, tag] }
function addTag() {
  const tag = tagInput.value.trim()
  if (!tag) return
  if (!form.tags.includes(tag)) form.tags.push(tag)
  tagInput.value = ''
}
async function importKey(event: Event, login: Login) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]
  input.value = ''
  if (!file) return
  const version = ++login.importVersion
  try {
    if (file.size > 32 * 1024) throw new Error(msg('text.1c70eb03cfff'))
    const key = await file.text()
    if (version !== login.importVersion || !dialog.value?.open) return
    login.target_private_key = key; login.target_key_passphrase = ''
  } catch (e) { if (version === login.importVersion) error.value = message(e) }
}
function message(e: unknown) { return e instanceof Error ? e.message : msg('text.e113c7d13e87') }
async function probe() {
  probing.value = true; error.value = ''; fingerprintSection.value = true
  const host = form.host, port = Number(form.port)
  try {
    const result = await api<{ fingerprint: string }>('/probe', 'POST', { host, port })
    if (host === form.host && port === Number(form.port) && dialog.value?.open) form.host_fingerprint = result.fingerprint
  } catch (e) { error.value = message(e) }
  finally { probing.value = false }
}
function revealInvalid(input: HTMLElement) {
  const login = input.closest<HTMLElement>('[data-login-id]')?.dataset.loginId
  if (login) activeLogin.value = login
  if (input.closest('[data-relays]')) relaySection.value = true
  if (input.closest('[data-sources]')) sourceSection.value = true
}
async function save() {
  if (busy.value || probing.value) return
  error.value = ''; addTag()
  if (!form.host_fingerprint) { fingerprintSection.value = true; error.value = msg('text.a749b9ef7169'); return }
  const invalidField = formElement.value?.querySelector<HTMLElement>('input:invalid, textarea:invalid, select:invalid')
  if (invalidField) { revealInvalid(invalidField); await nextTick(); formElement.value?.reportValidity(); invalidField.focus(); return }
  busy.value = true
  try {
    const result = await api<PutResult>(editing.value ? `/targets/${encodeURIComponent(form.id)}` : '/targets', editing.value ? 'PUT' : 'POST', {
      id: form.id, revision: form.revision, name: form.name, host: form.host, port: Number(form.port), enabled: form.enabled, tags: form.tags,
      default_login_id: form.default_login_id, host_fingerprint: form.host_fingerprint, source_mode: 'custom', allowed_sources: form.sources.split(/[\s,，]+/).filter(Boolean),
      logins: form.logins.map(({ id, user, auth_type, target_password, target_private_key, target_key_passphrase }) => ({ id, user, auth_type, target_password, target_private_key, target_key_passphrase })),
      relays: form.relays.map(relay => ({ id: relay.id, username: relay.automatic ? '' : relay.username, login_id: relay.login_id, enabled: relay.enabled, expires_at: relay.expires ? new Date(relay.expires).toISOString() : null, password: relay.automatic ? '' : relay.password })),
    })
    dialog.value?.close(); clearSecrets(); emit('saved', result)
  } catch (e) { error.value = message(e) }
  finally { busy.value = false }
}
defineExpose({ show, close })
</script>

<template>
  <dialog ref="dialog" class="editor-dialog target-editor" aria-labelledby="target-editor-title" @close="clearSecrets" @cancel="busy && $event.preventDefault()">
    <form ref="formElement" novalidate @submit.prevent="save">
      <header class="target-editor-header">
        <div><h2 id="target-editor-title">{{ display(editing ? t('text.c49d0350ec2b') : t('text.160dd26a0238')) }}</h2><p>{{ t('text.d09e1069e2b8') }}</p></div>
        <label class="editor-switch"><input v-model="form.enabled" type="checkbox" role="switch" :aria-label="t('text.25195ebe3359')" />{{ display(form.enabled ? t('text.878ff320ae17') : t('text.2e35cad050f5')) }}</label>
        <button v-if="originalTarget" type="button" class="danger-text editor-delete" :disabled="busy || probing" @click="emit('delete', originalTarget)">{{ t('text.2f9daa828907') }}</button>
        <button type="button" class="close-button" :aria-label="t('text.64280f0409a6')" :disabled="busy" @click="close">×</button>
      </header>
      <div class="target-editor-body">
        <div class="editor-column">
          <section class="editor-block" :aria-label="t('text.309dcbeed129')">
            <div class="editor-section-heading"><h3>{{ t('text.309dcbeed129') }}</h3></div>
            <div class="editor-fields"><label>{{ t('text.d44e9b3d3b31') }}<input v-model="form.name" required maxlength="100" :placeholder="t('text.56b804603622')" /></label><label class="port-field">{{ t('text.1c22cc57aee8') }}<input v-model.number="form.port" type="number" min="1" max="65535" required @change="form.host_fingerprint = ''" /></label></div>
            <label>{{ t('text.9a2aca2f99cd') }}<input v-model="form.host" required :placeholder="t('text.99c485452eec')" @input="form.host_fingerprint = ''" /></label>
            <div class="tag-picker">
              <div class="editor-section-heading"><span>{{ t('text.dd81be2f32d7') }} <small>{{ t('text.537352400c49') }}</small></span><button type="button" class="editor-link" :aria-expanded="pickingTags" @click="pickingTags = !pickingTags">{{ t('text.e530f798e6f4') }}</button></div>
              <div class="tag-input-box"><button v-for="tag in form.tags" :key="tag" type="button" class="tag-chip" :aria-label="display(t('text.efbf2ea99be9', [tag]))" @click="toggleTag(tag)">{{ display(tag) }} <span>×</span></button><input v-model="tagInput" :aria-label="t('text.dd81be2f32d7')" :placeholder="t('text.86bd596d65b9')" maxlength="100" @keydown.enter.prevent="addTag" /><button v-if="tagInput.trim()" type="button" class="editor-link" @click="addTag">{{ t('text.7a8a11ead507') }}</button></div>
              <div v-if="pickingTags" class="tag-options" role="group" :aria-label="t('text.2c7484d2c7ca')"><button v-for="tag in tagOptions" :key="tag" type="button" class="tag-choice" :aria-pressed="form.tags.includes(tag)" @click="toggleTag(tag)">{{ display(form.tags.includes(tag) ? '✓ ' : '') }}{{ display(tag) }}</button><small v-if="!tagOptions.length">{{ t('text.17d387a2a365') }}</small></div>
            </div>
          </section>
          <section class="editor-fold" data-sources>
            <button class="fold-toggle" type="button" :aria-expanded="sourceSection" @click="sourceSection = !sourceSection"><span>{{ t('text.b3e8912ac6e1') }}</span><small>{{ display(form.sources.split(/[\s,，]+/).filter(Boolean).length) }} {{ t('text.49ccde43a154') }}</small><ChevronIcon :direction="sourceSection ? 'up' : 'down'" /></button>
            <div v-show="sourceSection" class="fold-body"><label><span>{{ t('text.a2c22e596f1e') }} <small class="muted">{{ t('text.a44b76d1e3cd') }}</small></span><textarea v-model="form.sources" rows="6" required :placeholder="t('text.3987872d13c5')" /></label><div class="current-source-action"><span>{{ t('text.91e6675ea982') }} <code>{{ display(sourceIP || t('text.b336a174cd1f')) }}</code></span><button type="button" :disabled="busy || !sourceIP || hasCurrentSource" @click="addCurrentSource">{{ display(hasCurrentSource ? t('text.889839915c3f') : t('text.a8b4aa1e8c97')) }}</button></div><p class="hint">{{ t('text.5992981160e4') }}</p></div>
          </section>
          <section class="editor-fold">
            <div class="fingerprint-summary"><button class="fold-toggle" type="button" :aria-expanded="fingerprintSection" @click="fingerprintSection = !fingerprintSection"><span>{{ t('text.e78509a5f05a') }}</span><small :class="{ 'needs-input': !form.host_fingerprint }">{{ display(form.host_fingerprint ? t('text.fbf7fc3ed9ec') : t('text.24ae77da351a')) }}</small><ChevronIcon :direction="fingerprintSection ? 'up' : 'down'" /></button><button type="button" class="editor-link" :disabled="probing || busy || !form.host" @click="probe">{{ display(probing ? t('text.346ff60e6c7c') : t('text.b578cbb09126')) }}</button></div>
            <div v-show="fingerprintSection" class="fold-body"><label>{{ t('text.392f6d0403c2') }}<input v-model="form.host_fingerprint" placeholder="SHA256:…" /></label><p class="hint">{{ t('text.cfcc8d4e7ca5') }}</p></div>
          </section>
        </div>
        <div class="editor-column">
          <section class="editor-block" :aria-label="t('text.396582e8fbfd')">
            <div class="editor-section-heading"><h3>{{ t('text.4c74e040c922') }} <span class="editor-count">{{ display(form.logins.length) }}</span></h3><button type="button" class="editor-link" :disabled="form.logins.length >= 16 || busy" @click="addLogin">{{ t('text.79de59fa7f15') }}</button></div>
            <section v-for="(login, index) in form.logins" :key="login.id" class="login-card-editor" :class="{ 'is-active': activeLogin === login.id }" :data-login-id="login.id" :aria-label="display(t('text.53c35465ebea', [index + 1]))">
              <div class="login-summary"><button type="button" class="login-toggle" :aria-expanded="activeLogin === login.id" @click="activeLogin = activeLogin === login.id ? '' : login.id"><span class="login-avatar">{{ display(index + 1) }}</span><strong>{{ display(login.user || t('text.3bf93765ff0d')) }}</strong><small v-if="form.default_login_id === login.id">{{ t('text.844b8cc8dff7') }}</small><ChevronIcon :direction="activeLogin === login.id ? 'up' : 'down'" /></button><button v-if="form.logins.length > 1" type="button" class="editor-link danger-text" :aria-label="display(t('text.391ca5d9165a', [index + 1]))" @click="removeLogin(login)">×</button></div>
              <div v-show="activeLogin === login.id" class="login-body">
                <div class="editor-fields equal-fields"><label>{{ t('text.79322d7ec7f4') }}<input v-model="login.user" required maxlength="128" autocomplete="off" :placeholder="t('text.3f9fd09a549d')" /></label><label>{{ t('text.d1ff8f98bc3f') }}<select v-model="login.auth_type" @change="clearLogin(login)"><option value="password">{{ t('text.5c4ddb326e0d') }}</option><option value="private_key">{{ t('text.45ff0431f689') }}</option></select></label></div>
                <div v-if="login.auth_type === 'password'" class="editor-password-row"><label>{{ t('text.42ceea448f22') }}<input :value="login.target_password || login.revealedPassword" :type="login.passwordVisible ? 'text' : 'password'" @input="login.target_password = ($event.target as HTMLInputElement).value; login.revealedPassword = ''" autocomplete="new-password" :required="login.saved_type !== 'password'" :placeholder="display(login.saved_type === 'password' ? t('text.fd0b779b22c4') : t('text.c9c58b95c113'))" /></label><button type="button" :disabled="busy || revealingPassword || (!login.target_password && (!editing || login.saved_type !== 'password'))"  :aria-pressed="login.passwordVisible" class="secret-toggle" :aria-label="t(login.passwordVisible ? 'editor.hidePassword' : 'editor.showPassword')" :title="t(login.passwordVisible ? 'editor.hidePassword' : 'editor.showPassword')" @click="toggleSecret(login, 'password')"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/><path v-if="login.passwordVisible" d="m3 3 18 18"/></svg></button></div>
                <template v-else>
                  <div class="editor-password-row private-key-row"><label>{{ t('text.9c73b9717a07') }}
                    <textarea v-if="login.keyVisible" :value="login.target_private_key || login.revealedKey" rows="3" autocomplete="off" :spellcheck="false" :required="login.saved_type !== 'private_key'" @input="login.target_private_key = ($event.target as HTMLTextAreaElement).value; login.revealedKey = ''; login.importVersion++" />
                    <input v-else type="password" readonly :value="login.target_private_key ? '********' : ''" :placeholder="display(login.saved_type === 'private_key' ? t('text.208f089de5b9') : t('editor.showKeyToEdit'))" autocomplete="off" />
                  </label><button type="button" class="secret-toggle" :disabled="busy || revealingPassword" :aria-pressed="login.keyVisible" :aria-label="t(login.keyVisible ? 'editor.hideKey' : 'editor.showKey')" :title="t(login.keyVisible ? 'editor.hideKey' : 'editor.showKey')" @click="toggleSecret(login, 'key')"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/><path v-if="login.keyVisible" d="m3 3 18 18"/></svg></button></div>
                  <div class="key-file-row"><label class="key-file-button">{{ t('text.69e7bc724c7a') }}<input type="file" @change="importKey($event, login)" /></label><small>{{ t('text.17a60fb23ba7') }}</small></div>
                  <div class="editor-password-row"><label>{{ t('text.0212b3f97253') }}<input :value="login.target_key_passphrase || login.revealedPassphrase" :type="login.passphraseVisible ? 'text' : 'password'" @input="login.target_key_passphrase = ($event.target as HTMLInputElement).value; login.revealedPassphrase = ''" autocomplete="new-password" :placeholder="t('text.844e73e913b3')" /></label><button type="button" class="secret-toggle" :disabled="busy || revealingPassword" :aria-pressed="login.passphraseVisible" :aria-label="t(login.passphraseVisible ? 'editor.hidePassphrase' : 'editor.showPassphrase')" :title="t(login.passphraseVisible ? 'editor.hidePassphrase' : 'editor.showPassphrase')" @click="toggleSecret(login, 'passphrase')"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/><path v-if="login.passphraseVisible" d="m3 3 18 18"/></svg></button></div>
                </template>
                <label v-if="form.logins.length > 1" class="inline-checkbox"><input v-model="form.default_login_id" type="radio" :value="display(login.id)" :aria-label="display(t('text.a65f262b43ed', [login.user || t('text.9b7df81c876f', [index + 1])]))" />{{ t('text.e6ef500bf381') }}</label>
              </div>
            </section>
            <p class="hint default-login-hint">{{ t('text.c38d5fb50ecd') }}{{ display(selectedDefault?.user ? `：${selectedDefault.user}` : '') }}。</p>
          </section>
          <section class="editor-fold" data-relays>
            <button type="button" class="fold-toggle" :aria-expanded="relaySection" @click="relaySection = !relaySection"><span>{{ t('text.bdbd8139d9f5') }} <span class="editor-count">{{ display(form.relays.length) }}</span></span><small>{{ display(form.relays.every(relay => relay.automatic) ? t('text.c9c72e385af4') : t('text.638edd197fef', [validRelays])) }}</small><ChevronIcon :direction="relaySection ? 'up' : 'down'" /></button>
            <div v-show="relaySection" class="fold-body">
              <section v-for="(relay, index) in form.relays" :key="relay.id" class="relay-card-editor" :aria-label="display(t('text.3ceca3c48b4f', [index + 1]))">
                <div class="editor-section-heading"><strong>{{ display(form.logins.find(login => login.id === relay.login_id)?.user || t('text.09edd0b653ce', [index + 1])) }} {{ t('text.6b562fed8c03') }}</strong><label class="inline-checkbox"><input v-model="relay.enabled" type="checkbox" :aria-label="display(t('text.65f24e768f15', [index + 1]))" />{{ t('text.11afd2a53439') }}</label><button v-if="form.relays.length > 1" type="button" class="editor-link danger-text" :aria-label="display(t('text.14069bf124f6', [index + 1]))" @click="form.relays = form.relays.filter(item => item.id !== relay.id)">{{ t('text.2f9daa828907') }}</button></div>
                <label>{{ t('text.6ba45719551f') }}<select v-model="relay.login_id" required><option v-for="(login, loginIndex) in form.logins" :key="login.id" :value="display(login.id)">{{ display(login.user || t('text.21ce81dfefce', [loginIndex + 1])) }}</option></select></label>
                <div class="relay-expiry-row" :title="relay.expires || t('feature.permanent')">
                  <span>{{ t('feature.expiryLabel') }}</span>
                  <div class="relay-expiry-options" role="group" :aria-label="t('feature.expiryLabel')">
                    <button v-for="hours in [0,1,24]" :key="hours" type="button" :aria-pressed="relay.expiryPreset === hours" @click="setExpiry(relay,hours)">{{ t(hours === 0 ? 'feature.expiryForever' : hours === 1 ? 'feature.expiryHour' : 'feature.expiryDay') }}</button>
                  </div>
                </div>
                <p class="hint">{{ t('feature.expiryHint') }}</p>
                <label v-if="!relay.saved" class="inline-checkbox"><input v-model="relay.automatic" type="checkbox" />{{ t('text.3029e1729171') }}</label>
                <p v-if="relay.automatic" class="hint">{{ t('text.07042dfff6ba') }}</p>
                <div v-else class="editor-fields equal-fields"><label>{{ t('text.768a29eca4b8') }}<input v-model="relay.username" required pattern="[a-zA-Z0-9][a-zA-Z0-9_.\-]{0,63}" :placeholder="t('text.4da31b43ca6e')" /></label><label>{{ t('text.5c8a25fb92b0') }}<input v-model="relay.password" type="password" autocomplete="new-password" minlength="12" maxlength="72" :placeholder="display(relay.saved ? t('text.3fa725f3b3b6') : t('text.47d95068555b'))" /></label></div>
              </section>
              <button type="button" class="add-relay" :disabled="form.relays.length >= 32 || busy" @click="form.relays.push(makeRelay(form.default_login_id))">{{ t('text.6d922006d2bd') }}</button>
            </div>
          </section>
        </div>
      </div>
      <footer class="target-editor-footer"><p v-if="error" class="error" role="alert">{{ display(error) }}</p><div class="editor-footer-row"><span>{{ t('text.aa8c7ac8293c') }}</span><button type="button" :disabled="busy" @click="close">{{ t('text.2cd0f3be8738') }}</button><button class="primary" :disabled="busy || probing">{{ display(busy ? t('text.ff509c9ba052') : t('text.f5dede740ee0')) }}</button></div></footer>
    </form>
  </dialog>
</template>

<style scoped>
.target-editor{width:min(960px,calc(100vw - 32px));max-height:min(820px,calc(100dvh - 40px));overflow:hidden}.target-editor form{display:flex;flex-direction:column;max-height:inherit}.target-editor-header{display:flex;align-items:center;gap:16px;padding:19px 24px;border-bottom:1px solid #e5ebe8}.target-editor-header h2{font-size:19px}.target-editor-header p{font-size:12px;color:#7b8c85;margin:5px 0 0}.editor-switch{margin-left:auto;flex-direction:row;align-items:center;white-space:nowrap;font-size:12px;gap:7px}.target-editor-body{display:grid;grid-template-columns:1fr 1.05fr;gap:24px;padding:22px 24px;overflow-y:auto;min-height:0}.editor-column{min-width:0;display:flex;flex-direction:column;gap:12px}.editor-block{min-width:0}.editor-section-heading{display:flex;gap:10px;align-items:center;justify-content:space-between;margin-bottom:10px;font-size:12px}.editor-section-heading h3{margin:0;font-size:14px}.editor-section-heading small,.editor-section-heading span small{font-weight:400;color:#8b9992;font-size:11px}.editor-fields{display:grid;grid-template-columns:minmax(0,1fr) 94px;gap:12px;margin-bottom:10px}.equal-fields{grid-template-columns:1fr 1fr}.target-editor label{gap:5px;font-size:12px}.target-editor input,.target-editor textarea,.target-editor select{padding:8px 10px;font:inherit;font-size:12px;min-width:0}.target-editor select{width:100%;background:#fff;border:1px solid #d8e1df;border-radius:7px;color:#20312f}.target-editor select:focus-visible{outline:2px solid #16887a}.editor-link{padding:1px 0;border:0;background:transparent;color:#0c796d;font-size:11px;white-space:nowrap}.editor-link.danger-text{color:#b84b4b}.tag-picker{margin-top:15px}.tag-input-box{display:flex;align-items:center;flex-wrap:wrap;gap:5px;border:1px solid #d8e1df;border-radius:7px;padding:5px 8px;min-height:38px}.tag-input-box input{width:100px;flex:1;border:0;box-shadow:none;padding:3px 0;font-weight:400}.tag-chip,.tag-choice{font-size:11px;padding:3px 7px;border:0;background:#eaf5ef;color:#1b7761;border-radius:5px;max-width:100%;overflow-wrap:anywhere}.tag-chip span{margin-left:3px}.tag-options{display:flex;flex-wrap:wrap;gap:6px;padding:9px 0 0}.tag-choice{background:#f1f4f3;color:#6a7e73;border:1px solid transparent}.tag-choice[aria-pressed=true]{background:#eaf5ef;color:#0c796d;border-color:#adcebf}.tag-options small{color:#87978e}.editor-fold{border:1px solid #e0e8e3;border-radius:8px;overflow:hidden}.fold-toggle{display:flex;align-items:center;gap:9px;width:100%;border:0;border-radius:0;background:#fafcfb;padding:11px 12px;text-align:left;font-size:12px;font-weight:550}.fold-toggle small{margin-left:auto;color:#8c9b93;font-size:11px;font-weight:400}.fold-toggle .needs-input{color:#a57b31}.fold-toggle>span:last-child{color:#8c9b93}.fold-body{padding:12px;border-top:1px solid #e8ede9}.fold-body .hint{margin-bottom:0}.fingerprint-summary{display:flex;align-items:center;background:#fafcfb;padding-right:12px;gap:10px}.fingerprint-summary .fold-toggle{flex:1;min-width:0}.login-card-editor{border:1px solid #dce6df;border-radius:8px;overflow:hidden;margin-bottom:8px}.login-summary{display:flex;align-items:center;padding-right:12px;background:#f8fbf9}.login-toggle{display:flex;align-items:center;gap:9px;flex:1;min-width:0;border:0;border-radius:0;text-align:left;background:none;padding:10px 12px;font-size:12px}.login-toggle strong{font-weight:550;overflow:hidden;text-overflow:ellipsis}.login-toggle small{color:#0c796d;background:#e7f2ec;font-size:10px;padding:1px 6px;border-radius:4px}.login-toggle>span:last-child{margin-left:auto;color:#8c9b93}.login-avatar{font-size:10px;display:grid;place-items:center;width:22px;height:22px;background:#e9efeb;color:#708579;border-radius:5px}.login-body{padding:12px;border-top:1px solid #e5ede7}.inline-checkbox{display:flex;flex-direction:row;align-items:center;gap:6px!important;font-weight:400;margin-top:10px}.editor-section-heading .inline-checkbox{margin:0 0 0 auto}.default-login-hint{margin:5px 0 0;font-size:11px}.key-file-row{display:flex;align-items:center;justify-content:space-between;margin:6px 0 10px;color:#8a9991}.key-file-row small{font-size:10px}.key-file-button{position:relative;color:#0c796d;font-size:11px!important;cursor:pointer;overflow:hidden}.key-file-button input{position:absolute;inset:0;opacity:0;cursor:pointer}.relay-card-editor{border-bottom:1px solid #e5ebe7;padding-bottom:12px;margin-bottom:12px}.relay-card-editor .editor-fields{margin:10px 0 0}.add-relay{width:100%;border-style:dashed;padding:6px;font-size:12px;color:#0c796d}.target-editor-footer{padding:13px 24px;background:#fafcfb;border-top:1px solid #e5ebe7}.target-editor-footer .error{margin:0 0 10px;font-size:12px}.editor-footer-row{display:flex;align-items:center;gap:10px}.editor-footer-row>span{margin-right:auto;color:#91a097;font-size:11px}.target-editor .radio-group{gap:15px;margin-bottom:10px}.target-editor .hint{font-size:11px}.target-editor-footer button{font-size:12px;padding:8px 16px}
@media(max-width:700px){.target-editor{max-height:calc(100dvh - 24px);width:calc(100vw - 24px)}.target-editor-header{padding:16px;gap:9px}.target-editor-header p{display:none}.target-editor-header h2{font-size:17px}.target-editor-body{grid-template-columns:1fr;padding:16px;gap:18px}.target-editor-footer{padding:12px 16px}.editor-footer-row>span{font-size:10px;max-width:120px}.editor-switch{font-size:11px}}
.editor-delete{white-space:nowrap;font-size:12px;border-color:#ecd3d3;background:#fff7f6}.tag-options{padding:12px;border:1px solid #dce6e2;border-radius:10px;box-shadow:0 8px 24px #163b2d0c;gap:8px}.tag-choice{padding:7px 12px;border-radius:7px}.target-editor-header{flex-wrap:wrap}@media(max-width:600px){.target-editor-header{gap:10px}.target-editor-header>div{flex:1 1 100%}.editor-switch{margin-left:0}.editor-delete{margin-left:auto}}
/* 用轻微底色和边框区分编辑重点，次要设置保持紧凑。 */
.target-editor{--editor-surface:#f6f9f8;--editor-panel:#fff;--editor-line:#dce7e2;--editor-accent:#168775;--editor-accent-soft:#edf6f2;--editor-accent-line:#a8cfc0;--editor-strong:#254d41}
:global(:root[data-theme=dark] .target-editor){--editor-surface:#172326;--editor-panel:#1d2b2e;--editor-line:#344b4c;--editor-accent:#71bba6;--editor-accent-soft:#253d38;--editor-accent-line:#486e60;--editor-strong:#c7e4d9}
.target-editor-header{background:var(--editor-panel);box-shadow:inset 0 3px var(--editor-accent)}
.target-editor-header h2{font-weight:650;letter-spacing:.2px}
.target-editor-body{background:var(--editor-surface);gap:18px}
.editor-block{padding:15px;border:1px solid var(--editor-line);border-radius:10px;background:var(--editor-panel)}
.editor-block>.editor-section-heading{margin-bottom:16px}
.editor-block>.editor-section-heading h3{display:flex;align-items:center;gap:8px;color:var(--editor-strong);font-size:14px;font-weight:650}
.editor-block>.editor-section-heading h3::before{content:'';width:3px;height:15px;border-radius:2px;background:var(--editor-accent)}
.editor-count{display:inline-flex;align-items:center;justify-content:center;min-width:20px;height:20px;padding:0 5px;border-radius:5px;background:var(--editor-accent-soft);color:var(--editor-strong);font-size:11px;font-weight:600}
.target-editor .login-card-editor.is-active{border-color:var(--editor-accent-line);box-shadow:0 2px 7px #102c2806}
.target-editor .login-card-editor.is-active .login-summary{background:var(--editor-accent-soft)}
.target-editor .login-card-editor.is-active .login-toggle{background:transparent}
.target-editor .login-card-editor.is-active .login-toggle strong{color:var(--editor-strong);font-weight:650}
.target-editor .editor-fold[data-relays]{border-color:var(--editor-accent-line)}
.target-editor .editor-fold[data-relays]>.fold-toggle{background:var(--editor-accent-soft)}
.editor-fold[data-relays]>.fold-toggle>span:first-child{color:var(--editor-strong);font-weight:650}
.target-editor .fold-toggle .needs-input{padding:2px 6px;border-radius:4px;background:#a57b3112}
.target-editor-footer{box-shadow:0 -4px 16px #102c2805}
.target-editor-footer button.primary{min-width:106px;box-shadow:0 2px 5px #0c796d18}
@media(max-width:700px){.editor-block{padding:13px}.target-editor-body{gap:14px}.editor-block>.editor-section-heading{margin-bottom:13px}}
</style>

<style scoped>
.relay-expiry-row{display:flex;align-items:center;gap:10px;margin-top:12px;font-size:12px}.relay-expiry-row>span{white-space:nowrap;flex-shrink:0}.relay-expiry-options{display:flex;flex:1;min-width:0;gap:5px}.relay-expiry-options button{flex:1;min-width:0;padding:6px 3px;white-space:nowrap;font-size:12px;line-height:1.5;border-color:var(--editor-line);background:var(--editor-panel);color:inherit}.relay-expiry-options button[aria-pressed=true]{background:var(--editor-accent-soft);border-color:var(--editor-accent-line);color:var(--editor-strong)}.relay-expiry-options button:focus-visible{outline:2px solid var(--editor-accent);outline-offset:2px}
</style>

<style scoped>
.editor-password-row { display: flex; align-items: flex-end; gap: 6px; }
.editor-password-row > label { flex: 1; min-width: 0; }
.editor-password-row > button { flex-shrink: 0; }
.target-editor .secret-toggle { display: grid; place-items: center; width: 30px; height: 30px; padding: 4px; }

</style>
