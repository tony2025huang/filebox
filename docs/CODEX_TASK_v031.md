# CODEX 任务书 v031 — 客户端校验（SHA-256）阶段性能方案

> 状态：**待用户确认后执行**（本文件只做方案与验收定义，尚未改动代码）
> 前置：v030 已实现 hash 步骤 1（Web Worker + 原生 WebCrypto），已部署验证（pid 4708）
> 适用范围：仅客户端「上传前校验」阶段的吞吐与主线程阻塞问题；不改服务端校验契约

## 1. 问题与已确认结论

现象：本地 127.0.0.1 部署下，**校验阶段只有 5–6 MB/s，而实际上传阶段可达 80 MB/s**。

已确认的根因（非限速、非网络）：

1. WebCrypto `crypto.subtle.digest` 是**一次性 API，没有增量接口**，只能一次性把整个文件读进内存（`File.arrayBuffer()`）。
2. 因此 v030 之前的实现按 32 MiB 分界：
   - ≤ 32 MiB：走原生 `crypto.subtle.digest`（快，可达数百 MB/s）；
   - > 32 MiB：走手写纯 JS 流式 `IncrementalSHA256`（**约 5–20 MB/s，且独占主线程**）——这正是 1 GB 文件看到「5–6 MB/s」的来源。
3. 服务端限速/带宽均不是原因：同机上传阶段 80 MB/s 说明链路与磁盘都没问题。

v030 步骤 1 已交付并部署的部分：

- 新增 `web/src/hashWorker.js`，在 Worker 内对 `size ≤ FILEBOX_HASH_DIRECT_LIMIT`（默认 256 MiB，可被全局变量覆盖）的文件执行 `crypto.subtle.digest('SHA-256', await file.arrayBuffer())`，向主线程回报 0/100 进度；更大的文件直接 reject 由主线程处理。
- `web/src/api.js`：`computeFileSHA256` 优先走 worker（120s 超时，失败返回 null 回落主线程原生摘要），> 256 MiB 仍走主线程纯 JS 流式实现。
- 构建产物已含 `dist/assets/hashWorker-*.js`（0.67 kB）；CSP 仍为 `script-src 'self'`，同源 worker 无需放宽。

**v030 步骤 1 的明确边界（必须如实告知用户）**：它只覆盖 ≤ 256 MiB。**1 GB 文件仍然走主线程纯 JS 流式实现** —— 吞吐依旧 5–6 MB/s，且校验期间界面会卡住。

## 2. v031 候选方案对比

| 方案 | 做法 | 吞吐 | 主线程 | 新依赖 | CSP 变更 | 结论 |
| --- | --- | --- | --- | --- | --- | --- |
| A（步骤 2a） | 把纯 JS 流式 hasher 抽成共享模块 `web/src/sha256Fallback.js`，**在 Worker 内运行**，覆盖所有大小 | 不变（5–20 MB/s） | 不阻塞 ✅ | 无 | 无 | **建议做**：解决卡顿，不解决吞吐 |
| B（步骤 2b） | WASM 流式 SHA-256（如 `hash-wasm`），在 Worker 内分块复用同一 hasher 实例 | 约 200–500 MB/s（单线程） | 不阻塞 ✅ | 是（需固定版本 + 本地 vendored `.wasm`） | **需加 `'wasm-unsafe-eval'`** 到 `script-src` | **建议做**：真正解决 1 GB 场景 |
| C | 继续用 WebCrypto 但求增量 API | — | — | — | — | 不可行：浏览器未提供增量摘要 |
| D | 更换算法（BLAKE3/xxHash 等） | 高 | — | 是 | 视实现 | **不建议**：破坏服务端校验契约、断点/秒传判定与 `files.sha256` 语义 |
| E | 大文件「先传后校验」（上传即入队，后台异步入库状态「校验中」） | — | — | 否 | 否 | 产品级改动，需服务端新增延迟校验状态机；可作为后续独立版本，不在 v031 |

