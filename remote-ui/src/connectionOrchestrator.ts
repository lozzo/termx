import type {
  ConnectionPath,
  RtcConnectOptions,
  RtcSession,
} from './transport'
import type { LocalHubUrlProvider } from './localHubUrlProvider'
import type { ManagedHubApi } from './managedHubApi'
import type { ManagedHubRtcConnectInput } from './managedHubRtcConnector'
import type { ConnectionLogger } from './connectionLogger'
import { logConnectionEvent } from './connectionLogger'

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
  answerProofSecret?: string | undefined
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
  logger?: ConnectionLogger | undefined
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
    const hubUrls = input.hubUrls.length > 0 ? input.hubUrls : []
    this.log('connect_start', {
      level: 'info',
      machineId: input.machineId,
      terminalId: input.terminalId,
      details: {
        hubCount: hubUrls.length,
        hasSessionToken: Boolean(input.sessionToken),
        hasAnswerProofSecret: Boolean(input.answerProofSecret),
      },
    })
    try {
      const local = await this.tryLocalWithTimeout(input, connectOptions, failures, 2000)
      if (local) {
        ac.abort()
        this.log('connect_success', {
          level: 'info',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: local.path,
          details: { relayInUse: local.relayInUse },
        })
        return local
      }

      if (hubUrls.length === 0) {
        this.log('connect_no_hubs', {
          level: 'warn',
          machineId: input.machineId,
          terminalId: input.terminalId,
          details: { failures },
        })
        throw new Error('no hub URLs configured')
      }
      input.onSnapshot?.({
        stage: 'trying_managed',
        path: 'managed',
        message: `Racing ${hubUrls.length} hub(s)`,
      })
      const winner = await raceConnections(
        hubUrls.map((hubUrl, index) => () => this.connectManagedHub(input, hubUrl, connectOptions, index)),
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
        this.log('connect_success', {
          level: 'info',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: winner.path,
          details: { relayInUse: winner.relayInUse },
        })
        return winner
      }
    } catch (error) {
      if (!isAbortError(error, signal)) {
        this.log('connect_error', {
          level: 'error',
          machineId: input.machineId,
          terminalId: input.terminalId,
          message: errorMessage(error),
          details: { failures },
        })
      }
      throw error
    } finally {
      ac.abort()
    }

    input.onSnapshot?.({
      stage: 'failed',
      message: 'All connection paths failed',
      errors: failures,
    })
    this.log('connect_failed_all_paths', {
      level: 'error',
      machineId: input.machineId,
      terminalId: input.terminalId,
      message: 'All connection paths failed',
      details: { failures },
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
    this.log('path_attempt_start', {
      level: 'info',
      machineId: input.input.machineId,
      terminalId: input.input.terminalId,
      path: input.path,
      message: input.message,
    })
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
        this.log('path_attempt_success', {
          level: 'info',
          machineId: input.input.machineId,
          terminalId: input.input.terminalId,
          path: result.path,
          details: { relayInUse: result.relayInUse },
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
      this.log('path_attempt_failed', {
        level: 'warn',
        machineId: input.input.machineId,
        terminalId: input.input.terminalId,
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
        ...(input.answerProofSecret ? { answerProofSecret: input.answerProofSecret } : {}),
        path: 'local',
      }, options)
    }
    throw new Error('local Hub connector is not configured')
  }

  private async connectManagedHub(
    input: ConnectionOrchestratorInput,
    hubUrl: string,
    options: RtcConnectOptions,
    index = 0,
  ): Promise<ConnectionOrchestratorResult> {
    if (!this.options.managedHubApiFactory || !this.options.managedHubRtcConnectorFactory) {
      throw new Error('managed Hub connector is not configured')
    }
    if (!input.sessionToken) {
      throw new Error('session token is required before opening a managed Hub connection')
    }
    this.log('managed_hub_attempt_start', {
      level: 'info',
      machineId: input.machineId,
      terminalId: input.terminalId,
      path: 'managed',
      hubUrl,
      details: { index },
    })
    const api = this.options.managedHubApiFactory(hubUrl)
    const connector = this.options.managedHubRtcConnectorFactory({ hubUrl, api })
    let session: RtcSession
    try {
      session = await connector.connect({
        machineId: input.machineId,
        ...(input.terminalId ? { terminalId: input.terminalId } : {}),
        sessionToken: input.sessionToken,
        ...(input.answerProofSecret ? { answerProofSecret: input.answerProofSecret } : {}),
        path: 'managed',
      }, options)
    } catch (error) {
      if (isAbortError(error, options.signal)) {
        this.log('managed_hub_attempt_cancelled', {
          level: 'debug',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: 'managed',
          hubUrl,
          message: errorMessage(error),
          details: { index },
        })
        throw error
      }
      this.log('managed_hub_attempt_failed', {
        level: 'warn',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: 'managed',
        hubUrl,
        message: errorMessage(error),
        details: { index },
      })
      throw error
    }
    try {
      const info = await session.getConnectionInfo()
      if (info.path !== 'managed') {
        throw new Error(`connection path mismatch: ${info.path} != managed`)
      }
      this.log('managed_hub_attempt_success', {
        level: 'info',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: info.path,
        hubUrl,
        details: { index, relayInUse: info.relayInUse },
      })
      return {
        path: info.path,
        session,
        relayInUse: info.relayInUse === true,
      }
    } catch (error) {
      await session.disconnect().catch(() => {})
      if (isAbortError(error, options.signal)) {
        this.log('managed_hub_attempt_cancelled', {
          level: 'debug',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: 'managed',
          hubUrl,
          message: errorMessage(error),
          details: { index },
        })
        throw error
      }
      this.log('managed_hub_attempt_failed', {
        level: 'warn',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: 'managed',
        hubUrl,
        message: errorMessage(error),
        details: { index },
      })
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

  private log(event: string, input: {
    level?: 'debug' | 'info' | 'warn' | 'error'
    machineId?: string | undefined
    terminalId?: string | undefined
    path?: ConnectionPath | undefined
    hubUrl?: string | undefined
    message?: string | undefined
    details?: Record<string, unknown> | undefined
  }): void {
    logConnectionEvent(this.options.logger, {
      scope: 'orchestrator',
      event,
      ...input,
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
