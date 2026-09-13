<template>
  <main class="share-shell upload-public-shell">
    <header class="share-topbar"><BrandLogo variant="main" compact link /><LanguageSelect /></header>
    <section class="share-content">
      <div v-if="loading" class="empty-state"><LoaderCircle :size="30" class="spin" /><span>{{ t('common.loading') }}</span></div>
      <section v-else-if="metaError" class="share-error"><XCircle :size="42" /><h1>{{ metaError }}</h1><button class="primary-button" @click="loadMeta"><RefreshCw :size="16" /> {{ t('share.retry') }}</button></section>
      <section v-else-if="passwordGate" class="share-card upload-public-card">
        <p class="eyebrow">{{ t('collection.upload') }}</p>
        <h1>{{ t('collection.passwordTitle') }}</h1>
        <p class="muted">{{ t('collection.passwordHint') }}</p>
        <form class="public-password-form" @submit.prevent="submitPassword">
          <label class="form-label">{{ t('collection.passwordPlaceholder') }}<input ref="passwordInput" v-model.trim="passwordInputValue" type="password" autocomplete="off" maxlength="72" :placeholder="t('collection.passwordPlaceholder')" /></label>
          <p v-if="passwordError" class="alert error" role="alert">{{ passwordError }}</p>
          <button class="primary-button submit-button" :disabled="passwordSubmitting || !passwordInputValue"><span>{{ passwordSubmitting ? t('common.loading') : t('collection.passwordSubmit') }}</span><Lock :size="17" /></button>
        </form>
        <BrandFooter />
      </section>
      <section v-else class="share-card upload-public-card">
        <p class="eyebrow">{{ t('collection.upload') }}</p>
        <h1>{{ meta.name }}</h1>
        <dl class="share-meta"><div><dt>{{ t('collection.expiresAt') }}</dt><dd>{{ formatDate(meta.expiresAt) }}</dd></div><div><dt>{{ t('collection.uploadCount') }}</dt><dd>{{ meta.uploadCount }} / {{ meta.maxUploads || t('collection.unlimited') }}</dd></div><div><dt>{{ t('collection.maxFileBytes') }}</dt><dd>{{ meta.maxFileBytes ? formatBytes(meta.maxFileBytes) : t('collection.unlimited') }}</dd></div><div><dt>{{ t('collection.status') }}</dt><dd>{{ statusLabel }}</dd></div></dl>
        <p v-if="!meta.uploadAllowed" class="alert error">{{ statusLabel }}</p>
        <form v-else class="public-upload-form" @submit.prevent="startQueue">
          <label class="form-label">{{ t('collection.remark') }}<input v-model.trim="remark" maxlength="2000" :placeholder="t('collection.remarkPlaceholder')" /></label>
          <div class="public-upload-actions"><div class="public-upload-buttons"><button type="button" class="secondary-button" @click="fileInput?.click()"><Upload :size="17" /> {{ t('collection.choose') }}</button><button type="button" class="secondary-button" @click="folderInput?.click()"><FolderUp :size="17" /> {{ t('collection.chooseFolder') }}</button></div><input ref="fileInput" type="file" multiple hidden @change="handleInput" /><input ref="folderInput" type="file" webkitdirectory directory multiple hidden @change="handleFolderInput" /></div>
          <div v-if="queue.length" class="public-queue">
            <template v-for="row in displayRows" :key="row.key">
              <div v-if="row.type === 'group'" class="public-queue-row public-queue-dir">
                <button type="button" class="icon-button dir-toggle" :aria-expanded="row.group.expanded" :aria-label="row.group.label" @click="toggleGroup(row.group)"><ChevronDown v-if="row.group.expanded" :size="16" /><ChevronRight v-else :size="16" /></button>
                <div class="transfer-main">
                  <div class="transfer-name"><strong :title="row.group.label">{{ row.group.label }}</strong><span>{{ row.doneCount }}/{{ row.group.ids.length }} · {{ formatBytes(row.size) }}</span></div>
                  <div class="progress-track"><span :style="{ width: row.progress + '%' }"></span></div>
                  <div v-if="row.failed" class="collection-queued-details"><span>{{ row.failReason }}</span></div>
                </div>
                <span class="transfer-percent">{{ row.failed ? '' : row.progress + '%' }}<small v-if="row.rate > 0">{{ formatBytes(row.rate) }}/s</small></span>
              </div>
              <div v-else class="public-queue-row">
                <button type="button" class="icon-button dir-toggle" :aria-expanded="row.item.showDetail" :aria-label="row.item.label" @click="row.item.showDetail = !row.item.showDetail"><ChevronDown v-if="row.item.showDetail" :size="14" /><ChevronRight v-else :size="14" /></button>
                <div class="transfer-main">
                  <div class="transfer-name"><strong :title="row.item.label">{{ row.item.label }}</strong><span>{{ row.item.status }}</span></div>
                  <div class="progress-track"><span :style="{ width: row.item.progress + '%' }"></span></div>
                  <div v-if="row.item.state === 'queued'" class="collection-queued-card" role="status" aria-live="polite"><div class="collection-queued-heading"><LoaderCircle :size="16" class="spin" /><strong>{{ t('collection.queued') }}</strong></div><div class="collection-queued-details"><span v-if="row.item.queuePosition !== null">{{ t('collection.queuePosition', { position: row.item.queuePosition }) }}</span><span>{{ t('collection.waitReasonLabel') }}: {{ queueWaitReasonLabel(row.item) }}</span></div></div>
                  <div v-if="row.item.showDetail" class="collection-queued-card" role="status" aria-live="polite"><div class="collection-queued-details"><span>{{ formatBytes(row.loaded) }} / {{ formatBytes(row.item.file.size) }}</span><span v-if="row.rate > 0">{{ formatBytes(row.rate) }}/s</span><span>{{ row.item.progress }}%</span><span v-if="row.item.hashEngine">{{ row.item.hashEngine }} · {{ row.item.hashRate }} MB/s</span></div><div v-if="row.item.failed" class="collection-queued-details"><span>{{ row.item.status }}</span></div></div>
                </div>
                <span class="transfer-percent">{{ row.item.failed ? '' : row.item.progress + '%' }}<small v-if="row.rate > 0">{{ formatBytes(row.rate) }}/s</small></span><button v-if="row.item.state !== 'completed' && row.item.state !== 'cancelled'" type="button" class="icon-button" :title="t('common.cancel')" :aria-label="t('common.cancel')" @click.stop="cancelUpload(row.item)"><X :size="15" /></button>
              </div>
            </template>
          </div>
          <p v-if="error" class="alert error">{{ error }}</p><p v-if="notice" class="alert success">{{ notice }}</p>
          <button class="primary-button submit-button" :disabled="running || !queue.length"><span>{{ running ? t('collection.uploading') : t('collection.upload') }}</span><UploadCloud :size="17" /></button>
        </form>
        <div v-if="completed.length" class="collection-completed"><h2>{{ t('collection.uploadedFiles') }}</h2><div v-for="item in completed" :key="item.id" class="completed-row"><CheckCircle2 :size="17" /><span>{{ item.name }}</span><small>{{ formatBytes(item.size) }}</small></div></div>
        <BrandFooter />
      </section>
    </section>
  </main>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { CheckCircle2, ChevronDown, ChevronRight, FolderUp, LoaderCircle, Lock, RefreshCw, Upload, UploadCloud, X, XCircle } from 'lucide-vue-next'
