import { test } from 'node:test'
import assert from 'node:assert/strict'
import { FairStartGate, emaRate, isUploadTerminal, needsBulkConfirm, uploadResultKind } from '../src/transferFlow.js'

test('needsBulkConfirm: only plain (non-folder-confirmed) batches over 50 need the generic confirm', () => {
  assert.equal(needsBulkConfirm(51, false), true)
  assert.equal(needsBulkConfirm(50, false), false)
  assert.equal(needsBulkConfirm(5000, false), true)
  // Folder selection already showed exactly one custom confirmation: the
  // generic bulk confirmation must be suppressed for any count.
  assert.equal(needsBulkConfirm(51, true), false)
  assert.equal(needsBulkConfirm(5000, true), false)
  assert.equal(needsBulkConfirm(1, true), false)
})

test('isUploadTerminal classifies success, failure, and cancellation', () => {
  assert.equal(isUploadTerminal(null), false)
  assert.equal(isUploadTerminal({}), false)
  assert.equal(isUploadTerminal({ progress: 12, failed: false }), false)
  assert.equal(isUploadTerminal({ done: true }), true)
  assert.equal(isUploadTerminal({ progress: 100 }), true)
  assert.equal(isUploadTerminal({ failed: true, progress: 12 }), true)
  assert.equal(isUploadTerminal({ cancelled: true, progress: 12 }), true)
})

test('uploadResultKind: distinct labels for success/failed/cancelled, null while active', () => {
  assert.equal(uploadResultKind(null), null)
  assert.equal(uploadResultKind({ progress: 12, failed: false, cancelled: false }), null)
  assert.equal(uploadResultKind({ done: true, progress: 100 }), 'success')
  assert.equal(uploadResultKind({ progress: 100 }), 'success')
  assert.equal(uploadResultKind({ failed: true, progress: 30 }), 'failed')
  // Cancellation wins over a stale failure flag.
  assert.equal(uploadResultKind({ failed: true, cancelled: true, progress: 30 }), 'cancelled')
})

test('emaRate smooths without inventing rates', () => {
  assert.equal(emaRate(0, 100), 100)
  assert.equal(emaRate(100, 200), 140) // 100*0.6 + 200*0.4
  assert.equal(emaRate(0, 0), 0)
  assert.equal(emaRate(50, Number.NaN), 50)
  assert.equal(emaRate(50, -1), 50)
})

test('FairStartGate: FIFO and strict concurrency limit', async () => {
  const gate = new FairStartGate(2)
  assert.equal(gate.limit, 2)
  const order = []
  const p1 = gate.acquire(() => true).then(ok => { if (ok) order.push('a') })
  const p2 = gate.acquire(() => true).then(ok => { if (ok) order.push('b') })
  const p3 = gate.acquire(() => true).then(ok => { if (ok) order.push('c') })
  // a and b start immediately; c waits.
  await Promise.resolve()
  await Promise.resolve()
  assert.equal(gate.running, 2)
  assert.equal(gate.pending, 1)
  gate.release()
  await Promise.resolve()
  await Promise.resolve()
  assert.equal(gate.running, 2)
  await Promise.all([p1, p2, p3])
  assert.deepEqual(order, ['a', 'b', 'c'])
})

test('FairStartGate: invalid waiters resolve false without consuming a slot', async () => {
  const gate = new FairStartGate(1)
  let valid = true
  const granted = []
  const a = gate.acquire(() => valid)
  const b = gate.acquire(() => valid)
  // First slot goes to a; b waits. Now b becomes invalid (e.g. paused):
  valid = false
  gate.notify()
  const [aOk, bOk] = await Promise.all([a, b])
  assert.equal(aOk, true)
  assert.equal(bOk, false)
  assert.equal(gate.pending, 0)
  gate.release()
  assert.equal(gate.running, 0)
})

test('FairStartGate: re-acquire after release respects remaining capacity', async () => {
  const gate = new FairStartGate(1)
  const first = await gate.acquire(() => true)
  assert.equal(first, true)
  const second = gate.acquire(() => true)
  gate.release()
  const secondOk = await second
  assert.equal(secondOk, true)
})
