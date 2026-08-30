# 设计：同步功能（需求第 11 项）

> 状态：**已确认**（用户已确认全部设计要点；codex 开发中，dsh 校验测试）
> 确认记录：s1 双向（push+pull）；s2 覆盖/跳过/重命名三策略可选；s3 cron 表达式 + 常用预设；
> s4 pull 拉取计入属主配额；s5 日志按时间保留（默认 30 天）；s6 **独立目标系统配置**（remote systems，多任务复用）。
> 需求原文：同步功能——用户配置目标系统，文件/目录选择，一次性/周期同步，filebox↔sftp（密码/密钥含 passphrase），
> 自动创建目标目录，详细日志，按用户非全局，多任务，CRUD，源目录弹窗选择，目标目录浏览/创建。

## 一、需求理解

系统内用户可配置「同步任务」：把 FileBox 中的文件/目录**推送到**外部 SFTP 服务器，
或从外部 SFTP **拉取到** FileBox。每个用户管理自己的多个同步任务（按用户隔离），
支持一次性执行与周期（定时）同步，全程有详细日志。

## 二、设计

### 2.1 核心概念

- **同步任务（sync task）**：用户创建，含方向（push=本地上传至远端 / pull=远端下载至本地）、
  源、目标、认证信息、执行方式（once / periodic+cron）、状态与最近结果。
- **方向语义**：
  - push：源 = FileBox 文件或目录（弹窗选择）；目标 = SFTP 远端目录（浏览/输入）。
  - pull：源 = SFTP 远端目录；目标 = FileBox 目录（弹窗选择或新建）。
- **认证**：SFTP 支持密码 / 密钥文件（含 passphrase）；凭据加密存储（复用 TOTP 的 AES-GCM 密钥）。
- **周期同步**：5 段 cron 表达式（如 `0 3 * * *` 每天 3 点），由服务端后台调度（在现有
  cleanup goroutine 同层增加 scheduler）；服务器重启后按 cron 恢复下次触发。
- **执行**：push = 遍历 FileBox 源文件逐个上传到 SFTP（自动创建目标目录层级）；
  pull = 遍历 SFTP 源目录下载到 FileBox（自动创建目录）；同名覆盖策略（默认覆盖，可配置跳过）。
- **日志**：每次执行记录到 `sync_logs` 表（任务 id、时间、方向、结果 success/failure、
  文件数、字节数、错误信息），前端任务详情页展示历史执行记录。
- **并发控制**：同一任务同时只允许一个执行实例（执行中加锁，防止周期触发重叠）；
  任务级 rate limit 不引入，但每次同步受系统上传限速影响（复用现有限速）。

### 2.2 数据模型（store，migrate 自动建表）

**`remote_systems`（独立目标系统配置，可被多个任务复用）：**

| 列 | 说明 |
|---|---|
| id INTEGER PK | |
| user_id INTEGER | 属主 |
| name TEXT | 系统名称（如「客户备份服务器」） |
| host TEXT | SFTP 主机 |
| port INTEGER DEFAULT 22 | |
| username TEXT | |
| auth_type TEXT | password / key |
| auth_secret TEXT | 加密后的密码或密钥内容（AES-GCM） |
| auth_passphrase TEXT | 加密后的密钥 passphrase（可选） |
| created_at TEXT | |

**`sync_tasks`：**

| 列 | 说明 |
|---|---|
| id INTEGER PK | |
| user_id INTEGER | 任务属主 |
| name TEXT | 任务名称 |
| direction TEXT | push / pull |
| remote_system_id INTEGER | 引用的目标系统（FK→remote_systems） |
| source_type TEXT | filebox / sftp（源侧类型；push 时=filebox，pull 时=sftp） |
| source_path TEXT | 源路径（FileBox 相对目录或 SFTP 远端路径） |
| target_type TEXT | filebox / sftp |
| target_path TEXT | 目标路径 |
| conflict_policy TEXT DEFAULT 'overwrite' | overwrite / skip / rename |
| schedule_type TEXT | once / periodic |
| cron TEXT | 5 段 cron（periodic 时） |
| enabled INTEGER DEFAULT 1 | 是否启用（周期任务开关） |
| last_run_at TEXT | 最近执行时间 |
| last_result TEXT | 最近结果 success/failure |
| created_at TEXT | |

**`sync_logs`：**

| 列 | 说明 |
|---|---|
| id INTEGER PK | |
| task_id INTEGER | 关联任务 |
| user_id INTEGER | 属主（冗余便于隔离查询） |
| run_at TEXT | 执行时间 |
| direction TEXT | |
| result TEXT | success / failure |
| files INTEGER DEFAULT 0 | 处理文件数 |
| bytes INTEGER DEFAULT 0 | 传输字节数 |
| message TEXT | 结果/错误摘要 |
| detail TEXT | 详细日志（文件级列表） |

- 日志保留：**按时间保留，默认 30 天**（后台清理任务删除超过 retention 天数的 sync_logs；retention 可配置）。
- 删除 remote_system 时：被任务引用则拒绝删除（返回引用任务数）或级联停用（推荐：引用中的系统不可删，先解除任务）。

### 2.3 SFTP 依赖

