<template>
  <main class="app-shell">
    <AuthenticatedTopbar :user="user" section="sync" />

    <div v-if="activeConfirm" class="modal-backdrop" @click.self="chooseConfirm(false)"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">CONFIRM</p><h2>{{ t('common.confirm') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="chooseConfirm(false)"><X :size="18" /></button></div><p class="modal-copy">{{ activeConfirm.message }}</p><div class="modal-actions"><button class="secondary-button" @click="chooseConfirm(false)">{{ t('common.cancel') }}</button><button class="secondary-button danger-action" @click="chooseConfirm(true)">{{ t('common.confirm') }}</button></div></section></div>

    <section class="content-wrap sync-page">
      <div class="page-heading"><div><p class="eyebrow">{{ t('sync.eyebrow') }}</p><h1>{{ t('sync.heading') }}</h1><p class="muted">{{ t('sync.copy') }}</p></div><div class="sync-heading-actions"><button class="secondary-button" :title="t('sync.refresh')" @click="loadAll"><RefreshCw :size="16" :class="{ spin: loading }" /></button><button class="primary-button" @click="openTaskCreate"><Plus :size="17" /> {{ t('sync.newTask') }}</button></div></div>
      <div v-if="error" class="alert error">{{ error }}</div><div v-if="notice" class="alert success">{{ notice }}</div>

      <section class="form-panel sync-section"><div class="panel-heading"><div><p class="eyebrow">{{ t('sync.tasks') }}</p><h2>{{ t('sync.tasks') }}</h2></div></div><div class="file-table-wrap sync-table-wrap"><table class="file-table"><thead><tr><th>{{ t('sync.taskName') }}</th><th>{{ t('sync.direction') }}</th><th>{{ t('sync.source') }} → {{ t('sync.target') }}</th><th>{{ t('sync.schedule') }}</th><th>{{ t('sync.result') }}</th><th>{{ t('sync.actions') }}</th></tr></thead><tbody><tr v-for="item in tasks" :key="item.id"><td><strong>{{ item.name }}</strong><small class="sync-subline">{{ systemName(item.remoteSystemId) }}</small></td><td><span class="direction-label" :class="item.direction">{{ directionLabel(item.direction) }}</span></td><td><span class="sync-path" :title="item.sourcePath">{{ item.sourcePath || t('sync.root') }}</span><span class="sync-arrow">→</span><span class="sync-path" :title="item.targetPath">{{ item.targetPath || t('sync.root') }}</span></td><td>{{ item.scheduleType === 'periodic' ? item.cron : t('sync.once') }}<small class="sync-subline">{{ item.enabled ? t('sync.enabled') : t('sync.disabled') }}</small></td><td><span v-if="!item.lastResult" class="muted">-</span><span v-else class="result-label" :class="item.lastResult">{{ item.lastResult === 'success' ? t('sync.success') : t('sync.failure') }}</span></td><td><div class="row-actions"><button class="icon-button" :title="t('sync.run')" :disabled="runningId === item.id" @click="runTask(item)"><Play :size="16" :class="{ spin: runningId === item.id }" /></button><button class="icon-button" :title="t('sync.logs')" @click="openDetails(item)"><Eye :size="16" /></button><button class="icon-button" :title="t('sync.edit')" @click="openTaskEdit(item)"><Pencil :size="16" /></button><button class="icon-button danger-icon" :title="t('sync.delete')" @click="deleteTask(item)"><Trash2 :size="16" /></button></div></td></tr></tbody></table><div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('common.loading') }}</span></div><div v-else-if="!tasks.length" class="empty-state compact-empty"><RefreshCw :size="30" /><strong>{{ t('sync.noTasks') }}</strong></div></div></section>

      <section class="form-panel sync-section"><div class="panel-heading"><div><p class="eyebrow">{{ t('sync.systems') }}</p><h2>{{ t('sync.systems') }}</h2></div><button class="secondary-button" @click="openSystemCreate"><Plus :size="16" /> {{ t('sync.newSystem') }}</button></div><div class="file-table-wrap sync-table-wrap"><table class="file-table"><thead><tr><th>{{ t('sync.systemName') }}</th><th>{{ t('sync.kind') }}</th><th>{{ t('sync.host') }}</th><th>{{ t('sync.port') }}</th><th>{{ t('sync.username') }}</th><th>{{ t('sync.authType') }}</th><th>{{ t('sync.taskCount') }}</th><th>{{ t('sync.testAt') }}</th><th></th></tr></thead><tbody><tr v-for="item in systems" :key="item.id"><td><strong>{{ item.name }}</strong></td><td>{{ item.kind === 'filebox' ? t('sync.filebox') : t('sync.sftp') }}</td><td :title="item.kind === 'filebox' ? item.url : item.host">{{ item.kind === 'filebox' ? item.url : item.host }}</td><td>{{ item.kind === 'filebox' ? '-' : item.port }}</td><td>{{ item.username }}</td><td>{{ item.authType === 'key' ? t('sync.key') : t('sync.password') }}</td><td>{{ item.taskCount }}</td><td><span v-if="!item.lastTestAt" class="muted">-</span><span v-else class="result-label" :class="item.lastTestResult === 'success' ? 'success' : 'failure'" :title="item.lastTestMessage || ''">{{ item.lastTestResult === 'success' ? t('sync.testOk') : t('sync.testFailed') }}</span><small v-if="item.lastTestAt" class="sync-subline">{{ formatDate(item.lastTestAt) }}</small></td><td><div class="row-actions"><button class="icon-button" :title="t('sync.test')" :disabled="testingId === item.id" @click="testSystem(item)"><Activity :size="16" :class="{ spin: testingId === item.id }" /></button><button class="icon-button" :title="t('sync.edit')" @click="openSystemEdit(item)"><Pencil :size="16" /></button><button class="icon-button danger-icon" :title="t('sync.delete')" @click="deleteSystem(item)"><Trash2 :size="16" /></button></div></td></tr></tbody></table><div v-if="!systems.length" class="empty-state compact-empty"><Server :size="30" /><strong>{{ t('sync.noSystems') }}</strong></div></div></section>
    </section>

    <div v-if="taskModal" class="modal-backdrop" @click.self="taskModal = false"><section class="modal-panel sync-modal wide-modal" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ taskEditing ? t('sync.editTask') : t('sync.createTask') }}</p><h2>{{ taskForm.name || t('sync.createTask') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="taskModal = false"><X :size="18" /></button></div><form class="sync-form" @submit.prevent="saveTask"><label class="form-label">{{ t('sync.taskName') }}<input v-model.trim="taskForm.name" maxlength="255" required /></label><label class="form-label">{{ t('sync.direction') }}<select v-model="taskForm.direction" @change="syncDirection"><option value="push">{{ t('sync.push') }}</option><option value="pull">{{ t('sync.pull') }}</option></select></label><label class="form-label">{{ t('sync.remoteSystem') }}<select v-model.number="taskForm.remoteSystemId" required @change="syncDirection"><option :value="0" disabled>{{ t('sync.selectSystem') }}</option><option v-for="item in systems" :key="item.id" :value="item.id">{{ item.name }} ({{ item.kind === 'filebox' ? item.url : item.host }})</option></select></label><div class="sync-form-grid"><label class="form-label">{{ t('sync.sourcePath') }}<div class="field-with-action"><input v-model="taskForm.sourcePath" :placeholder="t('sync.root')" /><button type="button" class="icon-button" :title="taskForm.sourceType === 'filebox' ? t('sync.chooseFolder') : t('sync.browseRemote')" @click="openPathPicker('source')"><FolderOpen v-if="taskForm.sourceType === 'filebox'" :size="16" /><Globe v-else :size="16" /></button></div></label><label class="form-label">{{ t('sync.targetPath') }}<div class="field-with-action"><input v-model="taskForm.targetPath" :placeholder="t('sync.root')" /><button type="button" class="icon-button" :title="taskForm.targetType === 'filebox' ? t('sync.chooseFolder') : t('sync.browseRemote')" @click="openPathPicker('target')"><FolderOpen v-if="taskForm.targetType === 'filebox'" :size="16" /><Globe v-else :size="16" /></button></div></label></div><label class="check-label sync-auto-create"><input type="checkbox" checked disabled /> {{ t('sync.autoCreate') }}</label><label class="form-label">{{ t('sync.conflict') }}<select v-model="taskForm.conflictPolicy"><option value="overwrite">{{ t('sync.overwrite') }}</option><option value="skip">{{ t('sync.skip') }}</option><option value="rename">{{ t('sync.rename') }}</option></select></label><label class="form-label">{{ t('sync.scheduleType') }}<select v-model="taskForm.scheduleType"><option value="once">{{ t('sync.once') }}</option><option value="periodic">{{ t('sync.periodic') }}</option></select></label><div v-if="taskForm.scheduleType === 'periodic'" class="cron-row"><label class="form-label">{{ t('sync.cron') }}<input v-model.trim="taskForm.cron" placeholder="0 3 * * *" required /></label><label class="form-label">{{ t('sync.cronPreset') }}<select @change="applyPreset"><option value="">-</option><option value="daily">{{ t('sync.cronDaily') }}</option><option value="hourly">{{ t('sync.cronHourly') }}</option><option value="weekday">{{ t('sync.cronWeekday') }}</option></select></label></div><label class="check-label"><input v-model="taskForm.enabled" type="checkbox" /> {{ t('sync.enabled') }}</label><p v-if="formError" class="alert error">{{ formError }}</p><div class="modal-actions"><button class="primary-button" :disabled="saving"><Save :size="16" /> {{ t('sync.saveTask') }}</button><button type="button" class="secondary-button" @click="taskModal = false">{{ t('common.cancel') }}</button></div></form></section></div>

    <div v-if="systemModal" class="modal-backdrop" @click.self="systemModal = false"><section class="modal-panel sync-modal" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ systemEditing ? t('sync.editSystem') : t('sync.createSystem') }}</p><h2>{{ systemForm.name || t('sync.newSystem') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="systemModal = false"><X :size="18" /></button></div><form class="sync-form" @submit.prevent="saveSystem"><label class="form-label">{{ t('sync.systemName') }}<input v-model.trim="systemForm.name" maxlength="255" required /></label><label class="form-label">{{ t('sync.kind') }}<select v-model="systemForm.kind" @change="syncSystemKind"><option value="sftp">{{ t('sync.sftp') }}</option><option value="filebox">{{ t('sync.filebox') }}</option></select></label><div class="sync-form-grid"><template v-if="systemForm.kind === 'filebox'"><label class="form-label">{{ t('sync.url') }}<input v-model.trim="systemForm.url" placeholder="https://files.example.com" required /></label><label class="form-label">{{ t('sync.username') }}<input v-model.trim="systemForm.username" required /></label><label class="form-label">{{ t('sync.password') }}<div class="field-with-action"><input :value="secretInputValue()" :type="secretVisible ? 'text' : 'password'" :placeholder="systemEditing && systemForm.hasCredentials ? t('sync.savedSecretPlaceholder') : t('sync.password')" :required="!systemEditing" autocomplete="new-password" @input="updateSecretInput" /><button v-if="canRevealSystemSecret()" type="button" class="icon-button" :title="secretVisible ? t('sync.hideSecret') : t('sync.viewSecret')" @click="toggleSecret"><EyeOff v-if="secretVisible" :size="16" /><Eye v-else :size="16" /></button></div></label></template><template v-else><label class="form-label">{{ t('sync.host') }}<input v-model.trim="systemForm.host" required /></label><label class="form-label">{{ t('sync.port') }}<input v-model.number="systemForm.port" type="number" min="1" max="65535" required /></label></template></div><template v-if="systemForm.kind === 'filebox'"><p class="field-hint">{{ t('sync.urlHint') }}</p></template><template v-else><label class="form-label">{{ t('sync.username') }}<input v-model.trim="systemForm.username" required /></label><label class="form-label">{{ t('sync.authType') }}<select v-model="systemForm.authType"><option value="password">{{ t('sync.password') }}</option><option value="key">{{ t('sync.key') }}</option></select></label><label class="form-label">{{ systemForm.authType === 'key' ? t('sync.key') : t('sync.password') }}<div class="field-with-action"><input v-if="systemForm.authType === 'password'" :value="secretInputValue()" :type="secretVisible ? 'text' : 'password'" :placeholder="systemEditing && systemForm.hasCredentials ? t('sync.savedSecretPlaceholder') : t('sync.password')" :required="!systemEditing" autocomplete="new-password" @input="updateSecretInput" /><textarea v-else :value="secretInputValue()" rows="5" :required="!systemEditing" autocomplete="new-password" @input="updateSecretInput"></textarea><button v-if="canRevealSystemSecret()" type="button" class="icon-button" :title="secretVisible ? t('sync.hideSecret') : t('sync.viewSecret')" @click="toggleSecret"><EyeOff v-if="secretVisible" :size="16" /><Eye v-else :size="16" /></button></div></label><label v-if="systemForm.authType === 'key'" class="form-label">{{ t('sync.passphrase') }}<input v-model="systemForm.authPassphrase" type="password" autocomplete="new-password" /></label></template><p class="field-hint">{{ t('sync.credentialsHint') }}</p><p v-if="formError" class="alert error">{{ formError }}</p><div class="modal-actions"><button class="primary-button" :disabled="saving"><Save :size="16" /> {{ t('sync.saveSystem') }}</button><button type="button" class="secondary-button" @click="systemModal = false">{{ t('common.cancel') }}</button></div></form></section></div>

    <div v-if="picker" class="modal-backdrop" @click.self="picker = null"><section class="modal-panel sync-modal picker-modal" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ picker.side === 'local' ? t('sync.chooseFolder') : t('sync.browseRemote') }}</p><h2>{{ picker.path || t('sync.root') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="picker = null"><X :size="18" /></button></div><div class="picker-toolbar"><div class="picker-toolbar-buttons"><button v-if="picker.side === 'remote' && picker.path !== '.' && picker.path !== '/'" class="secondary-button" @click="browseRemote(parentRemotePath(picker.path))"><ArrowUp :size="15" /> {{ t('sync.back') }}</button><button v-if="picker.side === 'remote' && picker.path !== '/'" class="secondary-button" @click="browseRemote('/')"><Home :size="15" /> {{ t('sync.remoteRoot') }}</button><button v-if="picker.side === 'local' && picker.path !== ''" class="secondary-button" @click="browseLocal(parentFileBoxPath(picker.path))"><ArrowUp :size="15" /> {{ t('sync.back') }}</button></div><span>{{ t('sync.currentPath') }}: {{ picker.path || t('sync.root') }}</span><label class="form-label" style="flex: 1; min-width: 170px; margin: 0"><input v-model.trim="pickerFilter" :placeholder="t('sync.filterPlaceholder')" :aria-label="t('sync.filterPlaceholder')" /></label></div><div v-if="picker.side === 'local'" class="picker-list"><button class="picker-entry" @click="choosePath('')"><Folder :size="17" /> {{ t('sync.root') }}</button><button v-for="folder in filteredLocalFolders" :key="'d' + folder.id" class="picker-entry" @click="browseLocal(folder.path)"><Folder :size="17" /> {{ folder.name }}</button><template v-if="picker.includeFiles"><button v-for="entry in filteredLocalFileEntries" :key="'f' + entry.id" class="picker-entry picker-file" :title="formatBytes(entry.size)" @click="chooseEntry(entry)"><File :size="17" /> {{ entry.name }}<small>{{ formatBytes(entry.size) }}</small></button></template><span v-if="!filteredLocalFolders.length && (!picker.includeFiles || !filteredLocalFileEntries.length)" class="empty-state compact-empty">{{ t('sync.noSystems') }}</span></div><div v-else class="picker-list"><template v-for="entry in filteredRemoteEntries" :key="entry.path"><button v-if="entry.isDir" class="picker-entry" @click="browseRemote(entry.path)"><Folder :size="17" /> {{ entry.name }}</button><button v-else-if="picker.includeFiles" class="picker-entry picker-file" :title="formatBytes(entry.size)" @click="chooseEntry(entry)"><File :size="17" /> {{ entry.name }}<small>{{ formatBytes(entry.size) }}</small></button></template><span v-if="!filteredRemoteEntries.length" class="empty-state compact-empty">{{ t('sync.noSystems') }}</span></div><p v-if="formError" class="alert error">{{ formError }}</p><div class="field-with-action" style="align-items: end; margin-top: 14px"><label class="form-label" style="flex: 1; margin: 0">{{ t('sync.enterPath') }}<input v-model.trim="pickerPathInput" :placeholder="picker.path || t('sync.root')" /></label><button type="button" class="secondary-button" :disabled="pickerPathSaving" @click="confirmPickerPath">{{ t('sync.confirmPath') }}</button></div><div class="modal-actions"><button class="primary-button" @click="choosePath(picker.path === '.' ? '' : picker.path)"><Check :size="16" /> {{ t('sync.choosePath') }}</button><button class="secondary-button" @click="picker = null">{{ t('common.cancel') }}</button></div></section></div>

    <div v-if="details" class="modal-backdrop" @click.self="details = null"><section class="modal-panel wide-modal sync-modal" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ t('sync.taskDetails') }}</p><h2>{{ details.name }}</h2></div><button class="icon-button" :title="t('common.close')" @click="details = null"><X :size="18" /></button></div><dl class="sync-detail-grid"><div><dt>{{ t('sync.direction') }}</dt><dd>{{ directionLabel(details.direction) }}</dd></div><div><dt>{{ t('sync.source') }}</dt><dd>{{ details.sourcePath || t('sync.root') }}</dd></div><div><dt>{{ t('sync.target') }}</dt><dd>{{ details.targetPath || t('sync.root') }}</dd></div></dl><section class="sync-log-section"><div class="panel-heading"><h3>{{ t('sync.logsTitle') }}</h3><button class="icon-button" :title="t('sync.refresh')" @click="loadDetails(details)"><RefreshCw :size="16" :class="{ spin: detailsLoading }" /></button></div><div v-if="detailsLoading" class="empty-state compact-empty"><LoaderCircle :size="24" class="spin" /></div><div v-else-if="!detailLogs.length" class="empty-state compact-empty">{{ t('sync.noLogs') }}</div><div v-else class="sync-log-table-wrap"><table class="file-table sync-log-table"><thead><tr><th>{{ t('sync.startTime') }}</th><th>{{ t('sync.endTime') }}</th><th>{{ t('sync.status') }}</th><th v-if="details.scheduleType === 'periodic'">{{ t('sync.nextRun') }}</th><th>{{ t('sync.files') }}</th><th>{{ t('sync.bytes') }}</th><th>{{ t('sync.detail') }}</th></tr></thead><tbody><tr v-for="entry in detailLogs" :key="entry.id"><td>{{ formatDate(entry.runAt) }}</td><td>{{ entry.finishedAt ? formatDate(entry.finishedAt) : '-' }}</td><td><span class="result-label" :class="entry.result"><LoaderCircle v-if="entry.result === 'running'" :size="12" class="spin" />{{ entry.result === 'running' ? t('sync.running') : t('sync.ended') }}</span><small v-if="entry.message && entry.result !== 'running'" class="sync-subline" :title="entry.message">{{ entry.message }}</small></td><td v-if="details.scheduleType === 'periodic'">{{ details.nextRunAt ? formatDate(details.nextRunAt) : '-' }}</td><td>{{ entry.files }}</td><td>{{ formatBytes(entry.bytes) }}</td><td><details v-if="entry.detail" class="sync-log-detail"><summary>{{ t('sync.detail') }}</summary><pre>{{ entry.detail }}</pre></details><span v-else>-</span></td></tr><tr v-if="runningProgress"><td colspan="7"><div class="sync-progress-block"><div class="sync-progress-meta"><span><strong>{{ t('sync.currentFile') }}</strong> {{ runningProgress.currentFile || '-' }}</span><span>{{ runningProgress.doneFiles }} / {{ runningProgress.totalFiles || '?' }} {{ t('sync.files') }}</span><span>{{ formatBytes(runningProgress.transferredBytes) }}</span><span>{{ formatRate(syncRate) }}</span></div><div class="progress-track"><span :style="{ width: syncProgressPercent + '%' }"></span></div></div></td></tr></tbody></table></div></section></section></div>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api } from '../api'
