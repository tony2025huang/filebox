import test from 'node:test'
import assert from 'node:assert/strict'
import { DEFAULT_CLIENT_HASH_LIMIT, clientHashLimit, shouldSkipClientHash } from '../src/hashPolicy.js'

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
