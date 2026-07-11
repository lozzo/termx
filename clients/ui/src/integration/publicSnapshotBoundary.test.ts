import { describe, expect, it } from 'vitest'
import packageSource from '../../package.json?raw'
import viteConfigSource from '../../vite.config.ts?raw'

describe('public snapshot boundary', () => {
  it('owns runtime schema through public proto', () => {
    expect(packageSource).toContain('../proto/runtimepb/runtime.proto')
    expect(packageSource).not.toContain('../termx-remote/')
  })

  it('does not publish the archived localweb build path', () => {
    expect(packageSource).not.toContain('build:localweb')
    expect(viteConfigSource).not.toContain('localweb')
    expect(viteConfigSource).not.toContain('TERMX_LOCAL_WEB_ORIGIN')
  })
})
