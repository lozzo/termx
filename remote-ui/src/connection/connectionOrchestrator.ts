import type {
  ConnectionPath,
  RtcConnectOptions,
  RtcSession,
} from '../core/transport'
import { connectionStateFromAttempt } from './connectionState'
import type { LocalHubUrlProvider } from './localHubUrlProvider'
import type { ManagedHubApi } from '../api/managedHubApi'
import type { ManagedHubRtcConnectInput } from '../webrtc/managedHubRtcConnector'
import type { ConnectionLogger } from './connectionLogger'
import { logConnectionEvent } from './connectionLogger'

export type ConnectionAttemptStage =
  | 'trying_local'
  | 'trying_public_p2p'
  | 'trying_managed'
  | 'connected'
  | 'failed'

export type HubEndpointKind = 'local' | 'managed'
export type HubEndpointScope = 'loopback' | 'lan' | 'public_mapping' | 'cloud'
export type HubEndpointSource = 'pair_qr' | 'stored_machine' | 'web_control' | 'native_discovery' | 'manual'
export type ConnectionPolicy = 'local_web' | 'app_local_preferred' | 'managed_only'

export interface HubEndpoint {
  url: string
  kind: HubEndpointKind
  scope: HubEndpointScope
  source?: HubEndpointSource | undefined
}

export interface ConnectionAttemptError {
  path: ConnectionPath
  message: string
  hubUrl?: string | undefined
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
  policy?: ConnectionPolicy | undefined
  endpoints?: HubEndpoint[] | undefined
  localHubUrls?: string[] | undefined
  localFallbackHubUrls?: string[] | undefined
  hubUrls?: string[] | undefined
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
  localConnectTimeoutMs?: number | undefined
  managedConnectTimeoutMs?: number | undefined
  localPreferenceWindowMs?: number | undefined
  logger?: ConnectionLogger | undefined
}

interface PreparedEndpoint extends HubEndpoint {
  path: 'local' | 'managed'
}

interface EndpointGroups {
  local: PreparedEndpoint[]
  managed: PreparedEndpoint[]
}

interface RaceOutcome {
  result: ConnectionOrchestratorResult | null
  completed: boolean
}

export function createConnectionOrchestrator(options: ConnectionOrchestratorOptions): ConnectionOrchestrator {
  return new EndpointConnectionOrchestrator(options)
}

class EndpointConnectionOrchestrator implements ConnectionOrchestrator {
  constructor(private readonly options: ConnectionOrchestratorOptions) {}

