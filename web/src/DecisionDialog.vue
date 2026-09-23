<script setup lang="ts">
import { t, display } from './i18n'
import { ref, watch, nextTick } from 'vue'
import type { Decision } from './decision'
const props = defineProps<{ value: Decision | null }>()
const emit = defineEmits<{ finish: [choice: number, text: string] }>()
const dialog = ref<HTMLDialogElement>(),
  text = ref('')
watch(
  () => props.value,
  async (value) => {
    if (value) {
      text.value = value.input ?? ''
      await nextTick()
      dialog.value?.showModal()
    } else dialog.value?.close()
  },
)
</script>
<template>
  <dialog
    ref="dialog"
    class="machine-dialog"
    @cancel.prevent="emit('finish', -1, '')"
  >
    <template v-if="value"
      ><h3>{{ display(value.title) }}</h3>
      <p>{{ display(value.message) }}</p>
      <input
        v-if="value.input !== undefined"
        v-model="text"
        :aria-label="t('text.d44e9b3d3b31')"
        autofocus
        @keydown.enter="emit('finish', 0, text)"
      />
      <footer>
        <button @click="emit('finish', -1, '')">{{ t('text.2cd0f3be8738') }}</button
        ><button
          v-for="(choice, index) in value.choices"
          :key="choice"
          :class="{ primary: index === 0 }"
          @click="emit('finish', index, text)"
        >
          {{ display(choice) }}
        </button>
      </footer></template
    >
  </dialog>
</template>
