import type { RtcJsonRpcChannel } from './transport'
import type { RTCDataChannelLike } from './browserRtcSession'

export function withTimeout<T>(promise: Promise<T>, timeoutMs: number, createError: () => Error): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(createError()), timeoutMs)
    promise.then(
      (value) => {
        clearTimeout(timer)
        resolve(value)
      },
      (err: unknown) => {
        clearTimeout(timer)
        reject(err)
      },
    )
  })
}

export function messageBytes(data: unknown): Uint8Array | Promise<Uint8Array> {
  if (data instanceof Uint8Array) return data
  if (data instanceof ArrayBuffer) return new Uint8Array(data)
  if (typeof Blob !== 'undefined' && data instanceof Blob) {
    if (typeof data.arrayBuffer === 'function') {
      return data.arrayBuffer().then((buffer) => new Uint8Array(buffer))
    }
    return blobBytesWithFileReader(data)
  }
  if (typeof data === 'string') return new TextEncoder().encode(data)
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
  }
  throw new Error('unsupported data channel message')
}

function blobBytesWithFileReader(blob: Blob): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('failed to read data channel Blob'))
    reader.onload = () => {
      const result = reader.result
      if (!(result instanceof ArrayBuffer)) {
        reject(new Error('failed to read data channel Blob as ArrayBuffer'))
        return
      }
      resolve(new Uint8Array(result))
    }
    reader.readAsArrayBuffer(blob)
  })
}

export function waitChannelOpen(channel: RTCDataChannelLike): Promise<void> {
  if (channel.readyState === 'open') return Promise.resolve()
  if (channel.readyState === 'closed') return Promise.reject(new Error(`data channel ${channel.label} is closed`))
  return new Promise((resolve, reject) => {
    const onOpen = () => {
      cleanup()
      resolve()
    }
    const onClose = () => {
      cleanup()
      reject(new Error(`data channel ${channel.label} closed before opening`))
    }
    const onError = () => {
      cleanup()
      reject(new Error(`data channel ${channel.label} failed before opening`))
    }
    const cleanup = () => {
      channel.removeEventListener('open', onOpen)
      channel.removeEventListener('close', onClose)
      channel.removeEventListener('error', onError)
    }
    channel.addEventListener('open', onOpen)
    channel.addEventListener('close', onClose)
    channel.addEventListener('error', onError)
  })
}

export function waitChannelOpenWithTimeout(channel: RTCDataChannelLike, timeoutMs = 10000): Promise<void> {
  return withTimeout(
    waitChannelOpen(channel),
    timeoutMs,
    () => new Error(`timed out opening data channel ${channel.label}`),
  )
}

function parseAPIChunk(bytes: Uint8Array): { id: string; payload: Uint8Array; last: boolean } {
  if (bytes.length < 3 || bytes[0] !== 0xc0) {
    throw new Error('invalid api response chunk')
  }
  const flags = bytes[1] ?? 0
  const idLength = bytes[2] ?? 0
  const idStart = 3
  const idEnd = idStart + idLength
  if (idLength <= 0 || idEnd > bytes.length) {
    throw new Error('invalid api response chunk')
  }
  return {
    id: new TextDecoder().decode(bytes.slice(idStart, idEnd)),
    payload: bytes.slice(idEnd),
    last: (flags & 0x02) !== 0,
  }
}

function concatChunks(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0)
  const out = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    out.set(chunk, offset)
    offset += chunk.length
  }
  return out
}

function normalizeAPIRequest(method: string, params: unknown): { method: string; path: string; body?: unknown } {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) {
    return { method, path: method }
  }
  const record = params as Record<string, unknown>
  if (typeof record.path !== 'string') {
    return {
      method,
      path: method,
      body: normalizeAPIBody(record),
    }
  }
  const body = normalizeAPIBody(record.params)
  return {
    method: normalizeAPIMethod(method, record.path),
    path: record.path,
    ...(body !== undefined ? { body } : {}),
  }
}

