<template>
  <main class="app-shell" :class="{ 'read-only': readOnly }">
    <AuthenticatedTopbar :user="user" section="files">
      <template #actions>
        <button class="icon-text-button transfer-button" :title="t('files.transfers')" @click="transfersOpen = !transfersOpen"><ArrowUpDown :size="16" /> {{ t('files.transfers') }}<span v-if="activeTransferCount" class="transfer-badge">{{ activeTransferCount }}</span></button>
      </template>
    </AuthenticatedTopbar>
    <section class="content-wrap">
      <div class="page-heading"><div><h1>{{ t('files.heading') }}</h1><p class="muted">{{ t('files.copy') }}</p></div><div class="quota-block"><div class="quota-label"><span>{{ t('files.quota') }}</span><strong>{{ formatBytes(user.usedBytes) }}</strong> <span class="quota-total">/ {{ formatBytes(user.quotaBytes) }}</span></div><div class="progress-track"><span :style="{ width: quotaPercent + '%' }"></span></div></div></div>
      <div v-if="readOnly" class="alert read-only-notice">{{ t('readOnly.notice') }}</div>
      <div class="dir-bar"><div class="breadcrumb"><button class="breadcrumb-link" :class="{ active: !currentDir }" @click="navigateDir('')">{{ t('files.root') }}</button><template v-for="(seg, i) in breadcrumbs" :key="seg"><span class="breadcrumb-sep">/</span><button class="breadcrumb-link" :class="{ active: i === breadcrumbs.length - 1 }" @click="navigateDir(breadcrumbPath(i))">{{ seg }}</button></template></div></div>
      <div class="toolbar"><div class="search-box"><Search :size="17" /><input v-model="searchInput" :placeholder="t('files.searchPlaceholder')" @keyup.enter="search" /><button v-if="searchInput" :title="t('files.clearSearch')" @click="searchInput = ''; search()"><X :size="15" /></button></div><div v-if="!readOnly" class="upload-zone"><button class="secondary-button" @click="fileInput?.click()"><Upload :size="16" /> {{ t('files.choose') }}</button><button class="secondary-button" @click="pickFolder()"><FolderUp :size="16" /> {{ t('files.uploadFolder') }}</button><button class="secondary-button" @click="openNewFolder"><FolderPlus :size="16" /> {{ t('files.newFolder') }}</button><input ref="fileInput" type="file" multiple hidden @change="handleInput" /><input ref="folderInput" type="file" webkitdirectory directory multiple hidden @change="handleFolderInput" /></div><button v-if="selectedFolderIds.size && !readOnly" class="secondary-button" :disabled="folderSaving" @click="openBatchRename"><Pencil :size="16" /> {{ t('files.batchRenameFolders', { count: selectedFolderIds.size }) }}</button><button v-if="selectedIds.size" class="secondary-button batch-download-button" :disabled="batchDownloading || batchSharing || batchDeleting" @click="batchDownload"><Archive :size="16" /> {{ t('files.batchDownload', { count: selectedIds.size }) }}</button><button v-if="(selectedIds.size || selectedFolderIds.size) && !readOnly" class="secondary-button batch-share-button" :disabled="batchDownloading || batchSharing || batchDeleting" @click="openBatchShare"><Share2 :size="16" /> {{ t('files.batchShare', { count: selectedIds.size }) }}</button><button v-if="(selectedIds.size || selectedFolderIds.size) && !readOnly" class="secondary-button batch-delete-button" :disabled="batchDownloading || batchSharing || batchDeleting" @click="batchDelete"><Trash2 :size="16" /> {{ t('files.batchDelete', { count: selectedIds.size + selectedFolderIds.size }) }}</button><button v-if="!readOnly" class="secondary-button batch-delete-button" :title="t('files.clearAll')" @click="openClearAll"><Trash2 :size="16" /> {{ t('files.clearAll') }}</button><button v-if="batchDownloading" class="icon-button" :title="t('common.cancel')" @click="cancelBatchDownload"><X :size="16" /></button><label class="check-label md5-toggle"><input v-model="showMd5" type="checkbox" @change="persistMd5" /> {{ t('files.showMd5') }}</label><button class="refresh-button" :title="t('files.refresh')" @click="loadFiles"><RefreshCw :size="17" :class="{ spin: loading }" /></button><span class="result-count">{{ t('common.files', { count: total }) }}</span></div>
      <div v-if="notice" class="alert success">{{ notice }}</div><div v-if="error" class="alert error">{{ error }}</div>
      <div class="file-table-wrap"><table class="file-table"><thead><tr><th class="select-col"><input type="checkbox" :checked="allSelected" :aria-label="t('files.selectAll')" @change="toggleSelectAll" /></th><th class="sortable-col" tabindex="0" :aria-sort="sortAria('name')" :title="t('files.sortColumn', { column: t('files.name') })" @click="toggleSort('name')" @keydown.enter.prevent="toggleSort('name')" @keydown.space.prevent="toggleSort('name')"><span>{{ t('files.name') }}</span><span v-if="sortBy === 'name'" class="sort-indicator">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th><th class="sortable-col" tabindex="0" :aria-sort="sortAria('size')" :title="t('files.sortColumn', { column: t('files.size') })" @click="toggleSort('size')" @keydown.enter.prevent="toggleSort('size')" @keydown.space.prevent="toggleSort('size')"><span>{{ t('files.size') }}</span><span v-if="sortBy === 'size'" class="sort-indicator">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th><th class="sortable-col" tabindex="0" :aria-sort="sortAria('type')" :title="t('files.sortColumn', { column: t('files.type') })" @click="toggleSort('type')" @keydown.enter.prevent="toggleSort('type')" @keydown.space.prevent="toggleSort('type')"><span>{{ t('files.type') }}</span><span v-if="sortBy === 'type'" class="sort-indicator">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th><th>{{ t('files.integrity') }}</th><th class="sortable-col" tabindex="0" :aria-sort="sortAria('updatedAt')" :title="t('files.sortColumn', { column: t('files.uploadedAt') })" @click="toggleSort('updatedAt')" @keydown.enter.prevent="toggleSort('updatedAt')" @keydown.space.prevent="toggleSort('updatedAt')"><span>{{ t('files.uploadedAt') }}</span><span v-if="sortBy === 'updatedAt'" class="sort-indicator">{{ sortOrder === 'asc' ? '▲' : '▼' }}</span></th><th></th></tr></thead><tbody><template v-for="folder in childFolders" :key="'d' + folder.id"><tr class="folder-table-row" :class="{ 'row-selected': selectedFolderIds.has(folder.id) }"><td class="select-col"><input type="checkbox" :checked="selectedFolderIds.has(folder.id)" :aria-label="t('files.selectFolder', { name: folder.name })" @change="toggleFolderSelect(folder.id)" /></td><td><button type="button" class="folder-table-entry" :title="folder.path" @click="navigateDir(folder.path)"><span class="file-icon"><Folder :size="17" /></span><strong>{{ folder.name }}</strong></button></td><td>-</td><td><span class="mime-label folder-mime">{{ t('files.folder') }}</span></td><td>-</td><td>-</td><td><div v-if="!readOnly" class="row-actions"><button class="icon-button" :title="t('files.renameFolder')" @click="openRenameFolder(folder)"><Pencil :size="15" /></button><button class="icon-button danger-icon" :title="t('files.deleteFolder')" @click="removeFolder(folder)"><Trash2 :size="15" /></button></div></td></tr></template><tr v-for="file in files" :key="file.id" :class="{ 'row-selected': selectedIds.has(file.id) }"><td class="select-col"><input type="checkbox" :checked="selectedIds.has(file.id)" :aria-label="t('files.selectFile', { name: file.name })" @change="toggleSelect(file.id)" /></td><td><div class="file-title"><span class="file-icon"><component :is="fileIcon(file.mime, file.name)" :size="17" /></span><strong>{{ file.name }}</strong><span v-if="isShared(file)" class="shared-mark" :title="t('files.shared')"><Share2 :size="14" /></span></div></td><td>{{ formatBytes(file.size) }}</td><td><span class="mime-label" :class="{ 'preview-mime': canPreview(file.mime) }">{{ shortMime(file.mime) }}</span></td><td><code v-if="showMd5" class="md5-cell" :title="`MD5 ${file.md5}\nSHA-256 ${file.sha256}`">{{ file.md5 }}</code><span v-else class="hash-label" :title="`MD5 ${file.md5}\nSHA-256 ${file.sha256}`"><CheckCircle2 :size="15" /> {{ t('files.hashes') }}</span></td><td>{{ formatDate(file.createdAt) }}</td><td><div class="row-actions"><button v-if="canPreview(file.mime)" class="icon-button" :title="t('files.preview')" @click="openPreview(file)"><Eye :size="17" /></button><button v-if="!readOnly" class="icon-button" :title="t('files.share')" @click="openShare(file)"><Share2 :size="17" /></button><button class="icon-button" :title="t('files.download')" @click="download(file)"><Download :size="17" /></button><button v-if="!readOnly" class="icon-button danger-icon" :title="t('files.delete')" @click="remove(file)"><Trash2 :size="17" /></button></div></td></tr></tbody></table><div v-if="!loading && !files.length" class="empty-state"><FolderOpen :size="34" /><strong>{{ keyword ? t('files.noMatch') : t('files.noFiles') }}</strong><span>{{ keyword ? t('files.noMatchCopy') : t('files.noFilesCopy') }}</span></div><div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('files.loading') }}</span></div></div>
      <div v-if="total > pageSize" class="pagination"><button class="secondary-button" :disabled="page === 1" @click="page--; loadFiles()"><ChevronLeft :size="16" /> {{ t('common.previous') }}</button><span>{{ t('common.page', { page }) }} / {{ totalPages }}</span><button class="secondary-button" :disabled="page * pageSize >= total" @click="page++; loadFiles()">{{ t('common.next') }} <ChevronRight :size="16" /></button><label class="page-size-label">{{ t('common.pageSize') }}<select v-model.number="pageSize" @change="changePageSize"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label><template v-if="totalPages > 7"><input v-model="pageInput" class="jump-input" type="number" min="1" :max="totalPages" :placeholder="t('common.pageInput')" @keyup.enter="jumpPage" /><button class="secondary-button" @click="jumpPage">{{ t('common.jump') }}</button></template></div><BrandFooter />
    </section>
    <div v-if="activeConflict" class="modal-backdrop" @click.self="chooseConflict('cancel')"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ t('files.conflictHeading') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="chooseConflict('cancel')"><X :size="18" /></button></div><p class="modal-copy">{{ t('files.conflictCopy', { name: activeConflict.existing?.name }) }}</p><div class="conflict-details">{{ formatBytes(activeConflict.existing?.size || 0) }} · {{ formatDate(activeConflict.existing?.createdAt) }}</div><div v-if="conflictQueue.length > 1" class="conflict-queue-hint">{{ t('files.conflictQueue', { count: conflictQueue.length }) }}</div><div class="modal-actions"><button class="secondary-button" @click="chooseConflict('rename')"><FileEdit :size="16" /> {{ t('files.rename') }}<span>{{ t('files.renameHint') }}</span></button><button class="primary-button" @click="chooseConflict('overwrite')"><Replace :size="16" /> {{ t('files.overwrite') }}<span>{{ t('files.overwriteHint') }}</span></button></div></section></div>
    <div v-if="activeConfirm" class="modal-backdrop" @click.self="chooseConfirm(false)"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ t('common.confirm') }}</h2></div><button class="icon-button" :title="t('common.close')" @click="chooseConfirm(false)"><X :size="18" /></button></div><p class="modal-copy">{{ activeConfirm.message }}</p><div class="modal-actions"><button class="secondary-button" @click="chooseConfirm(false)">{{ t('common.cancel') }}</button><button class="primary-button" @click="chooseConfirm(true)">{{ t('common.confirm') }}</button></div></section></div>
    <div v-if="folderUploadPrompt" class="modal-backdrop" @click.self="cancelFolderUpload"><section class="modal-panel folder-upload-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div class="folder-upload-heading"><span class="folder-upload-icon"><FolderUp :size="20" /></span><div><h2>{{ t('files.folderUploadTitle') }}</h2></div></div><button class="icon-button" :title="t('common.close')" @click="cancelFolderUpload"><X :size="18" /></button></div><div class="folder-upload-summary"><div class="folder-upload-name"><Folder :size="19" /><strong :title="folderUploadPrompt.folderName">{{ folderUploadPrompt.folderName }}</strong></div><dl class="folder-upload-details"><div><dt>{{ t('files.uploadFolderConfirmCount', { count: folderUploadPrompt.count }) }}</dt><dd>{{ t('files.uploadFolderConfirmSize', { size: formatBytes(folderUploadPrompt.totalSize) }) }}</dd></div><div><dt>{{ t('files.uploadFolderTarget', { dir: folderUploadPrompt.targetDir || t('files.root') }) }}</dt></div></dl></div><div class="modal-actions"><button class="secondary-button" @click="cancelFolderUpload">{{ t('common.cancel') }}</button><button class="primary-button" @click="confirmFolderUpload"><Upload :size="16" /> {{ t('common.confirm') }}</button></div></section></div>
    <div v-if="shareFile" class="modal-backdrop" @click.self="closeShare"><section class="modal-panel share-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ shareFile.name }}</h2></div><button class="icon-button" :title="t('common.close')" @click="closeShare"><X :size="18" /></button></div><form v-if="!shareResult" @submit.prevent="createShare"><label class="form-label">{{ t('files.shareExpiresHours') }}<input v-model.number="shareForm.expiresInHours" type="number" min="1" required /></label><label class="form-label">{{ t('files.shareMaxDownloads') }}<input v-model.number="shareForm.maxDownloads" type="number" min="0" max="100000" required /></label><p v-if="shareError" class="alert error">{{ shareError }}</p><button class="primary-button submit-button" :disabled="shareLoading"><span>{{ shareLoading ? t('common.loading') : t('files.share') }}</span><Share2 :size="17" /></button></form><div v-else class="share-result"><label class="form-label">{{ t('files.shareUrl') }}<div class="share-url"><input :value="shareAbsoluteUrl" readonly /><button type="button" class="icon-button" :title="t('files.shareCopied')" @click="copyShare"><Copy :size="16" /></button></div></label><p class="muted">{{ t('share.expiresAt') }} {{ formatDate(shareResult.expiresAt) }}<span v-if="shareResult.maxDownloads"> · {{ t('share.availableDownloads', { count: shareResult.maxDownloads - shareResult.downloadCount }) }}</span></p><div class="modal-actions"><button class="secondary-button" @click="openSharePage"><ExternalLink :size="16" /> {{ t('files.openShare') }}</button><button class="secondary-button danger-action" @click="revokeShare"><Trash2 :size="16" /> {{ t('files.revokeShares') }}</button></div><p v-if="shareNotice" class="alert success">{{ shareNotice }}</p></div></section></div>
    <div v-if="previewFile" class="modal-backdrop" @click.self="closePreview"><section class="modal-panel preview-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ previewFile.name }}</h2></div><button class="icon-button" :title="t('common.close')" @click="closePreview"><X :size="18" /></button></div><div v-if="previewLoading" class="empty-state preview-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('files.previewLoading') }}</span></div><p v-else-if="previewError" class="alert error">{{ previewError }}</p><img v-else-if="previewKind === 'image'" class="preview-content preview-image" :src="previewUrl" :alt="previewFile.name" /><video v-else-if="previewKind === 'video'" class="preview-content" :src="previewUrl" controls></video><iframe v-else-if="previewKind === 'pdf'" class="preview-content preview-frame" :src="previewUrl" :title="previewFile.name"></iframe><pre v-else class="preview-text">{{ previewText }}</pre></section></div>
    <div v-if="batchShareOpen" class="modal-backdrop" @click.self="closeBatchShare"><section class="modal-panel batch-share-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ t('files.batchShareTitle', { count: batchShareCount }) }}</h2></div><button class="icon-button" :title="t('common.close')" @click="closeBatchShare"><X :size="18" /></button></div><form v-if="!batchShareResults.length" @submit.prevent="createBatchShare"><label class="form-label">{{ t('files.shareExpiresHours') }}<input v-model.number="batchShareForm.expiresInHours" type="number" min="1" required /></label><label class="form-label">{{ t('files.shareMaxDownloads') }}<input v-model.number="batchShareForm.maxDownloads" type="number" min="0" max="100000" required /></label><p v-if="batchShareError" class="alert error">{{ batchShareError }}</p><button class="primary-button submit-button" :disabled="batchSharing"><span>{{ batchSharing ? t('common.loading') : t('files.batchShareCreate') }}</span><Share2 :size="17" /></button></form><div v-else class="batch-share-results"><p class="muted">{{ t('files.batchShareUnifiedCreated', { count: batchShareResults.length }) }}</p><label class="form-label">{{ t('files.shareUrl') }}<div class="share-url"><input :value="batchShareGroupUrl" readonly /><button type="button" class="icon-button" :title="t('files.shareCopied')" @click="copyBatchShareGroupUrl"><Copy :size="16" /></button></div></label><div v-for="item in batchShareResults" :key="item.fileId" class="batch-share-result"><strong :title="item.fileName">{{ item.fileName }}</strong></div><p v-if="batchShareNotice" class="alert success">{{ batchShareNotice }}</p><div class="modal-actions"><button class="primary-button" @click="closeBatchShare">{{ t('common.close') }}</button></div></div></section></div>
    <div v-if="clearAllOpen" class="modal-backdrop" @click.self="!clearAllBusy && (clearAllOpen = false)"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ t('files.clearAll') }}</h2></div><button class="icon-button" :title="t('common.close')" :disabled="clearAllBusy" @click="clearAllOpen = false"><X :size="18" /></button></div><form @submit.prevent="submitClearAll"><p class="muted">{{ t('files.clearScopeHint', { count: clearOwnFileCount }) }}</p><p class="muted">{{ t('files.clearIrreversible') }}</p><label v-if="!user.totpEnabled" class="form-label">{{ t('files.clearPassword') }}<input v-model="clearAllAuth.password" type="password" required autofocus /></label><label v-else class="form-label">{{ t('files.clearCode') }}<input v-model="clearAllAuth.code" type="text" inputmode="numeric" maxlength="6" minlength="6" pattern="[0-9]{6}" required autofocus /></label><p v-if="clearAllError" class="alert error">{{ clearAllError }}</p><div class="modal-actions"><button class="secondary-button danger-action" type="submit" :disabled="clearAllBusy">{{ t('files.clearAllConfirm') }}</button><button class="secondary-button" type="button" :disabled="clearAllBusy" @click="clearAllOpen = false">{{ t('common.cancel') }}</button></div></form></section></div>
    <div v-if="folderPrompt" class="modal-backdrop" @click.self="folderPrompt = null"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ folderPrompt.batch ? t('files.batchRenameFolders', { count: batchRenameTotal }) : (folderPrompt.rename ? t('files.renameFolder') : t('files.newFolder')) }}</h2></div><button class="icon-button" :title="t('common.close')" @click="folderPrompt = null"><X :size="18" /></button></div><form @submit.prevent="submitFolder"><p v-if="folderPrompt.batch" class="muted">{{ t('files.batchRenameProgress', { done: batchRenameTotal - batchRenameQueue.length + 1, total: batchRenameTotal }) }}</p><label class="form-label">{{ t('files.folderName') }}<input v-model.trim="folderPrompt.name" maxlength="255" required autofocus /></label><p v-if="folderError" class="alert error">{{ folderError }}</p><div class="modal-actions"><button class="primary-button" :disabled="folderSaving"><Save :size="16" /> {{ t('common.save') }}</button><button type="button" class="secondary-button" @click="folderPrompt = null">{{ t('common.cancel') }}</button></div></form></section></div>
    <div v-if="transfersOpen" class="transfers-backdrop" @click="transfersOpen = false"></div>
    <aside class="transfers-drawer" :class="{ open: transfersOpen }" aria-label="transfers">
      <div class="transfers-header"><div><h2>{{ t('files.transfers') }}</h2></div><button class="icon-button" :title="t('files.clearList')" :disabled="!transferListCount" @click="clearTransferList"><Trash2 :size="16" /></button><button class="icon-button" :title="t('common.close')" @click="transfersOpen = false"><X :size="18" /></button></div>
      <div class="transfers-body">
        <div class="transfers-tabs" role="tablist">
          <button v-for="tab in stageTabs" :key="tab.key" class="transfers-tab" :class="{ active: transfersTab === tab.key }" role="tab" :aria-selected="transfersTab === tab.key" @click="transfersTab = tab.key">{{ t(tab.labelKey) }}<span class="transfers-count">{{ tab.count }}</span></button>
          <button class="transfers-tab" :class="{ active: transfersTab === 'done' }" role="tab" :aria-selected="transfersTab === 'done'" @click="transfersTab = 'done'">{{ t('files.transferDoneTab') }}<span class="transfers-count">{{ completedTransfers.length }}</span></button>
        </div>
        <div v-if="transfersTab !== 'done'" class="transfer-tab-panel" role="tabpanel">
          <div v-if="overallRate > 0" class="overall-rate"><Gauge :size="16" /><span>{{ t('files.overallRate') }}</span><strong>{{ formatRate(overallRate) }}</strong></div>
          <h3 class="transfers-section"><label class="transfer-section-select"><input type="checkbox" :checked="allUploadsSelected" :aria-label="t('files.selectAll')" @change="toggleAllUploads" /><span>{{ t('files.selectAll') }}</span></label><span class="transfers-section-name">{{ t('files.uploads') }}</span><span class="transfers-count">{{ tabUploads.length }}</span></h3>
          <div v-if="tabUploads.length" class="transfer-batch-toolbar"><span class="transfer-selection-count">{{ t('files.selectedTransfers', { count: selectedUploadIds.size }) }}</span><div class="transfer-batch-actions"><template v-if="selectedUploadIds.size"><button class="secondary-button" :disabled="uploadBatchBusy || !canPauseSelectedUploads" @click="pauseSelectedUploads"><Pause :size="14" /> {{ t('files.pauseSelected') }}</button><button class="secondary-button" :disabled="uploadBatchBusy || !selectedUploadIds.size || !canResumeSelectedUploads" @click="resumeSelectedUploads"><Play :size="14" /> {{ t('files.resumeSelected') }}</button><button class="secondary-button danger-action" :disabled="uploadBatchBusy || !selectedUploadIds.size || !canTerminateSelectedUploads" @click="terminateSelectedUploads"><X :size="14" /> {{ t('files.terminateSelected') }}</button></template><template v-else><button class="secondary-button" :disabled="uploadBatchBusy || !canPauseAllUploads" @click="pauseAllUploads"><Pause :size="14" /> {{ t('files.pauseAll') }}</button><button class="secondary-button" :disabled="uploadBatchBusy || !canResumeAllUploads" @click="resumeAllUploads"><Play :size="14" /> {{ t('files.resumeAll') }}</button><button class="secondary-button danger-action" :disabled="uploadBatchBusy || !canTerminateAllUploads" @click="terminateAllUploads"><X :size="14" /> {{ t('files.terminateAll') }}</button></template></div></div>
          <div v-if="!tabUploads.length" class="transfers-empty">{{ t('files.transferEmptyActive') }}</div>
          <div v-for="item in tabUploads" :key="item.id" class="transfer-row" :class="{ 'transfer-failed': item.failed, 'row-selected': selectedUploadIds.has(item.id) }" :title="transferRowTitle(item)"><input type="checkbox" class="transfer-select" :checked="selectedUploadIds.has(item.id)" :aria-label="item.relPath || item.file?.name || item.name" @change="toggleUploadSelect(item.id)" /><FileUp :size="16" class="transfer-icon" /><div class="transfer-main"><div class="transfer-name"><strong :title="item.relPath || item.file?.name || item.name">{{ item.relPath || item.file?.name || item.name }}</strong><span :class="{ 'transfer-error-text': item.failed }" :title="transferStateLabel(item)">{{ transferStateLabel(item) }}</span></div><div class="progress-track"><span :style="{ width: item.progress + '%' }"></span></div><div class="transfer-detail"><span>{{ t('download.detail.transferred', { loaded: formatBytes(item.loadedBytes || 0), total: item.size > 0 ? formatBytes(item.size) : formatBytes(item.file?.size || item.size || 0) }) }}</span><span>{{ item.progress }}%</span><span v-if="item.rate > 0">{{ t('download.detail.rate', { rate: formatRate(item.rate) }) }}</span></div></div><span class="transfer-percent">{{ item.failed ? '' : item.progress + '%' }}</span><button v-if="item.paused && !item.terminating" class="icon-button" :title="t('files.resume')" :disabled="!canResumeUpload(item)" @click="resumeUpload(item)"><Play :size="15" /></button><button v-else-if="item.running && !item.terminating" class="icon-button" :title="t('files.pause')" :disabled="!canPauseUpload(item)" @click="pauseUpload(item)"><Pause :size="15" /></button><button v-if="(item.canContinue || item.failed) && !item.terminating" class="icon-button" :title="t('files.retry')" :disabled="!item.file" @click="retryUpload(item)"><RefreshCw :size="15" /></button><button v-if="canTerminateUpload(item)" class="icon-button danger-icon" :title="t('files.terminate')" @click="terminateUploads([item])"><X :size="15" /></button><button v-if="item.failed" class="icon-button" :title="t('files.dismiss')" @click="dismissUpload(item)"><X :size="15" /></button></div>
          <h3 class="transfers-section"><label class="transfer-section-select"><input type="checkbox" :checked="allDownloadsSelected" :aria-label="t('files.selectAll')" @change="toggleAllDownloads" /><span>{{ t('files.selectAll') }}</span></label><span class="transfers-section-name">{{ t('files.downloads') }}</span><span class="transfers-count">{{ tabDownloads.length }}</span></h3>
          <div v-if="tabDownloads.length" class="transfer-batch-toolbar"><span class="transfer-selection-count">{{ t('files.selectedTransfers', { count: selectedDownloadIds.size }) }}</span><div class="transfer-batch-actions"><template v-if="selectedDownloadIds.size"><button class="secondary-button" :disabled="downloadBatchBusy || !canPauseSelectedDownloads" @click="pauseSelectedDownloads"><Pause :size="14" /> {{ t('files.pauseSelected') }}</button><button class="secondary-button" :disabled="downloadBatchBusy || !selectedDownloadIds.size || !canResumeSelectedDownloads" @click="resumeSelectedDownloads"><Play :size="14" /> {{ t('files.resumeSelected') }}</button><button class="secondary-button danger-action" :disabled="downloadBatchBusy || !selectedDownloadIds.size || !canTerminateSelectedDownloads" @click="terminateSelectedDownloads"><X :size="14" /> {{ t('files.terminateSelected') }}</button></template><template v-else><button class="secondary-button" :disabled="downloadBatchBusy || !canPauseAllDownloads" @click="pauseAllDownloads"><Pause :size="14" /> {{ t('files.pauseAll') }}</button><button class="secondary-button" :disabled="downloadBatchBusy || !canResumeAllDownloads" @click="resumeAllDownloads"><Play :size="14" /> {{ t('files.resumeAll') }}</button><button class="secondary-button danger-action" :disabled="downloadBatchBusy || !canTerminateAllDownloads" @click="terminateAllDownloads"><X :size="14" /> {{ t('files.terminateAll') }}</button></template></div></div>
          <div v-if="!tabDownloads.length" class="transfers-empty">{{ t('files.transferEmptyActive') }}</div>
          <div v-for="item in tabDownloads" :key="item.id" class="transfer-row" :class="{ 'transfer-failed': item.failed, 'row-selected': selectedDownloadIds.has(item.id) }" :title="transferRowTitle(item)"><input type="checkbox" class="transfer-select" :checked="selectedDownloadIds.has(item.id)" :aria-label="transferDisplayName(item)" @change="toggleDownloadSelect(item.id)" /><Download :size="16" class="transfer-icon" /><div class="transfer-main"><div class="transfer-name"><strong :title="transferDisplayName(item)">{{ transferDisplayName(item) }}</strong><span :class="{ 'transfer-error-text': item.failed }" :title="transferStateLabel(item)">{{ transferStateLabel(item) }}</span></div><div class="progress-track"><span :style="{ width: (item.progress < 0 ? 0 : item.progress) + '%' }"></span></div><div class="transfer-detail"><span>{{ t('download.detail.transferred', { loaded: formatBytes(item.loadedBytes), total: item.size > 0 ? formatBytes(item.size) : t('download.detail.unknown') }) }}</span><span>{{ item.progress < 0 ? t('download.detail.unknown') : item.progress + '%' }}</span><span>{{ t('download.detail.rate', { rate: formatRate(item.rate) }) }}</span></div></div><span class="transfer-percent">{{ item.progress < 0 ? '' : item.progress + '%' }}</span><button v-if="downloadCanResume(item)" class="icon-button" :title="t('files.resume')" @click="resumeDownload(item)"><Play :size="15" /></button><button v-else-if="downloadCanPause(item)" class="icon-button" :title="t('files.pause')" @click="pauseDownload(item)"><Pause :size="15" /></button><button v-if="downloadCanTerminate(item)" class="icon-button danger-icon" :title="t('common.cancel')" @click="terminateDownload(item)"><X :size="15" /></button></div>
        </div>
        <div v-else class="transfer-tab-panel" role="tabpanel">
          <h3 class="transfers-section"><span class="transfers-section-name">{{ t('files.transferDoneTab') }}</span><span class="transfers-count">{{ completedTransfers.length }}</span></h3>
          <div v-if="!completedTransfers.length" class="transfers-empty">{{ t('files.transferEmptyDone') }}</div>
          <template v-for="group in completedGroups" :key="group.id"><h4 class="transfers-stage"><span>{{ t(group.kindKey) }} · {{ t(group.labelKey) }}</span><span class="transfers-count">{{ group.items.length }}</span></h4><div v-for="item in group.items" :key="`${item.kind}-${item.id}`" class="transfer-row transfer-finished-row" :title="transferRowTitle(item)"><FileUp v-if="item.kind === 'upload'" :size="16" class="transfer-icon" /><Download v-else :size="16" class="transfer-icon" /><strong class="transfer-finished-name" :title="item.kind === 'upload' ? item.relPath || item.file?.name || item.name : transferDisplayName(item)">{{ item.kind === 'upload' ? item.relPath || item.file?.name || item.name : transferDisplayName(item) }}</strong><span class="transfer-finished-size">{{ formatBytes(item.kind === 'upload' ? item.file?.size || item.size || 0 : item.size || 0) }}</span><span class="transfer-finished-kind">{{ item.kind === 'upload' ? t('files.uploads') : t('files.downloads') }}</span><span class="transfer-finished-date">{{ formatDate(item.completedAt) }}</span><span class="finished-result" :class="item.kind === 'upload' ? uploadFinishedClass(item) : (item.cancelled ? 'finished-cancelled' : item.failed ? 'finished-failed' : 'finished-success')">{{ item.kind === 'upload' ? uploadFinishedLabel(item) : finishedResult(item, 'download') }}</span><button v-if="item.kind === 'upload' && canResumeUpload(item)" class="icon-button" :title="t('files.retry')" @click="retryUpload(item)"><RefreshCw :size="15" /></button><button v-if="item.kind === 'upload'" class="icon-button" :title="t('files.clearFinished')" @click="clearFinishedUpload(item)"><Trash2 :size="15" /></button><button v-else class="icon-button" :title="t('files.clearFinished')" @click="clearFinishedDownload(item)"><Trash2 :size="15" /></button></div></template>
        </div>
      </div>
    </aside>
  </main>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, batchDownloadFilename, clearSession, computeFileSHA256, localizeError } from '../api'
