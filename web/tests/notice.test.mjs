// v044.13：提示文案必须是"惰性本地化"——存键 + 参数、渲染时才翻译，切语言后旧提示也要跟着变。
// 旧写法 `notice.value = t('notice.x')` 把已翻译字符串存进状态，语言一切就永久定格。
import assert from 'node:assert/strict'
import test from 'node:test'

import { lazyText } from '../src/notice.js'
import { currentLocale, setLocale } from '../src/i18n.js'

test('惰性提示按当前语言渲染，并在切换语言后跟随变化', () => {
  setLocale('zh-CN')
  const notice = lazyText('notice.folderCreated')
  const zh = String(notice)
  assert.ok(zh.length > 0 && zh !== 'notice.folderCreated', `中文渲染异常：${zh}`)

  setLocale('en')
  const en = String(notice)
  assert.notEqual(en, zh, '切换语言后旧提示没有重译')
  assert.ok(!/[\u4e00-\u9fff]/.test(en), `英文渲染仍含中文：${en}`)

  setLocale('zh-TW')
  assert.ok(/[\u4e00-\u9fff]/.test(String(notice)), '繁中渲染异常')
  setLocale('zh-CN')
})

test('惰性提示支持参数插值，且未知键回退为键名而非崩溃', () => {
  setLocale('zh-CN')
  const withParams = lazyText('notice.filesDeleted', { count: 3 })
  assert.match(String(withParams), /3/, '参数未代入')
  assert.equal(String(lazyText('notice.doesNotExist')), 'notice.doesNotExist')
  assert.equal(currentLocale.value, 'zh-CN')
})
