import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { LocalRemoteApp, type LocalRemoteTransportFactory } from './LocalRemoteApp'
import { createLocalAgentApi } from './localAgentApi'
import { createLocalWebRtcPeerTransport, type LocalOfferSignature } from './localWebRtcTransport'
import type { LocalAgentApi } from './transport'
import './localWebEntry.css'

export interface LocalWebAppOptions {
  root?: HTMLElement | null | undefined
  api?: Pick<LocalAgentApi, 'getStatus' | 'listTerminals'> | undefined
  createTransport?: LocalRemoteTransportFactory | undefined
}

export function mountLocalWebApp(options: LocalWebAppOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('local web root element is required')
  }
  const api = options.api ?? createLocalAgentApi()
  const createTransport = options.createTransport ?? createBrowserLocalTransportFactory()
  const root = createRoot(rootElement)
  root.render(
    <StrictMode>
      <section className="termx-local-web-shell" data-testid="termx-local-web-shell">
        <LocalRemoteApp
          api={api}
          createTransport={createTransport}
          className="termx-local-web-app"
        />
      </section>
    </StrictMode>,
  )
  return root
}

function createBrowserLocalTransportFactory(): LocalRemoteTransportFactory {
  const api = createLocalAgentApi()
  const appCertificate = globalThis.localStorage?.getItem?.('termx.local.appCertificate') ?? null
  return ({ machineId, terminalId }) => {
    if (!appCertificate) {
      throw new Error('local app certificate is required before opening a terminal')
    }
    return createLocalWebRtcPeerTransport({
      machineId,
      terminalId,
      appCertificate,
      createAnswer: (offer) => api.createRTCAnswer(offer),
      signOffer: async (input) => signLocalOffer(input),
    })
  }
}

async function signLocalOffer(input: { sessionId: string; machineId: string; terminalId: string; sdp: string }): Promise<LocalOfferSignature> {
  // TODO(remote-ui-local-pairing): replace this placeholder with browser-local app key generation,
  // certificate storage, and Ed25519 offer signing after the local pairing UI exists.
  // It intentionally does not read, download, decrypt, or store any machine private key.
  void input
  throw new Error('local app offer signing is not configured')
}

if (typeof document !== 'undefined' && document.getElementById('root')) {
  mountLocalWebApp()
}
