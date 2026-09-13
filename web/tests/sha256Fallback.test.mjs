import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { createSha256, IncrementalSHA256 } from '../src/sha256Fallback.js'

// reference 用 Node 的 OpenSSL 实现作为标准答案，验证自研流式 SHA-256 的正确性（v031-A）。
// reference uses node:crypto as the standard answer for the hand-written streaming SHA-256 (v031-A).
const reference = bytes => createHash('sha256').update(bytes).digest('hex')

test('streaming SHA-256 matches node crypto across block boundaries', () => {
  // 覆盖 SHA-256 分组边界：空文件、55/56/57（填充单/双分组）、63/64/65、127/128/129 与跨块累积。
  const sizes = [0, 1, 55, 56, 57, 63, 64, 65, 127, 128, 129, 1024, 1024 * 1024 + 1]
  for (const size of sizes) {
    const bytes = new Uint8Array(size)
    for (let i = 0; i < size; i++) bytes[i] = (i * 31 + 7) & 0xff
    const hasher = new IncrementalSHA256()
    // 故意用非对齐分块（100 字节）喂入，覆盖内部缓冲区的拼接逻辑。
    for (let offset = 0; offset < bytes.length; offset += 100) {
      hasher.update(bytes.subarray(offset, Math.min(offset + 100, bytes.length)))
    }
    assert.equal(hasher.hexDigest(), reference(bytes), `size=${size}`)
  }
})

test('createSha256 exposes the streaming contract shared with the worker', () => {
  const bytes = new TextEncoder().encode('filebox-upload-checksum')
  const hasher = createSha256()
  hasher.update(bytes.subarray(0, 3))
  hasher.update(bytes.subarray(3, 10))
  hasher.update(bytes.subarray(10))
  assert.equal(hasher.digest(), reference(bytes))
  assert.equal(hasher.engine, 'js')
})

test('incremental hasher never retains a full-file copy', () => {
  const hasher = new IncrementalSHA256()
  const block = new Uint8Array(64 * 1024).fill(7)
  for (let i = 0; i < 32; i++) hasher.update(block)
  assert.ok(hasher.buffer.length <= 64, 'internal buffer must stay at one SHA-256 block')
})
