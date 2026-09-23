<script setup lang="ts">
import type { FileEntry } from './machine-api'
defineProps<{ kind: FileEntry['kind'] }>()
</script>
<template>
  <svg class="entry-icon" :class="kind === 'directory' ? 'folder-icon' : 'file-icon'" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path v-if="kind === 'directory'" d="M2 6V4.5h6l2 2h8v10H2V6Zm0 3h16" />
    <path v-else-if="kind === 'link'" d="M5 15 15 5M6 5h9v9" />
    <path v-else d="M4 2.5h8l4 4v11H4Zm8 0v4h4M7 10h6M7 13h6" />
  </svg>
</template>
