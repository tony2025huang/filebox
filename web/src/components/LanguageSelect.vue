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
import { currentLocale, setLocale, storedLocale, t } from '../i18n'

const props = defineProps({ user: { type: Object, default: null } })

// selectedLanguage 只能依赖响应式状态。直接读 localStorage 会让这个 computed 不依赖任何响应式数据
// （localStorage 不是响应式的），于是它永久缓存首次算出的值：用户切回已存过偏好的语言后，渲染会用
// 缓存值把下拉"改回"旧语言——界面已是新语言、下拉却显示旧语言（v044.3 修）。
// The selected option must derive only from reactive state; reading localStorage here would make the
// computed depend on nothing reactive and cache its first value forever.
const selectedLanguage = computed(() => {
  if (storedLocale.value) return storedLocale.value
  if (props.user) return props.user.language || ''
  return currentLocale.value
})

// changeLanguage switches immediately, then persists the preference for authenticated users.
// changeLanguage 立即切换语言，然后为已登录用户保存服务端偏好。
async function changeLanguage(event) {
  const language = event.target.value
  setLocale(language)
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
