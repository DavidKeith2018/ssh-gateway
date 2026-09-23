import { Events } from '@wailsio/runtime'
import * as App from './bindings/ssh-gateway-desktop/app'
import type { Native, TerminalEvent } from './desktop'

export const native: Native = App

export function onDesktopEvent(name: string, callback: (event: TerminalEvent) => void): () => void {
  return Events.On(name, event => callback(event.data))
}
