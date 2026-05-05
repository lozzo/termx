import type {
  ConnectionPath,
  RtcConnectOptions,
  RtcConnector,
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
  machinePublicKeyFingerprint: string
  appCertificate?: unknown
  managed: {
    hubSessionId: string
    deviceId: string
  }
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
  local?: RtcConnector<{ machineId: string; terminalId?: string | undefined }> | undefined
  localHubUrlProvider?: LocalHubUrlProvider | undefined
  managedHubApiFactory?: ((hubUrl: string) => ManagedHubApi) | undefined
  managedHubRtcConnectorFactory?: ((options: { hubUrl: string; api: ManagedHubApi }) => {
    connect(input: ManagedHubRtcConnectInput, options?: RtcConnectOptions): Promise<RtcSession>
  }) | undefined
  publicP2p: RtcConnector<{
    machineId: string
    terminalId: string
    machinePublicKeyFingerprint: string
  }>
  managed: RtcConnector<{
    machineId: string
    terminalId?: string | undefined
    hubSessionId: string
    deviceId: string
  }>
}

export function createConnectionOrchestrator(options: ConnectionOrchestratorOptions): ConnectionOrchestrator {
  return new OrderedConnectionOrchestrator(options)
}

class OrderedConnectionOrchestrator implements ConnectionOrchestrator {
  constructor(private readonly options: ConnectionOrchestratorOptions) {}

  async connect(input: ConnectionOrchestratorInput, options: RtcConnectOptions = {}): Promise<ConnectionOrchestratorResult> {
    const failures: ConnectionAttemptError[] = []
    try {
      const local = await this.tryPath({
        path: 'local',
        stage: 'trying_local',
        message: 'Trying local connection',
        input,
        failures,
        options,
        connect: () => this.connectLocal(input, options),
      })
      if (local) return local

      const publicP2p = await this.tryPath({
        path: 'public_p2p',
        stage: 'trying_public_p2p',
        message: 'Trying public P2P connection',
        input,
        failures,
        options,
        connect: () => this.options.publicP2p.connect(publicP2pInput(input), options),
      })
      if (publicP2p) return publicP2p

      const managed = await this.tryPath({
        path: 'managed',
        stage: 'trying_managed',
        message: 'Trying managed connection',
        input,
        failures,
        options,
        connect: () => this.options.managed.connect(managedInput(input), options),
      })
      if (managed) return managed
    } catch (error) {
      throw error
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
      if (input.appCertificate === undefined || input.appCertificate === null) {
        throw new Error('app certificate is required before opening a local Hub connection')
      }
      const api = this.options.managedHubApiFactory(localHubUrl)
      const connector = this.options.managedHubRtcConnectorFactory({ hubUrl: localHubUrl, api })
      return connector.connect({
        machineId: input.machineId,
        ...(input.terminalId ? { terminalId: input.terminalId } : {}),
        connectTicket: input.managed.hubSessionId,
        appCertificate: input.appCertificate,
        path: 'local',
      }, options)
    }
    if (!this.options.local) {
      throw new Error('local connector is not configured')
    }
    return this.options.local.connect(localInput(input), options)
  }
}

function localInput(input: ConnectionOrchestratorInput): {
  machineId: string
  terminalId?: string | undefined
} {
  return {
    machineId: input.machineId,
    ...(input.terminalId ? { terminalId: input.terminalId } : {}),
  }
}

function publicP2pInput(input: ConnectionOrchestratorInput): {
  machineId: string
  terminalId: string
  machinePublicKeyFingerprint: string
} {
  if (!input.terminalId) {
    throw new Error('public_p2p terminalId is required')
  }
  return {
    machineId: input.machineId,
    terminalId: input.terminalId,
    machinePublicKeyFingerprint: input.machinePublicKeyFingerprint,
  }
}

function managedInput(input: ConnectionOrchestratorInput): {
  machineId: string
  terminalId?: string | undefined
  hubSessionId: string
  deviceId: string
} {
  return {
    machineId: input.machineId,
    ...(input.terminalId ? { terminalId: input.terminalId } : {}),
    hubSessionId: input.managed.hubSessionId,
    deviceId: input.managed.deviceId,
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
