import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'

// 在 Node 里用桩 self 驱动 Worker 模块，覆盖 native / WASM 流式两条实现路径与错误分支。
// 注意：forceEngine（强制纯 JS 重试）属于已回退的 v032 变更，这里不再断言。
// Drives the worker module in Node with a stubbed `self`, covering the native and WASM streaming paths
// plus the error branch. forceEngine (the v032 forced-JS retry) was rolled back and is not asserted.
const messages = []
globalThis.self = { postMessage: message => messages.push(message), onmessage: null }
await import('../src/hashWorker.js')

const SIZE = 6 * 1024 * 1024 + 7
const bytes = new Uint8Array(SIZE)
for (let i = 0; i < SIZE; i++) bytes[i] = (i * 13 + 5) & 0xff
const want = createHash('sha256').update(bytes).digest('hex')

// file 桩只实现 Worker 真正用到的接口：size / slice().arrayBuffer() / arrayBuffer()。
const file = {
  size: SIZE,
  slice: (start, end) => ({ arrayBuffer: async () => bytes.subarray(start, end) }),
  arrayBuffer: async () => bytes.subarray(0, SIZE)
}

async function run(id, payload) {
  messages.length = 0
  await globalThis.self.onmessage({ data: { id, file, ...payload } })
  return {
    done: messages.find(message => message.type === 'done'),
    error: messages.find(message => message.type === 'error'),
    progress: messages.filter(message => message.type === 'progress')
  }
}

test('worker native path reports engine=native and the right digest', async () => {
  const { done, error } = await run('native', { directLimit: SIZE * 2 })
  assert.equal(error, undefined)
  assert.equal(done.engine, 'native')
  assert.equal(done.hex, want)
})

test('worker streaming path prefers WASM, reports progress and matches node:crypto', async () => {
  const { done, error, progress } = await run('stream', { directLimit: 1024 })
  assert.equal(error, undefined)
  assert.equal(done.engine, 'wasm', 'hash-wasm should be the streaming engine')
  assert.equal(done.hex, want)
  assert.ok(progress.length > 0, 'streaming must report progress')
})

test('worker rejects a message without a usable file', async () => {
  messages.length = 0
  await globalThis.self.onmessage({ data: { id: 'bad', file: { size: 1 }, directLimit: 0 } })
  assert.equal(messages.find(message => message.type === 'error') !== undefined, true)
})
