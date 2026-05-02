import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { LocalRemoteApp, type LocalRemoteTransportFactory } from './LocalRemoteApp'
import { createBrowserLocalAppCrypto, createLocalAppIdentityStore, createLocalOfferSigner } from './localAppIdentity'
import { createLocalAgentApi } from './localAgentApi'
import { createLocalWebRtcInventoryEvents, createLocalWebRtcPeerTransport } from './localWebRtcTransport'
import type { LocalAgentApi, TerminalInventoryEvents } from './transport'
import './localWebEntry.css'

export interface LocalWebAppOptions {
  root?: HTMLElement | null | undefined
  api?: Pick<LocalAgentApi, 'getStatus' | 'listTerminals'> & Partial<Pick<LocalAgentApi, 'createInventoryRTCAnswer'>> | undefined
  pairApi?: Pick<LocalAgentApi, 'pair'> | undefined
  createTransport?: LocalRemoteTransportFactory | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
}

export function mountLocalWebApp(options: LocalWebAppOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('local web root element is required')
  }
  const api = options.api ?? createLocalAgentApi()
  const createTransport = options.createTransport ?? createBrowserLocalTransportFactory()
  const inventoryEvents = options.inventoryEvents ?? createBrowserInventoryEvents(api)
  const pair = createBrowserPairOptions(options.pairApi ?? createLocalAgentApi())
  const root = createRoot(rootElement)
  root.render(
    <StrictMode>
      <section
        className="h-[100dvh] w-screen flex flex-col overflow-hidden bg-slate-50 text-zinc-950 antialiased"
        data-testid="termx-local-web-shell"
      >
        <LocalRemoteApp
          api={api}
          createTransport={createTransport}
          inventoryEvents={inventoryEvents}
          {...(pair ? { pair } : {})}
        />
      </section>
    </StrictMode>,
  )
  return root
}

function createBrowserPairOptions(api: Pick<LocalAgentApi, 'pair'>) {
  if (!globalThis.localStorage) return undefined
  try {
    return {
      api,
      storage: createLocalAppIdentityStore(globalThis.localStorage),
      crypto: createBrowserLocalAppCrypto(),
      appName: 'TermX Local Web',
    }
  } catch {
    return undefined
  }
}

function createBrowserInventoryEvents(api: Partial<Pick<LocalAgentApi, 'createInventoryRTCAnswer'>>): TerminalInventoryEvents | undefined {
  if (!globalThis.localStorage || typeof globalThis.localStorage.getItem !== 'function' || typeof globalThis.localStorage.setItem !== 'function') {
    return undefined
  }
  try {
    const crypto = createBrowserLocalAppCrypto()
    return {
      subscribe(machineId, handler) {
        const store = createLocalAppIdentityStore(globalThis.localStorage)
        const appCertificate = store.loadCertificate()
        if (!appCertificate || !api.createInventoryRTCAnswer) {
          return { close() {} }
        }
        const signer = createLocalOfferSigner({
          storage: store,
          crypto,
        })
        const connection = createLocalWebRtcInventoryEvents({
          machineId,
          appCertificate,
          createAnswer: (offer) => api.createInventoryRTCAnswer!(offer),
          signOffer: (input) => signer.signOffer(input),
        })
        return connection.subscribe(machineId, handler)
      },
    }
  } catch {
    return undefined
  }
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
