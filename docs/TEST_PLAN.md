# FileBox 阶段二 DSH 二次测试计划

依据 `app-testing` skill 方法论，对 FileBox 阶段二（分片断点续传/秒传/文件夹/分享/预览/限速/注册/统计）执行验收测试。
被测系统：`C:\Users\huangcp\dsh-project\filebox`（Go 后端 + Vue3 前端单文件，v0.2.0）。
测试方式：以 API（Invoke-RestMethod/curl）为主 + 前端构建产物检查 + 服务日志检查。

## 0. 准备
- 启动：`bin\filebox.exe --addr=:18081 --data=<tmp-data> --admin-user=admin --admin-pass=Admin123! --min-free-space=0 --log-enabled=true --log-dir=<tmp-logs>`（独立端口 18081，独立数据目录）
- 初始账号 admin/Admin123!（mustChangePassword=true）→ 登录后改密 Filebox123!
- 中文 JSON body 使用 UTF-8 字节数组（PS 5.1 避免乱码）；.ps1 脚本带 UTF-8 BOM
- 测试脚本：`.test-data\stage2\test-lib2.ps1`（辅助库）、`test-setup2.ps1`（准备）、`test-transfer2.ps1`（分片/秒传/文件夹/配额）、`test-share2.ps1`（分享/预览/限速/注册/统计/日志）、`test-expire.ps1`（分享过期）、`test-resume1gb.ps1`（1GB 断点续传）、`test-regress2.ps1`（阶段一回归）、`test-batch5.ps1`（v011 批次 14-17 前端专项）、`test-batch6.ps1`（v011 批次 18-19 专项）、`test-batch7.ps1`（v011 批次 20 前端专项）

## 1. 功能主流程（阶段二新增）
| 编号 | 用例 | 预期 |
|---|---|---|
| F201-F208 | 10MB 文件 4MB×3 片：乱序上传 0、2 → status=[0,2] → 缺片 complete 400 → 补传 1 → complete 200，md5 与本地一致；重复 complete 不重复落盘 | 分片闭环 |
| G01-G09 | 1GB 文件 8MB×128 片：并发 4 上传 30 片 → 中断 → **重启服务** → status 仍 30 片（DB 持久化）→ 续传剩余（含重传 0、1 验证幂等）→ complete 200 → md5 与本地一致 → 磁盘文件 1GB | 断点续传 |
| F301-F304 | 秒传：check md5/sha256 命中 instant=true 且磁盘文件数不增；未命中 instant=false；跨用户不命中 | 秒传 |
| F401-F403 | 文件夹：dir=assets/icons 与 assets/images 同名 logo.svg 各保留一份（不加序号）；`../` 目录 400 | 文件夹 |
| F501-F509 | 分享：创建（64 位 token）→ 无效有效期 400 → 匿名 meta → 匿名下载内容一致 → 超次数 403 → 无效 token 404 → 撤销 → 撤销后 404；越权分享 404 | 分享 |
| E01-E06 | 分享过期：构造过期时间后 meta 404「分享链接已过期」、download 403 | 过期拒绝 |
| F601-F603 | 预览：text/plain inline、.zip attachment；限速：设置 ~64KB/s 后 1MB 上传耗时明显 >3s 且成功，恢复 0 后不限速 | 预览/限速 |
| F701-F707 | 注册：关闭时 403 REGISTER_DISABLED；brand.registerEnabled 同步；开启后注册成功并返回 token；重复 409；弱密码 400 | 注册 |
| F801 | admin stats 含 shares/shareDownloads | 统计 |
| F901-F905 | 审计日志含 share/share_view/share_download/register；logActions 含 register | R-LOG |

## 2. 边界/安全
| 编号 | 用例 | 预期 |
|---|---|---|
| B01 | 分片大小 <2MB（多分片场景）400「分片大小必须在 2MB-8MB 之间」 | 校验 |
| B02 | 分片 index 越界/非法 400 | 校验 |
| B03 | 越权：user2 分享/下载/删除 user1 文件 404 | 隔离 |
| B04 | 未登录访问受保护 API 401；普通用户访问 admin 403 | 鉴权 |
| B05 | 0 字节文件上传 md5=d41d8cd98f00b204e9800998ecf8427e | 回归 |

