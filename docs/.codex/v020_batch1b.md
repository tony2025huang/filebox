你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树基本干净（仅有未跟踪的 docs/.codex 提示词文件）。**本任务只做一次精确的键拆分修改，只改两个文件：web/src/i18n.js 与 web/src/views/SharesView.vue。不要改动任何其它文件，不要运行 git commit / git add，不要重建 web/dist。**

背景（已精确定位）：web/src/i18n.js 中，三语字典各自通过 `Object.assign(zhCN, { ... })` / `Object.assign(zhTW, { ... })` / `Object.assign(en, { ... })` 追加分享管理键。每个 Object.assign 块内 `shares.copy` 键都出现了两次：
- 第 1 次：值是一段页面副标题描述句（zh『查看链接使用情况，调整有效期和下载次数，或撤销单条分享。』/ zh-TW『查看連結使用情況，調整有效期和下載次數，或撤銷單條分享。』/ en『Review link usage, adjust expiry and download limits, or revoke one share.』），供 SharesView.vue 第 5 行 `<p class="muted">{{ t('shares.copy') }}</p>` 页面副标题使用；
- 第 2 次：值是『复制链接』/『複製連結』/『Copy link』，供分享表格行内 Copy 图标按钮的 `:title="t('shares.copy')"` 使用（SharesView.vue 约第 7/8/11 行）。

JS 对象字面量重复键后者覆盖前者 → 三种语言下页面副标题都被覆盖成『复制链接/Copy link』，错误。

修改要求（严格按步骤，用文本替换实现；每步替换上下文要唯一）：
1. 在 web/src/i18n.js 中分别把三个 Object.assign 块里 **第一次出现的** `'shares.copy': '查看链接使用情况，调整有效期和下载次数，或撤销单条分享。'` / `'shares.copy': '查看連結使用情況，調整有效期和下載次數，或撤銷單條分享。'` / `'shares.copy': 'Review link usage, adjust expiry and download limits, or revoke one share.'` 的键名 `shares.copy` 改为 `shares.intro`（值、位置、周边内容一律不动）。替换时用「键名+值」整体作为匹配串以保证唯一（三个值互不相同，天然唯一）。
2. 完成后，zhCN/zhTW/en 三个 Object.assign 块中 `shares.copy` 应只剩一次（值为『复制链接』/『複製連結』/『Copy link』），且 `shares.intro` 各出现一次。
3. 把 web/src/views/SharesView.vue 第 5 行页面副标题 `<p class="muted">{{ t('shares.copy') }}</p>` 改为 `<p class="muted">{{ t('shares.intro') }}</p>`（该行其它内容不动）。
4. 自查：用文本搜索确认 (a) i18n.js 中每语言 `shares.copy` 仅 1 次、`shares.intro` 各 1 次；(b) 全文 `shares.copy` 的其余使用点（SharesView 内 Copy 图标按钮 title）保持不变，仍引用 shares.copy；(c) 没有其它键受影响。
5. 该文件每行很长，若你的整行替换工具匹配失败，请改用「最小唯一子串替换」：只替换 `'shares.copy': '查看链接使用情况，调整有效期和下载次数，或撤销单条分享。'` 中的键名前缀 `'shares.copy':` → `'shares.intro':`（连同该值唯一匹配），逐个语言进行，不要一次替换整行。完成后如不确定语法可运行 `cd web && npm run build` 自查（产物不提交）。

验收：三语字典 shares.copy 唯一且为复制链接文案；shares.intro 三语均为描述句；SharesView 页面副标题显示描述句；Copy 图标按钮 title 不变。
