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
    const body: Record<string, unknown> = { path: record.path }
    if (typeof record.offset === 'number') body.offset = record.offset
    if (typeof record.limit === 'number') body.limit = record.limit
    return body
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
