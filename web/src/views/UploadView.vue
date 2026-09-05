<template>
  <main class="share-shell upload-public-shell">
    <header class="share-topbar"><BrandLogo variant="main" compact link /><LanguageSelect /></header>
    <section class="share-content">
      <div v-if="loading" class="empty-state"><LoaderCircle :size="30" class="spin" /><span>{{ t('common.loading') }}</span></div>
      <section v-else-if="metaError" class="share-error"><XCircle :size="42" /><h1>{{ metaError }}</h1><button class="primary-button" @click="loadMeta"><RefreshCw :size="16" /> {{ t('share.retry') }}</button></section>
      <section v-else class="share-card upload-public-card">
        <p class="eyebrow">{{ t('collection.upload') }}</p>
        <h1>{{ meta.name }}</h1>
        <dl class="share-meta"><div><dt>{{ t('collection.expiresAt') }}</dt><dd>{{ formatDate(meta.expiresAt) }}</dd></div><div><dt>{{ t('collection.uploadCount') }}</dt><dd>{{ meta.uploadCount }} / {{ meta.maxUploads || t('collection.unlimited') }}</dd></div><div><dt>{{ t('collection.maxFileBytes') }}</dt><dd>{{ meta.maxFileBytes ? formatBytes(meta.maxFileBytes) : t('collection.unlimited') }}</dd></div><div><dt>{{ t('collection.status') }}</dt><dd>{{ statusLabel }}</dd></div></dl>
        <p v-if="!meta.uploadAllowed" class="alert error">{{ statusLabel }}</p>
        <form v-else class="public-upload-form" @submit.prevent="startQueue">
          <label class="form-label">{{ t('collection.remark') }}<input v-model.trim="remark" maxlength="2000" :placeholder="t('collection.remarkPlaceholder')" /></label>
          <div class="public-drop-zone" :class="{ dragging }" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="handleDrop"><UploadCloud :size="30" /><strong>{{ t('collection.drop') }}</strong><span>{{ t('collection.uploadCopy') }}</span><button type="button" class="secondary-button" @click="fileInput?.click()"><Upload :size="17" /> {{ t('collection.choose') }}</button><input ref="fileInput" type="file" multiple hidden @change="handleInput" /></div>
          <div v-if="queue.length" class="public-queue"><div v-for="item in queue" :key="item.id" class="public-queue-row"><div class="transfer-main"><div class="transfer-name"><strong :title="item.file.name">{{ item.file.name }}</strong><span>{{ item.status }}</span></div><div class="progress-track"><span :style="{ width: item.progress + '%' }"></span></div><div v-if="item.state === 'queued'" class="collection-queued-card" role="status" aria-live="polite"><div class="collection-queued-heading"><LoaderCircle :size="16" class="spin" /><strong>{{ t('collection.queued') }}</strong></div><div class="collection-queued-details"><span v-if="item.queuePosition !== null">{{ t('collection.queuePosition', { position: item.queuePosition }) }}</span><span>{{ t('collection.waitReasonLabel') }}: {{ queueWaitReasonLabel(item) }}</span></div></div></div><span class="transfer-percent">{{ item.failed ? '' : item.progress + '%' }}</span></div></div>
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
import { CheckCircle2, LoaderCircle, RefreshCw, Upload, UploadCloud, XCircle } from 'lucide-vue-next'
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
const loading = ref(true); const metaError = ref(''); const error = ref(''); const notice = ref(''); const meta = ref({}); const remark = ref(''); const queue = ref([]); const completed = ref([]); const dragging = ref(false); const running = ref(false); const fileInput = ref(null)
const mounted = ref(false); const viewGeneration = ref(0)
const statusLabel = computed(() => { const status = meta.value.status; if (status === 'expired') return t('collection.expired'); if (status === 'revoked') return t('collection.revoked'); if (status === 'limit_reached') return t('collection.limitReached'); return t('collection.active') })
const queueWaitReasonKeys = { quota_exceeded: 'collection.waitReason.quotaExceeded', collection_expired: 'collection.waitReason.collectionExpired', collection_revoked: 'collection.waitReason.collectionRevoked', collection_limit: 'collection.waitReason.collectionLimit' }

async function request(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } })
  let body = null
  try { body = await response.json() } catch {}
  if (!response.ok) throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { data: body?.data, status: response.status })
  return body
}

