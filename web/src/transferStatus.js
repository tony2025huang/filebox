// 传输状态契约（v044.2）：条目里存**稳定状态码**，展示时才翻译。
// 旧实现把 `t('files.uploading')` 这类本地化文案存进 item.status，导致两个问题：
// ① 切换语言后已存在的传输行不会重新翻译（后加入的行仍是旧语言）；
// ② 终止态判定/按钮可用性要拿"当前语言"的文案去比对，语言一换就漂移。
// 现在状态码与语言无关，翻译集中在 TRANSFER_STATUS_KEYS，node 可断言。
// Transfer status contract: items store a stable status code and the UI translates it at render
// time. Storing localized text made rows stick to the language they were created in, and made
// terminal-state checks depend on the active language.
export const TRANSFER_STATUS_KEYS = {
  preparing: 'files.uploadPreparing',
  uploading: 'files.uploading',
  checksum: 'files.checksum',
  instant: 'files.instantUpload',
  checking: 'files.checking',
  downloading: 'files.downloading',
  paused: 'files.paused',
  terminating: 'files.terminating',
  completed: 'files.completed',
  cancelled: 'files.finishedCancelled',
  failed: 'files.uploadFailed',
  need_reselect: 'files.needReselect',
  session_ended: 'files.sessionEnded'
}

const completedStatuses = new Set(['completed', 'instant', 'finished', 'cancelled'])

// v044.4：把"进行中"的稳定状态码归纳为可展示的阶段，供传输列表按阶段分组。
// 阶段只是状态码的归并，不引入新状态：准备/校验中 → 传输中 → 服务端处理中 → 已暂停/待处理。
// Active status codes are grouped into display stages; this adds no new state.
export const TRANSFER_STAGE_ORDER = ['preparing', 'transferring', 'processing', 'paused']
export const TRANSFER_STAGE_KEYS = {
  preparing: 'files.stagePreparing',
  transferring: 'files.stageTransferring',
  processing: 'files.stageProcessing',
  paused: 'files.stagePaused'
}
// 已完成记录的归类（成功/失败/取消）。
export const TRANSFER_OUTCOME_ORDER = ['success', 'failed', 'cancelled']
export const TRANSFER_OUTCOME_KEYS = {
  success: 'files.finishedSuccess',
  failed: 'files.finishedFailed',
  cancelled: 'files.finishedCancelled'
}

/** 归纳进行中条目所属阶段；未知码按"传输中"处理，避免条目消失。 */
export function transferStage(item) {
  switch (normalizeStatus(item?.status)) {
    case 'preparing':
    case 'checksum':
      return 'preparing'
    case 'checking':
      return 'processing'
    case 'paused':
    case 'need_reselect':
      return 'paused'
    default:
      return 'transferring'
  }
}

/** 归纳已完成条目属于成功/失败/取消。 */
export function transferOutcome(item) {
  if (item?.cancelled) return 'cancelled'
  if (item?.failed) return 'failed'
  return 'success'
}

/** 按阶段分组，只返回非空组并保持阶段顺序。 */
export function groupByStage(items) {
  const list = Array.isArray(items) ? items : []
  return TRANSFER_STAGE_ORDER
    .map(stage => ({ stage, labelKey: TRANSFER_STAGE_KEYS[stage], items: list.filter(item => transferStage(item) === stage) }))
    .filter(group => group.items.length > 0)
}

/** 按结果分组，只返回非空组并保持顺序。 */
export function groupByOutcome(items) {
  const list = Array.isArray(items) ? items : []
  return TRANSFER_OUTCOME_ORDER
    .map(outcome => ({ outcome, labelKey: TRANSFER_OUTCOME_KEYS[outcome], items: list.filter(item => transferOutcome(item) === outcome) }))
    .filter(group => group.items.length > 0)
}

// v044.5：「已完成」先按 上传/下载 分列，再按结果分组，与「进行中」的分区结构对齐。
// 条目需带 kind（'upload' | 'download'）；缺省视为上传。
export const TRANSFER_KIND_KEYS = { upload: 'files.uploads', download: 'files.downloads' }

/** 按 类型 × 结果 分组：上传在前、下载在后，各自 成功/失败/已取消，只返回非空组。 */
export function groupByKindOutcome(items) {
  const list = Array.isArray(items) ? items : []
  const groups = []
  for (const kind of ['upload', 'download']) {
    for (const outcome of TRANSFER_OUTCOME_ORDER) {
      const matched = list.filter(item => (item?.kind || 'upload') === kind && transferOutcome(item) === outcome)
      if (matched.length > 0) {
        groups.push({ id: `${kind}-${outcome}`, kind, kindKey: TRANSFER_KIND_KEYS[kind], outcome, labelKey: TRANSFER_OUTCOME_KEYS[outcome], items: matched })
      }
    }
  }
  return groups
}

/** 归一化状态文本用于比较（忽略大小写与首尾空白）。 */
export function normalizeStatus(value) {
  return String(value ?? '').trim().toLowerCase()
}

/** 是否是已知状态码（而不是服务端返回的动态错误文本）。 */
export function isTransferStatusCode(value) {
  return Object.prototype.hasOwnProperty.call(TRANSFER_STATUS_KEYS, normalizeStatus(value))
}

/**
 * 把状态码翻成当前语言；未知值按"动态文本"原样返回（例如服务端错误消息），
 * 因此调用方无需再区分"码"与"消息"。checksum 需要 {progress} 参数。
 */
export function statusLabel(value, t, params = {}) {
  const key = TRANSFER_STATUS_KEYS[normalizeStatus(value)]
  return key ? t(key, params) : String(value ?? '')
}

/**
 * 判断上传条目是否已进入终止态（成功/失败/取消）。成功路径必须满足该契约，
 * 否则条目会滞留在「进行中」。终止条件：done、cancelled、failed、progress ≥ 100，
 * 或状态码命中已完成/秒传——判定只看标志与状态码，与界面语言无关。
 */
export function isUploadTerminalState(item) {
  if (!item || typeof item !== 'object') return false
  if (item.done || item.cancelled) return true
  if (Boolean(item.failed)) return true
  if (Number(item.progress) >= 100) return true
  return completedStatuses.has(normalizeStatus(item.status))
}

/** 终止态归类：'success' | 'failed' | 'cancelled' | null。 */
export function uploadTerminalKind(item) {
  if (!isUploadTerminalState(item)) return null
  if (item.cancelled) return 'cancelled'
  if (item.failed) return 'failed'
  return 'success'
}

/** 下载条目终止态：done/completed/cancelled/failed/progress ≥ 100。 */
export function isDownloadTerminalState(item) {
  if (!item || typeof item !== 'object') return false
  if (item.cancelled || item.failed) return true
  if (item.done) return true
  if (Number(item.progress) >= 100) return true
  return completedStatuses.has(normalizeStatus(item.status))
}
