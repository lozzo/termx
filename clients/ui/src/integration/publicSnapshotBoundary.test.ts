import { describe, expect, it } from 'vitest'
import packageSource from '../../package.json?raw'
import viteConfigSource from '../../vite.config.ts?raw'

describe('public snapshot boundary', () => {
  it('generates public application and binding schemas without the migration runtime schema', () => {
    expect(packageSource).toContain('../../proto/apipb/*.proto')
    expect(packageSource).toContain('../../proto/bindingpb/client_binding.proto')
    expect(packageSource).not.toContain('runtimepb')
    expect(packageSource).not.toContain('../termx-remote/')
  })

  it('does not publish the archived localweb build path', () => {
    expect(packageSource).not.toContain('build:localweb')
    expect(viteConfigSource).not.toContain('localweb')
    expect(viteConfigSource).not.toContain('MUXVIA_LOCAL_WEB_ORIGIN')
  })
})
