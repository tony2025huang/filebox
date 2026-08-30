import { t } from './i18n'

const messageKeys = {
  '用户名或密码错误': 'error.loginFailed', '用户名和密码不能为空': 'error.invalidRequest', '请先登录': 'error.authRequired',
  '需要管理员权限': 'error.adminRequired', '请求格式无效': 'error.invalidRequest', '语言设置无效': 'error.invalidLanguage',
  '同名文件已存在': 'error.conflict', '用户名已存在': 'error.userExists', '超出用户配额': 'error.quotaExceeded',
  '系统存储空间不足，暂时禁止上传': 'error.diskFull', '文件不存在': 'error.fileNotFound', '文件内容不存在': 'error.fileContentNotFound',
  '上传失败': 'error.uploadFailed', '下载失败': 'error.downloadFailed', '删除文件失败': 'error.deleteFailed',
  '密码过长': 'error.passwordTooLong', '设置无效': 'error.invalidSettings', '用户信息无效': 'error.invalidUser',
  '不能删除当前管理员': 'error.cannotDeleteSelf', '文件校验值不匹配': 'error.checksumMismatch', '文件名包含非法字符，禁止上传': 'error.invalidFilename',
  '注册功能未开放': 'error.registerDisabled', '分享链接已过期': 'error.shareExpired', '分享次数已用完': 'error.shareLimit', '分享不存在': 'error.shareNotFound',
  '分片大小必须在 2MB-8MB 之间': 'error.invalidChunkSize', '目录无效': 'error.invalidDir', '上传限速无效': 'error.invalidRateLimit',
  '分享有效期无效': 'error.invalidShareHours', '分享次数限制无效': 'error.invalidShareMax'
}

const settingsMessageKeys = {
  '日志留存天数无效': 'error.invalidLogRetentionDays', '登录失败锁定阈值无效': 'error.invalidLockThreshold', '自动解锁时长无效': 'error.invalidAutoUnlockMinutes',
  '系统默认语言无效': 'error.invalidDefaultLang', '界面主题色无效': 'error.invalidThemeColor', '密码最小长度无效': 'error.invalidPasswordMinLength',
  '密码复杂度无效': 'error.invalidPasswordComplexity', 'IP 锁定窗口无效': 'error.invalidIPLockWindow', 'IP 锁定阈值无效': 'error.invalidIPLockThreshold', 'IP 解锁时长无效': 'error.invalidIPUnlockMinutes'
}

const codeKeys = { DISK_FULL: 'error.diskFull', PASSWORD_CHANGE_REQUIRED: 'error.passwordChangeRequired', REGISTER_DISABLED: 'error.registerDisabled', FILE_TOO_LARGE: 'error.fileTooLarge' }

// computeFileSHA256 computes the client checksum and reports progress for the upload row.
// computeFileSHA256 计算客户端 SHA-256，并向上传项报告校验进度。
export async function computeFileSHA256(file, onProgress = () => {}) {
  const directLimit = 32 * 1024 * 1024
  if (file.size <= directLimit) {
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
    onProgress(100)
    return [...new Uint8Array(digest)].map(value => value.toString(16).padStart(2, '0')).join('')
  }

  // WebCrypto has no incremental digest API, so large files use a small streaming
  // SHA-256 implementation and retain only one 8MB block in memory.
  // WebCrypto 没有增量摘要 API，因此大文件使用轻量流式 SHA-256，仅保留一个 8MB 分块。
  const blockSize = 8 * 1024 * 1024
  const hasher = new IncrementalSHA256()
  for (let offset = 0; offset < file.size; offset += blockSize) {
    const block = new Uint8Array(await file.slice(offset, Math.min(offset + blockSize, file.size)).arrayBuffer())
    hasher.update(block)
    onProgress(Math.round(Math.min(file.size, offset + block.length) / file.size * 100))
  }
  return hasher.hexDigest()
}

const SHA256_K = Uint32Array.from([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
])

