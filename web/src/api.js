import { t } from './i18n'

const messageKeys = {
  '用户名或密码错误': 'error.loginFailed', '用户名和密码不能为空': 'error.invalidRequest', '请先登录': 'error.authRequired',
  '需要管理员权限': 'error.adminRequired', '请求格式无效': 'error.invalidRequest', '语言设置无效': 'error.invalidLanguage',
  '同名文件已存在': 'error.conflict', '用户名已存在': 'error.userExists', '超出用户配额': 'error.quotaExceeded',
  '系统存储空间不足，暂时禁止上传': 'error.diskFull', '文件不存在': 'error.fileNotFound', '文件内容不存在': 'error.fileContentNotFound',
  '上传失败': 'error.uploadFailed', '下载失败': 'error.downloadFailed', '删除文件失败': 'error.deleteFailed',
  '密码过长': 'error.passwordTooLong', '设置无效': 'error.invalidSettings', '用户信息无效': 'error.invalidUser',
  '不能删除当前管理员': 'error.cannotDeleteSelf', '文件校验值不匹配': 'error.checksumMismatch', '文件名包含非法字符，禁止上传': 'error.invalidFilename'
}

const settingsMessageKeys = {
  '日志留存天数无效': 'error.invalidLogRetentionDays', '登录失败锁定阈值无效': 'error.invalidLockThreshold', '自动解锁时长无效': 'error.invalidAutoUnlockMinutes',
  '系统默认语言无效': 'error.invalidDefaultLang', '界面主题色无效': 'error.invalidThemeColor', '密码最小长度无效': 'error.invalidPasswordMinLength',
  '密码复杂度无效': 'error.invalidPasswordComplexity', 'IP 锁定窗口无效': 'error.invalidIPLockWindow', 'IP 锁定阈值无效': 'error.invalidIPLockThreshold', 'IP 解锁时长无效': 'error.invalidIPUnlockMinutes'
}

const codeKeys = { DISK_FULL: 'error.diskFull', PASSWORD_CHANGE_REQUIRED: 'error.passwordChangeRequired' }

// localizeError maps stable API status/codes/messages while retaining unknown backend messages as a fallback.
// localizeError 按稳定的状态码/错误码/消息映射翻译，并保留未知后端消息作为回退。
export function localizeError(error = {}) {
  const code = error.data?.code || error.code
  if (code && codeKeys[code]) return t(codeKeys[code])
  if (error.backendMessage && settingsMessageKeys[error.backendMessage]) return t(settingsMessageKeys[error.backendMessage])
  if (error.backendMessage && messageKeys[error.backendMessage]) return t(messageKeys[error.backendMessage])
  if (error.message && messageKeys[error.message]) return t(messageKeys[error.message])
  const statusKeys = { 401: 'error.authRequired', 403: 'error.adminRequired', 409: 'error.conflict', 413: 'error.uploadFailed', 503: 'error.diskFull' }
  if (statusKeys[error.status]) return t(statusKeys[error.status])
  return error.backendMessage || error.message || t('error.requestFailed')
}

// api 统一附加 Bearer token、JSON 请求头，并将非 2xx 响应转换为本地化错误。
// api centralizes Bearer-token and JSON headers, and turns non-2xx responses into localized errors.
export async function api(path, options = {}) {
  const headers = new Headers(options.headers || {})
  const token = localStorage.getItem('filebox_token')
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (options.body && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(path, { ...options, headers })
  let body = null
  try { body = await response.json() } catch { /* non-JSON response */ }
  if (!response.ok) {
    const error = new Error()
    error.status = response.status
    error.data = body?.data
    error.backendMessage = body?.message || ''
    error.message = localizeError(error)
    if (error.data?.code === 'PASSWORD_CHANGE_REQUIRED' && window.location.pathname !== '/change-password') {
      window.location.assign('/change-password')
    }
    throw error
  }
  return body
}

// clearSession 清除本地保存的认证令牌和用户快照。
// clearSession removes the locally stored authentication token and user snapshot.
export function clearSession() {
  localStorage.removeItem('filebox_token')
  localStorage.removeItem('filebox_user')
}

// saveSession 持久化登录响应中的 JWT 和公开用户信息。
// saveSession persists the JWT and public user information from the login response.
export function saveSession(body) {
  localStorage.setItem('filebox_token', body.data.token)
  localStorage.setItem('filebox_user', JSON.stringify(body.data.user))
}
