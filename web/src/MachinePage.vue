<script setup lang="ts">
import { msg, t, display } from './i18n'
import LanguageSwitcher from './LanguageSwitcher.vue'
import { machineConnectionKey } from './machine-connection'
import { provide } from 'vue'
import { connectionErrorsKey } from './connection-errors'
import ChevronIcon from './ChevronIcon.vue'
import { openMachineWindow } from './open-machine-window'
import { theme, changeTheme } from './theme'
import { randomID } from './random-id'
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import type { Target } from './api'
import type { TerminalConnectionInfo } from './terminal-transport'
import TerminalPanel from './TerminalPanel.vue'
import TerminalQuickActions from './TerminalQuickActions.vue'
import FileWorkspace from './FileWorkspace.vue'
import MachineInfo from './MachineInfo.vue'
import ConnectionInfo from './ConnectionInfo.vue'
import ConnectionDiagnostic from './ConnectionDiagnostic.vue'
import { relayOptions, relayActive } from './relay-access'
const diagnostic = ref<InstanceType<typeof ConnectionDiagnostic>>()
import DecisionDialog from './DecisionDialog.vue'
import { useDecision } from './decision'
import './machine.css'
import MappingWorkspace from './MappingWorkspace.vue'
const mappings = ref<InstanceType<typeof MappingWorkspace>>()
const props = defineProps<{ target: Target; selection?: string; detached?: boolean; canManage?: boolean }>()
const connectionTarget = computed(() => {
 const selection = props.selection
 const relay = props.target.relays?.find(item => selection ? item.id === selection : item.enabled)
 const loginID = selection?.startsWith('server:') ? selection.slice(7) : relay?.login_id
 const login = props.target.logins?.find(item => item.id === loginID)
 return login ? { ...props.target, user: login.user, auth_type: login.auth_type, relay_user: relay?.username || props.target.relay_user } : props.target
})
const selectedUser = computed(() => connectionTarget.value.user)
const previousTitle = document.title
watch(() => props.target.name, name => { document.title = `${name} — WebSSH` }, { immediate: true })


const emit = defineEmits<{ close: []; edit: [] }>()
const { decision, finish, ask } = useDecision()
const { decision: connectionError, finish: dismissConnectionError, ask: showConnectionError } = useDecision()
const failedSources = new Set<string>()
provide(connectionErrorsKey, {
  report(source, message) {
    if (failedSources.has(source)) return
    failedSources.add(source)
    if (!connectionError.value) void showConnectionError({ title: msg('text.89c4766afc85'), message, choices: [msg('text.de32e20193ad')] })
  },
  recover(source) { failedSources.delete(source) },
})

const files = ref<InstanceType<typeof FileWorkspace>>()
const connectionInfo = ref<InstanceType<typeof ConnectionInfo>>()
const dirty = ref(0),
  transferring = ref(false),
  leaving = ref(false)

