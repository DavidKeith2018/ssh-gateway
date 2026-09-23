<script setup lang="ts">
import { t, display } from './i18n'
import { computed, ref } from 'vue'
import FileEntryIcon from './FileEntryIcon.vue'
import type { FileEntry } from './machine-api'
const props = defineProps<{
  entry: FileEntry
  childrenByPath: Record<string, FileEntry[]>
  expanded: Set<string>
  selected: string
  loading: Set<string>
  depth?: number
}>()
// 大目录分批呈现，收藏定位的节点始终包含在可见批次中。
const visibleLimit = ref(100)
const shownChildren = computed(() => {
  const all = props.childrenByPath[props.entry.path] || []
  const visible = all.slice(0, visibleLimit.value)
  const focused = all.find(
    (child) =>
      props.selected === child.path ||
      props.selected.startsWith(child.path + '/'),
  )
  if (focused && !visible.includes(focused)) visible.push(focused)
  return visible
})
const remaining = computed(
  () =>
    (props.childrenByPath[props.entry.path]?.length || 0) -
    shownChildren.value.length,
)
const emit = defineEmits<{
  select: [entry: FileEntry]
  open: [entry: FileEntry]
  toggle: [entry: FileEntry]
  menu: [event: MouseEvent, entry: FileEntry]
}>()
</script>
<template>
  <li
    role="treeitem"
    :aria-expanded="
      entry.kind === 'directory' ? expanded.has(entry.path) : undefined
    "
    :aria-selected="selected === entry.path"
    :aria-busy="loading.has(entry.path)"
  >
    <div
      class="tree-row"
      :class="{ selected: selected === entry.path }"
      :style="{ paddingLeft: `${8 + (depth || 0) * 17}px` }"
      :title="display(entry.path)"
      tabindex="0"
      @click="entry.kind === 'directory' ? ($event.detail < 2 && emit('toggle', entry)) : emit('select', entry)"
      @dblclick="
        entry.kind !== 'directory' && emit('open', entry)
      "
      @keydown.enter.prevent="
        entry.kind === 'directory' ? emit('toggle', entry) : emit('open', entry)
      "
      @contextmenu.prevent.stop="emit('menu', $event, entry)"
    >
      <button
        v-if="entry.kind === 'directory'"
        class="tree-toggle"
        :aria-label="display(`${expanded.has(entry.path) ? t('text.afd4b783536b') : t('text.00bd3960fea8')} ${entry.name}`)"
        @click.stop="$event.detail < 2 && emit('toggle', entry)"
        @dblclick.stop
        @keydown.enter.stop.prevent="emit('toggle', entry)"
      >
        <svg class="tree-chevron" :class="{ expanded: expanded.has(entry.path) }" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="m4.5 3 3 3-3 3" />
        </svg></button
      ><span v-else class="tree-spacer" />
      <FileEntryIcon :kind="entry.kind" /><span class="truncate"
        >{{ display(entry.name)
        }}<small v-if="entry.link" :title="t('text.35d313a2d642')"> ↗</small></span
      >
    </div>
    <ul
      v-if="entry.kind === 'directory' && expanded.has(entry.path)"
      role="group"
    >
      <li v-if="loading.has(entry.path)" class="tree-loading" :style="{ paddingLeft: `${59 + (depth || 0) * 17}px` }" role="none">{{ t('text.21bd738e0d71') }}</li>
      <FileTreeNode
        v-for="child in shownChildren"
        :key="child.path"
        :entry="child"
        :children-by-path="childrenByPath"
        :expanded="expanded"
        :selected="selected"
        :loading="loading"
        :depth="(depth || 0) + 1"
        @select="emit('select', $event)"
        @open="emit('open', $event)"
        @toggle="emit('toggle', $event)"
        @menu="(event, item) => emit('menu', event, item)"
      />
      <li v-if="remaining > 0" role="none">
        <button class="tree-more" @click="visibleLimit += 100">
          {{ t('text.51a5f415b032') }} {{ display(remaining) }} {{ t('text.1f41b36769ce') }}
        </button>
      </li>
      <li
        v-if="!loading.has(entry.path) && !childrenByPath[entry.path]?.length"
        class="tree-empty"
        :style="{ paddingLeft: `${59 + (depth || 0) * 17}px` }"
      >
        {{ t('text.3ea0e6e41518') }}
      </li>
    </ul>
  </li>
</template>
