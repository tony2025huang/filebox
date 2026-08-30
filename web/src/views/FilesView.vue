<template>
  <main class="app-shell" :class="{ 'read-only': readOnly }">
    <AuthenticatedTopbar :user="user" section="files">
      <template #actions>
        <button class="icon-button transfer-button" :title="t('files.transfers')" @click="transfersOpen = !transfersOpen"><ArrowLeftRight :size="18" /><span v-if="activeTransferCount" class="transfer-badge">{{ activeTransferCount }}</span></button>
      </template>
    </AuthenticatedTopbar>
    <section class="content-wrap">
      <div class="page-heading"><div><p class="eyebrow">WORKSPACE / {{ user.role === 'admin' ? t('files.workspaceAdmin') : t('files.workspaceUser') }}</p><h1>{{ t('files.heading') }}</h1><p class="muted">{{ t('files.copy') }}</p></div><div class="quota-block"><div class="quota-label"><span>{{ t('files.quota') }}</span><strong>{{ formatBytes(user.usedBytes) }} <em>/ {{ formatBytes(user.quotaBytes) }}</em></strong></div><div class="progress-track"><span :style="{ width: quotaPercent + '%' }"></span></div></div></div>
      <div v-if="readOnly" class="alert read-only-notice">{{ t('readOnly.notice') }}</div>
      <div class="dir-bar"><div class="breadcrumb"><button class="breadcrumb-link" :class="{ active: !currentDir }" @click="navigateDir('')">{{ t('files.root') }}</button><template v-for="(seg, i) in breadcrumbs" :key="seg"><span class="breadcrumb-sep">/</span><button class="breadcrumb-link" :class="{ active: i === breadcrumbs.length - 1 }" @click="navigateDir(breadcrumbPath(i))">{{ seg }}</button></template></div><div class="dir-actions"><button v-if="!readOnly" class="secondary-button" @click="openNewFolder"><FolderPlus :size="17" /> {{ t('files.newFolder') }}</button><button v-if="!readOnly" class="secondary-button collection-entry" @click="openCollectionCreate"><UploadCloud :size="16" /> {{ t('collection.create') }}</button></div></div>
      <div v-if="childFolders.length" class="folder-list"><div v-for="folder in childFolders" :key="folder.id" class="folder-row"><button class="folder-entry" @click="navigateDir(folder.path)"><Folder :size="17" /> <strong>{{ folder.name }}</strong></button><div v-if="!readOnly" class="folder-actions"><button class="icon-button" :title="t('files.renameFolder')" @click="openRenameFolder(folder)"><Pencil :size="15" /></button><button class="icon-button danger-icon" :title="t('files.deleteFolder')" @click="removeFolder(folder)"><Trash2 :size="15" /></button></div></div></div>
      <div v-if="!readOnly" class="upload-zone" :class="{ dragging }" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="handleDrop"><UploadCloud :size="26" /><div><strong>{{ t('files.dropTitle') }}</strong><span>{{ t('files.dropCopy') }}</span></div><button class="secondary-button" @click="fileInput?.click()"><Upload :size="17" /> {{ t('files.choose') }}</button><button class="secondary-button" @click="folderInput?.click()"><FolderUp :size="17" /> {{ t('files.uploadFolder') }}</button><input ref="fileInput" type="file" multiple hidden @change="handleInput" /><input ref="folderInput" type="file" webkitdirectory directory multiple hidden @change="handleFolderInput" /></div>
      <section id="collections" class="collection-section"><div class="collection-section-header"><h2>{{ t('collection.my') }}</h2><button class="icon-button" :title="t('files.refresh')" @click="loadCollections"><RefreshCw :size="17" :class="{ spin: collectionsLoading }" /></button></div><div v-if="collectionsLoading" class="empty-state"><LoaderCircle :size="24" class="spin" /></div><div v-else-if="!collections.length" class="transfers-empty">{{ t('collection.noCollections') }}</div><div v-else class="collection-grid"><article v-for="item in collections" :key="item.id" class="collection-row"><strong>{{ item.name }}</strong><small>{{ item.uploadCount }} / {{ item.maxUploads || t('collection.unlimited') }}</small><span class="status-label" :class="{ disabled: item.status !== 'active' }"><i></i>{{ collectionStatusLabel(item) }}</span><button class="icon-text-button" @click="viewCollection(item)"><Eye :size="15" /> {{ t('collection.viewFiles') }}</button><a class="collection-link" :href="absoluteCollectionUrl(item)" target="_blank" rel="noopener">{{ absoluteCollectionUrl(item) }}</a><button class="icon-button" :title="t('collection.copy')" @click="copyCollection(item)"><Copy :size="15" /></button><button v-if="item.status === 'active'" class="icon-button" :title="t('collection.edit')" @click="openCollectionEdit(item)"><Pencil :size="15" /></button><button v-if="item.status === 'active'" class="icon-button danger-icon" :title="t('collection.revoke')" @click="revokeCollection(item)"><Trash2 :size="15" /></button><div v-if="collectionDetails?.id === item.id" class="collection-files"><div v-if="!collectionDetails.files?.length">{{ t('collection.noCollections') }}</div><div v-for="received in collectionDetails.files" :key="received.id" class="collection-file"><strong>{{ received.originalName }}</strong><span v-if="received.remark">{{ t('collection.remark') }}: {{ received.remark }}</span><small>{{ formatDate(received.createdAt) }}</small></div></div></article></div></section>
      <div class="toolbar"><div class="search-box"><Search :size="17" /><input v-model="searchInput" :placeholder="t('files.searchPlaceholder')" @keyup.enter="search" /><button v-if="searchInput" :title="t('files.clearSearch')" @click="searchInput = ''; search()"><X :size="15" /></button></div><button v-if="selectedIds.size" class="secondary-button batch-download-button" :disabled="batchDownloading || batchSharing || batchDeleting" @click="batchDownload"><Archive :size="16" /> {{ t('files.batchDownload', { count: selectedIds.size }) }}</button><button v-if="selectedIds.size && !readOnly" class="secondary-button batch-share-button" :disabled="batchDownloading || batchSharing || batchDeleting" @click="openBatchShare"><Share2 :size="16" /> {{ t('files.batchShare', { count: selectedIds.size }) }}</button><button v-if="selectedIds.size && !readOnly" class="secondary-button batch-delete-button" :disabled="batchDownloading || batchSharing || batchDeleting" @click="batchDelete"><Trash2 :size="16" /> {{ t('files.batchDelete', { count: selectedIds.size }) }}</button><button v-if="batchDownloading" class="icon-button" :title="t('common.cancel')" @click="cancelBatchDownload"><X :size="16" /></button><label class="check-label md5-toggle"><input v-model="showMd5" type="checkbox" @change="persistMd5" /> {{ t('files.showMd5') }}</label><button class="refresh-button" :title="t('files.refresh')" @click="loadFiles"><RefreshCw :size="17" :class="{ spin: loading }" /></button><span class="result-count">{{ t('common.files', { count: total }) }}</span></div>
      <div v-if="notice" class="alert success">{{ notice }}</div><div v-if="error" class="alert error">{{ error }}</div>
      <div class="file-table-wrap"><table class="file-table"><thead><tr><th class="select-col"><input type="checkbox" :checked="allSelected" :aria-label="t('files.selectAll')" @change="toggleSelectAll" /></th><th>{{ t('files.name') }}</th><th>{{ t('files.size') }}</th><th>{{ t('files.type') }}</th><th>{{ t('files.integrity') }}</th><th>{{ t('files.uploadedAt') }}</th><th></th></tr></thead><tbody><tr v-for="file in files" :key="file.id" :class="{ 'row-selected': selectedIds.has(file.id) }"><td class="select-col"><input type="checkbox" :checked="selectedIds.has(file.id)" :aria-label="t('files.selectFile', { name: file.name })" @change="toggleSelect(file.id)" /></td><td><div class="file-title"><span class="file-icon"><component :is="fileIcon(file.mime, file.name)" :size="17" /></span><strong>{{ file.name }}</strong><span v-if="isShared(file)" class="shared-mark" :title="t('files.shared')"><Share2 :size="14" /></span></div></td><td>{{ formatBytes(file.size) }}</td><td><span class="mime-label" :class="{ 'preview-mime': canPreview(file.mime) }">{{ shortMime(file.mime) }}</span></td><td><code v-if="showMd5" class="md5-cell" :title="`MD5 ${file.md5}\nSHA-256 ${file.sha256}`">{{ file.md5 }}</code><span v-else class="hash-label" :title="`MD5 ${file.md5}\nSHA-256 ${file.sha256}`"><CheckCircle2 :size="15" /> {{ t('files.hashes') }}</span></td><td>{{ formatDate(file.createdAt) }}</td><td><div class="row-actions"><button v-if="canPreview(file.mime)" class="icon-button" :title="t('files.preview')" @click="openPreview(file)"><Eye :size="17" /></button><button v-if="!readOnly" class="icon-button" :title="t('files.share')" @click="openShare(file)"><Share2 :size="17" /></button><button class="icon-button" :title="t('files.download')" @click="download(file)"><Download :size="17" /></button><button v-if="!readOnly" class="icon-button danger-icon" :title="t('files.delete')" @click="remove(file)"><Trash2 :size="17" /></button></div></td></tr></tbody></table><div v-if="!loading && !files.length" class="empty-state"><FolderOpen :size="34" /><strong>{{ keyword ? t('files.noMatch') : t('files.noFiles') }}</strong><span>{{ keyword ? t('files.noMatchCopy') : t('files.noFilesCopy') }}</span></div><div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('files.loading') }}</span></div></div>
      <div v-if="total > pageSize" class="pagination"><button class="secondary-button" :disabled="page === 1" @click="page--; loadFiles()"><ChevronLeft :size="16" /> {{ t('common.previous') }}</button><span>{{ t('common.page', { page }) }}</span><button class="secondary-button" :disabled="page * pageSize >= total" @click="page++; loadFiles()">{{ t('common.next') }} <ChevronRight :size="16" /></button></div><BrandFooter />
    </section>
    <div v-if="activeConflict" class="modal-backdrop" @click.self="chooseConflict('cancel')"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ t('files.conflictEyebrow') }}</p><h2>{{ t('files.conflictHeading') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="chooseConflict('cancel')"><X :size="18" /></button></div><p class="modal-copy">{{ t('files.conflictCopy', { name: activeConflict.existing?.name }) }}</p><div class="conflict-details">{{ formatBytes(activeConflict.existing?.size || 0) }} · {{ formatDate(activeConflict.existing?.createdAt) }}</div><div v-if="conflictQueue.length > 1" class="conflict-queue-hint">{{ t('files.conflictQueue', { count: conflictQueue.length }) }}</div><div class="modal-actions"><button class="secondary-button" @click="chooseConflict('rename')"><FileEdit :size="16" /> {{ t('files.rename') }}<span>{{ t('files.renameHint') }}</span></button><button class="primary-button" @click="chooseConflict('overwrite')"><Replace :size="16" /> {{ t('files.overwrite') }}<span>{{ t('files.overwriteHint') }}</span></button></div></section></div>
    <div v-if="shareFile" class="modal-backdrop" @click.self="closeShare"><section class="modal-panel share-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ t('files.shareDialog') }}</p><h2>{{ shareFile.name }}</h2></div><button class="icon-button" :title="t('common.close')" @click="closeShare"><X :size="18" /></button></div><form v-if="!shareResult" @submit.prevent="createShare"><label class="form-label">{{ t('files.shareExpiresHours') }}<input v-model.number="shareForm.expiresInHours" type="number" min="1" required /></label><label class="form-label">{{ t('files.shareMaxDownloads') }}<input v-model.number="shareForm.maxDownloads" type="number" min="0" max="100000" required /></label><p v-if="shareError" class="alert error">{{ shareError }}</p><button class="primary-button submit-button" :disabled="shareLoading"><span>{{ shareLoading ? t('common.loading') : t('files.share') }}</span><Share2 :size="17" /></button></form><div v-else class="share-result"><label class="form-label">{{ t('files.shareUrl') }}<div class="share-url"><input :value="shareAbsoluteUrl" readonly /><button type="button" class="icon-button" :title="t('files.shareCopied')" @click="copyShare"><Copy :size="16" /></button></div></label><p class="muted">{{ t('share.expiresAt') }} {{ formatDate(shareResult.expiresAt) }}<span v-if="shareResult.maxDownloads"> · {{ t('share.availableDownloads', { count: shareResult.maxDownloads - shareResult.downloadCount }) }}</span></p><div class="modal-actions"><button class="secondary-button" @click="openSharePage"><ExternalLink :size="16" /> {{ t('files.openShare') }}</button><button class="secondary-button danger-action" @click="revokeShare"><Trash2 :size="16" /> {{ t('files.revokeShares') }}</button></div><p v-if="shareNotice" class="alert success">{{ shareNotice }}</p></div></section></div>
    <div v-if="previewFile" class="modal-backdrop" @click.self="closePreview"><section class="modal-panel preview-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ t('files.preview') }}</p><h2>{{ previewFile.name }}</h2></div><button class="icon-button" :title="t('common.close')" @click="closePreview"><X :size="18" /></button></div><div v-if="previewLoading" class="empty-state preview-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('files.previewLoading') }}</span></div><p v-else-if="previewError" class="alert error">{{ previewError }}</p><img v-else-if="previewKind === 'image'" class="preview-content preview-image" :src="previewUrl" :alt="previewFile.name" /><video v-else-if="previewKind === 'video'" class="preview-content" :src="previewUrl" controls></video><iframe v-else-if="previewKind === 'pdf'" class="preview-content preview-frame" :src="previewUrl" :title="previewFile.name"></iframe><pre v-else class="preview-text">{{ previewText }}</pre></section></div>
    <div v-if="batchShareOpen" class="modal-backdrop" @click.self="closeBatchShare"><section class="modal-panel batch-share-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ t('files.batchShare') }}</p><h2>{{ t('files.batchShareTitle', { count: batchShareCount }) }}</h2></div><button class="icon-button" :title="t('common.close')" @click="closeBatchShare"><X :size="18" /></button></div><form v-if="!batchShareResults.length" @submit.prevent="createBatchShare"><label class="form-label">{{ t('files.shareExpiresHours') }}<input v-model.number="batchShareForm.expiresInHours" type="number" min="1" required /></label><label class="form-label">{{ t('files.shareMaxDownloads') }}<input v-model.number="batchShareForm.maxDownloads" type="number" min="0" max="100000" required /></label><p v-if="batchShareError" class="alert error">{{ batchShareError }}</p><button class="primary-button submit-button" :disabled="batchSharing"><span>{{ batchSharing ? t('common.loading') : t('files.batchShareCreate') }}</span><Share2 :size="17" /></button></form><div v-else class="batch-share-results"><p class="muted">{{ t('files.batchShareCreated', { count: batchShareResults.length }) }}</p><div v-for="item in batchShareResults" :key="item.fileId" class="batch-share-result"><strong :title="item.fileName">{{ item.fileName }}</strong><div class="share-url"><input :value="absoluteBatchShareUrl(item)" readonly /><button type="button" class="icon-button" :title="t('files.shareCopied')" @click="copyBatchShare(item)"><Copy :size="16" /></button></div></div><p v-if="batchShareNotice" class="alert success">{{ batchShareNotice }}</p><div class="modal-actions"><button class="primary-button" @click="closeBatchShare">{{ t('common.close') }}</button></div></div></section></div>
    <div v-if="folderPrompt" class="modal-backdrop" @click.self="folderPrompt = null"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">FOLDER</p><h2>{{ folderPrompt.rename ? t('files.renameFolder') : t('files.newFolder') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="folderPrompt = null"><X :size="18" /></button></div><form @submit.prevent="submitFolder"><label class="form-label">{{ t('files.folderName') }}<input v-model.trim="folderPrompt.name" maxlength="255" required autofocus /></label><p v-if="folderError" class="alert error">{{ folderError }}</p><div class="modal-actions"><button class="primary-button" :disabled="folderSaving"><Save :size="16" /> {{ t('common.save') }}</button><button type="button" class="secondary-button" @click="folderPrompt = null">{{ t('common.cancel') }}</button></div></form></section></div>
    <div v-if="collectionCreateOpen" class="modal-backdrop" @click.self="collectionCreateOpen = false"><section class="modal-panel share-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ editingCollection ? t('collection.editTitle') : t('collection.create') }}</p><h2>{{ editingCollection ? (collectionForm.name || t('collection.editTitle')) : t('collection.createTitle') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="collectionCreateOpen = false"><X :size="18" /></button></div><form @submit.prevent="saveCollection"><label class="form-label">{{ t('collection.name') }}<input v-model.trim="collectionForm.name" maxlength="255" required /></label><label v-if="!editingCollection" class="form-label">{{ t('collection.expiresInHours') }}<input v-model.number="collectionForm.expiresInHours" type="number" min="1" required /></label><label v-else class="form-label">{{ t('collection.expiresAtEdit') }}<input v-model="collectionForm.expiresAtLocal" type="datetime-local" required /></label><label class="form-label">{{ t('collection.maxUploads') }}<input v-model.number="collectionForm.maxUploads" type="number" min="0" required /></label><label class="form-label">{{ t('collection.maxFileBytesMB') }}<input v-model.number="collectionForm.maxFileBytesMB" type="number" min="0" required /></label><p v-if="collectionError" class="alert error">{{ collectionError }}</p><button class="primary-button submit-button" :disabled="collectionSaving"><span>{{ collectionSaving ? t('common.loading') : (editingCollection ? t('collection.saved') : t('collection.create')) }}</span><UploadCloud :size="17" /></button></form></section></div>
    <div v-if="collectionResult" class="modal-backdrop" @click.self="collectionResult = null"><section class="modal-panel share-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><p class="eyebrow">{{ t('collection.create') }}</p><h2>{{ collectionResult.name }}</h2></div><button class="icon-button" :title="t('common.close')" @click="collectionResult = null"><X :size="18" /></button></div><label class="form-label">{{ t('collection.link') }}<div class="share-url"><input :value="absoluteCollectionUrl(collectionResult)" readonly /><button type="button" class="icon-button" :title="t('collection.copy')" @click="copyCollection(collectionResult)"><Copy :size="16" /></button></div></label><p v-if="collectionNotice" class="alert success">{{ collectionNotice }}</p></section></div>
    <div v-if="transfersOpen" class="transfers-backdrop" @click="transfersOpen = false"></div>
    <aside class="transfers-drawer" :class="{ open: transfersOpen }" aria-label="transfers">
      <div class="transfers-header"><div><p class="eyebrow">TRANSFERS</p><h2>{{ t('files.transfers') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="transfersOpen = false"><X :size="18" /></button></div>
      <div class="transfers-body">
        <div v-if="overallRate > 0" class="overall-rate"><Gauge :size="16" /><span>{{ t('files.overallRate') }}</span><strong>{{ formatRate(overallRate) }}</strong></div>
        <h3 class="transfers-section">{{ t('files.uploads') }}<span class="transfers-count">{{ uploads.length }}</span></h3>
        <div v-if="!uploads.length" class="transfers-empty">{{ t('files.noTransfers') }}</div>
        <div v-for="item in uploads" :key="item.id" class="transfer-row" :class="{ 'transfer-failed': item.failed }"><FileUp :size="16" class="transfer-icon" /><div class="transfer-main"><div class="transfer-name"><strong :title="item.relPath || item.file.name">{{ item.relPath || item.file.name }}</strong><span :class="{ 'transfer-error-text': item.failed }" :title="item.failed ? (item.error || item.status) : item.status">{{ item.failed ? item.error || item.status : item.status }}</span></div><div class="progress-track"><span :style="{ width: item.progress + '%' }"></span></div></div><span class="transfer-percent">{{ item.failed ? '' : item.progress + '%' }}</span><button v-if="item.paused" class="icon-button" :title="t('files.resume')" @click="resumeUpload(item)"><Play :size="15" /></button><button v-else-if="item.running" class="icon-button" :title="t('files.pause')" @click="pauseUpload(item)"><Pause :size="15" /></button><button v-if="item.canContinue || item.failed" class="icon-button" :title="t('files.retry')" @click="retryUpload(item)"><RefreshCw :size="15" /></button><button v-if="item.failed" class="icon-button" :title="t('files.dismiss')" @click="dismissUpload(item)"><X :size="15" /></button></div>
        <h3 class="transfers-section">{{ t('files.downloads') }}<span class="transfers-count">{{ downloads.length }}</span></h3>
        <div v-if="!downloads.length" class="transfers-empty">{{ t('files.noTransfers') }}</div>
        <div v-for="item in downloads" :key="item.id" class="transfer-row" :class="{ 'transfer-failed': item.failed }"><Download :size="16" class="transfer-icon" /><div class="transfer-main"><div class="transfer-name"><strong :title="item.name">{{ item.name }}</strong><span :class="{ 'transfer-error-text': item.failed }" :title="item.status">{{ item.status }}</span></div><div class="progress-track"><span :style="{ width: (item.progress < 0 ? 0 : item.progress) + '%' }"></span></div><div class="transfer-detail"><span>{{ t('download.detail.transferred', { loaded: formatBytes(item.loadedBytes), total: item.size > 0 ? formatBytes(item.size) : t('download.detail.unknown') }) }}</span><span>{{ item.progress < 0 ? t('download.detail.unknown') : item.progress + '%' }}</span><span>{{ t('download.detail.rate', { rate: formatRate(item.rate) }) }}</span></div></div><span class="transfer-percent">{{ item.progress < 0 ? '' : item.progress + '%' }}</span><button v-if="item.running" class="icon-button" :title="t('common.cancel')" @click="cancelDownload(item)"><X :size="15" /></button></div>
      </div>
    </aside>
  </main>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, clearSession, computeFileSHA256, localizeError } from '../api'
import AuthenticatedTopbar from '../components/AuthenticatedTopbar.vue'
import BrandFooter from '../components/BrandFooter.vue'
import { brand } from '../brand'
import { currentLocale, t } from '../i18n'
import { Archive, CheckCircle2, ChevronLeft, ChevronRight, Copy, Download, ExternalLink, Eye, File, FileArchive, FileCode, FileEdit, FileImage, FileJson, FileSpreadsheet, FileText, FileType, FileUp, FileVideo, Folder, FolderOpen, FolderPlus, FolderUp, Gauge, Image, LoaderCircle, Music, Pause, Pencil, Play, RefreshCw, Replace, Save, Search, Share2, Table, Trash2, Upload, UploadCloud, Video, X, ArrowLeftRight } from 'lucide-vue-next'

const router = useRouter(); const route = useRoute(); const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}')); const files = ref([]); const total = ref(0); const page = ref(1); const pageSize = 20; const keyword = ref(''); const searchInput = ref(''); const loading = ref(false); const error = ref(''); const notice = ref(''); const dragging = ref(false); const uploads = ref([]); const downloads = ref([]); const transfersOpen = ref(false); const showMd5 = ref(localStorage.getItem('filebox_show_md5') !== '0'); const fileInput = ref(null); const folderInput = ref(null); const conflictQueue = ref([]); const currentDir = ref(''); const folders = ref([]); const folderPrompt = ref(null); const folderSaving = ref(false); const folderError = ref('')
const shareFile = ref(null); const shareForm = ref({ expiresInHours: 24, maxDownloads: 0 }); const shareResult = ref(null); const shareLoading = ref(false); const shareError = ref(''); const shareNotice = ref(''); const previewFile = ref(null); const previewLoading = ref(false); const previewError = ref(''); const previewUrl = ref(''); const previewText = ref(''); const previewKind = ref(''); const sharedIds = new Set(JSON.parse(localStorage.getItem('filebox_shared_ids') || '[]')); const chunkQueue = []; let activeWorkers = 0; let workerWake = null
const collections = ref([]); const collectionsLoading = ref(false); const collectionCreateOpen = ref(false); const collectionSaving = ref(false); const collectionError = ref(''); const collectionNotice = ref(''); const collectionResult = ref(null); const collectionDetails = ref(null); const editingCollection = ref(false); const collectionForm = ref({ name: '', expiresInHours: 24, maxUploads: 0, maxFileBytesMB: 0, expiresAtLocal: '' })
// 多选聚合下载状态：selectedIds 为当前选中文件集合，翻页/搜索时保留已选项。
// Batch-download selection: selectedIds holds the chosen file ids and survives page/search changes.
const selectedIds = reactive(new Set())
const batchDownloading = ref(false)
const batchDeleting = ref(false)
let batchDownloadController = null
let batchDownloadItem = null
const allSelected = computed(() => files.value.length > 0 && files.value.every(file => selectedIds.has(file.id)))
const readOnly = computed(() => user.value.readOnly === true)
const batchShareOpen = ref(false)
const batchSharing = ref(false)
const batchShareError = ref('')
const batchShareNotice = ref('')
const batchShareResults = ref([])
const batchShareForm = ref({ expiresInHours: 24, maxDownloads: 0 })
const batchShareCount = ref(0)
function toggleSelect(id) { if (selectedIds.has(id)) selectedIds.delete(id); else selectedIds.add(id) }
function toggleSelectAll() { if (allSelected.value) files.value.forEach(file => selectedIds.delete(file.id)); else files.value.forEach(file => selectedIds.add(file.id)) }
function openBatchShare() { if (readOnly.value) return; batchShareCount.value = selectedIds.size; batchShareOpen.value = true; batchShareError.value = ''; batchShareNotice.value = ''; batchShareResults.value = []; batchShareForm.value = { expiresInHours: 24, maxDownloads: 0 } }
function closeBatchShare() { if (!batchSharing.value) batchShareOpen.value = false }
async function createBatchShare() {
  if (readOnly.value) { batchShareError.value = t('readOnly.error'); return }
  const fileIds = [...selectedIds]
  if (!fileIds.length) return
  batchSharing.value = true
  batchShareError.value = ''
  try {
    const body = await api('/api/files/batch-share', { method: 'POST', body: JSON.stringify({ fileIds, ...batchShareForm.value }) })
    batchShareResults.value = body.data?.items || []
    batchShareResults.value.forEach(item => sharedIds.add(item.fileId))
    localStorage.setItem('filebox_shared_ids', JSON.stringify([...sharedIds]))
    selectedIds.clear()
  } catch (err) {
    batchShareError.value = err.message
  } finally {
    batchSharing.value = false
  }
}
function absoluteBatchShareUrl(item) { return item?.url ? new URL(item.url, window.location.origin).href : '' }
async function copyBatchShare(item) {
  const value = absoluteBatchShareUrl(item)
  try { await navigator.clipboard.writeText(value) } catch { const input = document.createElement('textarea'); input.value = value; document.body.appendChild(input); input.select(); document.execCommand('copy'); input.remove() }
  batchShareNotice.value = t('files.shareCopied')
}
async function batchDownload() {
  const ids = [...selectedIds]
  if (!ids.length) return
  batchDownloading.value = true
  error.value = ''
  const controller = new AbortController()
  batchDownloadController = controller
  const item = { id: `dl-batch-${Date.now()}`, name: t('download.detail.batchName'), size: 0, loadedBytes: 0, progress: 0, rate: 0, status: t('files.downloading'), failed: false, cancelled: false, running: true, error: '', controller }
  batchDownloadItem = item
  downloads.value.push(item)
  transfersOpen.value = true
  persistTransfers()
  try {
    await streamDownload(item, () => fetch('/api/files/batch-download', { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('filebox_token')}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ ids }), signal: controller.signal }), 'filebox-batch-download.zip')
    selectedIds.clear()
    notice.value = t('files.batchDownloadDone', { count: ids.length })
  } catch (err) {
    if (err.name === 'AbortError' || item.cancelled) { item.status = t('download.detail.cancelled'); item.progress = item.progress < 0 ? 0 : item.progress } else { item.failed = true; item.error = err.message; item.status = err.message; error.value = `${err.message}` }
  } finally { item.running = false; if (batchDownloadController === controller) batchDownloadController = null; if (batchDownloadItem === item) batchDownloadItem = null; batchDownloading.value = false; setTimeout(() => { downloads.value = downloads.value.filter(entry => entry !== item) }, 4000) }
}
function cancelBatchDownload() { if (batchDownloadItem) batchDownloadItem.cancelled = true; batchDownloadController?.abort() }
function cancelDownload(item) { item.cancelled = true; item.controller?.abort() }
// batchDelete 确认并删除选中文件，随后刷新配额和当前目录列表。
// batchDelete confirms and removes the selected files, then refreshes quota and the current directory.
async function batchDelete() {
  if (readOnly.value) { error.value = t('readOnly.error'); return }
  const ids = [...selectedIds]
  if (!ids.length || !window.confirm(t('confirm.deleteFiles', { count: ids.length }))) return
  batchDeleting.value = true
  error.value = ''
  try {
    await api('/api/files/batch-delete', { method: 'POST', body: JSON.stringify({ ids }) })
    selectedIds.clear()
    notice.value = t('notice.filesDeleted', { count: ids.length })
    await loadMe()
    await loadFiles()
  } catch (err) {
    error.value = err.message
  } finally {
    batchDeleting.value = false
  }
}
const quotaPercent = computed(() => Math.min(100, user.value.quotaBytes ? Math.round((user.value.usedBytes / user.value.quotaBytes) * 100) : 0))
// 整体上传速率：1s 采样所有进行中上传的合计 loadedBytes，3 秒滑动平均平滑。
// Overall upload rate: sample the total loadedBytes of active uploads every second and smooth with a 3-second moving average.
const overallRate = ref(0)
let rateWindow = []
let lastRateAt = 0
let lastRateBytes = 0
let rateTimer = null
function sampleOverallRate() {
  const now = Date.now()
  const active = uploads.value.filter(u => u.running && !u.paused && !u.failed)
  const total = active.reduce((sum, item) => sum + (item.loadedBytes || 0), 0)
  if (!active.length) { overallRate.value = 0; rateWindow = []; lastRateAt = 0; lastRateBytes = 0; return }
  if (lastRateAt > 0) {
    const elapsed = (now - lastRateAt) / 1000
    const delta = total - lastRateBytes
    if (elapsed > 0 && delta >= 0) { rateWindow.push(delta / elapsed); if (rateWindow.length > 3) rateWindow.shift(); overallRate.value = rateWindow.reduce((s, v) => s + v, 0) / rateWindow.length } else { rateWindow = [] }
  }
  lastRateAt = now
  lastRateBytes = total
}
// formatRate 自适应单位显示速率（B/KB/MB/GB per s）。
// formatRate formats a byte rate with adaptive units (B/KB/MB/GB per second).
function formatRate(bytesPerSecond = 0) {
  if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(bytesPerSecond < 10 ? 1 : 0)} B/s`
  const units = ['KB/s', 'MB/s', 'GB/s']
  let value = bytesPerSecond
  let unit = -1
  do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1)
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`
}
// activeTransferCount 顶栏角标：进行中或待处理的上传 + 未完成的下载数量。
// activeTransferCount is the topbar badge: running/pending uploads plus unfinished downloads.
const activeTransferCount = computed(() => uploads.value.filter(u => u.running || u.failed || u.paused).length + downloads.value.filter(d => d.running && !d.failed && !d.cancelled).length)
function persistMd5() { localStorage.setItem('filebox_show_md5', showMd5.value ? '1' : '0') }
// breadcrumbs 把当前目录路径拆成可点击的分段（根 / a / b）。
// breadcrumbs splits the current directory path into clickable segments (root / a / b).
const breadcrumbs = computed(() => currentDir.value ? currentDir.value.split('/') : [])
function breadcrumbPath(index) { return breadcrumbs.value.slice(0, index + 1).join('/') }
// childFolders 只取当前目录的直接子目录。
// childFolders selects only the direct children of the current directory.
const childFolders = computed(() => { const cd = currentDir.value; return folders.value.filter(folder => { const idx = folder.path.lastIndexOf('/'); return (idx >= 0 ? folder.path.slice(0, idx) : '') === cd }) })

