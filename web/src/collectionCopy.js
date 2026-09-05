// Pure helpers for copying collection credentials. Kept dependency-free so the
// Node test runner can import them directly (see web/tests/collectionCopy.test.mjs).
//
// Default sharing must NOT embed the password in the URL. The result dialog
// shows the clean link and the one-time password separately, and the primary
// copy action copies the two lines together in the active locale. Fragment
// (`#password=...`) support remains only as a backward-compatible API behavior
// and is never rendered as the default copy target.

export const COLLECTION_CREDENTIAL_LABELS = {
  'zh-CN': { link: '链接：', password: '密码：' },
  'zh-TW': { link: '連結：', password: '密碼：' },
  en: { link: 'Link: ', password: 'Password: ' },
}

export const DEFAULT_LOCALE = 'en'

// collectionCredentialText returns the localized two-line clipboard payload:
//   zh-CN: 链接：<cleanUrl>\n密码：<password>
//   zh-TW: 連結：<cleanUrl>\n密碼：<password>
//   en:    Link: <cleanUrl>\nPassword: <password>
export function collectionCredentialText(cleanUrl, password, locale = DEFAULT_LOCALE) {
  const labels = COLLECTION_CREDENTIAL_LABELS[locale] || COLLECTION_CREDENTIAL_LABELS[DEFAULT_LOCALE]
  return `${labels.link}${cleanUrl}\n${labels.password}${password}`
}
