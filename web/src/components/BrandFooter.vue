<template>
  <footer v-if="brand.icpText || brand.policeText || brand.copyrightText" class="brand-footer">
    <span v-if="brand.copyrightText">{{ brand.copyrightText }}</span>
    <span v-if="brand.icpText">{{ brand.icpText }}</span>
    <span v-if="brand.policeText">{{ brand.policeText }}</span>
  </footer>
</template>

<script setup>
// BrandFooter 仅渲染非空的版权、ICP 和公安备案文本，避免页面留下空白区域。
// BrandFooter renders only non-empty copyright, ICP, and filing text so empty branding leaves no blank area.
import { brand } from '../brand'
</script>
