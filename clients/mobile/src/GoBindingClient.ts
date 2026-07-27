import {
  ProtoBindingClient,
  ProtoBindingConnector,
  type BindingOperationCode,
  type EndpointInput,
  type ProtoBindingBackend,
} from '@anytty/ui'
import { NativeConnection } from './plugins/nativeConnection'

const OP_AUTH = 0x01
const OP_ACCEPTED = 0x20
const OP_ACK = 0x21
const OP_ERROR = 0x22
const OP_EVENT = 0x30
const RESPONSE_HEADER_BYTES = 21

type PendingBridgeRequest = {
  resolve(handle: bigint): void
  reject(error: Error): void
}

/** AndroidBindingBackend owns only the authenticated WebView-to-JNI binary bridge. */
export class AndroidBindingBackend implements ProtoBindingBackend {
  private socket: WebSocket | null = null
  private connectPromise: Promise<void> | null = null
  private nextRequestId = 0n
  private readonly pending = new Map<bigint, PendingBridgeRequest>()
  private onEvent: ((payload: Uint8Array) => void) | null = null
  private onClosed: ((error: Error) => void) | null = null
  private closed = false
  private intentionalClose = false

  start(onEvent: (payload: Uint8Array) => void, onClosed: (error: Error) => void): void {
    this.onEvent = onEvent
    this.onClosed = onClosed
  }

  async request(operation: BindingOperationCode, payload: Uint8Array, handle?: bigint, signal?: AbortSignal): Promise<bigint> {
    await this.ensureConnected()
    if (signal?.aborted) throw abortError(signal)
    const requestId = ++this.nextRequestId
    const socket = this.socket
    if (!socket || socket.readyState !== WebSocket.OPEN) throw new Error('Go binding bridge is unavailable')
    const frame = new Uint8Array(1 + 8 + (handle === undefined ? 0 : 8) + payload.byteLength)
    const view = new DataView(frame.buffer)
    view.setUint8(0, operation)
    view.setBigUint64(1, requestId)
    let offset = 9
    if (handle !== undefined) {
      view.setBigUint64(offset, handle)
      offset += 8
    }
    frame.set(payload, offset)
    const result = new Promise<bigint>((resolve, reject) => this.pending.set(requestId, { resolve, reject }))
    try {
      socket.send(frame)
    } catch (error) {
      this.pending.delete(requestId)
      throw error
    }
    return await result
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    this.intentionalClose = true
    const socket = this.socket
    this.socket = null
    socket?.close()
    this.rejectAll(new Error('Go binding bridge is closed'))
  }

  private async ensureConnected(): Promise<void> {
    if (this.closed) throw new Error('Go binding backend is closed')
    if (this.socket?.readyState === WebSocket.OPEN) return
    if (this.connectPromise) return await this.connectPromise
    this.connectPromise = this.connect()
    try {
      await this.connectPromise
    } finally {
      this.connectPromise = null
    }
  }

  private async connect(): Promise<void> {
    const endpoint = await NativeConnection.getBridgeEndpoint()
    const socket = new WebSocket(`ws://127.0.0.1:${endpoint.port}`)
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    await new Promise<void>((resolve, reject) => {
      let settled = false
      const finish = (failure?: unknown) => {
        if (settled) return
        settled = true
        globalThis.clearTimeout(timeout)
        if (failure) {
          socket.onclose = null
          socket.close()
          reject(failure)
          return
        }
        resolve()
      }
      // 网络切换期间 WebView 可能拿到刚创建但无法完成认证的 loopback socket；没有 deadline
      // 会永久阻塞整个 native generation replacement 队列。
      const timeout = globalThis.setTimeout(() => finish(new Error('Go binding bridge authentication timed out')), 5_000)
      socket.onerror = () => finish(new Error('Go binding bridge connection failed'))
      socket.onclose = () => finish(new Error('Go binding bridge closed during authentication'))
      socket.onopen = () => {
        const token = new TextEncoder().encode(endpoint.token)
        const auth = new Uint8Array(1 + token.byteLength)
        auth[0] = OP_AUTH
        auth.set(token, 1)
        try {
          socket.send(auth)
        } catch (error) {
          finish(error)
        }
      }
      socket.onmessage = (event: MessageEvent<ArrayBuffer>) => {
        let frame: ReturnType<typeof decodeBridgeFrame>
        try {
          frame = decodeBridgeFrame(new Uint8Array(event.data))
        } catch (error) {
          finish(error)
          return
        }
        if (frame.operation !== OP_ACK) {
          finish(new Error('Go binding bridge authentication failed'))
          return
        }
        socket.onmessage = (message) => this.handleMessage(socket, new Uint8Array(message.data as ArrayBuffer))
        socket.onclose = () => this.handleClosed(new Error('Go binding bridge disconnected'))
        finish()
      }
    })
  }

  private handleMessage(socket: WebSocket, bytes: Uint8Array): void {
    let frame: ReturnType<typeof decodeBridgeFrame>
    try {
      frame = decodeBridgeFrame(bytes)
    } catch (error) {
      socket.onclose = null
      socket.close()
      this.handleClosed(error instanceof Error ? error : new Error('invalid Go binding bridge frame'))
      return
    }
    if (frame.operation === OP_EVENT) {
      this.onEvent?.(frame.payload)
      return
    }
    const pending = this.pending.get(frame.requestId)
    if (!pending) return
    this.pending.delete(frame.requestId)
    if (frame.operation === OP_ERROR) {
      pending.reject(new Error(new TextDecoder().decode(frame.payload) || 'native binding request failed'))
      return
    }
    if (frame.operation !== OP_ACCEPTED && frame.operation !== OP_ACK) {
      pending.reject(new Error('unexpected native binding response'))
      return
    }
    pending.resolve(frame.handle)
  }

  private handleClosed(error: Error): void {
    this.socket = null
    this.rejectAll(error)
    if (!this.intentionalClose) this.onClosed?.(error)
  }

  private rejectAll(error: Error): void {
    for (const request of this.pending.values()) request.reject(error)
    this.pending.clear()
  }
}

/** GoBindingClient keeps the Android name while using the shared Proto binding owner. */
export class GoBindingClient extends ProtoBindingClient {
  constructor() { super(new AndroidBindingBackend()) }
}

export { ProtoBindingConnector as GoBindingConnector }
export type GoEndpointInput = EndpointInput

function decodeBridgeFrame(bytes: Uint8Array): { operation: number; requestId: bigint; handle: bigint; payload: Uint8Array } {
  if (bytes.byteLength < RESPONSE_HEADER_BYTES) throw new Error('native binding response is truncated')
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const length = view.getUint32(17)
  if (RESPONSE_HEADER_BYTES + length !== bytes.byteLength) throw new Error('native binding response length mismatch')
  return {
    operation: view.getUint8(0),
    requestId: view.getBigUint64(1),
    handle: view.getBigUint64(9),
    payload: bytes.slice(RESPONSE_HEADER_BYTES),
  }
}

function abortError(signal: AbortSignal): Error {
  return signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
}
