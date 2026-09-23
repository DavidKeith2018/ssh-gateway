<script setup lang="ts">
withDefaults(defineProps<{ direction?: 'down' | 'up' | 'right' }>(), { direction: 'down' })
</script>

<template>
  <span class="chevron-icon" aria-hidden="true">
    <svg :class="direction" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">
      <path d="m3 4.5 3 3 3-3" />
    </svg>
  </span>
</template>

<style scoped>
.chevron-icon { display: inline-flex; align-items: center; justify-content: center; width: 12px; height: 12px; flex: 0 0 12px; vertical-align: middle; }
.chevron-icon svg { display: block; width: 12px; height: 12px; }
.chevron-icon .up { transform: rotate(180deg); }
.chevron-icon .right { transform: rotate(-90deg); }
</style>
