你是 FileBox 项目（Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树基本干净。**本任务只改 web/src/i18n.js 一处（三语字典的 logReason 键补齐），不要改动其它文件（LogsView.vue/SharesView.vue 的映射表已由先前提交完成），不要运行 git add/commit。** 完成后运行 `cd web && npm run build` 确认通过（产物不提交）。

背景（已核）：web/src/views/LogsView.vue 的 reasonLabel 映射表（含并发提交 bdf1eee 后新增的 read_only/settings_failed/invalid_request/delete_failed/write_failed/batch）与 web/src/views/SharesView.vue 的分享下载日志 reasonLabel 都指向 `logReason.*` i18n 键；但 web/src/i18n.js 三语字典里这些键严重不齐（i18n t() 兜底是 当前语 ?? zhCN ?? 原键，缺键时页面直接显示 `logReason.xxx` 英文路径）。经核对：
- en 字典现有 32 个 logReason 键（含 shareNotFound/shareExpired/shareRevoked/shareLimit/shareDenied 与 ipLocked/totpFailed）；
- zhCN 缺 shareDenied/shareExpired/shareLimit/shareNotFound/shareRevoked 共 5 个；
- zhTW 除同样缺这 5 个外，还缺 ipLocked/totpFailed；
- 三语都缺 bdf1eee 新增映射对应的 6 个键：logReason.readOnly/logReason.settingsFailed/logReason.invalidRequest/logReason.deleteFailed/logReason.writeFailed/logReason.batch。

请修改 web/src/i18n.js：把三语字典（zhCN / zhTW / en 三个对象；logReason.* 键散落在主字典与 Object.assign 追加块中，先搜索定位所有现有 logReason 键的位置与写法再追加）补齐到完全一致的键集合：
1. zhCN（简体）补 5 个 share 键：
   - 'logReason.shareNotFound': '分享不存在'
   - 'logReason.shareExpired': '分享已过期'
   - 'logReason.shareRevoked': '分享已撤销'（若该块已有同键不同值，以一致为准）
   - 'logReason.shareLimit': '下载次数已用完'
   - 'logReason.shareDenied': '无权访问该分享'
2. zhTW（繁体）补同样 5 个 share 键（繁体译法：分享不存在/分享已過期/分享已撤銷/下載次數已用完/無權存取該分享），另补：
   - 'logReason.ipLocked': 'IP 已被鎖定'
   - 'logReason.totpFailed': '動態驗證碼錯誤'
3. en 不需补 share/ip/totp（已存在）。
4. 三语都补 6 个新键：
   - zhCN：'logReason.readOnly': '只读时段限制'；'logReason.settingsFailed': '设置保存失败'；'logReason.invalidRequest': '请求无效'；'logReason.deleteFailed': '删除失败'；'logReason.writeFailed': '写入失败'；'logReason.batch': '批量操作'
   - zhTW：'logReason.readOnly': '唯讀時段限制'；'logReason.settingsFailed': '設定儲存失敗'；'logReason.invalidRequest': '請求無效'；'logReason.deleteFailed': '刪除失敗'；'logReason.writeFailed': '寫入失敗'；'logReason.batch': '批次操作'
   - en：'logReason.readOnly': 'Read-only window restriction'；'logReason.settingsFailed': 'Settings update failed'；'logReason.invalidRequest': 'Invalid request'；'logReason.deleteFailed': 'Delete failed'；'logReason.writeFailed': 'Write failed'；'logReason.batch': 'Batch operation'
5. 追加位置：就近放在各字典现有 logReason 键集群里即可（保持代码风格；如该语言用 Object.assign 追加块承载 logReason 键，也在对应块内追加）。
6. 自查：写一个临时检查（node -e 或计数）确认三语字典 logReason 键集合完全一致（各语言数量相同、键名集合相同）；不要改动任何既有键的值。

验收：三语 logReason 键集合一致；日志失败原因列与分享下载日志原因列对所有已映射 reason 显示本地化文案；`npm run build` 通过。
