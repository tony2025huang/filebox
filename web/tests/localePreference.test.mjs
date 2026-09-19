// v044.3 回归：语言偏好必须是响应式状态。
// 旧实现把"当前选择的语言"只放在 localStorage 里，语言下拉的 computed 直接读 localStorage →
// 该 computed 不依赖任何响应式数据、永久缓存首次算出的值 → 切回已存过偏好的语言时，下拉会被
// 渲染回写成旧语言（界面已是新语言、下拉却显示旧语言）。
// Regression: the explicit language preference must be reactive state. Reading localStorage inside a
// computed made it cache its first value forever, so the selector reverted to the old language.
import assert from 'node:assert/strict'
import test from 'node:test'

import { currentLocale, dictionaries, setLocale, storedLocale, systemLocale } from '../src/i18n.js'

test('显式选择会同时更新 currentLocale 与响应式的 storedLocale', () => {
  setLocale('en')
  assert.equal(currentLocale.value, 'en')
  assert.equal(storedLocale.value, 'en')

  // 关键回归点：切回先前存过的语言时，偏好也必须更新（旧实现里它不动，于是下拉被回写）。
  setLocale('zh-CN')
  assert.equal(currentLocale.value, 'zh-CN')
  assert.equal(storedLocale.value, 'zh-CN')

  setLocale('zh-TW')
  assert.equal(currentLocale.value, 'zh-TW')
  assert.equal(storedLocale.value, 'zh-TW')
})

test('跟随系统会清空偏好并回落到 systemLocale', () => {
  setLocale('en')
  systemLocale.value = 'zh-TW'
  setLocale('')
  assert.equal(currentLocale.value, 'zh-TW')
  assert.equal(storedLocale.value, '')
})

test('非法语言回落到简体且不留偏好', () => {
  setLocale('en')
  setLocale('de')
  assert.equal(currentLocale.value, 'zh-CN')
  assert.equal(storedLocale.value, '')
})

test('storedLocale 是响应式引用（Vue 能追踪其变化）', () => {
  setLocale('')
  const before = storedLocale.value
  setLocale('en')
  assert.notEqual(storedLocale.value, before)
  assert.equal(storedLocale.value, 'en')
  // 三种语言都在字典里，保证下拉选项与实际语言一致。
  for (const value of ['zh-CN', 'zh-TW', 'en']) assert.ok(dictionaries[value], `${value} 缺少字典`)
})
