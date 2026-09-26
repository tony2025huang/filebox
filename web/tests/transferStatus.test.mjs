// 传输状态契约回归（v044.2）：状态判定只看标志与**稳定状态码**，与界面语言无关；
// 展示层再由 statusLabel 翻译。旧实现把本地化文案存进 item.status，切语言后既不会重译，
// 判定也会随语言漂移，这里把它固定住。
// Transfer status contract: terminal detection reads flags and stable status codes only, so it can
// never drift with the active language; the UI translates codes through statusLabel.
import assert from 'node:assert/strict'
import test from 'node:test'

import { TRANSFER_KIND_KEYS, TRANSFER_OUTCOME_KEYS, TRANSFER_OUTCOME_ORDER, TRANSFER_STAGE_KEYS, TRANSFER_STAGE_ORDER, TRANSFER_STATUS_KEYS, canAdoptReselectedFile, groupByKindOutcome, groupByOutcome, groupByStage, isDownloadTerminalState, isTransferStatusCode, isUploadTerminalState, progressHint, statusLabel, transferOutcome, transferStage, uploadTerminalKind } from '../src/transferStatus.js'
import { dictionaries } from '../src/i18n.js'

function translate(dict) {
  return (key, params = {}) => String(dict[key] ?? key).replace(/\{(\w+)\}/g, (_, name) => params[name] ?? `{${name}}`)
}

test('成功上传必然进入终止态（done / progress 100 / completed / instant）', () => {
  assert.equal(isUploadTerminalState({ done: true, progress: 100, status: 'completed' }), true)
  assert.equal(isUploadTerminalState({ progress: 100, status: 'checking' }), true)
  assert.equal(isUploadTerminalState({ status: 'completed' }), true)
  assert.equal(isUploadTerminalState({ status: 'instant' }), true)
  assert.equal(isUploadTerminalState({ status: 'Completed' }), true, '状态码比较忽略大小写')
})

test('失败与取消归类为终止态且类别正确', () => {
  assert.equal(uploadTerminalKind({ failed: true, status: '' }), 'failed')
  assert.equal(uploadTerminalKind({ cancelled: true, status: 'cancelled' }), 'cancelled')
  assert.equal(uploadTerminalKind({ done: true, status: 'completed' }), 'success')
  assert.equal(uploadTerminalKind({}), null)
})

test('进行中的条目不得被判为终止态', () => {
  for (const code of ['preparing', 'uploading', 'checksum', 'checking', 'downloading', 'paused', 'terminating']) {
    assert.equal(isUploadTerminalState({ progress: 42, status: code }), false, `${code} 不应是终止态`)
  }
  assert.equal(isUploadTerminalState(null), false)
  assert.equal(uploadTerminalKind({ progress: 10, status: 'uploading' }), null)
})

test('终止判定与界面语言无关（本地化文案不再参与判定）', () => {
  // 旧实现需要把 t('files.completed') 传进来才能判定；现在传入任何语言的文案都不影响判定，
  // 因为语言相关的只是 statusLabel 的输出，判定只看标志与状态码。
  assert.equal(isUploadTerminalState({ status: '已完成' }), false, '本地化文案不再被当作状态码')
  assert.equal(isUploadTerminalState({ done: true, status: '已完成' }), true, '标志位仍然决定终止态')
  assert.equal(isTransferStatusCode('已完成'), false)
  assert.equal(isTransferStatusCode('completed'), true)
})

test('每个状态码在三语字典中都有对应文案', () => {
  for (const [locale, dict] of Object.entries(dictionaries)) {
    for (const [code, key] of Object.entries(TRANSFER_STATUS_KEYS)) {
      assert.ok(key in dict, `${locale} 缺少 ${key}（状态码 ${code}）`)
    }
  }
})

test('statusLabel 会翻译状态码，并原样保留动态文本', () => {
  for (const [locale, dict] of Object.entries(dictionaries)) {
    const t = translate(dict)
    for (const code of Object.keys(TRANSFER_STATUS_KEYS)) {
      const label = statusLabel(code, t, { progress: 30 })
      assert.notEqual(label, code, `${locale} 的状态码 ${code} 未翻译`)
      assert.ok(label.length > 0, `${locale} 的状态码 ${code} 为空`)
    }
    assert.match(statusLabel('checksum', t, { progress: 30 }), /30/, `${locale} 的校验进度未代入`)
  }
  const t = translate(dictionaries['zh-CN'])
  assert.equal(statusLabel('服务器返回的错误消息', t), '服务器返回的错误消息')
  assert.equal(statusLabel('', t), '')
})