// loadMe refreshes the current user and quota snapshot, clearing the session on authentication failure.
// loadMe 刷新当前用户和配额快照，认证失效时清理会话并回到登录页。
async function loadMe() { try { const body = await api('/api/auth/me'); user.value = body.data; localStorage.setItem('filebox_user', JSON.stringify(body.data)) } catch { clearSession(); router.push('/login') } }
// loadFiles loads the file list for the current keyword, page, and directory.
// loadFiles 按当前关键字、页码与目录加载文件列表。
async function loadFiles() { loading.value = true; error.value = ''; try { const dirQuery = currentDir.value ? `&dir=${encodeURIComponent(currentDir.value)}` : ''; const body = await api(`/api/files?page=${page.value}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword.value)}${dirQuery}`); files.value = body.data.items; total.value = body.data.total } catch (err) { error.value = err.message } finally { loading.value = false } }
// loadFolders 拉取当前用户的全部目录，用于面包屑与子目录导航。
// loadFolders fetches all of the user's folders for breadcrumbs and child-folder navigation.
async function loadFolders() { try { const body = await api('/api/folders'); folders.value = body.data.items } catch { /* 目录不可用时保持空导航 */ } }
async function loadCollections() { collectionsLoading.value = true; try { const body = await api('/api/collections'); collections.value = body.data.items || [] } catch (err) { error.value = err.message } finally { collectionsLoading.value = false } }
function openCollectionCreate() { editingCollection.value = false; collectionCreateOpen.value = true; collectionResult.value = null; collectionError.value = ''; collectionNotice.value = ''; collectionForm.value = { name: '', expiresInHours: 24, maxUploads: 0, maxFileBytesMB: 0, expiresAtLocal: '' } }
// toLocalInputValue 把 UTC RFC3339 转成 datetime-local 需要的本地时间字符串。
// toLocalInputValue converts a UTC RFC3339 string to the local-time format expected by datetime-local inputs.
function toLocalInputValue(value) { const date = new Date(value); if (Number.isNaN(date.getTime())) return ''; const pad = num => String(num).padStart(2, '0'); return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}` }
function openCollectionEdit(item) { editingCollection.value = true; collectionCreateOpen.value = true; collectionResult.value = null; collectionError.value = ''; collectionNotice.value = ''; collectionForm.value = { _id: item.id, name: item.name || '', expiresInHours: 24, maxUploads: item.maxUploads || 0, maxFileBytesMB: item.maxFileBytes ? Math.round(item.maxFileBytes / 1024 / 1024 * 100) / 100 : 0, expiresAtLocal: toLocalInputValue(item.expiresAt) } }
// saveCollection 创建或更新收集链接：编辑模式提交绝对到期时间（转换 UTC）。
// saveCollection creates or updates a collection: edit mode submits an absolute expiry converted to UTC.
async function saveCollection() { collectionSaving.value = true; collectionError.value = ''; try { let body; if (editingCollection.value) { body = await api(`/api/collections/${collectionForm.value._id}`, { method: 'PUT', body: JSON.stringify({ name: collectionForm.value.name, expiresAt: new Date(collectionForm.value.expiresAtLocal).toISOString(), maxUploads: collectionForm.value.maxUploads, maxFileBytes: Math.round(collectionForm.value.maxFileBytesMB * 1024 * 1024) }) }) } else { body = await api('/api/collections', { method: 'POST', body: JSON.stringify({ name: collectionForm.value.name, expiresInHours: collectionForm.value.expiresInHours, maxUploads: collectionForm.value.maxUploads, maxFileBytes: Math.round(collectionForm.value.maxFileBytesMB * 1024 * 1024) }) }) } if (editingCollection.value) { collectionNotice.value = t('collection.saved') } else { collectionResult.value = body.data } collectionCreateOpen.value = false; await loadCollections() } catch (err) { collectionError.value = err.message } finally { collectionSaving.value = false } }
function absoluteCollectionUrl(item) { return item?.url ? new URL(item.url, window.location.origin).href : '' }
async function copyCollection(item) { try { await navigator.clipboard.writeText(absoluteCollectionUrl(item)); collectionNotice.value = t('collection.copied') } catch { const input = document.querySelector('.collection-result input, .share-url input'); input?.select(); document.execCommand('copy'); collectionNotice.value = t('collection.copied') } }
function remainingLabel(item) { if (!item.remainingSeconds) return `${t('collection.remaining')}: 0`; const hours = Math.floor(item.remainingSeconds / 3600); const minutes = Math.max(1, Math.floor(item.remainingSeconds / 60)); return `${t('collection.remaining')}: ${hours ? `${hours}h` : `${minutes}m`}` }
function collectionStatusLabel(item) { const label = item.status === 'expired' ? t('collection.expired') : item.status === 'revoked' ? t('collection.revoked') : item.status === 'limit_reached' ? t('collection.limitReached') : t('collection.active'); return `${label} · ${remainingLabel(item)}` }
async function viewCollection(item) { if (collectionDetails.value?.id === item.id) { collectionDetails.value = null; return } try { const body = await api(`/api/collections/${item.id}`); collectionDetails.value = body.data } catch (err) { error.value = err.message } }
async function revokeCollection(item) { if (!window.confirm(t('collection.confirmRevoke'))) return; try { await api(`/api/collections/${item.id}`, { method: 'DELETE' }); collectionNotice.value = t('collection.revoked'); await loadCollections() } catch (err) { error.value = err.message } }
function search() { page.value = 1; keyword.value = searchInput.value.trim(); loadFiles() }
function navigateDir(path) { currentDir.value = path; page.value = 1; loadFiles() }
function openNewFolder() { if (readOnly.value) return; folderPrompt.value = { rename: false, name: '' }; folderError.value = '' }
function openRenameFolder(folder) { if (readOnly.value) return; folderPrompt.value = { rename: true, id: folder.id, name: folder.name }; folderError.value = '' }
// submitFolder 创建或重命名目录，成功后刷新目录与文件列表。
// submitFolder creates or renames a folder, then refreshes folders and files.
async function submitFolder() { if (readOnly.value) { folderError.value = t('readOnly.error'); return } const prompt = folderPrompt.value; if (!prompt?.name) return; folderSaving.value = true; folderError.value = ''; try { if (prompt.rename) { await api(`/api/folders/${prompt.id}`, { method: 'PATCH', body: JSON.stringify({ name: prompt.name }) }); notice.value = t('notice.folderRenamed') } else { await api('/api/folders', { method: 'POST', body: JSON.stringify({ name: prompt.name, parent: currentDir.value }) }); notice.value = t('notice.folderCreated') } folderPrompt.value = null; loadFolders(); loadFiles() } catch (err) { folderError.value = err.message } finally { folderSaving.value = false } }
async function removeFolder(folder) { if (readOnly.value) { error.value = t('readOnly.error'); return } if (!window.confirm(t('confirm.deleteFolder', { name: folder.name }))) return; try { await api(`/api/folders/${folder.id}`, { method: 'DELETE' }); notice.value = t('notice.folderDeleted'); loadFolders(); loadFiles() } catch (err) { error.value = err.message } }
function handleInput(event) { queueFiles([...event.target.files]); event.target.value = '' }
function handleFolderInput(event) { queueFiles([...event.target.files]); event.target.value = '' }

// collectDropFiles walks browser directory entries and falls back to File.webkitRelativePath when unavailable.
// collectDropFiles 递归读取浏览器目录条目，不支持条目 API 时回退到 webkitRelativePath。
async function collectDropFiles(event) { const items = [...(event.dataTransfer?.items || [])]; if (!items.some(item => typeof item.webkitGetAsEntry === 'function')) return [...(event.dataTransfer?.files || [])].map(file => ({ file, relPath: file.webkitRelativePath || file.name })); const result = []; async function visit(entry, prefix = '') { if (entry.isFile) { const file = await new Promise((resolve, reject) => entry.file(resolve, reject)); result.push({ file, relPath: `${prefix}${file.name}` }); return } if (!entry.isDirectory) return; const reader = entry.createReader(); const entries = []; while (true) { const batch = await new Promise((resolve, reject) => reader.readEntries(resolve, reject)); if (!batch.length) break; entries.push(...batch) } for (const child of entries) await visit(child, `${prefix}${entry.name}/`) } for (const item of items) { const entry = item.webkitGetAsEntry?.(); if (entry) await visit(entry) } return result }
async function handleDrop(event) { dragging.value = false; const picked = await collectDropFiles(event); if (!picked.length) { notice.value = t('files.dropEmpty'); return } queueFiles(picked) }

// friendlyError 区分真实网络失败与业务错误，避免把不可上传条目误报为网络问题。
// friendlyError maps genuine network failures to the network message and passes business errors through.
function friendlyError(err) { if (err && (err.name === 'TypeError' || err.message === 'Failed to fetch' || err.message === 'NetworkError')) return new Error(t('error.network')); return err }

// —— 传输记录会话级持久化（#2）：刷新后恢复可序列化快照，File/AbortController/Set/Map 一律不入库。——
// —— Session-level transfer persistence (#2): only serializable snapshots survive refresh; File/AbortController/Set/Map are never stored. ——
const TRANSFERS_KEY = 'filebox_transfers_v1'
function snapshotUpload(item) {
  return {
    kind: 'upload', id: item.id, name: item.file?.name || item.name || '', relPath: item.relPath || '', dir: item.dir || '',
    size: item.file?.size || item.size || 0, progress: item.progress || 0, loadedBytes: item.loadedBytes || 0,
    status: item.status || '', taskId: item.taskId || '', sha256: item.sha256 || '',
    paused: !!item.paused, failed: !!item.failed, error: item.error || '', canContinue: !!item.canContinue
  }
}
function snapshotDownload(item) {
  return { kind: 'download', id: item.id, name: item.name || '', size: item.size || 0, loadedBytes: item.loadedBytes || 0, progress: item.progress ?? 0, status: item.status || '', failed: !!item.failed, error: item.error || '' }
}
// persistTransfers 把当前面板记录的可序列化快照写入 sessionStorage（按用户隔离，退出登录时清理）。
// persistTransfers writes serializable snapshots of the current drawer into sessionStorage (per-user; cleared on logout).
function persistTransfers() {
  try {
    const data = [...uploads.value.map(snapshotUpload), ...downloads.value.map(snapshotDownload)]
    sessionStorage.setItem(TRANSFERS_KEY, JSON.stringify(data))
  } catch { /* 存储满或隐私模式时静默失败，不影响传输本身 */ }
}
// restoreTransfers 在挂载时恢复快照：上传项标记 needsReselect（用户重选同名同大小文件即续传），
// 下载项仅作记录展示（Blob/流刷新即失，无法续传）。
// restoreTransfers restores snapshots on mount: upload items are marked needsReselect (re-picking the same
// name+size file resumes them); download items are display-only (Blob/streams cannot survive a refresh).
function restoreTransfers() {
  let data = []
  try { data = JSON.parse(sessionStorage.getItem(TRANSFERS_KEY) || '[]') } catch { sessionStorage.removeItem(TRANSFERS_KEY); return }
  for (const snap of data) {
    if (snap.kind === 'download') {
      downloads.value.push({ id: snap.id, name: snap.name, size: snap.size || 0, loadedBytes: snap.loadedBytes || 0, progress: snap.progress ?? 0, rate: 0, status: t('files.sessionEnded'), failed: snap.failed, cancelled: true, running: false, error: snap.error || '', controller: null, restored: true })
    } else if (snap.kind === 'upload') {
      uploads.value.push({
        id: snap.id, file: null, name: snap.name, relPath: snap.relPath || '', dir: snap.dir || '', size: snap.size || 0,
        progress: snap.progress || 0, loadedBytes: snap.loadedBytes || 0, status: snap.failed ? (snap.error || t('files.uploadFailed')) : t('files.needReselect'),
        taskId: snap.taskId || '', sha256: snap.sha256 || '', uploaded: [], paused: false, chunksTotal: 0, chunkSize: 0,
        error: snap.error || '', failed: false, canContinue: false, running: false, pending: new Set(), controllers: new Map(), needsReselect: true, restored: true
      })
    }
  }
  // 恢复后立即消费快照，避免重复恢复；后续状态由 persistTransfers 维护。
  // Consume the snapshot immediately to avoid double restoration; later state is maintained by persistTransfers.
  sessionStorage.removeItem(TRANSFERS_KEY)
}

// queueFiles creates one resumable task per selected file and preserves folder-relative directories.
// queueFiles 为每个文件创建可续传任务，并保留文件夹相对目录；上传目标为当前浏览目录。
// 上传并发闸门：同时最多 3 个文件进入校验/初始化阶段，避免大批量文件夹上传时
// 同源连接数超限导致部分文件误报"网络连接失败"（问题 3）。
// Upload gate: at most 3 files enter checksum/init concurrently so bulk folder uploads do not
// exhaust the browser's same-origin connection limit and fail with a misleading network error.
let uploadInFlight = 0
function queueFiles(list) { if (readOnly.value) { error.value = t('readOnly.error'); return } if (list.length) transfersOpen.value = true; const configuredMax = Number(brand.maxFileSize); const maxSize = Number.isFinite(configuredMax) && configuredMax > 0 ? configuredMax : 100 * 1024 * 1024 * 1024; const oversized = list.filter(value => (value.file || value).size > maxSize); const keep = list.filter(value => (value.file || value).size <= maxSize); if (oversized.length) { error.value = t('files.tooManyTooLarge', { count: oversized.length, max: formatBytes(maxSize) }) } if (keep.length > 50 && !window.confirm(t('files.bulkConfirm', { count: keep.length }))) return; keep.forEach(value => { const file = value.file || value; const path = value.relPath || file.webkitRelativePath || file.name; const parts = path.split('/').filter(Boolean); const dirParts = parts.length > 1 ? parts.slice(0, -1) : []; if (dirParts.length > 1) dirParts.shift(); const base = currentDir.value ? currentDir.value + '/' : ''; const relDir = dirParts.length ? dirParts.join('/') : ''; const restored = uploads.value.find(entry => entry.needsReselect && entry.name === file.name && entry.size === file.size); if (restored) { restored.file = file; restored.needsReselect = false; restored.paused = false; restored.failed = false; restored.error = ''; restored.status = t('files.uploadPreparing'); runGated(restored); persistTransfers(); return } const item = { id: `${Date.now()}-${Math.random()}-${file.name}`, file, relPath: path !== file.name ? path : '', dir: relDir ? `${base}${relDir}` : base.replace(/\/$/, ''), progress: 0, loadedBytes: 0, status: t('files.uploadPreparing'), taskId: '', uploaded: [], paused: false, chunksTotal: 0, chunkSize: 0, error: '', failed: false, canContinue: false, running: false, sha256: '', pending: new Set(), controllers: new Map(), resolve: '' }; uploads.value.push(item); runGated(item); persistTransfers() }) }
async function runGated(item) { if (item.paused || item.failed) return; while (uploadInFlight >= 3) { if (item.paused || item.failed) return; await new Promise(resolve => setTimeout(resolve, 120)) } uploadInFlight++; try { await startUpload(item) } finally { uploadInFlight-- } }
function wakeWorkers() { const wake = workerWake; workerWake = null; wake?.() }
function removeQueued(item) { for (let i = chunkQueue.length - 1; i >= 0; i--) if (chunkQueue[i].item === item) chunkQueue.splice(i, 1) }
function enqueueChunks(item, indexes) { indexes.forEach(index => { item.pending.add(index); chunkQueue.push({ item, index }) }); wakeWorkers(); ensureWorkers() }
function ensureWorkers() { while (activeWorkers < 4 && chunkQueue.some(task => !task.item.paused && !task.item.failed)) { activeWorkers++; chunkWorker().finally(() => { activeWorkers--; ensureWorkers() }) } }
async function chunkWorker() { while (true) { const position = chunkQueue.findIndex(task => !task.item.paused && !task.item.failed); if (position < 0) return; const task = chunkQueue.splice(position, 1)[0]; const { item, index } = task; if (!item.pending.has(index)) continue; try { await uploadChunkWithRetry(item, index); item.pending.delete(index); if (!item.uploaded.includes(index)) item.uploaded.push(index); item.uploaded.sort((a, b) => a - b); updateChunkProgress(item) } catch (err) { if (item.paused || err.name === 'AbortError') { chunkQueue.push(task); continue } const mapped = friendlyError(err); item.pending.delete(index); item.failed = true; item.error = mapped.message; item.status = mapped.message; error.value = `${item.file.name}: ${mapped.message}`; persistTransfers() } finally { item.controllers.delete(index) } } }
function updateChunkProgress(item) { item.progress = item.chunksTotal ? Math.round(25 + item.uploaded.length / item.chunksTotal * 75) : 25; syncLoadedBytes(item); item.status = t('files.uploading') }
// syncLoadedBytes 让 loadedBytes 与进度一致，供整体速率统计采样。
// syncLoadedBytes keeps loadedBytes in step with progress for overall-rate sampling.
function syncLoadedBytes(item) { item.loadedBytes = item.file?.size ? Math.round(item.file.size * (item.progress || 0) / 100) : 0 }

// uploadChunkWithRetry uploads one binary chunk with an abortable fetch and exponential backoff.
// uploadChunkWithRetry 使用可中止 fetch 上传单个二进制分片，并以指数退避重试。
async function uploadChunkWithRetry(item, index) { const start = index * item.chunkSize; const end = Math.min(item.file.size, start + item.chunkSize); for (let attempt = 0; attempt < 4; attempt++) { if (item.paused) throw new DOMException('paused', 'AbortError'); const controller = new AbortController(); item.controllers.set(index, controller); try { const response = await fetch(`/api/files/${item.taskId}/chunks/${index}`, { method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('filebox_token')}` }, body: item.file.slice(start, end), signal: controller.signal }); if (!response.ok) { let body = null; try { body = await response.json() } catch {} throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { status: response.status }) } return } catch (err) { if (err.name === 'AbortError' || item.paused) throw err; if (attempt === 3) throw err; await new Promise(resolve => setTimeout(resolve, 300 * 2 ** attempt)) } finally { item.controllers.delete(index) } } }
function waitForChunks(item) { return new Promise(resolve => { const check = () => { if (item.paused || item.failed || item.pending.size === 0) resolve(); else setTimeout(check, 80) }; check() }) }

