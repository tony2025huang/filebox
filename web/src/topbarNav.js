// Pure, framework-free topbar navigation helpers for AuthenticatedTopbar.
// Extracted so the section list and active-key resolution can be unit-tested
// deterministically without mounting Vue or a router.
//
// 与 Vue 无关的纯顶栏导航辅助：分区清单与“当前路由→激活分区”的解析均在此
// 实现，便于用 node:test 直接做确定性单测。

export const TOPBAR_SECTIONS = [
  { key: 'files', to: '/', labelKey: 'nav.files', adminOnly: false },
  { key: 'collections', to: '/collections', labelKey: 'nav.collections', adminOnly: false },
  { key: 'shares', to: '/shares', labelKey: 'nav.shares', adminOnly: false },
  { key: 'sync', to: '/sync', labelKey: 'nav.syncTasks', adminOnly: false },
  { key: 'logs', to: '/logs', labelKey: 'nav.logs', adminOnly: false },
  { key: 'admin', to: '/admin', labelKey: 'nav.system', adminOnly: true },
]

const KNOWN_KEYS = new Set(TOPBAR_SECTIONS.map((section) => section.key))

/** Resolve the active topbar section key from a router path. */
export function navKeyForPath(path = '') {
  const first = String(path).split('/').filter(Boolean)[0] || ''
  const key = first.toLowerCase()
  if (!key) return 'files' // the root route is the Files page
  return KNOWN_KEYS.has(key) ? key : ''
}

/** Sections visible to the current user, in stable display order. */
export function mobileNavItems({ isAdmin = false } = {}) {
  return TOPBAR_SECTIONS.filter((section) => !section.adminOnly || Boolean(isAdmin))
}