test('下载终止态与上传契约一致（避免两份定义漂移）', () => {
  assert.equal(isDownloadTerminalState({ done: true }), true)
  assert.equal(isDownloadTerminalState({ progress: 100 }), true)
  assert.equal(isDownloadTerminalState({ failed: true }), true)
  assert.equal(isDownloadTerminalState({ cancelled: true }), true)
  assert.equal(isDownloadTerminalState({ status: 'cancelled' }), true)
  assert.equal(isDownloadTerminalState({ progress: 88, status: 'downloading' }), false)
})

// v044.4：进行中按阶段分组、已完成按结果分组；分组只是状态码的归并，必须覆盖全部状态码。
test('进行中条目按状态码归入准备/传输/处理/暂停四个阶段', () => {
  const cases = {
    preparing: ['preparing', 'checksum'],
    transferring: ['uploading', 'downloading', 'terminating'],
    processing: ['checking'],
    paused: ['paused', 'need_reselect']
  }
  for (const [stage, codes] of Object.entries(cases)) {
    for (const code of codes) assert.equal(transferStage({ status: code }), stage, `${code} 应归入 ${stage}`)
  }
  // 未知/空状态必须仍然可见（按传输中处理），不能从列表里消失。
  assert.equal(transferStage({ status: 'brand_new_code' }), 'transferring')
  assert.equal(transferStage({}), 'transferring')
  assert.equal(transferStage(null), 'transferring')
})

test('每个状态码：非终止码落到某个阶段，终止码不属于任何阶段', () => {
  for (const code of Object.keys(TRANSFER_STATUS_KEYS)) {
    const stage = transferStage({ status: code })
    const terminal = isUploadTerminalState({ status: code })
    if (terminal) assert.equal(stage, '', `终止码 ${code} 不应归入任何阶段（否则会被算成"传输中"）`)
    else assert.ok(TRANSFER_STAGE_ORDER.includes(stage), `非终止码 ${code} 未归入任何阶段`)
  }
  // 已完成的条目绝不能出现在任何阶段分组里
  const done = { status: 'completed', done: true, progress: 100 }
  assert.equal(transferStage(done), '')
  assert.deepEqual(groupByStage([done, { status: 'uploading' }]).map(g => g.items.length), [1])
})

test('分组只返回非空组并保持顺序', () => {
  const items = [
    { id: 'a', status: 'uploading' },
    { id: 'b', status: 'checksum' },
    { id: 'c', status: 'paused' },
    { id: 'd', status: 'uploading' }
  ]
  const groups = groupByStage(items)
  assert.deepEqual(groups.map(group => group.stage), ['preparing', 'transferring', 'paused'])
  assert.deepEqual(groups.map(group => group.items.map(item => item.id)), [['b'], ['a', 'd'], ['c']])
  assert.equal(groupByStage([]).length, 0)
  assert.equal(groupByStage(undefined).length, 0)
})

test('已完成条目按成功/失败/取消分组', () => {
  assert.equal(transferOutcome({ cancelled: true }), 'cancelled')
  assert.equal(transferOutcome({ failed: true }), 'failed')
  assert.equal(transferOutcome({ done: true }), 'success')
  const groups = groupByOutcome([
    { id: 'ok' }, { id: 'bad', failed: true }, { id: 'stop', cancelled: true }, { id: 'ok2' }
  ])
  assert.deepEqual(groups.map(group => group.outcome), TRANSFER_OUTCOME_ORDER)
  assert.deepEqual(groups.map(group => group.items.map(item => item.id)), [['ok', 'ok2'], ['bad'], ['stop']])
})