import { computeFileSHA256, localizeError } from '../api'
import { brand, loadBrand } from '../brand'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { currentLocale, loadLocale, t } from '../i18n'

const QUEUE_MIN_DELAY = 2000
const QUEUE_MAX_DELAY = 4000
const QUEUE_BASE_DELAY = 2500
const QUEUE_JITTER = 250

const route = useRoute()
const token = computed(() => String(route.params.token || ''))
const loading = ref(true); const metaError = ref(''); const error = ref(''); const notice = ref(''); const meta = ref({}); const remark = ref(''); const queue = ref([]); const completed = ref([]); const running = ref(false); const fileInput = ref(null); const folderInput = ref(null); const passwordInput = ref(null)
const groups = ref([])
let rateTimer = null
const passwordGate = ref(false); const passwordInputValue = ref(''); const passwordError = ref(''); const passwordSubmitting = ref(false)
const activePassword = ref('')
const mounted = ref(false); const viewGeneration = ref(0)
const statusLabel = computed(() => { const status = meta.value.status; if (status === 'expired') return t('collection.expired'); if (status === 'revoked') return t('collection.revoked'); if (status === 'limit_reached') return t('collection.limitReached'); return t('collection.active') })
const queueWaitReasonKeys = { quota_exceeded: 'collection.waitReason.quotaExceeded', collection_expired: 'collection.waitReason.collectionExpired', collection_revoked: 'collection.waitReason.collectionRevoked', collection_limit: 'collection.waitReason.collectionLimit' }

