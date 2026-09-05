// Pure, framework-free transfer flow helpers for FilesView.
// Extracted so folder-confirmation, upload classification, fairness gating and
// rate smoothing can be unit-tested deterministically without mounting Vue.
//
// 与 Vue 无关的纯传输流程辅助：目录确认、上传终态分类、公平启动闸门与速率
// 平滑均在此实现，便于用 node:test 直接做确定性单测。

/** Folder uploads already show one custom confirmation, so the generic
 * >50-file bulk confirmation must never fire again for them. */
export const UPLOAD_BULK_CONFIRM_THRESHOLD = 50

/** Whether the generic bulk confirmation is still required.
 * @param {number} fileCount number of files about to enqueue
 * @param {boolean} folderConfirmed the list already passed a folder confirmation
 * @returns {boolean} true when a second (bulk) confirmation must be shown
 */
export function needsBulkConfirm(fileCount, folderConfirmed) {
  return Boolean(!folderConfirmed && fileCount > UPLOAD_BULK_CONFIRM_THRESHOLD)
}

/** Upload terminal classification. An item is finished when it succeeded
 * (done/progress 100), failed, or was cancelled — completed items must leave
 * the active list and only appear in the Done tab. */
export function isUploadTerminal(item) {
  if (!item) return false
  if (item.done || item.cancelled) return true
  if (Number(item.progress) >= 100) return true
  return Boolean(item.failed)
}

/** Result kind for a terminal upload: 'success' | 'failed' | 'cancelled' | null. */
export function uploadResultKind(item) {
  if (!item) return null
  if (item.cancelled) return 'cancelled'
  if (item.failed) return 'failed'
  if (item.done || Number(item.progress) >= 100) return 'success'
  return null
}

/** Exponential moving-average rate (bytes/s).
 * @param {number} previous previous smoothed rate, 0 when none
 * @param {number} instant newly measured rate
 * @param {number} alpha weight of the new sample (0..1)
 */
export function emaRate(previous, instant, alpha = 0.4) {
  if (!Number.isFinite(instant) || instant < 0) return previous > 0 ? previous : 0
  if (!(previous > 0)) return instant
  return previous * (1 - alpha) + instant * alpha
}

/** Fair, bounded start gate replacing busy-wait polling loops.
 * At most `limit` operations run at once; waiters are granted strictly FIFO.
 * A waiter whose `valid` predicate turns false is resolved as "not granted"
 * without consuming a slot (e.g. the upload was paused/terminated while
 * queued), so no unbounded timer work is created for large batches. */
export class FairStartGate {
  constructor(limit) {
    if (!Number.isInteger(limit) || limit < 1) throw new Error('FairStartGate requires a positive integer limit')
    this.limit = limit
    this.active = 0
    this.waiters = []
  }

  get running() { return this.active }

  get pending() { return this.waiters.length }

  /** Resolve the next batch of FIFO waiters while slots are free. */
  pump() {
    while (this.waiters.length) {
      const head = this.waiters[0]
      if (head.valid && !head.valid()) {
        this.waiters.shift()
        head.resolve(false)
        continue
      }
      if (this.active >= this.limit) break
      this.waiters.shift()
      this.active += 1
      head.resolve(true)
    }
  }

  /** Request a start slot; resolves true when granted, false when the waiter
   * became invalid (paused/failed/terminated) while queued. */
  acquire(valid) {
    return new Promise(resolve => {
      this.waiters.push({ resolve, valid: valid || null })
      this.pump()
    })
  }

  /** Release a granted slot and start the next waiter. */
  release() {
    if (this.active > 0) this.active -= 1
    this.pump()
  }

  /** Re-evaluate waiters after an external state change (pause/terminate). */
  notify() {
    this.pump()
  }
}