## 3. 回归（阶段一）
| 编号 | 用例 | 预期 |
|---|---|---|
| R01 | 单分片上传/下载/删除闭环，双哈希一致 | 回归 |
| R02 | 0 字节文件 | 回归 |
| R03 | 同名冲突 409 → rename 落盘 `(1)` 序号 → overwrite 后磁盘仅 1 个原名 | 回归 |
| R04 | 非法文件名 400 | 回归 |
| R05 | 越权下载/删除 404 | 回归 |
| R06 | 未登录 401 / 普通用户 admin 403 | 回归 |
| R07 | 普通用户仅见自己的日志 | 回归 |
| R08 | 登录锁定（阈值 3 → 锁定 → admin 解除 → 恢复） | 回归 |
| R09 | 品牌/主题色/多语言 API 基本 | 回归 |
| R10 | CLI reset-password（--data 解析修复）/ locks list | 回归 |
| R11 | Range 206 下载 | 回归 |

## 4. 前端专项（v011 反馈批次 14-17，test-batch5.ps1）
| 编号 | 用例 | 预期 |
|---|---|---|
| P14a | `/admin?tab=system` 返回 200 | 深链可用 |
| P14b-e | 产物含 `admin-tab`/`admin-layout`/`admin-sidebar` 与六个页签 i18n 键（tabOverview/tabUsers/tabSecurity/tabBrand/tabLocks/tabSystem）及 `query.tab` 读取 | 页签化 |
| P15a-b | 产物含 `modal-backdrop`/`modal-panel`；编辑逻辑保留 `totpReenroll` + `ip-acl` 三请求 | 弹窗 |
| P16a-c | 产物不含 `logs.retentionDays`（已从日志页移除）；含 `logRetentionDays`/`logRetentionCopy`（系统设置页签） | 日志周期迁移 |
| P17a-d | 产物含 `brand-footer-title`/`brand-footer-desc`；brand 接口含 siteTitle/siteDescription | 页脚品牌块 |

## 5. 前端专项（v011 反馈批次 18-19，test-batch6.ps1）
| 编号 | 用例 | 预期 |
|---|---|---|
| P18a-c | 同名冲突 409 产生审计失败记录（reason=conflict）；日志页 /api/logs 可见 upload_init 失败（含原因）；非法名/配额失败审计 reason 细分 | 上传失败日志 |
| P18d | logActions 含 upload_init/upload_chunk | 动作列表 |
| P19a | 配额不足 403 响应含 QUOTA_EXCEEDED + usedBytes/quotaBytes/fileSize 明细 | 配额明细 |
| P19b | 单文件超限 413 + FILE_TOO_LARGE + maxFileSize（与非法名分离） | 超限独立提示 |
| P19c | 前端产物含 QUOTA_EXCEEDED/FILE_TOO_LARGE 映射与冲突队列（conflictQueue） | 前端映射 |
| P18e | 3 个同名文件 rename 后全部完成（xx.txt / xx (1).txt / xx (2).txt） | 并发同名容错 |

## 6. 前端专项（v011 反馈批次 20，test-batch7.ps1）
| 编号 | 用例 | 预期 |
|---|---|---|
| P20a-b | 前端产物含 overallRate 状态与 loadedBytes 进度同步 | 速率数据链路 |
| P20c-d | 产物含 B/s/MB/s 单位自适应与 overall-rate 样式；i18n 三语言 overallRate | 速率展示 |
| P20e | 嵌入 bundle（Go embed）含速率 UI | 部署产物一致 |

## 7. 测试报告输出
- 用例通过/失败统计、缺陷清单（编号/级别/复现/实际/预期）、服务日志片段
- 结论：是否满足 docs/DEV_DOC.md 阶段二验收标准
