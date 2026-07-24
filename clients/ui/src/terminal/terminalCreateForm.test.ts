import { describe, expect, it } from 'vitest'
import { formatCommandLine, parseCommandLine, parseEnvironmentBlock } from './terminalCreateForm'

describe('terminal create form mapping', () => {
  it('round trips command arguments with spaces and quotes', () => {
    const command = ['/opt/My Shell/bin/zsh', '-lc', "printf '%s' ready"]
    expect(parseCommandLine(formatCommandLine(command))).toEqual(command)
  })

  it('parses pasted environment variables without losing equals signs', () => {
    expect(parseEnvironmentBlock('A=1\nexport TOKEN=a=b\n# ignored\n')).toEqual(['A=1', 'TOKEN=a=b'])
  })

  it('rejects invalid and duplicate environment keys', () => {
    expect(() => parseEnvironmentBlock('BAD-NAME=x')).toThrow(/Invalid/)
    expect(() => parseEnvironmentBlock('A=1\nA=2')).toThrow(/Duplicate/)
  })
})
