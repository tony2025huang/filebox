import { t } from './i18n'

// batchDownloadFilename uses the browser's local clock to match the backend's
// YYYYMMDD-HHMMSS archive naming format.
// batchDownloadFilename 使用浏览器本地时间生成与后端一致的 ZIP 文件名。
export function batchDownloadFilename(date = new Date()) {
  const pad = value => String(value).padStart(2, '0')
  const timestamp = `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  return `filebox-batch-${timestamp}.zip`
}

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
  '分享有效期无效': 'error.invalidShareHours', '分享次数限制无效': 'error.invalidShareMax', '同步任务参数无效': 'sync.invalid', '目标系统参数无效': 'sync.invalid'
}

const settingsMessageKeys = {
  '日志留存天数无效': 'error.invalidLogRetentionDays', '登录失败锁定阈值无效': 'error.invalidLockThreshold', '自动解锁时长无效': 'error.invalidAutoUnlockMinutes',
  '系统默认语言无效': 'error.invalidDefaultLang', '界面主题色无效': 'error.invalidThemeColor', '密码最小长度无效': 'error.invalidPasswordMinLength',
  '密码复杂度无效': 'error.invalidPasswordComplexity', 'IP 锁定窗口无效': 'error.invalidIPLockWindow', 'IP 锁定阈值无效': 'error.invalidIPLockThreshold', 'IP 解锁时长无效': 'error.invalidIPUnlockMinutes'
}

const codeKeys = { DISK_FULL: 'error.diskFull', PASSWORD_CHANGE_REQUIRED: 'error.passwordChangeRequired', REGISTER_DISABLED: 'error.registerDisabled', FILE_TOO_LARGE: 'error.fileTooLarge', SHARE_DOWNLOAD_LIMIT: 'error.shareLimit', SHARE_NOT_FOUND: 'error.shareNotFound', SHARE_REVOKED: 'error.shareRevoked', SHARE_EXPIRED: 'error.shareExpired', SHARE_CONTENT_MISSING: 'error.shareContentMissing', BATCH_DELETE_EMPTY: 'error.batchDeleteEmpty', INVALID_FILE_ID: 'error.invalidFileId', INVALID_READ_ONLY_WINDOW: 'readOnly.invalidWindow', READ_ONLY: 'readOnly.error', COLLECTION_LIMIT: 'collection.limitReached', COLLECTION_EXPIRED: 'collection.expired', COLLECTION_REVOKED: 'collection.revoked', COLLECTION_FILE_TOO_LARGE: 'collection.fileTooLarge', COLLECTION_QUOTA_EXCEEDED: 'collection.quotaExceeded', QUOTA_EXCEEDED: 'error.quotaExceeded', SYNC_TASK_RUNNING: 'sync.confirmRunning' }
const shareMessageKeys = { '分享已撤销': 'error.shareRevoked', '分享下载被拒绝': 'error.shareDenied', '获取分享列表失败': 'error.shareListFailed', '获取分享日志失败': 'error.shareLogsFailed', '延期分享失败': 'error.shareExtendFailed', '增加分享次数失败': 'error.shareIncreaseFailed' }

codeKeys.HOST_KEY_CHANGED = 'sync.hostKeyChanged'

import { createSha256 } from './sha256Fallback.js'

// lastHashInfo 记录最近一次校验实际走的实现与实测吞吐，供上传界面与排查使用（v032 诊断）。
// 没有它就无法回答「5GB 文件到底是 WASM 还是纯 JS 兜底」这类问题。
// lastHashInfo records which implementation actually hashed the last file plus the measured
// throughput, so upload diagnostics can answer questions like "WASM or the JS fallback?".
let lastHashInfo = null

// getLastHashInfo 返回最近一次 computeFileSHA256 的诊断信息（engine/bytes/elapsedMs/mbps）。
// getLastHashInfo returns the diagnostics of the most recent computeFileSHA256 call.
export function getLastHashInfo() { return lastHashInfo }

const HASH_INFO_LOG_BYTES = 64 * 1024 * 1024

function toHex(buffer) {
  return [...new Uint8Array(buffer)].map(value => value.toString(16).padStart(2, '0')).join('')
}

function nowMs() {
  return (globalThis.performance?.now ? globalThis.performance.now() : Date.now())
}

function reportHashInfo(engine, bytes, elapsedMs, onInfo) {
  const mbps = Number((bytes / 1024 / 1024 / Math.max(elapsedMs, 1) * 1000).toFixed(1))
  const info = { engine, bytes, elapsedMs: Math.round(elapsedMs), mbps }
  lastHashInfo = info
  try { onInfo(info) } catch {}
  // 大文件才打印，避免小文件刷屏；这行日志就是下次定位性能问题的直接证据。
  // Only log for large files to avoid spam; this line is the direct evidence for the next perf check.
  if (bytes >= HASH_INFO_LOG_BYTES) {
    console.info(`[filebox] checksum engine=${info.engine} bytes=${info.bytes} elapsed=${info.elapsedMs}ms rate=${info.mbps}MB/s`)
  }
  return info
}

// computeFileSHA256 computes the client checksum and reports progress for the upload row.
// computeFileSHA256 计算客户端 SHA-256，并向上传项报告校验进度；第三个参数回传本次实际使用的实现与实测吞吐。
export async function computeFileSHA256(file, onProgress = () => {}, onInfo = () => {}) {
  // 校验一律优先在 Worker 内进行：≤ 阈值走原生 WebCrypto，超过阈值走 Worker 内的流式哈希
  // （WASM 优先、纯 JS 兜底），因此主线程任何时候都不会被哈希阻塞（v031-A/B）。
  // Hashing always runs in the worker first: native WebCrypto up to the threshold, streaming
  // (WASM first, pure JS fallback) beyond it, so the main thread never blocks (v031-A/B).
  const directLimit = Number(globalThis.FILEBOX_HASH_DIRECT_LIMIT) || 256 * 1024 * 1024
  const started = nowMs()
  let hex = ''
  let engine = 'unknown'

  const first = await computeSHA256InWorker(file, onProgress, directLimit, false)
  if (first) {
    hex = first.hex
    engine = first.engine
  } else if (file.size <= directLimit) {
    onProgress(0)
    hex = toHex(await crypto.subtle.digest('SHA-256', await file.arrayBuffer()))
    engine = 'native-main'
    onProgress(100)
  } else {
    // 大文件超时/报错时，先在 Worker 内用纯 JS 重试一次：整文件回退到主线程会让界面冻结数分钟，
    // 这正是要避免的降级方式；只有重试也失败才走主线程。
    // On timeout or worker error for a large file, retry inside a worker with the pure-JS hasher first:
    // falling back to the main thread would freeze the UI for minutes, which is the outcome to avoid.
    const retry = await computeSHA256InWorker(file, onProgress, directLimit, true)
    if (retry) {
      hex = retry.hex
      engine = retry.engine
    } else {
      const blockSize = 8 * 1024 * 1024
      const hasher = createSha256()
      for (let offset = 0; offset < file.size; offset += blockSize) {
        const block = new Uint8Array(await file.slice(offset, Math.min(offset + blockSize, file.size)).arrayBuffer())
        hasher.update(block)
        onProgress(Math.round(Math.min(file.size, offset + block.length) / file.size * 100))
      }
      hex = hasher.digest()
      engine = 'js-main'
    }
  }
  reportHashInfo(engine, file.size, nowMs() - started, onInfo)
  // 降级必须显式可见：旧标签页引用已被替换的 Worker 分片时会静默变慢，只有告警能让用户知道要硬刷新。
  // Degradation must be visible: a stale tab referencing a replaced worker chunk silently gets slower, so
  // only an explicit warning tells the user to hard-refresh.
  if (file.size >= HASH_INFO_LOG_BYTES) {
    if (engine === 'js-main') {
      console.warn('[filebox] checksum fell back to the MAIN thread (worker unavailable; if this repeats, hard-refresh the page)')
    } else if (engine === 'js') {
      console.warn('[filebox] checksum is using the pure-JS worker fallback (WebAssembly unavailable in this browser)')
    }
  }
  return hex
}

// computeSHA256InWorker 在 Web Worker 内计算文件摘要，返回 { hex, engine }；Worker 不可用、报错或超时
// 时返回 null 由调用方决定降级方式。forceJs 为 true 时跳过 WASM（用于 WASM 失败后的重试）。
// computeSHA256InWorker hashes the file inside a Web Worker and resolves { hex, engine }, or null when
// the worker is unavailable, errors or times out. forceJs skips WASM (used for the retry).
function computeSHA256InWorker(file, onProgress, directLimit, forceJs) {
  return new Promise(resolve => {
    let worker
    try {
      worker = new Worker(new URL('./hashWorker.js', import.meta.url), { type: 'module' })
    } catch {
      resolve(null)
      return
    }
    const id = `hash-${Date.now()}-${Math.random().toString(16).slice(2)}`
    let watchdog = null
    const finish = value => {
      clearTimeout(timer)
      if (watchdog !== null) { clearInterval(watchdog); watchdog = null }
      try { worker.terminate() } catch {}
      resolve(value)
    }
    // 看门狗：Worker 分片被替换后模块加载失败可能不触发 onerror，只依赖总超时会白等十几分钟；
    // 90 秒内没有任何消息（进度或结果）即判定 Worker 不可用并立即降级。
    // Watchdog: a replaced worker chunk may fail to load without firing onerror, so waiting for the total
    // deadline wastes minutes; no message within 90s means the worker is unusable, degrade immediately.
    let lastMessageAt = nowMs()
    watchdog = setInterval(() => { if (nowMs() - lastMessageAt > 90000) finish(null) }, 5000)
    const streamingBlocks = Math.max(0, Math.ceil((file.size - directLimit) / (256 * 1024 * 1024)))
    // 纯 JS 重试的预算按 ~8MB/s 的最差情况给两倍余量，避免大文件被误判超时。
    // The forced-JS retry budget assumes an 8MB/s worst case with 2x headroom.
    const timeoutMs = forceJs
      ? Math.max(300000, Math.ceil(file.size / (8 * 1024 * 1024)) * 1000 * 2)
      : 120000 + streamingBlocks * 30000
    const timer = setTimeout(() => finish(null), timeoutMs)
    worker.onmessage = event => {
      lastMessageAt = nowMs()
      const data = event.data || {}
      if (data.id !== id) return
      if (data.type === 'progress') {
        onProgress(Number(data.value) || 0)
        return
      }
      finish(data.type === 'done' && typeof data.hex === 'string' ? { hex: data.hex, engine: data.engine || 'worker' } : null)
    }
    worker.onerror = () => finish(null)
    worker.onmessageerror = () => finish(null)
    try {
      worker.postMessage({ id, file, directLimit, forceEngine: forceJs ? 'js' : '' })
    } catch {
      finish(null)
    }
  })
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
  if (code === 'SYNC_SYSTEM_REFERENCED') return t('sync.referenced', { count: Number(error.data?.references) || 0 })
  if (code && codeKeys[code]) return t(codeKeys[code])
  if (error.backendMessage && shareMessageKeys[error.backendMessage]) return t(shareMessageKeys[error.backendMessage])
  if (error.backendMessage && settingsMessageKeys[error.backendMessage]) return t(settingsMessageKeys[error.backendMessage])
  if (error.backendMessage && messageKeys[error.backendMessage]) return t(messageKeys[error.backendMessage])
  if (error.message && messageKeys[error.message]) return t(messageKeys[error.message])
  const statusKeys = { 401: 'error.authRequired', 403: 'error.adminRequired', 409: 'error.conflict', 413: 'error.uploadFailed', 502: 'sync.networkError', 503: 'error.diskFull' }
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

// sessionExpiredRedirected 防止令牌失效后多个并发请求各自触发跳转。
// sessionExpiredRedirected prevents every concurrent 401 from triggering its own redirect.
let sessionExpiredRedirected = false

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
    // 令牌失效（401）：清除本地会话并跳转登录，避免 /sync、/shares、/admin 等页面
    // 继续携带旧 token 反复请求；登录接口的 401（密码错误）与公开收集页除外。
    // Token expiry (401): clear the local session and redirect to login so /sync, /shares,
    // /admin and friends stop hammering with a stale token; login 401s (wrong password) and
    // the public collection page are excluded.
    if (response.status === 401 && token && !path.startsWith('/api/auth/login')) {
      redirectOnSessionExpired()
    }
    throw error
  }
  return body
}

// redirectOnSessionExpired 清除会话并跳转登录页（保留当前地址供登录后回跳；与 router 守卫协作避免重复跳转）。
// redirectOnSessionExpired clears the session and redirects to login, preserving the current
// location for post-login return; it cooperates with the router guard to avoid double redirects.
function redirectOnSessionExpired() {
  if (sessionExpiredRedirected) return
  sessionExpiredRedirected = true
  clearSession()
  const current = window.location.pathname + window.location.search
  if (!window.location.pathname.startsWith('/login') && !window.location.pathname.startsWith('/u/')) {
    window.location.assign('/login?redirect=' + encodeURIComponent(current))
  }
}

// clearSession 清除本地保存的认证令牌、用户快照与同会话传输记录（避免换用户看到旧记录）。
// clearSession removes the locally stored authentication token, user snapshot, and same-session transfer records.
export function clearSession() {
  localStorage.removeItem('filebox_token')
  localStorage.removeItem('filebox_user')
  sessionStorage.removeItem('filebox_transfers_v1')
}

// saveSession 持久化登录响应中的 JWT 和公开用户信息。
// saveSession persists the JWT and public user information from the login response.
export function saveSession(body) {
  sessionExpiredRedirected = false
  localStorage.setItem('filebox_token', body.data.token)
  localStorage.setItem('filebox_user', JSON.stringify(body.data.user))
}
