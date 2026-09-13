// 客户端哈希策略：超过上限的文件不在浏览器里计算 SHA-256。
// 浏览器 WASM 吞吐差异极大（同一台机器实测比 Node 慢约 9 倍，取决于浏览器/会话的编译档位），
// 把 6GB 级文件的校验压在客户端会拖慢整个上传；改由服务端在组装时一趟算出并在完成响应里回传
// sha256（upload-init 允许不带哈希，complete 响应带 sha256）。代价是这类文件没有上传前的秒传匹配。
// Client hashing policy: files over the limit are not hashed in the browser. Browser WASM throughput
// varies wildly (measured ~9x slower than the same code under Node on one machine), so the server
// computes sha256 while assembling the file and returns it. Such files lose pre-upload instant matching.
export const DEFAULT_CLIENT_HASH_LIMIT = 1024 * 1024 * 1024

// clientHashLimit 返回当前生效的客户端哈希上限，可用 globalThis.FILEBOX_CLIENT_HASH_MAX 覆盖（字节）。
// clientHashLimit returns the effective limit in bytes; globalThis.FILEBOX_CLIENT_HASH_MAX overrides it.
export function clientHashLimit() {
  const override = Number(globalThis.FILEBOX_CLIENT_HASH_MAX)
  return Number.isFinite(override) && override > 0 ? override : DEFAULT_CLIENT_HASH_LIMIT
}

// shouldSkipClientHash 判定该大小的文件是否跳过客户端哈希（严格大于上限）。
// shouldSkipClientHash reports whether a file of this size skips client hashing (strictly above the limit).
export function shouldSkipClientHash(bytes) {
  const size = Number(bytes)
  return Number.isFinite(size) && size > clientHashLimit()
}
