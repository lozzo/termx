import type { ConnectionInfo, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from '../core/transport'

export type MockFileResponder =
  | unknown
  | Promise<unknown>
  | ((params: Record<string, unknown> | undefined) => unknown | Promise<unknown>)

export type MockFileTransferResponder =
  | Uint8Array[]
  | ((transferId: string) => Uint8Array[])

export interface MockFileError {
  status: number
  body: { error?: string; message?: string }
}

export function createMockFileSession(
  responders: Record<string, MockFileResponder> = {},
  errors: Record<string, MockFileError> = {},
  options: { machineId?: string; terminalId?: string; transfers?: Record<string, MockFileTransferResponder> } = {},
): MockFileSession {
  return new MockFileSession(responders, errors, options.machineId ?? 'machine-local', options.terminalId, options.transfers ?? {})
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
  readonly openedTransfers: string[] = []
  openApiCount = 0

  constructor(
    private readonly responders: Record<string, MockFileResponder>,
    private readonly errors: Record<string, MockFileError>,
    private readonly machineId: string,
    private readonly terminalId: string | undefined,
    private readonly transfers: Record<string, MockFileTransferResponder>,
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
        const responderKey = method.startsWith('file.') ? request.method : request.path
        const error = this.errors[responderKey]
        if (error) {
          throw fileSessionError(error)
        }
        const responder = this.responders[responderKey]
        if (typeof responder === 'function') {
          return protocolFixture(request.method, await responder(request.params)) as TResponse
        }
        if (responder !== undefined) {
          return protocolFixture(request.method, await responder) as TResponse
        }
        throw new Error(`unhandled file api method ${request.method}`)
      },
      close() {},
    }
  }

  async openFileChannel(channel: number, transferId: string): Promise<RtcBinaryChannel> {
    this.openedTransfers.push(transferId)
    const responder = this.transfers[transferId]
    const frames = typeof responder === 'function' ? responder(transferId) : responder
    return new MockFileTransferChannel(channel, transferId, frames ?? [])
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

class MockFileTransferChannel implements RtcBinaryChannel {
  readonly label: string
  readyState: 'connecting' | 'open' | 'closing' | 'closed' = 'open'
  private readonly messageHandlers = new Set<(data: Uint8Array) => void>()
  private readonly closeHandlers = new Set<() => void>()

  constructor(channel: number, transferId: string, private readonly frames: Uint8Array[]) {
    this.label = `protocol:${channel}:${transferId}`
  }

  send() {}

  close() {
    if (this.readyState === 'closed') return
    this.readyState = 'closed'
    for (const handler of Array.from(this.closeHandlers)) handler()
  }

  onMessage(handler: (data: Uint8Array) => void): RtcSubscription {
    this.messageHandlers.add(handler)
    window.setTimeout(() => {
      if (this.readyState !== 'open') return
      for (const frame of this.frames) {
        for (const current of Array.from(this.messageHandlers)) current(frame)
      }
    }, 0)
    return { close: () => { this.messageHandlers.delete(handler) } }
  }

  onClose(handler: () => void): RtcSubscription {
    this.closeHandlers.add(handler)
    return { close: () => { this.closeHandlers.delete(handler) } }
  }

  async waitOpen(): Promise<void> {}
}

function normalizeRequest(method: string, params: unknown): { method: string; path: string; params?: Record<string, unknown> } {
  if (typeof params !== 'object' || params === null) {
    return { method, path: method }
  }
  const record = params as Record<string, unknown>
  if (!method.startsWith('file.') && typeof record.path === 'string') {
    const request: { method: string; path: string; params?: Record<string, unknown> } = {
      method,
      path: record.path,
    }
    if (typeof record.params === 'object' && record.params !== null) request.params = record.params as Record<string, unknown>
    return request
  }
  return { method, path: method, params: record }
}

function fileSessionError(error: MockFileError): Error {
  const message = error.body.error ?? error.body.message ?? `file api failed: ${error.status}`
  const err = new Error(message)
  Object.assign(err, { status: error.status, body: error.body })
  return err
}

function protocolFixture(method: string, value: unknown): unknown {
  if (typeof value !== 'object' || value === null) return value
  const record = value as Record<string, unknown>
  if (method === 'file.list') {
    const directory = typeof record.path === 'string' ? record.path : '/'
    const entries = Array.isArray(record.entries) ? record.entries.map((item) => protocolEntry(directory, item)) : []
    return { path: directory, entries, next_cursor: typeof record.next_cursor === 'string' ? record.next_cursor : '' }
  }
  if (method === 'file.stat') return protocolEntry('', record)
  if (method === 'file.preview' && record.entry === undefined) {
    const path = typeof record.path === 'string' ? record.path : '/preview'
    const rawContent = record.content ?? record.content_base64
    const isText = record.is_text === true
    return {
      entry: protocolEntry('', { path, name: record.name, type: 'file', size: record.size }),
      mime_type: record.mime_type ?? 'application/octet-stream',
      content: previewBytes(rawContent, isText),
      truncated: record.truncated === true,
    }
  }
  if (method === 'file.download.open' && record.channel === undefined) {
    return {
      transfer_id: record.transfer_id,
      channel: 41,
      path: record.path ?? `/${String(record.name ?? 'download')}`,
      offset: record.offset ?? 0,
      size: record.size ?? 0,
      modified_at_unix_nano: 1,
      window_bytes: 262144,
      chunk_bytes: record.chunk_size ?? 65536,
    }
  }
  if (method === 'file.mkdir' || method === 'file.rename' || method === 'file.delete') {
    return { path: record.path ?? '', target_path: record.target_path ?? '', success: record.success ?? true, error_code: '', error_message: '' }
  }
  return value
}

function protocolEntry(directory: string, value: unknown): Record<string, unknown> {
  const entry = typeof value === 'object' && value !== null ? value as Record<string, unknown> : {}
  const name = typeof entry.name === 'string' ? entry.name : ''
  const path = typeof entry.path === 'string' ? entry.path : `${directory.replace(/\/$/, '')}/${name}` || '/'
  const modified = entry.modified_at_unix_nano ?? entry.mod_time ?? entry.modTime
  return {
    ...entry,
    path,
    name,
    type: entry.type ?? 'file',
    size: entry.size ?? 0,
    mode: typeof entry.mode === 'number' ? entry.mode : 0,
    modified_at_unix_nano: typeof modified === 'string' ? Date.parse(modified) * 1_000_000 : modified ?? 0,
    link_target: entry.link_target ?? entry.linkTarget ?? '',
  }
}

function previewBytes(value: unknown, isText: boolean): Uint8Array {
  if (value instanceof Uint8Array) return value
  if (typeof value !== 'string') return new Uint8Array()
  if (isText) return new TextEncoder().encode(value)
  const encoded = value.startsWith('data:') ? value.slice(value.indexOf(',') + 1) : value
  return Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))
}