import { advanceTransferGeneration, completionTimestamp, emaRate, FairStartGate, isTransferGenerationCurrent, isUploadTerminal, needsBulkConfirm, transferBatches, uploadResultKind } from '../transferFlow'
import { TRANSFER_STAGE_KEYS, TRANSFER_STAGE_ORDER, canAdoptReselectedFile, groupByKindOutcome, isUploadTerminalState, normalizeStatus, progressHint, statusLabel, transferStage } from '../transferStatus'
import AuthenticatedTopbar from '../components/AuthenticatedTopbar.vue'
import BrandFooter from '../components/BrandFooter.vue'
import { brand } from '../brand'
import { currentLocale, t } from '../i18n'
import { lazyText } from '../notice'
import { Archive, ArrowUpDown, CheckCircle2, ChevronLeft, ChevronRight, Copy, Download, ExternalLink, Eye, File, FileArchive, FileCode, FileEdit, FileJson, FileSpreadsheet, FileText, FileType, FileUp, Folder, FolderOpen, FolderPlus, FolderUp, Gauge, Image, LoaderCircle, Music, Pause, Pencil, Play, RefreshCw, Replace, Save, Search, Share2, Trash2, Upload, UploadCloud, Video, X } from 'lucide-vue-next'

const router = useRouter(); const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}')); const files = ref([]); const total = ref(0); const page = ref(1); const pageSize = ref(Number(localStorage.getItem('filebox_pagesize_files')) || 20); const pageInput = ref(''); const keyword = ref(''); const searchInput = ref(''); const sortBy = ref('name'); const sortOrder = ref('asc'); const loading = ref(false); const error = ref(''); const notice = ref(''); const uploads = ref([]); const downloads = ref([]); const transfersOpen = ref(false); const transfersTab = ref('transferring'); const showMd5 = ref(localStorage.getItem('filebox_show_md5') !== '0'); const fileInput = ref(null); const folderInput = ref(null); const conflictQueue = ref([]); const confirmQueue = ref([]); const currentDir = ref(''); const folders = ref([]); const folderPrompt = ref(null); const folderUploadPrompt = ref(null); const folderSaving = ref(false); const folderError = ref(''); const clearAllOpen = ref(false); const clearAllBusy = ref(false); const clearAllError = ref(''); const clearAllAuth = ref({ password: '', code: '' }); const clearAllDiskMode = ref('empty-dirs')
let fileLoadVersion = 0
const shareFile = ref(null); const shareForm = ref({ expiresInHours: 24, maxDownloads: 0 }); const shareResult = ref(null); const shareLoading = ref(false); const shareError = ref(''); const shareNotice = ref(''); const previewFile = ref(null); const previewLoading = ref(false); const previewError = ref(''); const previewUrl = ref(''); const previewText = ref(''); const previewKind = ref(''); const sharedIds = new Set(JSON.parse(localStorage.getItem('filebox_shared_ids') || '[]')); const chunkQueue = []; let activeWorkers = 0; let workerWake = null
// 多选聚合下载状态：selectedIds 为当前选中文件集合，翻页/搜索时保留已选项。
// Batch-download selection: selectedIds holds the chosen file ids and survives page/search changes.
const selectedIds = reactive(new Set())
// 目录多选（v019 #7）：selectedFolderIds 记录勾选的目录 id，支持批量删除。
// Folder multi-select (v019 #7): selectedFolderIds tracks checked folder ids for batch deletion.
const selectedFolderIds = reactive(new Set())
const selectedUploadIds = reactive(new Set())
const selectedDownloadIds = reactive(new Set())
const uploadBatchBusy = ref(false)
const downloadBatchBusy = ref(false)
const batchRenameQueue = ref([])
const batchRenameTotal = ref(0)
function toggleFolderSelect(id) { if (selectedFolderIds.has(id)) selectedFolderIds.delete(id); else selectedFolderIds.add(id) }
// openBatchRename/renameNextSelectedFolder 批量重命名目录（v019 #7）：逐个复用重命名弹窗，全部完成后刷新。
// openBatchRename/renameNextSelectedFolder rename selected folders one by one (v019 #7), reusing the rename dialog.
function openBatchRename() {
  if (readOnly.value || !selectedFolderIds.size) return
  batchRenameQueue.value = [...selectedFolderIds]
  batchRenameTotal.value = batchRenameQueue.value.length
  renameNextSelectedFolder()
}
function renameNextSelectedFolder() {
  if (!batchRenameQueue.value.length) { batchRenameQueue.value = []; batchRenameTotal.value = 0; loadFolders(); loadFiles(); return }
  const id = batchRenameQueue.value[0]
  const folder = childFolders.value.find(f => f.id === id)
  if (!folder) { batchRenameQueue.value.shift(); renameNextSelectedFolder(); return }
  openRenameFolder(folder)
  folderPrompt.value.batch = true
}
const batchDownloading = ref(false)
const batchDeleting = ref(false)
let batchDownloadController = null
let batchDownloadItem = null
const allSelected = computed(() => files.value.length > 0 && files.value.every(file => selectedIds.has(file.id)))
const uploadsActive = computed(() => uploads.value.filter(item => !isUploadTerminalState(item)))
const uploadsDone = computed(() => uploads.value.filter(item => isUploadTerminalState(item)))
// v044.6（方案 B）：阶段升为独立页签，面板内只显示当前阶段，故不再需要面板内的阶段小标题。
// Stage tabs (option B): each panel shows exactly one stage, so the in-panel stage headings are gone.
const stageTabs = computed(() => {
  const active = [...uploadsActive.value, ...downloadsActive.value]
  return TRANSFER_STAGE_ORDER.map(stage => ({ key: stage, labelKey: TRANSFER_STAGE_KEYS[stage], count: active.filter(item => transferStage(item) === stage).length }))
})
// tabUploads/tabDownloads 是"当前页签内可见的条目"；分区里的"全选 / 全部"动作都以它为准，
// 这样按钮作用范围与用户看到的列表一致（而不是影响其它页签里的条目）。
const tabUploads = computed(() => transfersTab.value === 'done' ? [] : uploadsActive.value.filter(item => transferStage(item) === transfersTab.value))
const tabDownloads = computed(() => transfersTab.value === 'done' ? [] : downloadsActive.value.filter(item => transferStage(item) === transfersTab.value))
const downloadsActive = computed(() => downloads.value.filter(item => !isDownloadComplete(item)))
const downloadsDone = computed(() => downloads.value.filter(isDownloadComplete))
const completedGroups = computed(() => groupByKindOutcome(completedTransfers.value))
const completedTransfers = computed(() => [...uploadsDone.value.map(item => ({ ...item, kind: 'upload' })), ...downloadsDone.value.map(item => ({ ...item, kind: 'download' }))].sort((a, b) => {
  const completedAtDiff = (new Date(b.completedAt || 0).getTime() || 0) - (new Date(a.completedAt || 0).getTime() || 0)
  return completedAtDiff || String(a.id).localeCompare(String(b.id))
}))
const allUploadsSelected = computed(() => tabUploads.value.length > 0 && tabUploads.value.every(item => selectedUploadIds.has(item.id)))
const allDownloadsSelected = computed(() => tabDownloads.value.length > 0 && tabDownloads.value.every(item => selectedDownloadIds.has(item.id)))
const selectedUploadItems = computed(() => uploadsActive.value.filter(item => selectedUploadIds.has(item.id)))
const selectedDownloadItems = computed(() => downloadsActive.value.filter(item => selectedDownloadIds.has(item.id)))
const canPauseSelectedUploads = computed(() => selectedUploadItems.value.some(canPauseUpload))
const canResumeSelectedUploads = computed(() => selectedUploadItems.value.some(canResumeUpload))
const canTerminateSelectedUploads = computed(() => selectedUploadItems.value.some(canTerminateUpload))
const canPauseAllUploads = computed(() => tabUploads.value.some(canPauseUpload))
const canResumeAllUploads = computed(() => tabUploads.value.some(canResumeUpload))
const canTerminateAllUploads = computed(() => tabUploads.value.some(canTerminateUpload))
const canPauseSelectedDownloads = computed(() => selectedDownloadItems.value.some(downloadCanPause))
const canResumeSelectedDownloads = computed(() => selectedDownloadItems.value.some(downloadCanResume))
const canTerminateSelectedDownloads = computed(() => selectedDownloadItems.value.some(downloadCanTerminate))
const canPauseAllDownloads = computed(() => tabDownloads.value.some(downloadCanPause))
const canResumeAllDownloads = computed(() => tabDownloads.value.some(downloadCanResume))
const canTerminateAllDownloads = computed(() => tabDownloads.value.some(downloadCanTerminate))
const readOnly = computed(() => user.value.readOnly === true)
const batchShareOpen = ref(false)
const batchSharing = ref(false)
const batchShareError = ref('')
const batchShareNotice = ref('')
const batchShareResults = ref([])
const batchShareGroup = ref(null)
const batchShareForm = ref({ expiresInHours: 24, maxDownloads: 0 })
const batchShareCount = ref(0)
function toggleSelect(id) { if (selectedIds.has(id)) selectedIds.delete(id); else selectedIds.add(id) }
function toggleSelectAll() { if (allSelected.value) { files.value.forEach(file => selectedIds.delete(file.id)); childFolders.value.forEach(folder => selectedFolderIds.delete(folder.id)) } else { files.value.forEach(file => selectedIds.add(file.id)); childFolders.value.forEach(folder => selectedFolderIds.add(folder.id)) } }
function toggleUploadSelect(id) { if (selectedUploadIds.has(id)) selectedUploadIds.delete(id); else selectedUploadIds.add(id) }
function toggleDownloadSelect(id) { if (selectedDownloadIds.has(id)) selectedDownloadIds.delete(id); else selectedDownloadIds.add(id) }
function toggleAllUploads() { if (allUploadsSelected.value) tabUploads.value.forEach(item => selectedUploadIds.delete(item.id)); else tabUploads.value.forEach(item => selectedUploadIds.add(item.id)) }
function toggleAllDownloads() { if (allDownloadsSelected.value) tabDownloads.value.forEach(item => selectedDownloadIds.delete(item.id)); else tabDownloads.value.forEach(item => selectedDownloadIds.add(item.id)) }
function openBatchShare() { if (readOnly.value) return; batchShareCount.value = selectedIds.size; batchShareOpen.value = true; batchShareError.value = ''; batchShareNotice.value = ''; batchShareResults.value = []; batchShareGroup.value = null; batchShareForm.value = { expiresInHours: 24, maxDownloads: 0 } }
function closeBatchShare() { if (!batchSharing.value) batchShareOpen.value = false }
// createBatchShare 创建统一聚合分享链接（#7）：一个 token 对应整个文件集合，替代逐文件独立链接。
// createBatchShare creates one unified aggregate share link (#7): a single token for the whole file set.
async function createBatchShare() {
  if (readOnly.value) { batchShareError.value = t('readOnly.error'); return }
  const fileIds = [...selectedIds]
  const folderIds = [...selectedFolderIds]
  if (!fileIds.length && !folderIds.length) return
  batchSharing.value = true
  batchShareError.value = ''
  try {
    const body = await api('/api/files/batch-share-group', { method: 'POST', body: JSON.stringify({ fileIds, folderIds, ...batchShareForm.value }) })
    batchShareGroup.value = body.data
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
// batchShareGroupUrl 统一分享链接的绝对地址。
// batchShareGroupUrl is the absolute URL of the unified share link.
const batchShareGroupUrl = computed(() => batchShareGroup.value?.url ? new URL(batchShareGroup.value.url, window.location.origin).href : '')
async function copyBatchShareGroupUrl() {
  const value = batchShareGroupUrl.value
  if (!value) return
  try { await navigator.clipboard.writeText(value) } catch { const input = document.querySelector('.batch-share-results .share-url input'); input?.select(); document.execCommand('copy') }
  batchShareNotice.value = t('files.shareCopied')
}
async function batchDownload() {
  const ids = [...selectedIds]
  if (!ids.length) return
  batchDownloading.value = true
  error.value = ''
  let item = { id: `dl-batch-${Date.now()}`, name: '', batch: true, downloadName: batchDownloadFilename(), batchIds: ids, size: 0, loadedBytes: 0, progress: 0, rate: 0, status: 'downloading', failed: false, paused: false, cancelled: false, completed: false, running: false, error: '', controller: null, parts: [], resumable: false, transferGeneration: 0 }
  batchDownloadItem = item
  downloads.value.push(item = reactive(item))
  transfersOpen.value = true
  persistTransfers()
  await startDownload(item)
  if (item.completed) { selectedIds.clear(); notice.value = lazyText('files.batchDownloadDone', { count: ids.length }) }
  if (batchDownloadItem === item) batchDownloadItem = null
  batchDownloading.value = false
}
function cancelBatchDownload() { if (batchDownloadItem) terminateDownload(batchDownloadItem); else batchDownloadController?.abort() }
function cancelDownload(item) { terminateDownload(item) }
// batchDelete confirms and removes the selected files and folders, then refreshes quota and the current directory.
async function batchDelete() {
  if (readOnly.value) { error.value = lazyText('readOnly.error'); return }
  const fileIds = [...selectedIds]
  const folderIds = [...selectedFolderIds]
  if (!fileIds.length && !folderIds.length) return
  const confirmation = fileIds.length && folderIds.length
    ? t('confirm.deleteSelection', { files: fileIds.length, folders: folderIds.length })
    : fileIds.length
      ? t('confirm.deleteFiles', { count: fileIds.length })
      : t('confirm.deleteFolders', { count: folderIds.length })
  if (!(await askConfirm(confirmation))) return
  batchDeleting.value = true
  error.value = ''
  try {
    try {
      await api('/api/files/batch-delete', { method: 'POST', body: JSON.stringify({ ids: fileIds, folder_ids: folderIds }) })
    } catch (err) {
      if (err?.data?.code !== 'FOLDER_NOT_EMPTY') throw err
      const folderMessage = Array.isArray(err.data?.folders) && err.data.folders.length
        ? err.data.folders.join('、')
        : err.message
      const confirmed = await askConfirm(t('confirm.deleteNonEmptyFolders', { message: folderMessage }))
      if (!confirmed) {
        error.value = err.message
        return
      }
      await api('/api/files/batch-delete', { method: 'POST', body: JSON.stringify({ ids: fileIds, folder_ids: folderIds, force: true }) })
    }
    selectedIds.clear()
    selectedFolderIds.clear()
    notice.value = fileIds.length && folderIds.length
      ? t('notice.deleteSelectionDone', { files: fileIds.length, folders: folderIds.length })
      : fileIds.length
        ? t('notice.filesDeleted', { count: fileIds.length })
        : t('notice.foldersDeleted')
    await loadMe()
    await loadFolders()
    await loadFiles()
  } catch (err) {
    error.value = err.message
  } finally {
    batchDeleting.value = false
  }
}
async function submitClearAll() {
  if (readOnly.value) return
  clearAllBusy.value = true
  clearAllError.value = ''
  try {
    const payload = user.value.totpEnabled
      ? { code: clearAllAuth.value.code }
      : { password: clearAllAuth.value.password }
    payload.diskMode = clearAllDiskMode.value
    const body = await api('/api/files/clear-all', { method: 'POST', body: JSON.stringify(payload) })
    const data = body.data || {}
    clearAllOpen.value = false
    clearAllAuth.value = { password: '', code: '' }
    selectedIds.clear()
    selectedFolderIds.clear()
    // 清空后总数变小，停在第 N 页会落到空列表，因此回到第 1 页（v030 #3）。
    page.value = 1
    await loadMe()
    await loadFolders()
    await loadFiles()
    notice.value = lazyText('files.clearedNotice', { files: data.files || 0, tasks: data.tasks || 0, shares: data.shares || 0 })
  } catch (err) {
    clearAllError.value = err.message
  } finally {
    clearAllBusy.value = false
  }
}
const quotaPercent = computed(() => Math.min(100, user.value.quotaBytes ? Math.round((user.value.usedBytes / user.value.quotaBytes) * 100) : 0))
// 整体速率 + 每个进行中上传的实时速率：按秒采样 loadedBytes 增量，EMA 平滑后
// 写到 item.rate 供行内展示；整体速率 = 各进行中上传 + 下载速率之和。
// Overall rate plus per-upload live rate: sample loadedBytes deltas every second,
// smooth with an EMA into item.rate for inline display; overall is the sum of
// active upload and download rates.
const overallRate = ref(0)
let rateTimer = null
function sampleOverallRate() {
  const now = Date.now()
  let uploadBytes = 0
  for (const item of uploads.value) {
    const active = item.running && !item.paused && !item.failed && !item.cancelled && !isUploadComplete(item) && !item.terminating
    if (!active) { item.rate = 0; continue }
    const elapsed = item._rateAt ? (now - item._rateAt) / 1000 : 0
    const delta = (item.loadedBytes || 0) - (item._rateBytes ?? 0)
    item._rateBytes = item.loadedBytes || 0
    item._rateAt = now
    if (elapsed > 0 && delta >= 0) {
      item.rate = emaRate(item.rate || 0, delta / elapsed)
    } else if (!(item.rate > 0)) {
      item.rate = 0
    }
    uploadBytes += item.rate || 0
  }
  let downloadBytes = 0
  for (const item of downloads.value) {
    if (item.running && !item.paused && !item.failed && !item.cancelled && item.rate > 0) downloadBytes += item.rate
  }
  overallRate.value = Math.round(uploadBytes + downloadBytes)
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
const activeTransferCount = computed(() => uploadsActive.value.length + downloadsActive.value.length)
function persistMd5() { localStorage.setItem('filebox_show_md5', showMd5.value ? '1' : '0') }
// breadcrumbs 把当前目录路径拆成可点击的分段（根 / a / b）。
// breadcrumbs splits the current directory path into clickable segments (root / a / b).
const breadcrumbs = computed(() => currentDir.value ? currentDir.value.split('/') : [])
function breadcrumbPath(index) { return breadcrumbs.value.slice(0, index + 1).join('/') }
// childFolders 只取当前目录的直接子目录。
// childFolders selects only the direct children of the current directory.
const childFolders = computed(() => { const cd = currentDir.value; return folders.value.filter(folder => { const idx = folder.path.lastIndexOf('/'); return (idx >= 0 ? folder.path.slice(0, idx) : '') === cd }) })
// totalPages 计算总页数；changePageSize/jumpPage 支持每页条数选择与页码跳转（v018 #7）。
// totalPages computes the page count; changePageSize/jumpPage add per-page-size selection and page jumping (v018 #7).
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
function changePageSize() { page.value = 1; localStorage.setItem('filebox_pagesize_files', String(pageSize.value)); loadFiles() }
function jumpPage() { const target = Number(pageInput.value); if (!target || target < 1 || target > totalPages.value) { pageInput.value = ''; return } page.value = target; pageInput.value = ''; loadFiles() }

// loadMe refreshes the current user and quota snapshot, clearing the session on authentication failure.
// loadMe 刷新当前用户和配额快照，认证失效时清理会话并回到登录页。
async function loadMe() { try { const body = await api('/api/auth/me'); user.value = body.data; localStorage.setItem('filebox_user', JSON.stringify(body.data)) } catch { clearSession(); router.push('/login') } }
// loadFiles loads the file list for the current keyword, page, and directory.
// loadFiles 按当前关键字、页码与目录加载文件列表。
async function loadFiles() { const requestVersion = ++fileLoadVersion; if (requestVersion === fileLoadVersion) { loading.value = true; error.value = '' } try { const dirQuery = currentDir.value ? `&dir=${encodeURIComponent(currentDir.value)}` : ''; const body = await api(`/api/files?page=${page.value}&pageSize=${pageSize.value}&keyword=${encodeURIComponent(keyword.value)}&sortBy=${sortBy.value}&sortOrder=${sortOrder.value}${dirQuery}`); if (requestVersion === fileLoadVersion) { files.value = body.data.items; total.value = body.data.total } } catch (err) { if (requestVersion === fileLoadVersion) error.value = err.message } finally { if (requestVersion === fileLoadVersion) loading.value = false } }
// loadFolders 拉取当前用户的全部目录，用于面包屑与子目录导航。
// loadFolders fetches all of the user's folders for breadcrumbs and child-folder navigation.
async function loadFolders() { try { const body = await api(`/api/folders?sortBy=${sortBy.value}&sortOrder=${sortOrder.value}`); folders.value = body.data.items } catch { /* 目录不可用时保持空导航 */ } }
function search() { page.value = 1; keyword.value = searchInput.value.trim(); loadFiles() }
function applySort() { page.value = 1; loadFiles(); loadFolders() } function toggleSort(field) { if (sortBy.value === field) { sortOrder.value = sortOrder.value === 'asc' ? 'desc' : 'asc' } else { sortBy.value = field; sortOrder.value = 'asc' } applySort() } function sortAria(field) { if (sortBy.value !== field) return 'none'; return sortOrder.value === 'asc' ? 'ascending' : 'descending' } function openClearAll() { clearAllError.value = ''; clearAllAuth.value = { password: '', code: '' }; clearAllDiskMode.value = 'empty-dirs'; clearAllOpen.value = true } const clearOwnFileCount = computed(() => Number(user.value.fileCount) || 0);
function transferRowTitle(item) { return transferStateLabel(item) }
// transferStateLabel 把状态码翻成当前语言（失败时优先显示服务端错误文本），
// 因此切换语言后已存在的传输行会跟着重译，而不是保留创建时的语言（v044.2）。
// transferStateLabel renders the status code in the active language (server errors win when failed),
// so existing rows re-translate after a language switch instead of keeping their creation language.
function transferStateLabel(item) { if (!item) return ''; if (item.failed && item.error) return item.error; return statusLabel(item.status, t, { progress: item.statusProgress || 0 }) }
// transferDisplayName 批量下载项不存本地化名称，改用标志位在渲染时取名。
function transferDisplayName(item) { if (!item) return ''; if (item.batch) return t('download.detail.batchName'); return item.name || '' }
const transferListCount = computed(() => uploads.value.length + downloads.value.length)
// clearTransferList 清空传输列表：先终止仍在进行的传输（中止请求并删除服务端任务，从而释放配额），
// 再清空进行中/已完成两个页签的全部记录；进行中的数量会在确认提示里说明（v044.2）。
// clearTransferList stops every running transfer first (aborting requests and deleting the server task
// so quota is released), then empties both tabs; the prompt states how many are still running.
async function clearTransferList() {
  const activeUploads = tabUploads.value.slice()
  const activeDownloads = tabDownloads.value.slice()
  const total = transferListCount.value
  if (!total) return
  const activeCount = activeUploads.length + activeDownloads.length
  const message = activeCount
    ? t('files.clearListConfirmActive', { active: activeCount, total })
    : t('files.clearListConfirm', { total })
  if (!(await askConfirm(message))) return
  if (activeUploads.length) await terminateUploads(activeUploads)
  if (activeDownloads.length) await terminateDownloads(activeDownloads)
  uploads.value = []
  downloads.value = []
  selectedUploadIds.clear()
  selectedDownloadIds.clear()
  batchDownloadItem = null
  batchDownloadController = null
  batchDownloading.value = false
  persistTransfers()
  notice.value = activeCount ? t('files.listClearedActive', { count: activeCount }) : t('files.listCleared')
}
function normalizeTransferDir(value = '') { return String(value || '').replace(/\\/g, '/').replace(/^\/+|\/+$/g, '') }
function navigateDir(path) { currentDir.value = normalizeTransferDir(path); page.value = 1; loadFiles() }
function openNewFolder() { if (readOnly.value) return; folderPrompt.value = { rename: false, name: '' }; folderError.value = '' }
function openRenameFolder(folder) { if (readOnly.value) return; folderPrompt.value = { rename: true, id: folder.id, name: folder.name }; folderError.value = '' }
// submitFolder 创建或重命名目录，成功后刷新目录与文件列表；批量重命名模式下逐个推进到下一目录。
// submitFolder creates or renames a folder and refreshes; in batch mode it advances to the next selected folder.
async function submitFolder() { if (readOnly.value) { folderError.value = t('readOnly.error'); return } const prompt = folderPrompt.value; if (!prompt?.name) return; folderSaving.value = true; folderError.value = ''; try { if (prompt.rename) { await api(`/api/folders/${prompt.id}`, { method: 'PATCH', body: JSON.stringify({ name: prompt.name }) }); notice.value = lazyText('notice.folderRenamed') } else { await api('/api/folders', { method: 'POST', body: JSON.stringify({ name: prompt.name, parent: currentDir.value }) }); notice.value = lazyText('notice.folderCreated') } folderPrompt.value = null; if (prompt.batch && prompt.rename) { selectedFolderIds.delete(prompt.id); batchRenameQueue.value.shift(); renameNextSelectedFolder(); return } loadFolders(); loadFiles() } catch (err) { folderError.value = err.message } finally { folderSaving.value = false } }
async function removeFolder(folder) { if (readOnly.value) { error.value = lazyText('readOnly.error'); return } if (!(await askConfirm(t('confirm.deleteFolder', { name: folder.name })))) return; try { await api(`/api/folders/${folder.id}`, { method: 'DELETE' }); notice.value = lazyText('notice.folderDeleted'); loadFolders(); loadFiles() } catch (err) { error.value = err.message } }
function handleInput(event) { queueFiles([...event.target.files]); event.target.value = '' }
function handleFolderInput(event) { openFolderUploadPrompt([...event.target.files]); event.target.value = '' }
async function pickFolder() {
  if (typeof globalThis.showDirectoryPicker !== 'function') { folderInput.value?.click(); return }
  try {
    const root = await globalThis.showDirectoryPicker()
    const picked = []
    async function collect(handle, relative = '') {
      for await (const entry of handle.values()) {
        const entryPath = relative ? `${relative}/${entry.name}` : entry.name
        if (entry.kind === 'file') {
          const file = await entry.getFile()
          picked.push({ file, relPath: `${root.name}/${entryPath}` })
        } else if (entry.kind === 'directory') {
          await collect(entry, entryPath)
        }
      }
    }
    await collect(root)
    openFolderUploadPrompt(picked)
  } catch (err) {
    if (err?.name !== 'AbortError') throw err
  }
}

function openFolderUploadPrompt(list) {
  if (!list?.length) { notice.value = lazyText('files.folderEmpty'); return }
  const normalized = list.map(value => { const file = value.file || value; return { file, relPath: value.relPath || file.webkitRelativePath || file.name } })
  const hasDirs = normalized.some(value => value.relPath.includes('/'))
  if (!hasDirs) { queueFiles(normalized); return }
  const folderNames = [...new Set(normalized.map(value => value.relPath.split('/').filter(Boolean)[0]).filter(Boolean))]
  folderUploadPrompt.value = {
    count: normalized.length,
    totalSize: normalized.reduce((sum, value) => sum + (value.file.size || 0), 0),
    hasDirs,
    folderName: folderNames.join(', '),
    targetDir: normalizeTransferDir(currentDir.value),
    list: normalized
  }
}
function cancelFolderUpload() { folderUploadPrompt.value = null }
function confirmFolderUpload() { const prompt = folderUploadPrompt.value; if (!prompt?.list?.length) { cancelFolderUpload(); return } folderUploadPrompt.value = null; queueFiles(prompt.list, { skipBulkConfirm: true, targetDir: prompt.targetDir }) }

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
    paused: !!item.paused, failed: !!item.failed, done: isUploadComplete(item), completedAt: item.completedAt || '', cancelled: !!item.cancelled, error: item.error || '', canContinue: !!item.canContinue
  }
}
function snapshotDownload(item) {
  return { kind: 'download', id: item.id, name: item.name || '', batch: !!item.batch, fileId: item.fileId || '', size: item.size || 0, loadedBytes: item.loadedBytes || 0, progress: item.progress ?? 0, status: item.status || '', paused: !!item.paused, failed: !!item.failed, cancelled: !!item.cancelled, completed: !!item.completed, completedAt: item.completedAt || '', error: item.error || '' }
}
// persistTransfers 把当前面板记录的可序列化快照写入 sessionStorage（按用户隔离，退出登录时清理）。
// persistTransfers writes serializable snapshots of the current drawer into sessionStorage (per-user; cleared on logout).
function persistTransfers() {
  // 轻推数组引用：条目是普通对象，其上的状态写入不会自动通知派生它的 computed（列表归属、页签计数），
  // 曾表现为"行内已是已完成、仍留在传输中"或"徽标有 1 而列表为空"，刷新后才恢复（v044.22）。
  uploads.value = uploads.value.slice()
  downloads.value = downloads.value.slice()
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
      const done = Boolean(snap.completed || snap.cancelled)
      downloads.value.push({ id: snap.id, name: snap.name || '', batch: !!snap.batch, fileId: snap.fileId || '', size: snap.size || 0, loadedBytes: snap.loadedBytes || 0, progress: snap.progress ?? 0, rate: 0, status: snap.cancelled ? 'cancelled' : (done ? 'completed' : 'session_ended'), paused: false, failed: !!snap.failed, cancelled: done ? !!snap.cancelled : true, completed: !!snap.completed, completedAt: snap.completedAt || '', running: false, error: snap.error || '', controller: null, parts: [], transferGeneration: 0, restored: true })
    } else if (snap.kind === 'upload') {
      const done = Boolean(snap.done || snap.cancelled)
      uploads.value.push({
        id: snap.id, file: null, name: snap.name, relPath: snap.relPath || '', dir: snap.dir || '', size: snap.size || 0,
        progress: snap.progress || 0, loadedBytes: snap.loadedBytes || 0, rate: 0,
        status: done ? (snap.cancelled ? 'cancelled' : 'completed') : (snap.failed ? 'failed' : 'need_reselect'),
        taskId: snap.taskId || '', sha256: snap.sha256 || '', uploaded: [], paused: !!snap.paused, chunksTotal: 0, chunkSize: 0,
        error: snap.error || '', failed: !!snap.failed, done, completedAt: snap.completedAt || '', cancelled: !!snap.cancelled, canContinue: !!snap.canContinue, running: false, terminating: false, pending: new Set(), controllers: new Map(), requestControllers: new Set(), transferGeneration: 0, needsReselect: !done, restored: true
      })
    }
  }
  // 恢复后立即消费快照，避免重复恢复；后续状态由 persistTransfers 维护。
  // Consume the snapshot immediately to avoid double restoration; later state is maintained by persistTransfers.
  sessionStorage.removeItem(TRANSFERS_KEY)
}

