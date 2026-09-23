<script setup lang="ts">
import { t, display } from './i18n'
import { onBeforeUnmount, ref, useId } from 'vue'

const dialog = ref<HTMLDialogElement>()
const trigger = ref<HTMLButtonElement>()
const headingID = useId()
function close() { dialog.value?.close() }
onBeforeUnmount(close)

defineProps<{ localSide: string; localDesktop: boolean; inline?: boolean }>()
</script>

<template>
  <div class="mapping-help" :class="{ 'mapping-help-inline': inline }">
    <span v-if="!inline">{{ t('text.4ce8108704d5') }}{{ display(localSide) }}</span>
    <button ref="trigger" type="button" class="mapping-help-trigger" @click="dialog?.showModal()">{{ t(inline ? 'mapping.viewHelp' : 'text.cb4ba40bf823') }}</button>
    <dialog ref="dialog" class="mapping-help-dialog" :aria-labelledby="headingID" @cancel.stop.prevent="close" @close.stop="trigger?.focus()">
      <header class="mapping-help-heading">
        <h2 :id="headingID">{{ t('text.cb4ba40bf823') }}</h2>
        <button type="button" :aria-label="t('text.a8c6c1c56c3d')" @click="close">×</button>
      </header>
      <div class="mapping-help-content">
        <p v-if="inline">{{ t('text.4ce8108704d5') }}{{ display(localSide) }}</p>
        <p><strong>{{ t('text.119ec2724d79') }}</strong>{{ t('text.04e8168400b0') }} {{ display(localSide) }}。</p>
        <p><strong>{{ t('text.191dd224652d') }}</strong>{{ display(localSide) }}{{ t('text.ad348a2b147c') }}</p>
        <p v-if="!localDesktop">{{ t('text.a26f5d3559cf') }}</p>
        <p><strong>{{ t('text.bc8d22d0061c') }}</strong>{{ t('text.4740e933e427') }}</p>
        <p><strong>{{ t('text.86344fd28ab4') }}</strong>{{ t('text.6b2da49e045a') }}</p>
      </div>
    </dialog>
  </div>
</template>

<style scoped>
.mapping-help{display:flex;flex-wrap:wrap;align-items:center;gap:6px 20px;margin:0 0 16px;color:var(--mp-muted);font-size:12px}
.mapping-help .mapping-help-trigger{padding:0;border:0;background:transparent;color:var(--mp-text);font-size:12px}
.mapping-help.mapping-help-inline{display:inline-flex;margin:0;vertical-align:baseline}.mapping-help-inline .mapping-help-trigger{color:#0c8b79;text-decoration:underline;text-underline-offset:3px;white-space:nowrap}
.mapping-help-dialog{position:fixed;inset:0;margin:auto;padding:0;width:560px;max-width:calc(100vw - 32px);max-height:90vh;max-height:90dvh;border:1px solid var(--mp-border);border-radius:14px;background:var(--mp-card);color:var(--mp-text);font-size:13px;overflow:hidden}
.mapping-help-dialog[open]{display:flex;flex-direction:column}
.mapping-help-dialog::backdrop{background:#152e2855}
.mapping-help-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-shrink:0;padding:18px 22px;border-bottom:1px solid var(--mp-border)}
.mapping-help-heading h2{margin:0;font-size:18px}
.mapping-help-heading button{padding:0 8px;border:0;background:transparent;font-size:23px}
.mapping-help-content{padding:18px 22px;min-height:0;overflow:auto;line-height:1.7;overflow-wrap:anywhere;color:var(--mp-muted)}
.mapping-help-content p{margin:0 0 12px}
.mapping-help-content p:last-child{margin-bottom:0}
.mapping-help-content strong{font-weight:500;color:var(--mp-text)}
@media(max-width:600px){.mapping-help-heading,.mapping-help-content{padding:16px}}
</style>