function passwordHeaders(extra = {}) {
  const headers = { ...(extra.headers || {}) }
  if (activePassword.value) headers['X-Collection-Password'] = activePassword.value
  return headers
}

// handlePasswordChallenge 统一处理后端 COLLECTION_UNAUTHORIZED：打开密码门禁、
// 停止全部上传活动并清除内存密码；若本次请求已带密码则给出“密码错误”提示。
function handlePasswordChallenge(err) {
  if (!err || err.data?.code !== 'COLLECTION_UNAUTHORIZED') return false
  const sentWithPassword = activePassword.value !== ''
  stopAllActivity()
  error.value = ''
  passwordGate.value = true
  if (sentWithPassword) {
    passwordError.value = t('collection.passwordWrong')
    activePassword.value = ''
    passwordInputValue.value = ''
  }
  return true
}

async function request(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...passwordHeaders(options) } })
  let body = null
  try { body = await response.json() } catch {}
  if (!response.ok) {
    const err = Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { data: body?.data, status: response.status })
    if (handlePasswordChallenge(err)) return Promise.reject(err)
    throw err
  }
  return body
}

// readFragmentPassword 仅在本公开收集上传页解析 #password=<URL-encoded>，
// 密码只进入内存状态，随后立即用 history.replaceState 移除 fragment，
// 绝不落入查询参数、localStorage 或再次出现在地址栏。
function readFragmentPassword() {
  try {
    const hash = window.location.hash || ''
    if (!hash.startsWith('#password=')) return
    const params = new URLSearchParams(hash.slice(1))
    const value = params.get('password')
    if (typeof value !== 'string' || value.length === 0 || value.length > 72) return
    activePassword.value = value
    history.replaceState(null, '', window.location.pathname + window.location.search)
  } catch { /* ignore malformed fragment */ }
}

async function submitPassword() {
  const value = passwordInputValue.value.trim()
  if (!value || passwordSubmitting.value) return
  passwordError.value = ''
  passwordSubmitting.value = true
  activePassword.value = value
  passwordGate.value = false
  try {
    await loadMeta()
    if (!passwordGate.value && !metaError.value) passwordInputValue.value = ''
  } finally {
    passwordSubmitting.value = false
    if (passwordGate.value && passwordInput.value) passwordInput.value.focus()
  }
}

async function loadMeta() {
  const generation = viewGeneration.value
  loading.value = true; metaError.value = ''
  try {
    const body = await request(`/api/collections/${encodeURIComponent(token.value)}/meta`)
    if (generation !== viewGeneration.value || !mounted.value) return
    meta.value = body.data
  } catch (err) {
    if (err?.data?.code === 'COLLECTION_UNAUTHORIZED') return
    if (generation === viewGeneration.value && mounted.value) metaError.value = err.message
  } finally {
    if (generation === viewGeneration.value) loading.value = false
  }
}

