import { t } from './i18n.js'

// lazyText 返回"惰性本地化"占位对象：渲染时才调用 t()，因此切换语言后提示会自动跟随。
// 背景（v044.13）：`notice.value = t('notice.folderCreated')` 这类写法把**已翻译的字符串**存进状态，
// 语言一变就永久定格在创建那一刻的语言（小弹窗"文件夹已创建"切语言后不变就是这个原因）。
// 直接存 t(...) 的字符串无法补救，只能存"键 + 参数"。
//
// 为什么不用改模板：Vue 渲染 `{{ notice }}` 走 toDisplayString —— 该函数仅当对象的 toString 是
// Object.prototype.toString（或不是函数）时才 JSON 序列化，否则用 String(val)，于是会调用下面的
// toString()，而它内部读取响应式的 currentLocale，使这次渲染订阅语言变化并自动重译。
// Lazy localized text: translate at render time so a language switch updates existing notices.
export function lazyText(key, params = {}) {
  const translate = () => t(key, params)
  return {
    key,
    params,
    toString: translate,
    valueOf: translate,
    [Symbol.toPrimitive]: translate
  }
}
