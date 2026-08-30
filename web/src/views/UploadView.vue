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
          <div v-if="queue.length" class="public-queue"><div v-for="item in queue" :key="item.id" class="public-queue-row"><div class="transfer-main"><div class="transfer-name"><strong :title="item.file.name">{{ item.file.name }}</strong><span>{{ item.status }}</span></div><div class="progress-track"><span :style="{ width: item.progress + '%' }"></span></div></div><span class="transfer-percent">{{ item.failed ? '' : item.progress + '%' }}</span></div></div>
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
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { CheckCircle2, LoaderCircle, RefreshCw, Upload, UploadCloud, XCircle } from 'lucide-vue-next'
import { computeFileSHA256, localizeError } from '../api'
import { brand, loadBrand } from '../brand'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { currentLocale, loadLocale, t } from '../i18n'

const route = useRoute()
const token = computed(() => String(route.params.token || ''))
const loading = ref(true); const metaError = ref(''); const error = ref(''); const notice = ref(''); const meta = ref({}); const remark = ref(''); const queue = ref([]); const completed = ref([]); const dragging = ref(false); const running = ref(false); const fileInput = ref(null)
const statusLabel = computed(() => { const status = meta.value.status; if (status === 'expired') return t('collection.expired'); if (status === 'revoked') return t('collection.revoked'); if (status === 'limit_reached') return t('collection.limitReached'); return t('collection.active') })

async function request(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } })
  let body = null
  try { body = await response.json() } catch {}
  if (!response.ok) throw Object.assign(new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })), { data: body?.data })
  return body
}
async function loadMeta() { loading.value = true; metaError.value = ''; try { const body = await request(`/api/collections/${encodeURIComponent(token.value)}/meta`); meta.value = body.data } catch (err) { metaError.value = err.message } finally { loading.value = false } }
function addFiles(files) { for (const file of files) queue.value.push({ id: `${Date.now()}-${Math.random()}-${file.name}`, file, progress: 0, status: t('collection.pending'), failed: false }) }
function handleInput(event) { addFiles(event.target.files || []); event.target.value = '' }
function handleDrop(event) { dragging.value = false; addFiles(event.dataTransfer.files || []) }
async function startQueue() { if (running.value || !queue.value.length) return; running.value = true; error.value = ''; notice.value = ''; for (const item of queue.value) { if (item.progress === 100) continue; item.failed = false; try { const result = await uploadOne(item); item.progress = 100; item.status = t('collection.completed'); completed.value.push(result || { name: item.file.name, size: item.file.size }) } catch (err) { item.failed = true; item.status = err.message; error.value = err?.data?.code === 'COLLECTION_QUOTA_EXCEEDED' ? err.message : `${item.file.name}: ${err.message}` } } running.value = false; if (!error.value) notice.value = t('collection.allCompleted'); await loadMeta() }
async function uploadOne(item) {
  item.status = t('collection.checksum'); const sha256 = await computeFileSHA256(item.file, progress => { item.progress = Math.round(progress * 0.2) }); const init = await request(`/api/collections/${encodeURIComponent(token.value)}/upload-init`, { method: 'POST', body: JSON.stringify({ name: item.file.name, size: item.file.size, chunkSize: item.file.size <= 8 * 1024 * 1024 ? item.file.size : 4 * 1024 * 1024, sha256, mime: item.file.type, remark: remark.value }) }); if (init.data?.instant) return init.data.file; const { taskId, chunkSize, totalChunks } = init.data; item.status = t('collection.uploading'); for (let index = 0; index < totalChunks; index++) { const start = index * chunkSize; const response = await fetch(`/api/collections/${encodeURIComponent(token.value)}/upload-chunk/${encodeURIComponent(taskId)}/${index}`, { method: 'PUT', body: item.file.slice(start, Math.min(item.file.size, start + chunkSize)) }); if (!response.ok) { let body = null; try { body = await response.json() } catch {}; throw new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message })) } item.progress = Math.round(20 + ((index + 1) / totalChunks) * 75) } item.status = t('collection.checking'); const completedResponse = await request(`/api/collections/${encodeURIComponent(token.value)}/upload-complete/${encodeURIComponent(taskId)}`, { method: 'POST', body: JSON.stringify({ sha256 }) }); return completedResponse.data }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '-' }
onMounted(async () => { await loadBrand(); await loadLocale(brand.defaultLang); await loadMeta() })
</script>
