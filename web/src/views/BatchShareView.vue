<template>
  <main class="share-shell">
    <header class="share-topbar"><BrandLogo variant="main" compact link /><LanguageSelect /></header>
    <section class="share-content">
      <div v-if="loading" class="empty-state"><LoaderCircle :size="30" class="spin" /><span>{{ t('share.loading') }}</span></div>
      <section v-else-if="error" class="share-error"><XCircle :size="42" /><h1>{{ error }}</h1><div class="share-actions"><button class="primary-button" @click="loadMeta"><RefreshCw :size="16" /> {{ t('share.retry') }}</button><RouterLink to="/login" class="secondary-button"><ArrowLeft :size="16" /> {{ t('share.backLogin') }}</RouterLink></div></section>
      <section v-else class="share-card">
        
        <h1>{{ t('batchShare.title') }}</h1>
        <dl class="share-meta">
          <div><dt>{{ t('batchShare.fileCount') }}</dt><dd>{{ files.length }}</dd></div>
          <div><dt>{{ t('share.owner') }}</dt><dd>{{ meta.createdByName || (meta.createdBy ? meta.createdBy : t('batchShare.owner')) }}</dd></div>
          <div><dt>{{ t('share.expiresAt') }}</dt><dd>{{ formatDate(meta.expiresAt) }}</dd></div>
          <div v-if="meta.maxDownloads"><dt>{{ t('share.availableDownloads', { count: Math.max(0, meta.maxDownloads - meta.downloadCount) }) }}</dt><dd>{{ t('share.downloads', { count: meta.downloadCount }) }}</dd></div>
        </dl>
        <p v-if="downloadExhausted || downloadError" class="alert error">{{ downloadExhausted ? t('share.limitReached') : downloadError }}</p>
        <div class="batch-share-toolbar">
          <label class="check-label batch-select-all"><input type="checkbox" :checked="allSelected" :aria-label="t('files.selectAll')" @change="toggleSelectAll" /> {{ t('files.selectAll') }}</label>
          <button type="button" class="primary-button batch-share-zip" :disabled="downloadLoading || !selectedCount" @click="downloadZip"><Archive :size="17" /> {{ t('batchShare.downloadZip', { count: selectedCount }) }}</button>
        </div>
        <div class="batch-share-list">
          <template v-for="row in visibleRows" :key="row.path">
            <div v-if="row.type === 'dir'" class="batch-share-dir" :style="{ paddingLeft: `${12 + row.depth * 18}px` }">
              <button type="button" class="icon-button dir-toggle" :aria-expanded="row.open" :aria-label="row.name" @click="toggleDir(row.path)"><ChevronDown v-if="row.open" :size="16" /><ChevronRight v-else :size="16" /></button>
              <input type="checkbox" :checked="row.ids.length > 0 && row.ids.every(id => selected.has(id))" :aria-label="row.name" @change="toggleSelectDir(row.ids)" />
              <FolderOpen v-if="row.open" :size="16" class="dir-icon" /><Folder v-else :size="16" class="dir-icon" />
              <strong :title="row.path">{{ row.name }}</strong>
              <small>{{ t('batchShare.fileCount') }} {{ row.ids.length }}</small>
            </div>
            <div v-else class="batch-share-file" :class="{ selected: selected.has(row.file.fileId) }" :style="row.depth ? { paddingLeft: `${12 + row.depth * 18}px` } : null">
              <input type="checkbox" :checked="selected.has(row.file.fileId)" :aria-label="t('files.selectFile', { name: row.file.name })" @change="toggleSelect(row.file.fileId)" />
              <div class="batch-share-file-main"><strong :title="row.name">{{ row.name }}</strong><small>{{ formatBytes(row.file.size) }} · {{ row.file.mime || t('common.none') }}</small></div>
              <button type="button" class="secondary-button" :disabled="downloadLoading" @click="downloadOne(row.file)"><Download :size="16" /> {{ t('share.download') }}</button>
            </div>
          </template>
          <div v-if="!files.length" class="empty-state compact-empty"><Archive :size="30" /><span>{{ t('batchShare.noFiles') }}</span></div>
        </div>
        <BrandFooter />
      </section>
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Archive, ArrowLeft, ChevronDown, ChevronRight, Download, Folder, FolderOpen, LoaderCircle, RefreshCw, XCircle } from 'lucide-vue-next'
import { api, batchDownloadFilename, localizeError } from '../api'
import { brand, loadBrand } from '../brand'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { currentLocale, loadLocale, t } from '../i18n'

