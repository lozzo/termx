import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { LocalRemoteApp, type LocalRemoteTransportFactory } from './LocalRemoteApp'
import { createBrowserLocalAppCrypto, createLocalAppIdentityStore, createLocalOfferSigner } from './localAppIdentity'
import { createLocalAgentApi } from './localAgentApi'
import { createLocalWebRtcPeerTransport } from './localWebRtcTransport'
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
  return ({ machineId, terminalId }) => {
    if (!globalThis.localStorage) {
      throw new Error('local app storage is required before opening a terminal')
    }
    const store = createLocalAppIdentityStore(globalThis.localStorage)
    const appCertificate = store.loadCertificate()
    if (!appCertificate) {
      throw new Error('local app certificate is required before opening a terminal')
    }
    const signer = createLocalOfferSigner({
      storage: store,
      crypto: createBrowserLocalAppCrypto(),
    })
    return createLocalWebRtcPeerTransport({
      machineId,
      terminalId,
      appCertificate,
      createAnswer: (offer) => api.createRTCAnswer(offer),
      signOffer: (input) => signer.signOffer(input),
    })
  }
}

if (typeof document !== 'undefined' && document.getElementById('root')) {
  mountLocalWebApp()
}
