<script setup lang="ts">
import { ref } from 'vue'
import { api } from './api'
import { native } from './desktop'
import { t, display } from './i18n'
import { masterPasswordTransportError } from './security-api'
const admin = ref(''), password = ref(''), confirm = ref(''), error = ref(''), notice = ref(''), busy = ref(false)
function clear() { admin.value = password.value = confirm.value = ''; error.value = notice.value = '' }
async function save() {
 if(busy.value) return
 error.value = notice.value = ''
 if(password.value !== confirm.value) { error.value = t('feature.passwordMismatch'); return }
 busy.value = true
 try {
  const result = await api<{data:string;filename:string}>('/backups','POST',{admin_password:admin.value,password:password.value})
  if(native) { if(!await native.SaveBackup(result.data)) { notice.value=t('feature.cancelled'); return } }
  else {
   const raw = atob(result.data), bytes = new Uint8Array(raw.length)
   for(let i=0;i<raw.length;i++) bytes[i]=raw.charCodeAt(i)
   const url=URL.createObjectURL(new Blob([bytes],{type:'application/octet-stream'}))
   const link=document.createElement('a');link.href=url;link.download=result.filename;link.click();setTimeout(()=>URL.revokeObjectURL(url),10000)
  }
  notice.value=t('feature.backupReady')
 } catch(e) { error.value=e instanceof Error?e.message:String(e) }
 finally { admin.value=password.value=confirm.value='';busy.value=false }
}
defineExpose({clear})
</script>
<template>
 <section class="backup-panel" :aria-label="t('feature.backup')">
  <p class="hint">{{ t('feature.backupHint') }}</p>
  <form @submit.prevent="save">
   <label>{{ t('feature.backupAdmin') }}<input v-model="admin" type="password" autocomplete="current-password" required maxlength="72" :disabled="busy" /></label>
   <label>{{ t('feature.backupPassword') }}<input v-model="password" type="password" autocomplete="new-password" required minlength="12" maxlength="1024" :disabled="busy" /></label>
   <label>{{ t('feature.backupConfirm') }}<input v-model="confirm" type="password" autocomplete="new-password" required minlength="12" maxlength="1024" :disabled="busy" /></label>
   <p v-if="error" class="error" role="alert">{{ display(error) }}</p><p v-if="notice" role="status">{{ notice }}</p>
   <button class="primary" :disabled="busy || !!masterPasswordTransportError()">{{ t(busy?'feature.working':'feature.exportBackup') }}</button>
  </form>
  <p class="hint">{{ t('feature.restoreHint') }}</p>
  <pre>ssh-gateway backup-check backup.sgb
ssh-gateway -data ./data backup-restore backup.sgb</pre>
 </section>
</template>
<style scoped>.backup-panel{min-width:0}pre{white-space:pre-wrap;overflow-wrap:anywhere;font-size:11px}</style>
