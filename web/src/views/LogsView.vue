<template>
  <main class="app-shell">
    <header class="topbar"><div class="topbar-brand"><BrandLogo variant="main" compact link /><span class="slash">/</span><span class="section-name">{{ t('nav.logs') }}</span></div><div class="topbar-actions"><LanguageSelect :user="user" /><RouterLink to="/" class="icon-text-button"><FolderOpen :size="16" /> {{ t('nav.files') }}</RouterLink><RouterLink v-if="user.role === 'admin'" to="/admin" class="icon-text-button"><Shield :size="16" /> {{ t('nav.admin') }}</RouterLink><RouterLink to="/change-password" class="icon-button" :title="t('nav.changePassword')"><KeyRound :size="17" /></RouterLink><button class="icon-button" :title="t('nav.logout')" @click="logout"><LogOut :size="18" /></button></div></header>
    <section class="content-wrap">
      <div class="page-heading"><div><p class="eyebrow">{{ t('logs.eyebrow') }}</p><h1>{{ t('logs.heading') }}</h1><p class="muted">{{ t('logs.copy') }}</p></div></div>
      <div class="toolbar logs-toolbar"><div class="search-box"><Search :size="17" /><input v-model="keywordInput" :placeholder="t('logs.searchPlaceholder')" @keyup.enter="applyFilters" /></div><select v-model="filters.action" :aria-label="t('logs.actionType')"><option value="">{{ t('logs.allActions') }}</option><optgroup :label="t('logs.actionType')"><option v-for="action in actionGroups.businessActions" :key="action" :value="action">{{ actionLabel(action) }}</option></optgroup><optgroup v-if="actionGroups.systemActions.length" :label="t('logs.systemGroup')"><option v-for="action in actionGroups.systemActions" :key="action" :value="action">{{ actionLabel(action) }}</option></optgroup></select><select v-model="filters.result" :aria-label="t('logs.result')"><option value="">{{ t('logs.allResults') }}</option><option value="success">{{ t('logs.success') }}</option><option value="failure">{{ t('logs.failure') }}</option></select><select v-if="user.role === 'admin'" v-model="filters.userId" :aria-label="t('logs.user')"><option value="">{{ t('logs.allUsers') }}</option><option v-for="item in users" :key="item.id" :value="String(item.id)">{{ item.username }}</option></select><button class="secondary-button" @click="applyFilters"><Search :size="16" /> {{ t('logs.filter') }}</button><button class="refresh-button" :title="t('logs.refresh')" @click="loadLogs"><RefreshCw :size="17" :class="{ spin: loading }" /></button><span class="result-count">{{ t('common.records', { count: total }) }}</span></div>
      <div v-if="error" class="alert error">{{ error }}</div><div v-if="notice" class="alert success">{{ notice }}</div>
      <div class="file-table-wrap log-table-wrap"><table class="file-table log-table"><thead><tr><th>{{ t('logs.time') }}</th><th>{{ t('logs.user') }}</th><th>{{ t('logs.actionType') }}</th><th>{{ t('logs.target') }}</th><th>{{ t('logs.sourceIP') }}</th><th>{{ t('logs.result') }}</th><th>{{ t('logs.failureReason') }}</th></tr></thead><tbody><tr v-for="entry in logs" :key="entry.id"><td>{{ formatDate(entry.createdAt) }}</td><td>{{ entry.username || '-' }}</td><td><span class="action-label">{{ actionLabel(entry.action) }}</span></td><td class="log-target">{{ entry.target || '-' }}</td><td>{{ entry.ip || '-' }}</td><td><span class="result-label" :class="entry.result">{{ entry.result === 'success' ? t('logs.success') : t('logs.failure') }}</span></td><td>{{ reasonLabel(entry.reason) }}</td></tr></tbody></table><div v-if="!loading && !logs.length" class="empty-state"><ScrollText :size="34" /><strong>{{ t('logs.empty') }}</strong><span>{{ t('logs.emptyCopy') }}</span></div><div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('logs.loading') }}</span></div></div>
      <div v-if="total > pageSize" class="pagination"><button class="secondary-button" :disabled="page === 1" @click="page--; loadLogs()"><ChevronLeft :size="16" /> {{ t('common.previous') }}</button><span>{{ t('common.page', { page }) }}</span><button class="secondary-button" :disabled="page * pageSize >= total" @click="page++; loadLogs()">{{ t('common.next') }} <ChevronRight :size="16" /></button></div>
      <BrandFooter />
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api, clearSession } from '../api'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { currentLocale, t } from '../i18n'
import { ChevronLeft, ChevronRight, FolderOpen, LoaderCircle, LogOut, RefreshCw, ScrollText, Search, KeyRound, Shield } from 'lucide-vue-next'

