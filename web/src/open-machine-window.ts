import { msg, locale } from './i18n'
import { native } from './desktop'

export async function openMachineWindow(targetID: string, selection = '') {
  if (native) {
    if (selection) await native.OpenMachineConnectionWindow(targetID, selection)
    else await native.OpenMachineWindow(targetID)
    return
  }
  const url = new URL(window.location.href)
  url.search = ''
  url.hash = ''
  url.searchParams.set('lang', locale.value)
  url.searchParams.set('machine', targetID)
  url.searchParams.set('window', '1')
  if (selection) url.searchParams.set('connection', selection)
  const popup = window.open(url.toString(), '_blank', 'popup,width=1440,height=960')
  if (!popup) throw new Error(msg('text.98adbd7e940e'))
  popup.opener = null
}
