import type { ConnectionInfo, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from '../core/transport'

export type MockFileResponder =
  | unknown
  | Promise<unknown>
  | ((params: Record<string, unknown> | undefined) => unknown | Promise<unknown>)

export interface MockFileError {
  status: number
  body: { error?: string; message?: string }
}

export function createMockFileSession(
  responders: Record<string, MockFileResponder> = {},
  errors: Record<string, MockFileError> = {},
  options: { machineId?: string; terminalId?: string } = {},
): MockFileSession {
  return new MockFileSession(responders, errors, options.machineId ?? 'machine-local', options.terminalId)
}

export function createDeferredFileResponder(): {
  promise: Promise<unknown>
  resolve: (value: unknown) => void
} {
  let resolve!: (value: unknown) => void
  const promise = new Promise<unknown>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

export class MockFileSession implements RtcSession {
  readonly requests: Array<{ method: string; path: string; params?: Record<string, unknown> }> = []
  openApiCount = 0

  constructor(
    private readonly responders: Record<string, MockFileResponder>,
    private readonly errors: Record<string, MockFileError>,
    private readonly machineId: string,
    private readonly terminalId: string | undefined,
  ) {}

  async connect(): Promise<void> {}

  async disconnect(): Promise<void> {}

  async openTerminal(): Promise<RtcBinaryChannel> {
    throw new Error('terminal channel is not used by file manager tests')
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    this.openApiCount += 1
    return {
      request: async <TResponse>(method: string, params?: unknown): Promise<TResponse> => {
        const request = normalizeRequest(method, params)
        this.requests.push(request)
        const error = this.errors[request.path]
        if (error) {
          throw fileSessionError(error)
        }
        const responder = this.responders[request.path]
        if (typeof responder === 'function') {
          return await responder(request.params) as TResponse
        }
        if (responder !== undefined) {
          return await responder as TResponse
        }
        throw new Error(`unhandled file api route ${request.path}`)
      },
      close() {},
    }
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    return {
      label: `file:${transferId}`,
      readyState: 'open',
      send() {},
      close() {},
      onMessage() { return { close() {} } },
      onClose() { return { close() {} } },
      async waitOpen() {},
    }
  }

  subscribeEvents(_handler: (event: RtcEvent) => void): RtcSubscription {
    return { close() {} }
  }

  async getCapabilities() {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    const info: ConnectionInfo = {
      path: 'local',
      connectionId: 'mock-file-connection',
      machineId: this.machineId,
      relayInUse: false,
    }
    if (this.terminalId !== undefined) info.terminalId = this.terminalId
    return info
  }
}

function normalizeRequest(method: string, params: unknown): { method: string; path: string; params?: Record<string, unknown> } {
  if (typeof params !== 'object' || params === null) {
    return { method, path: method }
  }
  const record = params as Record<string, unknown>
  if (typeof record.path !== 'string') {
    return { method, path: method, params: record }
  }
  const request: { method: string; path: string; params?: Record<string, unknown> } = {
    method,
    path: record.path,
  }
  if (record.params !== undefined) {
    request.params = record.params as Record<string, unknown>
  }
  return request
}

function fileSessionError(error: MockFileError): Error {
  const message = error.body.error ?? error.body.message ?? `file api failed: ${error.status}`
  const err = new Error(message)
  Object.assign(err, { status: error.status, body: error.body })
  return err
}