// queueFiles creates one resumable task per selected file and preserves folder-relative directories.
// queueFiles 为每个文件创建可续传任务，并固定本次队列的目标目录。
async function queueFiles(list, options = {}) {
  if (!list?.length) return
  if (readOnly.value) { error.value = lazyText('readOnly.error'); return }
  const targetDir = normalizeTransferDir(options.targetDir !== undefined ? options.targetDir : currentDir.value)
  const configuredMax = Number(brand.maxFileSize)
  const maxSize = Number.isFinite(configuredMax) && configuredMax > 0 ? configuredMax : 100 * 1024 * 1024 * 1024
  const oversized = list.filter(value => (value.file || value).size > maxSize)
  const keep = list.filter(value => (value.file || value).size <= maxSize)
  if (oversized.length) { error.value = lazyText('files.tooManyTooLarge', { count: oversized.length, max: formatBytes(maxSize) }) }
  if (needsBulkConfirm(keep.length, Boolean(options.skipBulkConfirm)) && !(await askConfirm(t('files.bulkConfirm', { count: keep.length })))) return
  if (!keep.length) return
  transfersOpen.value = true
  const batches = transferBatches(keep, 32)
  for (const [batchIndex, batch] of batches.entries()) {
    for (const value of batch) {
      const file = value.file || value
      const path = (value.relPath || file.webkitRelativePath || file.name).replace(/\\/g, '/')
      const parts = path.split('/').filter(Boolean)
      const dirParts = parts.length > 1 ? parts.slice(0, -1) : []
      const relDir = dirParts.length ? dirParts.join('/') : ''
      const dir = relDir ? `${targetDir ? `${targetDir}/` : ''}${relDir}` : targetDir
      const relPath = path !== file.name ? path : ''
      const candidates = uploads.value.filter(entry => canAdoptReselectedFile(entry) && entry.name === file.name && entry.size === file.size && normalizeTransferDir(entry.dir) === dir && (entry.relPath || '') === relPath)
      let restored = null
      let fileHash = ''
      for (const candidate of candidates) {
        if (candidate.sha256) {
          fileHash ||= await computeFileSHA256(file)
          // 超过客户端上限的文件会跳过哈希（返回空串），此时退化为按 名称+大小+目录+相对路径 匹配，
          // 以便断点续传仍能恢复（v036）。
          // Files over the client hash limit return an empty string; fall back to matching by
          // name+size+dir+relPath so resume still restores (v036).
          if (fileHash && fileHash !== candidate.sha256) continue
        }
        restored = candidate
        break
      }
      if (restored) {
        restored.file = file
        restored.dir = dir
        restored.relPath = relPath
        restored.needsReselect = false
        restored.paused = false
        restored.failed = false
        restored.done = false
        restored.cancelled = false
        restored.error = ''
        restored.status = 'preparing'
        // 必须同时清零进度：终止态契约把 progress ≥ 100 也算终止，若只清 done，一条被误标为
        // "已完成"（progress 100）的旧记录会让 uploadStartable 直接拦下，重选文件后静默无反应（v044.9）。
        restored.progress = 0
        restored.statusProgress = 0
        restored.transferGeneration = Number(restored.transferGeneration) || 0
        restored.requestControllers ||= new Set()
        restored.controllers ||= new Map()
        runGated(restored)
      } else {
        let item = {
          id: `${Date.now()}-${Math.random()}-${file.name}`, file, relPath, dir,
          progress: 0, loadedBytes: 0, rate: 0, status: t('files.uploadPreparing'),
          taskId: '', uploaded: [], paused: false, chunksTotal: 0, chunkSize: 0, error: '',
          failed: false, done: false, cancelled: false, canContinue: false, running: false, terminating: false,
          sha256: fileHash, pending: new Set(), controllers: new Map(), requestControllers: new Set(), resolve: '', transferGeneration: 0
        }
        uploads.value.push(item = reactive(item))
        runGated(item)
      }
    }
    if (batchIndex + 1 < batches.length) await new Promise(resolve => setTimeout(resolve, 0))
  }
  persistTransfers()
}

