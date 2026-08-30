# 设计：需求第 6 项——用户只读时段 + 外部用户上传收集

> 状态：**外部上传收集（第二部分）：已确认，codex 批次 C 开发中**；
> **用户只读时段（第一部分）：已确认**（codex 批次 C 完成后开发，dsh 校验测试）
> 需求原文：支持允许用户上传（限制类似分享下载）——先进行设计，设计后确认后再执行
> 2025 用户澄清（二次）：
> 1. 需要能够设置某用户**特定时间段只读**（查看/下载），由管理员设置——**新增需求，本文档第一部分**。
> 2. 「允许用户上传」= **分享给外部用户时，允许外部用户上传**，而不是系统中已存在的用户——
>    即本设计的第二部分（外部上传收集链接），已确认。

---

# 第一部分：系统内用户只读时段（管理员设置）

> 确认记录：r1 一次性起止窗口；r2 只读禁止全部写操作；r3 并入用户编辑弹窗；
> r4 前端禁用入口+提示条，后端 403 兜底。

## 一、需求理解

管理员可对**单个系统内用户**设置一个「只读时段」（read-only window）：在该时间段内，
该用户**只能查看/下载**自己的文件，不能执行任何写操作（上传、删除、重命名、建目录、
重命名/删除目录、创建分享等）。时段由管理员设置，到期自动恢复。

典型场景：账号到期前的只读过渡期、维护窗口期禁止改动、离职/停用前的只读观察期等。

## 二、设计

### 2.1 数据模型（store，migrate 自动补列）

users 表新增列：

| 列 | 说明 |
|---|---|
| read_only_from TEXT | 只读时段开始（RFC3339，空=未启用） |
| read_only_until TEXT | 只读时段结束（RFC3339，空=未启用） |

- 两个值同时非空且 `now ∈ [from, until]` 时为只读状态。
- 单个一次性窗口（管理员设起止时间）；到期自动恢复（无需手动清除，查询时按时间判定）。
- 独立于 `disabled`（禁用=不能登录）与 `must_change_password`。

### 2.2 API

- 管理端：`PUT /api/admin/users/{id}/read-only`（或并入现有用户编辑端点？——待确认点 3）
  请求体 `{from, until}`（RFC3339，可同时空串清除）。
- `GET /api/admin/users` 用户列表/详情返回 `readOnlyFrom`/`readOnlyUntil` 字段。
- 普通用户 `GET /api/auth/me` 返回 `readOnlyFrom`/`readOnlyUntil` + `readOnly`（当前是否只读），
  前端据此禁用写操作入口并提示。

### 2.3 服务端强制（httpapi）

写操作入口统一检查（与上传配额检查并列）：

| 入口 | 只读时行为 |
|---|---|
| uploadInit / uploadChunk / complete（含秒传、批量） | 403 `READ_ONLY`「当前账号处于只读时段，仅可查看和下载」 |
| deleteFile / batch-delete | 403 READ_ONLY |
| createFolder / renameFolder / deleteFolder | 403 READ_ONLY |
| createShare（创建分享链接） | 403 READ_ONLY |

- 实现：Server 增加 `userReadOnly(user) bool` 判定（读 user.ReadOnlyFrom/Until 与 now 比较），
  在各写操作 handler 开头调用，拒绝时 recordAudit(reason=read_only) + serviceEvent。
- 只读不影响：登录、查看列表、预览、下载、改密码、改语言、登出。

### 2.4 前端

- AdminView 用户编辑弹窗新增「只读时段」字段（开始时间 + 结束时间，datetime-local 输入，可清除）；
  用户列表可显示只读标记。
- FilesView：`me.readOnly=true` 时隐藏/禁用上传按钮、拖拽区、删除/重命名/新建目录等操作，
  并显示提示条「当前账号处于只读时段，仅可查看和下载」；后端 403 兜底。
- i18n 三语全量键（readOnly.* 前缀）。

### 2.5 与现有机制关系

- 独立于 disabled（禁用不能登录）、quota（空间总量）、上传收集链接（外部上传）。
- 管理员操作不受只读限制（只读约束的是该用户本人）。

## 三、验收标准（第一部分）

1. 管理员为 user1 设置只读时段（覆盖当前时间）→ user1 上传/删除/建目录/分享均 403 READ_ONLY，
   查看/下载正常；/me 返回 readOnly=true。
2. 只读时段外（或清除）→ 全部恢复。
3. 只读时段外的用户不受影响。
4. 审计记录 read_only 拒绝原因。
5. 前端只读时禁用写操作入口并提示。

## 四、待确认点（第一部分）——已确认

1. **时段形式**：一次性起止窗口（from/until 两时间点）——**已确认**。
2. **写操作范围**：禁止**全部**写操作（上传/删除/重命名/建目录/分享）——**已确认**。
3. **管理端设置入口**：并入现有「用户编辑」弹窗（加两个时间字段）——**已确认**。
4. **前端提示**：禁用入口 + 提示条，后端 403 兜底——**已确认**。

---

# 第二部分：外部用户上传收集链接（已确认）

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

## 三、验收标准（第二部分）

1. 登录用户创建收集链接（名称+有效期+次数上限）→ 得到 `/u/<token>` 链接。
2. 匿名访问 `/u/<token>`：可见名称、剩余次数、到期时间；可多文件上传，进度正常。
3. 上传次数达到上限 → 后续上传 403 COLLECTION_LIMIT，前端明确提示「收集次数已用完」。
4. 过期/撤销后 → 403 对应错误码，前端明确提示。
5. 单文件超过 maxFileBytes → 413，前端提示。
6. 上传成功后：创建者文件页出现 `uploads/<token>/` 目录，文件可预览/下载/删除。
7. 撤销链接 → 列表状态变更，链接失效，已传文件保留。
8. 审计日志记录外部上传（upload_collect）与失败原因。
9. 匿名上传有 IP 限速。

## 四、确认记录（第二部分，已完成）

| 确认点 | 结果 |
|---|---|
| 创建权限 | 所有登录用户可创建（与分享一致） |
| 文件落点 | 创建者名下 `uploads/<token>/` 自动子目录 |
| 限制维度 | 有效期 + 总上传次数 + 单文件大小上限（不要额外总字节上限） |
| 路由前缀 | `/u/:token`（与分享下载 `/:token` 区分） |
| 上传者备注 | 要，字段标签「备注」，不含「姓名」字样 |
