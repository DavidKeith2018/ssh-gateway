<script setup lang="ts">
import { ref } from 'vue'
import { api } from './api'
import { t, display, errorMessage, type LocalizedMessage } from './i18n'
type Login = {id?:string;user:string;auth_type:string;target_password?:string;target_private_key?:string;target_key_passphrase?:string}
type Input = Login & {name:string;host:string;port:number;tags:string[];allowed_sources:string[];host_fingerprint:string;logins?:Login[];[key:string]:unknown}
type Row = {input:Input;identity_file?:string;issues:string[];issues_i18n?:LocalizedMessage[];duplicate:boolean;blocked:boolean;selected:boolean;confirmed:boolean}
const emit=defineEmits<{saved:[]}>()
const dialog=ref<HTMLDialogElement>(), format=ref('ssh_config'), content=ref(''),rows=ref<Row[]>([]),busy=ref(false),error=ref(''),notice=ref('')
function clear(){rows.value=[];content.value='';error.value=notice.value=''}
function show(){clear();dialog.value?.showModal()}
function close(){if(!busy.value){dialog.value?.close();clear()}}
async function loadFile(event:Event){const input=event.target as HTMLInputElement,file=input.files?.[0];input.value='';if(!file)return;if(file.size>900000){error.value=t('feature.fileTooLarge');return}content.value=await file.text();rows.value=[]}
async function preview(){busy.value=true;error.value=notice.value='';try{rows.value=(await api<Row[]>('/import/preview','POST',{format:format.value,content:content.value})).map(row=>({...row,selected:!row.duplicate&&!row.blocked,confirmed:false}));content.value=''}catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=false}}
function accounts(row:Row):Login[]{return row.input.logins?.length?row.input.logins:[row.input]}
function changeAuth(login:Login){
 if(login.auth_type==='password'){login.target_private_key='';login.target_key_passphrase=''}
 else {login.target_password=''}
}
async function loadKey(event:Event,login:Login){const input=event.target as HTMLInputElement,file=input.files?.[0];input.value='';if(!file)return;if(file.size>32768){error.value=t('feature.keyTooLarge');return}login.target_private_key=await file.text()}
async function probe(row:Row){busy.value=true;error.value='';try{const result=await api<{fingerprint:string}>('/probe','POST',{host:row.input.host,port:Number(row.input.port)});row.input.host_fingerprint=result.fingerprint;row.confirmed=false}catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=false}}
async function commit(){
 const selected=rows.value.filter(row=>row.selected&&!row.duplicate&&!row.blocked)
 if(!selected.length || selected.some(row=>!row.confirmed)){error.value=t('feature.confirmFingerprint');return}
 busy.value=true;error.value=notice.value=''
 try{const result=await api<{ids:string[]}>('/import/commit','POST',{rows:selected.map(row=>row.input)});clear();notice.value=t('feature.imported',[result.ids.length]);emit('saved')}
 catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=false}
}
defineExpose({show})
</script>
<template>
 <dialog ref="dialog" class="import-dialog" aria-labelledby="import-heading" @cancel.prevent="close" @close="clear">
  <div class="dialog-heading"><h2 id="import-heading">{{ t('feature.import') }}</h2><button type="button" class="close-button" :disabled="busy" :aria-label="t('feature.close')" @click="close">×</button></div>
  <div class="dialog-content">
   <p class="hint">{{ t('feature.importHint') }}</p>
   <form v-if="!rows.length" @submit.prevent="preview">
    <label>{{ t('feature.format') }}<span class="polished-select"><select v-model="format"><option value="ssh_config">SSH config</option><option value="json">JSON</option></select></span></label>
    <label>{{ t('feature.chooseFile') }}<input type="file" :disabled="busy" @change="loadFile" /></label>
    <label>{{ t('feature.importContent') }}<textarea v-model="content" required rows="7" :disabled="busy" /></label>
    <button class="primary" :disabled="busy">{{ t('feature.preview') }}</button>
   </form>
   <form v-else @submit.prevent="commit">
    <fieldset v-for="(row,index) in rows" :key="index" :disabled="busy" class="import-row">
     <legend><label class="inline-checkbox"><input v-model="row.selected" type="checkbox" :disabled="row.duplicate||row.blocked" />{{ row.input.name || index+1 }}</label></legend>
     <p v-if="row.duplicate">{{ t('feature.duplicate') }}</p><p v-if="row.blocked" class="error">{{ t('feature.unsupported') }}</p>
     <ul v-if="row.issues.length" class="hint"><li v-for="(issue,i) in row.issues" :key="i">{{ display(errorMessage({error:issue,...row.issues_i18n?.[i]})) }}</li></ul>
     <template v-if="row.selected&&!row.duplicate&&!row.blocked">
      <p class="hint">{{ t('feature.previewIssues') }}</p>
      <div class="import-fields"><label>{{ t('feature.name') }}<input v-model="row.input.name" required /></label><label>{{ t('feature.host') }}<input v-model="row.input.host" required @input="row.confirmed=false" /></label><label>{{ t('feature.port') }}<input v-model.number="row.input.port" type="number" min="1" max="65535" required @input="row.confirmed=false" /></label></div>
      <p v-if="row.identity_file" class="hint">{{ t('feature.identityHint',[row.identity_file]) }}</p>
      <div v-for="(login,i) in accounts(row)" :key="i" class="import-fields">
       <label>{{ t('feature.user') }}<input v-model="login.user" required /></label>
       <label>{{ t('feature.authType') }}<span class="polished-select"><select v-model="login.auth_type" @change="changeAuth(login)"><option value="password">{{ t('feature.password') }}</option><option value="private_key">{{ t('feature.privateKey') }}</option></select></span></label>
       <label v-if="login.auth_type==='password'">{{ t('feature.password') }}<input v-model="login.target_password" type="password" autocomplete="new-password" required /></label>
       <template v-else><label>{{ t('feature.privateKey') }}<input type="file" @change="loadKey($event,login)" /><span>{{ login.target_private_key?t('feature.keyLoaded'):t('feature.keyRequired') }}</span></label><label>{{ t('feature.keyPassphrase') }}<input v-model="login.target_key_passphrase" type="password" autocomplete="off" /></label></template>
      </div>
      <label>{{ t('feature.sources') }}<textarea :value="row.input.allowed_sources?.join('\n')" required @input="row.input.allowed_sources=($event.target as HTMLTextAreaElement).value.split(/[\s,]+/).filter(Boolean);row.input.source_mode='custom'" /></label>
      <label>{{ t('feature.fingerprint') }}<input v-model="row.input.host_fingerprint" required @input="row.confirmed=false" /></label>
      <button type="button" @click="probe(row)">{{ t('feature.probe') }}</button>
      <label class="inline-checkbox"><input v-model="row.confirmed" type="checkbox" required />{{ t('feature.fingerprintTrust') }}</label>
     </template>
    </fieldset>
    <div class="actions"><button type="button" :disabled="busy" @click="clear">{{ t('feature.reselect') }}</button><button class="primary" :disabled="busy">{{ t('feature.commitImport') }}</button></div>
   </form>
   <p v-if="error" class="error" role="alert">{{ display(error) }}</p><p v-if="notice" role="status">{{ notice }}</p>
  </div>
 </dialog>