// startUpload computes the checksum, performs instant-upload lookup, resumes missing chunks, and completes the task.
// startUpload 负责计算校验值、秒传检查、补传缺片并提交完成请求。
async function startUpload(item) { if (item.running) return item.running; item.running = true; item.paused = false; item.failed = false; item.canContinue = false; item.running = (async () => { let init = null; try { if (item.taskId) { try { await finishExistingUpload(item); return } catch (err) { if (err.status !== 404) throw err; item.taskId = ''; item.uploaded = []; item.pending.clear(); item.canContinue = false } } if (!item.sha256) { item.status = t('files.checksum', { progress: 0 }); item.sha256 = await computeFileSHA256(item.file, progress => { if (!item.paused) { item.progress = Math.round(progress * 0.25); syncLoadedBytes(item); item.status = t('files.checksum', { progress }) } }) } if (item.paused) return; if (item.file.size > 0) { const check = await api('/api/files/check', { method: 'POST', body: JSON.stringify({ sha256: item.sha256, size: item.file.size, name: item.file.name, ...(item.dir ? { dir: item.dir } : {}) }) }); if (check.data?.instant) { item.progress = 100; syncLoadedBytes(item); item.status = t('files.instantUpload'); notice.value = t('files.instantUpload'); await loadFiles(); await loadMe(); removeUploadLater(item); return } if (check.data?.conflict) { const resolve = await askConflict(check.data.existing); if (resolve === 'cancel') throw new Error(t('files.uploadCancelled')); init = await requestUploadInit(item, resolve); item.taskId = init.data.taskId; item.chunkSize = init.data.chunkSize; item.chunksTotal = init.data.totalChunks; item.uploaded = [...(init.data.uploadedChunks || [])]; item.status = t('files.uploading'); await continueChunks(item); if (item.paused || item.failed) return; await finishExistingUpload(item); return } } try { init = await requestUploadInit(item) } catch (err) { if (err.status !== 409 || !err.data?.conflict) throw err; const resolve = await askConflict(err.data.existing); if (resolve === 'cancel') throw new Error(t('files.uploadCancelled')); init = await requestUploadInit(item, resolve) } item.taskId = init.data.taskId; item.chunkSize = init.data.chunkSize; item.chunksTotal = init.data.totalChunks; item.uploaded = [...(init.data.uploadedChunks || [])]; item.status = t('files.uploading'); await continueChunks(item); if (item.paused || item.failed) return; await finishExistingUpload(item) } catch (err) { if (item.paused) return; const mapped = friendlyError(err); item.failed = true; item.canContinue = Boolean(item.taskId); item.error = mapped.message; item.status = mapped.message; error.value = `${item.file.name}: ${mapped.message}` } finally { item.running = false } })(); return item.running }
async function finishExistingUpload(item) { if (item.paused) return; await continueChunks(item); if (item.paused || item.failed) return; item.status = t('files.checking'); item.progress = 99; syncLoadedBytes(item); const completeBody = { sha256: item.sha256 }; if (item.resolve) completeBody.action = item.resolve; await api(`/api/files/${item.taskId}/complete`, { method: 'POST', body: JSON.stringify(completeBody) }); item.progress = 100; syncLoadedBytes(item); item.status = t('files.completed'); notice.value = t('files.uploadComplete', { name: item.relPath || item.file.name }); await loadMe(); await loadFiles(); persistTransfers(); removeUploadLater(item) }
async function continueChunks(item) { const body = await api(`/api/files/${item.taskId}/status`); item.chunkSize = body.data.chunkSize; item.chunksTotal = body.data.totalChunks; item.uploaded = [...(body.data.uploadedChunks || [])].sort((a, b) => a - b); const missing = Array.from({ length: item.chunksTotal }, (_, index) => index).filter(index => !item.uploaded.includes(index)); item.pending.clear(); removeQueued(item); updateChunkProgress(item); if (!missing.length) return; enqueueChunks(item, missing); await waitForChunks(item) }
function requestUploadInit(item, resolve = '') { item.resolve = resolve; return api('/api/files/upload-init', { method: 'POST', body: JSON.stringify({ name: item.file.name, size: item.file.size, chunkSize: item.file.size <= 8 * 1024 * 1024 ? item.file.size : 4194304, mime: item.file.type, sha256: item.sha256, ...(item.dir ? { dir: item.dir } : {}), ...(resolve ? { resolve } : {}) }) }) }
function pauseUpload(item) { item.paused = true; item.status = t('files.paused'); removeQueued(item); item.controllers.forEach(controller => controller.abort()); wakeWorkers(); persistTransfers() }
async function resumeUpload(item) { if (item.running) return; item.paused = false; item.failed = false; item.canContinue = false; item.status = t('files.uploading'); await runGated(item); persistTransfers() }
async function retryUpload(item) { item.failed = false; item.error = ''; item.paused = false; await runGated(item); persistTransfers() }
// removeUploadLater 仅对成功项做短时保留后移除；失败/取消项保留在面板中，
// 由用户重试或点击关闭（dismissUpload）处理，避免"2.6 秒后消失、无提示"。
// removeUploadLater auto-removes only successful items; failed/cancelled ones stay in
// the drawer for the user to retry or dismiss, so failures are never silently dropped.
function removeUploadLater(item) { if (item.failed) return; setTimeout(() => { uploads.value = uploads.value.filter(entry => entry !== item) }, 2200) }
function dismissUpload(item) { uploads.value = uploads.value.filter(entry => entry !== item); persistTransfers() }
// askConflict 将冲突请求加入队列并返回 Promise；队列保证每个请求最终都被 resolve
// （用户选择或 60s 超时按取消处理），避免并发同名文件相互覆盖导致协程永久挂死。
// askConflict enqueues a conflict prompt and resolves every caller eventually — via the
// user's choice or a 60s timeout treated as cancel — so concurrent same-name conflicts
// can no longer overwrite each other and leave uploads stuck in "preparing".
function askConflict(existing) {
  return new Promise(resolve => {
    const entry = { existing, resolve }
    entry.timer = setTimeout(() => {
      const position = conflictQueue.value.indexOf(entry)
      if (position >= 0) conflictQueue.value.splice(position, 1)
      resolve('cancel')
    }, 60000)
    conflictQueue.value.push(entry)
  })
}
const activeConflict = computed(() => conflictQueue.value[0] || null)
function chooseConflict(value) {
  const entry = conflictQueue.value.shift()
  if (!entry) return
  clearTimeout(entry.timer)
  entry.resolve(value)
}
function canPreview(mime = '') { const value = mime.toLowerCase().split(';')[0]; return new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'text/plain', 'text/markdown', 'text/csv', 'text/x-log', 'application/json', 'application/pdf', 'video/mp4', 'video/webm']).has(value) }
// fileIcon 按 MIME/扩展名映射常见文件类型图标，未知类型回退默认文件图标。
// fileIcon maps common MIME/extensions to per-type icons and falls back to the default file icon.
function fileIcon(mime = '', name = '') {
  const value = mime.toLowerCase().split(';')[0]
  const ext = (name.split('.').pop() || '').toLowerCase()
  if (value.startsWith('image/') || ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext)) return Image
  if (value.startsWith('video/') || ['mp4', 'webm', 'mkv', 'avi', 'mov', 'flv'].includes(ext)) return Video
  if (value.startsWith('audio/') || ['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac'].includes(ext)) return Music
  if (value === 'application/pdf' || ext === 'pdf') return FileText
  if (value === 'application/json' || ['json', 'jsonl'].includes(ext)) return FileJson
  if (['application/zip', 'application/x-7z-compressed', 'application/x-rar-compressed', 'application/x-tar', 'application/gzip', 'application/x-bzip2', 'application/x-xz'].includes(value) || ['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz'].includes(ext)) return FileArchive
  // 代码与表格类优先于通用 text 分支，避免被 text/ 提前命中。
  // Code and spreadsheet extensions are checked before the generic text branch.
  if (['application/javascript', 'text/javascript', 'application/x-sh', 'application/xml', 'text/css', 'application/sql', 'application/x-yaml'].includes(value) || ['js', 'ts', 'jsx', 'tsx', 'vue', 'css', 'scss', 'go', 'rs', 'py', 'java', 'c', 'h', 'cpp', 'sh', 'bat', 'ps1', 'sql', 'html', 'htm', 'xml', 'yaml', 'yml', 'toml', 'ini', 'conf'].includes(ext)) return FileCode
  if (['application/vnd.ms-excel', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'].includes(value) || ['xls', 'xlsx', 'tsv'].includes(ext) || value === 'text/csv' || ['csv'].includes(ext)) return FileSpreadsheet
  if (value.startsWith('text/') || ['txt', 'md', 'log', 'csv'].includes(ext)) return FileText
  if (['doc', 'docx', 'rtf', 'odt'].includes(ext)) return FileText
  if (['ppt', 'pptx', 'odp'].includes(ext)) return FileText
  if (['exe', 'msi', 'dmg', 'appimage', 'deb', 'rpm'].includes(ext)) return FileType
  return File
}
function previewType(mime = '') { const value = mime.toLowerCase().split(';')[0]; if (value.startsWith('image/')) return 'image'; if (value.startsWith('video/')) return 'video'; if (value === 'application/pdf') return 'pdf'; return 'text' }
async function openPreview(file) { previewFile.value = file; previewLoading.value = true; previewError.value = ''; previewText.value = ''; previewUrl.value = ''; previewKind.value = previewType(file.mime); try { const response = await fetch(`/api/files/${file.id}/preview`, { headers: { Authorization: `Bearer ${localStorage.getItem('filebox_token')}` } }); if (!response.ok) { let body = null; try { body = await response.json() } catch {} throw new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })) } if (previewKind.value === 'text') previewText.value = await response.text(); else { const blob = await response.blob(); previewUrl.value = URL.createObjectURL(blob) } } catch (err) { previewError.value = err.message } finally { previewLoading.value = false } }
function closePreview() { if (previewUrl.value) URL.revokeObjectURL(previewUrl.value); previewFile.value = null; previewUrl.value = '' }
function openShare(file) { if (readOnly.value) return; shareFile.value = file; shareForm.value = { expiresInHours: 24, maxDownloads: 0 }; shareResult.value = null; shareError.value = ''; shareNotice.value = '' }
function closeShare() { shareFile.value = null }
async function createShare() { if (readOnly.value) { shareError.value = t('readOnly.error'); return } shareLoading.value = true; shareError.value = ''; try { const body = await api(`/api/files/${shareFile.value.id}/share`, { method: 'POST', body: JSON.stringify(shareForm.value) }); shareResult.value = body.data; sharedIds.add(shareFile.value.id); localStorage.setItem('filebox_shared_ids', JSON.stringify([...sharedIds])) } catch (err) { shareError.value = err.message } finally { shareLoading.value = false } }
const shareAbsoluteUrl = computed(() => shareResult.value ? new URL(shareResult.value.url, window.location.origin).href : '')
async function copyShare() { try { await navigator.clipboard.writeText(shareAbsoluteUrl.value) } catch { const input = document.querySelector('.share-url input'); input?.select(); document.execCommand('copy') } shareNotice.value = t('files.shareCopied') }
function openSharePage() { window.open(shareAbsoluteUrl.value, '_blank', 'noopener') }
async function revokeShare() { if (readOnly.value) { shareError.value = t('readOnly.error'); return } try { await api(`/api/files/${shareFile.value.id}/shares`, { method: 'DELETE' }); sharedIds.delete(shareFile.value.id); localStorage.setItem('filebox_shared_ids', JSON.stringify([...sharedIds])); shareResult.value = null; shareNotice.value = t('files.revokeShares') } catch (err) { shareError.value = err.message } }
function isShared(file) { return sharedIds.has(file.id) }
// streamDownload reads each response chunk so both file and ZIP downloads expose byte-level progress.
// streamDownload 逐块读取响应，让单文件和 ZIP 下载都能展示字节级进度。
async function streamDownload(item, request, downloadName) {
  const response = await request()
  if (!response.ok) {
    let body = null
    try { body = await response.json() } catch {}
    throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { status: response.status })
  }
  if (!response.body) throw new Error(t('error.downloadFailed'))
  const headerSize = Number(response.headers.get('Content-Length'))
  if (Number.isFinite(headerSize) && headerSize > 0) item.size = headerSize
  const reader = response.body.getReader()
  const parts = []
  const startedAt = performance.now()
  let received = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    parts.push(value)
    received += value.byteLength
    item.loadedBytes = received
    item.progress = item.size > 0 ? Math.min(100, Math.round(received / item.size * 100)) : -1
    const elapsed = (performance.now() - startedAt) / 1000
    item.rate = elapsed > 0 ? received / elapsed : 0
  }
  item.loadedBytes = received
  item.progress = 100
  item.rate = received / Math.max((performance.now() - startedAt) / 1000, 0.001)
  const blob = new Blob(parts, { type: response.headers.get('Content-Type') || 'application/octet-stream' })
  const link = document.createElement('a')
  const objectUrl = URL.createObjectURL(blob)
  link.href = objectUrl
  link.download = downloadName
  link.click()
  setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
  item.status = t('files.completed')
}

