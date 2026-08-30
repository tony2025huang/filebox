<template>
  <footer v-if="brand.siteTitle || brand.siteDescription || brand.icpText || brand.policeText || brand.copyrightText" class="brand-footer">
    <span v-if="brand.siteTitle" class="brand-footer-title">{{ brand.siteTitle }}</span>
    <small v-if="brand.siteDescription" class="brand-footer-desc">{{ brand.siteDescription }}</small>
    <span v-if="brand.copyrightText" class="brand-footer-line">{{ brand.copyrightText }}</span>
    <span v-if="brand.icpText" class="brand-footer-line">{{ brand.icpText }}</span>
    <span v-if="brand.policeText" class="brand-footer-line">{{ brand.policeText }}</span>
  </footer>
</template>

<script setup>
// BrandFooter 第一行渲染站点标题与描述，随后依次渲染版权、ICP 和公安备案文本；全部为空时不渲染。
// BrandFooter renders the site title and description on the first line, followed by copyright, ICP, and filing text; nothing renders when all are empty.
import { brand } from '../brand'
</script>

<style scoped>
.brand-footer { display: flex; flex-direction: column; gap: 4px; margin-top: 22px; padding-top: 14px; border-top: 1px solid #e3eaf1; color: #8a9aad; font-size: 12px; text-align: center; }
.brand-footer-title { color: #52657e; font-size: 13px; font-weight: 600; }
.brand-footer-desc { color: #8a9aad; }
.brand-footer-line { display: inline-block; }
</style>