// 上传并发闸门：公平有界调度，最多 3 个文件同时进入校验/初始化阶段。
// 使用 FairStartGate 取代忙等轮询：大量文件入队时不会产生无界定时器，
// 暂停/终止的文件会在等待队列中被公平地让出，不会误报网络失败。
// Bounded fair scheduler: at most 3 files enter checksum/init concurrently.
// FairStartGate replaces busy-wait polling so large batches spawn no unbounded
// timers; paused/terminated waiters yield their slot fairly.
const uploadStartGate = new FairStartGate(3)
function uploadStartable(item) { return Boolean(item && item.file && !item.paused && !item.failed && !item.terminating && !item.cancelled && !isUploadComplete(item)) }
async function runGated(item) {
  if (!uploadStartable(item)) return
  const granted = await uploadStartGate.acquire(() => uploadStartable(item))
  if (!granted) return
  try {
    if (!uploadStartable(item)) return
    await startUpload(item)
  } finally {
    uploadStartGate.release()
  }
}
function transferAbortError() { return new DOMException('stale transfer operation', 'AbortError') }
function transferIsCurrent(item, generation) { return isTransferGenerationCurrent(item, generation) }
function makeTransferRequest(item, generation) {
  if (!transferIsCurrent(item, generation)) throw transferAbortError()
  const controller = new AbortController()
  item.requestControllers ||= new Set()
  item.requestControllers.add(controller)
  return controller
}
async function transferApi(item, generation, path, options = {}) {
  const controller = makeTransferRequest(item, generation)
  try {
    return await api(path, { ...options, signal: controller.signal })
  } finally {
    item.requestControllers?.delete(controller)
  }
}
function abortTransferRequests(item, includeLifecycle = true) {
  item.controllers?.forEach(controller => controller.abort())
  if (includeLifecycle) item.requestControllers?.forEach(controller => controller.abort())
}
function cancelTransferPrompts(item) {
  for (const queue of [conflictQueue, confirmQueue]) {
    for (let index = queue.value.length - 1; index >= 0; index--) {
      const entry = queue.value[index]
      if (entry.owner !== item) continue
      queue.value.splice(index, 1)
      clearTimeout(entry.timer)
      entry.resolve(queue === conflictQueue ? 'cancel' : false)
    }
  }
}
function invalidateTransfer(item, includeLifecycle = true) {
  const generation = advanceTransferGeneration(item)
  cancelTransferPrompts(item)
  removeQueued(item)
  item.pending?.clear()
  abortTransferRequests(item, includeLifecycle)
  uploadStartGate.notify()
  wakeWorkers()
  return generation
}
function wakeWorkers() { const wake = workerWake; workerWake = null; wake?.() }
function removeQueued(item) { for (let index = chunkQueue.length - 1; index >= 0; index--) if (chunkQueue[index].item === item) chunkQueue.splice(index, 1) }
function enqueueChunks(item, indexes, generation = item.transferGeneration) {
  if (!transferIsCurrent(item, generation) || item.paused || item.failed || item.terminating) return
  indexes.forEach(index => { if (!item.pending.has(index)) { item.pending.add(index); chunkQueue.push({ item, index, generation }) } })
  wakeWorkers()
  ensureWorkers()
}
function ensureWorkers() {
  while (activeWorkers < 4 && chunkQueue.some(task => transferIsCurrent(task.item, task.generation) && !task.item.paused && !task.item.failed && !task.item.terminating)) {
    activeWorkers++
    chunkWorker().finally(() => { activeWorkers--; ensureWorkers() })
  }
}
async function chunkWorker() {
  while (true) {
    const position = chunkQueue.findIndex(task => transferIsCurrent(task.item, task.generation) && !task.item.paused && !task.item.failed && !task.item.terminating)
    if (position < 0) return
    const queued = chunkQueue.splice(position, 1)[0]
    const { item, index, generation } = queued
    if (!transferIsCurrent(item, generation) || !item.pending.has(index)) continue
    try {
      await uploadChunkWithRetry(item, index, generation)
      if (!transferIsCurrent(item, generation)) continue
      item.pending.delete(index)
      if (!item.uploaded.includes(index)) item.uploaded.push(index)
      item.uploaded.sort((a, b) => a - b)
      updateChunkProgress(item)
    } catch (err) {
      item.pending.delete(index)
      if (!transferIsCurrent(item, generation) || item.paused || item.terminating || err.name === 'AbortError') continue
      const mapped = friendlyError(err)
      item.failed = true
      item.error = mapped.message
      item.status = ''
      error.value = (item.file?.name || item.name) + ': ' + mapped.message
      persistTransfers()
    }
  }
}
function updateChunkProgress(item) { item.progress = item.chunksTotal ? Math.round(25 + item.uploaded.length / item.chunksTotal * 75) : 25; syncLoadedBytes(item); item.status = 'uploading' }
// syncLoadedBytes 让 loadedBytes 与进度一致，供整体速率统计采样。
// syncLoadedBytes keeps loadedBytes in step with progress for overall-rate sampling.
function syncLoadedBytes(item) { item.loadedBytes = item.file?.size ? Math.round(item.file.size * (item.progress || 0) / 100) : 0 }

