<script setup lang="ts">
import { msg, t, display, errorMessage } from './i18n'
import { machineConnectionKey } from './machine-connection'
import { onPageLeave } from './page-lifecycle'
import { connectionQuery } from './connection-query'
import { inject } from 'vue'
import { connectionErrorsKey } from './connection-errors'
import ChevronIcon from './ChevronIcon.vue'
import { randomID } from './random-id'
import {
  ref,
  shallowRef,
  reactive,
  watch,
  onMounted,
  onBeforeUnmount,
  nextTick,
  computed,
} from 'vue'
import type { Target } from './api'
import { native, onDesktopEvent } from './desktop'
import {
  machineAPI,
  MachineError,
  parentPath,
  baseName,
  joinPath,
  errorText,
  type FileEntry,
  type Directory,
  type FileContent,
} from './machine-api'
import { monaco, language } from './monaco'
import FilePreview from './FilePreview.vue'
import FileTreeNode from './FileTreeNode.vue'
import FileEntryIcon from './FileEntryIcon.vue'
import DecisionDialog from './DecisionDialog.vue'
import { useDecision } from './decision'
const props = defineProps<{
  target: Target
  theme: string
  showTree: boolean
  terminalConnected?: boolean
}>()
const emit = defineEmits<{ activity: [dirty: number, transferring: boolean]; terminal: [path: string, newTerminal: boolean] }>()
const { decision, finish, ask } = useDecision()
const root = ref(''),
  selected = ref(''),
  childrenByPath = reactive<Record<string, FileEntry[]>>({}),
  expanded = reactive(new Set<string>()),
  loading = reactive(new Set<string>())
const systemRoot: FileEntry = {
  name: '/',
  path: '/',
  kind: 'directory',
  size: 0,
  modified: '',
}
const treeElement = ref<HTMLElement>()
const directoryError = ref('')
const connectionErrors = inject(connectionErrorsKey)
watch(directoryError, message => {
  if (message) connectionErrors?.report('directory', message)
  else connectionErrors?.recover('directory')
})
interface PreviewDocument {
  path: string
  content: { mime: string; data: string } | null
  error: string
}
const previews = ref<PreviewDocument[]>([])
const lastOpenedDirectory = ref('')
const previewPath = ref('')
const previewExtensions = /\.(png|jpe?g|gif|webp|bmp|ico|svg|avif|pdf|mp3|wav|ogg|flac|m4a|mp4|webm|mov)$/i
const error = ref(''),
  note = ref(''),
  menu = ref<{ x: number; y: number; entry: FileEntry | null } | null>(null)
const favoritesExpanded = ref(true)
const favorites = ref<FileEntry[]>([]),
  favoriteKey = `ssh-gateway:favorites:${props.target.id}`
const uploadInput = ref<HTMLInputElement>(),
  uploadDirectory = ref('')
const transfer = ref<{ label: string; progress: number } | null>(null)
let transferCancel: (() => void) | undefined
let controller = new AbortController()
let noteController = new AbortController()
const sharedConnection = inject(machineConnectionKey)
const sharedID = () => sharedConnection?.value || ''
const directoryKey = `ssh-gateway:last-directory:${props.target.id}`
const directoryCookie = `ssh-gateway-directory-${encodeURIComponent(props.target.id)}`
function savedDirectory() {
 try {
  const cookie = document.cookie.split('; ').find(value => value.startsWith(directoryCookie + '='))?.slice(directoryCookie.length + 1)
  const path = (cookie ? decodeURIComponent(cookie) : localStorage.getItem(directoryKey)) || '/'
  return path.startsWith('/') ? path : '/'
 } catch { return '/' }
}
function rememberDirectory(path: string) {
 try { localStorage.setItem(directoryKey, path) } catch {}
 if (path.length < 2000) document.cookie = `${directoryCookie}=${encodeURIComponent(path)}; Path=/; Max-Age=31536000; SameSite=Strict`
}
async function listWithFallback(path: string, fallback: boolean): Promise<Directory> {
 for (;;) {
  try { return await call<Directory>({ op: 'list', path }) }
  catch (error) {
   if (!fallback || !(error instanceof MachineError) || error.status !== 404 || !path || path === '/') throw error
   delete childrenByPath[path]
   expanded.delete(path)
   path = parentPath(path)
   delete childrenByPath[path]
  }
 }
}
function refreshCurrentDirectory() { return navigate(lastOpenedDirectory.value || root.value || savedDirectory(), true, true) }

let requestedPath = ''
const resolvedPaths = new Map<string, string>()
let directoryEpoch = 0,
  disposed = false
