import { createSHA256 } from 'hash-wasm'
import { createSha256 } from '../src/sha256Fallback.js'

// Node 与浏览器同为 V8，可用同一份实现横向比较 WASM 与纯 JS 流式 SHA-256 的吞吐（v031 基准）。
// Node and the browser share V8, so the same implementations give a fair WASM vs pure-JS throughput
// comparison for the v031 baseline.
const SIZE = 64 * 1024 * 1024
const BLOCK = 8 * 1024 * 1024
const bytes = new Uint8Array(SIZE)
for (let i = 0; i < SIZE; i += 4096) bytes[i] = i & 0xff

async function measure(label, factory) {
  // 预热一次，避免把 WASM 编译/实例化时间算进吞吐。
  const warm = await factory()
  for (let offset = 0; offset < SIZE; offset += BLOCK) warm.update(bytes.subarray(offset, Math.min(offset + BLOCK, SIZE)))
  warm.digest()
  const started = performance.now()
  const hasher = await factory()
  for (let offset = 0; offset < SIZE; offset += BLOCK) hasher.update(bytes.subarray(offset, Math.min(offset + BLOCK, SIZE)))
  const hex = hasher.digest()
  const seconds = (performance.now() - started) / 1000
  const mbps = (SIZE / 1024 / 1024) / seconds
  console.log(`${label}: ${seconds.toFixed(2)}s  ${mbps.toFixed(1)} MB/s  hex=${hex.slice(0, 16)}…`)
  return { seconds, mbps, hex }
}

const wasm = await measure('hash-wasm (WASM, worker fast path)', () => createSHA256())
const js = await measure('sha256Fallback (pure JS, fallback)', async () => createSha256())
console.log(`speedup: ${(wasm.mbps / js.mbps).toFixed(1)}x`)
console.log(`1GiB estimate: WASM ${(1024 / wasm.mbps).toFixed(1)}s, JS ${(1024 / js.mbps).toFixed(0)}s`)
