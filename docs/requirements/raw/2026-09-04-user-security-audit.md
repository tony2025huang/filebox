# FileBox v023 安全审计任务提示词摘要

> 本文件是任务提示词摘要，不是用户原文全文；证据以当前源码审计规格为准。

- 审计范围：仅审计与文档，不修改产品代码；先核对 git 状态；只新增 `docs/requirements/` 下文档；不运行部署或 Release 操作。
- H1 分享下载次数限制：核查单文件匿名 `sharePreview` 是否绕过 `maxDownloads`，非白名单 MIME 的预览长度限制，Range 续传的 60 秒窗口 SQL，以及聚合分享过期后的详情、成员列表、浏览和下载入口。
- H2 同步 pull 路径安全：核查 SFTP pull 和 FileBox-to-FileBox pull 的远端中间目录段是否逐段校验，Windows 反斜杠或 `..` 是否可穿越 `data`，并对照现有 `validateUploadDir`/`safeSyncFileName` 实现。
- 输出：每条结论包含 ID、严重度、已确认/不成立/部分成立、受影响入口、项目相对路径和逐行原文证据；已确认缺陷要给出涉及函数/文件、预计测试名的修复建议，不替业务拍板策略。
- 校验与测试：说明是否存在 `scripts/verify_evidence.py`；若不存在，手工 grep/read 回核引用；仅运行 `go test ./internal/httpapi/ ./internal/store/` 及必要的同步相关包。
- 交付：报告 commit message 为 `docs: 审计分享与同步高风险链路`，完成后推送 `origin main`，回报 commit、测试结果、漏洞清单和修复建议。
