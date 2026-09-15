import test from 'node:test'
import assert from 'node:assert/strict'
import { applyServerHashLimits, clientHashLimit, DEFAULT_CLIENT_HASH_LIMIT, DEFAULT_DIRECT_HASH_LIMIT, hashDirectLimit, shouldSkipClientHash } from '../src/hashPolicy.js'

// 阈值策略是纯函数，单独覆盖边界；api.js 依据它决定是否完全跳过客户端哈希（v036）。
// The threshold policy is pure so its boundaries are covered directly; api.js uses it to skip the
// client hash entirely (v036).
test('default client hash limit is 1 GiB', () => {
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
  assert.equal(DEFAULT_CLIENT_HASH_LIMIT, 1024 * 1024 * 1024)
  assert.equal(clientHashLimit(), DEFAULT_CLIENT_HASH_LIMIT)
})

test('files at or below the limit are hashed on the client', () => {
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
  assert.equal(shouldSkipClientHash(DEFAULT_CLIENT_HASH_LIMIT), false)
  assert.equal(shouldSkipClientHash(512 * 1024 * 1024), false)
})

test('files above the limit skip client hashing', () => {
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
  assert.equal(shouldSkipClientHash(DEFAULT_CLIENT_HASH_LIMIT + 1), true)
  assert.equal(shouldSkipClientHash(6 * 1024 * 1024 * 1024), true)
})

test('the limit can be overridden at runtime', () => {
  globalThis.FILEBOX_CLIENT_HASH_MAX = 64 * 1024 * 1024
  assert.equal(clientHashLimit(), 64 * 1024 * 1024)
  assert.equal(shouldSkipClientHash(65 * 1024 * 1024), true)
  assert.equal(shouldSkipClientHash(63 * 1024 * 1024), false)
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
})

test('invalid input never throws and never skips', () => {
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
  assert.equal(shouldSkipClientHash(undefined), false)
  assert.equal(shouldSkipClientHash('nope'), false)
  globalThis.FILEBOX_CLIENT_HASH_MAX = 'broken'
  assert.equal(clientHashLimit(), DEFAULT_CLIENT_HASH_LIMIT)
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
})

// 直算上限与总上限一样，默认 256 MiB，可被服务端下发的设置覆盖（T3）。
// The direct limit defaults to 256 MiB and, like the overall limit, can be overridden (T3).
test('default direct hash limit is 256 MiB and can be overridden', () => {
  delete globalThis.FILEBOX_HASH_DIRECT_LIMIT
  assert.equal(DEFAULT_DIRECT_HASH_LIMIT, 256 * 1024 * 1024)
  assert.equal(hashDirectLimit(), DEFAULT_DIRECT_HASH_LIMIT)
  globalThis.FILEBOX_HASH_DIRECT_LIMIT = 512 * 1024 * 1024
  assert.equal(hashDirectLimit(), 512 * 1024 * 1024)
  globalThis.FILEBOX_HASH_DIRECT_LIMIT = 'broken'
  assert.equal(hashDirectLimit(), DEFAULT_DIRECT_HASH_LIMIT)
  delete globalThis.FILEBOX_HASH_DIRECT_LIMIT
})

test('server published limits override the embedded defaults', () => {
  delete globalThis.FILEBOX_HASH_DIRECT_LIMIT
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
  applyServerHashLimits({ hashDirectLimitBytes: 512 * 1024 * 1024, hashClientLimitBytes: 4 * 1024 * 1024 * 1024 })
  assert.equal(hashDirectLimit(), 512 * 1024 * 1024)
  assert.equal(clientHashLimit(), 4 * 1024 * 1024 * 1024)
  assert.equal(shouldSkipClientHash(1024 * 1024 * 1024), false)
  assert.equal(shouldSkipClientHash(4 * 1024 * 1024 * 1024 + 1), true)
  delete globalThis.FILEBOX_HASH_DIRECT_LIMIT
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
})

test('missing or invalid published limits keep the current values', () => {
  globalThis.FILEBOX_HASH_DIRECT_LIMIT = 128 * 1024 * 1024
  globalThis.FILEBOX_CLIENT_HASH_MAX = 2 * 1024 * 1024 * 1024
  applyServerHashLimits({})
  applyServerHashLimits()
  applyServerHashLimits({ hashDirectLimitBytes: 0, hashClientLimitBytes: -1 })
  applyServerHashLimits({ hashDirectLimitBytes: 'nope', hashClientLimitBytes: null })
  assert.equal(hashDirectLimit(), 128 * 1024 * 1024)
  assert.equal(clientHashLimit(), 2 * 1024 * 1024 * 1024)
  delete globalThis.FILEBOX_HASH_DIRECT_LIMIT
  delete globalThis.FILEBOX_CLIENT_HASH_MAX
})
