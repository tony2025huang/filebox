<template>
  <main class="app-shell">
    <header class="topbar">
      <div class="topbar-brand"><BrandLogo variant="main" compact /><span class="slash">/</span><span class="section-name">{{ t('nav.files') }}</span></div>
      <div class="topbar-actions">
        <span class="user-chip"><span class="avatar">{{ user.username?.slice(0, 1).toUpperCase() }}</span>{{ user.username }}</span>
        <LanguageSelect :user="user" />
        <RouterLink v-if="user.role === 'admin'" to="/admin" class="icon-text-button"><Shield :size="16" /> {{ t('nav.admin') }}</RouterLink>
        <RouterLink to="/logs" class="icon-text-button"><ScrollText :size="16" /> {{ t('nav.logs') }}</RouterLink>
        <button class="icon-button" :title="t('nav.logout')" @click="logout"><LogOut :size="18" /></button>
      </div>
    </header>

    <section class="content-wrap">
      <div class="page-heading">
        <div><p class="eyebrow">WORKSPACE / {{ user.role === 'admin' ? t('files.workspaceAdmin') : t('files.workspaceUser') }}</p><h1>{{ t('files.heading') }}</h1><p class="muted">{{ t('files.copy') }}</p></div>
        <div class="quota-block"><div class="quota-label"><span>{{ t('files.quota') }}</span><strong>{{ formatBytes(user.usedBytes) }} <em>/ {{ formatBytes(user.quotaBytes) }}</em></strong></div><div class="progress-track"><span :style="{ width: quotaPercent + '%' }"></span></div></div>
      </div>

      <div class="upload-zone" :class="{ dragging }" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="handleDrop">
        <UploadCloud :size="26" />
        <div><strong>{{ t('files.dropTitle') }}</strong><span>{{ t('files.dropCopy') }}</span></div>
        <button class="secondary-button" @click="fileInput?.click()"><Upload :size="17" /> {{ t('files.choose') }}</button>
        <input ref="fileInput" type="file" multiple hidden @change="handleInput" />
      </div>

      <div v-if="uploads.length" class="upload-list">
        <div v-for="item in uploads" :key="item.id" class="upload-row">
          <FileUp :size="18" class="upload-icon" /><div class="upload-main"><div class="upload-name"><strong>{{ item.file.name }}</strong><span>{{ item.status }}</span></div><div class="progress-track"><span :style="{ width: item.progress + '%' }"></span></div></div><span class="upload-percent">{{ item.progress }}%</span>
        </div>
      </div>

      <div class="toolbar"><div class="search-box"><Search :size="17" /><input v-model="searchInput" :placeholder="t('files.searchPlaceholder')" @keyup.enter="search" /><button v-if="searchInput" :title="t('files.clearSearch')" @click="searchInput = ''; search()"><X :size="15" /></button></div><button class="refresh-button" :title="t('files.refresh')" @click="loadFiles"><RefreshCw :size="17" :class="{ spin: loading }" /></button><span class="result-count">{{ t('common.files', { count: total }) }}</span></div>

      <div v-if="notice" class="alert success">{{ notice }}</div><div v-if="error" class="alert error">{{ error }}</div>
      <div class="file-table-wrap">
        <table class="file-table"><thead><tr><th>{{ t('files.name') }}</th><th>{{ t('files.size') }}</th><th>{{ t('files.type') }}</th><th>{{ t('files.integrity') }}</th><th>{{ t('files.uploadedAt') }}</th><th></th></tr></thead>
          <tbody><tr v-for="file in files" :key="file.id"><td><div class="file-title"><span class="file-icon"><FileText :size="17" /></span><strong>{{ file.name }}</strong></div></td><td>{{ formatBytes(file.size) }}</td><td><span class="mime-label">{{ shortMime(file.mime) }}</span></td><td><span class="hash-label" :title="`MD5 ${file.md5}\nSHA-256 ${file.sha256}`"><CheckCircle2 :size="15" /> {{ t('files.hashes') }}</span></td><td>{{ formatDate(file.createdAt) }}</td><td><div class="row-actions"><button class="icon-button" :title="t('files.download')" @click="download(file)"><Download :size="17" /></button><button class="icon-button danger-icon" :title="t('files.delete')" @click="remove(file)"><Trash2 :size="17" /></button></div></td></tr></tbody>
        </table>
        <div v-if="!loading && !files.length" class="empty-state"><FolderOpen :size="34" /><strong>{{ keyword ? t('files.noMatch') : t('files.noFiles') }}</strong><span>{{ keyword ? t('files.noMatchCopy') : t('files.noFilesCopy') }}</span></div>
        <div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('files.loading') }}</span></div>
      </div>
      <div v-if="total > pageSize" class="pagination"><button class="secondary-button" :disabled="page === 1" @click="page--; loadFiles()"><ChevronLeft :size="16" /> {{ t('common.previous') }}</button><span>{{ t('common.page', { page }) }}</span><button class="secondary-button" :disabled="page * pageSize >= total" @click="page++; loadFiles()">{{ t('common.next') }} <ChevronRight :size="16" /></button></div>
      <BrandFooter />
    </section>

    <div v-if="conflictPrompt" class="modal-backdrop" @click.self="chooseConflict('cancel')">
      <section class="modal-panel" role="dialog" aria-modal="true" aria-labelledby="conflict-title">
        <div class="panel-heading"><div><p class="eyebrow">{{ t('files.conflictEyebrow') }}</p><h2 id="conflict-title">{{ t('files.conflictHeading') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="chooseConflict('cancel')"><X :size="18" /></button></div>
        <p class="modal-copy">{{ t('files.conflictCopy', { name: conflictPrompt.existing?.name }) }}</p>
        <div class="conflict-details">{{ formatBytes(conflictPrompt.existing?.size || 0) }} · {{ formatDate(conflictPrompt.existing?.createdAt) }}</div>
        <div class="modal-actions"><button class="secondary-button" @click="chooseConflict('rename')"><FileEdit :size="16" /> {{ t('files.rename') }}<span>{{ t('files.renameHint') }}</span></button><button class="primary-button" @click="chooseConflict('overwrite')"><Replace :size="16" /> {{ t('files.overwrite') }}<span>{{ t('files.overwriteHint') }}</span></button></div>
      </section>
    </div>
  </main>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api, clearSession, localizeError } from '../api'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { currentLocale, t } from '../i18n'
import { CheckCircle2, ChevronLeft, ChevronRight, Download, FileEdit, FileText, FileUp, FolderOpen, LoaderCircle, LogOut, RefreshCw, Replace, ScrollText, Search, Shield, Trash2, Upload, UploadCloud, X } from 'lucide-vue-next'

const router = useRouter()
const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}'))
const files = ref([]); const total = ref(0); const page = ref(1); const pageSize = 20; const keyword = ref(''); const searchInput = ref(''); const loading = ref(false); const error = ref(''); const notice = ref(''); const dragging = ref(false); const uploads = ref([]); const fileInput = ref(null); const conflictPrompt = ref(null)
const quotaPercent = computed(() => Math.min(100, user.value.quotaBytes ? Math.round((user.value.usedBytes / user.value.quotaBytes) * 100) : 0))