// uploadChunkWithRetry uploads one binary chunk with an abortable fetch and exponential backoff.
// uploadChunkWithRetry 使用可中止 fetch 上传单个二进制分片，并以指数退避重试。
async function uploadChunkWithRetry(item, index, generation) {
  const start = index * item.chunkSize
  const end = Math.min(item.file.size, start + item.chunkSize)
  for (let attempt = 0; attempt < 4; attempt++) {
    if (!transferIsCurrent(item, generation) || item.paused || item.terminating) throw transferAbortError()
    const controller = new AbortController()
    item.controllers.set(index, controller)
    try {
      const response = await fetch('/api/files/' + item.taskId + '/chunks/' + index, { method: 'PUT', headers: { Authorization: 'Bearer ' + localStorage.getItem('filebox_token') }, body: item.file.slice(start, end), signal: controller.signal })
      if (!transferIsCurrent(item, generation) || item.paused || item.terminating) throw transferAbortError()
      if (!response.ok) {
        let body = null
        try { body = await response.json() } catch {}
        throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { status: response.status, data: body?.data })
      }
      return
    } catch (err) {
      if (err.name === 'AbortError' || !transferIsCurrent(item, generation) || item.paused || item.terminating) throw err
      if (attempt === 3) throw err
      await new Promise(resolve => setTimeout(resolve, 300 * 2 ** attempt))
    } finally {
      if (item.controllers.get(index) === controller) item.controllers.delete(index)
    }
  }
}
function waitForChunks(item, generation) {
  return new Promise(resolve => {
    const check = () => {
      if (!transferIsCurrent(item, generation) || item.paused || item.failed || item.terminating || item.pending.size === 0) { resolve(); return }
      setTimeout(check, 80)
    }
    check()
  })
}

