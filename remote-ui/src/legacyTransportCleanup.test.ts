import { describe, expect, it } from 'vitest'
import connectionReducerSource from './connectionMessageReducer.ts?raw'
import indexSource from './index.ts?raw'
import useFileManagerSource from './useFileManager.tsx?raw'

const sourceModules = import.meta.glob('./*.ts', { query: '?raw', import: 'default', eager: false })

describe('legacy transport cleanup', () => {
  it('does not keep compatibility modules for old local transport names', () => {
    expect(sourceModules).not.toHaveProperty('./localWebRtcTransport.ts')
    expect(sourceModules).not.toHaveProperty('./localTerminalProtocolTransport.ts')
  })

  it('does not export old local WebRTC or terminal transport aliases from the package barrel', () => {
    expect(indexSource).not.toMatch(/LocalWebRtc|LocalTerminalProtocolTransport|createLocalWebRtc|createLocalTerminalProtocolTransport/)
    expect(indexSource).not.toMatch(/RTCDataChannelLike|RTCPeerConnectionLike/)
    expect(indexSource).not.toMatch(/browserRtcSession|BrowserRtc/)
  })

  it('exports Web Control API adapters without browser adapter leakage', () => {
    expect(indexSource).toMatch(/createWebControlApi/)
    expect(indexSource).toMatch(/WebControlApi/)
    expect(indexSource).not.toMatch(/createBrowserRtcSession|BrowserRtcSession/)
  })

  it('keeps file manager target validation named as session validation', () => {
    expect(useFileManagerSource).not.toMatch(/assertTransportTarget|file transport/)
    expect(useFileManagerSource).toMatch(/assertSessionTarget/)
  })

  it('does not model UI lifecycle messages as transport taxonomy', () => {
    expect(connectionReducerSource).not.toMatch(/transport\.(connecting|connected|disconnected|failed|verified)/)
    expect(connectionReducerSource).toMatch(/connection\.connected/)
  })
})
