import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'

// 在 Node 里用桩 self 驱动 Worker 模块，覆盖三条实现路径与 engine 上报（v032 诊断）；
// 这样无需浏览器即可验证 native / wasm / 强制纯 JS 的行为与摘要一致性。
// Drives the worker module in Node with a stubbed `self` to cover the three implementations and the
// engine reporting (v032 diagnostics) without a browser.
const messages = []
globalThis.self = { postMessage: message => messages.push(message), onmessage: null }
await import('../src/hashWorker.js')

const SIZE = 6 * 1024 * 1024 + 7
const bytes = new Uint8Array(SIZE)
for (let i = 0; i < SIZE; i++) bytes[i] = (i * 13 + 5) & 0xff
const want = createHash('sha256').update(bytes).digest('hex')

// file 桩只实现 Worker 真正用到的接口：size / slice().arrayBuffer() / arrayBuffer()。
// The stub implements exactly what the worker uses.
const file = {
  size: SIZE,
  slice: (start, end) => ({ arrayBuffer: async () => bytes.subarray(start, end) }),
  arrayBuffer: async () => bytes.subarray(0, SIZE)
}

async function run(id, payload) {
  messages.length = 0
  await globalThis.self.onmessage({ data: { id, file, ...payload } })
  const done = messages.find(message => message.type === 'done')
  const error = messages.find(message => message.type === 'error')
  return { done, error, progress: messages.filter(message => message.type === 'progress') }
}

test('worker native path reports engine=native and the right digest', async () => {
  const { done, error } = await run('native', { directLimit: SIZE * 2 })
  assert.equal(error, undefined)
  assert.equal(done.engine, 'native')
  assert.equal(done.hex, want)
})

test('worker streaming path prefers WASM and matches node:crypto', async () => {
  const { done, error } = await run('stream', { directLimit: 1024 })
  assert.equal(error, undefined)
  assert.equal(done.engine, 'wasm', 'hash-wasm should be the default streaming engine')
  assert.equal(done.hex, want)
})

test('forceEngine=js skips WASM and still matches node:crypto', async () => {
  const { done, error, progress } = await run('forced', { directLimit: 1024, forceEngine: 'js' })
  assert.equal(error, undefined)
  assert.equal(done.engine, 'js')
  assert.equal(done.hex, want)
  assert.ok(progress.length > 0, 'streaming must report progress')
})

test('worker rejects a message without a usable file', async () => {
  messages.length = 0
  await globalThis.self.onmessage({ data: { id: 'bad', file: { size: 1 }, directLimit: 0 } })
  assert.equal(messages.find(message => message.type === 'error') !== undefined, true)
})
