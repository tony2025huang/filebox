你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务改后端 Go（internal/httpapi/server.go、internal/httpapi/share_group.go）与前端（web/src/views/FilesView.vue、web/src/views/BatchShareView.vue 及可能需要的 api.js/i18n）。完成后运行 `go test ./...`（后端）与 `cd web && npm run build`（前端）确认通过；产物 web/dist 不要提交。

## 需求 A：聚合下载失败透传具体原因（缺陷 2-A）

背景（已核）：
- internal/httpapi/server.go `batchDownload`（约 L2278-2399）：打包 ZIP 阶段的失败写死中文提示且不携带底层原因，仅 `log.Printf`：L2330/2354/2368/2372 `writeError(w, 500, "创建下载文件失败")`、L2362 `writeError(w, 500, "读取文件内容失败")`、L2319 `writeError(w, 500, "无法检查系统存储空间")`；写响应头前的容量预检 L2312（BATCH_TOO_LARGE）与 L2322（DISK_FULL）已用 writeErrorData 带 code。
- internal/httpapi/share_group.go `shareGroupBatchDownload`（约 L274-425）为匿名聚合下载，同类失败路径同样只写通用文案。
- 前端 FilesView.vue `batchDownload`/`streamDownload` 会把后端 JSON message 经 localizeError 展示；用户只看到"创建下载文件失败"，不知道是磁盘满/权限/文件缺失/IO 错。

请实现：
1. 在两个后端处理函数的 ZIP 创建/写入失败路径中，把底层错误归类为可读原因并随响应返回，例如：
   - 临时文件创建失败（os.CreateTemp）：err 为 *fs.PathError 且 syscall 为 ENOSPC/EDQUOT → 消息“创建下载文件失败：磁盘空间不足”；EACCES/EPERM → “创建下载文件失败：目录无写入权限”；其它 → “创建下载文件失败：{底层错误简短信息}”（保留内部错误仅进 log，对用户仅给归类后文案，注意不要泄露服务器绝对路径——用 err 的类型/系统错误号归类而非 err.Error() 原文）。
   - zip.Writer 创建/Close 失败 → 同样归类（IO/磁盘/权限）。
   - io.Copy 读文件失败：先判断源文件是否可打开/读到 EOF；若打开失败（源文件被删）保持“文件内容不存在”；复制中途错误按 IO 归类。
2. 建议实现一个小的辅助函数（如 `batchZipError(w, action, err)`）在两个 handler 中复用，保持两个入口行为一致。
3. 保留/合并原有语义：总量超限仍用 BATCH_TOO_LARGE code、容量预检失败仍用 DISK_FULL code；新增的归类消息可带 data.code（如 ZIP_CREATE_FAILED + reason），前端 API 层无需大改（message 直接展示），但如有现成 writeErrorData 用法请保持一致。
4. 添加后端单元测试：构造无法创建临时文件（如把 DataDir/tmp 指向只读/不存在路径或注入 CreateTemp 错误）或源文件缺失场景，断言响应包含归类后文案而非仅“创建下载文件失败”（参考现有 server_test.go 中 batch 相关测试的写法；若注入 os.CreateTemp 不可行，可改为验证“文件内容不存在/磁盘不足”路径已有文案或引入小规模可注入点，测试保持稳健不脆弱）。

## 需求 B：聚合下载 ZIP 文件名加时间戳（缺陷 2-B）

背景：用户下载批量 ZIP 的文件名目前固定 `filebox-batch-download.zip`：
- 后端响应头：server.go L2387 与 share_group.go 约 L425 `contentDisposition("filebox-batch-download.zip")`；
- 前端实际落盘名：FilesView.vue L154 `streamDownload(..., 'filebox-batch-download.zip')`（前端用 fetch 收集 Blob 后 <a download> 触发，download 属性优先于响应头）；BatchShareView.vue 约 L78 也有 fallback `link.download`（若该视图走同类 Blob 下载则同样生效）。

请改为带时间戳：`filebox-batch-YYYYMMDD-HHMMSS.zip`（如 filebox-batch-20260902-153000.zip）：
1. 后端：两处 Content-Disposition 文件名用当前时间生成（time.Now().Format("20060102-150405")）；前端 FilesView.vue 的 downloadName 参数生成同格式时间戳字符串（在发起时 new Date() 本地时间格式化，格式与后端一致即可，避免重复代码可放一个 helper）；BatchShareView.vue 若有同样固定名 fallback 一并处理。
2. 单文件下载名仍用原文件名，不受影响。

验收：批量下载（登录与匿名聚合两种入口）落盘文件名带时间戳；失败时提示包含具体原因（磁盘满/无权限/文件缺失/IO 错误）；`go test ./...` 与 `npm run build` 通过。
