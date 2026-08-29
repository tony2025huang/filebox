<template>
  <RouterView />
</template>

<script setup>
// App 作为单页应用根组件承载当前路由视图。
// App is the SPA root component that hosts the active route view.
import { watch } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import { brand } from './brand'
import { currentLocale, t } from './i18n'

const route = useRoute()

watch([currentLocale, () => brand.siteTitle, () => route.meta.titleKey], () => {
  document.title = `${t(route.meta.titleKey || 'page.login')} ${t('title.separator')}${brand.siteTitle}`
}, { immediate: true })
</script>
