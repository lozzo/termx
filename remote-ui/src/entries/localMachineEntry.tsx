import { StrictMode, useMemo, useState, type FormEvent } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createBrowserRemoteNetworkRuntime } from '../connection/browserNetworkRuntime'
import { MachineWorkspace, type MachineWorkspaceInventoryApi, type MachineWorkspaceConnector } from '../app/MachineWorkspace'
import { createMachineSessionStore } from '../state/localAppIdentity'
import { createBrowserRtcSession } from '../webrtc/browserRtcSession'
import { consoleConnectionLogger } from '../connection/connectionLogger'
import { createHubApi } from '../api/hubApi'
import { createHubRtcConnector } from '../webrtc/hubRtcConnector'
import { normalizeTerminalInventory } from '../terminal/terminalInventory'
import type { Terminal } from '../core/model'
import type { LocalPairingApi, RemoteNetworkRuntime, RemoteRuntimeStorage, RtcConnectOptions, RtcConnector, RtcSession, TerminalInventoryEvents } from '../core/transport'
import './appStyles.css'

export interface LocalWebAppOptions {
  root?: HTMLElement | null | undefined
  api?: MachineWorkspaceInventoryApi | undefined
  pairApi?: LocalPairingApi | undefined
  connector?: MachineWorkspaceConnector | undefined
  inventoryEvents?: TerminalInventoryEvents | undefined
  networkRuntime?: RemoteNetworkRuntime | undefined
  localHubUrl?: string | undefined
}

export function mountLocalWebApp(options: LocalWebAppOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('local web root element is required')
  }
  const networkRuntime = options.networkRuntime ?? createBrowserRemoteNetworkRuntime()
  const hubUrl = options.localHubUrl ?? localHubUrlFromRuntime(networkRuntime)
  const root = createRoot(rootElement)
  root.render(
    <StrictMode>
      <LocalWebShell
        initialHubUrl={hubUrl}
        networkRuntime={networkRuntime}
        options={options}
      />
    </StrictMode>,
  )
  return root
}

function LocalWebShell({
  initialHubUrl,
  networkRuntime,
  options,
}: {
  initialHubUrl: string
  networkRuntime: RemoteNetworkRuntime
  options: LocalWebAppOptions
}) {
  const [hubUrl, setHubUrl] = useState(initialHubUrl)
  const [draftHubUrl, setDraftHubUrl] = useState(initialHubUrl)
  const [hubUrlError, setHubUrlError] = useState<string | null>(null)
  const connector = useMemo(
    () => options.connector ?? createBrowserLocalConnector(networkRuntime, hubUrl),
    [hubUrl, networkRuntime, options.connector],
  )
  const api = useMemo(
    () => options.api ?? createBrowserLocalRuntimeApi(connector, hubUrl, networkRuntime.fetch, networkRuntime.storage),
    [connector, hubUrl, networkRuntime.fetch, networkRuntime.storage, options.api],
  )
  const pairApi = useMemo(
    () => options.pairApi ?? createHubApi({ baseUrl: hubUrl, fetch: networkRuntime.fetch }),
    [hubUrl, networkRuntime.fetch, options.pairApi],
  )
  const pair = useMemo(
    () => createBrowserPairOptions(pairApi, networkRuntime.storage),
    [networkRuntime.storage, pairApi],
  )

  function submitHubUrl(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      const normalized = normalizeHubUrlInput(draftHubUrl)
      setHubUrlError(null)
      setDraftHubUrl(normalized)
      setHubUrl(normalized)
      networkRuntime.storage?.setItem('termx.local.hubUrl', normalized)
    } catch (err) {
      setHubUrlError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <section
      className="relative h-[100dvh] w-screen overflow-hidden bg-slate-50 text-zinc-950 antialiased"
      data-testid="termx-local-web-shell"
    >
      <MachineWorkspace
        api={api}
        className="h-full min-h-0"
        connector={connector}
        inventoryEvents={options.inventoryEvents}
        subscribeRuntimeInventoryEvents
        {...(pair ? { pair } : {})}
      />
      <form
        className="absolute right-3 top-3 z-50 flex max-w-[calc(100vw-1.5rem)] items-center gap-1 rounded-md border border-zinc-200 bg-white/95 p-1 shadow-sm backdrop-blur"
        data-testid="termx-local-hub-url-form"
        onSubmit={(event) => submitHubUrl(event)}
      >
        <label className="flex min-w-0 items-center gap-2">
          <span className="shrink-0 px-2 text-xs font-semibold text-zinc-500">Local hub</span>
          <input
            aria-label="Local hub URL"
            className="h-8 w-[min(52vw,18rem)] rounded border border-zinc-200 bg-zinc-50 px-2 text-sm text-zinc-900 outline-none focus:border-zinc-400"
            inputMode="url"
            spellCheck={false}
            value={draftHubUrl}
            onChange={(event) => setDraftHubUrl(event.currentTarget.value)}
          />
        </label>
        <button
          className="h-8 rounded bg-zinc-900 px-3 text-xs font-semibold text-white hover:bg-zinc-800 active:bg-zinc-700"
          type="submit"
        >
          Use
        </button>
        {hubUrlError ? (
          <span className="sr-only" role="alert">{hubUrlError}</span>
        ) : null}
      </form>
    </section>
  )
}