function createUploadItem(file, relPath = '') {
  const path = relPath || file.name
  const parts = path.split('/').filter(Boolean)
  const dirParts = parts.length > 1 ? parts.slice(0, -1) : []
  const dir = dirParts.join('/')
  const label = dir ? `${dir}/${file.name}` : file.name
  return { id: `${Date.now()}-${Math.random()}-${file.name}`, file, relPath: path !== file.name ? path : '', dir, label, progress: 0, status: t('collection.pending'), failed: false, state: 'pending', taskId: '', sha256: '', chunkSize: 0, totalChunks: 0, queuePosition: null, waitReason: '', pollTimer: null, pollController: null, pollFailures: 0, uploadController: null, running: false, cancelRequested: false, generation: viewGeneration.value, groupId: '', groupKey: '', rate: 0, rateSample: null, showDetail: false, hashEngine: '', hashRate: 0 }
}
function addFiles(files) {
  let added = 0
  for (const entry of files) {
    const file = entry.file || entry
    const relPath = entry.relPath || file.webkitRelativePath || ''
    const item = createUploadItem(file, relPath)
    queue.value.push(item)
    registerGroup(item)
    added += 1
  }
  if (!added && files.length > 0) notice.value = t('collection.folderEmpty')
}
function handleInput(event) { const picked = [...(event.target.files || [])].map(file => ({ file, relPath: file.webkitRelativePath || '' })); addFiles(picked); event.target.value = '' }
function handleFolderInput(event) {
  const files = [...(event.target.files || [])]
  if (!files.length) { notice.value = t('collection.folderEmpty'); event.target.value = ''; return }
  const picked = files.map(file => ({ file, relPath: file.webkitRelativePath || file.name }))
  addFiles(picked)
  event.target.value = ''
}
function safeQueuePosition(value) { const position = Number(value); return Number.isSafeInteger(position) && position > 0 ? position : null }
function queueWaitReasonLabel(item) { return t(queueWaitReasonKeys[item.waitReason] || 'collection.waitReason.unknown') }

// v030 #5/#8：目录上传在待传队列里聚合为一条目录条目（可展开出文件明细），每个文件行可展开
// 传输进度明细（已传/总量、实时速率、失败原因）。上传引擎仍按 queue 里的文件条目逐个执行，
// 目录条目只是展示层聚合，因此不影响既有的排队、轮询与失败重试语义。
// v030 #5/#8: directory uploads collapse into one queue row (expandable to its files), and each file
// row can expand a transfer detail block (bytes, live rate, failure reason). The upload engine still
// iterates the flat queue, so grouping is presentation-only and leaves queueing/retry semantics intact.
function registerGroup(item) {
  const key = String(item.dir || '').split('/').filter(Boolean)[0] || ''
  if (!key) return
  item.groupKey = key
  let group = groups.value.find(entry => entry.key === key)
  if (!group) { group = { key, label: key, ids: [], expanded: true }; groups.value.push(group) }
  if (!group.ids.includes(item.id)) group.ids.push(item.id)
}
function toggleGroup(group) { group.expanded = !group.expanded }
function itemsOfGroup(group) { return queue.value.filter(item => group.ids.includes(item.id)) }
function loadedOf(item) { return (Number(item.progress) || 0) / 100 * (item.file?.size || 0) }
function groupStats(group) {
  const items = itemsOfGroup(group)
  const size = items.reduce((sum, item) => sum + (item.file?.size || 0), 0)
  const loaded = items.reduce((sum, item) => sum + loadedOf(item), 0)
  const rate = items.reduce((sum, item) => sum + (Number(item.rate) || 0), 0)
  const doneCount = items.filter(item => item.state === 'completed' || item.progress >= 100).length
  const failure = items.find(item => item.failed)
  return { size, loaded, rate, doneCount, failed: Boolean(failure), failReason: failure?.status || '', progress: size ? Math.round(loaded / size * 100) : (items.length && doneCount === items.length ? 100 : 0) }
}
const displayRows = computed(() => {
  const rows = []
  const emitted = new Set()
  for (const item of queue.value) {
    if (item.groupKey) {
      const group = groups.value.find(entry => entry.key === item.groupKey)
      if (!group || emitted.has(group.key)) continue
      emitted.add(group.key)
      rows.push({ key: `group-${group.key}`, type: 'group', group, ...groupStats(group) })
      if (group.expanded) {
        for (const child of itemsOfGroup(group)) rows.push({ key: child.id, type: 'file', item: child, loaded: loadedOf(child), rate: Number(child.rate) || 0 })
      }
      continue
    }
    rows.push({ key: item.id, type: 'file', item, loaded: loadedOf(item), rate: Number(item.rate) || 0 })
  }
  return rows
})
// sampleQueueRates 每秒按已传字节差计算实时速率（EMA 平滑），与文件库的速率口径保持一致。
// sampleQueueRates derives a live rate from the per-second byte delta with EMA smoothing.
function sampleQueueRates() {
  const now = Date.now()
  for (const item of queue.value) {
    const loaded = loadedOf(item)
    const previous = item.rateSample
    if (previous && now > previous.at) {
      const elapsed = (now - previous.at) / 1000
      const delta = Math.max(0, loaded - previous.loaded)
      if (elapsed >= 0.2 && delta > 0) {
        const instant = delta / elapsed
        item.rate = item.rate ? item.rate * 0.6 + instant * 0.4 : instant
      } else if (delta === 0) {
        item.rate = 0
      }
    }
    item.rateSample = { at: now, loaded }
    if (item.failed || item.state === 'queued' || item.state === 'completed') item.rate = 0
  }
}
function isCurrent(item, generation = item.generation) { return mounted.value && generation === viewGeneration.value && queue.value.includes(item) }
function abortError() { const err = new Error('Upload stopped'); err.name = 'AbortError'; return err }
function throwIfStale(item, generation, signal) { if (signal?.aborted || !isCurrent(item, generation)) throw abortError() }

