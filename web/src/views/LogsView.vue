<template>
  <main class="app-shell">
    <AuthenticatedTopbar :user="user" section="logs" />
    <section class="content-wrap">
      <div class="page-heading"><div><p class="eyebrow">{{ t('logs.eyebrow') }}</p><h1>{{ t('logs.heading') }}</h1><p class="muted">{{ t('logs.copy') }}</p></div></div>
      <div class="toolbar logs-toolbar"><div class="search-box"><Search :size="17" /><input v-model="keywordInput" :placeholder="t('logs.searchPlaceholder')" @keyup.enter="applyFilters" /></div><select v-model="filters.action" :aria-label="t('logs.actionType')" :disabled="actionsLoading"><option value="">{{ actionsLoading ? t('logs.loadingActions') : t('logs.allActions') }}</option><optgroup :label="t('logs.actionType')"><option v-for="action in actionGroups.businessActions" :key="action" :value="action">{{ actionLabel(action) }}</option></optgroup><optgroup v-if="actionGroups.systemActions.length" :label="t('logs.systemGroup')"><option v-for="action in actionGroups.systemActions" :key="action" :value="action">{{ actionLabel(action) }}</option></optgroup></select><select v-model="filters.result" :aria-label="t('logs.result')"><option value="">{{ t('logs.allResults') }}</option><option value="success">{{ t('logs.success') }}</option><option value="failure">{{ t('logs.failure') }}</option></select><select v-if="user.role === 'admin'" v-model="filters.userId" :aria-label="t('logs.user')"><option value="">{{ t('logs.allUsers') }}</option><option v-for="item in users" :key="item.id" :value="String(item.id)">{{ item.username }}</option></select><div class="time-range-field"><button type="button" class="secondary-button" @click="timeRangeOpen = !timeRangeOpen"><CalendarClock :size="16" /> {{ t('logs.timeRange') }}</button><div v-if="timeRangeOpen" class="time-range-popover"><label>{{ t('logs.fromTime') }}<input v-model="filters.from" type="datetime-local" :aria-label="t('logs.fromTime')" /></label><label>{{ t('logs.toTime') }}<input v-model="filters.to" type="datetime-local" :aria-label="t('logs.toTime')" /></label></div></div><button class="secondary-button" @click="applyFilters"><Search :size="16" /> {{ t('logs.filter') }}</button><button class="refresh-button" :title="t('logs.refresh')" @click="loadLogs"><RefreshCw :size="17" :class="{ spin: loading }" /></button><span class="result-count">{{ t('common.records', { count: total }) }}</span></div>
      <div v-if="error" class="alert error">{{ error }}</div><div v-if="notice" class="alert success">{{ notice }}</div>
      <div class="file-table-wrap log-table-wrap"><table class="file-table log-table"><thead><tr><th>{{ t('logs.time') }}</th><th>{{ t('logs.user') }}</th><th>{{ t('logs.actionType') }}</th><th>{{ t('logs.target') }}</th><th>{{ t('logs.sourceIP') }}</th><th>{{ t('logs.result') }}</th><th>{{ t('logs.failureReason') }}</th></tr></thead><tbody><tr v-for="entry in logs" :key="entry.id"><td>{{ formatDate(entry.createdAt) }}</td><td>{{ entry.username || '-' }}</td><td><span class="action-label">{{ actionLabel(entry.action) }}</span></td><td class="log-target">{{ entry.target || '-' }}</td><td>{{ entry.ip || '-' }}</td><td><span class="result-label" :class="entry.result">{{ entry.result === 'success' ? t('logs.success') : t('logs.failure') }}</span></td><td>{{ reasonLabel(entry.reason) }}</td></tr></tbody></table><div v-if="!loading && !logs.length" class="empty-state"><ScrollText :size="34" /><strong>{{ t('logs.empty') }}</strong><span>{{ t('logs.emptyCopy') }}</span></div><div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('logs.loading') }}</span></div></div>
      <div v-if="total > pageSize.value" class="pagination"><button class="secondary-button" :disabled="page === 1" @click="gotoPage(page - 1)"><ChevronLeft :size="16" /> {{ t('common.previous') }}</button><button v-for="num in pageNumbers" :key="num" class="secondary-button page-number" :class="{ active: num === page }" @click="gotoPage(num)">{{ num }}</button><span>{{ t('common.page', { page }) }} / {{ totalPages }}</span><button class="secondary-button" :disabled="page >= totalPages" @click="gotoPage(page + 1)">{{ t('common.next') }} <ChevronRight :size="16" /></button><label class="page-size-label">{{ t('common.pageSize') }}<select v-model.number="pageSize" @change="changePageSize"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label><template v-if="totalPages > 7"><input v-model="pageInput" class="jump-input" type="number" min="1" :max="totalPages" :placeholder="t('common.pageInput')" @keyup.enter="jumpPage" /><button class="secondary-button" @click="jumpPage">{{ t('common.jump') }}</button></template></div>
      <BrandFooter />
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import AuthenticatedTopbar from '../components/AuthenticatedTopbar.vue'
import BrandFooter from '../components/BrandFooter.vue'
import { currentLocale, t } from '../i18n'
import { CalendarClock, ChevronLeft, ChevronRight, LoaderCircle, RefreshCw, ScrollText, Search } from 'lucide-vue-next'

