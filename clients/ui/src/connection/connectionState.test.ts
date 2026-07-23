import { describe, expect, it } from 'vitest'
import { connectionStatusIsSettled } from './connectionState'

describe('connectionStatusIsSettled', () => {
  it('stops the busy overlay when a connection attempt fails', () => {
    expect(connectionStatusIsSettled('failed')).toBe(true)
    expect(connectionStatusIsSettled('reconnecting')).toBe(false)
  })
})