// BatchShareView 是聚合分享公开页（/g/:token，v013 #7）：文件列表、单文件下载、全选/选中 ZIP 下载。
// BatchShareView is the aggregate-share public page (/g/:token, v013 #7): file list, single download, select-all/ZIP download.
const route = useRoute()
const token = computed(() => String(route.params.token || ''))
const loading = ref(true); const error = ref(''); const meta = ref({}); const files = ref([]); const selected = ref(new Set()); const downloadLoading = ref(false); const downloadError = ref('')
const selectedCount = computed(() => selected.value.size)
const allSelected = computed(() => files.value.length > 0 && files.value.every(file => selected.value.has(file.fileId)))
const downloadExhausted = computed(() => meta.value.maxDownloads > 0 && (meta.value.downloadAvailable === false || meta.value.downloadCount >= meta.value.maxDownloads))

// v030 #9：按 relativePath 构建目录树，折叠状态记在 path 集合里，渲染时拍平成缩进行。
// v030 #9: build the directory tree from relativePath; collapse state is a path set, rendered as flattened indented rows.
const collapsedDirs = ref(new Set())
const fileTree = computed(() => {
  const dirs = new Map(); const roots = []
  for (const file of files.value) {
    const rel = String(file.relativePath || file.name || '')
    const parts = rel.split('/').filter(Boolean)
    const fileName = parts.pop() || file.name || rel
    let parent = null
    parts.forEach((part, index) => {
      const key = parts.slice(0, index + 1).join('/')
      if (!dirs.has(key)) { const node = { type: 'dir', name: part, path: key, depth: index, ids: [], children: [] }; dirs.set(key, node); if (parent) parent.children.push(node); else roots.push(node) }
      parent = dirs.get(key)
    })
    const entry = { type: 'file', name: fileName, path: rel, depth: parts.length, file }
    if (parent) parent.children.push(entry); else roots.push(entry)
    for (let key = parent?.path; key; key = key.includes('/') ? key.slice(0, key.lastIndexOf('/')) : '') dirs.get(key)?.ids.push(file.fileId)
  }
  const sortDir = entries => { entries.sort((a, b) => (a.type === b.type ? a.name.localeCompare(b.name) : (a.type === 'dir' ? -1 : 1))); entries.forEach(entry => entry.type === 'dir' && sortDir(entry.children)) }
  sortDir(roots)
  return roots
})
const visibleRows = computed(() => {
  const rows = []
  const emit = entries => { for (const entry of entries) { if (entry.type === 'dir') { const open = !collapsedDirs.value.has(entry.path); rows.push({ ...entry, open }); if (open) emit(entry.children) } else rows.push(entry) } }
  emit(fileTree.value)
  return rows
})

function toggleSelect(id) { if (selected.value.has(id)) selected.value.delete(id); else selected.value.add(id) }
function toggleSelectAll() { if (allSelected.value) selected.value.clear(); else files.value.forEach(file => selected.value.add(file.fileId)) }
function toggleDir(path) { const next = new Set(collapsedDirs.value); if (next.has(path)) next.delete(path); else next.add(path); collapsedDirs.value = next }
function toggleSelectDir(ids) { const all = ids.length > 0 && ids.every(id => selected.value.has(id)); ids.forEach(id => { if (all) selected.value.delete(id); else selected.value.add(id) }) }
async function loadMeta() { loading.value = true; error.value = ''; downloadError.value = ''; try { const body = await api(`/api/shared-groups/${encodeURIComponent(token.value)}/meta`); meta.value = body.data; files.value = body.data.files || [] } catch (err) { error.value = err.message } finally { loading.value = false } }
async function requestDownload(path, options, fallbackName) {
  if (downloadLoading.value) return
  if (downloadExhausted.value) { downloadError.value = t('share.limitReached'); return }
  downloadLoading.value = true
  downloadError.value = ''
  try {
    const response = await fetch(path, options)
    if (!response.ok) {
      let body = null
      try { body = await response.json() } catch {}
      const mapped = new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message }))
      mapped.data = body?.data || {}
      throw mapped
    }
    const blob = await response.blob()
    const objectUrl = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectUrl
    const disposition = response.headers.get('Content-Disposition') || ''
    const match = disposition.match(/filename\*?=(?:UTF-8'')?"?([^";]+)"?/i)
    link.download = match ? decodeURIComponent(match[1]) : (fallbackName || batchDownloadFilename())
    document.body.appendChild(link)
    link.click()
    link.remove()
    setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
    await loadMeta()
  } catch (err) {
    if (err.data?.code === 'SHARE_DOWNLOAD_LIMIT') { meta.value.downloadAvailable = false; downloadError.value = '' } else {
      downloadError.value = err instanceof TypeError ? t('error.network') : (err.message || t('error.downloadFailed'))
    }
  } finally { downloadLoading.value = false }
}
function downloadOne(file) { requestDownload(`/api/shared-groups/${encodeURIComponent(token.value)}/download/${file.fileId}`, undefined, file.name) }
function downloadZip() { if (!selectedCount.value) return; requestDownload(`/api/shared-groups/${encodeURIComponent(token.value)}/batch-download`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: [...selected.value] }) }, batchDownloadFilename()) }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '-' }
onMounted(async () => { await loadBrand(); await loadLocale(brand.defaultLang); loadMeta() })
</script>