function createBrowserPairOptions(api: LocalPairingApi, storage: RemoteRuntimeStorage | undefined) {
  if (!storage) return undefined
  return {
    api,
    sessionStore: createMachineSessionStore(storage),
    appName: 'TermX Local Web',
  }
}

function createBrowserLocalConnector(networkRuntime: RemoteNetworkRuntime, hubUrl: string): MachineWorkspaceConnector {
  const api = createHubApi({ baseUrl: hubUrl, fetch: networkRuntime.fetch })
  const connector = createHubRtcConnector({
    api,
    hubUrl,
    logger: consoleConnectionLogger,
    createSession: ({ machineId }) => createBrowserRtcSession({
      machineId,
      path: 'local',
      logger: consoleConnectionLogger,
    }),
  })
  return {
    async connect(input, options) {
      const storage = networkRuntime.storage
      if (!storage) {
        throw new Error('local app storage is required before opening a terminal')
      }
      const sessionStore = createMachineSessionStore(storage)
      const sessionToken = sessionStore.getSessionToken(input.machineId)
      if (!sessionToken) {
        throw new Error('session token is required before opening a terminal')
      }
      const answerProofSecret = sessionStore.getAnswerProofSecret(input.machineId) ?? undefined
      return connector.connect({
        machineId: input.machineId,
        sessionToken,
        ...(answerProofSecret ? { answerProofSecret } : {}),
        path: 'local',
      }, options)
    },
  }
}

