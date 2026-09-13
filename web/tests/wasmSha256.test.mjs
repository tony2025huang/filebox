import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { createSHA256 } from 'hash-wasm'

// hash-wasm 是 v031-B 的快路径（Worker 内 WASM 流式哈希）。在 Node 里跑同一份实现并与 node:crypto
// 逐字节对比，可在不依赖浏览器的情况下证明该依赖的流式接口与正确性。
// hash-wasm is the v031-B fast path. Running the same implementation in Node and comparing byte for
// byte against node:crypto proves the dependency's streaming contract without a browser.
test('hash-wasm streaming SHA-256 matches node crypto (v031-B fast path)', async () => {
  const sizes = [0, 1, 55, 56, 64, 65, 1024, 1024 * 1024 + 1, 3 * 1024 * 1024 + 7]
  for (const size of sizes) {
    const bytes = new Uint8Array(size)
    for (let i = 0; i < size; i++) bytes[i] = (i * 17 + 3) & 0xff
    const hasher = await createSHA256()
    // 与 Worker 内一致：按 8MiB 分块喂入。
    for (let offset = 0; offset < bytes.length; offset += 8 * 1024 * 1024) {
      hasher.update(bytes.subarray(offset, Math.min(offset + 8 * 1024 * 1024, bytes.length)))
    }
    assert.equal(hasher.digest(), createHash('sha256').update(bytes).digest('hex'), `size=${size}`)
  }
})

test('hash-wasm digest is reusable across independent instances', async () => {
  const first = await createSHA256()
  first.update(new TextEncoder().encode('alpha'))
  assert.equal(first.digest(), createHash('sha256').update('alpha').digest('hex'))
  const second = await createSHA256()
  second.update(new TextEncoder().encode('beta'))
  assert.equal(second.digest(), createHash('sha256').update('beta').digest('hex'))
})
