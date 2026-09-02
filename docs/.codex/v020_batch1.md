你是 FileBox 项目（Go 后端 + Vue3 前端，i18n 三语 zh-CN/zh-TW/en）的编码工程师。当前 git 工作树干净。本任务只改前端文件，**不得改动后端 Go 代码、路由、语义无关代码**。完成后运行 `cd web && npm run build` 确认通过（产物 web/dist 不要提交，只确认构建成功即可；如发现报错请修正到通过）。

## 需求 1：修复「我的分享」页副标题被覆盖为「复制链接」（缺陷 6）

背景：`web/src/i18n.js` 的中文（zh，大约第 146 行）字典里 `shares.copy` 这个键被定义了两次：
1. 第一次定义值是页面副标题描述句『查看链接使用情况，调整有效期和下载次数，或撤销单条分享。』，供 `web/src/views/SharesView.vue` 第 5 行页面副标题 `<p class="muted">{{ t('shares.copy') }}</p>` 使用；
2. 第二次定义值是『复制链接』，供分享表格每行/详情弹窗里 Copy 图标按钮的 `:title="t('shares.copy')"` 使用（SharesView.vue 第 7/8/11 行附近）。
JS 对象字面量重复键后者覆盖前者，导致中文下页面副标题（本该是描述句）实际显示成『复制链接』。而 zh-TW / en 字典里 `shares.copy` 只有『複製連結』/『Copy link』这一个值，同样造成繁体/英文副标题显示错误（英文副标题本该是 "Review link usage, adjust expiry and download limits, or revoke one share."，繁体是『查看連結使用情況，調整有效期和下載次數，或撤銷單條分享。』）。

请修复：
1. 在 `web/src/i18n.js` 三种语言字典（zh 简体、zh-TW 繁体、en）中新增独立键 `shares.intro`，值为该页面副标题描述句：
   - zh：查看链接使用情况，调整有效期和下载次数，或撤销单条分享。
   - zh-TW：查看連結使用情況，調整有效期和下載次數，或撤銷單條分享。
   - en：Review link usage, adjust expiry and download limits, or revoke one share.
2. 把 `web/src/views/SharesView.vue` 第 5 行页面副标题的 `{{ t('shares.copy') }}` 改为 `{{ t('shares.intro') }}`。
3. 删除中文字典里第一次出现的 `shares.copy` 重复定义（保留值为『复制链接』的那次），使每个语言字典中 `shares.copy` 唯一且值为 复制链接/複製連結/Copy link（该键继续供 Copy 图标按钮 title 使用，无需改动行内按钮）。
4. 请顺带检查三种语言字典中是否还有其他重复定义的键（同一语言同一键出现多次），如有与本次修改同语义的重复也一并去重；如无则不要扩大改动范围。

验收：SharesView 页面副标题显示描述句（三语各自正确文案）；复制图标 title 仍为 复制链接/複製連結/Copy link；i18n.js 中三语各字典无重复键。

## 需求 2：传输按钮图标方向改为上下箭头（缺陷 11）

背景：`web/src/views/FilesView.vue`：
- 第 5 行模板：传输按钮 `<button class="icon-button transfer-button" ...><ArrowLeftRight :size="18" />...`；
- 第 47 行 script：`import { ..., ArrowLeftRight } from 'lucide-vue-next'`。

文件上传/下载属于「上下」的传输语义，左右箭头（ArrowLeftRight）不符合，应改为上下箭头 `ArrowUpDown`。

请修改：
1. 第 5 行模板中 `<ArrowLeftRight :size="18" />` 改为 `<ArrowUpDown :size="18" />`；
2. 第 47 行 import 中 `ArrowLeftRight` 替换为 `ArrowUpDown`（保持字母序风格）；
3. 检查整个 `web/src` 下是否还有其它使用左右箭头表达「传输/上传下载」语义不合适的场景：全仓确认 `ArrowLeftRight` 仅 FilesView.vue 第 5/47 行这两处使用（已核），若有其它 ArrowLeftRight 使用且语义为传输方向则一并评估是否改上下箭头；与传输无关的左右箭头（如分页 ChevronLeft/Right、面包屑）一律不动。

验收：FilesView 顶栏传输按钮显示上下箭头；`npm run build` 通过。
