import { describe, expect, it } from 'vitest'

import { checkPasswordStrength } from './password'

describe('checkPasswordStrength', () => {
  it.each([
    ['', { level: 'weak', text: '弱' }],
    ['abc12', { level: 'weak', text: '弱' }],
    ['abc123', { level: 'medium', text: '中等' }],
    ['abcdefghij', { level: 'medium', text: '中等' }],
    ['Abcdefgh1!', { level: 'strong', text: '强' }],
  ] as const)('正确判断密码 %s 的强度', (password, expected) => {
    expect(checkPasswordStrength(password)).toEqual(expected)
  })
})
