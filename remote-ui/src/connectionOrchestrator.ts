import type {
  ConnectionPath,
  RtcConnectOptions,
  RtcSession,
} from './transport'
import type { LocalHubUrlProvider } from './localHubUrlProvider'
import type { ManagedHubApi } from './managedHubApi'
import type { ManagedHubRtcConnectInput } from './managedHubRtcConnector'

export type ConnectionAttemptStage =
  | 'trying_local'
  | 'trying_public_p2p'
  | 'trying_managed'
  | 'connected'
  | 'failed'

export interface ConnectionAttemptError {
  path: ConnectionPath
  message: string
}

export interface ConnectionAttemptSnapshot {
  stage: ConnectionAttemptStage
  path?: ConnectionPath | undefined
  relayInUse?: boolean | undefined
  message: string
  errors?: ConnectionAttemptError[] | undefined
}

export interface ConnectionOrchestratorInput {
  machineId: string
  terminalId?: string | undefined
  sessionToken?: string | undefined
  hubUrls: string[]
  onSnapshot?: ((snapshot: ConnectionAttemptSnapshot) => void) | undefined
}

export interface ConnectionOrchestratorResult {
  path: ConnectionPath
  session: RtcSession
  relayInUse: boolean
}

export interface ConnectionOrchestrator {
  connect(input: ConnectionOrchestratorInput, options?: RtcConnectOptions): Promise<ConnectionOrchestratorResult>
}

export interface ConnectionOrchestratorOptions {
  localHubUrlProvider?: LocalHubUrlProvider | undefined
  managedHubApiFactory?: ((hubUrl: string) => ManagedHubApi) | undefined
  managedHubRtcConnectorFactory?: ((options: { hubUrl: string; api: ManagedHubApi }) => {
    connect(input: ManagedHubRtcConnectInput, options?: RtcConnectOptions): Promise<RtcSession>
  }) | undefined
}

export function createConnectionOrchestrator(options: ConnectionOrchestratorOptions): ConnectionOrchestrator {
  return new OrderedConnectionOrchestrator(options)
}

class OrderedConnectionOrchestrator implements ConnectionOrchestrator {
  constructor(private readonly options: ConnectionOrchestratorOptions) {}

  async connect(input: ConnectionOrchestratorInput, options: RtcConnectOptions = {}): Promise<ConnectionOrchestratorResult> {
    const failures: ConnectionAttemptError[] = []
    const ac = new AbortController()
    const signal = combineSignals(options.signal, ac.signal)
    const connectOptions = { ...options, signal }
    try {
      const local = await this.tryLocalWithTimeout(input, connectOptions, failures, 2000)
      if (local) {
        ac.abort()
        return local
      }

      const hubUrls = input.hubUrls.length > 0 ? input.hubUrls : []
      if (hubUrls.length === 0) {
        throw new Error('no hub URLs configured')
      }
      input.onSnapshot?.({
        stage: 'trying_managed',
        path: 'managed',
        message: `Racing ${hubUrls.length} hub(s)`,
      })
      const winner = await raceConnections(
        hubUrls.map((hubUrl) => () => this.connectManagedHub(input, hubUrl, connectOptions)),
        signal,
      )
      if (winner) {
        ac.abort()
        input.onSnapshot?.({
          stage: 'connected',
          path: winner.path,
          relayInUse: winner.relayInUse,
          message: 'Connected',
        })
        return winner
      }
    } catch (error) {
      throw error
    } finally {
      ac.abort()
    }

    input.onSnapshot?.({
      stage: 'failed',
      message: 'All connection paths failed',
      errors: failures,
    })
    throw new Error(`all connection paths failed: ${failures.map((failure) => `${failure.path}: ${failure.message}`).join('; ')}`)
  }

  private async tryPath(input: {
    path: ConnectionPath
    stage: ConnectionAttemptStage
    message: string
    input: ConnectionOrchestratorInput
    failures: ConnectionAttemptError[]
    options: RtcConnectOptions
    connect: () => Promise<RtcSession>
  }): Promise<ConnectionOrchestratorResult | null> {
    throwIfAborted(input.options.signal)
    input.input.onSnapshot?.({
      stage: input.stage,
      path: input.path,
      message: input.message,
    })
    try {
      const session = await input.connect()
      try {
        throwIfAborted(input.options.signal)
        const info = await session.getConnectionInfo()
        if (info.path !== input.path) {
          throw new Error(`connection path mismatch: ${info.path} != ${input.path}`)
        }
        if (info.path !== 'managed' && info.relayInUse === true) {
          throw new Error(`${info.path} connection must not report relay usage`)
        }
        const result = {
          path: info.path,
          session,
          relayInUse: info.relayInUse === true,
        }
        input.input.onSnapshot?.({
          stage: 'connected',
          path: result.path,
          relayInUse: result.relayInUse,
          message: 'Connected',
        })
        return result
      } catch (error) {
        await session.disconnect().catch(() => {})
        if (isAbortError(error, input.options.signal)) throw error
        throw error
      }
    } catch (error) {
      if (isAbortError(error, input.options.signal)) throw error
      input.failures.push({
        path: input.path,
        message: errorMessage(error),
      })
      return null
    }
  }