  async connect(input: ConnectionOrchestratorInput, options: RtcConnectOptions = {}): Promise<ConnectionOrchestratorResult> {
    const failures: ConnectionAttemptError[] = []
    const ac = new AbortController()
    const signal = combineSignals(options.signal, ac.signal)
    const connectOptions = { ...options, signal }
    const policy = input.policy ?? 'app_local_preferred'
    const endpoints = this.prepareEndpoints(input, policy)
    this.log('connect_start', {
      level: 'info',
      machineId: input.machineId,
      terminalId: input.terminalId,
      details: {
        policy,
        localEndpointCount: endpoints.local.length,
        managedEndpointCount: endpoints.managed.length,
        hasSessionToken: Boolean(input.sessionToken),
        hasAnswerProofSecret: Boolean(input.answerProofSecret),
      },
    })
    try {
      let result: ConnectionOrchestratorResult | null
      if (policy === 'local_web') {
        result = await this.connectLocalOnly(input, connectOptions, failures, endpoints.local)
      } else if (policy === 'managed_only' || options.forceRelay === true) {
        result = await this.connectManagedOnly(input, connectOptions, failures, endpoints.managed, options.forceRelay === true)
      } else {
        result = await this.connectAppLocalPreferred(input, connectOptions, failures, endpoints)
      }
      if (result) {
        ac.abort()
        this.emitConnected(input, options, result)
        this.log('connect_success', {
          level: 'info',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: result.path,
          details: { relayInUse: result.relayInUse },
        })
        return result
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
    return this.failAllPaths(input, options, failures)
  }

  private prepareEndpoints(input: ConnectionOrchestratorInput, policy: ConnectionPolicy): EndpointGroups {
    const explicit = input.endpoints ?? []
    const legacy = input.endpoints ? [] : legacyEndpoints(input)
    const endpoints = compactEndpoints([...explicit, ...legacy])
    if (policy === 'local_web') {
      return {
        local: endpoints.filter((endpoint) => endpoint.kind === 'local').map((endpoint) => ({ ...endpoint, path: 'local' })),
        managed: [],
      }
    }
    if (policy === 'managed_only') {
      return {
        local: [],
        managed: endpoints.filter((endpoint) => endpoint.kind === 'managed').map((endpoint) => ({ ...endpoint, path: 'managed' })),
      }
    }
    return {
      local: endpoints.filter((endpoint) => endpoint.kind === 'local').map((endpoint) => ({ ...endpoint, path: 'local' })),
      managed: endpoints.filter((endpoint) => endpoint.kind === 'managed').map((endpoint) => ({ ...endpoint, path: 'managed' })),
    }
  }

  private async connectLocalOnly(
    input: ConnectionOrchestratorInput,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    endpoints: PreparedEndpoint[],
  ): Promise<ConnectionOrchestratorResult | null> {
    const resolved = await this.localEndpointsWithProvider(input, endpoints)
    if (resolved.length === 0) {
      throw new Error('local Hub endpoint is required for local web connections')
    }
    const outcome = await this.raceEndpointGroup({
      input,
      options,
      failures,
      endpoints: resolved,
      timeoutMs: this.options.localConnectTimeoutMs,
      stage: 'trying_local',
      path: 'local',
      message: localRaceMessage(resolved),
      event: 'local_hub_race',
    })
    return outcome.result
  }

  private async connectManagedOnly(
    input: ConnectionOrchestratorInput,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    endpoints: PreparedEndpoint[],
    forceRelay: boolean,
  ): Promise<ConnectionOrchestratorResult | null> {
    if (endpoints.length === 0) {
      throw new Error('managed Hub endpoint is required before opening this machine runtime')
    }
    const outcome = await this.raceEndpointGroup({
      input,
      options,
      failures,
      endpoints,
      timeoutMs: this.options.managedConnectTimeoutMs,
      stage: 'trying_managed',
      path: 'managed',
      message: forceRelay ? `Connecting through relay on ${endpoints.length} managed hub(s)` : `Racing ${endpoints.length} managed hub(s)`,
      event: 'managed_hub_race',
    })
    return outcome.result
  }

  private async connectAppLocalPreferred(
    input: ConnectionOrchestratorInput,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    endpoints: EndpointGroups,
  ): Promise<ConnectionOrchestratorResult | null> {
    const localEndpoints = await this.localEndpointsWithProvider(input, endpoints.local)
    const managedEndpoints = endpoints.managed
    if (localEndpoints.length === 0 && managedEndpoints.length === 0) {
      throw new Error('Hub endpoint is required before opening this machine runtime')
    }
    if (localEndpoints.length === 0) {
      return this.connectManagedOnly(input, options, failures, managedEndpoints, false)
    }
    if (managedEndpoints.length === 0) {
      return this.connectLocalOnly(input, options, failures, localEndpoints)
    }

    const localRace = this.raceEndpointGroup({
      input,
      options,
      failures,
      endpoints: localEndpoints,
      timeoutMs: this.options.localConnectTimeoutMs,
      stage: 'trying_local',
      path: 'local',
      message: localRaceMessage(localEndpoints),
      event: 'local_hub_race',
    })
    const managedRace = this.raceEndpointGroup({
      input,
      options,
      failures,
      endpoints: managedEndpoints,
      timeoutMs: this.options.managedConnectTimeoutMs,
      stage: 'trying_managed',
      path: 'managed',
      message: `Racing ${managedEndpoints.length} managed hub(s)`,
      event: 'managed_hub_race',
    })

    const first = await Promise.race([
      localRace.then((outcome) => ({ group: 'local' as const, outcome })),
      managedRace.then((outcome) => ({ group: 'managed' as const, outcome })),
    ])
    if (first.group === 'local' && first.outcome.result) {
      return first.outcome.result
    }
    if (first.group === 'local' && !first.outcome.result) {
      const managed = await managedRace
      return managed.result
    }
    if (!first.outcome.result) {
      const local = await localRace
      return local.result
    }

    const local = await waitForLocalPreference(localRace, this.options.localPreferenceWindowMs ?? 500, options.signal)
    if (local.result) {
      await first.outcome.result.session.disconnect().catch(() => {})
      return local.result
    }
    if (local.completed) {
      return first.outcome.result
    }
    void localRace.then((lateLocal) => {
      if (lateLocal.result) void lateLocal.result.session.disconnect().catch(() => {})
    })
    return first.outcome.result
  }

  private async localEndpointsWithProvider(
    input: ConnectionOrchestratorInput,
    endpoints: PreparedEndpoint[],
  ): Promise<PreparedEndpoint[]> {
    if (endpoints.length > 0 || !this.options.localHubUrlProvider) return endpoints
    const url = await this.options.localHubUrlProvider.getLocalHubUrl()
    if (!url) return endpoints
    return compactEndpoints([
      ...endpoints,
      { url, kind: 'local', scope: 'loopback', source: 'manual' },
    ]).map((endpoint) => ({ ...endpoint, path: 'local' }))
  }

  private async raceEndpointGroup(input: {
    input: ConnectionOrchestratorInput
    options: RtcConnectOptions
    failures: ConnectionAttemptError[]
    endpoints: PreparedEndpoint[]
    timeoutMs?: number | undefined
    stage: ConnectionAttemptStage
    path: 'local' | 'managed'
    message: string
    event: string
  }): Promise<RaceOutcome> {
    if (input.endpoints.length === 0) return { result: null, completed: true }
    throwIfAborted(input.options.signal)
    this.log(`${input.event}_start`, {
      level: 'info',
      machineId: input.input.machineId,
      terminalId: input.input.terminalId,
      path: input.path,
      message: input.message,
      details: { endpointCount: input.endpoints.length },
    })
    input.input.onSnapshot?.({
      stage: input.stage,
      path: input.path,
      message: input.message,
    })
    emitConnectionState(input.input, input.options, {
      stage: input.stage,
      path: input.path,
      message: input.message,
    })
    input.options.onStatus?.(input.message)
    const race = raceConnections(
      input.endpoints.map((endpoint, index) => () => this.connectEndpoint(input.input, endpoint, input.options, input.failures, index)),
      input.options.signal,
    ).then((result) => ({ type: 'result' as const, result }))
    const timeout = input.timeoutMs && input.timeoutMs > 0
      ? delay(input.timeoutMs, input.options.signal).then(() => ({ type: 'timeout' as const }))
      : null
    const outcome = timeout ? await Promise.race([race, timeout]) : await race
    if (outcome.type === 'timeout') {
      input.failures.push({
        path: input.path,
        message: `${input.path} connection timed out after ${input.timeoutMs}ms`,
      })
      this.log(`${input.event}_timeout`, {
        level: 'warn',
        machineId: input.input.machineId,
        terminalId: input.input.terminalId,
        path: input.path,
        message: `${input.path} connection timed out after ${input.timeoutMs}ms`,
        details: { endpointCount: input.endpoints.length },
      })
      return { result: null, completed: false }
    }
    if (!outcome.result) {
      this.log(`${input.event}_failed`, {
        level: 'warn',
        machineId: input.input.machineId,
        terminalId: input.input.terminalId,
        path: input.path,
        message: `all ${input.path} endpoint attempts failed`,
        details: { endpointCount: input.endpoints.length },
      })
      return { result: null, completed: true }
    }
    return { result: outcome.result, completed: true }
  }

  private async connectEndpoint(
    input: ConnectionOrchestratorInput,
    endpoint: PreparedEndpoint,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    index: number,
  ): Promise<ConnectionOrchestratorResult> {
    if (endpoint.path === 'local') {
      return this.connectLocalHub(input, endpoint, options, failures, index)
    }
    return this.connectManagedHub(input, endpoint, options, failures, index)
  }

  private async connectLocalHub(
    input: ConnectionOrchestratorInput,
    endpoint: PreparedEndpoint,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    index: number,
  ): Promise<ConnectionOrchestratorResult> {
    if (!this.options.managedHubApiFactory || !this.options.managedHubRtcConnectorFactory) {
      throw new Error('local Hub connector is not configured')
    }
    if (!input.sessionToken) {
      throw new Error('session token is required before opening a local Hub connection')
    }
    this.log('local_hub_attempt_start', {
      level: 'info',
      machineId: input.machineId,
      terminalId: input.terminalId,
      path: 'local',
      hubUrl: endpoint.url,
      details: { index, scope: endpoint.scope, source: endpoint.source },
    })
    const api = this.options.managedHubApiFactory(endpoint.url)
    const connector = this.options.managedHubRtcConnectorFactory({ hubUrl: endpoint.url, api })
    let session: RtcSession
    try {
      session = await connector.connect({
        machineId: input.machineId,
        ...(input.terminalId ? { terminalId: input.terminalId } : {}),
        sessionToken: input.sessionToken,
        ...(input.answerProofSecret ? { answerProofSecret: input.answerProofSecret } : {}),
        path: 'local',
      }, options)
    } catch (error) {
      if (isAbortError(error, options.signal)) {
        this.log('local_hub_attempt_cancelled', {
          level: 'debug',
          machineId: input.machineId,
          terminalId: input.terminalId,
          path: 'local',
          hubUrl: endpoint.url,
          message: errorMessage(error),
          details: { index },
        })
        throw error
      }
      this.recordLocalHubFailure(failures, endpoint.url, error)
      this.log('local_hub_attempt_failed', {
        level: 'warn',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: 'local',
        hubUrl: endpoint.url,
        message: errorMessage(error),
        details: { index },
      })
      throw error
    }
    try {
      const info = await session.getConnectionInfo()
      if (info.path !== 'local') {
        throw new Error(`connection path mismatch: ${info.path} != local`)
      }
      if (info.relayInUse === true) {
        throw new Error('local connection must not report relay usage')
      }
      this.log('local_hub_attempt_success', {
        level: 'info',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: info.path,
        hubUrl: endpoint.url,
        details: { index, relayInUse: info.relayInUse },
      })
      return {
        path: info.path,
        session,
        relayInUse: false,
      }
    } catch (error) {
      await session.disconnect().catch(() => {})
      if (isAbortError(error, options.signal)) throw error
      this.recordLocalHubFailure(failures, endpoint.url, error)
      this.log('local_hub_attempt_failed', {
        level: 'warn',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: 'local',
        hubUrl: endpoint.url,
        message: errorMessage(error),
        details: { index },
      })
      throw error
    }
  }

  private async connectManagedHub(
    input: ConnectionOrchestratorInput,
    endpoint: PreparedEndpoint,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
    index: number,
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
      hubUrl: endpoint.url,
      details: { index, scope: endpoint.scope, source: endpoint.source },
    })
    const api = this.options.managedHubApiFactory(endpoint.url)
    const connector = this.options.managedHubRtcConnectorFactory({ hubUrl: endpoint.url, api })
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
          hubUrl: endpoint.url,
          message: errorMessage(error),
          details: { index },
        })
        throw error
      }
      this.recordManagedHubFailure(failures, endpoint.url, error)
      this.log('managed_hub_attempt_failed', {
        level: 'warn',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: 'managed',
        hubUrl: endpoint.url,
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
        hubUrl: endpoint.url,
        details: { index, relayInUse: info.relayInUse },
      })
      return {
        path: info.path,
        session,
        relayInUse: info.relayInUse === true,
      }
    } catch (error) {
      await session.disconnect().catch(() => {})
      if (isAbortError(error, options.signal)) throw error
      this.recordManagedHubFailure(failures, endpoint.url, error)
      this.log('managed_hub_attempt_failed', {
        level: 'warn',
        machineId: input.machineId,
        terminalId: input.terminalId,
        path: 'managed',
        hubUrl: endpoint.url,
        message: errorMessage(error),
        details: { index },
      })
      throw error
    }
  }

  private emitConnected(
    input: ConnectionOrchestratorInput,
    options: RtcConnectOptions,
    result: ConnectionOrchestratorResult,
  ): void {
    input.onSnapshot?.({
      stage: 'connected',
      path: result.path,
      relayInUse: result.relayInUse,
      message: 'Connected',
    })
    emitConnectionState(input, options, {
      stage: 'connected',
      path: result.path,
      relayInUse: result.relayInUse,
      message: 'Connected',
    })
    options.onStatus?.('Connected')
  }

  private failAllPaths(
    input: ConnectionOrchestratorInput,
    options: RtcConnectOptions,
    failures: ConnectionAttemptError[],
  ): never {
    input.onSnapshot?.({
      stage: 'failed',
      message: 'All connection paths failed',
      errors: failures,
    })
    emitConnectionState(input, options, {
      stage: 'failed',
      message: 'All connection paths failed',
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

  private recordManagedHubFailure(failures: ConnectionAttemptError[], hubUrl: string, error: unknown): void {
    failures.push({
      path: 'managed',
      hubUrl,
      message: errorMessage(error),
    })
  }

  private recordLocalHubFailure(failures: ConnectionAttemptError[], hubUrl: string, error: unknown): void {
    failures.push({
      path: 'local',
      hubUrl,
      message: errorMessage(error),
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

function legacyEndpoints(input: ConnectionOrchestratorInput): HubEndpoint[] {
  return [
    ...(input.localHubUrls ?? []).map((url) => ({
      url,
      kind: 'local' as const,
      scope: inferLocalScope(url, 'lan'),
      source: 'stored_machine' as const,
    })),
    ...(input.localFallbackHubUrls ?? []).map((url) => ({
      url,
      kind: 'local' as const,
      scope: 'public_mapping' as const,
      source: 'stored_machine' as const,
    })),
    ...(input.hubUrls ?? []).map((url) => ({
      url,
      kind: 'managed' as const,
      scope: 'cloud' as const,
      source: 'web_control' as const,
    })),
  ]
}

function compactEndpoints(endpoints: readonly HubEndpoint[]): HubEndpoint[] {
  const out: HubEndpoint[] = []
  const seen = new Set<string>()
  for (const endpoint of endpoints) {
    const url = endpoint.url.trim().replace(/\/+$/, '')
    if (!url) continue
    const key = `${endpoint.kind}:${url}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push({
      ...endpoint,
      url,
    })
  }
  return out
}

function inferLocalScope(url: string, fallback: HubEndpointScope): HubEndpointScope {
  try {
    const host = new URL(url).hostname
    if (host === 'localhost' || host === '127.0.0.1' || host === '::1') return 'loopback'
    return fallback
  } catch {
    return fallback
  }
}

function localRaceMessage(endpoints: readonly PreparedEndpoint[]): string {
  const publicCount = endpoints.filter((endpoint) => endpoint.scope === 'public_mapping').length
  if (publicCount > 0 && publicCount === endpoints.length) {
    return `Racing ${endpoints.length} public local address(es)`
  }
  return `Racing ${endpoints.length} local address(es)`
}

async function waitForLocalPreference(
  localRace: Promise<RaceOutcome>,
  timeoutMs: number,
  signal: AbortSignal | undefined,
): Promise<RaceOutcome> {
  try {
    return await Promise.race([
      localRace,
      delay(timeoutMs, signal).then(() => ({ result: null, completed: false })),
    ])
  } catch (error) {
    if (isAbortError(error, signal)) throw error
    return { result: null, completed: true }
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

function emitConnectionState(
  input: Pick<ConnectionOrchestratorInput, 'machineId'>,
  options: RtcConnectOptions,
  snapshot: ConnectionAttemptSnapshot,
): void {
  options.onConnectionState?.(connectionStateFromAttempt({
    machineId: input.machineId,
    stage: snapshot.stage,
    message: snapshot.message,
    ...(snapshot.path ? { path: snapshot.path } : {}),
    relayInUse: snapshot.relayInUse,
  }))
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
