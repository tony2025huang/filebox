<template>
  <main class="app-shell">
    <AuthenticatedTopbar :user="user" section="logs" />
    <section class="content-wrap">
      <div class="page-heading"><div><h1>{{ t('logs.heading') }}</h1><p class="muted">{{ t('logs.copy') }}</p></div></div>
      <div class="toolbar logs-toolbar"><div class="search-box"><Search :size="17" /><input v-model="keywordInput" :placeholder="t('logs.searchPlaceholder')" @keyup.enter="applyFilters" /></div><select v-model="filters.action" :aria-label="t('logs.actionType')" :disabled="actionsLoading"><option value="">{{ actionsLoading ? t('logs.loadingActions') : t('logs.allActions') }}</option><optgroup :label="t('logs.actionType')"><option v-for="action in actionGroups.businessActions" :key="action" :value="action">{{ actionLabel(action) }}</option></optgroup><optgroup v-if="actionGroups.systemActions.length" :label="t('logs.systemGroup')"><option v-for="action in actionGroups.systemActions" :key="action" :value="action">{{ actionLabel(action) }}</option></optgroup></select><select v-model="filters.result" :aria-label="t('logs.result')"><option value="">{{ t('logs.allResults') }}</option><option value="success">{{ t('logs.success') }}</option><option value="failure">{{ t('logs.failure') }}</option></select><select v-if="user.role === 'admin'" v-model="filters.userId" :aria-label="t('logs.user')"><option value="">{{ t('logs.allUsers') }}</option><option v-for="item in users" :key="item.id" :value="String(item.id)">{{ item.username }}</option></select><div class="time-range-field"><button type="button" class="secondary-button" @click="toggleTimeRange"><CalendarClock :size="16" /> {{ t('logs.timeRange') }}</button><div v-if="timeRangeOpen" class="time-range-popover"><label>{{ t('logs.fromTime') }}<input v-model="timeDraft.from" type="datetime-local" :aria-label="t('logs.fromTime')" /></label><label>{{ t('logs.toTime') }}<input v-model="timeDraft.to" type="datetime-local" :aria-label="t('logs.toTime')" /></label><div class="time-range-actions"><button type="button" class="secondary-button" @click="applyTimeRange"><Check :size="14" /> {{ t('common.confirm') }}</button><button type="button" class="secondary-button" @click="clearTimeRange"><X :size="14" /> {{ t('common.clear') }}</button></div></div></div><button class="secondary-button" @click="applyFilters"><Search :size="16" /> {{ t('logs.filter') }}</button><button class="refresh-button" :title="t('logs.refresh')" @click="loadLogs"><RefreshCw :size="17" :class="{ spin: loading }" /></button><span class="result-count">{{ t('common.records', { count: total }) }}</span></div>
      <div v-if="error" class="alert error">{{ error }}</div><div v-if="notice" class="alert success">{{ notice }}</div>
      <div class="file-table-wrap log-table-wrap"><table class="file-table log-table"><thead><tr><th>{{ t('logs.time') }}</th><th>{{ t('logs.user') }}</th><th>{{ t('logs.actionType') }}</th><th>{{ t('logs.target') }}</th><th>{{ t('logs.sourceIP') }}</th><th>{{ t('logs.result') }}</th><th>{{ t('logs.reasonColumn') }}</th></tr></thead><tbody><tr v-for="entry in logs" :key="entry.id"><td>{{ formatDate(entry.createdAt) }}</td><td>{{ entry.username || '-' }}</td><td><span class="action-label">{{ actionLabel(entry.action) }}</span></td><td class="log-target">{{ entry.target || '-' }}</td><td>{{ entry.ip || '-' }}</td><td><span class="result-label" :class="entry.result">{{ entry.result === 'success' ? t('logs.success') : t('logs.failure') }}</span></td><td>{{ reasonLabel(entry.reason) }}</td></tr></tbody></table><div v-if="!loading && !logs.length" class="empty-state"><ScrollText :size="34" /><strong>{{ t('logs.empty') }}</strong><span>{{ t('logs.emptyCopy') }}</span></div><div v-if="loading" class="empty-state"><LoaderCircle :size="28" class="spin" /><span>{{ t('logs.loading') }}</span></div></div>
      <div v-if="total > pageSize" class="pagination"><button class="secondary-button" :disabled="page === 1" @click="gotoPage(page - 1)"><ChevronLeft :size="16" /> {{ t('common.previous') }}</button><button v-for="num in pageNumbers" :key="num" class="secondary-button page-number" :class="{ active: num === page }" @click="gotoPage(num)">{{ num }}</button><span>{{ t('common.page', { page }) }} / {{ totalPages }}</span><button class="secondary-button" :disabled="page >= totalPages" @click="gotoPage(page + 1)">{{ t('common.next') }} <ChevronRight :size="16" /></button><label class="page-size-label">{{ t('common.pageSize') }}<select v-model.number="pageSize" @change="changePageSize"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label><template v-if="totalPages > 7"><input v-model="pageInput" class="jump-input" type="number" min="1" :max="totalPages" :placeholder="t('common.pageInput')" @keyup.enter="jumpPage" /><button class="secondary-button" @click="jumpPage">{{ t('common.jump') }}</button></template></div>
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
import { actionLabel as logActionLabel, reasonLabel as logReasonLabel } from '../logLabels'
import { CalendarClock, Check, ChevronLeft, ChevronRight, LoaderCircle, RefreshCw, ScrollText, Search, X } from 'lucide-vue-next'