function stopPolling(item) {
  if (item.pollTimer !== null) { clearTimeout(item.pollTimer); item.pollTimer = null }
  item.polling = false
  if (item.pollController) { item.pollController.abort(); item.pollController = null }
}
function stopItemActivity(item) {
  stopPolling(item)
  if (item.uploadController) { item.uploadController.abort(); item.uploadController = null }
}
function stopAllActivity() { for (const item of queue.value) stopItemActivity(item) }
function queuePollDelay(failures) {
  const backoff = Math.min(3500, QUEUE_BASE_DELAY * Math.pow(1.4, Math.min(failures, 4)))
  return Math.min(QUEUE_MAX_DELAY, Math.max(QUEUE_MIN_DELAY, Math.round(backoff + (Math.random() * QUEUE_JITTER * 2 - QUEUE_JITTER))))
}
function scheduleQueuePoll(item) {
  if (!mounted.value || item.state !== 'queued' || item.pollTimer !== null || item.pollController !== null) return
  item.pollTimer = setTimeout(() => { item.pollTimer = null; pollQueueState(item) }, queuePollDelay(item.pollFailures))
}
function startQueuePolling(item) { item.polling = true; scheduleQueuePoll(item) }
function isRetryablePollError(err) { return err?.status === 429 || err?.status >= 500 || (!err?.status && err?.name !== 'AbortError') }

async function cancelServerTask(item, taskId, generation) {
  const body = await request('/api/collections/' + encodeURIComponent(token.value) + '/upload-task/' + encodeURIComponent(taskId), { method: 'DELETE' })
  if (!isCurrent(item, generation)) return body
  if (body.data?.state === 'complete') { markCompleted(item, body.data); return body }
  item.state = 'cancelled'
  item.failed = false
  item.running = false
  item.cancelRequested = false
  item.status = t('files.finishedCancelled')
  return body
}

async function cancelUpload(item) {
  if (!item || item.state === 'completed' || item.state === 'cancelled' || item.state === 'cancelling') return
  const generation = ++item.generation
  const taskId = item.taskId
  item.cancelRequested = true
  item.state = 'cancelling'
  item.running = false
  item.status = t('files.terminating')
  if (taskId) {
    stopItemActivity(item)
    try {
      await cancelServerTask(item, taskId, generation)
    } catch (err) {
      if (!isCurrent(item, generation)) return
      item.cancelRequested = false
      item.state = 'active'
      item.failed = true
      item.status = err.message || t('collection.queueFailed')
      error.value = item.status
    }
    return
  }
  if (item.pollController) { item.pollController.abort(); item.pollController = null }
  if (isCurrent(item, generation)) { item.state = 'cancelled'; item.running = false; item.status = t('files.finishedCancelled') }
}

