# FileBox 阶段一 DSH 二次测试计划

依据 `app-testing` skill 方法论，对 FileBox 阶段一（含全部 7 批需求补丁）执行验收测试。
被测系统：`C:\Users\huangcp\dsh-project\filebox`（Go 后端 + Vue3 前端单文件）。
测试方式：以 API（curl/Invoke-RestMethod）为主 + 前端构建产物检查 + 服务日志检查。

## 0. 准备
- 启动：`go run ./cmd/filebox --data=<tmp-data> --admin-user=admin --admin-pass=Admin123! --log-enabled=true --log-dir=<tmp-logs>`（独立端口如 18080）
- 初始账号 admin/Admin123!（mustChangePassword=true）
- 清理：测试后删除临时 data/logs 目录

## 1. 功能主流程（每项记录通过/失败）
| 编号 | 用例 | 预期 |
|---|---|---|
| F01 | admin 首次登录 → me.mustChangePassword=true → 访问文件列表 403 → change-password（弱密码 400；强密码成功）→ 恢复访问 | 强制改密闭环 |
| F02 | admin 创建用户 user1（弱密码 400 / 强密码成功）→ user1 登录 | 密码策略生效 |
| F03 | user1 上传 a.txt → 列表可见（md5/sha256 返回）→ 下载内容一致 → 删除 | 上传/下载/删除闭环 |
| F04 | 再次上传 a.txt → 409 冲突 → rename 落盘 a (1).txt；再传 overwrite → 磁盘仅 1 个 a.txt、DB 记录数正确、used_bytes 正确 | 冲突选择闭环 |
| F05 | 非法文件名（a<b>.txt、含 `..`）→ upload-init 400 | R-VALID |
| F06 | admin 开启 user1 TOTP → user1 登录返回 totpSetup（secret+二维码 URL）→ 错码 400 → 用 TOTP 工具生成正确码 → 绑定成功；再次登录两步（密码→动态码） | TOTP 闭环 |
| F07 | admin 为 user1 设 IP 白名单（仅 127.0.0.1）→ 本机请求正常；白名单改 10.0.0.0/8 → 403；关闭 → 恢复 | R-IPACL |
| F08 | 品牌：改标题/描述/ICP/公安/favicon/logo/主色 → /api/brand 生效 → reset 回默认；备案留空页脚不渲染（检查前端代码） | R-BRAND/R-THEME |
| F09 | 语言：admin 设 defaultLang=en → 未登录 brand 返回 en；user1 设个人语言 zh-TW → me.language 生效（前端逻辑检查） | R-LANG |
| F10 | 日志页：user1 登录/上传/下载后 /api/logs 可见自己记录；admin 可见全部（含登录失败原因 user_not_found/wrong_password） | R-LOG |
| F11 | 服务日志：--log-enabled 时 logs 目录生成当天文件，含 login/upload/admin 事件且带 operator+ip | R-SRVLOG |

## 2. 边界与健壮性
| 编号 | 用例 | 预期 |
|---|---|---|
| B01 | 空文件名/超长名（256+）→ 400 | 拒绝 |
| B02 | 0 字节文件上传 → 成功且 md5=d41d8cd98f00b204e9800998ecf8427e | 边界可用 |
| B03 | 用户名重名创建 → 409 | 冲突 |
| B04 | 配额：user1 配额设为 1KB → 上传 2KB → 403 配额错误 | R-QUOTA |
| B05 | 畸形请求（缺字段/错类型）→ 结构化错误 | 健壮性 |

## 3. 安全测试
| 编号 | 用例 | 预期 |
|---|---|---|
| S01 | 连续 5 次错误密码 → 用户锁定（locks 列表可见）→ 第 6 次正确密码也 401 统一提示 → admin 解除或等自动解禁 | R-LOCK |
| S02 | 调低 IP 锁定阈值（如 3）→ 不同用户名连续失败 3 次 → IP 锁定（locks 可见）→ 正确密码也 401 → DELETE locks/ip 解除 → 恢复 | R-IPBAN |
| S03 | 伪造 X-Forwarded-For（未配置 trusted-proxies）→ 审计日志记录真实 127.0.0.1；配置 trusted-proxies=127.0.0.1/32 后 → 记录伪造头中的 IP | R-PROXY |
| S04 | user1 尝试下载/删除 user2 文件 → 404（归属隔离） | 越权防护 |
| S05 | 未登录访问受保护 API → 401；普通用户访问 /api/admin/* → 403 | 鉴权 |
| S06 | 错误信息不泄露内部路径（检查响应体） | 信息泄露防护 |
| S07 | 统一登录失败文案（不存在用户 vs 错误密码均「用户名或密码错误」） | 防枚举 |

## 4. 并发与一致性
| 编号 | 用例 | 预期 |
|---|---|---|
| C01 | 并发上传 5 个文件（PowerShell 并行任务）→ 全部成功、DB 记录一致 | 并发上传 |
| C02 | 两个同名任务并发 complete（一个 rename 一个 overwrite）→ 无脏数据 | 冲突并发 |
| C03 | 上传中删除同目录文件 → 上传仍完成或报可理解错误 | 一致性 |

## 5. 性能（轻量）
| 编号 | 用例 | 预期 |
|---|---|---|
| P01 | 100MB 文件上传耗时与后端内存（观察进程内存平稳） | 流式处理 |
| P02 | 大文件下载 Range 请求（bytes=0-1023 → 206 且长度正确） | Range |
| P03 | 磁盘卡片数值与 Get-PSDrive 实际一致 | R-DISK |

## 6. 运维命令（R-OPS / R-SERVICE）
| 编号 | 用例 | 预期 |
|---|---|---|
| O01 | `filebox admin reset-password --username=admin --new-password=Xxx12345!` → 新密码登录且强制改密 | CLI 重置 |
| O02 | `filebox admin reset-password --generate` 打印强密码可登录 | 生成 |
| O03 | `filebox locks list` 显示锁定 → `filebox locks clear --all` 清空 | CLI 锁管理 |
| O04 | deploy/ 目录存在 filebox.service、install-service.ps1、nginx.conf.example、双语 README | 交付物 |

## 7. 测试报告输出
- 用例通过/失败统计、缺陷清单（编号/级别/复现/实际/预期）、截图或响应片段
- 结论：是否满足 docs/DEV_DOC.md 阶段一验收标准
