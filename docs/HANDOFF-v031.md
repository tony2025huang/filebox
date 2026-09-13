# FileBox 会话交接（v030 完成 / v031 已实施）— 2026-09-12

> 本文件替代 `HANDOFF-v030.md` 作为最新续做依据（v030 事项已全部完成并部署）。

## 一、当前部署状态

- 运行中进程：`filebox-demo\filebox.exe`，pid **3472**，`--addr=127.0.0.1:18080`，数据目录 `filebox-demo\data`。
- 线上核验：`/` 200；`Content-Security-Policy` 含 `script-src 'self' 'wasm-unsafe-eval'`；首页引用 `assets/index-CG6Ibt_X.js`，其中含 `public-queue-dir`/`batch-share-dir` 标记；Worker 产物 `assets/hashWorker-DYkO4yhb.js` 21.4 kB。
- 工作区：v028/v029/v030/v031 改动**全部未提交**；v027 已提交 `607209f`（未推送）。**未推送任何东西**。

## 二、构建流程（已变更，务必注意）

- **不要再用 3b 流程**（`git checkout -- web/src/transferFlow.js`）：`FilesView.vue` 依赖该文件 v026 版本的导出（`transferBatches` 等），HEAD 版本没有 → 用 HEAD 构建必定失败。
- 现行流程：`npm --prefix web run build` → `go run ./scripts/sync-web.go` → `go build ./cmd/filebox` → 覆盖 `filebox-demo\filebox.exe` → 用原命令行重启。
- **不要提交或合并 `web/src/transferFlow.js`**（用户明确要求），但它已是前端源码的必需依赖：请用户决定提交它或回退 `FilesView.vue` 对它的引用。

## 三、v030 完成情况（全部已部署）

| 项 | 状态 |
| --- | --- |
| #1 上传终态契约单源化 | 完成（`web/src/transferStatus.js` + node 测试，用户已复测） |
| #3 清空未生效 | 实测判定非缺陷；附带的体验项「清空后分页回到第 1 页」已实现（`FilesView.submitClearAll`） |
| #4 收集密码页样式 | 完成：此前 `.public-password-form` **完全没有 CSS 规则**，已补齐（grid 间距、label、alert） |
| #5 目录上传聚合为单条目录项 | 完成：`UploadView.vue` 用 `groups`/`displayRows` 把目录来源的文件聚合为可展开的目录行（文件数、总大小、聚合进度、失败原因），上传引擎仍按扁平 queue 执行，语义未变 |
| #8 传输进度展开页 | 完成：每个文件行可展开明细（已传/总量、实时速率 EMA、状态、失败原因）；1s 采样定时器随组件卸载清理 |
| #7 收集落盘目录 | 完成并在线验证：`files/<uid>/collections/<收集名>-<token 前 8 位>`；新增 CLI `filebox admin migrate-collection-dirs [--dry-run]`；目录在界面显示用户语言的收集名（不带 token 后缀） |
| #9 分享页目录树 | 完成并在线验证（后端 `files[].relativePath` + 前端扁平缩进可折叠树 + 整目录勾选） |
| #10/#11 | 完成并在线验证 |
| hash 步骤 1 | 已完成并被 v031 取代 |

### #7 的两个实现要点（易踩坑）

1. **路径分隔符**：`filepath.Join` 在 Windows 上产生反斜杠，迁移 SQL 必须先 `replace(col, '\', '/')` 归一化，否则线上 0 命中。
2. **前缀判定**：`substr(x, 1, ?) = ? || '/'` 的长度参数必须是「前缀长度 + 1」，否则永远不相等。
   （两处都是被新增测试抓出来的真实缺陷，见 `internal/httpapi/collection_dir_migration_test.go`。）

### #7 CLI 现状

演示数据仍有 **9 个收集**处于旧布局；迁移是**显式**的（启动期不自动迁移），未执行。预演结果正常：

```powershell
.\filebox-demo\filebox.exe admin migrate-collection-dirs --data=.\filebox-demo\data --dry-run
```

## 四、v031 完成情况（A+B 均已实施并部署）

- A：新增 `web/src/sha256Fallback.js`（零依赖流式 SHA-256，主线程与 Worker 共用）。
- B：Worker 静态导入 `hash-wasm@4.11.0`，大文件走 WASM 流式；WASM 初始化失败回退纯 JS。
- `computeFileSHA256` 现在所有大小都先交 Worker（>256MiB 不再阻塞主线程）。
- CSP 增加 `'wasm-unsafe-eval'`（`internal/httpapi/server.go` 的 CSP 注入处）。
- 新增测试：`web/tests/sha256Fallback.test.mjs`、`web/tests/wasmSha256.test.mjs`、基准脚本 `web/tests/hashBench.mjs`。
- 实测（Node/V8，64MiB）：WASM **203.7 MB/s** vs 纯 JS **23.4 MB/s**（8.7×），摘要一致；1GiB 估算 5.0s vs 44s。
- **待用户侧确认**：浏览器内 1GiB 端到端耗时、Performance long task、界面不卡顿；#5/#8 的目录聚合与展开明细的界面观感（受限环境无法驱动文件上传）。

## 五、质量门与常用命令

```powershell
go test -count=1 ./...                                     # 后端全量
foreach ($f in (Get-ChildItem 'web\tests' -Filter '*.test.mjs')) { node $f.FullName }   # 6 个 node 测试
node web\tests\hashBench.mjs                               # WASM/JS 吞吐基准
```

- 已知工具坑：PowerShell 用 `-File` 脚本而非 `-Command`（管道与 here-string 易被 cmd 解析）；`npm run build` 的 stderr 会让 `$ErrorActionPreference='Stop'` 误报中断；PS 管道里不要写 `Select-String -Pattern 'a|b'`（会被 cmd 当管道）。
- 不要动演示数据（admin 用户 id=2 有 1000+ 文件）；收集相关验证请用一次性收集并清理。