function setItemFailure(item, err) {
  const message = err?.message || t('collection.queueFailed')
  item.failed = true; item.state = 'failed'; item.status = message; item.running = false
  if (isCurrent(item) && !passwordGate.value) error.value = err?.data?.code === 'COLLECTION_QUOTA_EXCEEDED' ? message : `${item.label || item.file.name}: ${message}`
}
function markCompleted(item, result) {
  if (!isCurrent(item) || item.state === 'completed') return
  item.progress = 100; item.state = 'completed'; item.failed = false; item.running = false; item.status = t('collection.completed')
  completed.value.push({ ...(result || {}), id: item.id, name: result?.name || item.label || item.file.name, size: result?.size ?? item.file.size, completedAt: result?.completedAt || result?.createdAt || new Date().toISOString() })
}

async function pollQueueState(item) {
  if (!isCurrent(item) || item.state !== 'queued') return
  const generation = item.generation
  const controller = new AbortController()
  item.pollController = controller
  try {
    const body = await request(`/api/collections/${encodeURIComponent(token.value)}/upload-queue/${encodeURIComponent(item.taskId)}`, { signal: controller.signal })
    throwIfStale(item, generation, controller.signal)
    const state = body.data || {}
    if (state.taskId && String(state.taskId) !== item.taskId) throw new Error(t('collection.queueFailed'))
    item.pollFailures = 0
    if (state.state === 'queued') {
      item.queuePosition = safeQueuePosition(state.queuePosition)
      item.waitReason = typeof state.waitReason === 'string' ? state.waitReason : ''
      item.status = t('collection.queued')
      scheduleQueuePoll(item)
      return
    }
    if (state.state === 'active') {
      stopPolling(item)
      item.state = 'active'; item.running = true; item.status = t('collection.uploading')
      const uploadController = new AbortController()
      item.uploadController = uploadController
      try {
        const result = await uploadTask(item, generation, uploadController.signal)
        if (isCurrent(item, generation)) { markCompleted(item, result); await loadMeta() }
      } catch (err) {
        if (err?.name !== 'AbortError' && isCurrent(item, generation)) setItemFailure(item, err)
      } finally {
        if (item.uploadController === uploadController) item.uploadController = null
      }
      return
    }
    throw new Error(t('collection.queueFailed'))
  } catch (err) {
    if (err?.name === 'AbortError' || controller.signal.aborted || !isCurrent(item, generation)) return
    if (isRetryablePollError(err)) { item.pollFailures += 1; scheduleQueuePoll(item); return }
    stopPolling(item)
    setItemFailure(item, err)
  } finally {
    if (item.pollController === controller) item.pollController = null
  }
}

async function uploadTask(item, generation, signal) {
  throwIfStale(item, generation, signal)
  item.status = t('collection.uploading')
  for (let index = 0; index < item.totalChunks; index++) {
    throwIfStale(item, generation, signal)
    const start = index * item.chunkSize
    const response = await fetch(`/api/collections/${encodeURIComponent(token.value)}/upload-chunk/${encodeURIComponent(item.taskId)}/${index}`, { method: 'PUT', headers: passwordHeaders(), body: item.file.slice(start, Math.min(item.file.size, start + item.chunkSize)), signal })
    let body = null
    try { body = await response.json() } catch {}
    if (!response.ok) {
      const err = Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { data: body?.data, status: response.status })
      if (handlePasswordChallenge(err)) throw err
      throw err
    }
    item.progress = Math.round(20 + ((index + 1) / item.totalChunks) * 75)
  }
  item.status = t('collection.checking')
  const completedResponse = await request(`/api/collections/${encodeURIComponent(token.value)}/upload-complete/${encodeURIComponent(item.taskId)}`, { method: 'POST', body: JSON.stringify({ sha256: item.sha256 }), signal })
  return completedResponse.data
}

