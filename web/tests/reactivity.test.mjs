// v044.23 契约用例：条目状态必须"写在响应式对象上"，否则派生列表（列表归属、页签计数）不会重算。
// 这钉住的是我们踩过的那类 bug：行内已显示"已完成"，条目却仍留在「传输中」，刷新才恢复。
import assert from 'node:assert/strict'
import test from 'node:test'

import { computed, reactive } from 'vue'

test('写入原始对象不会触发派生计算（复现缺陷形状）', () => {
  const items = reactive([])
  const raw = { name: 'a.bin', done: false }
  items.push(raw)
  const done = computed(() => items.filter(item => item.done).length)
  assert.equal(done.value, 0)
  raw.done = true            // ← 写在原始对象上：依赖未被通知
  assert.equal(done.value, 0, '这是一次"静默失效"：派生结果停在旧值')
})

test('写入代理对象会立即触发派生计算（修复后的形状）', () => {
  const items = reactive([])
  const item = reactive({ name: 'a.bin', done: false })
  items.push(item)
  const done = computed(() => items.filter(entry => entry.done).length)
  assert.equal(done.value, 0)
  item.done = true           // ← 写在代理上：依赖立即失效
  assert.equal(done.value, 1)
})

test('push(item = reactive(item)) 让数组元素与局部变量指向同一代理', () => {
  const items = reactive([])
  let item = { name: 'b.bin', status: 'uploading', progress: 0 }
  items.push((item = reactive(item)))
  const stages = computed(() => items.filter(entry => entry.status === 'uploading').length)
  assert.equal(stages.value, 1)
  item.status = 'completed'   // 局部变量改状态
  assert.equal(stages.value, 0, '局部变量与数组元素是同一代理时，改动必须被追踪')
  // 且数组元素本身也是该代理（改数组元素同样触发）
  item.status = 'uploading'
  assert.equal(stages.value, 1)
})
