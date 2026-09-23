<script setup lang="ts">
import { t, display, formatNumber } from './i18n'
import { machineConnectionKey } from './machine-connection'
import { onPageLeave } from './page-lifecycle'
import { inject, watch, ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { connectionErrorsKey } from './connection-errors'
import type { Target } from './api'
import {
  machineAPI,
  errorText,
  type Sample,
  type HardwareInfo,
} from './machine-api'
const props = defineProps<{ target: Target }>()
const sharedConnection = inject(machineConnectionKey)
const sharedID = () => sharedConnection?.value || ''
const connectionErrors = inject(connectionErrorsKey)
const hardware = ref<HardwareInfo>(),
  hardwareLoading = ref(false),
  hardwareError = ref('')
let suspended = false
let hardwareController: AbortController | undefined
async function loadHardware() {
  if (!sharedID() || suspended || hardwareLoading.value) return
  hardwareLoading.value = true
  hardwareError.value = ''
  hardwareController = new AbortController()
  const request = hardwareController
  try {
    const data = await machineAPI<HardwareInfo>(
      props.target.id,
      'hardware',
      undefined,
      request.signal,
      sharedID(),
    )
    if (!request.signal.aborted) hardware.value = data
  } catch (e) {
    if (!disposed && !request.signal.aborted) hardwareError.value = errorText(e)
  } finally {
    if (hardwareController === request) hardwareLoading.value = false
  }
}
const current = ref<Sample>(),
  error = ref(''),
  now = ref(Date.now())
let previous: Sample | undefined,
  timer: ReturnType<typeof setInterval>,
  controller: AbortController | undefined,
  disposed = false
const lastCPU = ref<number | null>(null)
const cpu = computed(() => lastCPU.value)
const memory = computed(() =>
  current.value?.memory_ready
    ? ((current.value.memory_total - current.value.memory_available) /
        current.value.memory_total) *
      100
    : null,
)
const stale = computed(
  () =>
    current.value && now.value - new Date(current.value.time).getTime() > 15000,
)
function gb(value: number) {
  return formatNumber(value / 1024 ** 3, { minimumFractionDigits: 1, maximumFractionDigits: 1 })
}
async function sample() {
  if (!sharedID() || disposed || suspended || document.hidden || controller) return
  controller = new AbortController()
  const request = controller
  try {
    const data = await machineAPI<Sample>(
      props.target.id,
      'resources',
      undefined,
      request.signal,
      sharedID(),
    )
    if (disposed || request.signal.aborted) return
    const time = new Date(data.time).getTime()
    let value: number | null = null
    if (
      data.cpu_ready &&
      previous?.cpu_ready &&
      time - new Date(previous.time).getTime() > 0 &&
      time - new Date(previous.time).getTime() < 15000
    ) {
      const total = data.cpu_total - previous.cpu_total,
        idle = data.cpu_idle - previous.cpu_idle
      if (total > 0 && idle >= 0 && idle <= total)
        value = ((total - idle) / total) * 100
    }
    lastCPU.value = value
    previous = data
    current.value = data
    error.value = ''

  } catch (e) {
    if (disposed || request.signal.aborted) return
    error.value = errorText(e)
    previous = undefined
    lastCPU.value = null
  } finally {
    if (controller === request) controller = undefined
  }
}
watch(hardwareError, message => {
  if (message) connectionErrors?.report('hardware', message)
  else connectionErrors?.recover('hardware')
})
watch(error, message => {
  if (message) connectionErrors?.report('resources', message)
  else connectionErrors?.recover('resources')
})
watch(() => sharedID(), () => {
  controller?.abort()
  hardwareController?.abort()
  hardware.value = undefined
  current.value = undefined
  previous = undefined
  lastCPU.value = null
  error.value = ''
  hardwareError.value = ''
  // 旧请求通过自己的取消信号丢弃结果。
  hardwareLoading.value = false
  controller = undefined
  if (sharedID()) { void loadHardware(); void sample() }
})
function visibility() {
  if (document.hidden) {
    controller?.abort()
    previous = undefined
  } else void sample()
}
onMounted(() => {
  void loadHardware()
  void sample()
  timer = setInterval(() => {
    now.value = Date.now()
    void sample()
  }, 5000)
  document.addEventListener('visibilitychange', visibility)
})
onPageLeave(() => {
  suspended = true
  controller?.abort()
  hardwareController?.abort()
}, () => {
  suspended = false
  void loadHardware()
  void sample()
})
onBeforeUnmount(() => {
  disposed = true
  clearInterval(timer)
  controller?.abort()
  hardwareController?.abort()
  document.removeEventListener('visibilitychange', visibility)
})
</script>
<template>
  <aside class="machine-info">
    <section
      class="machine-card config-card hardware-card"
      :aria-label="t('text.c2537bf08a1f')"
    >
      <header class="section-heading">
        <strong>{{ t('text.c2537bf08a1f') }}</strong
        ><button
          :aria-label="t('text.eb8a96bcee8c')"
          :disabled="hardwareLoading"
          @click="loadHardware"
        >
          ↻
        </button>
      </header>
      <p v-if="hardwareError && !connectionErrors" class="hardware-error" role="alert">
        {{ display(hardwareError) }}
      </p>
      <p v-else-if="hardwareLoading && !hardware" class="empty-hint">
        {{ t('text.de69914b4aaf') }}
      </p>
      <dl>
        <dt>{{ t('text.0224c3f6672e') }}</dt>
        <dd :title="display(hardware?.hostname)">
          {{ display(hardware?.hostname || t('text.c9c37a1ebaf2')) }}
        </dd>
        <dt>{{ t('text.360480034cc3') }}</dt>
        <dd :title="display(hardware?.os)">{{ display(hardware?.os || t('text.c9c37a1ebaf2')) }}</dd>
        <dt>{{ t('text.416baf7ad2a7') }}</dt>
        <dd :title="display(hardware?.kernel)">{{ display(hardware?.kernel || t('text.c9c37a1ebaf2')) }}</dd>
        <dt>{{ t('text.8b784b628805') }}</dt>
        <dd>{{ display(hardware?.architecture || t('text.c9c37a1ebaf2')) }}</dd>
        <dt>{{ t('text.940259777914') }}</dt>
        <dd class="hardware-model">{{ display(hardware?.cpu_model || t('text.c9c37a1ebaf2')) }}</dd>
        <dt>{{ t('text.b564a6065938') }}</dt>
        <dd>
          {{
            display(hardware?.logical_cpus ? hardware.logical_cpus + t('text.7aaf96ad52e0') : t('text.c9c37a1ebaf2'))
          }}
        </dd>
        <dt>{{ t('text.e42429e78fa9') }}</dt>
        <dd>
          {{
            display(hardware?.memory_total
              ? gb(hardware.memory_total) + ' GiB'
              : t('text.c9c37a1ebaf2'))
          }}
        </dd>
        <dt :title="t('text.2a0b12a25898')">{{ t('text.16f0998a9688') }}</dt>
        <dd>{{ display(hardware?.disk_total ? gb(hardware.disk_total) + ' GiB' : t('text.c9c37a1ebaf2')) }}</dd>
        <dt>{{ t('text.d082d02d37f1') }}</dt>
        <dd class="hardware-model">{{ display(hardware?.product || t('text.c9c37a1ebaf2')) }}</dd>
      </dl>
    </section>
    <section class="machine-card resource-card" :aria-label="t('text.90c12f0b00b7')">
      <dl class="resource-metrics">
        <div><dt>{{ t('text.1c82e5547854') }}</dt><dd>{{ display(error || stale || cpu === null ? '—' : formatNumber(cpu, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + '%') }}</dd></div>
        <div><dt>{{ t('text.ace212ad8fd3') }}</dt><dd>{{ display(error || stale || memory === null ? '—' : formatNumber(memory, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + '%') }}</dd></div>
        <div><dt :title="t('text.eab0148ab060')">{{ t('text.681378c55ba5') }}</dt><dd>{{ display(!error && !stale && current?.disk_ready ? formatNumber(current.disk_usage, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + '%' : '—') }}</dd></div>
      </dl>
      <small v-if="!sharedID()">{{ t('text.01f6e4f38c77') }}</small>
      <small v-else>{{ display((error ? (connectionErrors ? t('text.f36cac96220b') : error) : '') || (stale ? t('text.380088fe1757') : !current ? t('text.54a5daf5fbc2') : t('text.76162dd1852c'))) }}</small>
    </section>
  </aside>
</template>
