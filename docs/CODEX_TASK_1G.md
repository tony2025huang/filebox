# Codex 变更补丁任务 — 阶段一增量 5（R-THEME 界面主题色）

你在 `C:\Users\huangcp\dsh-project\filebox` 完成了 FileBox 阶段一 MVP 及补丁 1B/1C/1D/1F（如 1F 多语言未完成先完成，因为 1F 会大改前端文件，避免样式与文案改动交叉）。现在实现用户第五批需求。**先完整阅读 `docs/DEV_DOC.md`（第 1 节 R-THEME、5.9 节）**，再动手。

## 需求
1. 管理员可设置 **Web 界面主色**：支持**输入 RGB 色号（`#RGB`/`#RRGGBB`）或弹出色盘选择**（`<input type="color">`）。
2. 支持**重置为默认**（默认主色 `#1b998b`）。
3. 设置后**无需重新登录、无需刷新，立即生效**（全站主色元素：按钮、链接、进度条、焦点环、选中态、状态标签等）。

## 后端（较小）

1. settings 增加 `theme_color`（默认 `#1b998b`；`''` = 默认）。
2. `GET/PUT /api/admin/settings` 扩展 `themeColor` 字段（现有 LogSettings 结构加 `ThemeColor`；校验 `#RGB`/`#RRGGBB` 格式或空串；非法值 400；空串=恢复默认）。
3. `GET /api/brand`（公开）响应增加 `themeColor`（读 settings，未设置返回默认 `#1b998b`）。

## 前端

1. **CSS 变量化**：`web/src/style.css`（及组件内联样式）中主色相关硬编码统一替换：
   - `#1b998b` → `var(--brand-color)`
   - 深色 hover 变体（如 `#178a7d`/`#168172` 类）→ `var(--brand-color-strong)`
   - 透明/淡色变体（如 `#1b998b18`、`#eaf8f5` 等背景）→ `var(--brand-color-soft)`
   - `:root { --brand-color:#1b998b; --brand-color-strong:#137a6f; --brand-color-soft:#eaf8f5; }`（默认值可微调，保持现有视觉近似）
   - 注意：**只替换主色相关**；中性色（#102a43、#587087、边框 #d4dee8 等）不动；避免全局替换造成误伤（逐处核对语义）。
2. **应用与即时生效**：`brand.js`（或 i18n 同风格模块）增加 `applyTheme(themeColor)`：`document.documentElement.style.setProperty('--brand-color', c)`（strong/soft 由前端按色值计算变体：可用简单亮度调整或 color-mix；也可固定取默认变体，说明取舍）；启动时随 `/api/brand` 应用；品牌面板保存/重置后立即调用。
3. **品牌面板新增「界面主色」控件**（`AdminView.vue`）：文本框（`#RRGGBB` 校验，非法红框提示）+ `<input type="color">` 色盘（双向同步）+ 「恢复默认」按钮（置空）；保存时随品牌一起提交（themeColor 走 `/api/admin/settings` 或并入品牌表单请求，任选清晰方案，保持现有接口约定）。
4. 未登录页面（登录页）同样应用 themeColor（启动时从 `/api/brand` 读取应用）。

## 执行要求

1. 先读 `docs/DEV_DOC.md`（5.9 节）；确认 1D/1F 已完成再开始。
2. 验证全过：`go build ./...`、`go vet ./...`、`go test ./...`；`npm --prefix web run build` + `go run ./scripts/sync-web.go`；Linux 交叉编译。
3. 冒烟（`go run ./cmd/filebox --data=./data-patch5`）：
   - `PUT /api/admin/settings {"themeColor":"#3366ff"}` → `/api/brand` themeColor=`#3366ff`。
   - `PUT` 非法值（`"red"`/`"#12"`）→ 400。
   - `PUT {"themeColor":""}` → `/api/brand` 回默认 `#1b998b`。
   - 前端：色盘与输入框同步、保存后 CSS 变量即时更新（npm build 通过 + 代码审查确认 setProperty 调用链）。
   - 冒烟后删除 `data-patch5/`。
4. 更新 `docs/requirements/CHANGELOG.md`（追加 R-THEME 变更）、`docs/requirements/STATE.md`（R-THEME done）；同步 `README.md`/`README.en.md` 功能清单（增加「界面主题色定制」一条）。
5. 最终报告：改动文件清单、CSS 变量替换统计、验证结果、冒烟摘要。