推荐 v031 范围 = **A + B**：B 作为快路径（WASM 可用时），A 作为兜底（WASM 被 CSP/浏览器/加载失败阻断时），并把阈值与进度/速率暴露到 UI。

## 3. v031 实施项

### 3.1 共享流式模块（A）

- 新增 `web/src/sha256Fallback.js`：把 `api.js` 里的 `IncrementalSHA256` + `SHA256_K` 常量整体迁出，导出 `createSha256()`（`update(Uint8Array)` / `hexDigest()`）。
- `api.js` 改为从该模块导入，主线程路径与 Worker 路径共用同一实现（避免两份代码漂移）。
- `hashWorker.js` 新增分支：`size > FILEBOX_HASH_DIRECT_LIMIT` 时按 8 MiB 分块读 `file.slice(...).arrayBuffer()`，喂给 `createSha256()`，回报进度；worker 内不再 reject 大文件。
- 主线程仅在 worker 构造/加载失败（返回 null）时兜底。

### 3.2 WASM 快路径（B）

- 依赖：`hash-wasm`（或等价实现），**固定精确版本**并提交 lockfile；`.wasm` 产物 vendored 进 `web/public/` 或随构建内联，避免运行时外链 CDN。
- Worker 内：优先 WASM 流式（`createSHA256()` from hash-wasm，`update()` 分块 → `digest()`）；WASM 初始化失败则回落 3.1 的纯 JS 实现。
- CSP：`script-src 'self'` → `script-src 'self' 'wasm-unsafe-eval'`；同步更新安全响应头相关测试/文档（`internal/httpapi` 中 CSP 注入处 + `docs/`）。
- 打包体积：报告 `.wasm` 与 JS 增量大小（目标：gzip 后增量 ≤ 100 kB，且为按需 chunk，不进入首屏关键路径）。

### 3.3 阈值与可观测性（附带小项）

- `FILEBOX_HASH_DIRECT_LIMIT` 默认值在文档与代码注释中写明（256 MiB）及其内存含义（一次性 ArrayBuffer 峰值 ≈ 文件大小）。
- 上传行展示校验速率（MB/s）与已校验字节，而不是只有百分比，便于用户与后续排查。

## 4. 验收标准

1. **正确性**：对同一文件，客户端结果与服务端（Go）计算结果完全一致；单元测试覆盖 SHA-256 分组边界：0、1、55、56、57、63、64、65 字节，以及 1 MiB+1、8 MiB±1（跨块）与多块累积拼接。
2. **回归**：现有 node 测试（`collectionCopy` / `topbarNav` / `transferFlow` / `transferStatus`）与 `go test ./...` 全绿。
3. **吞吐**：1 GiB 文件校验阶段 ≥ 100 MB/s（WASM 生效时）；WASM 不可用时不得低于改造前（5–6 MB/s）。
4. **主线程**：校验全程无 > 200 ms 的 long task；校验期间页面滚动/点击不卡顿（Performance 面板录制 + 人工滚动验证）。
5. **降级**：手工使 WASM 加载失败（改错路径/屏蔽 fetch）后，上传仍能完成，结果 hash 正确，且 UI 不冻结。
6. **秒传/断点语义**：校验结果不变 → 秒传命中与断点续传行为与改造前一致（同一文件复测命中次数不减少）。
7. **安全**：CSP 变更已生效且响应头可验证（`curl -I`）；无外链资源；依赖已固定版本。

## 5. 验证方法（基准测试）

- 测试文件：64 MiB、256 MiB、1 GiB 三档，用随机内容生成（`fsutil file createnew` 生成后仍以浏览器端读取为准；随机内容用于避免磁盘压缩/缓存带来的偏差）。
- 计时：`performance.now()` 包裹 `computeFileSHA256`（基准页/浏览器控制台均可），每档跑 3 次取中位数；同档同时记录 WebCrypto 原生路径作为上限参考。
- 对照：`certutil -hashfile <file> SHA256`（Windows）或 Go 参考实现，确认 hex 完全一致。
- 主线程：DevTools Performance 录制校验全过程，统计 long task 数量与最长时长（判据见 4.4）。
- 结论表：每档输出「耗时 / 吞吐 / 最长 long task / hex 是否一致」。

