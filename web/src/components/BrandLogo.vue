<template>
  <div class="brand-logo" :class="{ compact }" :aria-label="brand.siteTitle" role="img">
    <img v-if="hasCustomAsset" :src="assetPath" :alt="brand.siteTitle" />
    <svg v-else viewBox="0 0 208 48" aria-hidden="true">
      <rect x="0" y="22" width="12" height="22" rx="2" fill="var(--brand-color)" />
      <rect x="17" y="5" width="12" height="39" rx="2" fill="#e76f51" />
      <rect x="34" y="14" width="12" height="30" rx="2" fill="#264653" />
      <text x="59" y="33" fill="currentColor" font-family="Arial, sans-serif" font-size="24" font-weight="700">FileBox</text>
    </svg>
  </div>
</template>

<script setup>
// BrandLogo 根据页面变体选择自定义资源，并在缺失时使用内置 SVG。
// BrandLogo selects a custom asset by page variant and falls back to the embedded SVG when absent.
import { computed } from 'vue'
import { brand } from '../brand'

const props = defineProps({ variant: { type: String, default: 'main' }, compact: Boolean })
const assetPath = computed(() => props.variant === 'login' ? '/brand/login-logo' : '/brand/main-logo')
const hasCustomAsset = computed(() => props.variant === 'login' ? brand.hasLoginLogo : brand.hasMainLogo)
</script>