function normalizeAPIMethod(method: string, path: string): string {
  if ((path === '/files/list' || path === '/files/stat') && method === 'GET') return 'POST'
  return method
}

function normalizeAPIBody(params: unknown): unknown {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) return params
  const record = params as Record<string, unknown>
  if (typeof record.path === 'string') {
    return { ...record, path: record.path }
  }
  return params
}

export class LocalApiChannel implements RtcJsonRpcChannel {
  private static readonly openTimeoutMs = 10000
  private static readonly responseTimeoutMs = 10000
  private nextID = 1
  private readonly waiters = new Map<string, {
    chunks: Uint8Array[]
    timeout: ReturnType<typeof setTimeout> | null
    method: string
    path: string
    startedAt: number
    resolve: (value: unknown) => void
    reject: (err: Error) => void
  }>()

  constructor(private readonly channel: RTCDataChannelLike) {
    channel.addEventListener('message', (event) => this.handleMessage((event as MessageEvent).data))
    channel.addEventListener('close', () => this.rejectPending(new Error(`api data channel ${channel.label} closed`)))
    channel.addEventListener('error', () => this.rejectPending(new Error(`api data channel ${channel.label} failed`)))
  }

  request<TResponse>(method: string, params?: unknown): Promise<TResponse> {
    const payload = normalizeAPIRequest(method, params)
    const id = `req_${this.nextID++}`
    return new Promise<TResponse>((resolve, reject) => {
      this.waiters.set(id, {
        chunks: [],
        timeout: null,
        method: payload.method,
        path: payload.path,
        startedAt: Date.now(),
        resolve: (value) => {
          this.clearWaiterTimeout(id)
          resolve(value as TResponse)
        },
        reject: (err) => {
          this.clearWaiterTimeout(id)
          reject(err)
        },
      })
      const rejectAndDelete = (err: unknown) => {
        this.rejectWaiter(id, err instanceof Error ? err : new Error(String(err)))
      }
      const sendRequest = () => {
        try {
          this.startResponseTimeout(id)
          this.logAPI('request', {
            id,
            channel: this.channel.label,
            readyState: this.channel.readyState,
            method: payload.method,
            path: payload.path,
            body: summarizeAPIValue(payload.body, payload.path),
          })
          this.channel.send(JSON.stringify({
            id,
            method: payload.method,
            path: payload.path,
            body: payload.body,
          }))
        } catch (err) {
          rejectAndDelete(normalizeChannelSendError(this.channel, err))
        }
      }
      if (this.channel.readyState === 'open') {
        sendRequest()
        return
      }
      void waitChannelOpenWithTimeout(
        this.channel,
        LocalApiChannel.openTimeoutMs,
      ).then(sendRequest, (err: unknown) => rejectAndDelete(err))
    })
  }

  close(): void {
    this.rejectPending(new Error(`api data channel ${this.channel.label} closed`))
    this.channel.close()
  }

  private handleMessage(data: unknown): void {
    try {
      const bytes = messageBytes(data)
      if (bytes instanceof Uint8Array) {
        this.handleMessageBytes(bytes)
        return
      }
      void bytes.then(
        (resolved) => this.handleMessageBytes(resolved),
        (err: unknown) => this.rejectOldestPending(err instanceof Error ? err : new Error(String(err))),
      )
    } catch (err) {
      this.rejectOldestPending(err instanceof Error ? err : new Error(String(err)))
    }
  }

