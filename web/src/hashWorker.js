// hashWorker 在 Worker 线程内计算上传文件的 SHA-256，主线程因此全程不被阻塞（v030 步骤 1 / v031）。
// 快路径是原生 WebCrypto（一次性摘要，仅用于 ≤ directLimit 的文件），大文件走流式哈希：
// 优先 WASM（hash-wasm，单线程 200–500MB/s），WASM 不可用时回退到零依赖的纯 JS 流式实现。
// 诊断：结果里附带 readMs/hashMs（读盘与计算各占多少）与 wasmError（WASM 初始化失败的原因），
// 用于区分「WASM 没生效」和「磁盘读取是瓶颈」——这两个结论对应完全不同的优化方向。
// hashWorker keeps SHA-256 off the main thread. Fast path: the native WebCrypto digest (whole-file
// buffer, only up to directLimit). Larger files stream: WASM (hash-wasm) first, the dependency-free
// pure-JS implementation as fallback. The result carries readMs/hashMs and the WASM init error so the
// caller can tell "WASM inactive" apart from "disk read is the bottleneck".
import { createSHA256 } from 'hash-wasm'
import { createSha256 } from './sha256Fallback.js'

const DEFAULT_DIRECT_LIMIT = 256 * 1024 * 1024
const BLOCK_SIZE = 8 * 1024 * 1024

// createStreamingHasher 返回 { update, digest, engine, wasmError }。hash-wasm 必须在构建期静态导入：
// Vite 的 worker 产物是单文件，运行时动态 import 会强制代码分割而失败。WASM 初始化异常（例如 CSP 未放行
// 'wasm-unsafe-eval'）时回退到零依赖的纯 JS 流式实现，并带上失败原因供诊断。
// createStreamingHasher returns { update, digest, engine, wasmError }. hash-wasm must be a static import
// because Vite emits a worker as a single file and a runtime dynamic import would force code splitting.
// When WASM cannot be initialised the dependency-free pure-JS implementation is used and the reason is
// reported for diagnostics.
async function createStreamingHasher() {
  try {
    const hasher = await createSHA256()
    return { engine: 'wasm', wasmError: '', update: chunk => hasher.update(chunk), digest: () => hasher.digest() }
  } catch (err) {
    const fallback = createSha256()
    return { ...fallback, wasmError: String((err && err.message) || err) }
  }
}

function toHex(buffer) {
  return [...new Uint8Array(buffer)].map(value => value.toString(16).padStart(2, '0')).join('')
}

// streamingHex 分别累计读（slice + arrayBuffer）与算（update + digest）的耗时。
// streamingHex accumulates the read (slice + arrayBuffer) and hash (update + digest) time separately.
async function streamingHex(file, post) {
  const hasher = await createStreamingHasher()
  let readMs = 0
  let hashMs = 0
  for (let offset = 0; offset < file.size; offset += BLOCK_SIZE) {
    const end = Math.min(offset + BLOCK_SIZE, file.size)
    const readStarted = Date.now()
    const block = new Uint8Array(await file.slice(offset, end).arrayBuffer())
    readMs += Date.now() - readStarted
    const hashStarted = Date.now()
    hasher.update(block)
    hashMs += Date.now() - hashStarted
    post(Math.round(end / file.size * 100))
  }
  const digestStarted = Date.now()
  const hex = await hasher.digest()
  hashMs += Date.now() - digestStarted
  return { hex, engine: hasher.engine, readMs, hashMs, wasmError: hasher.wasmError || '' }
}

self.onmessage = async event => {
  const { id, file, directLimit } = event.data || {}
  if (!id) return
  const limit = Number(directLimit) || DEFAULT_DIRECT_LIMIT
  const post = value => { try { self.postMessage({ id, type: 'progress', value }) } catch {} }
  try {
    if (!file || typeof file.arrayBuffer !== 'function' || typeof file.size !== 'number') throw new Error('missing file')
    if (file.size <= limit) {
      post(0)
      const started = Date.now()
      const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
      post(100)
      self.postMessage({ id, type: 'done', hex: toHex(digest), engine: 'native', readMs: Date.now() - started, hashMs: 0, wasmError: '' })
      return
    }
    const { hex, engine, readMs, hashMs, wasmError } = await streamingHex(file, post)
    self.postMessage({ id, type: 'done', hex, engine, readMs, hashMs, wasmError })
  } catch (err) {
    self.postMessage({ id, type: 'error', message: String((err && err.message) || err) })
  }
}
