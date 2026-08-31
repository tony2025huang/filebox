<template>
  <header class="topbar">
    <div class="topbar-brand"><BrandLogo variant="main" compact link /><span class="slash">/</span><span class="section-name">{{ t(`nav.${section}`) }}</span></div>
    <div class="topbar-actions">
      <slot name="actions" />
      <LanguageSelect :user="user" />
      <RouterLink to="/" class="icon-text-button"><FolderOpen :size="16" /> {{ t('nav.files') }}</RouterLink>
      <RouterLink to="/shares" class="icon-text-button"><Share2 :size="16" /> {{ t('nav.shares') }}</RouterLink>
      <RouterLink to="/sync" class="icon-text-button"><RefreshCw :size="16" /> {{ t('nav.sync') }}</RouterLink>
      <RouterLink to="/logs" class="icon-text-button"><ScrollText :size="16" /> {{ t('nav.logs') }}</RouterLink>
      <RouterLink to="/collections" class="icon-text-button"><UploadCloud :size="16" /> {{ t('nav.collections') }}</RouterLink>
      <RouterLink v-if="user.role === 'admin'" to="/admin" class="icon-text-button"><Shield :size="16" /> {{ t('nav.admin') }}</RouterLink>
      <RouterLink to="/change-password?mode=self" class="icon-button" :title="t('nav.changePassword')"><KeyRound :size="17" /></RouterLink>
      <button class="icon-button" :title="t('nav.logout')" @click="logout"><LogOut :size="18" /></button>
    </div>
  </header>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { api, clearSession } from '../api'
import BrandLogo from './BrandLogo.vue'
import LanguageSelect from './LanguageSelect.vue'
import { t } from '../i18n'
import { FolderOpen, KeyRound, LogOut, RefreshCw, ScrollText, Share2, Shield, UploadCloud } from 'lucide-vue-next'

// AuthenticatedTopbar 是登录后所有视图共享的顶栏（#12/13/14）：统一包含 文件/分享/同步/日志/收集/管理后台(admin)/语言/改密/退出，
// 各页差异通过 actions 插槽保留（如 FilesView 的传输按钮与角标）。
// AuthenticatedTopbar is the shared topbar for all authenticated views (#12/13/14): it unifies files/shares/sync/logs/
// collections/admin(admin-only)/language/change-password/logout links; per-page extras ride the actions slot.
const props = defineProps({
  user: { type: Object, default: () => ({}) },
  section: { type: String, default: 'files' }
})
const router = useRouter()
async function logout() { try { await api('/api/auth/logout', { method: 'POST' }) } finally { clearSession(); router.push('/login') } }
</script>