  private handleMessageBytes(data: Uint8Array): void {
    let frame: { id: string; payload: Uint8Array; last: boolean }
    try {
      frame = parseAPIChunk(data)
    } catch (err) {
      this.rejectOldestPending(err instanceof Error ? err : new Error(String(err)))
      return
    }
    const waiter = this.waiters.get(frame.id)
    if (!waiter) return
    waiter.chunks.push(frame.payload)
    if (!frame.last) return
    let response: {
      status: number
      body: unknown
    }
    try {
      response = JSON.parse(new TextDecoder().decode(concatChunks(waiter.chunks))) as {
        status: number
        body: unknown
      }
    } catch (err) {
      this.rejectWaiter(frame.id, err instanceof Error ? err : new Error(String(err)))
      return
    }
    this.logAPI(response.status >= 400 ? 'response_error' : 'response', {
      id: frame.id,
      channel: this.channel.label,
      method: waiter.method,
      path: waiter.path,
      status: response.status,
      durationMs: Date.now() - waiter.startedAt,
      body: summarizeAPIValue(response.body, waiter.path),
    }, response.status >= 400 ? 'error' : 'info')
    if (response.status >= 400) {
      const error = response.body as { error?: string; message?: string }
      this.rejectWaiter(frame.id, new Error(error.error ?? error.message ?? `local api failed: ${response.status}`))
      return
    }
    this.resolveWaiter(frame.id, response.body)
  }

  private rejectPending(err: Error): void {
    for (const id of Array.from(this.waiters.keys())) {
      this.rejectWaiter(id, err)
    }
  }

  private rejectOldestPending(err: Error): void {
    const first = this.waiters.entries().next()
    if (first.done) return
    this.rejectWaiter(first.value[0], err)
  }

  private startResponseTimeout(id: string): void {
    const waiter = this.waiters.get(id)
    if (!waiter || waiter.timeout) return
    waiter.timeout = setTimeout(() => {
      this.rejectWaiter(id, new Error(`timed out waiting for api response ${id}`))
    }, LocalApiChannel.responseTimeoutMs)
  }

  private resolveWaiter(id: string, value: unknown): void {
    const waiter = this.waiters.get(id)
    if (!waiter) return
    this.waiters.delete(id)
    if (waiter.timeout) clearTimeout(waiter.timeout)
    waiter.resolve(value)
  }

  private rejectWaiter(id: string, err: Error): void {
    const waiter = this.waiters.get(id)
    if (!waiter) return
    this.logAPI('error', {
      id,
      channel: this.channel.label,
      method: waiter.method,
      path: waiter.path,
      durationMs: Date.now() - waiter.startedAt,
      error: err.message,
    }, 'error')
    this.waiters.delete(id)
    if (waiter.timeout) clearTimeout(waiter.timeout)
    waiter.reject(err)
  }

  private clearWaiterTimeout(id: string): void {
    const waiter = this.waiters.get(id)
    if (!waiter?.timeout) return
    clearTimeout(waiter.timeout)
    waiter.timeout = null
  }

  private logAPI(event: string, details: Record<string, unknown>, level: 'info' | 'error' = 'info'): void {
    if (!shouldLogAPITraffic(details.path)) return
    try {
      console[level](`[termx:webrtc-api] ${event} ${safeJSONStringify(details)}`)
    } catch {
      // Frontend diagnostics must not affect WebRTC traffic.
    }
  }
}

function normalizeChannelSendError(channel: RTCDataChannelLike, err: unknown): Error {
  const message = err instanceof Error ? err.message : String(err)
  if (
    channel.readyState !== 'open' ||
    /RTCDataChannel\.readyState is not 'open'|data channel .*not open/i.test(message)
  ) {
    return new Error(`api data channel ${channel.label} is not open`)
  }
  return err instanceof Error ? err : new Error(message)
}

function shouldLogAPITraffic(path: unknown): boolean {
  return path !== 'ping'
}

function summarizeAPIValue(value: unknown, path?: string): unknown {
  return summarizeAPIValueAtDepth(value, path, 0)
}

