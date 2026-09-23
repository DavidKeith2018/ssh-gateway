<script setup lang="ts">
import { msg, t, display, formatNumber } from './i18n'
import { computed, ref, watch } from 'vue'
import type { PageChange } from './pagination'
defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{ page: number; pageSize: number; total: number; loading?: boolean; label?: string }>(), { label: msg('text.bf6e7b32c721'), loading: false })
const emit = defineEmits<{ change: [value: PageChange] }>()
const last = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))
const jump = ref(String(props.page))
watch(() => props.page, value => { jump.value = String(value) })
const pages = computed(() => {
  const values = new Set([1, last.value])
  for (let n = Math.max(1, props.page - 2); n <= Math.min(last.value, props.page + 2); n++) values.add(n)
  const result: (number | string)[] = []
  for (const n of [...values].sort((a, b) => a - b)) {
    const previous = result.at(-1)
    if (typeof previous === 'number' && n - previous > 1) {
      if (n - previous === 2) result.push(previous + 1)
      else result.push(msg('text.c1533f3c1146', [n]))
    }
    result.push(n)
  }
  return result
})
function go(page: number, pageSize = props.pageSize) {
  if (!Number.isInteger(page) || props.loading) return
  emit('change', { page: Math.max(1, Math.min(last.value, page)), pageSize })
}
function submit() { const n = Number(jump.value); if (String(jump.value).trim() && Number.isInteger(n)) go(n); else jump.value = String(props.page) }
</script>
<template>
  <nav class="pagination" :aria-label="display(label)" :aria-busy="loading">
    <span class="pagination-total">{{ t('pagination.total', [formatNumber(total)]) }}<span v-if="total">{{ t('pagination.range', [formatNumber((page - 1) * pageSize + 1), formatNumber(Math.min(page * pageSize, total))]) }}</span></span>
    <label>{{ t('text.8c932f987c21') }} <select :value="display(pageSize)" :disabled="loading" :aria-label="t('text.940a168911ad')" @change="go(1, Number(($event.target as HTMLSelectElement).value))"><option v-for="size in [10, 20, 50, 100]" :key="size" :value="display(size)">{{ display(size) }}</option></select> {{ t('text.f004f1d84cf9') }}</label>
    <div class="pagination-pages">
      <button type="button" :disabled="loading || !total || page <= 1" :aria-label="t('text.c9b9ae7a6144')" @click="go(page - 1)">{{ t('text.c9b9ae7a6144') }}</button>
      <template v-for="n in pages" :key="n"><button v-if="typeof n === 'number'" type="button" :aria-label="display(t('text.94289aa33f49', [n]))" :aria-current="n === page ? 'page' : undefined" :disabled="loading || !total" @click="go(n)">{{ display(n) }}</button><span v-else aria-hidden="true">…</span></template>
      <button type="button" :disabled="loading || !total || page >= last" :aria-label="t('text.8a8542f69648')" @click="go(page + 1)">{{ t('text.8a8542f69648') }}</button>
    </div>
    <form class="pagination-jump" @submit.prevent="submit"><label>{{ t('text.fa96079c3b2f') }} <input v-model="jump" :aria-label="t('text.e6944bbd76f0')" type="number" min="1" :max="last" step="1" :disabled="loading || !total" /> {{ t('text.d24d3c99460c') }}</label><button type="submit" :disabled="loading || !total">{{ t('text.afd0b9482dfa') }}</button></form>
  </nav>
</template>
<style scoped>
.pagination{flex-shrink:0;display:flex;flex-wrap:wrap;align-items:center;gap:12px;padding:14px 16px;color:inherit;border-top:1px solid var(--mp-border,var(--border,#dce5e2));font-size:12px;line-height:1.5}
.pagination-total{margin-right:auto}.pagination label,.pagination-jump,.pagination-pages{display:flex;flex-direction:row;align-items:center;gap:6px;margin:0;white-space:nowrap}.pagination button,.pagination input,.pagination select{font:inherit;color:inherit;background:transparent;border:1px solid var(--mp-border,var(--border,#a2afa980));border-radius:6px;padding:6px 9px;min-height:32px;width:auto;margin:0}.pagination input{width:64px;min-width:0}.pagination button{cursor:pointer}.pagination button[aria-current=page]{background:#0c796d;border-color:#0c796d;color:white}.pagination button:disabled,.pagination input:disabled{opacity:.45;cursor:default}.pagination :focus-visible{outline:2px solid #0c9e8e;outline-offset:2px}.pagination select option{color:#20312f;background:#fff}
@media(max-width:600px){.pagination{gap:10px;padding:12px}.pagination-total{flex-basis:100%}.pagination-pages{flex-wrap:wrap}.pagination button{padding:6px 8px}}
</style>
