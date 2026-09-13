import { createSHA256 } from 'hash-wasm'
import { createSha256 } from '../src/sha256Fallback.js'
import { createHash } from 'node:crypto'

const SIZE = 256 * 1024 * 1024
const bytes = new Uint8Array(SIZE)
for (let i = 0; i < SIZE; i += 65536) bytes[i] = i & 0xff
const blob = new Blob([bytes])

async function timeIt(label, run) {
  const started = performance.now()
  const hex = await run()
  const seconds = (performance.now() - started) / 1000
  console.log(`${label.padEnd(46)} ${(SIZE / 1024 / 1024 / seconds).toFixed(1).padStart(7)} MB/s   ${seconds.toFixed(2)}s   ${String(hex).slice(0, 12)}`)
}

async function wasmBlocks(blockSize) {
  const hasher = await createSHA256()
  for (let offset = 0; offset < SIZE; offset += blockSize) {
    hasher.update(bytes.subarray(offset, Math.min(offset + blockSize, SIZE)))
  }
  return hasher.digest()
}

async function jsBlocks(blockSize) {
  const hasher = createSha256()
  for (let offset = 0; offset < SIZE; offset += blockSize) {
    hasher.update(bytes.subarray(offset, Math.min(offset + blockSize, SIZE)))
  }
  return hasher.digest()
}

// Blob slice -> ArrayBuffer: the per-block async read the worker performs for every block.
async function wasmViaBlob(blockSize) {
  const hasher = await createSHA256()
  for (let offset = 0; offset < SIZE; offset += blockSize) {
    const chunk = new Uint8Array(await blob.slice(offset, Math.min(offset + blockSize, SIZE)).arrayBuffer())
    hasher.update(chunk)
  }
  return hasher.digest()
}

console.log('--- hashing ceilings on this machine (256 MiB in-memory, Node/V8 = same engine as browser) ---')
await timeIt('hash-wasm (WASM) 8 MiB blocks', () => wasmBlocks(8 * 1024 * 1024))
await timeIt('hash-wasm (WASM) 32 MiB blocks', () => wasmBlocks(32 * 1024 * 1024))
await timeIt('hash-wasm (WASM) 64 MiB blocks', () => wasmBlocks(64 * 1024 * 1024))
await timeIt('pure JS fallback 8 MiB blocks', () => jsBlocks(8 * 1024 * 1024))
await timeIt('node:crypto / OpenSSL one-shot (native ref)', async () => createHash('sha256').update(bytes).digest('hex'))
await timeIt('WASM + Blob.slice().arrayBuffer() 8 MiB', () => wasmViaBlob(8 * 1024 * 1024))
await timeIt('WASM + Blob.slice().arrayBuffer() 64 MiB', () => wasmViaBlob(64 * 1024 * 1024))
console.log('diag: engine fields ->', 'wasm=' + (await createSHA256()).constructor?.name, 'js=' + createSha256().engine)
