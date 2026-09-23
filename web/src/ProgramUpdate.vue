<script setup lang="ts">
import { msg, t, display } from './i18n'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from './api'
import { native, type UpdateInfo } from './desktop'
const props = defineProps<{ enabled: boolean; inline?: boolean }>()
const info = ref<UpdateInfo>()
const currentVersion = ref('')
const error = ref(''), busy = ref(false), notice = ref('')
const dialog = ref<HTMLDialogElement>()
let timer: ReturnType<typeof setInterval> | undefined
let stopped = false
const message = (e: unknown) => e instanceof Error ? e.message : String(e)
async function check() {
  if (!props.enabled || busy.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const result = native ? await native.CheckUpdate() : await api<UpdateInfo>('/update')
    if (stopped || !props.enabled || !result) return
    info.value = result
    currentVersion.value = result.current_version || currentVersion.value
    if (!result.available) notice.value = result.configured ? msg('text.51799763d933') : msg('text.7fdfcfb23d4b')
  } catch (e) { if (!stopped && props.enabled) error.value = message(e) }
  finally { busy.value = false }
}
async function openRelease(event: MouseEvent) {
  if (!native || !info.value?.url) return
  event.preventDefault()
  try { await native.OpenExternalURL(info.value.url) }
  catch (e) { error.value = message(e) }
}
async function show() {
  dialog.value?.showModal()
  await check()
}
defineExpose({ show })
watch(() => props.enabled, enabled => { if (enabled) void check(); else { info.value = undefined; dialog.value?.close() } }, { immediate: true })
onMounted(async () => {
  timer = setInterval(() => { void check() }, 6 * 60 * 60 * 1000)
  try {
    const result = native ? await native.Info() : await api<{ version: string }>('/version')
    if (!stopped) currentVersion.value = result.version || currentVersion.value
  } catch { /* 版本请求失败不影响登录和连接；检查更新成功时也会返回当前版本。 */ }
})
onBeforeUnmount(() => { stopped = true; clearInterval(timer) })
</script>

<template>
  <section class="program-update" :aria-label="t('text.ec5658d4a5d2')">
    <div class="version-status" :class="{ 'sidebar-version': inline }">
      <button class="current-version" :disabled="!enabled" :aria-label="display(t('text.1b9e9d2bae45', [currentVersion || t('text.4d8c1c5b4283')]))" :title="display(enabled ? t('text.9695df23e7f5') : t('text.9ff53bd778b6'))" @click="show"><template v-if="inline"><span class="version-label">{{ t('text.ec5658d4a5d2') }}</span><span class="version-number" :title="currentVersion ? `v${currentVersion}` : ''">{{ display(currentVersion ? `v${currentVersion}` : t('text.4d8c1c5b4283')) }}</span></template><template v-else>{{ t('text.837bc9576721') }} {{ display(currentVersion || t('text.4d8c1c5b4283')) }}</template></button>
      <button v-if="enabled && info?.available" class="new-version" @click="dialog?.showModal()">{{ t('text.ac217e4d1ca4') }} {{ display(info.version) }}</button>
    </div>
    <dialog ref="dialog" class="small-dialog">
      <div class="dialog-heading"><h2>{{ display(info?.available ? t('text.071d9e2914c4', [info.version]) : t('text.ec5658d4a5d2')) }}</h2><button class="close-button" :aria-label="t('text.341755549ed3')" @click="dialog?.close()">×</button></div>
      <div class="dialog-content">
        <p>{{ t('text.837bc9576721') }} {{ display(currentVersion || t('text.4d8c1c5b4283')) }}</p>
        <pre v-if="info?.available" class="release-notes">{{ display(info?.notes || t('text.631cfa84fce1')) }}</pre>
        <p v-if="info?.available">{{ t('text.74d21092c51d') }}</p>
        <p v-if="error" class="error" role="alert">{{ display(error) }}</p>
        <p v-if="notice" role="status">{{ display(notice) }}</p>
      </div>
      <div class="dialog-footer">
        <button @click="dialog?.close()">{{ t('text.bf639a51feec') }}</button>
        <button :disabled="busy || !enabled" @click="check">{{ display(busy ? t('text.6b72c3d6855c') : t('text.7f68ebad19ba')) }}</button>
        <a v-if="info?.url" :href="info.url" target="_blank" rel="noopener noreferrer" @click="openRelease">{{ t('text.783b855677c7') }}</a>
      </div>
    </dialog>
  </section>
</template>

<style scoped>
.version-status { position: fixed; left: 16px; bottom: 12px; z-index: 500; display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-start; max-width: calc(100vw - 32px); }
.version-status button { font-size: 12px; padding: 6px 10px; box-shadow: 0 2px 8px #0001; }
.version-status button:disabled { opacity: 1; cursor: default; }
.sidebar-version { position: static; margin-top: 16px; padding-top: 12px; border-top: 1px solid #82958c40; max-width: 100%; flex-direction: column; align-items: flex-start; gap: 9px; }
.sidebar-version button { padding: 0; border: 0; border-radius: 0; background: transparent; box-shadow: none; color: inherit; font-size: 11px; text-align: left; }
.sidebar-version .current-version { display: flex; flex-direction: column; align-items: flex-start; gap: 5px; width: 100%; min-width: 0; }
.version-label { font-size: 11px; opacity: .8; white-space: nowrap; flex-shrink: 0; }
.version-number { display: block; max-width: 100%; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding: 2px 7px; border: 1px solid #82958c30; border-radius: 5px; background: #82958c14; font-family: ui-monospace, monospace; font-size: 11px; font-weight: 600; }
.current-version:hover .version-number { border-color: #82958c80; background: #82958c25; }
.sidebar-version .new-version { color: var(--accent, #16887a); }
.sidebar-version .new-version:hover { text-decoration: underline; text-underline-offset: 3px; }
.new-version { color: var(--accent, #16887a); }
.release-notes { white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; max-height: 240px; overflow-y: auto; }
.dialog-footer a { align-self: center; }
</style>
