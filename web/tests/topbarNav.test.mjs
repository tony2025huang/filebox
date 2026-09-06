import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mobileNavItems, navKeyForPath, TOPBAR_SECTIONS } from '../src/topbarNav.js'

test('navKeyForPath resolves active topbar section from route path', () => {
  assert.equal(navKeyForPath('/'), 'files')
  assert.equal(navKeyForPath(''), 'files')
  assert.equal(navKeyForPath('/collections'), 'collections')
  assert.equal(navKeyForPath('/collections/42/upload'), 'collections')
  assert.equal(navKeyForPath('/shares'), 'shares')
  assert.equal(navKeyForPath('/sync'), 'sync')
  assert.equal(navKeyForPath('/logs'), 'logs')
  assert.equal(navKeyForPath('/admin'), 'admin')
  // Unknown routes simply highlight nothing rather than a wrong section.
  assert.equal(navKeyForPath('/change-password'), '')
  assert.equal(navKeyForPath('/totally-unknown'), '')
})

test('TOPBAR_SECTIONS keeps stable order with admin last and labelled keys', () => {
  assert.deepEqual(
    TOPBAR_SECTIONS.map((section) => section.key),
    ['files', 'collections', 'shares', 'sync', 'logs', 'admin'],
  )
  for (const section of TOPBAR_SECTIONS) {
    assert.equal(typeof section.to, 'string')
    assert.equal(typeof section.labelKey, 'string')
    assert.ok(section.to.startsWith('/'))
  }
})

test('mobileNavItems filters admin-only section for regular users', () => {
  const regular = mobileNavItems({ isAdmin: false })
  assert.deepEqual(
    regular.map((section) => section.key),
    ['files', 'collections', 'shares', 'sync', 'logs'],
  )
  const admin = mobileNavItems({ isAdmin: true })
  assert.deepEqual(
    admin.map((section) => section.key),
    ['files', 'collections', 'shares', 'sync', 'logs', 'admin'],
  )
  assert.equal(mobileNavItems({ isAdmin: true }).some((section) => section.key === 'admin'), true)
})