function summarizeAPIValueAtDepth(value: unknown, path: string | undefined, depth: number): unknown {
  if (value === undefined || value === null) return value
  if (typeof value === 'boolean' || typeof value === 'number') return value
  if (typeof value === 'string') return summarizeString(value)
  if (Array.isArray(value)) return summarizeArray(value, path, depth)
  if (typeof value !== 'object') return String(value)

  const record = value as Record<string, unknown>
  if (Array.isArray(record.terminals)) {
    return {
      ...summarizeObjectWithout(record, new Set(['terminals']), path, depth),
      terminalCount: record.terminals.length,
      terminals: record.terminals.slice(0, 25).map(summarizeTerminal),
    }
  }
  if (Array.isArray(record.entries)) {
    return {
      ...summarizeObjectWithout(record, new Set(['entries']), path, depth),
      entryCount: record.entries.length,
      entries: record.entries.slice(0, 10).map(summarizeFileEntry),
    }
  }
  return summarizeObjectWithout(record, new Set(), path, depth)
}

function summarizeObjectWithout(
  record: Record<string, unknown>,
  skipped: Set<string>,
  path: string | undefined,
  depth: number,
): Record<string, unknown> {
  if (depth >= 3) return { type: 'object' }
  const out: Record<string, unknown> = {}
  for (const [key, nested] of Object.entries(record).slice(0, 30)) {
    if (skipped.has(key)) continue
    out[key] = summarizeRecordField(key, nested, path, depth + 1)
  }
  if (Object.keys(record).length > 30) out.truncatedKeys = Object.keys(record).length - 30
  return out
}

function summarizeRecordField(key: string, value: unknown, path: string | undefined, depth: number): unknown {
  if (shouldRedactField(key)) {
    if (typeof value === 'string') return `[redacted ${value.length} chars]`
    if (value instanceof Uint8Array) return `[redacted ${value.byteLength} bytes]`
    if (value instanceof ArrayBuffer) return `[redacted ${value.byteLength} bytes]`
    return '[redacted]'
  }
  return summarizeAPIValueAtDepth(value, path, depth)
}

function summarizeArray(values: unknown[], path: string | undefined, depth: number): unknown {
  if (depth >= 3) return { type: 'array', length: values.length }
  return {
    type: 'array',
    length: values.length,
    sample: values.slice(0, 10).map((item) => summarizeAPIValueAtDepth(item, path, depth + 1)),
  }
}

function summarizeTerminal(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return { value: summarizeAPIValue(value) }
  }
  const record = value as Record<string, unknown>
  return cleanSummary({
    terminalId: firstString(record.terminal_id, record.terminalId, record.id, record.ID),
    machineId: firstString(record.machine_id, record.machineId),
    name: firstString(record.name, record.title, record.Name),
    state: firstString(record.state, record.State),
    command: summarizeCommand(record.command ?? record.Command),
    cols: firstNumber(record.cols, record.Cols),
    rows: firstNumber(record.rows, record.Rows),
  })
}

function summarizeFileEntry(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return { value: summarizeAPIValue(value) }
  }
  const record = value as Record<string, unknown>
  return cleanSummary({
    name: firstString(record.name),
    path: firstString(record.path),
    type: firstString(record.type),
    size: firstNumber(record.size),
    modifiedAt: firstString(record.modified_at, record.modifiedAt),
  })
}

function summarizeCommand(value: unknown): string | undefined {
  if (Array.isArray(value) && value.every((item) => typeof item === 'string')) {
    return value.join(' ')
  }
  return typeof value === 'string' ? value : undefined
}

function shouldRedactField(key: string): boolean {
  return /^(data|bytes|content|payload|chunk|buffer|blob)$/i.test(key)
}

function summarizeString(value: string): string {
  return value.length > 240 ? `${value.slice(0, 240)}...[${value.length} chars]` : value
}

function firstString(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value
  }
  return undefined
}

function firstNumber(...values: unknown[]): number | undefined {
  for (const value of values) {
    if (typeof value === 'number' && Number.isFinite(value)) return value
  }
  return undefined
}

function cleanSummary<T extends Record<string, unknown>>(record: T): T {
  for (const key of Object.keys(record)) {
    if (record[key] === undefined) delete record[key]
  }
  return record
}

function safeJSONStringify(value: unknown): string {
  try {
    return JSON.stringify(value)
  } catch {
    return JSON.stringify({ error: 'failed to serialize api log' })
  }
}