const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}')); const logs = ref([]); const actions = ref([]); const actionsLoading = ref(true); const users = ref([]); const total = ref(0); const page = ref(1); const pageSize = ref(Number(localStorage.getItem('filebox_pagesize_logs')) || 20); const pageInput = ref(''); const loading = ref(false); const error = ref(''); const notice = ref(''); const keywordInput = ref(''); const timeRangeOpen = ref(false); const filters = reactive({ action: '', result: '', keyword: '', userId: '', from: '', to: '' })
async function loadLogs() { loading.value = true; error.value = ''; const query = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value), action: filters.action, result: filters.result, keyword: filters.keyword }); if (user.value.role === 'admin' && filters.userId) query.set('userId', filters.userId); if (filters.from) query.set('from', new Date(filters.from).toISOString()); if (filters.to) query.set('to', new Date(filters.to).toISOString()); try { const body = await api(`/api/logs?${query}`); logs.value = body.data.items; total.value = body.data.total } catch (err) { error.value = err.message } finally { loading.value = false } }
// loadActions 异步拉取筛选项：普通用户只取自己实际存在的动作类型（不展示无权分类），
// 加载完成前在筛选中显示"加载中"占位（问题 5/6）。
// loadActions asynchronously fetches filter options: regular users only see action types that
// actually exist in their logs, with a "loading" placeholder until the request settles.
async function loadActions() {
  actionsLoading.value = true
  try {
    const usedOnly = user.value.role !== 'admin' ? '?usedOnly=true' : ''
    actions.value = (await api(`/api/logs/actions${usedOnly}`)).data
  } catch (err) { error.value = err.message } finally { actionsLoading.value = false }
}
async function loadAdminData() { if (user.value.role !== 'admin') return; try { const usersBody = await api('/api/admin/users?page=1&pageSize=100'); users.value = usersBody.data.items } catch (err) { error.value = err.message } }
function applyFilters() { page.value = 1; filters.keyword = keywordInput.value.trim(); loadLogs() }
// pageNumbers 计算分页数字按钮（当前页前后各 2 页，含首末页）。
// pageNumbers computes the pagination number buttons (two around the current page plus first/last).
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const pageNumbers = computed(() => {
  const current = page.value
  const pages = new Set([1, totalPages.value, current - 2, current - 1, current, current + 1, current + 2])
  return [...pages].filter(p => p >= 1 && p <= totalPages.value).sort((a, b) => a - b)
})
function gotoPage(target) { if (target < 1 || target > totalPages.value || target === page.value) return; page.value = target; loadLogs() }
// changePageSize/jumpPage 支持每页条数与页码跳转（v018 #7）。
// changePageSize/jumpPage add per-page-size selection and page jumping (v018 #7).
function changePageSize() { page.value = 1; localStorage.setItem('filebox_pagesize_logs', String(pageSize.value)); loadLogs() }
function jumpPage() { const target = Number(pageInput.value); if (!target || target < 1 || target > totalPages.value) { pageInput.value = ''; return } page.value = target; pageInput.value = ''; loadLogs() }
function actionLabel(value) {
  const labels = {
    login: t('logs.login'), register: t('logs.register'), upload: t('logs.upload'), upload_init: t('logs.uploadInit'), upload_chunk: t('logs.uploadChunk'), download: t('logs.download'),
     share: t('logs.share'), share_view: t('logs.shareView'), share_download: t('logs.shareDownload'), share_extend: t('logs.shareExtend'), share_increase: t('logs.shareIncrease'), share_revoke: t('logs.shareRevoke'), batch_share: t('logs.batchShare'), share_group_extend: t('logs.shareGroupExtend'), share_group_increase: t('logs.shareGroupIncrease'),
    settings_update: t('logs.settingsUpdate'), brand_update: t('logs.brandUpdate'), language_update: t('logs.languageUpdate'),
    password_change: t('logs.passwordChange'), password_reset: t('logs.passwordReset'), user_create: t('logs.userCreate'),
    user_update: t('logs.userUpdate'), user_disabled: t('logs.userDisabled'), totp_update: t('logs.totpUpdate'),
    ip_acl_update: t('logs.ipAclUpdate'), folder_create: t('logs.folderCreate'), folder_list: t('logs.folderList'), collection: t('logs.collection'), upload_collect: t('logs.uploadCollect'), upload_collect_fail: t('logs.uploadCollectFail'), collection_update: t('logs.collectionUpdate'),
    folder_rename: t('logs.folderRename'), folder_delete: t('logs.folderDelete'), file_list: t('logs.fileList'),
    admin_stats: t('logs.adminStats'), log_list: t('logs.logList')
  }
  return labels[value] || value
}
// actionGroups 把动作分为业务与"系统配置"两组，供筛选下拉分组展示。
// actionGroups splits actions into business and "system configuration" groups for grouped filter options.
const actionGroups = computed(() => {
  const business = new Set(['login', 'register', 'upload', 'upload_init', 'upload_chunk', 'upload_collect', 'upload_collect_fail', 'download', 'delete', 'share', 'share_view', 'share_download', 'share_extend', 'share_increase', 'share_revoke', 'batch_share', 'share_group_extend', 'share_group_increase', 'collection_update'])
  const businessActions = actions.value.filter(action => business.has(action))
  const systemActions = actions.value.filter(action => !business.has(action))
  return { businessActions, systemActions }
})
 function reasonLabel(value) { const keys = { user_not_found: 'logReason.userNotFound', wrong_password: 'logReason.wrongPassword', user_disabled: 'logReason.userDisabled', locked: 'logReason.locked', ip_locked: 'logReason.ipLocked', totp_failed: 'logReason.totpFailed', not_found: 'logReason.notFound', content_not_found: 'logReason.contentNotFound', checksum_mismatch: 'logReason.checksumMismatch', save_failed: 'logReason.saveFailed', upload_failed: 'logReason.uploadFailed', invalid_name: 'logReason.invalidName', too_large: 'logReason.tooLarge', conflict: 'logReason.conflict', disk_full: 'logReason.diskFull', quota_exceeded: 'logReason.quotaExceeded', task_not_found: 'logReason.taskNotFound', invalid_index: 'logReason.invalidIndex', rate_limited: 'logReason.rateLimited', size_mismatch: 'logReason.sizeMismatch', invalid_dir: 'logReason.invalidDir', invalid_resolve: 'logReason.invalidResolve', prepare_failed: 'logReason.prepareFailed', conflict_check_failed: 'logReason.conflictCheckFailed', disk_check_failed: 'logReason.diskCheckFailed', task_create_failed: 'logReason.taskCreateFailed', invalid_chunk_size: 'logReason.invalidChunkSize', share_not_found: 'logReason.shareNotFound', share_expired: 'logReason.shareExpired', share_revoked: 'logReason.shareRevoked', share_limit: 'logReason.shareLimit', share_denied: 'logReason.shareDenied' }; return keys[value] ? t(keys[value]) : value || '-' }
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '-' }
onMounted(() => { loadActions(); loadAdminData(); loadLogs() })
</script>
