# 设计：允许用户上传限制（需求第 6 项）

> 状态：待确认（用户确认后由 codex 开发，dsh 校验测试）
> 需求原文：支持允许用户上传（限制类似分享下载）——先进行设计，设计后确认后再执行

## 一、需求理解

"允许用户上传"的开关 + "限制类似分享下载"的约束。参考分享下载的限制模型（有效期/次数），上传限制应提供：

1. **上传开关**：管理员可对单个用户启用/禁用上传（disabled 只禁登录，上传开关独立控制只读账号）。
2. **上传次数限制**：类似 maxDownloads——每日或累计允许上传的文件数上限（0=不限）。
3. **上传流量限制**：每日允许上传的字节总量（0=不限）；超出后拒绝 upload-init。
4. **临时禁止上传**：管理员可手动锁定某用户上传一段时间（如锁 24 小时），用于应急。

## 二、设计决策

### 2.1 数据模型（store）

users 表新增列（migrate 自动补）：
- `upload_enabled INTEGER NOT NULL DEFAULT 1` —— 是否允许上传（0=禁止）
- `upload_daily_limit INTEGER NOT NULL DEFAULT 0` —— 每日上传文件数上限（0=不限）
- `upload_daily_bytes INTEGER NOT NULL DEFAULT 0` —— 每日上传字节上限（0=不限）
- `upload_locked_until TEXT` —— 手动禁止上传截止时间（空=未锁定）

每日统计需独立表（按用户+日期）：
- `upload_usage(user_id, date, count, bytes)`：`date` 为 `YYYY-MM-DD`（UTC），UPSERT 累计；每日零时自动新行。

### 2.2 API

- `GET/PUT /api/admin/users/{id}/upload-policy`：读取/更新上传策略（uploadEnabled/uploadDailyLimit/uploadDailyBytes/uploadLockedUntil）。
- AdminView 用户编辑弹窗增加「上传策略」区块（与 TOTP/IP 白名单并列），可直接在创建用户时设置（配合第 3 项）。
- 普通用户 `GET /api/auth/me` 返回 `uploadEnabled`（前端禁用上传按钮/拖拽区并提示原因）。

### 2.3 服务端强制（httpapi）

`uploadInit` 入口统一检查（在创建任务前）：
1. `user.UploadEnabled == false` → 403 `UPLOAD_DISABLED`「当前账号不允许上传」。
2. `upload_locked_until > now` → 403 `UPLOAD_LOCKED`「上传功能已被临时禁用，请稍后重试」。
3. 今日 count/bytes 已达上限 → 403 `UPLOAD_LIMIT_REACHED`「今日上传次数/流量已用完」。
4. 通过后尝试 UPSERT 今日 usage（count+1，bytes+size）；若与上限并发竞争，以事务内条件更新保证不超限。
5. 失败原因记入审计（reason: upload_disabled/upload_locked/upload_daily_limit）。

> 说明：上限检查基于"发起 upload-init 时的预留"，实际完成/失败由 complete 回滚（预留-未完成部分按实际入库文件数修正）——为避免复杂度，**简化为 upload-init 即占用额度**（与现有 pending 配额预留一致），失败的任务在 24h 清理时释放当日额度。

### 2.4 前端

- AdminView 用户管理：表格「上传」状态列（正常/禁止/已锁定）；编辑弹窗含「允许上传」「每日上传文件数上限」「每日上传字节上限（MB）」「锁定上传至（日期时间）」；创建用户弹窗同步支持（第 3 项统一）。
- FilesView：`user.uploadEnabled=false` 或锁定/超限时，上传区/拖拽/文件夹按钮禁用并显示原因；由后端 403 兜底（前端提示优先，后端强制）。

### 2.5 与现有机制关系

- 与 disabled（账号禁用）独立：禁用=不能登录；上传开关=能登录但只读。
- 与配额（quotaBytes）独立：配额是空间总量；上传限制是操作频率/流量。
- 与 max-file-size 独立：单文件大小限制保持不变。

## 三、验收标准

1. 管理员禁用某用户上传 → 该用户上传按钮/拖拽禁用，API 返回 403 UPLOAD_DISABLED，日志记 upload_disabled。
2. 设置每日上传文件数上限 3 → 第 4 个文件 upload-init 403 UPLOAD_LIMIT_REACHED；次日重置。
3. 设置每日字节上限 → 超量后 403。
4. 临时锁定上传 → 锁定期内 403 UPLOAD_LOCKED，到期自动恢复。
5. 创建用户时即可设置上传策略（无需二次编辑）。
6. 普通用户 /me 可见 uploadEnabled；前端按状态禁用并提示。
7. 审计日志可见 upload 失败（reason 细分）。

## 四、待确认点

1. **上传次数/流量的统计口径**：按 UTC 自然日（0 点重置）？还是滚动 24h？→ 建议 UTC 自然日（与现有日志留存一致）。
2. **锁定粒度**：手动锁定仅管理员操作？到期自动解除（locked_until 语义）？→ 建议仅管理员可锁，到 locked_until 自动解除。
3. **前端展示位置**：上传策略字段放在编辑用户弹窗的「安全设置」区块（与 TOTP/IP 并列）？→ 建议如此。
4. 是否需要「仅允许上传到指定目录」（写权限白名单）？→ 首版不做，如需可二期。