const treeWidthKey = 'ssh-gateway:tree-width'
const treeWidth = ref(260)
try { const cookie = document.cookie.split('; ').find(value => value.startsWith('ssh-gateway-tree-width='))?.split('=')[1]; const saved = Number(cookie || localStorage.getItem(treeWidthKey)); if (Number.isFinite(saved) && saved >= 180) treeWidth.value = Math.min(520, saved) } catch {}
function setTreeWidth(value: number) {
  treeWidth.value = Math.max(180, Math.min(520, window.innerWidth - 360, value))
  try { localStorage.setItem(treeWidthKey, String(treeWidth.value)) } catch {}
  // Desktop workspace ports change between launches; the layout cookie also survives that change.
  document.cookie = `ssh-gateway-tree-width=${treeWidth.value}; Path=/; Max-Age=31536000; SameSite=Strict`
  window.dispatchEvent(new Event('resize'))
}
let stopTreeDrag: (() => void) | undefined
function dragTree(event: PointerEvent) {
  if (event.button !== 0) return
  const handle = event.currentTarget as HTMLElement
  handle.setPointerCapture(event.pointerId)
  const start = event.clientX, width = treeWidth.value
  const move = (next: PointerEvent) => setTreeWidth(width + next.clientX - start)
  stopTreeDrag?.()
  stopTreeDrag = () => { handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', stopTreeDrag!); handle.removeEventListener('pointercancel', stopTreeDrag!) }
  handle.addEventListener('pointermove', move)
  handle.addEventListener('pointerup', stopTreeDrag, { once: true })
  handle.addEventListener('pointercancel', stopTreeDrag, { once: true })
}
const showTree = ref(window.innerWidth >= 900),
  showInfo = ref(window.innerWidth >= 1200),
  split = ref(49)
const sessions = ref<
    { id: string; name: string; status: string; connected: boolean; initialDirectory?: string; connection?: TerminalConnectionInfo }[]
  >([]),
  activeID = ref('')
const terminalRefs = new Map<string, InstanceType<typeof TerminalPanel>>()
const currentConnection = computed(() => sessions.value.find(s => s.id === activeID.value)?.connection)
function setTerminalRef(id: string, instance: unknown) {
  if (instance)
    terminalRefs.set(id, instance as InstanceType<typeof TerminalPanel>)
  else terminalRefs.delete(id)
}
let serial = 0
const active = computed(() =>
  sessions.value.find((s) => s.id === activeID.value),
)
provide(machineConnectionKey, computed(() => active.value?.connected ? active.value.connection?.shared_id || '' : ''))
function addTerminal(initialDirectory?: string) {
  if (!props.target.enabled) return
  const id = randomID()
  sessions.value.push({
    id,
    name: msg('text.4ca917e64e35', [++serial]),
    initialDirectory,
    status: msg('text.8be0ea36a9bb'),
    connected: false,
  })
  activeID.value = id
}
function directoryTerminal(path: string, newTerminal: boolean) {
  if (newTerminal) addTerminal(path)
  else if (active.value?.connected) terminalRefs.get(activeID.value)?.enterDirectory(path)
}
function closeTerminal(id: string) {
  const index = sessions.value.findIndex((s) => s.id === id)
  sessions.value = sessions.value.filter((s) => s.id !== id)
  if (activeID.value === id)
    activeID.value =
      sessions.value[Math.min(index, sessions.value.length - 1)]?.id || ''
}
const windowError = ref('')
async function openWindow() {
  windowError.value = ''
  try {
    await openMachineWindow(props.target.id, props.selection)
  } catch (error) {
    windowError.value =
      error instanceof Error ? error.message : msg('text.9e12a8aeec49')
  }
}
async function leave(edit = false) {
  if (leaving.value) return false
  leaving.value = true
  try {
    if (
      dirty.value ||
      transferring.value ||
      sessions.value.some((s) => s.connected || s.status === msg('text.8be0ea36a9bb'))
    ) {
      const result = await ask({
        title: msg('text.2296dd9d1046'),
        message: [
          dirty.value ? msg('text.67bd2092d3e1', [dirty.value]) : '',
          transferring.value ? msg('text.40b21fbbe010') : '',
          msg('text.16cc42f77eea'),
        ]
          .filter(Boolean)
          .join('\n'),
        choices: dirty.value ? [msg('text.d5f83d4ab7e1'), msg('text.ccb06b8a183b')] : [msg('text.de37806110a4')],
      })
      if (result.choice < 0) return false
      if (dirty.value && result.choice === 0 && !(await files.value?.saveAll()))
        return false
    }
    if (edit) emit('edit')
    else emit('close')
    return true
  } finally {
    leaving.value = false
  }
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (
    dirty.value ||
    transferring.value ||
    sessions.value.some((s) => s.connected)
  ) {
    event.preventDefault()
    event.returnValue = ''
  }
}
let stopDrag: (() => void) | undefined
function startDrag(event: PointerEvent) {
  const container = (event.currentTarget as HTMLElement).parentElement!
  const rect = container.getBoundingClientRect()
  const move = (event: PointerEvent) => {
    split.value = Math.max(
      25,
      Math.min(75, ((event.clientY - rect.top) / rect.height) * 100),
    )
  }
  stopDrag = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', stopDrag!)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', stopDrag, { once: true })
}
function resize() {
  if (window.innerWidth < 900) showTree.value = false
  if (window.innerWidth < 1200) showInfo.value = false
}
onMounted(() => {
  addTerminal()
  window.addEventListener('beforeunload', beforeUnload)
  window.addEventListener('resize', resize)
})
onBeforeUnmount(() => {
  document.title = previousTitle
  finish(-1)
  stopDrag?.()
  stopTreeDrag?.()
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('resize', resize)
})
defineExpose({ leave })
</script>
<template>
  <MappingWorkspace v-if="canManage" ref="mappings" :targets="[target]" :theme="theme" />
  <main class="machine-page" :data-theme="theme" :aria-label="t('text.791ca16886db')">
    <header class="machine-header">
      <strong :title="display(target.name)">{{ display(target.name) }}</strong
      ><button
        :aria-label="t('text.0bd2c8517649')"
        @click="connectionInfo?.show()"
        class="machine-address"
        :title="display(`${selectedUser}@${target.host}:${target.port}`)"
      >
        <span class="machine-address-text">{{ display(selectedUser) }}@{{
          display(target.host.includes(':') ? '[' + target.host + ']' : target.host)
        }}</span><ChevronIcon /></button
      ><span
        class="machine-connection"
        :class="{ online: active?.connected }"
        :title="display(active?.status)"
        >● <span>{{ display(active?.status || t('text.75f5cd694342')) }}</span></span
      >
      <div class="grow" />
      <nav>
        <button :title="t('feature.diagnostics')" @click="diagnostic?.show(target, selection || relayOptions(target).find(relayActive)?.id || 'server:default')">{{ t('feature.diagnostics') }}</button>
        <button
          class="sidebar-toggle"
          :aria-pressed="showTree"
          :title="t('text.4bb57cf05bdc')"
          @click="showTree = !showTree"
        >
          ▤</button
        ><button
          class="sidebar-toggle"
          :aria-pressed="showInfo"
          :title="t('text.d6cef07740ed')"
          @click="showInfo = !showInfo"
        >
          ⓘ</button>
        <div class="language-theme-controls">
        <button
          :aria-label="t('text.7a1604323e8b')"
          :title="display(theme === 'light' ? t('text.e75058317e93') : t('text.1818ec7cb982'))"
          @click="changeTheme"
        >
          {{ display(theme === 'light' ? '☾' : '☀') }}<span> {{ t('text.788db1cfec2a') }}</span>
        </button>
        <LanguageSwitcher />
        </div>
        <button
          v-if="!detached"
          :aria-label="t('text.67fb45b6bcaf')"
          :title="t('text.5518ed6840f4')"
          @click="openWindow"
        >
          ↗<span> {{ t('text.9daee40c1a6b') }}</span>
        </button>
        <button
          v-if="!detached"
          :aria-label="t('text.3fd47edce45b')"
          :title="t('text.60b8d5c498d6')"
          :disabled="leaving"
          @click="leave()"
        >
          ×<span> {{ t('text.3fd47edce45b') }}</span>
        </button>
      </nav>
    </header>
    <div v-if="windowError" class="machine-window-error" role="alert">
      {{ display(windowError)
      }}<button :aria-label="t('text.16074c413dc7')" @click="windowError = ''">×</button>
    </div>
    <div
      class="machine-grid"
      :class="{ 'hide-tree': !showTree, 'hide-info': !showInfo }"
      :style="{ '--editor-height': `${split}%`, '--tree-width': `${treeWidth}px` }"
    >
      <div v-if="showTree" class="tree-width-handle" role="separator" tabindex="0" aria-orientation="vertical" :aria-label="t('files.resizeDirectory')" :aria-valuenow="treeWidth" :aria-valuemin="180" :aria-valuemax="520" @pointerdown.prevent="dragTree" @keydown.left.prevent="setTreeWidth(treeWidth - 20)" @keydown.right.prevent="setTreeWidth(treeWidth + 20)" />
      <FileWorkspace
        ref="files"
        :target="target"
        :theme="theme"
        :show-tree="showTree"
        :terminal-connected="!!active?.connected"
        @terminal="directoryTerminal"
        @activity="
          (count, progress) => {
            dirty = count
            transferring = progress
          }
        "
      />
      <div
        class="machine-divider"
        role="separator"
        :aria-label="t('text.62401cfafdff')"
        aria-orientation="horizontal"
        :aria-valuenow="split"
        :aria-valuemin="25"
        :aria-valuemax="75"
        tabindex="0"
        @pointerdown.prevent="startDrag"
        @keydown.up.prevent="split = Math.max(25, split - 5)"
        @keydown.down.prevent="split = Math.min(75, split + 5)"
      >
        <span />
      </div>
      <section class="terminal-area machine-card">
        <div class="terminal-tabs-bar">
          <div class="terminal-tabs" role="tablist" :aria-label="t('text.528c3bb2e2d7')">
            <div
              v-for="session in sessions"
              :key="session.id"
              class="terminal-tab"
              :class="{ active: session.id === activeID }"
            >
              <button
                role="tab"
                :aria-selected="session.id === activeID"
                :title="display(session.status)"
                @click="activeID = session.id"
              >
                <i :class="{ online: session.connected }" />{{
                  display(session.name)
                }}</button
              ><button
                :aria-label="display(t('text.bd47bf947c86', [session.name]))"
                @click="closeTerminal(session.id)"
              >
                ×
              </button>
            </div>
            <button :aria-label="t('text.14ee5380fc86')" :disabled="!target.enabled" @click="addTerminal()">＋</button>
          </div>
          <div v-if="canManage" class="terminal-tabs-actions">
            <button class="terminal-mapping-button" :aria-label="t('text.c263674112e4')" :title="t('text.ecdd3ebeb7ac')" @click="mappings?.show(target)">{{ t('text.951c3aa87c99') }}</button>
          </div>
        </div>
        <TerminalPanel
          :selection="selection"
          :ref="(instance) => setTerminalRef(session.id, instance)"
          v-for="session in sessions"
          v-show="session.id === activeID"
          :key="session.id"
          :target="target"
          :active="session.id === activeID"
          :initial-directory="session.initialDirectory"
          :theme="theme"
          @connection="session.connection = $event"
          @status="
            (status, connected) => {
              session.status = status
              session.connected = connected
            }
          "
        />
        <div v-if="!sessions.length" class="terminal-empty">
          <p>{{ display(target.enabled ? t('text.0c1413f34b4a') : t('text.08e9c5ba2803')) }}</p>
          <button :disabled="!target.enabled" @click="addTerminal()">{{ t('text.14ee5380fc86') }}</button>
        </div>
        <TerminalQuickActions
          :can-manage="canManage"
          :target-id="target.id"
          :target-name="target.name"
          :connected="!!active?.connected"
          :has-terminal="!!active"
          @send="terminalRefs.get(activeID)?.sendCommand($event)"
          @clear="terminalRefs.get(activeID)?.clear()"
        />
      </section>
      <MachineInfo v-show="showInfo" :target="target" />
    </div>
    <ConnectionDiagnostic ref="diagnostic" :can-manage="canManage" />
    <ConnectionInfo
      ref="connectionInfo"
      :target="connectionTarget"
      :connection="currentConnection"
      :readonly="detached || !canManage"
      @edit="leave(true)"
    />
    <DecisionDialog :value="display(decision)" @finish="finish" />
    <DecisionDialog :value="display(connectionError)" @finish="dismissConnectionError" />
  </main>
</template>
