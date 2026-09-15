import { t } from './i18n'

// batchDownloadFilename uses the browser's local clock to match the backend's
// YYYYMMDD-HHMMSS archive naming format.
// batchDownloadFilename ä½¿ç”¨æµè§ˆå™¨æœ¬åœ°æ—¶é—´ç”Ÿæˆä¸ŽåŽç«¯ä¸€è‡´çš„ ZIP æ–‡ä»¶åã€‚
export function batchDownloadFilename(date = new Date()) {
  const pad = value => String(value).padStart(2, '0')
  const timestamp = `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  return `filebox-batch-${timestamp}.zip`
}

const messageKeys = {
  'ç”¨æˆ·åæˆ–å¯†ç é”™è¯¯': 'error.loginFailed', 'ç”¨æˆ·åå’Œå¯†ç ä¸èƒ½ä¸ºç©º': 'error.invalidRequest', 'è¯·å…ˆç™»å½•': 'error.authRequired',
  'éœ€è¦ç®¡ç†å‘˜æƒé™': 'error.adminRequired', 'è¯·æ±‚æ ¼å¼æ— æ•ˆ': 'error.invalidRequest', 'è¯­è¨€è®¾ç½®æ— æ•ˆ': 'error.invalidLanguage',
  'åŒåæ–‡ä»¶å·²å­˜åœ¨': 'error.conflict', 'ç”¨æˆ·åå·²å­˜åœ¨': 'error.userExists', 'è¶…å‡ºç”¨æˆ·é…é¢': 'error.quotaExceeded',
  'ç³»ç»Ÿå­˜å‚¨ç©ºé—´ä¸è¶³ï¼Œæš‚æ—¶ç¦æ­¢ä¸Šä¼ ': 'error.diskFull', 'æ–‡ä»¶ä¸å­˜åœ¨': 'error.fileNotFound', 'æ–‡ä»¶å†…å®¹ä¸å­˜åœ¨': 'error.fileContentNotFound',
  'ä¸Šä¼ å¤±è´¥': 'error.uploadFailed', 'ä¸‹è½½å¤±è´¥': 'error.downloadFailed', 'åˆ é™¤æ–‡ä»¶å¤±è´¥': 'error.deleteFailed',
  'å¯†ç è¿‡é•¿': 'error.passwordTooLong', 'è®¾ç½®æ— æ•ˆ': 'error.invalidSettings', 'ç”¨æˆ·ä¿¡æ¯æ— æ•ˆ': 'error.invalidUser',
  'ä¸èƒ½åˆ é™¤å½“å‰ç®¡ç†å‘˜': 'error.cannotDeleteSelf', 'æ–‡ä»¶æ ¡éªŒå€¼ä¸åŒ¹é…': 'error.checksumMismatch', 'æ–‡ä»¶ååŒ…å«éžæ³•å­—ç¬¦ï¼Œç¦æ­¢ä¸Šä¼ ': 'error.invalidFilename',
  'æ³¨å†ŒåŠŸèƒ½æœªå¼€æ”¾': 'error.registerDisabled', 'åˆ†äº«é“¾æŽ¥å·²è¿‡æœŸ': 'error.shareExpired', 'åˆ†äº«æ¬¡æ•°å·²ç”¨å®Œ': 'error.shareLimit', 'åˆ†äº«ä¸å­˜åœ¨': 'error.shareNotFound',
  'åˆ†ç‰‡å¤§å°å¿…é¡»åœ¨ 2MB-8MB ä¹‹é—´': 'error.invalidChunkSize', 'ç›®å½•æ— æ•ˆ': 'error.invalidDir', 'ä¸Šä¼ é™é€Ÿæ— æ•ˆ': 'error.invalidRateLimit',
  'åˆ†äº«æœ‰æ•ˆæœŸæ— æ•ˆ': 'error.invalidShareHours', 'åˆ†äº«æ¬¡æ•°é™åˆ¶æ— æ•ˆ': 'error.invalidShareMax', 'åŒæ­¥ä»»åŠ¡å‚æ•°æ— æ•ˆ': 'sync.invalid', 'ç›®æ ‡ç³»ç»Ÿå‚æ•°æ— æ•ˆ': 'sync.invalid'
}

const settingsMessageKeys = {
  'æ—¥å¿—ç•™å­˜å¤©æ•°æ— æ•ˆ': 'error.invalidLogRetentionDays', 'ç™»å½•å¤±è´¥é”å®šé˜ˆå€¼æ— æ•ˆ': 'error.invalidLockThreshold', 'è‡ªåŠ¨è§£é”æ—¶é•¿æ— æ•ˆ': 'error.invalidAutoUnlockMinutes',
  'ç³»ç»Ÿé»˜è®¤è¯­è¨€æ— æ•ˆ': 'error.invalidDefaultLang', 'ç•Œé¢ä¸»é¢˜è‰²æ— æ•ˆ': 'error.invalidThemeColor', 'å¯†ç æœ€å°é•¿åº¦æ— æ•ˆ': 'error.invalidPasswordMinLength',
  'å¯†ç å¤æ‚åº¦æ— æ•ˆ': 'error.invalidPasswordComplexity', 'IP é”å®šçª—å£æ— æ•ˆ': 'error.invalidIPLockWindow', 'IP é”å®šé˜ˆå€¼æ— æ•ˆ': 'error.invalidIPLockThreshold', 'IP è§£é”æ—¶é•¿æ— æ•ˆ': 'error.invalidIPUnlockMinutes'
}

const codeKeys = { DISK_FULL: 'error.diskFull', PASSWORD_CHANGE_REQUIRED: 'error.passwordChangeRequired', REGISTER_DISABLED: 'error.registerDisabled', FILE_TOO_LARGE: 'error.fileTooLarge', SHARE_DOWNLOAD_LIMIT: 'error.shareLimit', SHARE_NOT_FOUND: 'error.shareNotFound', SHARE_REVOKED: 'error.shareRevoked', SHARE_EXPIRED: 'error.shareExpired', SHARE_CONTENT_MISSING: 'error.shareContentMissing', SHARE_DENIED: 'error.shareDenied', BATCH_DELETE_EMPTY: 'error.batchDeleteEmpty', BATCH_TOO_LARGE: 'error.batchTooLarge', BATCH_LIMIT_EXCEEDED: 'error.batchLimitExceeded', INVALID_FILE_ID: 'error.invalidFileId', INVALID_USER_ID: 'error.invalidUser', INVALID_DISK_MODE: 'error.invalidDiskMode', INVALID_READ_ONLY_WINDOW: 'readOnly.invalidWindow', READ_ONLY: 'readOnly.error', REAUTH_RATE_LIMITED: 'error.reauthRateLimited', SCOPE_UNSUPPORTED: 'error.scopeUnsupported', FOLDER_EMPTY: 'error.folderEmpty', FOLDER_NOT_EMPTY: 'error.folderNotEmpty', task_not_found: 'error.fileNotFound', HOST_KEY_UPDATE_CONFLICT: 'sync.hostKeyUpdateConflict', COLLECTION_LIMIT: 'collection.limitReached', COLLECTION_EXPIRED: 'collection.expired', COLLECTION_REVOKED: 'collection.revoked', COLLECTION_FILE_TOO_LARGE: 'collection.fileTooLarge', COLLECTION_QUOTA_EXCEEDED: 'collection.quotaExceeded', COLLECTION_UNAUTHORIZED: 'collection.passwordWrong', COLLECTION_TASK_QUEUED: 'collection.queued', COLLECTION_RATE_LIMITED: 'error.collectionRateLimited', COLLECTION_MAX_UPLOADS_BELOW_USED: 'collection.maxUploadsBelow', QUOTA_EXCEEDED: 'error.quotaExceeded', SYNC_TASK_RUNNING: 'sync.confirmRunning' }
const shareMessageKeys = { 'åˆ†äº«å·²æ’¤é”€': 'error.shareRevoked', 'åˆ†äº«ä¸‹è½½è¢«æ‹’ç»': 'error.shareDenied', 'èŽ·å–åˆ†äº«åˆ—è¡¨å¤±è´¥': 'error.shareListFailed', 'èŽ·å–åˆ†äº«æ—¥å¿—å¤±è´¥': 'error.shareLogsFailed', 'å»¶æœŸåˆ†äº«å¤±è´¥': 'error.shareExtendFailed', 'å¢žåŠ åˆ†äº«æ¬¡æ•°å¤±è´¥': 'error.shareIncreaseFailed' }

codeKeys.HOST_KEY_CHANGED = 'sync.hostKeyChanged'

import { clientHashLimit, hashDirectLimit, shouldSkipClientHash } from './hashPolicy.js'
import { createSha256 } from './sha256Fallback.js'

// lastHashInfo è®°å½•æœ€è¿‘ä¸€æ¬¡æ ¡éªŒå®žé™…ä½¿ç”¨çš„å®žçŽ°ä¸Žå®žæµ‹åžåï¼ˆv034 è¯Šæ–­ï¼‰ã€‚æ²¡æœ‰å®ƒå°±æ— æ³•å›žç­”
// ã€Œ5GB æ–‡ä»¶åˆ°åº•æ˜¯ WASM è¿˜æ˜¯çº¯ JS å…œåº•ã€ã€Œç“¶é¢ˆæ˜¯è¯»ç›˜è¿˜æ˜¯è®¡ç®—ã€è¿™ç±»é—®é¢˜ã€‚
// lastHashInfo records which implementation hashed the last file plus read/hash timings (v034).
let lastHashInfo = null

// getLastHashInfo è¿”å›žæœ€è¿‘ä¸€æ¬¡ computeFileSHA256 çš„è¯Šæ–­ä¿¡æ¯ã€‚
// getLastHashInfo returns the diagnostics of the most recent computeFileSHA256 call.
export function getLastHashInfo() { return lastHashInfo }

const HASH_INFO_LOG_BYTES = 64 * 1024 * 1024

// å®¢æˆ·ç«¯å“ˆå¸Œä¸Šé™ä¸Žè·³è¿‡åˆ¤å®šè§ ./hashPolicy.jsï¼ˆé˜ˆå€¼ä¸Žè¾¹ç•Œç”± web/tests/hashPolicy.test.mjs è¦†ç›–ï¼‰ã€‚

// è¯Šæ–­æµ®å±‚å·²ç§»é™¤ï¼ˆv041ï¼‰ï¼šå®ƒçš„ç”¨é€”æ˜¯åœ¨æŽ§åˆ¶å°ä¸å¯ç”¨æ—¶ç¡®è®¤"è·‘çš„æ˜¯å“ªæ¡å®žçŽ°"ï¼Œç»“è®ºå·²ç»æ‹¿åˆ°
// ï¼ˆWASM æ­£å¸¸åˆå§‹åŒ–ï¼Œé—®é¢˜åœ¨æµè§ˆå™¨ä¾§çš„ WASM æ‰§è¡Œé€Ÿåº¦ï¼‰ã€‚ä¿ç•™ä¸‹é¢ reportHashInfo çš„ warn çº§æ—¥å¿—ï¼š
// æˆæœ¬ä¸ºé›¶ã€ä¸å¹²æ‰°ç•Œé¢ï¼Œä¸”åœ¨ä¸‹æ¬¡å‡ºçŽ°æ ¡éªŒå¼‚å¸¸æ—¶æ˜¯æœ€ç›´æŽ¥çš„è¯æ®ã€‚
// The on-page diagnostic overlay was removed (v041); the console warning below is kept on purpose.

function reportHashInfo(engine, bytes, elapsedMs, detail = {}, onInfo = () => {}) {
  const mbps = Number((bytes / 1024 / 1024 / Math.max(elapsedMs, 1) * 1000).toFixed(1))
  const info = {
    engine, bytes, elapsedMs: Math.round(elapsedMs), mbps,
    readMs: Number(detail.readMs) || 0, hashMs: Number(detail.hashMs) || 0,
    wasmError: typeof detail.wasmError === 'string' ? detail.wasmError : ''
  }
  lastHashInfo = info
  try { onInfo(info) } catch {}
  // åªå¯¹å¤§æ–‡ä»¶æŠ¥å‘Šï¼Œé¿å…å°æ–‡ä»¶åˆ·å±ã€‚ç”¨ warn çº§è€Œéž infoï¼šå³ä½¿æŽ§åˆ¶å°çº§åˆ«è¢«æ”¶çª„åˆ° Warnings+Errors ä¹Ÿèƒ½çœ‹åˆ°ã€‚
  // Only report for large files. warn (not info) so it stays visible even when the console level is narrowed.
  if (bytes >= HASH_INFO_LOG_BYTES) {
    if (info.engine === 'skipped') {
      console.warn(`[filebox] checksum skipped bytes=${bytes} (over the ${(clientHashLimit() / 1024 / 1024 / 1024).toFixed(1)}GiB client limit); the server computes sha256`)
    } else {
      const timing = info.readMs || info.hashMs ? ` read=${info.readMs}ms hash=${info.hashMs}ms` : ''
      const why = info.wasmError ? ` wasmError=${JSON.stringify(info.wasmError)}` : ''
      console.warn(`[filebox] checksum engine=${info.engine} bytes=${info.bytes} elapsed=${info.elapsedMs}ms rate=${info.mbps}MB/s${timing}${why}`)
    }
  }
  return info
}

// computeFileSHA256 computes the client checksum and reports progress for the upload row.
// computeFileSHA256 è®¡ç®—å®¢æˆ·ç«¯ SHA-256ï¼Œå¹¶å‘ä¸Šä¼ é¡¹æŠ¥å‘Šæ ¡éªŒè¿›åº¦ï¼›ç¬¬ä¸‰ä¸ªå‚æ•°å›žä¼ æœ¬æ¬¡å®žé™…ä½¿ç”¨çš„
// å®žçŽ°ä¸Žè¯»å†™è€—æ—¶æ‹†åˆ†ï¼ˆengine/readMs/hashMs/wasmErrorï¼‰ã€‚
export async function computeFileSHA256(file, onProgress = () => {}, onInfo = () => {}) {
  // æ ¡éªŒä¸€å¾‹ä¼˜å…ˆåœ¨ Worker å†…è¿›è¡Œï¼šâ‰¤ é˜ˆå€¼èµ°åŽŸç”Ÿ WebCryptoï¼Œè¶…è¿‡é˜ˆå€¼èµ° Worker å†…çš„æµå¼å“ˆå¸Œ
  // ï¼ˆWASM ä¼˜å…ˆã€çº¯ JS å…œåº•ï¼‰ï¼Œå› æ­¤ä¸»çº¿ç¨‹ä»»ä½•æ—¶å€™éƒ½ä¸ä¼šè¢«å“ˆå¸Œé˜»å¡žï¼ˆv031-A/Bï¼‰ã€‚
  // Hashing always runs in the worker first: native WebCrypto up to the threshold, streaming
  // (WASM first, pure JS fallback) beyond it, so the main thread never blocks (v031-A/B).
  const directLimit = hashDirectLimit()
  const started = Date.now()
  // è¶…è¿‡å®¢æˆ·ç«¯ä¸Šé™çš„æ–‡ä»¶ç›´æŽ¥è·³è¿‡ï¼šæœåŠ¡ç«¯ä¼šç®—å‡ºæƒå¨å“ˆå¸Œå¹¶åœ¨å®Œæˆå“åº”é‡Œå›žä¼ ï¼ˆv036ï¼‰ã€‚
  // Files over the client limit are skipped outright; the server computes the hash and returns it (v036).
  if (shouldSkipClientHash(file.size)) {
    reportHashInfo('skipped', file.size, 0, {}, onInfo)
    return ''
  }
  // å¤§æ–‡ä»¶æŠŠè¿›åº¦åŒæ—¶ç”»åˆ°é¡µé¢è¯Šæ–­æ¡†é‡Œï¼Œè¿™æ ·"æ ¡éªŒé€ŸçŽ‡æ˜¯å¤šå°‘"ä¸å¿…ä¾èµ–æŽ§åˆ¶å°ã€‚
  // 进度只回传给调用方（上传行自己渲染），不再画到已移除的页面诊断浮层（v041）。
  // Progress is reported to the caller only; the removed on-page diagnostic overlay is gone (v041).
  const viaWorker = await computeSHA256InWorker(file, onProgress, directLimit)
  if (viaWorker) {
    reportHashInfo(viaWorker.engine, file.size, Date.now() - started, viaWorker, onInfo)
    return viaWorker.hex
  }

  if (file.size <= directLimit) {
    onProgress(0)
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
    onProgress(100)
    reportHashInfo('native-main', file.size, Date.now() - started, {}, onInfo)
    return [...new Uint8Array(digest)].map(value => value.toString(16).padStart(2, '0')).join('')
  }

  // Worker ä¸å¯ç”¨æ—¶çš„æœ€åŽå…œåº•ï¼šä¸»çº¿ç¨‹æµå¼å“ˆå¸Œï¼Œä»…ä¿ç•™ä¸€ä¸ª 8MB åˆ†å—åœ¨å†…å­˜ä¸­ã€‚
  // Last-resort fallback when no worker is available: main-thread streaming with one 8MB block.
  const blockSize = 8 * 1024 * 1024
  const hasher = createSha256()
  for (let offset = 0; offset < file.size; offset += blockSize) {
    const block = new Uint8Array(await file.slice(offset, Math.min(offset + blockSize, file.size)).arrayBuffer())
    hasher.update(block)
    onProgress(Math.round(Math.min(file.size, offset + block.length) / file.size * 100))
  }
  const hex = hasher.digest()
  reportHashInfo('js-main', file.size, Date.now() - started, {}, onInfo)
  return hex
}

// computeSHA256InWorker åœ¨ Web Worker å†…è®¡ç®—æ–‡ä»¶æ‘˜è¦ï¼›Worker ä¸å¯ç”¨ã€æŠ¥é”™æˆ–è¶…æ—¶ï¼ˆå¤§æ–‡ä»¶æŒ‰ 120 ç§’/256MiB
// ä¼°ç®—ä¸Šé™ï¼‰æ—¶è¿”å›ž nullï¼Œç”±è°ƒç”¨æ–¹å›žé€€åˆ°ä¸»çº¿ç¨‹å®žçŽ°ã€‚æ–‡ä»¶å¯¹è±¡æŒ‰ç»“æž„åŒ–å…‹éš†ä¼ å…¥ Workerã€‚
// computeSHA256InWorker hashes the file inside a Web Worker and returns null when the worker is
// unavailable, errors, or exceeds its deadline (120s plus 30s per 256MiB) so the caller can fall back.
function computeSHA256InWorker(file, onProgress, directLimit) {
  return new Promise(resolve => {
    let worker
    try {
      worker = new Worker(new URL('./hashWorker.js', import.meta.url), { type: 'module' })
    } catch (err) {
      console.warn(`[filebox] checksum worker could not start (${String((err && err.message) || err)}); hashing on the main thread`)
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
    // çœ‹é—¨ç‹—ï¼šWorker åˆ†ç‰‡è¢«æ›¿æ¢åŽæ¨¡å—åŠ è½½å¤±è´¥å¯èƒ½ä¸è§¦å‘ onerrorï¼Œåªä¾èµ–æ€»è¶…æ—¶ä¼šç™½ç­‰åå‡ åˆ†é’Ÿï¼›
    // 90 ç§’å†…æ²¡æœ‰ä»»ä½•æ¶ˆæ¯ï¼ˆè¿›åº¦æˆ–ç»“æžœï¼‰å³åˆ¤å®šä¸å¯ç”¨å¹¶ç«‹å³é™çº§ã€‚åªå½±å“å¤±è´¥åœºæ™¯ï¼Œä¸æ”¹å˜æ­£å¸¸åžåã€‚
    // Watchdog: a replaced worker chunk can fail to load without firing onerror. No message within 90s
    // means the worker is unusable. Failure path only: normal throughput is unchanged.
    let lastMessageAt = Date.now()
    watchdog = setInterval(() => {
      if (Date.now() - lastMessageAt > 90000) {
        console.warn('[filebox] checksum worker reported nothing for 90s; falling back')
        finish(null)
      }
    }, 5000)
    const streamingBlocks = Math.max(0, Math.ceil((file.size - directLimit) / (256 * 1024 * 1024)))
    const timer = setTimeout(() => finish(null), 120000 + streamingBlocks * 30000)
    worker.onmessage = event => {
      lastMessageAt = Date.now()
      const data = event.data || {}
      if (data.id !== id) return
      if (data.type === 'progress') {
        onProgress(Number(data.value) || 0)
        return
      }
      finish(data.type === 'done' && typeof data.hex === 'string'
        ? {
            hex: data.hex,
            engine: data.engine || 'worker',
            readMs: Number(data.readMs) || 0,
            hashMs: Number(data.hashMs) || 0,
            wasmError: typeof data.wasmError === 'string' ? data.wasmError : ''
          }
        : null)
    }
    worker.onerror = () => {
      console.warn('[filebox] checksum worker failed to load (stale asset or blocked module?); falling back')
      finish(null)
    }
    worker.onmessageerror = () => {
      console.warn('[filebox] checksum worker sent an unreadable message; falling back')
      finish(null)
    }
    try {
      worker.postMessage({ id, file, directLimit })
    } catch {
      finish(null)
    }
  })
}

// localizeError maps stable API status/codes/messages while retaining unknown backend messages as a fallback.
// localizeError æŒ‰ç¨³å®šçš„çŠ¶æ€ç /é”™è¯¯ç /æ¶ˆæ¯æ˜ å°„ç¿»è¯‘ï¼Œå¹¶ä¿ç•™æœªçŸ¥åŽç«¯æ¶ˆæ¯ä½œä¸ºå›žé€€ã€‚
export function localizeError(error = {}) {
  const code = error.data?.code || error.code
  if (code === 'QUOTA_EXCEEDED') {
    const used = Number(error.data?.usedBytes) || 0
    const quota = Number(error.data?.quotaBytes) || 0
    const fileSize = Number(error.data?.fileSize) || 0
    // pendingBytes 是"未完成上传已预留"的额度（v039 起由后端返回）：把它一起显示，
    // 否则用户会看到"已用远低于配额却提示配额不足"而无从理解（v042）。
    // pendingBytes is the quota reserved by unfinished uploads; showing it explains a quota error
    // whose used value looks far below the quota (v042).
    const pending = Number(error.data?.pendingBytes) || 0
    if (pending > 0) {
      return t('error.quotaExceededPending', { used: formatErrorBytes(used), pending: formatErrorBytes(pending), quota: formatErrorBytes(quota), fileSize: formatErrorBytes(fileSize) })
    }
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
  // 带稳定错误码但上面没有映射：返回已翻译的通用文案，不再透出裸码或仅中文的后端消息（v042）。
  // A stable but unmapped code yields a translated generic message instead of a raw code or a
  // Chinese-only backend string, so the failure text follows the user's language (v042).
  if (code) return t('error.requestFailed')
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

// sessionExpiredRedirected é˜²æ­¢ä»¤ç‰Œå¤±æ•ˆåŽå¤šä¸ªå¹¶å‘è¯·æ±‚å„è‡ªè§¦å‘è·³è½¬ã€‚
// sessionExpiredRedirected prevents every concurrent 401 from triggering its own redirect.
let sessionExpiredRedirected = false

// api ç»Ÿä¸€é™„åŠ  Bearer tokenã€JSON è¯·æ±‚å¤´ï¼Œå¹¶å°†éž 2xx å“åº”è½¬æ¢ä¸ºæœ¬åœ°åŒ–é”™è¯¯ã€‚
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
    // ä»¤ç‰Œå¤±æ•ˆï¼ˆ401ï¼‰ï¼šæ¸…é™¤æœ¬åœ°ä¼šè¯å¹¶è·³è½¬ç™»å½•ï¼Œé¿å… /syncã€/sharesã€/admin ç­‰é¡µé¢
    // ç»§ç»­æºå¸¦æ—§ token åå¤è¯·æ±‚ï¼›ç™»å½•æŽ¥å£çš„ 401ï¼ˆå¯†ç é”™è¯¯ï¼‰ä¸Žå…¬å¼€æ”¶é›†é¡µé™¤å¤–ã€‚
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

// redirectOnSessionExpired æ¸…é™¤ä¼šè¯å¹¶è·³è½¬ç™»å½•é¡µï¼ˆä¿ç•™å½“å‰åœ°å€ä¾›ç™»å½•åŽå›žè·³ï¼›ä¸Ž router å®ˆå«åä½œé¿å…é‡å¤è·³è½¬ï¼‰ã€‚
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

// clearSession æ¸…é™¤æœ¬åœ°ä¿å­˜çš„è®¤è¯ä»¤ç‰Œã€ç”¨æˆ·å¿«ç…§ä¸ŽåŒä¼šè¯ä¼ è¾“è®°å½•ï¼ˆé¿å…æ¢ç”¨æˆ·çœ‹åˆ°æ—§è®°å½•ï¼‰ã€‚
// clearSession removes the locally stored authentication token, user snapshot, and same-session transfer records.
export function clearSession() {
  localStorage.removeItem('filebox_token')
  localStorage.removeItem('filebox_user')
  sessionStorage.removeItem('filebox_transfers_v1')
}

// saveSession æŒä¹…åŒ–ç™»å½•å“åº”ä¸­çš„ JWT å’Œå…¬å¼€ç”¨æˆ·ä¿¡æ¯ã€‚
// saveSession persists the JWT and public user information from the login response.
export function saveSession(body) {
  sessionExpiredRedirected = false
  localStorage.setItem('filebox_token', body.data.token)
  localStorage.setItem('filebox_user', JSON.stringify(body.data.user))
}

