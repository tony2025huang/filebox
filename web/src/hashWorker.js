// hashWorker 在 Worker 线程内计算上传文件的 SHA-256，主线程因此全程不被阻塞（v030 步骤 1 / v031）。
// 快路径是原生 WebCrypto（一次性摘要，仅用于 ≤ directLimit 的文件），大文件走流式哈希：
// 优先 WASM（hash-wasm，单线程 200–500MB/s），WASM 不可用时回退到零依赖的纯 JS 流式实现。
// hashWorker keeps SHA-256 off the main thread. Fast path: the native WebCrypto digest (whole-file
// buffer, only up to directLimit). Larger files stream: WASM (hash-wasm) first, the dependency-free
// pure-JS implementation as fallback.
import { createSHA256 } from 'hash-wasm'
import { createSha256 } from './sha256Fallback.js'

const DEFAULT_DIRECT_LIMIT = 256 * 1024 * 1024
const BLOCK_SIZE = 8 * 1024 * 1024

// createStreamingHasher 返回 { update, digest, engine }。hash-wasm 必须在构建期静态导入：Vite 的 worker
// 产物是单文件，运行时动态 import 会强制代码分割而失败。WASM 初始化异常（例如 CSP 未放行
// 'wasm-unsafe-eval'）时回退到零依赖的纯 JS 流式实现；forceEngine === 'js' 时直接跳过 WASM
// （主线程在 WASM 路径超时后会用这个模式在 Worker 内重试，避免回退到主线程冻结界面）。
// createStreamingHasher returns { update, digest, engine }. hash-wasm must be a static import because
// Vite emits a worker as a single file and a runtime dynamic import would force code splitting. When
// WASM cannot be initialised (for example a CSP without 'wasm-unsafe-eval') it falls back to the
// dependency-free pure-JS implementation; forceEngine === 'js' skips WASM entirely, which the main
// thread uses to retry inside a worker after a WASM-path timeout instead of freezing the main thread.
async function createStreamingHasher(forceEngine) {
  if (forceEngine === 'js') return createSha256()
  try {
    const hasher = await createSHA256()
    return { engine: 'wasm', update: chunk => hasher.update(chunk), digest: () => hasher.digest() }
  } catch {
    return createSha256()
  }
}

function toHex(buffer) {
  return [...new Uint8Array(buffer)].map(value => value.toString(16).padStart(2, '0')).join('')
}

async function streamingHex(file, post, forceEngine) {
  const hasher = await createStreamingHasher(forceEngine)
  for (let offset = 0; offset < file.size; offset += BLOCK_SIZE) {
    const end = Math.min(offset + BLOCK_SIZE, file.size)
    const block = new Uint8Array(await file.slice(offset, end).arrayBuffer())
    hasher.update(block)
    post(Math.round(end / file.size * 100))
  }
  return { hex: await hasher.digest(), engine: hasher.engine }
}

self.onmessage = async event => {
  const { id, file, directLimit, forceEngine } = event.data || {}
  if (!id) return
  const limit = Number(directLimit) || DEFAULT_DIRECT_LIMIT
  const post = value => { try { self.postMessage({ id, type: 'progress', value }) } catch {} }
  try {
    if (!file || typeof file.arrayBuffer !== 'function' || typeof file.size !== 'number') throw new Error('missing file')
    if (file.size <= limit) {
      post(0)
      const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
      post(100)
      self.postMessage({ id, type: 'done', hex: toHex(digest), engine: 'native' })
      return
    }
    const { hex, engine } = await streamingHex(file, post, forceEngine)
    self.postMessage({ id, type: 'done', hex, engine })
  } catch (err) {
    self.postMessage({ id, type: 'error', message: String((err && err.message) || err) })
  }
}
