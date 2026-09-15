// 客户端哈希策略：超过上限的文件不在浏览器里计算 SHA-256。
// 浏览器 WASM 吞吐差异极大（同一台机器实测比 Node 慢约 9 倍，取决于浏览器/会话的编译档位），
// 把 6GB 级文件的校验压在客户端会拖慢整个上传；改由服务端在组装时一趟算出并在完成响应里回传
// sha256（upload-init 允许不带哈希，complete 响应带 sha256）。代价是这类文件没有上传前的秒传匹配。
// Client hashing policy: files over the limit are not hashed in the browser. Browser WASM throughput
// varies wildly (measured ~9x slower than the same code under Node on one machine), so the server
// computes sha256 while assembling the file and returns it. Such files lose pre-upload instant matching.
//
// 两个阈值都由服务端设置下发（经公开的 /api/brand，收集页未登录上传者也拿得到），见
// applyServerHashLimits；globalThis 覆盖保留给本地调试与单测；服务端保证直算上限 ≤ 总上限。
// Both thresholds are published by the server through the public /api/brand payload so that anonymous
// collection uploaders get them too; globalThis overrides remain for debugging and tests.

// 直算上限：不超过它的文件用原生 WebCrypto（快，但会把整个文件读进内存）。
export const DEFAULT_DIRECT_HASH_LIMIT = 256 * 1024 * 1024
// 总上限：严格超过它的文件整个跳过客户端哈希。
export const DEFAULT_CLIENT_HASH_LIMIT = 1024 * 1024 * 1024

// positiveNumber 把配置值解析为正数；非法值返回 0，表示"不覆盖"。
function positiveNumber(value) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

// hashDirectLimit 返回当前生效的直算上限（字节），可用 globalThis.FILEBOX_HASH_DIRECT_LIMIT 覆盖。
// hashDirectLimit returns the effective native limit in bytes; globalThis.FILEBOX_HASH_DIRECT_LIMIT overrides it.
export function hashDirectLimit() {
  return positiveNumber(globalThis.FILEBOX_HASH_DIRECT_LIMIT) || DEFAULT_DIRECT_HASH_LIMIT
}

// clientHashLimit 返回当前生效的客户端哈希上限，可用 globalThis.FILEBOX_CLIENT_HASH_MAX 覆盖（字节）。
// clientHashLimit returns the effective limit in bytes; globalThis.FILEBOX_CLIENT_HASH_MAX overrides it.
export function clientHashLimit() {
  return positiveNumber(globalThis.FILEBOX_CLIENT_HASH_MAX) || DEFAULT_CLIENT_HASH_LIMIT
}

// shouldSkipClientHash 判定该大小的文件是否跳过客户端哈希（严格大于上限）。
// shouldSkipClientHash reports whether a file of this size skips client hashing (strictly above the limit).
export function shouldSkipClientHash(bytes) {
  const size = Number(bytes)
  return Number.isFinite(size) && size > clientHashLimit()
}

// applyServerHashLimits 用服务端下发的阈值覆盖内置默认值；字段缺失或非法时保持现状。
// applyServerHashLimits applies the thresholds published by the server; missing or invalid fields
// leave the current values untouched.
export function applyServerHashLimits(config = {}) {
  const direct = positiveNumber(config.hashDirectLimitBytes)
  const client = positiveNumber(config.hashClientLimitBytes)
  if (direct > 0) globalThis.FILEBOX_HASH_DIRECT_LIMIT = direct
  if (client > 0) globalThis.FILEBOX_CLIENT_HASH_MAX = client
}