const user = ref(JSON.parse(localStorage.getItem('filebox_user') || '{}')); const logs = ref([]); const actions = ref([]); const actionsLoading = ref(true); const users = ref([]); const total = ref(0); const page = ref(1); const pageSize = ref(Number(localStorage.getItem('filebox_pagesize_logs')) || 20); const pageInput = ref(''); const loading = ref(false); const error = ref(''); const notice = ref(''); const keywordInput = ref(''); const timeRangeOpen = ref(false); const timeDraft = reactive({ from: '', to: '' }); const filters = reactive({ action: '', result: '', keyword: '', userId: '', from: '', to: '' })
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
// toggleTimeRange 打开时把当前生效时间同步进草稿；未点「确定」关闭不生效（v019 #1）。
// toggleTimeRange seeds the draft with the active values on open; closing without "Apply" has no effect (v019 #1).
function toggleTimeRange() { timeRangeOpen.value = !timeRangeOpen.value; if (timeRangeOpen.value) { timeDraft.from = filters.from; timeDraft.to = filters.to } }
// applyTimeRange 应用草稿时间并刷新；clearTimeRange 清空草稿与生效时间并刷新（v019 #1）。
// applyTimeRange commits the draft times and reloads; clearTimeRange clears draft and active times and reloads (v019 #1).
function applyTimeRange() { filters.from = timeDraft.from; filters.to = timeDraft.to; timeRangeOpen.value = false; page.value = 1; loadLogs() }
function clearTimeRange() { timeDraft.from = ''; timeDraft.to = ''; filters.from = ''; filters.to = ''; timeRangeOpen.value = false; page.value = 1; loadLogs() }
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
// 操作类型与原因标签统一由 ../logLabels 维护：未知码回退为"待翻译"提示，不再裸露英文码。
// Action and reason labels live in ../logLabels; unknown codes fall back to a marked placeholder.
function actionLabel(value) { return logActionLabel(value, t) }
function reasonLabel(value) { return logReasonLabel(value, t) }
// actionGroups 把动作分为业务与"系统配置"两组，供筛选下拉分组展示。
// actionGroups splits actions into business and "system configuration" groups for grouped filter options.
const actionGroups = computed(() => {
  const business = new Set(['login', 'register', 'upload', 'upload_init', 'upload_chunk', 'upload_collect', 'upload_collect_fail', 'download', 'delete', 'share', 'share_view', 'share_download', 'share_extend', 'share_increase', 'share_revoke', 'batch_share', 'share_group_extend', 'share_group_increase', 'share_group_update', 'collection_update'])
  const businessActions = actions.value.filter(action => business.has(action))
  const systemActions = actions.value.filter(action => !business.has(action))
  return { businessActions, systemActions }
})
function formatDate(value) { return value ? new Date(value).toLocaleString(currentLocale.value === 'en' ? 'en-US' : currentLocale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '-' }
onMounted(() => { loadActions(); loadAdminData(); loadLogs() })
</script>