import AuthenticatedTopbar from '../components/AuthenticatedTopbar.vue'
import { t, currentLocale } from '../i18n'
import { Activity, ArrowUp, Check, Eye, EyeOff, File, Folder, FolderOpen, Globe, Home, LoaderCircle, Pencil, Play, Plus, RefreshCw, Save, Server, Trash2, X } from 'lucide-vue-next'

const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}'))
const tasks = ref([]); const systems = ref([]); const folders = ref([]); const loading = ref(false); const saving = ref(false); const error = ref(''); const notice = ref(''); const formError = ref(''); const runningId = ref(0); const confirmQueue = ref([])
const taskModal = ref(false); const taskEditing = ref(false); const systemModal = ref(false); const systemEditing = ref(false); const picker = ref(null); const pickerFilter = ref(''); const pickerPathInput = ref(''); const pickerPathSaving = ref(false); const remoteEntries = ref([]); const localFileEntries = ref([]); const details = ref(null); const detailLogs = ref([]); const detailsLoading = ref(false); const testingId = ref(0)
const taskForm = reactive({ id: 0, name: '', direction: 'push', remoteSystemId: 0, sourceType: 'filebox', sourcePath: '', sourceKind: 'directory', targetType: 'sftp', targetPath: '.', conflictPolicy: 'overwrite', scheduleType: 'once', cron: '0 3 * * *', enabled: true })
// 进行中同步进度（v018 #4）：详情弹窗打开时每 2s 轮询 /api/sync/tasks/{id}/progress，速率由两次采样差值计算。
// In-flight sync progress (v018 #4): polls /api/sync/tasks/{id}/progress every 2s while the detail dialog is open;
// the transfer rate is derived from the delta between two samples.
const syncProgress = ref(null); const syncRate = ref(0); let progressTimer = null; let lastProgressSample = null
const runningProgress = computed(() => (syncProgress.value?.running ? syncProgress.value.progress : null))
const syncProgressPercent = computed(() => { const p = runningProgress.value; if (!p || !p.totalFiles) return 0; return Math.min(100, Math.round(p.doneFiles / p.totalFiles * 100)) })
async function pollSyncProgress() {
  if (!details.value) return
  try {
    const body = await api(`/api/sync/tasks/${details.value.id}/progress`)
    const now = Date.now()
    if (body.data.running) {
      const progress = body.data.progress
      if (lastProgressSample && lastProgressSample.bytes >= 0) {
        const deltaBytes = progress.transferredBytes - lastProgressSample.bytes
        const deltaMs = now - lastProgressSample.at
        syncRate.value = deltaMs > 0 && deltaBytes >= 0 ? deltaBytes / (deltaMs / 1000) : 0
      } else {
        syncRate.value = 0
      }
      lastProgressSample = { at: now, bytes: progress.transferredBytes }
      syncProgress.value = { running: true, progress }
    } else {
      syncProgress.value = null; lastProgressSample = null; syncRate.value = 0
    }
  } catch { /* 轮询失败静默，下次重试 */ }
}
function startProgressPolling() { stopProgressPolling(); pollSyncProgress(); progressTimer = window.setInterval(pollSyncProgress, 2000) }
function stopProgressPolling() { if (progressTimer) { window.clearInterval(progressTimer); progressTimer = null } lastProgressSample = null }
watch(details, value => { if (value) startProgressPolling(); else stopProgressPolling() })
const systemForm = reactive({ id: 0, name: '', kind: 'sftp', host: '', url: '', port: 22, username: '', authType: 'password', authSecret: '', authPassphrase: '', hasCredentials: false })
const revealedSecret = ref(null); const secretVisible = ref(false); const secretDisplay = ref('')
const fileboxFoldersAt = computed(() => path => folders.value.filter(folder => { const parent = folder.path.includes('/') ? folder.path.slice(0, folder.path.lastIndexOf('/')) : ''; return parent === path }))
const filteredLocalFolders = computed(() => picker.value ? fileboxFoldersAt.value(picker.value.path).filter(folder => folder.name.includes(pickerFilter.value)) : [])
const filteredLocalFileEntries = computed(() => localFileEntries.value.filter(entry => entry.name.includes(pickerFilter.value)))
const filteredRemoteEntries = computed(() => remoteEntries.value.filter(entry => entry.name.includes(pickerFilter.value)))

