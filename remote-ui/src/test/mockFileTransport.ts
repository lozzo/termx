import type { BinaryChannel, ConnectionInfo, JsonRpcChannel, PeerTransport } from '../transport'

export type MockFileResponder =
  | unknown
  | Promise<unknown>
  | ((params: Record<string, unknown> | undefined) => unknown | Promise<unknown>)

export interface MockFileError {
  status: number
  body: { error?: string; message?: string }
}

export function createMockFilePeerTransport(
  responders: Record<string, MockFileResponder> = {},
  errors: Record<string, MockFileError> = {},
  options: { machineId?: string; terminalId?: string } = {},
): MockFilePeerTransport {
  return new MockFilePeerTransport(responders, errors, options.machineId ?? 'machine-local', options.terminalId)
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

export class MockFilePeerTransport implements PeerTransport {
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

  async openTerminal(): Promise<BinaryChannel> {
    throw new Error('terminal channel is not used by file manager tests')
  }

  async openApi(): Promise<JsonRpcChannel> {
    this.openApiCount += 1
    return {
      request: async <TResponse>(method: string, params?: unknown): Promise<TResponse> => {
        const request = normalizeRequest(method, params)
        this.requests.push(request)
        const error = this.errors[request.path]
        if (error) {
          throw fileTransportError(error)
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

  async openFileTransfer(transferId: string): Promise<BinaryChannel> {
    return {
      label: `file:${transferId}`,
      readyState: 'open',
      send() {},
      close() {},
    }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    const info: ConnectionInfo = {
      mode: 'local',
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
    throw new Error('file api request path is required')
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

function fileTransportError(error: MockFileError): Error {
  const message = error.body.error ?? error.body.message ?? `file api failed: ${error.status}`
  const err = new Error(message)
  Object.assign(err, { status: error.status, body: error.body })
  return err
}