// startUpload computes the checksum, performs instant-upload lookup, resumes missing chunks, and completes the task.
// startUpload 负责计算客户端校验值、秒传检查、补传缺片并提交完成请求。
function startUpload(item) {
  if (item.running || item.terminating) return item.uploadPromise
  const generation = advanceTransferGeneration(item)
  item.running = true
  item.paused = false
  item.failed = false
  item.canContinue = false
  item.done = false
  item.cancelled = false
  item.rate = 0
  item._rateAt = 0
  item._rateBytes = undefined
  let operationPromise
  operationPromise = (async () => {
    let init = null
    try {
      if (item.taskId) {
        try {
          await finishExistingUpload(item, generation)
          return
        } catch (err) {
          if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
          if (err.status !== 404) throw err
          item.taskId = ''
          item.uploaded = []
          item.pending.clear()
          item.canContinue = false
        }
      }
      if (!item.sha256) {
        item.status = 'checksum'
        item.statusProgress = 0
        item.sha256 = await computeFileSHA256(item.file, progress => {
          if (transferIsCurrent(item, generation) && !item.paused && !item.terminating) {
            item.progress = Math.round(progress * 0.25)
            syncLoadedBytes(item)
            item.statusProgress = progress
          }
        })
      }
      if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
      // 超过客户端上限的大文件跳过了哈希（item.sha256 为空），此时不能调用按哈希查重的
      // /api/files/check（该接口要求至少一个哈希，空哈希会返回 400「文件校验值缺失」），
      // 直接进入上传流程；服务端会在组装时算出 sha256（v036）。
      // A skipped hash means /api/files/check cannot be called (it requires a hash and answers 400
      // otherwise), so go straight to the upload; the server computes sha256 while assembling (v036).
      if (item.file.size > 0 && item.sha256) {
        const check = await transferApi(item, generation, '/api/files/check', { method: 'POST', body: JSON.stringify({ sha256: item.sha256, size: item.file.size, name: item.file.name, ...(item.dir ? { dir: item.dir } : {}) }) })
        if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
        if (check.data?.instant) {
          item.progress = 100
          syncLoadedBytes(item)
          item.status = 'instant'
          item.done = true
          item.completedAt = completionTimestamp(check.data, new Date().toISOString())
          item.rate = 0
          selectedUploadIds.delete(item.id)
          notice.value = lazyText('files.instantUpload')
          await loadFiles()
          if (!transferIsCurrent(item, generation)) return
          await loadMe()
          persistTransfers()
          return
        }
        if (check.data?.conflict) {
          const resolve = await askConflict(check.data.existing, item)
          if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
          if (resolve === 'cancel') throw new Error(t('files.uploadCancelled'))
          init = await requestUploadInit(item, resolve, generation)
          if (!transferIsCurrent(item, generation) || item.paused || item.terminating) {
            if (item.terminating && init?.data?.taskId) item.taskId = init.data.taskId
            return
          }
          item.taskId = init.data.taskId
          item.chunkSize = init.data.chunkSize
          item.chunksTotal = init.data.totalChunks
          item.uploaded = [...(init.data.uploadedChunks || [])]
          item.status = 'uploading'
          await continueChunks(item, generation)
          if (!transferIsCurrent(item, generation) || item.paused || item.failed || item.terminating) return
          await finishExistingUpload(item, generation)
          return
        }
      }
      try {
        init = await requestUploadInit(item, '', generation)
      } catch (err) {
        if (err.status !== 409 || !err.data?.conflict) throw err
        const resolve = await askConflict(err.data.existing, item)
        if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
        if (resolve === 'cancel') throw new Error(t('files.uploadCancelled'))
        init = await requestUploadInit(item, resolve, generation)
      }
      if (!transferIsCurrent(item, generation) || item.paused || item.terminating) {
        if (item.terminating && init?.data?.taskId) item.taskId = init.data.taskId
        return
      }
      item.taskId = init.data.taskId
      item.chunkSize = init.data.chunkSize
      item.chunksTotal = init.data.totalChunks
      item.uploaded = [...(init.data.uploadedChunks || [])]
      item.status = 'uploading'
      await continueChunks(item, generation)
      if (!transferIsCurrent(item, generation) || item.paused || item.failed || item.terminating) return
      await finishExistingUpload(item, generation)
    } catch (err) {
      if (!transferIsCurrent(item, generation) || item.paused || item.terminating || err.name === 'AbortError') return
      const mapped = friendlyError(err)
      item.failed = true
      item.canContinue = Boolean(item.taskId)
      item.error = mapped.message
      item.status = ''
      error.value = (item.file?.name || item.name) + ': ' + mapped.message
    } finally {
      if (item.uploadPromise === operationPromise) {
        item.running = false
        item.uploadPromise = null
      }
    }
  })()
  item.uploadPromise = operationPromise
  return operationPromise
}
async function finishExistingUpload(item, generation) {
  if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
  await continueChunks(item, generation)
  if (!transferIsCurrent(item, generation) || item.paused || item.failed || item.terminating) return
  item.status = 'checking'
  item.progress = 99
  syncLoadedBytes(item)
  const completeBody = { sha256: item.sha256 }
  if (item.resolve) completeBody.action = item.resolve
  const body = await transferApi(item, generation, '/api/files/' + item.taskId + '/complete', { method: 'POST', body: JSON.stringify(completeBody) })
  if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
  item.progress = 100
  syncLoadedBytes(item)
  item.status = 'completed'
  item.done = true
  item.completedAt = completionTimestamp(body?.data, new Date().toISOString())
  item.rate = 0
  selectedUploadIds.delete(item.id)
  notice.value = lazyText('files.uploadComplete', { name: item.relPath || item.file.name })
  await loadMe()
  if (!transferIsCurrent(item, generation)) return
  await loadFiles()
  persistTransfers()
}
async function continueChunks(item, generation) {
  if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
  const body = await transferApi(item, generation, '/api/files/' + item.taskId + '/status')
  if (!transferIsCurrent(item, generation) || item.paused || item.terminating) return
  item.chunkSize = body.data.chunkSize
  item.chunksTotal = body.data.totalChunks
  item.uploaded = [...(body.data.uploadedChunks || [])].sort((a, b) => a - b)
  const missing = Array.from({ length: item.chunksTotal }, (_, index) => index).filter(index => !item.uploaded.includes(index))
  item.pending.clear()
  removeQueued(item)
  updateChunkProgress(item)
  if (!missing.length || item.paused || item.terminating) return
  enqueueChunks(item, missing, generation)
  await waitForChunks(item, generation)
}
function requestUploadInit(item, resolve = '', generation = item.transferGeneration) {
  item.resolve = resolve
  return transferApi(item, generation, '/api/files/upload-init', { method: 'POST', body: JSON.stringify({ name: item.file.name, size: item.file.size, chunkSize: item.file.size <= 8 * 1024 * 1024 ? item.file.size : 4194304, mime: item.file.type, sha256: item.sha256, ...(item.dir ? { dir: item.dir } : {}), ...(resolve ? { resolve } : {}) }) })
}
function pauseUpload(item) { if (!canPauseUpload(item)) return; item.paused = true; item.status = 'paused'; invalidateTransfer(item, true); persistTransfers() }
async function resumeUpload(item) { if (!canResumeUpload(item)) return; item.paused = false; item.failed = false; item.cancelled = false; item.canContinue = false; item.status = 'uploading'; await runGated(item); persistTransfers() }
async function retryUpload(item) { if (item.running || item.terminating || !item.file) return; item.failed = false; item.error = ''; item.paused = false; item.cancelled = false; await runGated(item); persistTransfers() }
function isUploadComplete(item) { return isUploadTerminalState(item, t('files.completed')) }
function isDownloadComplete(item) { return Boolean(item && (item.completed || item.cancelled || (!item.failed && item.progress >= 100))) }
function finishedResult(item, kind) { if (kind === 'download' && item.cancelled) return t('files.finishedCancelled'); if (item.failed) return t('files.finishedFailed'); return t('files.finishedSuccess') }
function uploadFinishedLabel(item) {
  const kind = uploadResultKind(item)
  if (kind === 'cancelled') return t('files.finishedCancelled')
  if (kind === 'failed') return t('files.finishedFailed')
  return t('files.finishedSuccess')
}
function uploadFinishedClass(item) {
  const kind = uploadResultKind(item)
  if (kind === 'cancelled') return 'finished-cancelled'
  if (kind === 'failed') return 'finished-failed'
  return 'finished-success'
}
function canPauseUpload(item) { const busyState = ['checking', 'checksum'].includes(normalizeStatus(item?.status)); return Boolean(item && item.running && item.progress < 100 && !item.paused && !item.terminating && !busyState) }
function canResumeUpload(item) { return Boolean(item && item.file && !item.running && !item.terminating && !isUploadComplete(item) && (item.paused || item.failed || item.canContinue)) }
function canTerminateUpload(item) { return Boolean(item && !item.terminating && !isUploadComplete(item)) }
function uniqueTransferItems(items) { return [...new Map(items.filter(Boolean).map(item => [item.id, item])).values()] }
function pauseUploads(items) { uniqueTransferItems(items).forEach(item => { if (canPauseUpload(item)) pauseUpload(item) }); persistTransfers() }
function pauseSelectedUploads() { pauseUploads(selectedUploadItems.value.slice()) }
function pauseAllUploads() { pauseUploads(tabUploads.value.slice()) }
async function resumeUploads(items) { const targets = uniqueTransferItems(items).filter(canResumeUpload); if (!targets.length) return; uploadBatchBusy.value = true; try { await Promise.all(targets.map(resumeUpload)) } finally { uploadBatchBusy.value = false; persistTransfers() } }
function resumeSelectedUploads() { return resumeUploads(selectedUploadItems.value.slice()) }
function resumeAllUploads() { return resumeUploads(tabUploads.value.slice()) }
async function deleteUploadTask(item, taskId = item.taskId) { if (!taskId) return null; const body = await api('/api/upload-tasks/' + taskId, { method: 'DELETE' }); if (item.taskId === taskId) item.taskId = ''; return body }
async function terminateUploads(items) {
  const targets = uniqueTransferItems(items).filter(item => uploads.value.some(entry => entry.id === item.id) && canTerminateUpload(item))
  if (!targets.length) return
  uploadBatchBusy.value = true
  let success = 0
  let failed = 0
  try {
    for (const item of targets) {
      const generation = invalidateTransfer(item, false)
      item.terminating = true
      item.paused = true
      item.status = 'terminating'
      const uploadPromise = item.uploadPromise
      let outcome = null
      let removed = true
      try {
        if (uploadPromise) await uploadPromise
        const taskId = item.taskId
        if (taskId) outcome = await deleteUploadTask(item, taskId)
      } catch (err) {
        removed = false
        failed++
        item.terminating = false
        item.failed = true
        item.canContinue = Boolean(item.taskId)
        item.error = err.message
        item.status = ''
        item.paused = true
      }
      if (!removed || !transferIsCurrent(item, generation)) continue
      const completed = outcome?.data?.state === 'complete'
      item.terminating = false
      item.paused = false
      item.failed = false
      item.error = ''
      item.done = completed
      item.cancelled = !completed
      item.canContinue = false
      item.rate = 0
      if (completed) {
        item.progress = 100
        syncLoadedBytes(item)
        item.completedAt = completionTimestamp(outcome.data, item.completedAt || new Date().toISOString())
        item.status = 'completed'
      } else {
        item.status = 'cancelled'
      }
      selectedUploadIds.delete(item.id)
      success++
    }
    notice.value = lazyText('files.transferTerminated', { success, failed })
  } finally {
    uploadBatchBusy.value = false
    persistTransfers()
  }
}
function terminateSelectedUploads() { return terminateUploads(selectedUploadItems.value.slice()) }
function terminateAllUploads() { return terminateUploads(tabUploads.value.slice()) }
function dismissUpload(item) { uploads.value = uploads.value.filter(entry => entry !== item); selectedUploadIds.delete(item.id); persistTransfers() }
function clearFinishedUpload(item) { if (!isUploadTerminal(item)) return; dismissUpload(item) }
// askConflict 将冲突请求加入队列并返回 Promise；队列保证每个请求最终都被 resolve
// （用户选择或 60s 超时按取消处理），避免并发同名文件相互覆盖导致协程永久挂死。
// askConflict enqueues a conflict prompt and resolves every caller eventually — via the
// user's choice or a 60s timeout treated as cancel — so concurrent same-name conflicts
// can no longer overwrite each other and leave uploads stuck in "preparing".
function askConflict(existing, owner = null) {
  return new Promise(resolve => {
    const entry = { existing, owner, resolve }
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
async function streamDownload(item, request, downloadName, generation) {
  if (!isTransferGenerationCurrent(item, generation)) throw transferAbortError()
  const requestedOffset = item.loadedBytes || 0
  const response = await request(requestedOffset)
  if (!isTransferGenerationCurrent(item, generation)) throw transferAbortError()
  if (!response.ok) {
    let body = null
    try { body = await response.json() } catch {}
    throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { status: response.status })
  }
  if (!response.body) throw new Error(t('error.downloadFailed'))
  const contentRange = response.headers.get('Content-Range') || ''
  const rangeMatch = contentRange.match(/^bytes\s+(\d+)-(\d+)\/(\d+|\*)$/i)
  const isPartial = response.status === 206 && rangeMatch && Number(rangeMatch[1]) === requestedOffset
  if (response.status === 206 && !isPartial) throw new Error(t('error.downloadFailed'))
  if (isPartial) {
    const totalSize = Number(rangeMatch[3])
    if (Number.isFinite(totalSize) && totalSize > 0) item.size = totalSize
  } else {
    item.parts = []
    item.loadedBytes = 0
  }
  const headerSize = Number(response.headers.get('Content-Length'))
  if (!isPartial && Number.isFinite(headerSize) && headerSize > 0) item.size = headerSize
  const reader = response.body.getReader()
  const parts = item.parts || (item.parts = [])
  const startedAt = performance.now()
  let received = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (!isTransferGenerationCurrent(item, generation)) throw transferAbortError()
    if (done) break
    parts.push(value)
    received += value.byteLength
    item.loadedBytes = (isPartial ? requestedOffset : 0) + received
    item.progress = item.size > 0 ? Math.min(100, Math.round(item.loadedBytes / item.size * 100)) : -1
    const elapsed = (performance.now() - startedAt) / 1000
    item.rate = elapsed > 0 ? received / elapsed : 0
  }
  if (!isTransferGenerationCurrent(item, generation) || item.paused || item.cancelled) throw transferAbortError()
  item.loadedBytes = (isPartial ? requestedOffset : 0) + received
  if (item.size > 0 && item.loadedBytes < item.size) throw new Error(t('error.downloadFailed'))
  item.progress = 100
  item.rate = received / Math.max((performance.now() - startedAt) / 1000, 0.001)
  if (!isTransferGenerationCurrent(item, generation)) throw transferAbortError()
  const blob = new Blob(parts, { type: response.headers.get('Content-Type') || 'application/octet-stream' })
  const link = document.createElement('a')
  const objectUrl = URL.createObjectURL(blob)
  link.href = objectUrl
  link.download = downloadName
  link.click()
  setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
  if (!isTransferGenerationCurrent(item, generation)) return
  item.completed = true
  item.completedAt = new Date().toISOString()
  selectedDownloadIds.delete(item.id)
  item.paused = false
  item.status = 'completed'
}

// download streams the file and reports detailed progress in the transfers drawer.
// download 流式下载并在传输面板显示详细进度。
async function download(file) {
  let item = { id: 'dl-' + Date.now() + '-' + file.id, fileId: file.id, name: file.name, size: file.size || 0, loadedBytes: 0, progress: 0, rate: 0, status: t('files.downloading'), failed: false, paused: false, cancelled: false, completed: false, running: false, error: '', controller: null, parts: [], resumable: true, transferGeneration: 0 }
  downloads.value.push(item = reactive(item))
  transfersOpen.value = true
  persistTransfers()
  void startDownload(item)
}
function downloadRequest(item, offset) {
  const headers = new Headers({ Authorization: 'Bearer ' + localStorage.getItem('filebox_token') })
  if (item.resumable && offset > 0) headers.set('Range', 'bytes=' + offset + '-')
  if (item.fileId) return fetch('/api/files/' + item.fileId + '/download', { headers, signal: item.controller.signal })
  headers.set('Content-Type', 'application/json')
  return fetch('/api/files/batch-download', { method: 'POST', headers, body: JSON.stringify({ ids: item.batchIds || [] }), signal: item.controller.signal })
}
async function startDownload(item) {
  if (item.running || item.cancelled || item.completed) return item.downloadPromise
  const generation = advanceTransferGeneration(item)
  item.running = true
  item.paused = false
  item.failed = false
  item.error = ''
  item.status = 'downloading'
  const controller = new AbortController()
  item.controller = controller
  if (item === batchDownloadItem) batchDownloadController = controller
  let promise
  promise = (async () => {
    try {
      await streamDownload(item, offset => downloadRequest(item, offset), item.downloadName || item.name, generation)
    } catch (err) {
      if (!isTransferGenerationCurrent(item, generation)) return
      if (err.name === 'AbortError' && item.paused && !item.cancelled) item.status = 'paused'
      else if (err.name === 'AbortError' || item.cancelled) { item.status = 'cancelled'; item.progress = item.progress < 0 ? 0 : item.progress }
      else { const mapped = friendlyError(err); item.failed = true; item.error = mapped.message; item.status = ''; error.value = item.name + ': ' + mapped.message }
    } finally {
      if (item.controller === controller) { item.running = false; item.controller = null }
      if (batchDownloadController === controller) batchDownloadController = null
      persistTransfers()
    }
  })()
  item.downloadPromise = promise
  return promise
}
function invalidateDownload(item) { const generation = advanceTransferGeneration(item); item.controller?.abort(); return generation }
function downloadCanPause(item) { return Boolean(item && item.running && !item.paused && !item.cancelled && !item.completed) }
function downloadCanResume(item) { return Boolean(item && !item.running && !item.cancelled && !item.completed && (item.paused || item.failed)) }
function downloadCanTerminate(item) { return Boolean(item && !item.cancelled && !item.completed) }
function pauseDownload(item) { if (!downloadCanPause(item)) return; item.paused = true; item.status = 'paused'; invalidateDownload(item); persistTransfers() }
function resumeDownload(item) { if (!downloadCanResume(item)) return; void startDownload(item) }
function terminateDownload(item) { if (!downloadCanTerminate(item)) return; invalidateDownload(item); item.cancelled = true; item.paused = false; item.status = 'cancelled'; downloads.value = downloads.value.filter(entry => entry !== item); selectedDownloadIds.delete(item.id); if (item === batchDownloadItem) { batchDownloadItem = null; batchDownloadController = null; batchDownloading.value = false } persistTransfers() }
function pauseDownloads(items) { uniqueTransferItems(items).forEach(item => { if (downloadCanPause(item)) pauseDownload(item) }); persistTransfers() }
function pauseSelectedDownloads() { pauseDownloads(selectedDownloadItems.value.slice()) }
function pauseAllDownloads() { pauseDownloads(tabDownloads.value.slice()) }
async function resumeDownloads(items) { const targets = uniqueTransferItems(items).filter(downloadCanResume); if (!targets.length) return; downloadBatchBusy.value = true; try { await Promise.all(targets.map(item => startDownload(item))) } finally { downloadBatchBusy.value = false; persistTransfers() } }
function resumeSelectedDownloads() { return resumeDownloads(selectedDownloadItems.value.slice()) }
function resumeAllDownloads() { return resumeDownloads(tabDownloads.value.slice()) }
function terminateDownloads(items) { uniqueTransferItems(items).forEach(item => { if (downloadCanTerminate(item)) terminateDownload(item) }); persistTransfers() }
function terminateSelectedDownloads() { terminateDownloads(selectedDownloadItems.value.slice()) }
function terminateAllDownloads() { terminateDownloads(tabDownloads.value.slice()) }
function clearFinishedDownload(item) { if (!isDownloadComplete(item)) return; downloads.value = downloads.value.filter(entry => entry !== item); selectedDownloadIds.delete(item.id); persistTransfers() }
async function remove(file) { if (readOnly.value) { error.value = lazyText('readOnly.error'); return } if (!(await askConfirm(t('confirm.deleteFile', { name: file.name })))) return; try { await api(`/api/files/${file.id}`, { method: 'DELETE' }); selectedIds.delete(file.id); notice.value = lazyText('notice.fileDeleted'); await loadMe(); await loadFiles() } catch (err) { error.value = err.message } }
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
    if (!item || item.terminating || item.cancelled || item.done) continue
    // 进度样本只作提示：progressHint 严格小于 100，且这里不再用样本写 completedAt——
    // 否则一条"数据库说齐全、磁盘有洞"的样本会让条目显示"成功"（v044.8 修复）。
    const hint = progressHint(item.progress, task)
    if (hint > (item.progress || 0)) { item.progress = hint; syncLoadedBytes(item) }
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
onMounted(() => { restoreTransfers(); loadMe(); loadFiles(); loadFolders(); rateTimer = setInterval(sampleOverallRate, 1000); connectProgressStream(); window.addEventListener('beforeunload', persistTransfers) })
onBeforeUnmount(() => {
  if (rateTimer) clearInterval(rateTimer)
  window.removeEventListener('beforeunload', persistTransfers)
  closeProgressStream()
  cancelBatchDownload()
  downloads.value.filter(item => item.running).forEach(item => { invalidateDownload(item); item.cancelled = true; item.controller?.abort() })
  uploads.value.forEach(item => invalidateTransfer(item, true))
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
  persistTransfers()
})
</script>