async function loadMeta() {
  const generation = viewGeneration.value
  loading.value = true; metaError.value = ''
  try {
    const body = await request(`/api/collections/${encodeURIComponent(token.value)}/meta`)
    if (generation !== viewGeneration.value || !mounted.value) return
    meta.value = body.data
  } catch (err) {
    if (generation === viewGeneration.value && mounted.value) metaError.value = err.message
  } finally {
    if (generation === viewGeneration.value) loading.value = false
  }
}

function createUploadItem(file) {
  return { id: `${Date.now()}-${Math.random()}-${file.name}`, file, progress: 0, status: t('collection.pending'), failed: false, state: 'pending', taskId: '', sha256: '', chunkSize: 0, totalChunks: 0, queuePosition: null, waitReason: '', pollTimer: null, pollController: null, pollFailures: 0, uploadController: null, running: false, generation: viewGeneration.value }
}
function addFiles(files) { for (const file of files) queue.value.push(createUploadItem(file)) }
function handleInput(event) { addFiles(event.target.files || []); event.target.value = '' }
function handleDrop(event) { dragging.value = false; addFiles(event.dataTransfer.files || []) }
function safeQueuePosition(value) { const position = Number(value); return Number.isSafeInteger(position) && position > 0 ? position : null }
function queueWaitReasonLabel(item) { return t(queueWaitReasonKeys[item.waitReason] || 'collection.waitReason.unknown') }
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

function setItemFailure(item, err) {
  const message = err?.message || t('collection.queueFailed')
  item.failed = true; item.state = 'failed'; item.status = message; item.running = false
  if (isCurrent(item)) error.value = err?.data?.code === 'COLLECTION_QUOTA_EXCEEDED' ? message : `${item.file.name}: ${message}`
}
function markCompleted(item, result) {
  if (!isCurrent(item) || item.state === 'completed') return
  item.progress = 100; item.state = 'completed'; item.failed = false; item.running = false; item.status = t('collection.completed')
  completed.value.push({ ...(result || {}), id: item.id, name: result?.name || item.file.name, size: result?.size ?? item.file.size })
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
    const response = await fetch(`/api/collections/${encodeURIComponent(token.value)}/upload-chunk/${encodeURIComponent(item.taskId)}/${index}`, { method: 'PUT', body: item.file.slice(start, Math.min(item.file.size, start + item.chunkSize)), signal })
    let body = null
    try { body = await response.json() } catch {}
    if (!response.ok) throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { data: body?.data, status: response.status })
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
    const sha256 = await computeFileSHA256(item.file, progress => { if (isCurrent(item, generation)) item.progress = Math.round(progress * 0.2) })
    throwIfStale(item, generation, controller.signal)
    item.sha256 = sha256
    const init = await request(`/api/collections/${encodeURIComponent(uploadToken)}/upload-init`, { method: 'POST', body: JSON.stringify({ name: item.file.name, size: item.file.size, chunkSize: item.file.size <= 8 * 1024 * 1024 ? item.file.size : 4 * 1024 * 1024, sha256, mime: item.file.type, remark: remark.value }), signal: controller.signal })
    if (init.data?.instant) return init.data.file
    const data = init.data || {}
    if (!data.taskId || !Number.isSafeInteger(Number(data.chunkSize)) || !Number.isSafeInteger(Number(data.totalChunks)) || Number(data.chunkSize) <= 0 || Number(data.totalChunks) <= 0) throw new Error(t('collection.queueFailed'))
    item.taskId = String(data.taskId); item.chunkSize = Number(data.chunkSize); item.totalChunks = Number(data.totalChunks); item.queuePosition = safeQueuePosition(data.queuePosition); item.waitReason = typeof data.waitReason === 'string' ? data.waitReason : ''
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
    if (!isCurrent(item, generation) || item.progress === 100 || item.state === 'queued' || item.running) continue
    item.failed = false; item.taskId = ''; item.state = 'pending'
    try {
      const result = await uploadOne(item)
      if (result) markCompleted(item, result)
    } catch (err) {
      if (err?.name !== 'AbortError' && isCurrent(item, generation)) setItemFailure(item, err)
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
  viewGeneration.value += 1; stopAllActivity(); queue.value = []; completed.value = []; error.value = ''; notice.value = ''; remark.value = ''; loadMeta()
})

onMounted(async () => {
  mounted.value = true
  await loadBrand(); await loadLocale(brand.defaultLang)
  if (mounted.value) await loadMeta()
})
onBeforeUnmount(() => { mounted.value = false; viewGeneration.value += 1; stopAllActivity() })
</script>
