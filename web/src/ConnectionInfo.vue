<script setup lang="ts">
import { msg, t, display } from './i18n'
import { ref } from 'vue'
import type { TerminalConnectionInfo } from './terminal-transport'
import type { Target } from './api'
defineProps<{ target: Target; connection?: TerminalConnectionInfo; readonly?: boolean }>()
const emit = defineEmits<{ edit: [] }>()
const dialog = ref<HTMLDialogElement>(),
  copyNote = ref('')
async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    copyNote.value = msg('text.8f6f8d979c98')
  } catch {
    copyNote.value = msg('text.c9c4c20dc1e2')
  }
}
function show() {
  copyNote.value = ''
  dialog.value?.showModal()
}
defineExpose({ show })
</script>
<template>
  <dialog
    ref="dialog"
    class="machine-dialog connection-info-dialog"
    :aria-label="t('text.065015932c70')"
  >
    <section class="machine-card config-card">
      <header class="section-heading">
        <strong>{{ t('text.065015932c70') }}</strong
        ><button :aria-label="t('text.da4972dbd6d4')" @click="dialog?.close()">×</button>
      </header>
      <dl>
        <dt>{{ t('text.d44e9b3d3b31') }}</dt>
        <dd :title="display(target.name)">{{ display(target.name) }}</dd>
        <dt>{{ t('text.317c133e7a87') }}</dt>
        <dd>
          <button
            class="copy-value"
            :title="display(target.host)"
            @click="copy(target.host)"
          >
            {{ display(target.host) }}
          </button>
        </dd>
        <dt>{{ t('text.1c22cc57aee8') }}</dt>
        <dd>{{ display(target.port) }}</dd>
        <dt>{{ t('text.4c74e040c922') }}</dt>
        <dd>{{ display(target.user) }}</dd>
        <dt>{{ t('text.d1ff8f98bc3f') }}</dt>
        <dd>
          {{ display(target.auth_type === 'private_key' ? t('text.45ff0431f689') : t('text.5c4ddb326e0d')) }}
        </dd>
        <dt>{{ t('text.a3d15aa9e6ad') }}</dt>
        <dd>{{ display(connection ? (connection.mode === 'relay' ? t('text.073caef588ad') : t('text.2cb4fc030fc0')) : t('text.346ff60e6c7c')) }}</dd>
        <dt>{{ t('text.7977db408132') }}</dt>
        <dd>{{ display(connection?.user || '—') }}</dd>
        <dt>{{ t('text.bd70806eb483') }}</dt>
        <dd>{{ display(connection?.host || '—') }}</dd>
        <dt>{{ t('text.7a63b404f1b1') }}</dt>
        <dd>{{ display(connection?.port || '—') }}</dd>
      </dl>
      <div class="config-footer">
        <small>{{ display(copyNote) }}</small
        ><button v-if="!readonly" @click="emit('edit')">{{ t('text.066a63f95f73') }}</button>
      </div>
    </section>
  </dialog>
</template>
