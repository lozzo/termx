import { describe, expect, it } from 'vitest'
import source from './terminalProtocolClient.ts?raw'
import { createTerminalProtocolClient } from './terminalProtocolClient'

describe('TerminalProtocolClient boundary', () => {
  it('names terminal protocol code as a protocol client, not a transport', () => {
    expect(createTerminalProtocolClient).toBeTypeOf('function')
    expect(source).toContain('class TerminalProtocolClient')
    expect(source).not.toMatch(/class LocalTerminalProtocolTransport|createLocalTerminalProtocolTransport/)
  })
})
