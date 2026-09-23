import { readFileSync } from 'node:fs'
import assert from 'node:assert/strict'
const read = path => JSON.parse(readFileSync(new URL(path, import.meta.url), 'utf8'))
const zh = read('../src/locales/zh-CN.json')
const en = read('../src/locales/en.json')
const backend = read('../../localization/catalog.json')
assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort(), '中英文词条必须一致')
const params = text => [...text.matchAll(/\{\d+\}/g)].map(match => match[0]).sort()
for (const [key, text] of Object.entries(zh)) {
  assert.ok(en[key], `英文词条为空：${key}`)
  assert.deepEqual(params(text), params(en[key]), `插值参数不一致：${key}`)
}
for (const [key, value] of Object.entries(backend)) {
  assert.equal(zh[key], value['zh-CN'], `后端中文词条不同步：${key}`)
  assert.equal(en[key], value.en, `后端英文词条不同步：${key}`)
}
console.log(`已校验 ${Object.keys(zh).length} 组中英文词条及后端消息参数。`)
