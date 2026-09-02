你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端 web/src/views/FilesView.vue（必要时少量 styles.css 与 i18n.js、web/src/api.js），后端 Go **只读查阅不改**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交；如报错修正到通过）。

## 需求：传输任务批量控制（缺陷 3）

背景（已核）：
- FilesView.vue 传输抽屉（transfers-drawer，约 L23-35）：上传区（uploads 数组）每行已有单任务 暂停(pauseUpload L336：paused=true+abort controllers+removeQueued+wakeWorkers)/继续(resumeUpload L337)/重试(retryUpload L338)/移除(dismissUpload L344)；下载区（downloads 数组）每行只有取消（cancelDownload L162：controller.abort），无暂停/继续。
- 上传断点续传基础已具备：taskId + uploaded chunks（服务端 DELETE /api/upload-tasks/{taskID} 存在，server.go L351，终止可释放配额）；下载走 /api/files/{id}/download，后端 http.ServeContent 支持 Range（可断点续传），但前端下载逻辑 streamDownload(L403-438)/download(L442-454) 整流下载未用 Range、无暂停态。
- 会话快照持久化 persistTransfers/restoreTransfers（L262-304）：上传项有 taskId/uploaded 等字段可恢复续传；下载项仅作展示记录。

请实现传输抽屉的批量控制（UI + 逻辑）：
1. **批量选择**：上传区与下载区各自行首增加 checkbox；抽屉标题/各区头部提供「全选本区」；已勾选集合用 reactive Set（选中行 id 即可，行对象在数组里按 id 查找）。勾选行有高亮（可复用 row-selected 类）。考虑上传/下载区是否分开选择与操作：建议每个区一行批量工具条（显示已选 n 项 + 按钮），或合并区级工具栏——选择对实现最直接的方式，但要清晰。
2. **批量操作（上传）**：
   - 暂停选中/继续选中/终止选中 三按钮（仅在选中>0 时可用/显示）；
   - 「全部」级按钮：暂停全部上传、继续全部上传、终止全部上传（可放上传区标题行右侧小按钮组或工具条）；
   - 暂停=pauseUpload；继续=resumeUpload；终止=DELETE /api/upload-tasks/{taskId}（对每个仍处初始化中/暂停/失败且 taskId 存在的项调用；taskId 为空串的项无需调用后端，直接移除），随后从 uploads 移除该项并 persistTransfers；需要 try/catch 与提示（成功/失败数）。
3. **批量操作（下载）**：为下载项增加 paused 状态与按钮：
   - 暂停单个/选中/全部：记录已接收字节，AbortController.abort() 中止当前流（区别于失败/取消：item.paused=true）；
   - 继续单个/选中/全部：以 Range 头重新请求 `/api/files/{id}/download`（Range: bytes={loadedBytes}-），追加到已有 Blob 片段，合并进度与速率（新建/改造下载函数支持续传：fetch 带 Range 头；响应 206 时把新片段并入 parts；Content-Length 处理注意剩余长度；200 时全量重下兜底），完成后整体落盘；
   - 终止选中/全部下载：abort 并移除记录。
4. **按钮状态**：对已完成的项、正在校验中的上传项等禁用相应操作（继续仅对 paused/failed/canContinue 项有意义；暂停仅对 running 项）。操作后即时刷新 UI、persistTransfers。
5. **i18n**：需要的按钮文案在三语 i18n.js 补键（如 暂停选中/继续选中/终止选中/暂停全部/继续全部/终止全部/全选 等，若已有复用）。样式最小必要（批量工具条、行 checkbox 列）。
6. 注意并发安全：批量暂停/终止时遍历副本操作，防止对同一项的控制器重复 abort；不要破坏既有单行按钮与冲突队列/上传闸门（runGated/uploadInFlight）逻辑。

验收：抽屉内可勾选多个上传/下载任务执行 暂停/继续/终止 的选中与全部操作；终止上传释放服务端任务（调用 DELETE /api/upload-tasks/{id}）；下载暂停后继续从断点续传（网络面板可见 Range 请求、最终文件完整）；UI 状态与持久化正确；`npm run build` 通过。
