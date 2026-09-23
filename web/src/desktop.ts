import { watch } from 'vue'
import { locale, errorMessage, localizeReply, type LocalizedMessage } from './i18n'
export interface DesktopSettings { external: boolean; port: number; connect_host: string }
export interface DesktopInfo { ready: boolean; needs_setup: boolean; data_dir: string; listen: string; error: string; active: number; settings: DesktopSettings; version: string }
export interface TerminalEvent extends LocalizedMessage { id: string; type: string; data?: string; message?: string }
export interface UpdateInfo { configured: boolean; current_version: string; version: string; available: boolean; url: string; notes: string }
export interface Native {
  RememberedLogin(): Promise<{username:string;password:string}>
  SaveRememberedLogin(username:string,password:string): Promise<void>
  ForgetRememberedLogin(): Promise<void>
  SetLocale(locale: string): Promise<void>
 CheckUpdate(): Promise<UpdateInfo>
  FrontendReady(): Promise<void>
  CancelQuit(): Promise<void>
  ConfirmOverwrite(id: string, confirmed: boolean): Promise<void>
  OpenExternalURL(address: string): Promise<void>
  OpenMachineWindow(target: string): Promise<void>
  OpenMachineConnectionWindow(target: string, selection: string): Promise<void>
  MachineCallConnection(id: string, target: string, operation: string, body: string, query: string): Promise<{ status: number; data: unknown }>
  TransferFileConnection(id: string, target: string, mode: string, path: string, query: string): Promise<void>
  MachineCall(id: string, target: string, operation: string, body: string): Promise<{ status: number; data: unknown }>
  CancelWork(id: string): Promise<void>
  TransferFile(id: string, target: string, mode: string, path: string): Promise<void>
  SaveBackup(data: string): Promise<boolean>
  Quit(): Promise<void>
  ConfirmQuit(): Promise<void>
  Info(): Promise<DesktopInfo>
  Call(method: string, path: string, body: string): Promise<{ status: number; data: unknown }>
  SaveSettings(settings: DesktopSettings, confirmed: boolean): Promise<void>
  ChooseDataDir(): Promise<void>
  OpenTerminalConnection(id: string, target: string, selection: string): Promise<void>
  OpenTerminal(id: string, target: string): Promise<void>
  TerminalInput(id: string, data: string, columns: number, rows: number): Promise<void>
  AckTerminal(id: string): Promise<void>
  CloseTerminal(id: string): Promise<void>
}
export let native: Native | undefined
let subscribe: ((name: string, callback: (event: TerminalEvent) => void) => () => void) | undefined

export async function initialiseDesktop() {
  if (import.meta.env.MODE !== 'desktop') return
  const bridge = await import('./desktop-v3')
  native = new Proxy(bridge.native, {
    get(target, property: keyof Native) {
      const method = target[property]
      return (...args: unknown[]) => (method as (...args: unknown[]) => Promise<unknown>)(...args)
        .then(result => property === 'Info' ? localizeReply(result) : result)
        .catch(error => { throw new Error(errorMessage(error)) })
    },
  })
  subscribe = bridge.onDesktopEvent
  await native.SetLocale(locale.value)
  watch(locale, value => { void native?.SetLocale(value).catch(error => console.error(error)) })
}

export function onDesktopEvent(name: string, callback: (event: TerminalEvent) => void): () => void {
  return subscribe?.(name, callback) ?? (() => {})
}