  private async connectLocal(input: ConnectionOrchestratorInput, options: RtcConnectOptions): Promise<RtcSession> {
    if (this.options.localHubUrlProvider && this.options.managedHubApiFactory && this.options.managedHubRtcConnectorFactory) {
      const localHubUrl = await this.options.localHubUrlProvider.getLocalHubUrl()
      if (!localHubUrl) {
        throw new Error('local Hub URL is required before opening a local connection')
      }
      if (!input.sessionToken) {
        throw new Error('session token is required before opening a local Hub connection')
      }
      const api = this.options.managedHubApiFactory(localHubUrl)
      const connector = this.options.managedHubRtcConnectorFactory({ hubUrl: localHubUrl, api })
      return connector.connect({
        machineId: input.machineId,
        ...(input.terminalId ? { terminalId: input.terminalId } : {}),
        sessionToken: input.sessionToken,
        path: 'local',
      }, options)
    }
    throw new Error('local Hub connector is not configured')
  }

  private async connectManagedHub(input: ConnectionOrchestratorInput, hubUrl: string, options: RtcConnectOptions): Promise<ConnectionOrchestratorResult> {
    if (!this.options.managedHubApiFactory || !this.options.managedHubRtcConnectorFactory) {
      throw new Error('managed Hub connector is not configured')
    }
    if (!input.sessionToken) {
      throw new Error('session token is required before opening a managed Hub connection')
    }
    const api = this.options.managedHubApiFactory(hubUrl)
    const connector = this.options.managedHubRtcConnectorFactory({ hubUrl, api })
    const session = await connector.connect({
      machineId: input.machineId,
      ...(input.terminalId ? { terminalId: input.terminalId } : {}),
      sessionToken: input.sessionToken,
      path: 'managed',
    }, options)
    try {
      const info = await session.getConnectionInfo()
      if (info.path !== 'managed') {
        throw new Error(`connection path mismatch: ${info.path} != managed`)
      }
      return {
        path: info.path,
        session,
        relayInUse: info.relayInUse === true,
      }
    } catch (error) {
      await session.disconnect().catch(() => {})
      throw error
    }
  }

  private async tryLocalWithTimeout(
    input: ConnectionOrchestratorInput,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    timeoutMs: number,
  ): Promise<ConnectionOrchestratorResult | null> {
    if (!this.options.localHubUrlProvider || !this.options.managedHubApiFactory || !this.options.managedHubRtcConnectorFactory) {
      return null
    }
    return this.tryPath({
      path: 'local',
      stage: 'trying_local',
      message: 'Trying local connection',
      input,
      failures,
      options,
      connect: () => Promise.race([
        this.connectLocal(input, options),
        delay(timeoutMs, options.signal).then(() => {
          throw new Error('local connection timed out')
        }),
      ]),
    })
  }
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (!signal?.aborted) return
  throw signal.reason instanceof Error ? signal.reason : new Error('connection orchestration aborted')
}

function isAbortError(error: unknown, signal: AbortSignal | undefined): boolean {
  return signal?.aborted === true && (error === signal.reason || errorMessage(error) === errorMessage(signal.reason))
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

async function raceConnections(
  connectors: Array<() => Promise<ConnectionOrchestratorResult>>,
  signal?: AbortSignal,
): Promise<ConnectionOrchestratorResult | null> {
  if (connectors.length === 0) return null
  return new Promise((resolve) => {
    let settled = false
    let remaining = connectors.length
    for (const connector of connectors) {
      connector().then(
        (result) => {
          if (!settled) {
            settled = true
            resolve(result)
          }
        },
        () => {
          remaining -= 1
          if (remaining === 0 && !settled) {
            settled = true
            resolve(null)
          }
        },
      )
    }
    signal?.addEventListener('abort', () => {
      if (!settled) {
        settled = true
        resolve(null)
      }
    }, { once: true })
  })
}

function combineSignals(...signals: Array<AbortSignal | undefined>): AbortSignal {
  const ac = new AbortController()
  for (const s of signals) {
    if (!s) continue
    if (s.aborted) {
      ac.abort(s.reason)
      return ac.signal
    }
    s.addEventListener('abort', () => ac.abort(s.reason), { once: true })
  }
  return ac.signal
}

function delay(ms: number, signal: AbortSignal | undefined): Promise<void> {
  if (!signal) return new Promise((resolve) => setTimeout(resolve, ms))
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup()
      resolve()
    }, ms)
    const onAbort = () => {
      cleanup()
      reject(signal.reason instanceof Error ? signal.reason : new Error('connection orchestration aborted'))
    }
    const cleanup = () => {
      clearTimeout(timer)
      signal.removeEventListener('abort', onAbort)
    }
    signal.addEventListener('abort', onAbort, { once: true })
  })
}
