你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。本任务只改前端 `web/src/views/AdminView.vue` 与 `web/src/i18n.js`，**不得改动后端 Go 代码、不得改路由**。完成后运行 `cd web && npm run build` 确认通过（构建产物 web/dist 不要提交，只需确认构建成功）。

## 需求：管理后台各页签说明文案区分

背景：管理后台 AdminView 有六个左侧页签：概览(overview)/用户管理(users)/安全设置(security)/品牌设置(brand)/锁定管理(locks)/系统设置(system)。当前页面顶部只有一条共用说明 `admin.copy`（三语均为"管理账号、分配空间，并查看当前存储概况"），所有页签显示同一句话，与各页签实际功能不匹配。需要为每个页签配置各自的说明文案（三语）。

现状：
- `web/src/views/AdminView.vue` 第 9 行 page-heading：`<p class="eyebrow">{{ t('admin.eyebrow') }}</p><h1>{{ t('admin.heading') }}</h1><p class="muted">{{ t('admin.copy') }}</p>`。
- 各页签内容块：`<div v-show="activeTab === 'overview'">`（统计卡片+系统语言）、`users`（用户表）、`security`（密码/IP 锁定设置，面板内已有 admin.securityEyebrow/securityHeading/securityCopy）、`brand`（品牌设置，面板内已有 brandEyebrow/brandHeading/brandCopy）、`locks`（锁定列表，面板内已有 locksEyebrow/locksHeading/locksCopy）、`system`（系统设置）。
- `activeTab` 变量与 tabs 定义在 script 中（tabs = [{key:'overview',...},{key:'users',...},{key:'security',...},{key:'brand',...},{key:'locks',...},{key:'system',...}]）。

要求：
1. 在 `web/src/i18n.js` 三语（zhCN、zhTW、en）各新增 6 个键（语义要匹配对应页签的实际功能）：
   - `admin.copyOverview`：概览页说明（存储/文件/账号/分享概况，例如 zh-CN："查看存储用量、文件与账号概况，以及系统默认语言。"）
   - `admin.copyUsers`：用户管理说明（账号、角色、配额与状态，例如 zh-CN："创建与管理账号，分配空间配额，调整角色与账号状态。"）
   - `admin.copySecurity`：安全设置说明（密码强度与登录锁定策略，例如 zh-CN："配置密码强度要求，以及失败登录的 IP 锁定策略。"）
   - `admin.copyBrand`：品牌设置说明（站点标题/描述/标识/备案，例如 zh-CN："配置网站标题、描述、品牌标识与备案信息。"）
   - `admin.copyLocks`：锁定管理说明（IP 与账号锁定，例如 zh-CN："查看并解除 IP 与账号的登录失败锁定。"）
   - `admin.copySystem`：系统设置说明（日志留存/注册/限速/代理信任，例如 zh-CN："配置日志留存、开放注册、上传限速与代理信任等系统参数。"）
   （en/zh-TW 请按同等语义翻译；en 页签名仍用 Overview/Users/Security/Branding/Locks/System。）
2. AdminView.vue 页面顶部 copy 改为随 `activeTab` 动态取键：`{{ t('admin.copy' + activeTab.charAt(0).toUpperCase() + activeTab.slice(1)) }}`（即 admin.copyOverview / admin.copyUsers / admin.copySecurity / admin.copyBrand / admin.copyLocks / admin.copySystem）。
3. 页签面板内部已有的独立 eyebrow/heading/copy（security/brand/locks 面板内的）**保持不动**，不重复改造。
4. `admin.eyebrow`/`admin.heading`/`admin.copy` 旧键保留（admin.copy 可不再被引用，允许保留在 i18n 中；若希望整洁也可删除，但必须保证三语同步删除，且页面不再引用）。

## 验收

1. 六个页签切换时，页面顶部说明文案各不相同、与该页签实际功能匹配。
2. 三语（zh-CN/zh-TW/en）键完整，无缺键、无语法错误。
3. `cd web && npm run build` 构建成功。
4. 不触碰：router.js、任何 .go 文件、其它 .vue 文件。

请实施并简述每处改动。
