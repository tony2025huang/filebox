// 上传/下载「终止态」契约（v030 #1 最小补丁）：单一来源、可在 node 中断言。
// Terminal-state contract for transfers (v030 #1 minimal patch): single source, node-testable.

const completedStatuses = new Set(['completed', 'instant', 'finished', 'cancelled'])

/** 归一化状态文本用于比较（忽略大小写与首尾空白）。 */
export function normalizeStatus(value) {
  return String(value ?? '').trim().toLowerCase()
}

/**
 * 判断上传条目是否已进入终止态（成功/失败/取消）。成功路径必须满足该契约，
 * 否则条目会滞留在「进行中」。终止条件：done、cancelled、failed、progress ≥ 100、
 * 或状态文本命中已完成/秒传（支持传入本地化后的状态文本）。
 */
export function isUploadTerminalState(item, terminatedLabel = '') {
  if (!item || typeof item !== 'object') return false
  if (item.done || item.cancelled) return true
  if (Boolean(item.failed)) return true
  if (Number(item.progress) >= 100) return true
  const status = normalizeStatus(item.status)
  const labels = Array.isArray(terminatedLabel) ? terminatedLabel : [terminatedLabel]
  if (labels.some(label => label && status === normalizeStatus(label))) return true
  return completedStatuses.has(status)
}

/** 终止态归类：'success' | 'failed' | 'cancelled' | null。 */
export function uploadTerminalKind(item, terminatedLabel = '') {
  if (!isUploadTerminalState(item, terminatedLabel)) return null
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
