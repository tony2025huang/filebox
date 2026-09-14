<template>
  <main class="app-shell">
    <AuthenticatedTopbar :user="user" section="admin" />
    <section class="content-wrap">
      <div class="page-heading">
        <div><p class="eyebrow">WORKSPACE / RECYCLE</p><h1>{{ t('recycle.title') }}</h1><p class="muted">{{ t('recycle.confirmPurge') }}</p></div>
        <div class="sync-heading-actions">
          <button class="secondary-button" :title="t('files.refresh')" @click="loadRecycle"><RefreshCw :size="16" :class="{ spin: loading }" /></button>
          <button class="secondary-button" :disabled="!selectedIds.length" @click="openMove"><FolderUp :size="16" /> {{ t('recycle.move') }}</button>
          <button class="secondary-button danger-action" :disabled="!totalFiles" @click="openPurge"><Trash2 :size="16" /> {{ t('recycle.purge') }}</button>
        </div>
      </div>
      <div v-if="error" class="alert error">{{ error }}</div><div v-if="notice" class="alert success">{{ notice }}</div>

      <div v-if="loading" class="empty-state"><LoaderCircle :size="26" class="spin" /></div>
      <div v-else-if="!totalFiles" class="empty-state"><Archive :size="32" /><strong>{{ t('recycle.empty') }}</strong></div>
      <template v-else>
      <section v-for="group in groups" :key="ownerOf(group)" class="collection-section">
        <div class="collection-section-header">
          <h2>{{ t('recycle.owner') }}: {{ ownerOf(group) }}</h2>
          <span class="muted">{{ filesOf(group).length }}</span>
        </div>
        <div class="user-table-wrap"><table class="file-table user-table">
          <thead><tr><th><input type="checkbox" :checked="groupSelected(group)" :aria-label="t('recycle.selectAll')" @change="toggleGroup(group)" /></th><th>{{ t('files.name') }}</th><th>{{ t('files.size') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="file in filesOf(group)" :key="file.id">
              <td><input type="checkbox" :checked="selectedIds.includes(file.id)" :aria-label="file.name" @change="toggleOne(file.id)" /></td>
              <td><strong>{{ file.name }}</strong><br /><small>{{ file.relativePath || file.name }}</small></td>
              <td>{{ formatBytes(file.size) }}</td>
              <td><div class="row-actions">
                <button class="icon-button" :title="t('recycle.download')" @click="download(file)"><Download :size="16" /></button>
                <button class="icon-button danger-icon" :title="t('recycle.delete')" @click="removeOne(file)"><Trash2 :size="16" /></button>
              </div></td>
            </tr>
          </tbody>
        </table></div>
      </section>
      </template>
      <BrandFooter />
    </section>

    <div v-if="moveOpen" class="modal-backdrop" @click.self="!busy && (moveOpen = false)"><section class="modal-panel share-panel" role="dialog" aria-modal="true">
      <div class="panel-heading"><div><p class="eyebrow">{{ t('recycle.move') }}</p><h2>{{ t('recycle.moveTitle') }}</h2></div><button class="icon-button" :title="t('common.close')" :disabled="busy" @click="moveOpen = false"><X :size="18" /></button></div>
      <form @submit.prevent="submitMove">
        <label class="form-label">{{ t('recycle.targetUser') }}<select v-model.number="moveForm.targetUserId" required><option v-for="candidate in users" :key="candidate.id" :value="candidate.id">{{ candidate.username }}</option></select></label>
        <label class="form-label">{{ t('recycle.targetDir') }}<input v-model.trim="moveForm.targetDir" maxlength="512" placeholder="docs/2026" /></label>
        <p class="muted">{{ t('recycle.moved', { count: selectedIds.length }) }}</p>
        <p v-if="moveError" class="alert error">{{ moveError }}</p>
        <button class="primary-button submit-button" :disabled="busy || !moveForm.targetUserId"><span>{{ busy ? t('common.loading') : t('recycle.moveConfirm') }}</span><FolderUp :size="17" /></button>
      </form>
    </section></div>

    <div v-if="purgeOpen" class="modal-backdrop" @click.self="!busy && (purgeOpen = false)"><section class="modal-panel share-panel" role="dialog" aria-modal="true">
      <div class="panel-heading"><div><p class="eyebrow">{{ t('recycle.purge') }}</p><h2>{{ t('recycle.purgeConfirm') }}</h2></div><button class="icon-button" :title="t('common.close')" :disabled="busy" @click="purgeOpen = false"><X :size="18" /></button></div>
      <form @submit.prevent="submitPurge">
        <p class="alert info">{{ t('recycle.confirmPurge') }}</p>
        <label v-if="!user.totpEnabled" class="form-label">{{ t('files.clearPassword') }}<input v-model="purgeForm.password" type="password" required autofocus /></label>
        <label v-else class="form-label">{{ t('files.clearCode') }}<input v-model="purgeForm.code" type="text" inputmode="numeric" maxlength="6" minlength="6" pattern="[0-9]{6}" required autofocus /></label>
        <p v-if="purgeError" class="alert error">{{ purgeError }}</p>
        <button class="secondary-button danger-action submit-button" :disabled="busy"><span>{{ busy ? t('common.loading') : t('recycle.purgeConfirm') }}</span><Trash2 :size="17" /></button>
      </form>
    </section></div>
  </main>
</template>

<script setup>
// RecycleView æ˜¯å›žæ”¶ç«™ç‹¬ç«‹é¡µé¢ï¼ˆä»…ç®¡ç†å‘˜ï¼Œv041ï¼‰ï¼šæŒ‰åŽŸç”¨æˆ·ååˆ†ç»„æŸ¥çœ‹å›žæ”¶ç«™æ–‡ä»¶ï¼Œæ”¯æŒå¤šé€‰ç§»åŠ¨
// ï¼ˆåˆ°æŒ‡å®šç”¨æˆ· + æŒ‡å®šç›®å½•ï¼‰ã€ä¸‹è½½ã€æ°¸ä¹…åˆ é™¤ï¼Œä»¥åŠéœ€è¦äºŒæ¬¡è®¤è¯çš„æ¸…ç©ºå…¨éƒ¨ã€‚
// RecycleView is the standalone recycle-bin page (admin only, v041): files grouped by former owner with
// multi-select move, download, permanent delete, and a re-auth protected empty-all action.
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import AuthenticatedTopbar from '../components/AuthenticatedTopbar.vue'
import BrandFooter from '../components/BrandFooter.vue'
import { t } from '../i18n'
import { Archive, Download, FolderUp, LoaderCircle, RefreshCw, Trash2, X } from 'lucide-vue-next'

const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}'))
const groups = ref([]); const users = ref([]); const selectedIds = ref([])
const loading = ref(false); const busy = ref(false); const error = ref(''); const notice = ref('')
const moveOpen = ref(false); const moveError = ref(''); const moveForm = ref({ targetUserId: 0, targetDir: '' })
const purgeOpen = ref(false); const purgeError = ref(''); const purgeForm = ref({ password: '', code: '' })

const totalFiles = computed(() => groups.value.reduce((sum, group) => sum + filesOf(group).length, 0))
function filesOf(group) { return group?.files || group?.items || [] }
function ownerOf(group) { return group?.user || group?.recycleUser || group?.name || '-' }
function groupSelected(group) { const files = filesOf(group); return files.length > 0 && files.every(file => selectedIds.value.includes(file.id)) }
function toggleGroup(group) { const files = filesOf(group); const all = groupSelected(group); const ids = files.map(file => file.id); selectedIds.value = all ? selectedIds.value.filter(id => !ids.includes(id)) : [...new Set([...selectedIds.value, ...ids])] }
function toggleOne(id) { selectedIds.value = selectedIds.value.includes(id) ? selectedIds.value.filter(value => value !== id) : [...selectedIds.value, id] }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }

