# FileBox 阶段一 DSH 二次测试报告

日期：2026-08-29 ｜ 测试环境：Windows / Go 1.27 / filebox 单二进制（127.0.0.1:18080）
测试依据：docs/TEST_PLAN.md ｜ 被测版本：阶段一全量（MVP + R-DISK ~ R-PROXY 共 7 批）

## 一、结果统计

| 类别 | 用例数 | 通过 | 失败 |
|---|---|---|---|
| 功能主流程（F01–F08） | 27 | 26 | 1（D1 断言） |
| 边界（B01–B05） | 5 | 4 | 1（D2 0字节文件） |
| 安全（S01–S07） | 17 | 17 | 0 |
| 并发（C01） | 2 | 2 | 0 |
| 性能/磁盘（P03） | 1 | 1 | 0 |
| 运维命令（O01–O03） | 7 | 7 | 0 |
| 服务日志（SRVLOG） | 4 | 4 | 0 |
| **合计** | **63** | **61** | **2（均为真实缺陷）** |

## 二、缺陷清单

| 编号 | 级别 | 模块 | 描述 | 复现 | 预期 |
|---|---|---|---|---|---|
| D1 | 轻微 | 存储命名 | 同目录重名选择「重命名」时，磁盘文件名为 `name.txt (1)`（序号在扩展名后），与开发文档约定 `name (1).txt`（扩展名前）不一致 | 上传 conflict.txt 两次，第二次 resolve=rename | 磁盘文件应为 `conflict (1).txt` |
| D2 | 一般 | 上传 | **0 字节文件无法上传**：size=0 时 totalChunks=0，服务端校验「阶段一仅支持单分片上传」返回 400；空文件上传是常见场景 | upload-init {name:"empty.bin", size:0} | 应允许上传，md5=d41d8cd98f00b204e9800998ecf8427e |
| D3 | 轻微 | IP 白名单 | 管理员将自身 IP 白名单配错后，Web 端被 403 自锁，**CLI 无自救命令**（admin reset-password 不处理 ip-acl） | 白名单设为 10.0.0.0/8 后本机请求全 403 | 建议新增 `filebox admin clear-ip-acl --username=<user>` |
| D4 | 建议 | API | `PUT /api/admin/settings` 为全量语义：缺字段按零值校验失败（如缺 passwordMinLength → 400「设置无效」），错误信息笼统 | PUT {ipLockThreshold:3} | 部分更新语义或明确报错字段 |

## 三、验证亮点（全部通过）

- **强制改密闭环**：初始 admin 登录 → 403 PASSWORD_CHANGE_REQUIRED → 弱密码 400 → 强密码通过 → 恢复访问
- **双哈希**：上传后服务端 md5/sha256 与本地 `Get-FileHash` 完全一致
- **Range 206**：bytes=0-9 返回精确内容
- **冲突事务**：rename 落盘 `(1)` 序号；overwrite 后磁盘仅剩 1 个原名文件、used_bytes=20 精确
- **TOTP**：绑定（二维码+secret）→ 两步登录 → 错码拒绝 → **同窗口重放被防重放拒绝（ConsumeTOTP）**
- **IP 锁定**：阈值 3 次失败即锁 127.0.0.1，正确密码也统一 401，解除后恢复
- **可信代理**：未配置 --trusted-proxies 时伪造 X-Forwarded-For 被忽略（审计 IP=127.0.0.1）
- **并发**：5 路并行上传全部成功、列表一致
- **运维命令**：CLI 与服务共存（busy_timeout）、重置后强制改密、--generate 可登录、退出码规范（1/2）
- **服务日志**：login/upload/user_create 事件均含 operator+ip，**无任何密码/token/文件内容**

## 五、缺陷修复与复测（2026-08-29，codex FIX 任务）

| 缺陷 | 修复 | 复测结果 |
|---|---|---|
| D1 重命名序号位置 | `stem + trailer + extension` → `d1 (1).txt` | ✅ 磁盘文件 `d1 (1).txt` |
| D2 0 字节文件 | size=0 按单分片（chunkSize=1/totalChunks=1） | ✅ 上传成功，md5=d41d8cd9…，sha256=e3b0c442… |
| D3 IP 白名单自锁 | 新增 `filebox admin clear-ip-acl --username` | ✅ 403 自锁 → CLI 解除 → 恢复 200 |
| D4 settings 部分更新 | 字段指针化合并 + 具体字段错误提示 | ✅ 部分更新保留其他值；passwordMinLength=0 → 400「密码最小长度无效」 |

修复验证：`go build/vet/test`、Linux 交叉编译、前端构建+embed 同步全过；新增单元测试（0 字节上传、部分更新、命名规则、ClearIPACL、CLI 命令）；前端三语言新增 settings 错误键。

**最终结论：63/63 用例通过，阶段一全部验收标准达成。**