function createBrowserLocalRuntimeApi(connector: RtcConnector<{ machineId: string }>, hubUrl: string, fetchImpl: RemoteNetworkRuntime['fetch'], storage?: RemoteRuntimeStorage | undefined) {
  let machineId: string | null = null
  let cachedTerminals: Terminal[] = []
  let inventorySession: RtcSession | null = null
  let inventorySessionMachineId: string | null = null
  let inventorySessionForceRelay = false
  return {
    async getStatus() {
      const discovered = await discoverLocalAgent(hubUrl, fetchImpl)
      machineId = discovered.machineId
      cachedTerminals = discovered.terminals
      return {
        machine: {
          machineId: discovered.machineId,
          name: discovered.machineName,
          state: 'online' as const,
          terminalCount: discovered.terminals.length,
          localRTC: { signalingUrl: hubUrl },
        },
        localWeb: {
          httpUrl: hubUrl,
          rtcOfferUrl: hubUrl,
        },
      }
    },
    async listTerminals(options?: Pick<RtcConnectOptions, 'forceRelay' | 'onStatus' | 'onConnectionState'>) {
      const targetMachineId = machineId ?? 'local'
      if (!storage || !createMachineSessionStore(storage).getSessionToken(targetMachineId)) {
        return cachedTerminals
      }
      emitLocalConnectionState(options, targetMachineId, 'connecting', 'Connecting to local agent...')
      const forceRelay = options?.forceRelay === true
      if (
        !inventorySession ||
        inventorySessionMachineId !== targetMachineId ||
        inventorySessionForceRelay !== forceRelay ||
        !isSessionAlive(inventorySession)
      ) {
        await inventorySession?.disconnect().catch(() => {})
        inventorySession = await connector.connect({ machineId: targetMachineId }, options)
        inventorySessionMachineId = targetMachineId
        inventorySessionForceRelay = forceRelay
      }
      emitLocalConnectionState(options, targetMachineId, 'connecting', 'Fetching terminals...')
      const channel = await inventorySession.openApi()
      try {
        const response = await channel.request<{ terminals: Record<string, unknown>[] }>('list', {})
        emitLocalConnectionState(options, targetMachineId, 'connected', 'Connected')
        return normalizeTerminalInventory({
          machine_id: targetMachineId,
          terminals: response.terminals ?? [],
        }).terminals
      } finally {
        channel.close()
      }
    },
  }
}

function isSessionAlive(session: RtcSession): boolean {
  const candidate = session as RtcSession & { isAlive?: () => boolean }
  return typeof candidate.isAlive === 'function' ? candidate.isAlive() : true
}

function emitLocalConnectionState(
  options: Pick<RtcConnectOptions, 'onStatus' | 'onConnectionState'> | undefined,
  machineId: string,
  phase: 'connecting' | 'connected',
  statusText: string,
): void {
  options?.onStatus?.(statusText)
  options?.onConnectionState?.({
    machineId,
    phase,
    path: 'local',
    statusText,
    relayInUse: false,
  })
}

async function discoverLocalAgent(hubUrl: string, fetchImpl: RemoteNetworkRuntime['fetch']): Promise<{ machineId: string; machineName: string; terminals: Terminal[] }> {
  const response = await fetchImpl(`${hubUrl.replace(/\/+$/, '')}/api/v1/agents/online`, {
    headers: { accept: 'application/json' },
  })
  if (!response.ok) {
    throw new Error(`local hub agent discovery failed: ${response.status}`)
  }
  const body = await response.json() as { agents?: Array<Record<string, unknown>> }
  const agent = body.agents?.[0]
  const machineId = typeof agent?.machine_id === 'string' ? agent.machine_id.trim() : ''
  if (!machineId) {
    throw new Error('local hub has no online agent')
  }
  const machineName = typeof agent?.machine_name === 'string' && agent.machine_name.trim() ? agent.machine_name.trim() : machineId
  const rawTerminals = Array.isArray(agent?.terminals) ? agent.terminals : []
  const terminals = normalizeTerminalInventory({
    machine_id: machineId,
    terminals: rawTerminals,
  }).terminals
  return { machineId, machineName, terminals }
}

function localHubUrlFromRuntime(networkRuntime: RemoteNetworkRuntime): string {
  const fromQuery = networkRuntime.queryParam('hub') ?? networkRuntime.queryParam('hub_url')
  if (fromQuery?.trim()) return fromQuery.trim()
  const stored = networkRuntime.storage?.getItem('termx.local.hubUrl')?.trim()
  if (stored) return stored
  return 'http://127.0.0.1:18888'
}

function normalizeHubUrlInput(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) throw new Error('local hub URL is required')
  const withScheme = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`
  try {
    return new URL(withScheme).toString().replace(/\/+$/, '')
  } catch {
    throw new Error('local hub URL is invalid')
  }
}

if (typeof document !== 'undefined' && document.getElementById('root')) {
  mountLocalWebApp()
}
