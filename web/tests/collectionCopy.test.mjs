import { test } from 'node:test'
import assert from 'node:assert/strict'
import { collectionCredentialText } from '../src/collectionCopy.js'

const URL = 'https://files.example.test/u/abcXYZ123'
const PASSWORD = 'Kx9pQw2vR7'

test('collectionCredentialText: zh-CN uses 链接/密码 labels', () => {
  assert.equal(
    collectionCredentialText(URL, PASSWORD, 'zh-CN'),
    `链接：${URL}\n密码：${PASSWORD}`,
  )
})

test('collectionCredentialText: zh-TW uses 連結/密碼 labels', () => {
  assert.equal(
    collectionCredentialText(URL, PASSWORD, 'zh-TW'),
    `連結：${URL}\n密碼：${PASSWORD}`,
  )
})

test('collectionCredentialText: en uses Link/Password labels', () => {
  assert.equal(
    collectionCredentialText(URL, PASSWORD, 'en'),
    `Link: ${URL}\nPassword: ${PASSWORD}`,
  )
})

test('collectionCredentialText: never embeds the password in the URL portion', () => {
  for (const locale of ['zh-CN', 'zh-TW', 'en']) {
    const text = collectionCredentialText(URL, PASSWORD, locale)
    // The password must appear exactly once, on its own line after the link.
    assert.ok(!text.includes(`${URL}#password=`))
    assert.equal(text.split(PASSWORD).length - 1, 1)
  }
})

test('collectionCredentialText: unknown locale falls back to English', () => {
  assert.equal(
    collectionCredentialText(URL, PASSWORD, 'fr'),
    `Link: ${URL}\nPassword: ${PASSWORD}`,
  )
})

test('collectionCredentialText: empty password still yields a clean link line', () => {
  assert.equal(
    collectionCredentialText(URL, '', 'zh-CN'),
    `链接：${URL}\n密码：`,
  )
})
