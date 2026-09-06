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
    <button ref="mobileMenuButton" type="button" class="mobile-menu-toggle icon-button" :aria-label="t('nav.openMenu')" :aria-expanded="mobileMenuOpen ? 'true' : 'false'" aria-controls="mobile-nav-panel" @click="toggleMobileMenu"><Menu :size="20" /></button>
    <template v-if="mobileMenuOpen">
      <div class="mobile-nav-backdrop" @click="closeMobileMenu"></div>
      <nav id="mobile-nav-panel" class="mobile-nav-panel" role="dialog" aria-modal="true" :aria-label="t('nav.menu')">
        <div class="mobile-nav-head"><span class="mobile-nav-heading">{{ t(sectionKey) }}</span><button ref="mobileCloseButton" type="button" class="icon-button" :aria-label="t('nav.closeMenu')" :title="t('nav.closeMenu')" @click="closeMobileMenu"><X :size="18" /></button></div>
        <div class="mobile-nav-body">
          <div v-if="hasActionsSlot" class="mobile-nav-slot" @click="closeMobileMenu"><slot name="actions" /></div>
          <RouterLink v-for="item in mobileNav" :key="item.key" :to="item.to" class="mobile-nav-link" :class="{ active: activeMobileKey === item.key }" @click="closeMobileMenu"><component :is="mobileNavIcon(item.key)" :size="17" />{{ t(item.labelKey) }}</RouterLink>
          <div class="mobile-nav-divider"></div>
          <div class="mobile-nav-account">
            <div class="mobile-nav-lang"><LanguageSelect :user="user" /></div>
            <button type="button" class="mobile-nav-tool" @click="menuPassword"><KeyRound :size="16" /> {{ t('nav.changePassword') }}</button>
            <button type="button" class="mobile-nav-tool danger" @click="menuLogout"><LogOut :size="16" /> {{ t('nav.logout') }}</button>
          </div>
        </div>
      </nav>
    </template>
    <div v-if="changePasswordOpen" class="modal-backdrop" @click.self="closeChangePassword"><section class="modal-panel" role="dialog" aria-modal="true"><div class="panel-heading"><div><h2>{{ t('password.title') }}</h2></div><button type="button" class="icon-button" :title="t('common.close')" @click="closeChangePassword"><X :size="18" /></button></div><form @submit.prevent="submitChangePassword"><label class="form-label">{{ t('password.old') }}<input v-model="changePasswordForm.oldPassword" type="password" autocomplete="current-password" required /></label><label class="form-label">{{ t('password.new') }}<input v-model="changePasswordForm.newPassword" type="password" autocomplete="new-password" required /></label><label class="form-label">{{ t('password.confirm') }}<input v-model="changePasswordConfirm" type="password" autocomplete="new-password" required /></label><p class="muted">{{ t('password.policy', changePasswordPolicy) }}</p><p v-if="changePasswordError" class="alert error">{{ changePasswordError }}</p><div class="modal-actions"><button class="primary-button" :disabled="changePasswordLoading"><LoaderCircle v-if="changePasswordLoading" :size="16" class="spin" /><KeyRound v-else :size="16" /> {{ changePasswordLoading ? t('password.submitting') : t('password.submit') }}</button><button type="button" class="secondary-button" :disabled="changePasswordLoading" @click="closeChangePassword">{{ t('common.cancel') }}</button></div></form></section></div>
    <p v-if="changePasswordNotice" class="alert success" style="position: fixed; top: 70px; right: 20px; z-index: 11; margin: 0">{{ changePasswordNotice }}</p>
  </header>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, reactive, ref, useSlots, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, clearSession, saveSession } from '../api'
import BrandLogo from './BrandLogo.vue'
import LanguageSelect from './LanguageSelect.vue'
import { t } from '../i18n'
import { mobileNavItems, navKeyForPath } from '../topbarNav'
import { FolderOpen, KeyRound, LoaderCircle, LogOut, Menu, RefreshCw, ScrollText, Share2, Shield, UploadCloud, X } from 'lucide-vue-next'

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
  sync: 'nav.syncTasks',
  files: 'nav.files',
  logs: 'nav.logs',
  collections: 'nav.collections',
  shares: 'nav.shares'
}
const sectionKey = computed(() => {
  return SECTION_NAV_KEYS[props.section] || `nav.${props.section}`
})
const router = useRouter()
const route = useRoute()
const slots = useSlots()

// v025 mobile navigation: the inline `.topbar-actions` stay untouched for desktop;
// on phones they collapse behind a single accessible menu button (logo + current
// section remain visible). The panel closes on backdrop click, Escape, route
// change and on rotating back to a desktop-width viewport.
const mobileMenuOpen = ref(false)
const mobileMenuButton = ref(null)
const mobileCloseButton = ref(null)
const hasActionsSlot = computed(() => Boolean(slots.actions))
const isAdmin = computed(() => props.user?.role === 'admin')
const mobileNav = computed(() => mobileNavItems({ isAdmin: isAdmin.value }))
const activeMobileKey = computed(() => navKeyForPath(route.path))
const NAV_ICON_MAP = { files: FolderOpen, collections: UploadCloud, shares: Share2, sync: RefreshCw, logs: ScrollText, admin: Shield }
function mobileNavIcon(key) { return NAV_ICON_MAP[key] || FolderOpen }
function openMobileMenu() { mobileMenuOpen.value = true; nextTick(() => { mobileCloseButton.value?.focus() }) }
function closeMobileMenu() { mobileMenuOpen.value = false }
function toggleMobileMenu() { if (mobileMenuOpen.value) closeMobileMenu(); else openMobileMenu() }
function onMobileEscape(event) { if (event.key === 'Escape' && mobileMenuOpen.value) { closeMobileMenu(); mobileMenuButton.value?.focus() } }
const mobileBreakpoint = typeof window !== 'undefined' ? window.matchMedia('(max-width: 800px)') : null
function onMobileBreakpointChange(event) { if (!event.matches) mobileMenuOpen.value = false }
function menuPassword() { closeMobileMenu(); openChangePassword() }
function menuLogout() { closeMobileMenu(); logout() }
watch(mobileMenuOpen, (open) => {
  if (open) {
    window.addEventListener('keydown', onMobileEscape)
    document.body.style.overflow = 'hidden'
  } else {
    window.removeEventListener('keydown', onMobileEscape)
    document.body.style.overflow = ''
  }
})
watch(() => route.fullPath, () => { if (mobileMenuOpen.value) closeMobileMenu() })
if (mobileBreakpoint) mobileBreakpoint.addEventListener('change', onMobileBreakpointChange)
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onMobileEscape)
  if (mobileBreakpoint) mobileBreakpoint.removeEventListener('change', onMobileBreakpointChange)
  document.body.style.overflow = ''
})
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