// IncrementalSHA256 hashes browser chunks without allocating a full-file buffer.
// IncrementalSHA256 在不创建完整文件缓冲区的前提下计算浏览器分块哈希。
class IncrementalSHA256 {
  constructor() { this.state = Uint32Array.from([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19]); this.buffer = new Uint8Array(64); this.bufferLength = 0; this.bytesHashed = 0 }
  update(data) { let offset = 0; this.bytesHashed += data.length; if (this.bufferLength) { const needed = Math.min(64 - this.bufferLength, data.length); this.buffer.set(data.subarray(0, needed), this.bufferLength); this.bufferLength += needed; offset += needed; if (this.bufferLength === 64) { this.process(this.buffer); this.bufferLength = 0 } } while (offset + 64 <= data.length) { this.process(data.subarray(offset, offset + 64)); offset += 64 } if (offset < data.length) { this.buffer.set(data.subarray(offset), 0); this.bufferLength = data.length - offset } }
  process(block) { const words = new Uint32Array(64); for (let i = 0; i < 16; i++) words[i] = (block[i * 4] << 24) | (block[i * 4 + 1] << 16) | (block[i * 4 + 2] << 8) | block[i * 4 + 3]; for (let i = 16; i < 64; i++) { const s0 = ((words[i - 15] >>> 7) | (words[i - 15] << 25)) ^ ((words[i - 15] >>> 18) | (words[i - 15] << 14)) ^ (words[i - 15] >>> 3); const s1 = ((words[i - 2] >>> 17) | (words[i - 2] << 15)) ^ ((words[i - 2] >>> 19) | (words[i - 2] << 13)) ^ (words[i - 2] >>> 10); words[i] = (words[i - 16] + s0 + words[i - 7] + s1) >>> 0 } let [a, b, c, d, e, f, g, h] = this.state; for (let i = 0; i < 64; i++) { const s1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7)); const choice = (e & f) ^ (~e & g); const temp1 = (h + s1 + choice + SHA256_K[i] + words[i]) >>> 0; const s0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10)); const majority = (a & b) ^ (a & c) ^ (b & c); const temp2 = (s0 + majority) >>> 0; h = g; g = f; f = e; e = (d + temp1) >>> 0; d = c; c = b; b = a; a = (temp1 + temp2) >>> 0 } this.state[0] = (this.state[0] + a) >>> 0; this.state[1] = (this.state[1] + b) >>> 0; this.state[2] = (this.state[2] + c) >>> 0; this.state[3] = (this.state[3] + d) >>> 0; this.state[4] = (this.state[4] + e) >>> 0; this.state[5] = (this.state[5] + f) >>> 0; this.state[6] = (this.state[6] + g) >>> 0; this.state[7] = (this.state[7] + h) >>> 0 }
  hexDigest() { const paddingLength = this.bufferLength < 56 ? 64 : 128; const padding = new Uint8Array(paddingLength); padding.set(this.buffer.subarray(0, this.bufferLength)); padding[this.bufferLength] = 0x80; const bits = this.bytesHashed * 8; const view = new DataView(padding.buffer); view.setUint32(paddingLength - 8, Math.floor(bits / 0x100000000)); view.setUint32(paddingLength - 4, bits >>> 0); for (let offset = 0; offset < padding.length; offset += 64) this.process(padding.subarray(offset, offset + 64)); return [...this.state].map(value => value.toString(16).padStart(8, '0')).join('') }
}

// localizeError maps stable API status/codes/messages while retaining unknown backend messages as a fallback.
// localizeError 按稳定的状态码/错误码/消息映射翻译，并保留未知后端消息作为回退。
export function localizeError(error = {}) {
  const code = error.data?.code || error.code
  if (code === 'QUOTA_EXCEEDED') {
    const used = Number(error.data?.usedBytes) || 0
    const quota = Number(error.data?.quotaBytes) || 0
    const fileSize = Number(error.data?.fileSize) || 0
    return t('error.quotaExceededDetail', { used: formatErrorBytes(used), quota: formatErrorBytes(quota), fileSize: formatErrorBytes(fileSize), short: formatErrorBytes(Math.max(0, used + fileSize - quota)) })
  }
  if (code === 'FILE_TOO_LARGE') {
    const max = Number(error.data?.maxFileSize) || 0
    return t('error.fileTooLargeDetail', { max: formatErrorBytes(max) })
  }
  if (code && codeKeys[code]) return t(codeKeys[code])
  if (error.backendMessage && settingsMessageKeys[error.backendMessage]) return t(settingsMessageKeys[error.backendMessage])
  if (error.backendMessage && messageKeys[error.backendMessage]) return t(messageKeys[error.backendMessage])
  if (error.message && messageKeys[error.message]) return t(messageKeys[error.message])
  const statusKeys = { 401: 'error.authRequired', 403: 'error.adminRequired', 409: 'error.conflict', 413: 'error.uploadFailed', 503: 'error.diskFull' }
  if (statusKeys[error.status]) return t(statusKeys[error.status])
  return error.backendMessage || error.message || t('error.requestFailed')
}

function formatErrorBytes(bytes = 0) {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = -1
  do { value /= 1024; unit++ } while (value >= 1024 && unit < units.length - 1)
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`
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
