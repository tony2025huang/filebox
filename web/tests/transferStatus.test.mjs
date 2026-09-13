// v030 #1 契约回归：断言成功路径必然进入终止态、失败/取消归类正确、进行中不误判。
// v030 #1 contract regression: the success path must be terminal, failed/cancelled classify
// correctly, and in-flight items are never treated as terminal.
//
// 说明：状态文本是本地化文案，因此调用方需把"已完成/秒传完成"传入 terminatedLabels；
// 真实路径（FilesView）按 t('files.completed') / t('files.instantUpload') 传入。
// Localized status text must be supplied via terminatedLabels by the caller, which FilesView does.
import assert from 'node:assert/strict'
import test from 'node:test'

import { isUploadTerminalState, isDownloadTerminalState, uploadTerminalKind } from '../src/transferStatus.js'

const labels = ['已完成', '秒传完成']

test('成功上传必然进入终止态（done / progress 100 / 已完成 / 秒传）', () => {
  assert.equal(isUploadTerminalState({ done: true, progress: 100, status: '已完成' }, labels), true)
  assert.equal(isUploadTerminalState({ progress: 100, status: '校验中' }, labels), true)
  assert.equal(isUploadTerminalState({ status: '已完成' }, labels), true)
  assert.equal(isUploadTerminalState({ status: '秒传完成' }, labels), true)
  assert.equal(isUploadTerminalState({ status: 'instant' }, labels), true)
  assert.equal(isUploadTerminalState({ status: 'Completed' }, labels), true)
  // 未传标签时本地化文案不应被猜测命中（避免与后端英文状态混淆）。
  assert.equal(isUploadTerminalState({ status: '已完成' }), false)
})

test('失败与取消归类为终止态且类别正确', () => {
  assert.equal(uploadTerminalKind({ failed: true, status: '网络异常' }, labels), 'failed')
  assert.equal(uploadTerminalKind({ cancelled: true }, labels), 'cancelled')
  assert.equal(uploadTerminalKind({ done: true, status: '已完成' }, labels), 'success')
  assert.equal(uploadTerminalKind({}, labels), null)
})

test('进行中的条目不得被判为终止态', () => {
  assert.equal(isUploadTerminalState({ progress: 42, status: '上传中' }, labels), false)
  assert.equal(isUploadTerminalState({ progress: 99, status: '校验中' }, labels), false)
  assert.equal(isUploadTerminalState({ progress: 0, status: '排队中' }, labels), false)
  assert.equal(isUploadTerminalState(null, labels), false)
  assert.equal(uploadTerminalKind({ progress: 10 }, labels), null)
})

test('下载终止态与上传契约一致（避免两份定义漂移）', () => {
  assert.equal(isDownloadTerminalState({ done: true }), true)
  assert.equal(isDownloadTerminalState({ progress: 100 }), true)
  assert.equal(isDownloadTerminalState({ failed: true }), true)
  assert.equal(isDownloadTerminalState({ cancelled: true }), true)
  assert.equal(isDownloadTerminalState({ progress: 88, status: '下载中' }), false)
})