const router = useRouter(); const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}')); const logs = ref([]); const actions = ref([]); const users = ref([]); const total = ref(0); const page = ref(1); const pageSize = 20; const loading = ref(false); const error = ref(''); const notice = ref(''); const keywordInput = ref(''); const filters = reactive({ action: '', result: '', keyword: '', userId: '' })
async function loadLogs() { loading.value = true; error.value = ''; const query = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize), action: filters.action, result: filters.result, keyword: filters.keyword }); if (user.value.role === 'admin' && filters.userId) query.set('userId', filters.userId); try { const body = await api(`/api/logs?${query}`); logs.value = body.data.items; total.value = body.data.total } catch (err) { error.value = err.message } finally { loading.value = false } }
async function loadActions() { try { actions.value = (await api('/api/logs/actions')).data } catch (err) { error.value = err.message } }
async function loadAdminData() { if (user.value.role !== 'admin') return; try { const usersBody = await api('/api/admin/users?page=1&pageSize=100'); users.value = usersBody.data.items } catch (err) { error.value = err.message } }
function applyFilters() { page.value = 1; filters.keyword = keywordInput.value.trim(); loadLogs() }
async function logout() { try { await api('/api/auth/logout', { method: 'POST' }) } finally { clearSession(); router.push('/login') } }
function actionLabel(value) {
  const labels = {
    login: t('logs.login'), register: t('logs.register'), upload: t('logs.upload'), download: t('logs.download'),
    share: t('logs.share'), share_view: t('logs.shareView'), share_download: t('logs.shareDownload'),
    settings_update: t('logs.settingsUpdate'), brand_update: t('logs.brandUpdate'), language_update: t('logs.languageUpdate'),
    password_change: t('logs.passwordChange'), password_reset: t('logs.passwordReset'), user_create: t('logs.userCreate'),
    user_update: t('logs.userUpdate'), user_disabled: t('logs.userDisabled'), totp_update: t('logs.totpUpdate'),
    ip_acl_update: t('logs.ipAclUpdate'), folder_create: t('logs.folderCreate'), folder_list: t('logs.folderList'),
    folder_rename: t('logs.folderRename'), folder_delete: t('logs.folderDelete'), file_list: t('logs.fileList'),
    admin_stats: t('logs.adminStats'), log_list: t('logs.logList')
  }
  return labels[value] || value
}
// actionGroups 把动作分为业务与"系统配置"两组，供筛选下拉分组展示。
// actionGroups splits actions into business and "system configuration" groups for grouped filter options.
const actionGroups = computed(() => {
  const business = new Set(['login', 'register', 'upload', 'download', 'share', 'share_view', 'share_download'])
  const businessActions = actions.value.filter(action => business.has(action))
  const systemActions = actions.value.filter(action => !business.has(action))
  return { businessActions, systemActions }
})
function reasonLabel(value) { const keys = { user_not_found: 'logReason.userNotFound', wrong_password: 'logReason.wrongPassword', user_disabled: 'logReason.userDisabled', locked: 'logReason.locked', ip_locked: 'logReason.ipLocked', totp_failed: 'logReason.totpFailed', not_found: 'logReason.notFound', content_not_found: 'logReason.contentNotFound', checksum_mismatch: 'logReason.checksumMismatch', save_failed: 'logReason.saveFailed', upload_failed: 'logReason.uploadFailed' }; return keys[value] ? t(keys[value]) : value || '-' }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '-' }
onMounted(() => { loadActions(); loadAdminData(); loadLogs() })
</script>
