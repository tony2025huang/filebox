<template>
  <main class="login-shell">
    <div class="login-language"><LanguageSelect /></div>
    <section class="login-intro"><BrandLogo variant="login" /><p class="eyebrow">{{ t('password.eyebrow') }}</p><h1>{{ t('password.heading') }}</h1><p class="intro-copy">{{ t('password.copy') }}</p><div class="intro-rule"></div><p class="intro-meta">{{ t('login.meta') }}</p></section>
    <section class="login-panel"><div class="login-form-wrap"><div class="mobile-brand"><BrandLogo variant="login" compact /></div><p class="eyebrow">{{ t('password.eyebrow') }}</p><h2>{{ t('password.title') }}</h2><p class="muted">{{ t('password.policy', policy) }}</p><form @submit.prevent="submit"><label>{{ t('password.old') }}<input v-model="form.oldPassword" type="password" autocomplete="current-password" required /></label><label>{{ t('password.new') }}<input v-model="form.newPassword" type="password" autocomplete="new-password" required /></label><label>{{ t('password.confirm') }}<input v-model="confirmation" type="password" autocomplete="new-password" required /></label><p v-if="error" class="alert error">{{ error }}</p><button class="primary-button submit-button" :disabled="loading"><span>{{ loading ? t('password.submitting') : t('password.submit') }}</span><ArrowRight :size="18" /></button></form><BrandFooter /></div></section>
  </main>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight } from 'lucide-vue-next'
import { api, saveSession } from '../api'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { t } from '../i18n'

const router = useRouter()
const form = reactive({ oldPassword: '', newPassword: '' })
const confirmation = ref('')
const policy = reactive({ passwordMinLength: 8, passwordComplexity: 3 })
const loading = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  if (form.newPassword !== confirmation.value) { error.value = t('password.mismatch'); return }
  loading.value = true
  try {
    const body = await api('/api/auth/change-password', { method: 'POST', body: JSON.stringify(form) })
    saveSession(body)
    router.push('/')
  } catch (err) { error.value = err.message } finally { loading.value = false }
}

onMounted(async () => {
  try { Object.assign(policy, (await api('/api/auth/password-policy')).data) } catch { /* keep defaults when the session is unavailable */ }
})
</script>