</template>
<style scoped>.import-dialog{width:min(840px,calc(100vw - 24px));max-height:90dvh;overflow:auto}.import-row{margin:16px 0;padding:12px;min-width:0;border:1px solid var(--border,#dce7e2);border-radius:8px}.import-fields{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(180px,100%),1fr));gap:12px}input,textarea,select{max-width:100%;min-width:0}li,p{overflow-wrap:anywhere}.inline-checkbox{display:flex;flex-direction:row;align-items:center;gap:8px}.inline-checkbox input{width:auto}.actions{display:flex;gap:12px}</style>

<style scoped>
.import-dialog[open]{display:flex;flex-direction:column;overflow:hidden}.dialog-heading{flex-shrink:0}.dialog-content{overflow:auto;min-height:0}.dialog-content>form{display:grid;gap:18px}.dialog-content>p.hint{padding:12px 14px;background:#f0f6f3;border:1px solid #dce9e2;border-radius:9px;line-height:1.8}.import-row{display:grid;gap:16px;margin:0;padding:18px;background:#fbfdfc;border-radius:12px}.import-row legend{padding:0 8px;font-weight:600}.import-row p,.import-row ul{margin:0}.import-fields{gap:14px}.import-row .inline-checkbox{padding:10px 12px;background:#edf5f0;border-radius:8px;font-weight:400}.import-row legend .inline-checkbox{padding:4px 8px}.import-dialog textarea{min-height:88px;line-height:1.7}.import-dialog textarea[rows]{font-family:monospace;min-height:170px}.import-dialog input[type=file]{padding:8px;background:#f7faf9;border-style:dashed;font-size:12px}.import-dialog input[type=file]::file-selector-button{font:inherit;padding:6px 12px;margin-right:12px;border:1px solid #c9ddd3;border-radius:6px;background:#edf5f0;color:#24634f;cursor:pointer}.actions{justify-content:flex-end;flex-wrap:wrap;padding-top:8px}.import-row>button{justify-self:start}.import-dialog form>.primary{justify-self:end}
:root[data-theme=dark] .import-row{background:#1a2927;border-color:#3c514b}:root[data-theme=dark] .dialog-content>p.hint,:root[data-theme=dark] .import-row .inline-checkbox,:root[data-theme=dark] .import-dialog input[type=file]{background:#223831;color:#bdd4ca;border-color:#3c514b}:root[data-theme=dark] .import-dialog input[type=file]::file-selector-button{background:#2e4d41;color:#d5e9df;border-color:#567364}
@media(max-width:480px){.import-row{padding:12px}.import-fields{grid-template-columns:1fr}.dialog-content{padding:16px}.actions>button{flex:1}}
</style>
