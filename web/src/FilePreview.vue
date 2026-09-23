<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { t } from './i18n'
const props = defineProps<{ path: string; preview: { mime: string; data: string } | null; failed: boolean }>()
defineEmits<{ download: [] }>()
const url = ref(''), unsupported = ref(false)
const viewport = ref<HTMLElement>()
const natural = ref({ width: 0, height: 0 }), available = ref({ width: 0, height: 0 })
const zoom = ref<number | null>(null)
const scale = computed(() => zoom.value ?? (natural.value.width && available.value.width ? Math.max(0.01, Math.min(1, (available.value.width - 16) / natural.value.width, (available.value.height - 16) / natural.value.height)) : 1))
const imageStyle = computed(() => natural.value.width ? { width: Math.max(1, natural.value.width * scale.value) + 'px', height: Math.max(1, natural.value.height * scale.value) + 'px' } : {})
function resizeImage(event: Event) {
 const image = event.target as HTMLImageElement
 natural.value = { width: image.naturalWidth, height: image.naturalHeight }
}
function changeZoom(factor: number) { zoom.value = Math.max(0.1, Math.min(8, scale.value * factor)) }
function wheelZoom(event: WheelEvent) {
 if (!event.deltaY || !props.preview?.mime.startsWith('image/')) return
 event.preventDefault()
 changeZoom(event.deltaY < 0 ? 1.2 : 1 / 1.2)
}
const observer = new ResizeObserver(entries => {
 const rect = entries[0]?.contentRect
 if (rect) available.value = { width: rect.width, height: rect.height }
})
watch(viewport, element => { observer.disconnect(); if (element) observer.observe(element) })
onBeforeUnmount(() => observer.disconnect())
watch(() => props.preview, value => {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''; unsupported.value = false; zoom.value = null; natural.value = { width: 0, height: 0 }
  if (value) {
    const bytes = Uint8Array.from(atob(value.data), c => c.charCodeAt(0))
    url.value = URL.createObjectURL(new Blob([bytes], { type: value.mime }))
  }
}, { immediate: true })
onBeforeUnmount(() => { if (url.value) URL.revokeObjectURL(url.value) })
</script>
<template>
  <section class="file-preview" :aria-label="t('files.preview')">
    <div class="preview-toolbar"><span :title="path">{{ path }}</span><div v-if="preview?.mime.startsWith('image/') && !failed && !unsupported" class="image-zoom-controls">
      <button :aria-label="t('files.zoomOut')" :title="t('files.zoomOut')" :disabled="scale <= 0.1" @click="changeZoom(1 / 1.2)">−</button>
      <button :aria-label="t('files.actualSize')" :title="t('files.actualSize')" @click="zoom = 1">{{ Math.round(scale * 100) }}%</button>
      <button :aria-label="t('files.zoomIn')" :title="t('files.zoomIn')" :disabled="scale >= 8" @click="changeZoom(1.2)">＋</button>
      <button @click="zoom = null">{{ t('files.fitImage') }}</button>
    </div><button @click="$emit('download')">{{ t('files.download') }}</button></div>
    <p v-if="failed || unsupported" role="status">{{ t('files.previewUnavailable') }}</p>
    <p v-else-if="!preview">{{ t('files.previewLoading') }}</p>
    <div v-else ref="viewport" class="preview-content" :class="{ 'image-content': preview.mime.startsWith('image/') }" @wheel="wheelZoom">
      <div v-if="preview.mime.startsWith('image/')" class="image-canvas"><img :src="url" :alt="path" :style="imageStyle" @load="resizeImage" @error="unsupported = true" /></div>
      <audio v-else-if="preview.mime.startsWith('audio/')" :src="url" controls preload="metadata" @error="unsupported = true" />
      <video v-else-if="preview.mime.startsWith('video/')" :src="url" controls preload="metadata" @error="unsupported = true" />
      <iframe v-else-if="preview.mime === 'application/pdf'" :src="url" :title="path" />
    </div>
  </section>
</template>
<style scoped>
.file-preview { min-height: 0; flex: 1; display: flex; flex-direction: column; overflow: hidden; background: var(--surface); }
.preview-toolbar { display: flex; align-items: center; gap: 12px; padding: 6px 10px; border-bottom: 1px solid var(--border); }
.image-zoom-controls { display: flex; align-items: center; gap: 3px; flex-shrink: 0; }
.preview-toolbar { flex-wrap: wrap; gap: 6px; }
.preview-toolbar > button { flex-shrink: 0; }
.preview-toolbar span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.preview-content { flex: 1; min-height: 0; display: flex; align-items: center; justify-content: center; overflow: auto; padding: 8px; }
.preview-content :is(img,video) { max-width: 100%; max-height: 100%; object-fit: contain; }
.preview-content audio { width: min(500px,100%); }
.preview-content iframe { width: 100%; height: 100%; border: 0; background: white; }
.preview-content.image-content { display: block; padding: 0; }
.image-canvas { display: flex; width: max-content; min-width: 100%; min-height: 100%; padding: 8px; }
.image-canvas img { display: block; max-width: none; max-height: none; margin: auto; flex-shrink: 0; }
.file-preview p { padding: 12px; }
</style>