async function uploadOne(item) {
  const generation = item.generation
  const uploadToken = token.value
  const controller = new AbortController()
  item.uploadController = controller; item.running = true; item.state = 'pending'
  try {
    item.status = t('collection.checksum')
    const sha256 = await computeFileSHA256(item.file, progress => { if (isCurrent(item, generation)) item.progress = Math.round(progress * 0.2) }, info => { if (isCurrent(item, generation)) { item.hashEngine = info.engine; item.hashRate = info.mbps } })
    throwIfStale(item, generation, controller.signal)
    item.sha256 = sha256
    // 超过客户端上限的文件跳过校验（sha256 为空）时，补齐进度前段，避免进度条长时间停在 0（v036）。
    // When the client hash is skipped (empty sha256) advance past the hash segment of the progress bar.
    if (!sha256 && isCurrent(item, generation)) item.progress = 20
    const init = await request(`/api/collections/${encodeURIComponent(uploadToken)}/upload-init`, { method: 'POST', body: JSON.stringify({ name: item.file.name, size: item.file.size, chunkSize: item.file.size <= 8 * 1024 * 1024 ? item.file.size : 4 * 1024 * 1024, sha256, mime: item.file.type, remark: remark.value, ...(item.dir ? { dir: item.dir } : {}) }), signal: controller.signal })
    if (init.data?.instant) {
       if (item.cancelRequested) { item.state = 'cancelled'; item.running = false; item.status = t('files.finishedCancelled'); return null }
       throwIfStale(item, generation, controller.signal)
       return init.data
     }
    const data = init.data || {}
    if (!data.taskId || !Number.isSafeInteger(Number(data.chunkSize)) || !Number.isSafeInteger(Number(data.totalChunks)) || Number(data.chunkSize) <= 0 || Number(data.totalChunks) <= 0) throw new Error(t('collection.queueFailed'))
    item.taskId = String(data.taskId); item.chunkSize = Number(data.chunkSize); item.totalChunks = Number(data.totalChunks); item.queuePosition = safeQueuePosition(data.queuePosition); item.waitReason = typeof data.waitReason === 'string' ? data.waitReason : ''
    if (item.cancelRequested) { await cancelServerTask(item, item.taskId, item.generation); return null }
     throwIfStale(item, generation, controller.signal)
     if (data.state === 'queued') {
      item.state = 'queued'; item.running = false; item.status = t('collection.queued'); startQueuePolling(item); return null
    }
    item.state = 'active'
    return await uploadTask(item, generation, controller.signal)
  } finally {
    if (item.uploadController === controller) item.uploadController = null
  }
}

async function startQueue() {
  if (running.value || !queue.value.length) return
  running.value = true; error.value = ''; notice.value = ''
  const generation = viewGeneration.value
  for (const item of [...queue.value]) {
    if (!isCurrent(item, generation) || item.progress === 100 || item.state === 'queued' || item.state === 'cancelled' || item.state === 'cancelling' || item.running) continue
    item.failed = false; item.taskId = ''; item.state = 'pending'
    try {
      const result = await uploadOne(item)
      if (result) markCompleted(item, result)
    } catch (err) {
      if (err?.name !== 'AbortError' && isCurrent(item, generation)) setItemFailure(item, err)
      if (passwordGate.value) break
    }
  }
  if (!isCurrent(queue.value[0], generation)) return
  running.value = false
  const hasActiveWork = queue.value.some(item => item.state === 'pending' || item.state === 'queued' || item.running)
  if (!error.value && !hasActiveWork) notice.value = t('collection.allCompleted')
  await loadMeta()
}

function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '-' }

watch(token, (next, previous) => {
  if (!mounted.value || next === previous) return
  viewGeneration.value += 1; stopAllActivity(); queue.value = []; groups.value = []; completed.value = []; error.value = ''; notice.value = ''; remark.value = ''
  activePassword.value = ''; passwordInputValue.value = ''; passwordError.value = ''; passwordGate.value = false; metaError.value = ''
  loadMeta()
})

onMounted(async () => {
  mounted.value = true
  rateTimer = setInterval(sampleQueueRates, 1000)
  await loadBrand(); await loadLocale(brand.defaultLang)
  readFragmentPassword()
  if (mounted.value) await loadMeta()
})
onBeforeUnmount(() => { mounted.value = false; viewGeneration.value += 1; stopAllActivity(); if (rateTimer) { clearInterval(rateTimer); rateTimer = null } })
</script>