// loadMe refreshes the current user and quota snapshot, clearing the session on authentication failure.
// loadMe 刷新当前用户和配额快照，认证失效时清理会话并回到登录页。
async function loadMe() { try { const body = await api('/api/auth/me'); user.value = body.data; localStorage.setItem('filebox_user', JSON.stringify(body.data)) } catch { clearSession(); router.push('/login') } }
// loadFiles loads the file list for the current keyword and page.
// loadFiles 按当前关键字和页码加载文件列表。
async function loadFiles() { loading.value = true; error.value = ''; try { const body = await api(`/api/files?page=${page.value}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword.value)}`); files.value = body.data.items; total.value = body.data.total } catch (err) { error.value = err.message } finally { loading.value = false } }
function search() { page.value = 1; keyword.value = searchInput.value.trim(); loadFiles() }
function handleInput(event) { queueFiles([...event.target.files]); event.target.value = '' }
function handleDrop(event) { dragging.value = false; queueFiles([...event.dataTransfer.files]) }
function queueFiles(list) { list.forEach(uploadFile) }
// uploadFile runs initialization, conflict resolution, single-chunk transfer, and completion verification.
// uploadFile 执行初始化、冲突选择、单分片传输和完成校验的完整上传流程。
async function uploadFile(file) {
  const item = { id: `${Date.now()}-${file.name}`, file, progress: 0, status: t('files.uploadPreparing') }; uploads.value.push(item)
  try {
    let init
    try { init = await requestUploadInit(file) } catch (err) {
      if (err.status !== 409 || !err.data?.conflict) throw err
      const resolve = await askConflict(err.data.existing)
      if (resolve === 'cancel') throw new Error(t('files.uploadCancelled'))
      init = await requestUploadInit(file, resolve)
    }
    item.status = t('files.uploading')
    await uploadChunk(init.data.taskId, file, value => { item.progress = value })
    item.status = t('files.checking'); item.progress = 99
    await api(`/api/files/${init.data.taskId}/complete`, { method: 'POST', body: JSON.stringify({}) })
    item.progress = 100; item.status = t('files.completed'); notice.value = t('files.uploadComplete', { name: file.name }); await loadMe(); await loadFiles()
  } catch (err) { item.status = err.message; error.value = `${file.name}: ${err.message}` }
  setTimeout(() => { uploads.value = uploads.value.filter(entry => entry !== item) }, 2600)
}
function requestUploadInit(file, resolve = '') { return api('/api/files/upload-init', { method: 'POST', body: JSON.stringify({ name: file.name, size: file.size, chunkSize: file.size, mime: file.type, ...(resolve ? { resolve } : {}) }) }) }
function askConflict(existing) { return new Promise(resolve => { conflictPrompt.value = { existing, resolve } }) }
function chooseConflict(value) { const prompt = conflictPrompt.value; conflictPrompt.value = null; prompt?.resolve(value) }
function uploadChunk(taskId, file, onProgress) { return new Promise((resolve, reject) => { const xhr = new XMLHttpRequest(); xhr.open('PUT', `/api/files/${taskId}/chunks/0`); const token = localStorage.getItem('filebox_token'); if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`); xhr.upload.onprogress = event => { if (event.lengthComputable) onProgress(Math.round(event.loaded / event.total * 98)) }; xhr.onerror = () => reject(new Error(t('error.network'))); xhr.onload = () => { try { const body = JSON.parse(xhr.responseText); if (xhr.status >= 200 && xhr.status < 300) resolve(body); else { const err = { status: xhr.status, data: body?.data, backendMessage: body?.message }; reject(new Error(localizeError(err))) } } catch { reject(new Error(t('error.invalidResponse'))) } }; xhr.send(file) }) }
async function download(file) { try { const response = await fetch(`/api/files/${file.id}/download`, { headers: { Authorization: `Bearer ${localStorage.getItem('filebox_token')}` } }); if (!response.ok) { let body = null; try { body = await response.json() } catch {} throw new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })) }; const blob = await response.blob(); const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = file.name; link.click(); URL.revokeObjectURL(link.href) } catch (err) { error.value = err.message } }
async function remove(file) { if (!window.confirm(t('confirm.deleteFile', { name: file.name }))) return; try { await api(`/api/files/${file.id}`, { method: 'DELETE' }); notice.value = t('notice.fileDeleted'); await loadMe(); await loadFiles() } catch (err) { error.value = err.message } }
async function logout() { try { await api('/api/auth/logout', { method: 'POST' }) } finally { clearSession(); router.push('/login') } }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '-' }
function shortMime(value = '') { return value.split('/').pop()?.toUpperCase() || 'FILE' }
onMounted(() => { loadMe(); loadFiles() })
</script>
