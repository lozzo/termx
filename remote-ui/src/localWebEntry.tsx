import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { LocalRemoteApp, type LocalRemoteSessionConnector } from './LocalRemoteApp'
import { createBrowserLocalAppCrypto, createLocalAppIdentityStore, createLocalOfferSigner } from './localAppIdentity'
import { createLocalAgentApi } from './localAgentApi'
import { createBrowserRtcInventoryEvents, createBrowserRtcSession } from './browserRtcSession'
import { createLocalRtcConnector } from './localRtcConnector'
import type { LocalAgentApi, TerminalInventoryEvents } from './transport'
import './localWebEntry.css'

export interface LocalWebAppOptions {
  root?: HTMLElement | null | undefined
  api?: Pick<LocalAgentApi, 'getStatus' | 'listTerminals'> & Partial<Pick<LocalAgentApi, 'createInventoryRTCAnswer'>> | undefined
  pairApi?: Pick<LocalAgentApi, 'pair'> | undefined
  connector?: LocalRemoteSessionConnector | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
}

export function mountLocalWebApp(options: LocalWebAppOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('local web root element is required')
  }
  const api = options.api ?? createLocalAgentApi()
  const connector = options.connector ?? createBrowserLocalConnector()
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
          connector={connector}
          inventoryEvents={inventoryEvents}
          {...(pair ? { pair } : {})}
        />
      </section>
    </StrictMode>,
  )
  return root
}

function createBrowserPairOptions(api: Pick<LocalAgentApi, 'pair'>) {
  const storage = browserStorage()
  if (!storage) return undefined
  try {
    return {
      api,
      storage: createLocalAppIdentityStore(storage),
      crypto: createBrowserLocalAppCrypto(),
      appName: 'TermX Local Web',
    }
  } catch {
    return undefined
  }
}

function createBrowserInventoryEvents(api: Partial<Pick<LocalAgentApi, 'createInventoryRTCAnswer'>>): TerminalInventoryEvents | undefined {
  const storage = browserStorage()
  if (!storage) return undefined
  try {
    const crypto = createBrowserLocalAppCrypto()
    return {
      subscribe(machineId, handler) {
        const store = createLocalAppIdentityStore(storage)
        const appCertificate = store.loadCertificate()
        if (!appCertificate || !api.createInventoryRTCAnswer) {
          return { close() {} }
        }
        const signer = createLocalOfferSigner({
          storage: store,
          crypto,
        })
        const connection = createBrowserRtcInventoryEvents({
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

function createBrowserLocalConnector(): LocalRemoteSessionConnector {
  const api = createLocalAgentApi()
  return createLocalRtcConnector({
    api,
    createSession: ({ machineId, terminalId }) => createBrowserRtcSession({
      machineId,
      ...(terminalId ? { terminalId } : {}),
    }),
    getAppCertificate() {
      const storage = browserStorage()
      if (!storage) {
        throw new Error('local app storage is required before opening a terminal')
      }
      const store = createLocalAppIdentityStore(storage)
      const appCertificate = store.loadCertificate()
      if (!appCertificate) {
        throw new Error('local app certificate is required before opening a terminal')
      }
      return appCertificate
    },
    signOffer(input) {
      const storage = browserStorage()
      if (!storage) {
        throw new Error('local app storage is required before opening a terminal')
      }
      const store = createLocalAppIdentityStore(storage)
      const signer = createLocalOfferSigner({
        storage: store,
        crypto: createBrowserLocalAppCrypto(),
      })
      return signer.signOffer(input)
    },
  })
}

function browserStorage(): Pick<Storage, 'getItem' | 'setItem'> | undefined {
  const storage = globalThis.localStorage
  if (!storage || typeof storage.getItem !== 'function' || typeof storage.setItem !== 'function') {
    return undefined
  }
  return storage
}

if (typeof document !== 'undefined' && document.getElementById('root')) {
  mountLocalWebApp()
}
