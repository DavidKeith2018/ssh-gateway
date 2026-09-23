<script setup lang="ts">
import { ref, onUnmounted } from 'vue'
import { api, type Target } from './api'
import { t, type MessageKey, display } from './i18n'
import { relayOptions, relayLabel, relayActive, loginOptions } from './relay-access'
type Step={name:string;status:string;code:string;duration_ms:number}
const dialog=ref<HTMLDialogElement>(),target=ref<Target>(),selection=ref(''),job=ref(''),busy=ref(false),steps=ref<Step[]>([]),error=ref(''),done=ref(false)
let generation=0,timer:ReturnType<typeof setTimeout>|undefined
const names=['configuration','dns','tcp','ssh','fingerprint','authentication','relay']
function label(prefix:string,value:string){return t(('feature.'+prefix+value) as MessageKey)}
function state(name:string){return steps.value.find(s=>s.name===name)}
function show(value:Target,connection:string){target.value=value;selection.value=connection;steps.value=[];error.value='';done.value=false;dialog.value?.showModal()}
async function cancel(){generation++;clearTimeout(timer);const id=job.value;job.value='';busy.value=false;done.value=true;if(id)await api('/diagnostics/'+id,'DELETE').catch(()=>{});error.value=t('feature.cancelled')}
async function close(){await cancel();dialog.value?.close()}
async function run(){
 if(!target.value||busy.value)return
 const current=++generation;busy.value=true;done.value=false;error.value='';steps.value=[]
 try {
  const started=await api<{id:string}>(`/targets/${encodeURIComponent(target.value.id)}/diagnostics`,'POST',{connection:selection.value})
  if(current!==generation){await api('/diagnostics/'+started.id,'DELETE');return};job.value=started.id
  const poll=async()=>{try{const report=await api<{done:boolean;steps:Step[]}>('/diagnostics/'+started.id);if(current!==generation)return;steps.value=report.steps;done.value=report.done;busy.value=!report.done;if(!report.done)timer=setTimeout(poll,300);else job.value=''}catch(e){if(current===generation){error.value=e instanceof Error?e.message:String(e);busy.value=false}}}
  await poll()
 }catch(e){if(current===generation){error.value=e instanceof Error?e.message:String(e);busy.value=false}}
}
onUnmounted(()=>{void cancel()})
defineExpose({show})
</script>
<template>
 <dialog ref="dialog" class="diagnostic-dialog" aria-labelledby="diagnostic-heading" @cancel.prevent="close">
  <div class="dialog-heading"><h2 id="diagnostic-heading">{{ t('feature.diagnostics') }}</h2><button type="button" :aria-label="t('feature.close')" @click="close">×</button></div>
  <div v-if="target" class="dialog-content">
   <p>{{ target.name }}</p><p class="hint">{{ t('feature.diagnosticHint') }}</p>
   <label>{{ t('feature.connection') }}<span class="polished-select diagnostic-account-select"><select v-model="selection" :disabled="busy"><option v-for="relay in relayOptions(target)" :key="relay.id" :value="relay.id" :disabled="!relayActive(relay)">{{ display(relayLabel(target,relay)) }}</option><option v-for="login in loginOptions(target)" :key="login.id" :value="'server:'+login.id">{{ login.user }} · {{ t('feature.direct') }}</option></select></span></label>
   <ol class="diagnostic-steps"><li v-for="name in names.filter(n=>n!=='relay'||!selection.startsWith('server:'))" :key="name"><strong>{{ label('stage.',name) }}</strong><span>{{ state(name)?label('status.',state(name)!.status):t(done?'feature.skipped':'feature.waiting') }}</span><small v-if="state(name)">{{ state(name)!.duration_ms }} ms · {{ label('code.',state(name)!.code) }}</small></li></ol>
   <p v-if="error" class="error" role="alert">{{ display(error) }}</p>
   <button v-if="busy" @click="cancel">{{ t('feature.cancel') }}</button><button v-else class="primary" @click="run">{{ t('feature.startDiagnostic') }}</button>
  </div>
 </dialog>
</template>
<style scoped>.diagnostic-dialog{width:min(650px,calc(100vw - 24px));max-height:90dvh;overflow:auto}.diagnostic-account-select{width:100%;margin-top:2px}.diagnostic-account-select select{font-size:13px;min-height:42px}.diagnostic-steps{padding-left:20px}.diagnostic-steps li{padding:8px 0}.diagnostic-steps span{margin-left:12px}.diagnostic-steps small{display:block;overflow-wrap:anywhere}</style>