test('阶段与结果分组的标题键在三语字典中都有', () => {
  for (const [locale, dict] of Object.entries(dictionaries)) {
    for (const key of Object.values(TRANSFER_STAGE_KEYS)) {
      assert.ok(key in dict, `${locale} 缺少阶段标题 ${key}`)
    }
    for (const key of Object.values(TRANSFER_OUTCOME_KEYS)) {
      assert.ok(key in dict, `${locale} 缺少结果标题 ${key}`)
    }
    for (const key of Object.values(TRANSFER_KIND_KEYS)) {
      assert.ok(key in dict, `${locale} 缺少类型标题 ${key}`)
    }
  }
})

// v044.9：已完成的条目不得被当作"待重选"去承接用户重新选择的文件，否则重选会被静默吞掉。
test('假"已完成"记录不得吞掉重新选择的文件', () => {
  const stuck = { needsReselect: true, done: true, progress: 100, status: 'completed', name: 'iso', size: 6482409472 }
  assert.equal(canAdoptReselectedFile(stuck), false)
  // 只清了 done、进度仍是 100 的旧记录同样不行：终止态契约把 progress ≥ 100 也算终止。
  assert.equal(canAdoptReselectedFile({ needsReselect: true, progress: 100 }), false)
  assert.equal(canAdoptReselectedFile({ needsReselect: true, cancelled: true }), false)
  assert.equal(canAdoptReselectedFile({ needsReselect: true, failed: true }), false)
  // 真正的续传条目（待重选、进度未满）可以承接。
  assert.equal(canAdoptReselectedFile({ needsReselect: true, done: false, progress: 40, status: 'need_reselect' }), true)
  // 没标记待重选的条目不参与匹配（例如正在进行的传输）。
  assert.equal(canAdoptReselectedFile({ progress: 30, status: 'uploading' }), false)
  assert.equal(canAdoptReselectedFile(null), false)
})

// v044.8：服务端进度样本只是提示，绝不能把条目推到 100%（否则会被判成完成并显示"成功"）。
test('进度样本永远不会把条目推到 100%', () => {
  // 极端情况：样本声称 1546/1546 片、字节也全齐（旧实现正是这样报"有行无文件"的任务）
  const full = progressHint(0, { uploaded: 1546, totalChunks: 1546, uploadedBytes: 6482409472, totalBytes: 6482409472 })
  assert.equal(full, 99)
  const item = { status: 'uploading', progress: full, failed: false, cancelled: false, done: false }
  assert.equal(isUploadTerminalState(item), false, '进度样本不得让条目进入终止态')
  // 正常部分进度：按 25–99 区间推导，且不回退已有进度
  assert.equal(progressHint(0, { uploaded: 1546, totalChunks: 1546 * 2 }), Math.round(25 + 0.5 * 75))
  assert.equal(progressHint(40, { uploaded: 1, totalChunks: 1546, uploadedBytes: 4194304, totalBytes: 6482409472 }), 40)
  // 无有效样本时保持原值；异常输入不抛错
  assert.equal(progressHint(7, {}), 7)
  assert.equal(progressHint(0, { uploaded: -5, totalChunks: 0, uploadedBytes: NaN, totalBytes: 0 }), 0)
  assert.equal(progressHint(12, undefined), 12)
})
test('已完成条目按 类型×结果 分组（上传在前，各自 成功/失败/已取消）', () => {
  const items = [
    { kind: 'download', id: 'd-ok' },
    { kind: 'upload', id: 'u-bad', failed: true },
    { kind: 'upload', id: 'u-ok1' },
    { kind: 'download', id: 'd-stop', cancelled: true },
    { kind: 'upload', id: 'u-ok2' },
    { kind: 'upload', id: 'u-stop', cancelled: true }
  ]
  const groups = groupByKindOutcome(items)
  assert.deepEqual(groups.map(group => group.id), [
    'upload-success', 'upload-failed', 'upload-cancelled', 'download-success', 'download-cancelled'
  ])
  assert.deepEqual(groups.map(group => group.items.map(item => item.id)), [
    ['u-ok1', 'u-ok2'], ['u-bad'], ['u-stop'], ['d-ok'], ['d-stop']
  ])
  // 缺省 kind 视为上传；空输入返回空数组。
  assert.deepEqual(groupByKindOutcome([{ id: 'x' }]).map(group => group.id), ['upload-success'])
  assert.equal(groupByKindOutcome([]).length, 0)
  assert.equal(groupByKindOutcome(undefined).length, 0)
})
