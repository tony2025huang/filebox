# 设计：外部用户上传收集链接（需求第 6 项）

> 状态：**已确认**（用户已确认全部设计要点；codex 开发中，dsh 校验测试）
> 确认记录：c1 所有登录用户可创建；c2 创建者目录自动子目录；c3 有效期+次数+单文件大小；
> c4 路由 /u/:token；c5 上传者可选填**备注**（字段标签用「备注」，不出现「姓名」字样）。
> 需求原文：支持允许用户上传（限制类似分享下载）——先进行设计，设计后确认后再执行
> 2025 用户澄清：实际需求是**外部用户上传**（外部访客无需登录、通过链接向系统上传文件），
> **不是**系统内用户的上传配额限制。本文档据此重写（v2）。

## 一、需求理解

与「分享下载」对称：分享下载 = 把系统内文件给外部下载；**上传收集 = 接收外部文件**。

场景示例：合作方/客户通过一个链接把文件传给你，无需注册登录；你（创建者）在系统内即可看到、管理这些文件。

「限制类似分享下载」= 沿用分享链接的限制模型：**有效期 + 次数上限**（可叠加单文件大小上限）。

## 二、设计

### 2.1 核心概念

- 系统内登录用户（创建者）生成一个「上传收集链接」（upload collection），类似创建分享链接。
- 链接含 64 位随机 token，外部访客无需登录即可访问公开上传页，拖拽/选择文件上传。
- 上传的文件归**创建者**所有，计入创建者配额，落入创建者目录下的自动子目录
  `uploads/<token>/`，创建者可在文件管理页看到并删除。
- 创建者可撤销链接（撤销后不可再上传，已传文件保留）。

### 2.2 数据模型（store，migrate 自动建表）

新表 `upload_collections`：

| 列 | 说明 |
|---|---|
| id INTEGER PK | |
| token TEXT UNIQUE | 64 位随机 token |
| created_by INTEGER | 创建者用户 ID |
| name TEXT | 收集名称/说明（创建者填写） |
| expires_at TEXT | 有效期截止（RFC3339，必填） |
| max_uploads INTEGER DEFAULT 0 | 总上传次数上限（0=不限） |
| max_file_bytes INTEGER DEFAULT 0 | 单文件大小上限（0=不限） |
| upload_count INTEGER DEFAULT 0 | 已上传次数 |
| status TEXT DEFAULT 'active' | active / revoked |
| created_at TEXT | |

外部上传的文件直接复用 `files` 表（file 的 user_id = created_by，target 为
`uploads/<token>/<name>`），**不新增**外部上传记录表——创建者通过文件管理即可查看。
审计日志新增 action：`upload_collect`（外部上传成功）与 `upload_collect_fail`
（失败原因细分）。

### 2.3 API

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/collections` | JWT | 创建收集链接 `{name, expiresInHours, maxUploads, maxFileBytes}` |
| GET | `/api/collections` | JWT | 我的收集链接列表（含 uploadCount、剩余可用、状态） |
| DELETE | `/api/collections/{id}` | JWT+归属 | 撤销链接 |
| GET | `/api/collections/{token}/meta` | 匿名 | 公开元数据（名称、到期、已收数/上限、单文件上限、是否可上传） |
| POST | `/api/collections/{token}/upload-init` | token | 外部上传初始化（复用分片逻辑） |
| POST | `/api/collections/{token}/upload-chunk` | token | 分片上传 |
| POST | `/api/collections/{token}/upload-complete` | token | 完成/秒传/合并 |

- 外部上传鉴权：`uploadInit` 等入口目前要求 JWT；新增按 token 校验的变体
  （校验 collection 有效：status=active 且 expires_at 未过且 uploadCount<maxUploads）。
- 上传前原子预留：`IncrementCollectionUploads`（同 IncrementShareDownloads 模式），
  超限返回 403 `COLLECTION_LIMIT`（对应「下载次数用完」的对称错误）。
- 超限/过期/撤销 → 403，错误码区分：`COLLECTION_EXPIRED` / `COLLECTION_REVOKED` /
  `COLLECTION_LIMIT` / `COLLECTION_FILE_TOO_LARGE`（413）。
- 匿名上传统一加 rate limit（IP 维度，复用现有 x/time/rate 基础设施）。

### 2.4 前端

- **公开上传页**：新路由 `/u/:token`（与分享下载 `/:token` 区分，避免路由冲突），
  无登录，拖拽/选择文件、多文件排队、进度条、秒传提示、完成后显示已上传列表。
  链接格式：`<origin>/u/<token>`。
- **创建者端**：文件页新增「上传收集」入口（按钮 + 弹窗：名称、有效期小时、
  上传次数上限、单文件大小上限 → 生成链接可复制）；「我的收集」列表（名称、状态、
  已收/上限、剩余时间、撤销）；已收文件在文件树 `uploads/<token>/` 目录可见。
- i18n 三语全量键。

### 2.5 与现有机制关系

- 与系统内用户上传限制**无关**（用户澄清）；系统内用户上传不受影响。
- 配额：外部上传计入创建者 quotaBytes，超配额返回 413 QUOTA_EXCEEDED。
- 与分享链接并存：分享 = 出站，收集 = 入站，两套独立 token/表。
- 大文件分片/秒传/断点续传复用现有 uploadInit/uploadChunk/complete 链路。

## 三、验收标准

1. 登录用户创建收集链接（名称+有效期+次数上限）→ 得到 `/u/<token>` 链接。
2. 匿名访问 `/u/<token>`：可见名称、剩余次数、到期时间；可多文件上传，进度正常。
3. 上传次数达到上限 → 后续上传 403 COLLECTION_LIMIT，前端明确提示「收集次数已用完」。
4. 过期/撤销后 → 403 对应错误码，前端明确提示。
5. 单文件超过 maxFileBytes → 413，前端提示。
6. 上传成功后：创建者文件页出现 `uploads/<token>/` 目录，文件可预览/下载/删除。
7. 撤销链接 → 列表状态变更，链接失效，已传文件保留。
8. 审计日志记录外部上传（upload_collect）与失败原因。
9. 匿名上传有 IP 限速。

## 四、待确认点（v2，基于用户澄清重写）

1. **创建权限**：上传收集链接是否所有登录用户都能创建（推荐，与分享一致）？还是仅管理员？
2. **文件落点**：自动落入创建者名下 `uploads/<token>/` 子目录（推荐，创建者直接可见可管）？
   还是创建者可指定目标目录？
3. **限制维度**：有效期 + 总上传次数 + 单文件大小上限（推荐，对齐「分享下载」的次数限制模型）。
   是否需要额外「总字节上限」？
4. **路由前缀**：公开上传页用 `/u/:token`，与分享下载 `/:token` 区分（推荐）——确认？
5. **上传者备注**：外部上传页提供可选「备注」输入框（字段标签为「备注」，不含「姓名」字样），
   创建者可在已收文件（文件树或收集详情）中查看备注。已确认：**要**。
