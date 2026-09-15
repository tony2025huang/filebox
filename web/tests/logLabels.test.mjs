import test from 'node:test'
import assert from 'node:assert/strict'
import { ACTION_CODES, ACTION_LABEL_KEYS, REASON_CODES, REASON_LABEL_KEYS, actionLabel, reasonLabel } from '../src/logLabels.js'
import { dictionaries } from '../src/i18n.js'

// 后端会写入的审计码全集：源码里的字面量 ∪ 演示库 audit_logs 实际出现的值（v044 盘点的快照）。
// 这些码出现在日志页的「操作类型 / 失败原因」两列，任何一个缺少三语键都会退回裸露英文。
// Codes the backend writes to audit_logs (source literals plus the values observed in the demo
// database). Every one of them is rendered in the log page, so each needs three translations.
const AUDIT_ACTIONS = [
  'batch_share', 'clear_all', 'collection', 'collection_update', 'delete', 'download', 'folder_delete',
  'login', 'password_change', 'recycle_move', 'recycle_purge', 'register', 'share', 'share_download',
  'share_extend', 'share_group_extend', 'share_group_increase', 'share_group_update', 'share_increase',
  'share_preview', 'share_revoke', 'share_view', 'sync_host_key', 'sync_host_key_update', 'sync_run',
  'upload', 'upload_chunk', 'upload_collect', 'upload_collect_fail', 'upload_init'
]

const AUDIT_REASONS = [
  'add_files', 'batch', 'batch_group', 'changed', 'checksum_mismatch', 'clear_all',
  'collection_file_too_large', 'collection_limit', 'collection_upload', 'create', 'explicit_confirmation',
  'extend', 'folder_delete', 'incomplete', 'increase', 'instant', 'invalid_dir', 'invalid_fingerprint',
  'invalid_index', 'invalid_name', 'invalid_request', 'ip_locked', 'locked', 'not_found', 'prepare_failed',
  'quota_exceeded', 'rate_limited', 'read_only', 'reauth_failed', 'reauth_rate_limited', 'register_disabled',
  'remove_file', 'revoke', 'revoke_group', 'save_failed', 'scope_forbidden', 'settings_failed',
  'share_content_missing', 'share_denied', 'share_expired', 'share_limit', 'share_not_found', 'share_revoked',
  'size_mismatch', 'stale_confirmation', 'success', 'task_not_found', 'too_large', 'totp_failed',
  'unauthorized', 'update', 'update_attributes', 'upload_failed', 'user_disabled', 'user_not_found',
  'write_failed', 'wrong_password'
]

// 用真实的字典实现 t()，测试不再依赖 vue 的响应式状态。
function translate(dict) {
  return (key, params = {}) => String(dict[key] ?? key).replace(/\{(\w+)\}/g, (_, name) => params[name] ?? `{${name}}`)
}

test('every backend audit code has a label mapping', () => {
  for (const code of AUDIT_ACTIONS) {
    assert.ok(ACTION_LABEL_KEYS[code], `action ${code} has no label key`)
  }
  for (const code of AUDIT_REASONS) {
    assert.ok(REASON_LABEL_KEYS[code], `reason ${code} has no label key`)
  }
})

test('every mapped key exists in all three dictionaries', () => {
  for (const [locale, dict] of Object.entries(dictionaries)) {
    for (const key of Object.values(ACTION_LABEL_KEYS)) {
      assert.ok(key in dict, `${locale} is missing ${key}`)
    }
    for (const key of Object.values(REASON_LABEL_KEYS)) {
      assert.ok(key in dict, `${locale} is missing ${key}`)
    }
    assert.ok('logs.unknownAction' in dict, `${locale} is missing logs.unknownAction`)
    assert.ok('logReason.unknown' in dict, `${locale} is missing logReason.unknown`)
    // 日志表头：成功记录也带"原因/说明"，列名必须存在，否则表头会退化成键名（v044）。
    assert.ok('logs.reasonColumn' in dict, `${locale} is missing logs.reasonColumn`)
  }
})

test('all three dictionaries keep the same key set', () => {
  const counts = Object.fromEntries(Object.entries(dictionaries).map(([locale, dict]) => [locale, Object.keys(dict).length]))
  const values = Object.values(counts)
  assert.equal(new Set(values).size, 1, `dictionary sizes differ: ${JSON.stringify(counts)}`)
})

test('known codes never render as raw codes in any locale', () => {
  for (const [locale, dict] of Object.entries(dictionaries)) {
    const t = translate(dict)
    for (const code of ACTION_CODES) {
      const label = actionLabel(code, t)
      assert.notEqual(label, code, `${locale} action ${code} fell through to the raw code`)
      assert.ok(label.length > 0, `${locale} action ${code} is empty`)
    }
    for (const code of REASON_CODES) {
      const label = reasonLabel(code, t)
      assert.notEqual(label, code, `${locale} reason ${code} fell through to the raw code`)
      assert.ok(label.length > 0, `${locale} reason ${code} is empty`)
    }
  }
})

test('unknown codes fall back to a marked placeholder that keeps the code', () => {
  for (const [locale, dict] of Object.entries(dictionaries)) {
    const t = translate(dict)
    const action = actionLabel('brand_new_action', t)
    const reason = reasonLabel('brand_new_reason', t)
    assert.ok(action.includes('brand_new_action'), `${locale} action fallback lost the code: ${action}`)
    assert.ok(reason.includes('brand_new_reason'), `${locale} reason fallback lost the code: ${reason}`)
    assert.notEqual(action, 'brand_new_action')
    assert.notEqual(reason, 'brand_new_reason')
  }
})

test('empty action and reason render as a placeholder', () => {
  const t = translate(dictionaries['zh-CN'])
  assert.equal(actionLabel('', t), '-')
  assert.equal(reasonLabel('', t), '-')
  assert.equal(reasonLabel(undefined, t), '-')
})
