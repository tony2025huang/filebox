<template>
  <footer v-if="brand.siteTitle || brand.siteDescription || brand.icpText || brand.policeText || brand.copyrightText" class="brand-footer">
    <div v-if="brand.siteTitle || brand.siteDescription" class="brand-footer-heading">
      <span v-if="brand.siteTitle" class="brand-footer-title">{{ brand.siteTitle }}</span>
      <small v-if="brand.siteDescription" class="brand-footer-desc">{{ brand.siteDescription }}</small>
    </div>
    <div v-if="brand.copyrightText || brand.icpText || brand.policeText" class="brand-footer-compliance">
      <span v-if="brand.copyrightText" class="brand-footer-line">{{ brand.copyrightText }}</span>
      <span v-if="brand.icpText" class="brand-footer-line">{{ brand.icpText }}</span>
      <span v-if="brand.policeText" class="brand-footer-line">{{ brand.policeText }}</span>
    </div>
  </footer>
</template>

<script setup>
// BrandFooter 顶部为站点标题与描述（独立层级），其下版权/ICP/公安备案文本组成单一横向合规行，
// 窄屏自动换行并保持居中与可读间距；内容全部为空时不渲染。
// BrandFooter shows the site title/description as its own heading block, then renders copyright,
// ICP and police filing texts as one horizontal compliance row that wraps centered on narrow screens.
import { brand } from '../brand'
</script>

<style scoped>
.brand-footer { display: flex; flex-direction: column; gap: 10px; margin-top: 22px; padding-top: 14px; border-top: 1px solid #e3eaf1; color: #8a9aad; font-size: 12px; text-align: center; }
.brand-footer-heading { display: flex; flex-direction: column; align-items: center; gap: 3px; max-width: 100%; }
.brand-footer-title { color: #52657e; font-size: 13px; font-weight: 600; }
.brand-footer-desc { color: #8a9aad; max-width: min(620px, 100%); line-height: 1.6; overflow-wrap: anywhere; }
.brand-footer-compliance { display: flex; flex-wrap: wrap; justify-content: center; align-items: center; gap: 4px 18px; max-width: 100%; line-height: 1.8; }
.brand-footer-line { display: inline-block; max-width: 100%; overflow-wrap: anywhere; }
</style>
