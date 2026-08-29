<template>
  <label class="language-select" :aria-label="t('common.language')">
    <span class="sr-only">{{ t('common.language') }}</span>
    <select :value="selectedLanguage" @change="changeLanguage">
      <option value="">{{ t('lang.followSystem') }}</option>
      <option value="zh-CN">{{ t('lang.zhCN') }}</option>
      <option value="zh-TW">{{ t('lang.zhTW') }}</option>
      <option value="en">{{ t('lang.en') }}</option>
    </select>
  </label>
</template>

<script setup>
import { computed } from 'vue'
import { api } from '../api'
import { currentLocale, systemLocale, setLocale, t } from '../i18n'

const props = defineProps({ user: { type: Object, default: null } })

const selectedLanguage = computed(() => {
  const saved = localStorage.getItem('filebox_locale')
  if (saved) return saved
  if (props.user) return props.user.language || ''
  return currentLocale.value
})

// changeLanguage switches immediately, then persists the preference for authenticated users.
// changeLanguage 立即切换语言，然后为已登录用户保存服务端偏好。
async function changeLanguage(event) {
  const language = event.target.value
  setLocale(language || systemLocale.value)
  if (language === '') localStorage.removeItem('filebox_locale')
  const token = localStorage.getItem('filebox_token')
  if (!token) return
  try {
    const body = await api('/api/auth/language', { method: 'PUT', body: JSON.stringify({ language }) })
    localStorage.setItem('filebox_user', JSON.stringify(body.data))
  } catch {
    // The current screen stays responsive; the next authenticated refresh will retry the preference.
  }
}
</script>
