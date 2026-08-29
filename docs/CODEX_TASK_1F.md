# Codex 变更补丁任务 — 阶段一增量 4（R-LANG 多语言）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及补丁 1B/1C/1D（如 1D 未完成先完成；**1E 双语文档任务需先完成**，因为 1F 会修改前端文案，避免与 1E 的注释工作交叉）。现在实现用户第四批需求。**先完整阅读 `docs/DEV_DOC.md`（第 1 节 R-LANG、5.8 节、6 节）**，再动手。

## 需求
1. 管理员可设置**系统默认语言**（settings.defaultLang，默认 `zh-CN`）；个人可设置自己的语言（`''`=跟随系统默认）。
2. 支持 `zh-CN`（简体中文）/ `zh-TW`（繁体中文）/ `en`（英文）。
3. 登录后按用户设置的语言显示；**用户修改语言后立即生效，无需重新登录**（管理员改品牌等前端属性保存后也已即时生效，保持）。
4. **前端全部文案语言转化**（导航、按钮、表单、表格、弹窗、上传状态、错误提示、品牌面板、日志页、日期、空态等）；后端错误消息由前端按状态码/错误码映射本地化文案，映射不到回退后端原始消息（中文兜底）。

## 后端

1. `users` 表加列 `language TEXT NOT NULL DEFAULT ''`（迁移：ALTER TABLE 或重建，参考现有 migrateUsersSchema 模式）。
2. `GET /api/auth/me` 返回 `language` 字段。
3. 新增 `PUT /api/auth/language`（登录用户）：body `{language: ''|'zh-CN'|'zh-TW'|'en'}`，非法值 400；保存成功返回最新用户信息。
4. `settings` 增加 `defaultLang`（默认 `zh-CN`）：`GET/PUT /api/admin/settings` 扩展该字段（现有 LogSettings 结构加 `DefaultLang`；校验取值）。
5. `GET /api/brand`（公开）响应增加 `defaultLang`（读 settings.defaultLang）。
6. 语言值校验函数（store 或 httpapi）：`isValidLang(string)`。

## 前端（重点）

1. **i18n 模块 `web/src/i18n.js`**（轻量自建，不引入 vue-i18n 依赖，保持现有风格）：
   - 三份字典：`zhCN`（简体，默认，可直接用现有中文文案）、`zhTW`（繁体，**人工翻译，不用简繁转换工具**）、`en`（英文）；key 用语义化点号（如 `nav.files`、`action.upload`、`error.loginFailed`）。
   - 导出 `t(key)`、`setLocale(lang)`、`currentLocale`（reactive）、`loadLocale()`（启动解析：localStorage 保存值 → 登录后 me.language（非空）→ /api/brand defaultLang → 'zh-CN'）。
   - 语言切换持久化：登录用户调用 `PUT /api/auth/language` 并存 localStorage；未登录仅 localStorage。
   - 组件内通过 `t()` 渲染文案（组件需要响应 locale 变化：用 computed 依赖 currentLocale 或提供订阅）。
2. **全部组件 i18n 化**：`LoginView.vue`、`FilesView.vue`（含上传状态、冲突弹窗、表格列、分页、配额、空态）、`AdminView.vue`（用户 CRUD 表单、统计卡片、编辑面板、品牌面板全部字段与按钮提示）、`LogsView.vue`（筛选、表格列、留存设置、原因/操作映射）、`App.vue`/`router`（页面标题等）；`index.html` 静态标题仅作首屏默认（运行时由 brand/i18n 覆盖）。
3. **错误消息本地化**：`api.js` 中封装 `localizeError(err)`：按 `err.status` + `err.data?.code`（如 DISK_FULL）+ 常见 message 匹配字典（`error.*`），匹配不到返回后端 message。
4. **语言选择器 UI**：登录页右上角下拉 + 登录后顶栏用户区下拉（简体中文/繁體中文/English）；选择后 `setLocale` 立即生效（无需刷新/重新登录），并持久化。
5. 日期时间格式：`formatDate` 按 locale 选择（zh-CN/zh-TW 用 zh 格式，en 用 en-US 格式）。

## 执行要求

1. 先读 `docs/DEV_DOC.md`（R-LANG 相关章节）；确认 1D/1E 已完成再开始。
2. 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`；`npm --prefix web run build` + `go run ./scripts/sync-web.go`；Linux 交叉编译。
3. 冒烟（`go run ./cmd/filebox --data=./data-patch4`）：
   - `PUT /api/auth/language {language:'en'}` → me 返回 en；再 `{language:''}` → 回跟随系统。
   - admin `PUT /api/admin/settings {defaultLang:'zh-TW'}` → `/api/brand` defaultLang='zh-TW'。
   - 非法语言值 → 400。
   - 前端三语言切换后界面文案随之变化（npm build 通过即视为前端结构正确；手工核对登录页/文件页关键文案字典齐全）。
   - 冒烟后删除 `data-patch4/`。
4. 同步更新 `README.md` 与 `README.en.md` 的功能清单（增加「多语言：简体中文/繁体中文/英文，系统默认+个人语言」一条）。
5. 更新 `docs/requirements/CHANGELOG.md`（追加 R-LANG 变更）、`docs/requirements/STATE.md`（R-LANG done）。
6. 最终报告：改动文件清单、三语言字典 key 数量、验证结果、冒烟摘要。