// download streams the file and reports detailed progress in the transfers drawer.
// download 流式下载并在传输面板显示详细进度。
async function download(file) {
  const controller = new AbortController()
  const item = { id: `dl-${Date.now()}-${file.id}`, name: file.name, size: file.size || 0, loadedBytes: 0, progress: 0, rate: 0, status: t('files.downloading'), failed: false, cancelled: false, running: true, error: '', controller }
  downloads.value.push(item); transfersOpen.value = true; persistTransfers()
  try {
    await streamDownload(item, () => fetch(`/api/files/${file.id}/download`, { headers: { Authorization: `Bearer ${localStorage.getItem('filebox_token')}` }, signal: controller.signal }), file.name)
  } catch (err) {
    if (err.name === 'AbortError' || item.cancelled) { item.status = t('download.detail.cancelled'); item.progress = item.progress < 0 ? 0 : item.progress } else { const mapped = friendlyError(err); item.failed = true; item.error = mapped.message; item.status = mapped.message; error.value = `${file.name}: ${mapped.message}` }
  } finally {
    item.running = false
  }
  setTimeout(() => { downloads.value = downloads.value.filter(entry => entry !== item) }, 4000)
}
async function remove(file) { if (readOnly.value) { error.value = t('readOnly.error'); return } if (!window.confirm(t('confirm.deleteFile', { name: file.name }))) return; try { await api(`/api/files/${file.id}`, { method: 'DELETE' }); selectedIds.delete(file.id); notice.value = t('notice.fileDeleted'); await loadMe(); await loadFiles() } catch (err) { error.value = err.message } }
// scrollToCollections 顶栏"我的收集"通过 /#collections 锚定到文件页收集区块（#12 最小方案）。
// scrollToCollections scrolls to the collection section when the topbar "My collections" link targets /#collections (#12 minimal).
function scrollToCollections() { if (route.hash === '#collections') { setTimeout(() => document.getElementById('collections')?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 250) } }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '-' }
function shortMime(value = '') { return value.split('/').pop()?.toUpperCase() || 'FILE' }
// progressController keeps the authenticated SSE fetch cancellable on navigation or logout.
// progressController 让带认证的 SSE fetch 可在离开页面或退出登录时取消。
let progressController = null
function applyProgress(tasks) {
  if (!Array.isArray(tasks)) return
  for (const task of tasks) {
    const item = uploads.value.find(entry => entry.taskId === task.taskId)
    if (!item) continue
    const ratio = task.totalChunks ? task.uploaded / task.totalChunks : 0
    if (ratio > 0) { item.progress = Math.max(item.progress, Math.round(25 + ratio * 75)); syncLoadedBytes(item) }
  }
}
function handleProgressEvent(block) {
  const lines = block.split(/\r?\n/)
  const eventName = lines.find(line => line.startsWith('event:'))?.slice(6).trim()
  const data = lines.filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n')
  if (!data) return
  try {
    const payload = JSON.parse(data)
    if (eventName === 'auth-error' || payload?.status === 401) {
      clearSession()
      if (router.currentRoute.value.path !== '/login') router.push({ path: '/login', query: { redirect: router.currentRoute.value.fullPath } })
      return
    }
    applyProgress(payload)
  } catch { /* ignore malformed events */ }
}
function connectProgressStream() {
  closeProgressStream()
  const token = localStorage.getItem('filebox_token')
  if (!token) return
  const controller = new AbortController()
  progressController = controller
  void (async () => {
    try {
      const response = await fetch('/api/files/progress/stream', { headers: { Authorization: `Bearer ${token}` }, signal: controller.signal })
      if (!response.ok) {
        if (response.status === 401) {
          clearSession()
          if (router.currentRoute.value.path !== '/login') router.push({ path: '/login', query: { redirect: router.currentRoute.value.fullPath } })
        } else if (response.status === 403) {
          let body = null
          try { body = await response.json() } catch { /* ignore non-JSON response */ }
          if (body?.data?.code === 'PASSWORD_CHANGE_REQUIRED') router.push('/change-password')
        }
        return
      }
      if (!response.body) return
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split(/\r?\n\r?\n/)
        buffer = blocks.pop() || ''
        blocks.forEach(handleProgressEvent)
      }
      buffer += decoder.decode()
      if (buffer.trim()) handleProgressEvent(buffer)
    } catch (err) {
      if (err.name !== 'AbortError') return
    } finally {
      if (progressController === controller) progressController = null
    }
  })()
}
function closeProgressStream() { progressController?.abort(); progressController = null }
onMounted(() => { restoreTransfers(); loadMe(); loadFiles(); loadFolders(); loadCollections(); rateTimer = setInterval(sampleOverallRate, 1000); connectProgressStream(); window.addEventListener('beforeunload', persistTransfers); scrollToCollections(); watch(() => route.hash, scrollToCollections) })
onBeforeUnmount(() => { if (rateTimer) clearInterval(rateTimer); window.removeEventListener('beforeunload', persistTransfers); closeProgressStream(); cancelBatchDownload(); downloads.value.filter(item => item.running).forEach(cancelDownload); uploads.value.forEach(item => item.controllers.forEach(controller => controller.abort())); if (previewUrl.value) URL.revokeObjectURL(previewUrl.value); persistTransfers() })
</script>
