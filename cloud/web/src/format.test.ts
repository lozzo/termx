import { describe, expect, it } from 'vitest'
import { bytes, compactID, money } from './format'

describe('中文运营投影格式', () => {
  it('使用稳定单位展示流量与金额', () => {
    expect(bytes(1536n)).toBe('1.5 KB')
    expect(money('CNY', 1299n)).toContain('12.99')
  })

  it('只压缩过长标识', () => {
    expect(compactID('short-id')).toBe('short-id')
    expect(compactID('12345678-1234-1234-1234-123456789abc')).toBe('12345678…789abc')
  })
})
