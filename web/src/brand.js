import { reactive } from 'vue'

export const DEFAULT_THEME_COLOR = '#1b998b'
const themeColorPattern = /^#[0-9a-f]{3}([0-9a-f]{3})?$/i

const defaultBrand = {
  siteTitle: 'FileBox 文件管理',
  siteDescription: '',
  icpText: '',
  policeText: '',
  copyrightText: '',
  hasFavicon: false,
  hasLoginLogo: false,
  hasMainLogo: false,
  registerEnabled: false,
  defaultLang: 'zh-CN',
  themeColor: DEFAULT_THEME_COLOR,
  maxFileSize: 100 * 1024 * 1024 * 1024
}

// brand 保存当前页面使用的公开品牌状态。
// brand stores the public branding state used by the current page.
export const brand = reactive({ ...defaultBrand })

export function isThemeColor(value) {
  return value === '' || themeColorPattern.test(String(value).trim())
}

export function normalizeThemeColor(value = '') {
  const color = String(value).trim().toLowerCase()
  if (!themeColorPattern.test(color)) return DEFAULT_THEME_COLOR
  if (color.length === 4) return `#${color[1]}${color[1]}${color[2]}${color[2]}${color[3]}${color[3]}`
  return color
}

function hexToRgb(color) {
  const normalized = normalizeThemeColor(color).slice(1)
  return {
    r: parseInt(normalized.slice(0, 2), 16),
    g: parseInt(normalized.slice(2, 4), 16),
    b: parseInt(normalized.slice(4, 6), 16)
  }
}

function rgbToHex({ r, g, b }) {
  return `#${[r, g, b].map(channel => Math.max(0, Math.min(255, Math.round(channel))).toString(16).padStart(2, '0')).join('')}`
}

function mixWithWhite(color, amount) {
  const rgb = hexToRgb(color)
  return rgbToHex({ r: rgb.r + (255 - rgb.r) * amount, g: rgb.g + (255 - rgb.g) * amount, b: rgb.b + (255 - rgb.b) * amount })
}

function darken(color, amount) {
  const rgb = hexToRgb(color)
  return rgbToHex({ r: rgb.r * (1 - amount), g: rgb.g * (1 - amount), b: rgb.b * (1 - amount) })
}

// applyTheme updates the CSS color tokens and browser theme metadata without a page reload.
// applyTheme 更新 CSS 颜色变量和浏览器主题元数据，无需刷新页面。
export function applyTheme(themeColor = '') {
  const color = normalizeThemeColor(themeColor)
  const strong = color === DEFAULT_THEME_COLOR ? '#137a6f' : darken(color, 0.12)
  const soft = color === DEFAULT_THEME_COLOR ? '#eaf8f5' : mixWithWhite(color, 0.9)
  document.documentElement.style.setProperty('--brand-color', color)
  document.documentElement.style.setProperty('--brand-color-strong', strong)
  document.documentElement.style.setProperty('--brand-color-soft', soft)
  const themeMeta = document.head.querySelector('meta[name="theme-color"]')
  if (themeMeta) themeMeta.content = color
  brand.themeColor = color
  return color
}

// applyBrand 更新标题、SEO 描述和 favicon，并在空描述时移除 meta 标签。
// applyBrand updates the title, SEO description, and favicon, removing the meta tag when the description is empty.
export function applyBrand(value = {}) {
  Object.assign(brand, defaultBrand, value)
  applyTheme(brand.themeColor)
  document.title = brand.siteTitle || defaultBrand.siteTitle

  let description = document.head.querySelector('meta[name="description"]')
  if (!brand.siteDescription) {
    description?.remove()
  } else {
    if (!description) {
      description = document.createElement('meta')
      description.name = 'description'
      document.head.appendChild(description)
    }
    description.content = brand.siteDescription
  }

  let icon = document.head.querySelector('link[rel~="icon"]')
  if (!icon) {
    icon = document.createElement('link')
    icon.rel = 'icon'
    document.head.appendChild(icon)
  }
  icon.href = '/brand/favicon'
}

// loadBrand 从公开 API 加载品牌配置，失败时回退到内置默认值。
// loadBrand loads branding from the public API and falls back to embedded defaults on failure.
export async function loadBrand() {
  try {
    const response = await fetch('/api/brand')
    if (!response.ok) throw new Error('brand request failed')
    const body = await response.json()
    applyBrand(body.data)
  } catch {
    applyBrand(defaultBrand)
  }
  return brand
}
