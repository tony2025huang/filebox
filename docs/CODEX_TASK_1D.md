# Codex 变更补丁任务 — 阶段一增量 3（R-BRAND 品牌定制）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及补丁 1B（R-DISK/R-NAME）与 1C（R-CONFLICT/R-VALID/R-LOG/R-LOCK，如未完成先完成）。现在实现用户第三批需求。**先完整阅读 `docs/DEV_DOC.md` 第 5.7 节（R-BRAND）**，再动手。使用 skills：`web-app-dev`、`file-transfer-design`。

## 目标：管理员可自主设置/恢复默认的品牌项
1. 网站标题（网页 title）
2. 网站描述（SEO meta description）
3. 网页 favicon（.ico/.png/.svg）
4. 登录页 logo
5. 主页（文件页）logo
6. **ICP 备案号**（可空，默认空）
7. **公安备案号**（可空，默认空）

设置后全站生效（登录页、文件页、管理页、分享页）；「恢复默认」回退内置 FileBox 品牌。**前端空值不留空白**：备案号/描述等为空时对应区域不渲染（不留空白占位、不留空行）。

## 后端

1. **settings 表**新增品牌 key：`brand_title`、`brand_description`、`brand_icp`、`brand_police`、`brand_favicon`（文件名标记，如 `favicon.ico`）、`brand_login_logo`、`brand_main_logo`（空=未设置/默认）。
2. **资源存储**：`data/brand/` 目录，固定文件名按上传扩展名覆盖写入：`favicon.<ext>`、`login-logo.<ext>`、`main-logo.<ext>`（ext 仅允许 ico/png/svg/jpg；服务端固定拼接，禁止用户输入路径，防路径穿越）。settings 记录对应文件名。
3. **公开接口**：
   - `GET /api/brand` → `{siteTitle, siteDescription, icpText, policeText, hasFavicon, hasLoginLogo, hasMainLogo}`（siteTitle 为空时返回默认 `FileBox 文件管理`；siteDescription/icpText/policeText 默认空字符串）。
   - `GET /brand/favicon`、`GET /brand/login-logo`、`GET /brand/main-logo`：有自定义 → 输出 `data/brand/` 对应文件（Content-Type 按扩展名）；无自定义 → 输出内置默认资源（在 `internal/webassets` 增加内置 `brand/` 目录：`favicon.svg`、`logo.svg`（FileBox 三色方块风格），用 `embed` 提供）。
4. **管理接口**（仅 admin）：
   - `PUT /api/admin/brand`（`multipart/form-data`）：`siteTitle`（≤64 字符，可空=不修改）、`siteDescription`（≤200 字符，可空=不修改）、`icpText`（≤128 字符）、`policeText`（≤128 字符）、`favicon`/`loginLogo`/`mainLogo`（文件，≤512KB，类型白名单 ico/png/svg/jpg）；`reset=true` 时删除 `data/brand/*` 并清空全部品牌 settings（回退默认）。**文本字段的清除约定**：请求带 `clearTitle`/`clearDescription`/`clearIcp`/`clearPolice` 布尔标记时置空对应字段（siteTitle 置空后 API 返回默认标题；icp/police 置空即无备案）。响应返回最新 brand 配置。
5. 默认值常量：`siteTitle` 默认 `FileBox 文件管理`；其余文本默认空；内置 favicon 与 logo 为 SVG（`<svg>` 三色方块/文字 FileBox）。

## 前端

1. **全局 SEO（R-BRAND）**：`brand.js`（或 api.js 扩展）：启动时 `GET /api/brand` 缓存；`applyBrand(brand)`：设置 `document.title = siteTitle`、动态更新 `<meta name="description">`（siteDescription 为空则移除该 meta，不留空）、`<meta name="keywords">`（可由标题派生或省略）、动态替换 `<link rel="icon" href="/brand/favicon">`；`index.html` 的静态 title/meta 作为首屏默认值。
2. **登录页 `LoginView.vue`**：品牌 logo 显示区（有自定义 → `<img src="/brand/login-logo">`，无 → 内置 SVG）；页脚备案区：**仅当 icpText 或 policeText 非空时渲染**（两行或两列显示 ICP 备案号与公安备案号；两者都为空则整个备案区不渲染，不留空白/空行）。
3. **文件页 `FilesView.vue` 与管理页 `AdminView.vue`**：顶栏品牌区使用 `/brand/main-logo`（有自定义时）或内置 SVG；页脚（如有布局则加）显示备案区（同登录页规则：空则不渲染）；标题 brand.siteTitle。
4. **管理后台品牌设置面板**（`AdminView.vue` 或独立区块）：表单字段：网站标题（输入+清除按钮）、网站描述（textarea+清除，用于 SEO）、favicon 上传、登录页 logo 上传、主页 logo 上传（每项带预览+移除）、ICP 备案号（输入+清除）、公安备案号（输入+清除）、「保存」按钮（multipart 提交）、「恢复默认」按钮（reset=true，二次确认）。**清除按钮对应 clearXxx 标记**；保存/重置后立即 `applyBrand` 刷新。
5. 样式沿用现有设计语言（浅色、圆角、品牌色 #1b998b）。

## 执行要求

1. 先读 `docs/DEV_DOC.md`（重点 5.7、安全基线、验收）；1C 未完成先完成 1C。
2. 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`；`npm --prefix web run build` + `go run ./scripts/sync-web.go`；Linux 交叉编译。
3. 冒烟（`go run ./cmd/filebox --data=./data-patch3`）：
   - 未设置时 `/api/brand` 返回默认 title、空 description/icp/police、hasFavicon=false；`/brand/favicon` 返回 200（内置）。
   - admin 登录 → PUT 品牌（标题「测试云盘」、描述「测试描述」、上传一个 png logo、ICP「京ICP备00000000号」、公安「京公网安备00000000000000号」）→ `/api/brand` 返回新值；`/brand/login-logo` 返回上传内容（比对字节）；再 PUT 只清除 icp（clearIcp=true）→ `/api/brand` icpText 为空、policeText 保留。
   - 再 PUT `reset=true` → `/api/brand` 回默认；`data/brand/` 清空。
   - 非法文件类型（如 .exe）上传 → 400；>512KB → 400。
   - 冒烟后删除 `data-patch3/`。
4. 更新 `docs/requirements/CHANGELOG.md`（追加 R-BRAND 变更）、`docs/requirements/STATE.md`（R-BRAND 条目 done）。
5. 最终报告：改动文件清单、验证结果、冒烟摘要。