interface Document {
  path: string
  model: monaco.editor.ITextModel
  saved: string
  version: string
  view: monaco.editor.ICodeEditorViewState | null
  subscription: monaco.IDisposable
  saving: boolean
  local?: boolean
  loaded?: boolean
  refreshing?: boolean
  location?: string
}
const documents = shallowRef<Document[]>([]),
  activePath = ref(''),
  revision = ref(0),
  editorElement = ref<HTMLElement>()
let editor: monaco.editor.IStandaloneCodeEditor | undefined
const pending = new Map<string, Promise<void>>()
const active = computed(() =>
  documents.value.find((d) => d.path === activePath.value),
)
const dirtyCount = computed(() => {
  revision.value
  return documents.value.filter((d) => d.model.getValue() !== d.saved).length
})
function dirty(d: Document) {
  revision.value
  return d.model.getValue() !== d.saved
}
watch(
  [dirtyCount, transfer],
  () => emit('activity', dirtyCount.value, !!transfer.value),
  { immediate: true },
)
function call<T>(body: unknown) {
  if (!sharedID()) return Promise.reject(new Error(msg('text.01f6e4f38c77')))
  return machineAPI<T>(props.target.id, 'files', body, controller.signal, sharedID())
}
// 系统树始终以 / 为根，当前目录只决定定位和空白处文件操作的目标。
async function navigate(path = '/', reveal = true, fallback = false) {
  const epoch = ++directoryEpoch
  directoryError.value = ''
  loading.add('$root')
  try {
    const target = await listWithFallback(path, fallback)
    if (epoch !== directoryEpoch) return
    const ancestors = ['/']
    let cursor = ''
    for (const part of target.path.split('/').filter(Boolean)) {
      cursor += '/' + part
      ancestors.push(cursor)
    }
    const directories: { ancestor: string; data: Directory | null }[] = []
    for (const ancestor of ancestors) {
      if (disposed || epoch !== directoryEpoch || controller.signal.aborted) return
      const data = ancestor === target.path ? target : childrenByPath[ancestor] ? null : await call<Directory>({ op: 'list', path: ancestor })
      if (disposed || epoch !== directoryEpoch || controller.signal.aborted) return
      directories.push({ ancestor, data })
    }
    for (const { ancestor, data } of directories) {
      if (data) childrenByPath[ancestor] = data.entries
      expanded.add(ancestor)
    }
    lastOpenedDirectory.value = target.path
    root.value = target.path
    rememberDirectory(target.path)
    selected.value = target.path
    if (reveal) await revealSelected()
  } catch (e) {
    if (!disposed && epoch === directoryEpoch)
      directoryError.value = errorText(e)
  } finally {
    if (epoch === directoryEpoch) loading.delete('$root')
  }
}
async function revealSelected() {
  await nextTick()
  treeElement.value
    ?.querySelector('.tree-row.selected')
    ?.scrollIntoView({ block: 'nearest' })
}
function selectEntry(entry: FileEntry) {
  selected.value = entry.path
  root.value = entry.kind === 'directory' ? entry.path : parentPath(entry.path)
  rememberDirectory(root.value)
}
async function toggle(entry: FileEntry) {
  selectEntry(entry)
  if (expanded.has(entry.path)) {
    expanded.delete(entry.path)
    return
  }
  lastOpenedDirectory.value = entry.path
  expanded.add(entry.path)
  if (childrenByPath[entry.path] || loading.has(entry.path)) return
  loading.add(entry.path)
  directoryError.value = ''
  const epoch = directoryEpoch
  try {
    const data = await call<Directory>({ op: 'list', path: entry.path })
    if (epoch === directoryEpoch) childrenByPath[entry.path] = data.entries
  } catch (e) {
    if (epoch === directoryEpoch) {
      directoryError.value = errorText(e)
      expanded.delete(entry.path)
    }
  } finally {
    loading.delete(entry.path)
  }
}
async function refreshDirectory(path: string) {
  const epoch = directoryEpoch
  loading.add(path)
  try {
    const data = await call<Directory>({ op: 'list', path })
    if (epoch === directoryEpoch) childrenByPath[path] = data.entries
  } finally { loading.delete(path) }
}
let favoriteQueue = Promise.resolve()
function updateFavorites(op: 'read' | 'add' | 'remove' | 'import', entries: FileEntry[] = []) {
  const task = favoriteQueue.then(async () => {
    const result = await machineAPI<FileEntry[]>(props.target.id, 'favorites', {
      op, entries: entries.map(({ path, kind }) => ({ path, kind })),
    }, noteController.signal)
    if (!disposed) favorites.value = result
  })
  favoriteQueue = task.catch(() => {})
  return task
}
async function favorite(entry: FileEntry) {
  try {
    await favoriteQueue
    await updateFavorites(favorites.value.some(f => f.path === entry.path) ? 'remove' : 'add', [entry])
  } catch (e) { if (!disposed) error.value = errorText(e) }
}
async function loadFavorites() {
  try {
    await updateFavorites('read')
    let stored: unknown
    try { stored = JSON.parse(localStorage.getItem(favoriteKey) || '[]') } catch { return }
    if (!Array.isArray(stored)) return
    const legacy = stored.filter(e => typeof e?.path === 'string' && e.path.startsWith('/') && ['file', 'directory', 'link'].includes(e.kind))
    if (legacy.length) await updateFavorites('import', legacy)
    // Keep the old copy on failure, so migration can be retried safely.
    localStorage.removeItem(favoriteKey)
  } catch (e) { if (!disposed) error.value = errorText(e) }
}
async function locate(entry: FileEntry) {
  await navigate(
    entry.kind === 'directory' ? entry.path : parentPath(entry.path),
  )
  if (directoryError.value) return
  selected.value = entry.path
  await revealSelected()
  if (entry.kind !== 'directory') await open(entry)
}
function context(event: MouseEvent, entry: FileEntry | null) {
  selected.value = entry?.path || ''
  menu.value = {
    x: Math.min(event.clientX, window.innerWidth - 220),
    y: Math.max(52, Math.min(event.clientY, window.innerHeight - 410)),
    entry,
  }
}
function hideMenu() {
  menu.value = null
}
function activate(d: Document) {
  previewPath.value = ''
  if (!d.local) lastOpenedDirectory.value = parentPath(d.path)
  requestedPath = d.path
  if (active.value && editor) active.value.view = editor.saveViewState()
  activePath.value = d.path
  editor?.setModel(d.model)
  editor?.updateOptions({ readOnly: !!d.local && !d.loaded })
  if (d.view) editor?.restoreViewState(d.view)
  editor?.focus()
}
function activatePreview(doc: PreviewDocument) {
  if (!previewPath.value && active.value && editor) active.value.view = editor.saveViewState()
  requestedPath = doc.path
  previewPath.value = doc.path
  lastOpenedDirectory.value = parentPath(doc.path)
  error.value = ''
  note.value = ''
}
function closePreview(doc: PreviewDocument) {
  const index = previews.value.indexOf(doc)
  previews.value = previews.value.filter(item => item !== doc)
  if (previewPath.value !== doc.path) return
  const next = previews.value[Math.min(index, previews.value.length - 1)]
  if (next) activatePreview(next)
  else {
    previewPath.value = ''
    if (active.value) activate(active.value)
    else requestedPath = ''
  }
}
function documentName(doc: Document) {
  return doc.local ? msg('text.b778e307964f') : baseName(doc.path)
}
function documentCall<T>(doc: Document, body: Record<string, unknown>) {
  return doc.local
    ? machineAPI<T>(props.target.id, 'notes', body, noteController.signal)
    : call<T>({ ...body, path: doc.path })
}
async function refreshDocument(doc = active.value, confirmed = false) {
  if (!doc || doc.saving || doc.refreshing) return
  if (dirty(doc) && !confirmed) {
    const answer = await ask({
      title: msg('text.adea6b99fe8d'),
      message: msg('text.3da3b2883854', [documentName(doc)]),
      choices: [msg('text.847fc4ef7be9')],
    })
    if (answer.choice !== 0 || disposed) return
  }
  const localVersion = doc.model.getAlternativeVersionId()
  doc.refreshing = true
  revision.value++
  error.value = ''
  note.value = ''
  try {
    const data = await documentCall<FileContent>(doc, { op: 'read' })
    if (disposed || doc.model.isDisposed()) return
    if (doc.model.getAlternativeVersionId() !== localVersion) {
      error.value = msg('text.f073032be03a')
      return
    }
    doc.model.setValue(data.content)
    doc.saved = data.content
    doc.version = data.version
    doc.location = doc.local ? undefined : data.path
    doc.loaded = true
    if (active.value === doc) editor?.updateOptions({ readOnly: false })
  } catch (e) {
    if (!disposed) error.value = errorText(e)
  } finally {
    doc.refreshing = false
    revision.value++
  }
}
function openLocalNote() {
  const model = monaco.editor.createModel(
    '',
    'markdown',
    monaco.Uri.from({
      scheme: 'local-note',
      authority: props.target.id,
      path: '/note.md',
    }),
  )
  const doc: Document = {
    path: 'local-note:note.md',
    local: true,
    loaded: false,
    model,
    saved: '',
    version: '',
    view: null,
    saving: false,
    subscription: model.onDidChangeContent(() => revision.value++),
  }
  documents.value = [doc, ...documents.value]
  activate(doc)
  void refreshDocument(doc)
}
async function open(entry: FileEntry) {
  if (entry.kind === 'directory') {
    await navigate(entry.path)
    return
  }
  lastOpenedDirectory.value = parentPath(entry.path)
  if (previewExtensions.test(entry.path)) {
    let doc = previews.value.find(item => item.path === entry.path)
    if (doc) { activatePreview(doc); return }
    previews.value.push({ path: entry.path, content: null, error: '' })
    doc = previews.value[previews.value.length - 1]!
    activatePreview(doc)
    try {
      const result = await call<{ path: string; mime: string; data: string }>({ op: 'preview', path: entry.path })
      if (!disposed && previews.value.includes(doc)) doc.content = result
    } catch (e) {
      if (!disposed && previews.value.includes(doc)) doc.error = errorText(e)
    }
    return
  }
  previewPath.value = ''
  requestedPath = entry.path
  const existing = documents.value.find(
    (d) => d.path === (resolvedPaths.get(entry.path) || entry.path),
  )
  if (existing) {
    activate(existing)
    return
  }
  if (pending.has(entry.path)) {
    await pending.get(entry.path)
    const loaded = documents.value.find(
      (d) => d.path === (resolvedPaths.get(entry.path) || entry.path),
    )
    if (loaded && requestedPath === entry.path) activate(loaded)
    return
  }
  const task = (async () => {
    error.value = ''
    try {
      const data = await call<FileContent>({ op: 'read', path: entry.path })
      if (disposed) return
      resolvedPaths.set(entry.path, data.path)
      const duplicate = documents.value.find((d) => d.path === data.path)
      if (duplicate) {
        if (requestedPath === entry.path) activate(duplicate)
        return
      }
      const model = monaco.editor.createModel(
        data.content,
        language(data.path),
        monaco.Uri.from({
          scheme: 'ssh',
          authority: props.target.id,
          path: data.path,
        }),
      )
      const doc: Document = {
        path: data.path,
        model,
        saved: data.content,
        version: data.version,
        view: null,
        subscription: model.onDidChangeContent(() => revision.value++),
        saving: false,
      }
      documents.value = [...documents.value, doc]
      if (requestedPath === entry.path) activate(doc)
      revision.value++
    } catch (e) {
      error.value = errorText(e)
      if (e instanceof MachineError && [413, 422].includes(e.status)) {
        const result = await ask({
          title: msg('text.bfa12df5657d'),
          message: error.value,
          choices: [msg('text.3926d1bb98be')],
        })
        if (result.choice === 0) await download(entry.path)
      }
    }
  })()
  pending.set(entry.path, task)
  try {
    await task
  } finally {
    pending.delete(entry.path)
  }
}
async function save(doc = active.value, overwrite = false): Promise<boolean> {
  if (!doc || doc.saving || doc.refreshing || (doc.local && !doc.loaded))
    return false
  doc.saving = true
  revision.value++
  const content = doc.model.getValue()
  error.value = ''
  try {
    const result = await documentCall<{ version: string }>(doc, {
      op: 'write',
      content,
      version: doc.version,
      overwrite,
    })
    doc.saved = content
    doc.version = result.version
    note.value = ''
    return true
  } catch (e) {
    error.value = errorText(e)
    if (e instanceof MachineError && e.status === 409) {
      doc.saving = false
      const answer = await ask({
        title: msg('text.12e5b9314b5f'),
        message: msg('text.51ae249cc93d', [documentName(doc)]),
        choices: [
          doc.local ? msg('text.57b9d705ba84') : msg('text.7d1a1268f196'),
          msg('text.e6acc7ea5d68'),
        ],
      })
      if (answer.choice === 0) return await save(doc, true)
      if (answer.choice === 1) {
        await refreshDocument(doc, true)
      }
    }
    return false
  } finally {
    doc.saving = false
    revision.value++
  }
}
function disposeDoc(doc: Document) {
  const index = documents.value.indexOf(doc)
  documents.value = documents.value.filter((d) => d !== doc)
  if (activePath.value === doc.path) {
    editor?.setModel(null)
    activePath.value = ''
    const next = documents.value[Math.min(index, documents.value.length - 1)]
    if (next) activate(next)
  }
  doc.subscription.dispose()
  doc.model.dispose()
  revision.value++
}
async function closeDoc(doc: Document) {
  if (doc.local || doc.saving || doc.refreshing) return
  if (dirty(doc)) {
    const answer = await ask({
      title: msg('text.493941a2b199'),
      message: doc.path,
      choices: [msg('text.ed8bcd6d9ed6'), msg('text.0711cc459572')],
    })
    if (answer.choice < 0) return
    if (answer.choice === 0 && (!(await save(doc)) || dirty(doc))) return
  }
  disposeDoc(doc)
}
async function create(directory: boolean, destination: string) {
  const result = await ask({
    title: directory ? msg('text.84244abc71de') : msg('text.6ddd1eaccab1'),
    message: msg('text.82220682a6fa', [destination]),
    choices: [msg('text.50ef2f4cf6a4')],
    input: '',
  })
  if (result.choice < 0) return
  const name = result.text.trim()
  if (!name || name === '.' || name === '..' || /[\/\0]/.test(name)) {
    error.value = msg('text.c39d8e23e029')
    return
  }
  try {
    await call({ op: 'create', path: joinPath(destination, name), directory })
    await refreshDirectory(destination)
  } catch (e) {
    error.value = errorText(e)
  }
}
async function remove(entry: FileEntry) {
  const affected = documents.value.filter(
    (d) =>
      !d.local &&
      !entry.link &&
      (d.path === entry.path || d.path.startsWith(entry.path + '/')),
  )
  if (affected.some((d) => d.saving)) {
    error.value = msg('text.506b31a8e621')
    return
  }
  const answer = await ask({
    title: msg('text.b5b5231a4d76') + (entry.kind === 'directory' ? msg('text.52daa71ebc31') : msg('text.39932f24fe11')),
    message: `${entry.path}${entry.link ? msg('text.d5bb882a1961') : entry.kind === 'directory' ? msg('text.37278acec02d') : ''}${affected.some((d) => dirty(d)) ? msg('text.0bc92eb653c7') : ''}`,
    choices: [msg('text.a3ea3c17b401')],
  })
  if (answer.choice < 0) return
  try {
    await call({ op: 'delete', path: entry.path })
    affected.forEach(disposeDoc)
    await updateFavorites('remove', favorites.value.filter(
      f => f.path === entry.path || f.path.startsWith(entry.path + '/'),
    ))
    await refreshDirectory(parentPath(entry.path))
  } catch (e) {
    error.value = errorText(e)
  }
}
async function menuAction(action: string) {
  const item = menu.value?.entry
  const destination =
    item?.kind === 'directory'
      ? item.path
      : item
        ? parentPath(item.path)
        : root.value
  hideMenu()
  if (!destination) return
  if (action === 'new-terminal' || action === 'enter-directory') emit('terminal', destination, action === 'new-terminal')
  if (action === 'favorite' && item) favorite(item)
  if (action === 'open' && item) await open(item)
  if (action === 'download' && item) await download(item.path)
  if (action === 'delete' && item) await remove(item)
  if (action === 'file' || action === 'directory')
    await create(action === 'directory', destination)
  if (action === 'refresh')
    try {
      directoryError.value = ''
      await refreshDirectory(destination)
    } catch (e) {
      directoryError.value = errorText(e)
    }
  if (action === 'upload') {
    uploadDirectory.value = destination
    if (native) await nativeTransfer('upload', destination)
    else uploadInput.value?.click()
  }
}
async function nativeTransfer(mode: 'upload' | 'download', path: string) {
  if (transfer.value) return
  const id = randomID()
  transfer.value = {
    label: mode === 'upload' ? msg('text.bb5caa9c5976') : msg('text.3926d1bb98be'),
    progress: 0,
  }
  transferCancel = () => {
    void native!.CancelWork(id)
  }
  const off = onDesktopEvent('file-progress', (event) => {
    if (event.id === id && transfer.value)
      transfer.value.progress = Number(event.data) || 0
  })
  let askingOverwrite = false
  const offConfirm = onDesktopEvent('file:confirm-overwrite', async event => {
    if (event.id !== id) return
    askingOverwrite = true
    const answer = await ask({ title: msg('text.e64e8419ca18'), message: event.data || path, choices: [msg('text.e4b715a60c85')] })
    askingOverwrite = false
    await native!.ConfirmOverwrite(id, answer.choice === 0).catch(e => { error.value = errorText(e) })
  })
  try {
    await native!.TransferFileConnection(id, props.target.id, mode, path, connectionQuery(props.target.id, '', sharedID()))
    note.value = msg('text.305eef7a84a9')
    if (mode === 'upload') await refreshDirectory(path)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    offConfirm()
    if (askingOverwrite) finish(-1, '')
    off()
    transfer.value = null
    transferCancel = undefined
  }
}
async function uploadFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || transfer.value) return
  if (file.size > 1024 ** 3) {
    error.value = msg('text.2f18db41f18c')
    return
  }
  const destination = uploadDirectory.value
  let overwrite = false
  try {
    const data = await call<Directory>({ op: 'list', path: destination })
    if (data.entries.some((e) => e.name === file.name)) {
      const answer = await ask({
        title: msg('text.bbd2a22f3a54'),
        message: joinPath(destination, file.name),
        choices: [msg('text.e4b715a60c85')],
      })
      if (answer.choice < 0) return
      overwrite = true
    }
  } catch (e) {
    error.value = errorText(e)
    return
  }
  transfer.value = { label: msg('text.925f3a4fb62d', [file.name]), progress: 0 }
  const xhr = new XMLHttpRequest()
  transferCancel = () => xhr.abort()
  try {
    await new Promise<void>((resolve, reject) => {
      xhr.open(
        'PUT',
        `/api/targets/${encodeURIComponent(props.target.id)}/transfer?path=${encodeURIComponent(joinPath(destination, file.name))}&overwrite=${overwrite}${connectionQuery(props.target.id, '&', sharedID())}`,
      )
      xhr.setRequestHeader('Content-Type', 'application/octet-stream')
      xhr.upload.onprogress = (e) => {
        if (transfer.value && e.lengthComputable)
          transfer.value.progress = Math.round((e.loaded / e.total) * 100)
      }
      xhr.onload = () => {
        if (xhr.status === 401)
          window.dispatchEvent(new Event('session-expired'))
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve()
          return
        }
        let message = msg('text.f9338a3940df', [xhr.status])
        try {
          message = errorMessage(JSON.parse(xhr.responseText))
        } catch {}
        reject(new Error(message))
      }
      xhr.onerror = () => reject(new Error(msg('text.43180a174bfa')))
      xhr.onabort = () => reject(new Error(msg('text.2e6b43128bf1')))
      xhr.send(file)
    })
    note.value = msg('text.52c6b886b559')
    await refreshDirectory(destination)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    transfer.value = null
    transferCancel = undefined
  }
}
async function download(path: string) {
  if (native) {
    await nativeTransfer('download', path)
    return
  }
  const link = document.createElement('a')
  link.href = `/api/targets/${encodeURIComponent(props.target.id)}/transfer?path=${encodeURIComponent(path)}${connectionQuery(props.target.id, '&', sharedID())}`
  link.download = baseName(path)
  document.body.append(link)
  link.click()
  link.remove()
  note.value = msg('text.2b3611793ca2')
}
async function saveAll() {
  for (const doc of documents.value) {
    if (doc.saving) return false
    if (dirty(doc) && !(await save(doc))) return false
  }
  return !documents.value.some((d) => dirty(d))
}
defineExpose({
  saveAll,
  dirtyCount,
  transferring: computed(() => !!transfer.value),
})
onMounted(async () => {
  void loadFavorites()
  await nextTick()
  editor = monaco.editor.create(editorElement.value!, {
    automaticLayout: true,
    editContext: false,
    // Monaco 0.55 的词语高亮延迟任务在快速切 model 时会产生未处理取消。
    occurrencesHighlight: 'off',
    minimap: { enabled: false },
    fontSize: 14,
    scrollBeyondLastLine: false,
    model: null,
    theme: props.theme === 'dark' ? 'vs-dark' : 'vs',
  })
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
    void save()
  })
  openLocalNote()
  if (sharedID()) void navigate(root.value || savedDirectory(), true, true)
  window.addEventListener('click', hideMenu)
  window.addEventListener('keydown', escapeMenu)
})
watch(() => sharedID(), () => {
  ++directoryEpoch
  controller.abort()
  controller = new AbortController()
  transferCancel?.()
  for (const path of Object.keys(childrenByPath)) delete childrenByPath[path]
  expanded.clear()
  loading.clear()
  directoryError.value = ''
  if (sharedID()) void navigate(root.value || savedDirectory(), true, true)
})
function escapeMenu(e: KeyboardEvent) {
  if (e.key === 'Escape') hideMenu()
}
watch(
  () => props.theme,
  (theme) => monaco.editor.setTheme(theme === 'dark' ? 'vs-dark' : 'vs'),
)
onPageLeave(() => {
  disposed = true
  noteController.abort()
  ++directoryEpoch
  controller.abort()
  transferCancel?.()
}, () => {
  disposed = false
  noteController = new AbortController()
  controller = new AbortController()
  loading.clear()
})
onBeforeUnmount(() => {
  disposed = true
  noteController.abort()
  controller.abort()
  transferCancel?.()
  finish(-1)
  window.removeEventListener('click', hideMenu)
  window.removeEventListener('keydown', escapeMenu)
  editor?.dispose()
  documents.value.forEach((d) => {
    d.subscription.dispose()
    d.model.dispose()
  })
})
</script>
<template>
  <aside v-show="showTree" class="files-left machine-card">
    <button class="favorites-heading" :aria-expanded="favoritesExpanded" :aria-label="t('text.a1afe0530ef9')" @click="favoritesExpanded = !favoritesExpanded">
      <ChevronIcon :direction="favoritesExpanded ? 'down' : 'right'" /><strong>{{ t('text.a1afe0530ef9') }}</strong><small>{{ display(favorites.length) }} {{ t('text.49ccde43a154') }}</small>
    </button>
    <div v-show="favoritesExpanded" class="favorites">
      <p v-if="!favorites.length" class="empty-hint">
        {{ t('text.bd96cada9d4a') }}
      </p>
      <div v-for="item in favorites" :key="item.path" class="favorite-row">
        <button :title="display(item.path)" @click="locate(item)">
          <FileEntryIcon :kind="item.kind" /><span class="truncate">{{ display(item.name) }}</span></button
        ><button :aria-label="display(t('text.8e8af5df6014', [item.name]))" @click="favorite(item)">
          ×
        </button>
      </div>
    </div>
    <div class="system-tree-heading">
      <strong>{{ t('text.a94ee187b654') }}</strong
      ><button
        :aria-label="t('text.6e47a48c0a2d')"
        :title="t('text.38e44b678ede')"
        @click="navigate('')"
      >
        ⌂</button
      ><button
        :aria-label="t('text.d093db003b48')"
        :title="t('text.a4e0f339f3ad')"
        @click="navigate('/')"
      >
        /
      </button>
      <button :aria-label="t('files.refreshDirectory')" :title="t('files.refreshDirectory')" :disabled="!sharedID() || loading.has('$root')" @click="refreshCurrentDirectory">↻</button>
    </div>
    <p v-if="directoryError && !connectionErrors" class="tree-error" role="alert">
      {{ display(directoryError) }}<button @click="navigate(root || '/')">{{ t('text.b8784c8dd563') }}</button>
    </p>
    <div
      ref="treeElement"
      class="tree-scroll"
      @contextmenu.prevent="context($event, null)"
    >
      <p v-if="loading.has('$root')" class="tree-loading">{{ t('text.51958e139c9c') }}</p>
      <p v-if="!sharedID()" class="empty-hint">{{ t('text.01f6e4f38c77') }}</p>
      <ul v-else role="tree" :aria-label="t('text.a9823260266d')">
        <FileTreeNode
          :entry="systemRoot"
          :children-by-path="childrenByPath"
          :expanded="expanded"
          :selected="selected"
          :loading="loading"
          @select="selectEntry"
          @open="open"
          @toggle="toggle"
          @menu="context"
        />
      </ul>
    </div>
    <div v-if="transfer" class="transfer-progress">
      <span>{{ display(transfer.label) }} · {{ display(transfer.progress) }}%</span
      ><progress :value="display(transfer.progress)" max="100" /><button
        @click="transferCancel?.()"
      >
        {{ t('text.ce29be0644d0') }}
      </button>
    </div>
    <footer class="file-footer">{{ t('text.58c16be2350a') }}</footer>
    <input ref="uploadInput" type="file" hidden @change="uploadFile" />
  </aside>
  <section class="file-editor machine-card" :aria-label="t('text.069c3fee7005')">
    <div class="editor-tabbar">
      <div class="file-tabs" role="tablist" :aria-label="t('text.6280ea55b0a0')">
        <div
          v-for="doc in documents"
          :key="doc.path"
          class="file-tab"
          :class="{ active: !previewPath && activePath === doc.path }"
        >
          <button
            role="tab"
            :aria-selected="!previewPath && activePath === doc.path"
            :title="display(doc.location || doc.path)"
            @click="activate(doc)"
          >
            ▤ {{ display(documentName(doc)) }}
            <span v-if="dirty(doc)" class="dirty-dot">●</span></button
          ><button
            v-if="!doc.local"
            :aria-label="display(t('text.60658f0d7dc6', [baseName(doc.path)]))"
            :disabled="doc.saving || doc.refreshing"
            @click="closeDoc(doc)"
          >
            ×
          </button>
        </div>
        <div v-for="doc in previews" :key="doc.path" class="file-tab" :class="{ active: previewPath === doc.path }">
          <button role="tab" :aria-selected="previewPath === doc.path" :title="doc.path" @click="activatePreview(doc)">{{ baseName(doc.path) }}</button>
          <button :aria-label="t('files.closePreview')" :title="doc.path" @click="closePreview(doc)">×</button>
        </div>
      </div>
      <div class="editor-actions" v-if="!previewPath">
        <button
          :aria-label="t('text.adea6b99fe8d')"
          :title="t('text.17dc05229494')"
          :disabled="!active || active.saving || active.refreshing"
          @click="refreshDocument()"
        >
          ↻
        </button>
        <button
          v-if="active && (dirty(active) || active.saving)"
          class="primary"
          :disabled="active.saving || active.refreshing"
          @click="save()"
        >
          {{ display(active.saving ? t('text.ff509c9ba052') : t('text.938f2dd3e5ce')) }}
        </button>
      </div>
    </div>
    <div v-if="error" class="file-message error" role="alert">
      {{ display(error)
      }}<button :aria-label="t('text.76d59174e47d')" @click="error = ''">×</button>
    </div>
    <div v-else-if="note" class="file-message" role="status">
      {{ display(note) }}<button :aria-label="t('text.453274a6f188')" @click="note = ''">×</button>
    </div>
    <template v-for="doc in previews" :key="doc.path">
      <FilePreview v-show="previewPath === doc.path" :path="doc.path" :preview="doc.content" :failed="!!doc.error" @download="download(doc.path)" />
    </template>
    <div v-show="!previewPath" class="editor-container">
      <div ref="editorElement" class="monaco-host" />
      <div v-if="active?.local && !active.loaded" class="editor-empty">
        {{
          display(active.refreshing
            ? t('text.3534428ca16c')
            : t('text.a05f1ff0d7f4'))
        }}
      </div>
    </div>
    <footer class="editor-status">
      <span class="truncate" :title="display(previewPath || active?.location || activePath)">{{
        display(previewPath || (active?.local ? t('text.c9140b537cd1') : activePath || 'UTF-8'))
      }}</span
      ><span>{{ display(dirtyCount ? t('text.7ff0b8cf0ff5', [dirtyCount]) : t('text.1bd91a7d0c53')) }}</span>
    </footer>
  </section>
  <div
    v-if="menu"
    class="file-context-menu"
    role="menu"
    :style="{ left: `${menu.x}px`, top: `${menu.y}px` }"
    @click.stop
  >
    <small class="truncate" :title="display(menu.entry?.path || root)">{{
      display(menu.entry?.name || t('text.257f4442af73'))
    }}</small
    ><button role="menuitem" @click="menuAction('refresh')">{{ t('text.ae93d47bc513') }}</button>
    <button role="menuitem" :disabled="!target.enabled" @click="menuAction('new-terminal')">{{ t('text.fd5257a55b62') }}</button>
    <button role="menuitem" :disabled="!terminalConnected" @click="menuAction('enter-directory')">{{ t('text.a573c613a8c5') }}</button>
    <hr role="separator" />
    <button
      v-if="menu.entry && menu.entry.kind !== 'directory'"
      role="menuitem"
      @click="menuAction('open')"
    >{{ t('text.4c8a4e3da39e') }}</button
    ><button role="menuitem" @click="menuAction('upload')">
      {{ t('text.e53737c32e6c') }}</button
    ><button
      v-if="menu.entry && menu.entry.kind !== 'directory'"
      role="menuitem"
      @click="menuAction('download')"
    >
      {{ t('text.3926d1bb98be') }}</button
    ><button role="menuitem" @click="menuAction('file')">{{ t('text.6ddd1eaccab1') }}</button
    ><button role="menuitem" @click="menuAction('directory')">{{ t('text.84244abc71de') }}</button
    ><button v-if="menu.entry" role="menuitem" @click="menuAction('favorite')">
      {{
        display(favorites.some((f) => f.path === menu!.entry!.path)
          ? t('text.dca60869e7d2')
          : t('text.991ab30a8e4d'))
      }}</button
    ><button
      v-if="menu.entry"
      role="menuitem"
      class="danger-text"
      @click="menuAction('delete')"
    >
      {{ t('text.2f9daa828907') }}
    </button>
  </div>
  <DecisionDialog :value="display(decision)" @finish="finish" />
</template>
