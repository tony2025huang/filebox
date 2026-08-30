<template>
  <main class="share-shell">
    <header class="share-topbar"><BrandLogo variant="main" compact /><LanguageSelect /></header>
    <section class="share-content">
      <div v-if="loading" class="empty-state"><LoaderCircle :size="30" class="spin" /><span>{{ t('share.loading') }}</span></div>
      <section v-else-if="error" class="share-error"><XCircle :size="42" /><h1>{{ error }}</h1><div class="share-actions"><button class="primary-button" @click="loadMeta"><RefreshCw :size="16" /> {{ t('share.retry') }}</button><RouterLink to="/login" class="secondary-button"><ArrowLeft :size="16" /> {{ t('share.backLogin') }}</RouterLink></div></section>
      <section v-else class="share-card"><p class="eyebrow">{{ t('share.heading') }}</p><h1>{{ meta.fileName }}</h1><dl class="share-meta"><div><dt>{{ t('files.size') }}</dt><dd>{{ formatBytes(meta.fileSize) }}</dd></div><div><dt>{{ t('files.type') }}</dt><dd>{{ meta.mime }}</dd></div><div><dt>{{ t('share.owner') }}</dt><dd>{{ meta.createdBy }}</dd></div><div><dt>{{ t('share.expiresAt') }}</dt><dd>{{ formatDate(meta.expiresAt) }}</dd></div><div v-if="meta.maxDownloads"><dt>{{ t('share.availableDownloads', { count: Math.max(0, meta.maxDownloads - meta.downloadCount) }) }}</dt><dd>{{ t('share.downloads', { count: meta.downloadCount }) }}</dd></div></dl><a class="primary-button share-download" :href="downloadUrl" download><Download :size="17" /> {{ t('share.download') }}</a><div v-if="previewAllowed" class="share-preview"><h2>{{ t('share.preview') }}</h2><div v-if="previewLoading" class="empty-state"><LoaderCircle :size="26" class="spin" /></div><p v-else-if="previewError" class="alert error">{{ previewError }}</p><img v-else-if="previewKind === 'image'" class="preview-content preview-image" :src="previewUrl" :alt="meta.fileName" /><video v-else-if="previewKind === 'video'" class="preview-content" :src="previewUrl" controls></video><iframe v-else-if="previewKind === 'pdf'" class="preview-content preview-frame" :src="previewUrl" :title="meta.fileName"></iframe><pre v-else class="preview-text">{{ previewText }}</pre></div><BrandFooter /></section>
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

const route = useRoute(); const token = computed(() => route.params.token); const loading = ref(true); const error = ref(''); const meta = ref({}); const previewLoading = ref(false); const previewError = ref(''); const previewKind = ref(''); const previewUrl = ref(''); const previewText = ref('')
const downloadUrl = computed(() => `/api/files/shared/${encodeURIComponent(token.value)}/download`); const previewAllowed = computed(() => canPreview(meta.value.mime))

// loadMeta loads public share metadata and translates stable share errors for anonymous visitors.
// loadMeta 读取公开分享元数据，并为匿名访问者翻译稳定的分享错误。
async function loadMeta() { loading.value = true; error.value = ''; try { const body = await api(`/api/files/shared/${encodeURIComponent(token.value)}/meta`); meta.value = body.data; if (previewAllowed.value) await loadPreview() } catch (err) { error.value = err.message } finally { loading.value = false } }
async function loadPreview() { previewLoading.value = true; previewError.value = ''; previewKind.value = previewType(meta.value.mime); try { if (previewKind.value === 'text') { const response = await fetch(downloadUrl.value); if (!response.ok) throw new Error(localizeError({ status: response.status })); previewText.value = await response.text() } else { const response = await fetch(downloadUrl.value); if (!response.ok) throw new Error(localizeError({ status: response.status })); previewUrl.value = URL.createObjectURL(await response.blob()) } } catch (err) { previewError.value = err.message } finally { previewLoading.value = false } }
function canPreview(mime = '') { const value = mime.toLowerCase().split(';')[0]; return new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/svg+xml', 'text/plain', 'text/markdown', 'text/csv', 'text/x-log', 'text/html', 'application/json', 'application/pdf', 'video/mp4', 'video/webm']).has(value) }
function previewType(mime = '') { const value = mime.toLowerCase().split(';')[0]; if (value.startsWith('image/')) return 'image'; if (value.startsWith('video/')) return 'video'; if (value === 'application/pdf') return 'pdf'; return 'text' }
function formatBytes(bytes = 0) { if (bytes < 1024) return `${bytes} B`; const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes; let unit = -1; do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1); return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}` }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '-' }
onMounted(async () => { await loadBrand(); await loadLocale(brand.defaultLang); loadMeta() })
onBeforeUnmount(() => { if (previewUrl.value) URL.revokeObjectURL(previewUrl.value) })
</script>