async function loadAll() { loading.value = true; error.value = ''; try { const [taskBody, systemBody, folderBody] = await Promise.all([api('/api/sync/tasks'), api('/api/sync/systems'), api('/api/folders')]); tasks.value = taskBody.data.items || []; systems.value = systemBody.data.items || []; folders.value = folderBody.data.items || [] } catch (err) { error.value = err.message } finally { loading.value = false } }
function systemName(id) { return systemTargetLabel(systemForTask(id)) }
function systemForTask(id) { return systems.value.find(item => item.id === id) }
function systemTargetLabel(system) {
  if (!system) return t('sync.remoteSystem')
  if (system.kind === 'sftp') {
    const host = String(system.host || '').trim()
    if (host) return system.port ? `${host}:${system.port}` : host
  } else if (system.kind === 'filebox') {
    const url = String(system.url || '').trim()
    if (url) {
      try { return new URL(url).host || url } catch { return url }
    }
  }
  return system.name || t('sync.remoteSystem')
}
function directionLabel(value) { return value === 'pull' ? t('sync.pull') : t('sync.push') }
function resetTask() { Object.assign(taskForm, { id: 0, name: '', direction: 'push', remoteSystemId: systems.value[0]?.id || 0, sourceType: 'filebox', sourcePath: '', sourceKind: 'directory', targetType: 'sftp', targetPath: '.', conflictPolicy: 'overwrite', scheduleType: 'once', cron: '0 3 * * *', enabled: true }); syncDirection() }
function openTaskCreate() { resetTask(); taskEditing.value = false; formError.value = ''; taskModal.value = true }
function openTaskEdit(item) { Object.assign(taskForm, item); taskEditing.value = true; formError.value = ''; taskModal.value = true }
// selectedSystemKind 返回当前选中目标系统的类型（sftp/filebox），用于方向矩阵的端点类型推导。
// selectedSystemKind returns the selected remote system kind (sftp/filebox) used to derive endpoint types.
function selectedSystemKind() { return systems.value.find(item => item.id === Number(taskForm.remoteSystemId))?.kind || 'sftp' }
function syncDirection() {
  const remoteKind = selectedSystemKind()
  if (taskForm.direction === 'push') {
    taskForm.sourceType = 'filebox'
    taskForm.targetType = remoteKind === 'filebox' ? 'filebox' : 'sftp'
    if (!taskForm.targetPath) taskForm.targetPath = remoteKind === 'filebox' ? '' : '.'
  } else {
    taskForm.sourceType = remoteKind === 'filebox' ? 'filebox' : 'sftp'
    taskForm.targetType = 'filebox'
    if (taskForm.targetPath === '.') taskForm.targetPath = ''
  }
}
function applyPreset(event) { const values = { daily: '0 3 * * *', hourly: '0 * * * *', weekday: '0 9 * * 1-5' }; if (values[event.target.value]) taskForm.cron = values[event.target.value]; event.target.value = '' }
function taskPayload() { return { name: taskForm.name, direction: taskForm.direction, remoteSystemId: Number(taskForm.remoteSystemId), sourceType: taskForm.sourceType, sourcePath: taskForm.sourcePath, sourceKind: taskForm.sourceKind || 'directory', targetType: taskForm.targetType, targetPath: taskForm.targetPath, conflictPolicy: taskForm.conflictPolicy, scheduleType: taskForm.scheduleType, cron: taskForm.cron, enabled: taskForm.enabled } }
async function saveTask() { saving.value = true; formError.value = ''; try { const body = await api(taskEditing.value ? `/api/sync/tasks/${taskForm.id}` : '/api/sync/tasks', { method: taskEditing.value ? 'PUT' : 'POST', body: JSON.stringify(taskPayload()) }); notice.value = t('sync.taskSaved'); taskModal.value = false; await loadAll(); if (body.data) tasks.value = tasks.value } catch (err) { formError.value = err.message } finally { saving.value = false } }
async function runTask(item) { runningId.value = item.id; error.value = ''; try { await api(`/api/sync/tasks/${item.id}/run`, { method: 'POST' }); notice.value = t('sync.runStarted'); await loadAll() } catch (err) { error.value = err.message } finally { runningId.value = 0 } }
function askConfirm(message) {
  return new Promise(resolve => {
    const entry = { message, resolve }
    entry.timer = setTimeout(() => {
      const position = confirmQueue.value.indexOf(entry)
      if (position >= 0) confirmQueue.value.splice(position, 1)
      resolve(false)
    }, 60000)
    confirmQueue.value.push(entry)
  })
}
const activeConfirm = computed(() => confirmQueue.value[0] || null)
function chooseConfirm(value) {
  const entry = confirmQueue.value.shift()
  if (!entry) return
  clearTimeout(entry.timer)
  entry.resolve(Boolean(value))
}
async function deleteTask(item) { if (!(await askConfirm(t('sync.confirmDeleteTask', { name: item.name })))) return; try { await api(`/api/sync/tasks/${item.id}`, { method: 'DELETE' }); notice.value = t('sync.taskDeleted'); await loadAll() } catch (err) { error.value = err.message } }
function resetSecretView() { revealedSecret.value = null; secretVisible.value = false; secretDisplay.value = '' }
function resetSystem() { Object.assign(systemForm, { id: 0, name: '', kind: 'sftp', host: '', url: '', port: 22, username: '', authType: 'password', authSecret: '', authPassphrase: '', hasCredentials: false }); resetSecretView() }
function openSystemCreate() { resetSystem(); systemEditing.value = false; formError.value = ''; systemModal.value = true }
function openSystemEdit(item) { Object.assign(systemForm, { ...item, authSecret: '', authPassphrase: '' }); if (!systemForm.kind) systemForm.kind = 'sftp'; systemEditing.value = true; formError.value = ''; resetSecretView(); systemModal.value = true }
function canRevealSystemSecret() { return systemEditing.value && systemForm.hasCredentials && systemForm.authType === 'password' }
function secretInputValue() { return secretDisplay.value }
function updateSecretInput(event) { systemForm.authSecret = event.target.value; secretDisplay.value = event.target.value }
async function toggleSecret() {
  if (!canRevealSystemSecret()) return
  if (secretVisible.value) { secretVisible.value = false; secretDisplay.value = ''; return }
  formError.value = ''
  try {
    if (!revealedSecret.value || revealedSecret.value.id !== systemForm.id) {
      const body = await api(`/api/sync/systems/${systemForm.id}/secret`)
      revealedSecret.value = { id: systemForm.id, secret: body.data.secret, authPassphrase: body.data.authPassphrase }
    }
    secretDisplay.value = systemForm.authSecret || revealedSecret.value.secret
    secretVisible.value = true
  } catch (err) {
    secretVisible.value = false
    secretDisplay.value = ''
    formError.value = t('sync.secretError')
  }
}
function syncSystemKind() { if (systemForm.kind === 'filebox') { systemForm.authType = 'password'; systemForm.host = ''; systemForm.authPassphrase = '' } }
async function saveSystem() { saving.value = true; formError.value = ''; try { const body = { name: systemForm.name, kind: systemForm.kind || 'sftp', host: systemForm.kind === 'filebox' ? '' : systemForm.host, url: systemForm.kind === 'filebox' ? systemForm.url : '', port: Number(systemForm.port) || 22, username: systemForm.username, authType: systemForm.authType, authSecret: systemForm.authSecret, authPassphrase: systemForm.authPassphrase }; await api(systemEditing.value ? `/api/sync/systems/${systemForm.id}` : '/api/sync/systems', { method: systemEditing.value ? 'PUT' : 'POST', body: JSON.stringify(body) }); notice.value = t('sync.systemSaved'); systemModal.value = false; await loadAll() } catch (err) { formError.value = err.message } finally { saving.value = false } }
async function deleteSystem(item) { if (!(await askConfirm(t('sync.confirmDeleteSystem', { name: item.name })))) return; try { await api(`/api/sync/systems/${item.id}`, { method: 'DELETE' }); notice.value = t('sync.systemDeleted'); await loadAll() } catch (err) { error.value = err.message } }
// testSystem 探测目标系统连通性（#5）：调用 POST /api/sync/systems/{id}/test，
// 成功后把 ok/失败与测试时间写回行内徽标；失败消息仅临时展示，不落库（避免保存敏感信息）。
// testSystem probes a remote system's connectivity (#5) via POST /api/sync/systems/{id}/test,
// updating the row badge with ok/failure and the tested time; the failure message stays transient.
async function testSystem(item) { testingId.value = item.id; error.value = ''; try { const body = await api(`/api/sync/systems/${item.id}/test`, { method: 'POST' }); item.lastTestAt = body.data.testedAt; item.lastTestResult = body.data.ok ? 'success' : 'failure'; item.lastTestMessage = body.data.ok ? '' : body.data.message; notice.value = body.data.ok ? `${t('sync.testOk')} · ${formatDate(body.data.testedAt)}` : `${t('sync.testFailed')}: ${body.data.message}` } catch (err) { error.value = err.message } finally { testingId.value = 0 } }
// openPathPicker 打开路径选择器：本地 FileBox 目录用 folders/files 接口，远端按目标系统类型浏览。
// 方向判定：push 的源与 pull 的目标是本地 FileBox；另一端是远端（SFTP 或远端 FileBox）。
// openPathPicker opens the path picker: local FileBox dirs use folders/files APIs; remote uses the system-kind browse.
// Side logic: push's source and pull's target are local FileBox; the other side is remote (SFTP or a remote FileBox).
function openPathPicker(target) {
  // v014：本地与远端（SFTP/FileBox）统一支持选文件，两侧均 includeFiles=1；
  // 选中文件后 sourceKind 写入 file（chooseEntry 处理），目录选择仍写回 directory。
  // v014: both local and remote sides (SFTP/FileBox) support picking files, so includeFiles is 1
  // on every side; picking a file writes sourceKind=file (see chooseEntry), folders write directory.
  const includeFiles = true
  const isLocal = (target === 'source' && taskForm.direction === 'push') || (target === 'target' && taskForm.direction === 'pull')
  pickerFilter.value = ''
  formError.value = ''
  if (isLocal) {
    picker.value = { target, side: 'local', path: taskForm[`${target}Path`] || '', includeFiles }
    pickerPathInput.value = picker.value.path
    browseLocal(picker.value.path)
    return
  }
  if (!taskForm.remoteSystemId) { formError.value = t('sync.selectSystem'); return }
  picker.value = { target, side: 'remote', kind: selectedSystemKind(), path: taskForm[`${target}Path`] || '.', includeFiles }
  pickerPathInput.value = picker.value.path
  browseRemote(picker.value.path)
}
async function browseRemote(path) { if (!picker.value) return; picker.value.path = path || '.'; pickerPathInput.value = picker.value.path; pickerFilter.value = ''; formError.value = ''; try { const query = `path=${encodeURIComponent(picker.value.path)}${picker.value.includeFiles ? '&includeFiles=1' : ''}`; remoteEntries.value = (await api(`/api/sync/systems/${taskForm.remoteSystemId}/browse?${query}`)).data.items || [] } catch (err) { formError.value = err.message } }
async function browseLocal(path) { if (!picker.value) return; picker.value.path = path || ''; pickerPathInput.value = picker.value.path; pickerFilter.value = ''; formError.value = ''; try { const query = `path=${encodeURIComponent(picker.value.path)}${picker.value.includeFiles ? '&includeFiles=1' : ''}`; const body = await api(`/api/sync/browse-filebox?${query}`); localFileEntries.value = (body.data?.items || []).filter(entry => !entry.isDir) } catch (err) { formError.value = err.message } }
// parentRemotePath 归一化上级路径：`.` 表示用户 home（无上级），`foo`→`.`，`foo/bar`→`foo`，`/tmp`→`/`，`/`→无上级。
// parentRemotePath normalizes the parent path: `.` is the user home (no parent), `foo`→`.`, `foo/bar`→`foo`, `/tmp`→`/`, `/`→no parent.
function parentRemotePath(value) {
  const clean = value.replace(/\/+$/, '')
  if (clean === '.' || clean === '/') return clean
  const index = clean.lastIndexOf('/')
  if (index < 0) return '.'
  if (index === 0) return '/'
  return clean.slice(0, index)
}
// parentFileBoxPath 返回本地 FileBox 路径的上级：`''`→无上级，`a`→`''`，`a/b`→`a`。
// parentFileBoxPath returns the parent of a local FileBox path: `''`→no parent, `a`→`''`, `a/b`→`a`.
function parentFileBoxPath(value) {
  const clean = value.replace(/\/+$/, '')
  const index = clean.lastIndexOf('/')
  return index < 0 ? '' : clean.slice(0, index)
}
// chooseEntry 选择文件条目（源端）：文件路径直接选中并标记 sourceKind=file；目录条目仅导航。
// chooseEntry selects a file entry (source side): file paths are picked directly with sourceKind=file; directory entries only navigate.
function chooseEntry(entry) { if (!picker.value || entry.isDir) return; taskForm[`${picker.value.target}Path`] = entry.path; if (picker.value.target === 'source') taskForm.sourceKind = 'file'; picker.value = null }
function choosePath(value) { if (!picker.value) return; taskForm[`${picker.value.target}Path`] = value; if (picker.value.target === 'source') taskForm.sourceKind = 'directory'; picker.value = null }
async function confirmPickerPath() {
  if (!picker.value || pickerPathSaving.value) return
  const path = pickerPathInput.value.trim()
  if (picker.value.side === 'local') { choosePath(path); return }
  if (!path) { formError.value = t('sync.invalidRemotePath'); return }
  pickerPathSaving.value = true
  formError.value = ''
  try {
    const query = `path=${encodeURIComponent(path)}${picker.value.includeFiles ? '&includeFiles=1' : ''}`
    await api(`/api/sync/systems/${taskForm.remoteSystemId}/browse?${query}`)
    choosePath(path === '.' ? '' : path)
  } catch {
    formError.value = t('sync.invalidRemotePath')
  } finally {
    pickerPathSaving.value = false
  }
}
async function openDetails(item) { details.value = item; await loadDetails(item) }
async function loadDetails(item) { detailsLoading.value = true; try { const body = await api(`/api/sync/tasks/${item.id}`); details.value = body.data; detailLogs.value = body.data.logs || [] } catch (err) { error.value = err.message } finally { detailsLoading.value = false } }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatRate(bytesPerSecond = 0) { if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(bytesPerSecond < 10 ? 1 : 0)} B/s`; const units = ['KB/s', 'MB/s', 'GB/s']; let value = bytesPerSecond; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '-' }
onMounted(loadAll)
</script>
