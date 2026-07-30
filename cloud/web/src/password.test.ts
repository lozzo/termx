import { describe, expect, it } from 'vitest'
import { isValidPassword, passwordByteLength } from './password'

describe('Cloud password byte contract', () => {
  it('accepts only 8 through 72 UTF-8 bytes', () => {
    expect(isValidPassword('a'.repeat(7))).toBe(false)
    expect(isValidPassword('a'.repeat(8))).toBe(true)
    expect(isValidPassword('a'.repeat(72))).toBe(true)
    expect(isValidPassword('a'.repeat(73))).toBe(false)
    expect(passwordByteLength('界'.repeat(24))).toBe(72)
    expect(isValidPassword('界'.repeat(24))).toBe(true)
    expect(isValidPassword(`${'界'.repeat(24)}a`)).toBe(false)
  })
})