async function loadRecycle() {
  loading.value = true; error.value = ''
  try {
    const body = await api('/api/admin/recycle')
    groups.value = body.data.groups || []
    selectedIds.value = []
  } catch (err) { error.value = err.message } finally { loading.value = false }
}
async function loadUsers() {
  try { const body = await api('/api/admin/users'); users.value = (body.data.items || []).filter(candidate => candidate.id > 0) } catch { users.value = [] }
}
function openMove() {
  if (!selectedIds.value.length) { error.value = t('recycle.emptySelection'); return }
  moveError.value = ''; moveForm.value = { targetUserId: users.value[0]?.id || 0, targetDir: '' }; moveOpen.value = true
}
async function submitMove() {
  busy.value = true; moveError.value = ''
  try {
    const body = await api('/api/admin/recycle/move', { method: 'POST', body: JSON.stringify({ fileIds: selectedIds.value, targetUserId: moveForm.value.targetUserId, targetDir: moveForm.value.targetDir }) })
    notice.value = t('recycle.moved', { count: body.data.moved || 0 })
    moveOpen.value = false; await loadRecycle()
  } catch (err) { moveError.value = err.message } finally { busy.value = false }
}
async function removeOne(file) {
  if (!window.confirm(`${t('recycle.delete')}: ${file.name}?`)) return
  busy.value = true
  try { await api(`/api/admin/recycle/${file.id}`, { method: 'DELETE' }); notice.value = t('recycle.deleted'); await loadRecycle() } catch (err) { error.value = err.message } finally { busy.value = false }
}
async function download(file) {
  try {
    const response = await fetch(`/api/files/${file.id}/download`, { headers: { Authorization: `Bearer ${localStorage.getItem('filebox_token')}` } })
    if (!response.ok) throw new Error(t('error.downloadFailed'))
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url; link.download = file.name || 'download'
    document.body.appendChild(link); link.click(); link.remove()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  } catch (err) { error.value = err.message || t('error.downloadFailed') }
}
function openPurge() { purgeError.value = ''; purgeForm.value = { password: '', code: '' }; purgeOpen.value = true }
async function submitPurge() {
  busy.value = true; purgeError.value = ''
  try {
    const payload = user.value.totpEnabled ? { code: purgeForm.value.code } : { password: purgeForm.value.password }
    const body = await api('/api/admin/recycle/purge', { method: 'POST', body: JSON.stringify(payload) })
    notice.value = t('recycle.purged')
    purgeOpen.value = false
    await loadRecycle()
    if (body.data?.count !== undefined) notice.value = `${t('recycle.purged')} (${body.data.count})`
  } catch (err) { purgeError.value = err.message } finally { busy.value = false }
}
onMounted(() => { loadUsers(); loadRecycle() })
</script>


