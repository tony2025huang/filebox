<template>
  <main class="login-shell">
    <div class="login-language"><LanguageSelect /></div>
    <section class="login-intro">
      <BrandLogo variant="login" />
      <p class="eyebrow">{{ t('login.introEyebrow') }}</p>
      <h1>{{ t('login.heading') }}</h1>
      <p class="intro-copy">{{ t('login.copy') }}</p>
      <div class="intro-rule"></div>
      <p class="intro-meta">{{ t('login.meta') }}</p>
    </section>
    <section class="login-panel">
      <div class="login-form-wrap">
        <div class="mobile-brand"><BrandLogo variant="login" compact /></div>
        <p class="eyebrow">{{ t('login.eyebrow') }}</p>
        <h2>{{ t('login.formHeading', { title: brand.siteTitle }) }}</h2>
        <p class="muted">{{ t('login.formCopy') }}</p>
        <form @submit.prevent="submit">
          <template v-if="!totpMode && !registerMode">
            <label>{{ t('login.username') }}<input v-model.trim="form.username" autocomplete="username" required autofocus /></label>
            <label>{{ t('login.password') }}<input v-model="form.password" type="password" autocomplete="current-password" required /></label>
            <label class="check-label"><input v-model="remember" type="checkbox" /> {{ t('login.remember') }}</label>
          </template>
          <template v-else-if="registerMode">
            <label>{{ t('login.username') }}<input v-model.trim="registerForm.username" autocomplete="username" required autofocus /></label>
            <label>{{ t('login.password') }}<input v-model="registerForm.password" type="password" autocomplete="new-password" required /></label>
            <label>{{ t('login.confirmPassword') }}<input v-model="registerForm.confirmPassword" type="password" autocomplete="new-password" required /></label>
            <p class="field-hint">{{ t('login.passwordPolicy') }}</p>
          </template>
          <template v-else>
            <p class="alert success">{{ totpSetup ? t('login.totpSetupCopy') : t('login.totpRequiredCopy') }}</p>
            <div v-if="totpSetup" class="totp-setup"><img :src="qrCodeUrl" :alt="t('login.totpQrAlt')" /><code>{{ totpSecret }}</code></div>
            <label>{{ t('login.totpCode') }}<input v-model="totpCode" inputmode="numeric" autocomplete="one-time-code" maxlength="6" pattern="[0-9]{6}" required autofocus /></label>
          </template>
          <p v-if="error" class="alert error">{{ error }}</p>
          <button class="primary-button submit-button" :disabled="loading"><span>{{ loading ? t('login.submitting') : totpMode ? t('login.totpSubmit') : registerMode ? t('login.registerSubmit') : t('login.submit') }}</span><ArrowRight :size="18" /></button>
          <button v-if="totpMode" type="button" class="secondary-button totp-back" @click="resetTOTP">{{ t('login.totpBack') }}</button>
          <button v-else-if="brand.registerEnabled && !registerMode" type="button" class="secondary-button login-switch" @click="registerMode = true">{{ t('login.registerCopy') }}</button>
          <button v-else-if="registerMode" type="button" class="secondary-button login-switch" @click="registerMode = false">{{ t('login.hasAccount') }}</button>
        </form>
        <p class="login-foot">{{ t('login.defaultAdmin') }}</p>
        <BrandFooter />
      </div>
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowRight } from 'lucide-vue-next'
import { api, saveSession } from '../api'
import { brand, loadBrand } from '../brand'
import BrandFooter from '../components/BrandFooter.vue'
import BrandLogo from '../components/BrandLogo.vue'
import LanguageSelect from '../components/LanguageSelect.vue'
import { setLocale, t } from '../i18n'

const router = useRouter()
const route = useRoute()
const form = reactive({ username: '', password: '' })
const registerForm = reactive({ username: '', password: '', confirmPassword: '' })
const remember = ref(false)
const loading = ref(false)
const error = ref('')
const registerMode = ref(false)
const totpMode = ref(false)
const totpSetup = ref(false)
const totpChallenge = ref('')
const totpSecret = ref('')
const totpCode = ref('')
const qrCodeUrl = computed(() => `/api/auth/totp-qrcode?challenge=${encodeURIComponent(totpChallenge.value)}`)

function resetTOTP() { totpMode.value = false; totpSetup.value = false; totpChallenge.value = ''; totpSecret.value = ''; totpCode.value = ''; error.value = '' }

// submit authenticates and applies an authenticated user's explicit language preference immediately.
// submit 登录并立即应用已登录用户明确设置的语言偏好。
async function submit() {
  loading.value = true
  error.value = ''
  try {
    if (registerMode.value && registerForm.password !== registerForm.confirmPassword) throw new Error(t('login.passwordMismatch'))
    const registerBody = { username: registerForm.username, password: registerForm.password }
    const body = await api(totpMode.value ? '/api/auth/totp' : registerMode.value ? '/api/auth/register' : '/api/auth/login', { method: 'POST', body: JSON.stringify(totpMode.value ? { totpChallenge: totpChallenge.value, code: totpCode.value } : registerMode.value ? registerBody : form) })
    if (body.data?.totpRequired || body.data?.totpSetup) {
      totpMode.value = true
      totpSetup.value = Boolean(body.data.totpSetup)
      totpChallenge.value = body.data.totpChallenge
      totpSecret.value = body.data.secret || ''
      totpCode.value = ''
      return
    }
    saveSession(body)
    if (body.data.user.language && !localStorage.getItem('filebox_locale')) setLocale(body.data.user.language)
    if (!remember.value) sessionStorage.setItem('filebox_session', '1')
    router.push(registerMode.value ? '/' : route.query.redirect || '/')
  } catch (err) {
    error.value = err.message
  } finally {
    loading.value = false
  }
}

// loadPublicBrand refreshes the registration switch before rendering the login entry point.
// loadPublicBrand 在渲染登录入口前刷新公开注册开关。
onMounted(() => { loadBrand() })
</script>