- 引入 `github.com/pkg/sftp` + `golang.org/x/crypto/ssh`（纯 Go，静态编译兼容现有 CGO-free 构建）。
- 服务端执行同步时按需建立 SSH/SFTP 连接；连接凭据从 store 解密后使用，不落日志。

### 2.4 API

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/sync/systems` | JWT | 创建目标系统 |
| GET | `/api/sync/systems` | JWT | 我的目标系统列表 |
| PUT | `/api/sync/systems/{id}` | JWT+归属 | 更新（改凭据需重新提供） |
| DELETE | `/api/sync/systems/{id}` | JWT+归属 | 删除（被任务引用时拒绝并返回引用数） |
| POST | `/api/sync/tasks` | JWT | 创建任务（引用 remote_system_id） |
| GET | `/api/sync/tasks` | JWT | 我的任务列表 |
| GET | `/api/sync/tasks/{id}` | JWT+归属 | 任务详情（含最近日志） |
| PUT | `/api/sync/tasks/{id}` | JWT+归属 | 更新任务 |
| DELETE | `/api/sync/tasks/{id}` | JWT+归属 | 删除任务（日志一并删除或保留，见确认） |
| POST | `/api/sync/tasks/{id}/run` | JWT+归属 | 立即执行一次 |
| GET | `/api/sync/tasks/{id}/logs` | JWT+归属 | 任务执行日志（分页） |
| GET | `/api/sync/systems/{id}/browse` | JWT+归属 | 指定系统的 SFTP 远端目录浏览 `{path}`（返回子目录列表） |
| POST | `/api/sync/systems/{id}/mkdir` | JWT+归属 | 指定系统远端创建目录 |

- 越权（他人系统/任务）一律 404。
- 浏览/建目录走已保存的系统凭据（服务端解密使用），前端无需再传凭据。

### 2.5 调度器

- 服务端后台 goroutine（现有 cleanup 同层）：每分钟扫描启用且 periodic 的任务，
  cron 匹配（参考 `github.com/robfig/cron` 或手写 5 段匹配——建议引入 robfig/cron 简化）→ 触发执行。
- 执行队列：每任务互斥锁防重叠；失败不影响下次调度。
- 执行引擎（sync engine）：按 direction 走 push/pull 逻辑；每文件进度写入 sync_logs.detail。

### 2.6 前端

- 新页面「同步」入口（侧栏/顶栏）→ SyncView：任务列表（名称/方向/源→目标/调度/最近结果/操作：立即执行/编辑/删除/查看日志）。
- 新建/编辑弹窗：名称、方向（push/pull 切换后源/目标表单联动）、
  目标系统（下拉选择已配置系统 + 「新建目标系统」子弹窗）、
  源选择（FileBox 侧用目录弹窗选择器；SFTP 侧用浏览按钮调 `/systems/{id}/browse`）、
  目标（FileBox 目录弹窗/新建；SFTP 浏览/输入 + 自动创建勾选）、
  冲突策略（覆盖/跳过/重命名）、调度（一次性 / 周期 cron 输入 + 常用预设）、
- 目标系统管理：列表/新建（名称/主机/端口/用户名/密码或密钥+passphrase）/编辑/删除（引用中拒绝）。
- 详情页：任务信息 + 执行日志列表（时间/结果/文件数/字节/错误）+ 文件级详情展开。
- i18n 三语全量键（sync.* 前缀）。

### 2.7 安全

- SFTP 凭据 AES-GCM 加密存储（复用现有加密密钥机制）。
- 日志不打印密码/密钥；detail 只含文件名与错误信息。
- 同步任务操作（run/edit/delete）与目标系统操作均归属校验；管理员可查看所有（与文件管理一致）。

## 三、验收标准

1. 用户配置目标系统（密码认证）→ 创建 push 任务（选 FileBox 目录 → 远端目标）→ 立即执行 → 远端出现文件，目标目录自动创建。
2. pull 任务：远端源目录 → FileBox 目标 → 立即执行 → FileBox 出现文件，目录结构保留，文件计入属主配额。
3. 密钥认证（含 passphrase）可用。
4. 冲突策略三选一生效（覆盖/跳过/重命名）。
5. 周期任务：cron 到点自动执行（测试用短周期验证），结果写入日志。
6. 同一任务并发触发不重叠（执行中跳过/排队）。
7. 多用户隔离：user1 看不到 user2 的系统/任务；越权 404。
8. 详细日志：每次执行记录时间/结果/文件数/字节/错误；详情页可查看；超 30 天自动清理。
9. 目标系统目录浏览/创建可用（复用已存凭据）。
10. 凭据加密存储：库中不可见明文密码。
11. 删除任务后不再调度；重启后周期任务恢复；被引用的目标系统不可删。

## 四、待确认点——已确认

1. **同步方向**：双向都做（push + pull）——**已确认**。
2. **同名冲突策略**：覆盖/跳过/重命名三策略可选——**已确认**。
3. **调度粒度**：cron 5 段表达式 + 常用预设——**已确认**。
4. **配额归属**：pull 拉取计入任务属主配额——**已确认**。
5. **日志保留**：按时间保留，默认 30 天——**已确认**（用户自定义）。
6. **SFTP 凭据组织**：独立目标系统配置（remote_systems，多任务复用）——**已确认**。
