import { describe, expect, it } from 'vitest'
import config from '../vite.config'

describe('vite dev config', () => {
  it('proxies local daemon api requests to the embedded local web origin by default', () => {
    expect(config.server?.proxy?.['/api']).toMatchObject({
      target: 'http://127.0.0.1:18888',
      changeOrigin: false,
    })
  })
})
