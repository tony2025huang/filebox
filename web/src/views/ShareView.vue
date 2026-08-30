<template>
  <main class="share-shell">
    <header class="share-topbar"><BrandLogo variant="main" compact link /><LanguageSelect /></header>
    <section class="share-content">
      <div v-if="loading" class="empty-state"><LoaderCircle :size="30" class="spin" /><span>{{ t('share.loading') }}</span></div>
      <section v-else-if="error" class="share-error"><XCircle :size="42" /><h1>{{ error }}</h1><div class="share-actions"><button class="primary-button" @click="loadMeta"><RefreshCw :size="16" /> {{ t('share.retry') }}</button><RouterLink to="/login" class="secondary-button"><ArrowLeft :size="16" /> {{ t('share.backLogin') }}</RouterLink></div></section>
      <section v-else class="share-card"><p class="eyebrow">{{ t('share.heading') }}</p><h1>{{ meta.fileName }}</h1><dl class="share-meta"><div><dt>{{ t('files.size') }}</dt><dd>{{ formatBytes(meta.fileSize) }}</dd></div><div><dt>{{ t('files.type') }}</dt><dd>{{ meta.mime }}</dd></div><div><dt>{{ t('share.owner') }}</dt><dd>{{ meta.createdBy }}</dd></div><div><dt>{{ t('share.expiresAt') }}</dt><dd>{{ formatDate(meta.expiresAt) }}</dd></div><div v-if="meta.maxDownloads"><dt>{{ t('share.availableDownloads', { count: Math.max(0, meta.maxDownloads - meta.downloadCount) }) }}</dt><dd>{{ t('share.downloads', { count: meta.downloadCount }) }}</dd></div></dl><p v-if="downloadExhausted" class="alert error">{{ t('share.limitReached') }}</p><p v-if="downloadError" class="alert error">{{ downloadError }}</p><button type="button" class="primary-button share-download" :disabled="downloadLoading" @click="downloadFile"><Download :size="17" /> {{ downloadLoading ? t('common.loading') : t('share.download') }}</button><div v-if="previewAllowed" class="share-preview"><h2>{{ t('share.preview') }}</h2><div v-if="previewLoading" class="empty-state"><LoaderCircle :size="26" class="spin" /></div><p v-else-if="previewError" class="alert error">{{ previewError }}</p><img v-else-if="previewKind === 'image'" class="preview-content preview-image" :src="previewUrl" :alt="meta.fileName" /><video v-else-if="previewKind === 'video'" class="preview-content" :src="previewUrl" controls></video><iframe v-else-if="previewKind === 'pdf'" class="preview-content preview-frame" :src="previewUrl" :title="meta.fileName"></iframe><pre v-else class="preview-text">{{ previewText }}</pre></div><BrandFooter /></section>
    </section>
  </main>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ArrowLeft, Download, LoaderCircle, RefreshCw, XCircle } from 'lucide-vue-next'
import { api, localizeError } from '../api'
import { brand, loadBrand } from '../brand'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { currentLocale, loadLocale, t } from '../i18n'

const route = useRoute(); const token = computed(() => route.params.token); const loading = ref(true); const error = ref(''); const meta = ref({}); const previewLoading = ref(false); const previewError = ref(''); const previewKind = ref(''); const previewUrl = ref(''); const previewText = ref(''); const downloadLoading = ref(false); const downloadError = ref('')
const downloadUrl = computed(() => `/api/files/shared/${encodeURIComponent(token.value)}/download`); const previewApiUrl = computed(() => `/api/files/shared/${encodeURIComponent(token.value)}/preview`); const previewAllowed = computed(() => canPreview(meta.value.mime)); const downloadExhausted = computed(() => meta.value.maxDownloads > 0 && (meta.value.downloadAvailable === false || meta.value.downloadCount >= meta.value.maxDownloads))

// loadMeta loads public share metadata and translates stable share errors for anonymous visitors.
// loadMeta 读取公开分享元数据，并为匿名访问者翻译稳定的分享错误。
async function loadMeta() { loading.value = true; error.value = ''; downloadError.value = ''; try { const body = await api(`/api/files/shared/${encodeURIComponent(token.value)}/meta`); meta.value = body.data; if (previewAllowed.value) await loadPreview() } catch (err) { error.value = err.message } finally { loading.value = false } }
// downloadFile 先用 fetch 获取响应，确保 JSON 错误能显示在分享页，而不是变成浏览器下载权限错误。
// downloadFile fetches the response so JSON errors remain visible on the share page instead of becoming browser download errors.
async function downloadFile() {
  if (downloadLoading.value) return
  if (downloadExhausted.value) { downloadError.value = t('share.limitReached'); return }
  downloadLoading.value = true
  downloadError.value = ''
  try {
    const response = await fetch(downloadUrl.value)
    let body = null
    if (!response.ok) {
      try { body = await response.json() } catch {}
      const mapped = new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message }))
      mapped.data = body?.data || {}
      throw mapped
    }
    const blob = await response.blob()
    const objectUrl = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectUrl
    link.download = meta.value.fileName || 'download'
    document.body.appendChild(link)
    link.click()
    link.remove()
    setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
  } catch (err) {
    if (err.data?.code === 'SHARE_DOWNLOAD_LIMIT') {
      meta.value.downloadAvailable = false
      downloadError.value = t('share.limitReached')
    } else {
      downloadError.value = err instanceof TypeError ? t('error.network') : (err.message || t('error.downloadFailed'))
    }
  } finally {
    downloadLoading.value = false
  }
}
async function loadPreview() {
  previewLoading.value = true
  previewError.value = ''
  previewKind.value = previewType(meta.value.mime)
  try {
    const response = await fetch(previewApiUrl.value)
    if (!response.ok) {
      let body = null
      try { body = await response.json() } catch {}
      throw new Error(localizeError({ status: response.status, data: body?.data, backendMessage: body?.message }))
    }
    if (previewKind.value === 'text') {
      previewText.value = await response.text()
    } else {
      previewUrl.value = URL.createObjectURL(await response.blob())
    }
  } catch (err) {
    previewError.value = err.message
  } finally {
    previewLoading.value = false
  }
}
function canPreview(mime = '') { const value = mime.toLowerCase().split(';')[0]; return new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/svg+xml', 'text/plain', 'text/markdown', 'text/csv', 'text/x-log', 'text/html', 'application/json', 'application/pdf', 'video/mp4', 'video/webm']).has(value) }
function previewType(mime = '') { const value = mime.toLowerCase().split(';')[0]; if (value.startsWith('image/')) return 'image'; if (value.startsWith('video/')) return 'video'; if (value === 'application/pdf') return 'pdf'; return 'text' }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '-' }
onMounted(async () => { await loadBrand(); await loadLocale(brand.defaultLang); loadMeta() })
onBeforeUnmount(() => { if (previewUrl.value) URL.revokeObjectURL(previewUrl.value) })
</script>
