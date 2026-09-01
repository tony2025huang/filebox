<template>
  <header class="topbar">
    <div class="topbar-brand"><BrandLogo variant="main" compact link /><span class="slash">/</span><span class="section-name">{{ t(sectionKey) }}</span></div>
    <div class="topbar-actions">
      <slot name="actions" />
      <LanguageSelect :user="user" />
      <RouterLink to="/" class="icon-text-button"><FolderOpen :size="16" /> {{ t('nav.files') }}</RouterLink>
      <RouterLink to="/collections" class="icon-text-button"><UploadCloud :size="16" /> {{ t('nav.collections') }}</RouterLink>
      <RouterLink to="/shares" class="icon-text-button"><Share2 :size="16" /> {{ t('nav.shares') }}</RouterLink>
      <RouterLink to="/sync" class="icon-text-button"><RefreshCw :size="16" /> {{ t('nav.syncTasks') }}</RouterLink>
      <RouterLink to="/logs" class="icon-text-button"><ScrollText :size="16" /> {{ t('nav.logs') }}</RouterLink>
      <RouterLink v-if="user.role === 'admin'" to="/admin" class="icon-text-button"><Shield :size="16" /> {{ t('nav.system') }}</RouterLink>
      <button type="button" class="icon-button" :title="t('nav.changePassword')" @click="openChangePassword"><KeyRound :size="17" /></button>
      <button class="icon-button" :title="t('nav.logout')" @click="logout"><LogOut :size="18" /></button>
    </div>
    <div v-if="changePasswordOpen" class="modal-backdrop" @click.self="closeChangePassword"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ t('password.title') }}</h2></div><button type="button" class="icon-button" :title="t('common.close')" @click="closeChangePassword"><X :size="18" /></button></div><form @submit.prevent="submitChangePassword"><label class="form-label">{{ t('password.old') }}<input v-model="changePasswordForm.oldPassword" type="password" autocomplete="current-password" required /></label><label class="form-label">{{ t('password.new') }}<input v-model="changePasswordForm.newPassword" type="password" autocomplete="new-password" required /></label><label class="form-label">{{ t('password.confirm') }}<input v-model="changePasswordConfirm" type="password" autocomplete="new-password" required /></label><p class="muted">{{ t('password.policy', changePasswordPolicy) }}</p><p v-if="changePasswordError" class="alert error">{{ changePasswordError }}</p><div class="modal-actions"><button class="primary-button" :disabled="changePasswordLoading"><LoaderCircle v-if="changePasswordLoading" :size="16" class="spin" /><KeyRound v-else :size="16" /> {{ changePasswordLoading ? t('password.submitting') : t('password.submit') }}</button><button type="button" class="secondary-button" :disabled="changePasswordLoading" @click="closeChangePassword">{{ t('common.cancel') }}</button></div></form></section></div>
    <p v-if="changePasswordNotice" class="alert success" style="position: fixed; top: 70px; right: 20px; z-index: 11; margin: 0">{{ changePasswordNotice }}</p>
  </header>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, clearSession, saveSession } from '../api'
import BrandLogo from './BrandLogo.vue'
import LanguageSelect from './LanguageSelect.vue'
import { t } from '../i18n'
import { FolderOpen, KeyRound, LoaderCircle, LogOut, RefreshCw, ScrollText, Share2, Shield, UploadCloud, X } from 'lucide-vue-next'

// AuthenticatedTopbar 是登录后所有视图共享的顶栏（#12/13/14）：统一包含 文件/分享/同步/日志/收集/管理后台(admin)/语言/改密/退出，
// 各页差异通过 actions 插槽保留（如 FilesView 的传输按钮与角标）。
// AuthenticatedTopbar is the shared topbar for all authenticated views (#12/13/14): it unifies files/shares/sync/logs/
// collections/admin(admin-only)/language/change-password/logout links; per-page extras ride the actions slot.
const props = defineProps({
  user: { type: Object, default: () => ({}) },
  section: { type: String, default: 'files' }
})
const SECTION_NAV_KEYS = {
  admin: 'nav.system',
  sync: 'nav.sync',
  files: 'nav.files',
  logs: 'nav.logs',
  collections: 'nav.collections',
  shares: 'nav.shares'
}
const sectionKey = computed(() => {
  return SECTION_NAV_KEYS[props.section] || `nav.${props.section}`
})
const router = useRouter()
const changePasswordOpen = ref(false)
const changePasswordForm = reactive({ oldPassword: '', newPassword: '' })
const changePasswordConfirm = ref('')
const changePasswordError = ref('')
const changePasswordLoading = ref(false)
const changePasswordNotice = ref('')
const changePasswordPolicy = reactive({ passwordMinLength: 8, passwordComplexity: 3 })
const changePasswordPolicyLoaded = ref(false)

async function openChangePassword() {
  changePasswordOpen.value = true
  changePasswordError.value = ''
  changePasswordNotice.value = ''
  if (changePasswordPolicyLoaded.value) return
  try {
    Object.assign(changePasswordPolicy, (await api('/api/auth/password-policy')).data)
  } catch { /* keep defaults when the policy request is unavailable */
  } finally {
    changePasswordPolicyLoaded.value = true
  }
}

function closeChangePassword() {
  changePasswordOpen.value = false
  changePasswordForm.oldPassword = ''
  changePasswordForm.newPassword = ''
  changePasswordConfirm.value = ''
  changePasswordError.value = ''
}

async function submitChangePassword() {
  changePasswordError.value = ''
  if (changePasswordForm.newPassword !== changePasswordConfirm.value) {
    changePasswordError.value = t('password.mismatch')
    return
  }
  changePasswordLoading.value = true
  try {
    const body = await api('/api/auth/change-password', { method: 'POST', body: JSON.stringify(changePasswordForm) })
    saveSession(body)
    changePasswordNotice.value = t('notice.passwordChanged')
    closeChangePassword()
    window.setTimeout(() => { changePasswordNotice.value = '' }, 3000)
  } catch (err) {
    changePasswordError.value = err.message
  } finally {
    changePasswordLoading.value = false
  }
}

async function logout() { try { await api('/api/auth/logout', { method: 'POST' }) } finally { clearSession(); router.push('/login') } }
</script>