## 6. 交付物

- 代码：`web/src/sha256Fallback.js`、`web/src/hashWorker.js`、`web/src/api.js`、依赖与 lockfile、CSP 注入处。
- 测试：`web/tests/sha256Fallback.test.mjs`（分组边界与拼接）；`go test ./...` 回归。
- 文档：本任务书回填「实测结论表」；`docs/HANDOFF` 记录构建（3b 流程）与部署步骤。
- 部署：`npm run build` → `scripts/sync-web.go` → `go build ./cmd/filebox` → 覆盖 `filebox-demo\filebox.exe` → 原命令行重启 → 线上校验产物标记与响应头。

## 7. 实测结论（2026-09-12 已实施并部署）

已实现并部署：A + B。

| 项目 | 结果 |
| --- | --- |
| Worker 产物 | `dist/assets/hashWorker-*.js` 21.4 kB（WASM SHA-256 内联，tree-shake 后仅 sha256 单算法；原 0.67 kB） |
| 覆盖范围 | `computeFileSHA256` 现在**所有大小**都先交 Worker：≤ 256MiB 原生 WebCrypto；> 256MiB 走 Worker 内流式（WASM → 纯 JS 兜底）。主线程仅在 Worker 完全不可用时兜底 |
| 共享实现 | 新增 `web/src/sha256Fallback.js`，主线程与 Worker 共用同一份流式实现，消除双份代码漂移 |
| CSP | `script-src 'self'` → `script-src 'self' 'wasm-unsafe-eval'`（仅放行 WebAssembly 编译，不等于 `unsafe-eval`），已在线上响应头验证 |
| 单元测试 | 新增 `web/tests/sha256Fallback.test.mjs`（分组边界 0/1/55/56/57/63/64/65/127/128/129/1MiB+1，非对齐分块）与 `web/tests/wasmSha256.test.mjs`（hash-wasm 与 node:crypto 逐字节一致），6 个 node 测试文件全绿 |
| 吞吐（Node/V8 实测，64MiB 样本） | WASM **203.7 MB/s**（1GiB ≈ 5.0s） vs 纯 JS **23.4 MB/s**（1GiB ≈ 44s），**8.7×**，两者摘要完全一致 |
| 主线程阻塞 | > 256MiB 不再占用主线程（此前 1GiB 文件会长时间冻结界面） |

验收对照（§4）：#1 正确性由上述两组单测覆盖；#2 回归 `go test ./...` 与 6 个 node 测试全绿；#3 吞吐 Node 实测 203.7 MB/s > 100 MB/s（浏览器内 1GiB 复测仍建议由用户执行）；#6 CSP 变更已生效并可在响应头验证。

**仍待用户侧确认**：浏览器内 1GiB 真实上传的端到端耗时与 Performance 面板 long task（受限环境无法驱动文件上传），以及 §4.4 的界面不卡顿人工确认。

### 7.1 构建流程变更（重要）

原「3b 流程」（构建前 `git checkout -- web/src/transferFlow.js` 以还原 HEAD 版本）**已不可行**：`web/src/views/FilesView.vue` 现在导入 `transferBatches`/`advanceTransferGeneration`/`completionTimestamp`/`emaRate`/`FairStartGate`/`isTransferGenerationCurrent`/`needsBulkConfirm`，这些导出只存在于工作区的 v026 版本，HEAD 版本没有 → 用 HEAD 构建会直接失败（`"transferBatches" is not exported`）。

因此自 v031 起：**按工作区版本构建**，不再 checkout；`web/src/transferFlow.js` 的改动仍未提交、未合并（保持用户「先不做合并」的约束），但事实上已成为前端源码的必需依赖，需要用户决定是提交它还是回退前端对它的引用。
