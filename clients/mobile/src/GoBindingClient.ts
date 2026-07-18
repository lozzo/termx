import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import {
  TermxApiApplication,
  TermxClientBinding,
  type ProtoClientSession,
  type ProtoClientSubscription,
  type ProtoResourceStream,
} from '@termx/ui'
import { NativeConnection } from './plugins/nativeConnection'

const OP_AUTH = 0x01
const OP_OPEN_SESSION = 0x10
const OP_EXECUTE = 0x11
const OP_IMPORT_PAIRING = 0x12
const OP_DELETE_CREDENTIAL = 0x13
const OP_CANCEL = 0x14
const OP_CLOSE_SESSION = 0x15
const OP_RELEASE = 0x16
const OP_OPEN_RESOURCE_STREAM = 0x17
const OP_SEND_RESOURCE_STREAM_FRAME = 0x18
const OP_CLOSE_RESOURCE_STREAM = 0x19
const OP_ACCEPTED = 0x20
const OP_ACK = 0x21
const OP_ERROR = 0x22
const OP_EVENT = 0x30
const RESPONSE_HEADER_BYTES = 21
const MAX_EARLY_STREAM_EVENTS = 64

type PendingBridgeRequest = {
  resolve(handle: bigint): void
  reject(error: Error): void
}

type PendingOperation<T> = {
  resolve(value: T): void
  reject(error: Error): void
}

/** GoBindingClient owns one authenticated WebView-to-JNI bridge connection. */
export class GoBindingClient {
  private socket: WebSocket | null = null
  private connectPromise: Promise<void> | null = null
  private nextRequestId = 0n
  private readonly pendingBridge = new Map<bigint, PendingBridgeRequest>()
  private readonly openOperations = new Map<bigint, PendingOperation<GoProtoSession>>()
  private readonly executeOperations = new Map<bigint, PendingOperation<TermxApiApplication.ResultEnvelope>>()
  private readonly importOperations = new Map<bigint, PendingOperation<TermxClientBinding.ImportPairingResult>>()
  private readonly deleteOperations = new Map<bigint, PendingOperation<void>>()
  private readonly sessions = new Map<bigint, GoProtoSession>()
  private readonly streams = new Map<bigint, GoProtoResourceStream>()
  private readonly earlyOperationEvents = new Map<bigint, TermxClientBinding.EventEnvelope>()
  private readonly earlyStreamEvents = new Map<bigint, TermxClientBinding.EventEnvelope[]>()
  private closed = false
  private intentionalClose = false

  async openSession(request: TermxClientBinding.OpenSessionRequest, signal?: AbortSignal): Promise<ProtoClientSession> {
    const operation = await this.request(OP_OPEN_SESSION, toBinary(TermxClientBinding.OpenSessionRequestSchema, request), undefined, signal)
    return await this.waitOperation(this.openOperations, operation, signal)
  }

  async importPairing(request: TermxClientBinding.ImportPairingRequest, signal?: AbortSignal): Promise<TermxClientBinding.ImportPairingResult> {
    const operation = await this.request(OP_IMPORT_PAIRING, toBinary(TermxClientBinding.ImportPairingRequestSchema, request), undefined, signal)
    return await this.waitOperation(this.importOperations, operation, signal)
  }

  async deleteCredential(request: TermxClientBinding.DeleteCredentialRequest, signal?: AbortSignal): Promise<void> {
    const operation = await this.request(OP_DELETE_CREDENTIAL, toBinary(TermxClientBinding.DeleteCredentialRequestSchema, request), undefined, signal)
    await this.waitOperation(this.deleteOperations, operation, signal)
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    this.intentionalClose = true
    const socket = this.socket
    this.socket = null
    socket?.close()
    const error = new Error('Go binding bridge is closed')
    this.rejectAll(error)
    this.sessions.forEach((session) => session.markClosed())
    this.streams.forEach((stream) => stream.markClosed(error))
    this.sessions.clear()
    this.streams.clear()
  }

  async execute(session: bigint, command: TermxApiApplication.CommandEnvelope, signal?: AbortSignal): Promise<TermxApiApplication.ResultEnvelope> {
    const operation = await this.request(OP_EXECUTE, toBinary(TermxApiApplication.CommandEnvelopeSchema, command), session, signal)
    return await this.waitOperation(this.executeOperations, operation, signal)
  }

  async openResourceStream(session: bigint, resource: NonNullable<TermxClientBinding.OpenResourceStreamRequest['resource']>): Promise<GoProtoResourceStream> {
    const request = create(TermxClientBinding.OpenResourceStreamRequestSchema, { resource })
    const handle = await this.request(OP_OPEN_RESOURCE_STREAM, toBinary(TermxClientBinding.OpenResourceStreamRequestSchema, request), session)
    const stream = new GoProtoResourceStream(this, handle)
    this.streams.set(handle, stream)
    const early = this.earlyStreamEvents.get(handle)
    if (early) {
      this.earlyStreamEvents.delete(handle)
      for (const event of early) queueMicrotask(() => this.onEvent(event))
    }
    return stream
  }

  async sendResourceStreamFrame(handle: bigint, type: TermxClientBinding.ResourceStreamFrameType, payload: Uint8Array): Promise<void> {
    const frame = create(TermxClientBinding.ResourceStreamFrameSchema, { streamHandle: handle, type, payload })
    await this.request(OP_SEND_RESOURCE_STREAM_FRAME, toBinary(TermxClientBinding.ResourceStreamFrameSchema, frame), handle)
  }

  async closeResourceStream(handle: bigint): Promise<void> {
    await this.request(OP_CLOSE_RESOURCE_STREAM, new Uint8Array(), handle)
  }

  async closeSession(handle: bigint): Promise<void> {
    await this.request(OP_CLOSE_SESSION, new Uint8Array(), handle)
  }

  async release(handle: bigint): Promise<void> {
    await this.request(OP_RELEASE, new Uint8Array(), handle)
  }

  private async waitOperation<T>(registry: Map<bigint, PendingOperation<T>>, operation: bigint, signal?: AbortSignal): Promise<T> {
    if (signal?.aborted) {
      await this.cancel(operation)
      throw signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
    }
    return await new Promise<T>((resolve, reject) => {
      registry.set(operation, { resolve, reject })
      const abort = () => {
        registry.delete(operation)
        void this.cancel(operation)
        reject(signal?.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError'))
      }
      signal?.addEventListener('abort', abort, { once: true })
      const entry = registry.get(operation)
      if (entry) {
        entry.resolve = (value) => {
          signal?.removeEventListener('abort', abort)
          resolve(value)
        }
        entry.reject = (error) => {
          signal?.removeEventListener('abort', abort)
          reject(error)
        }
      }
      const early = this.earlyOperationEvents.get(operation)
      if (early) {
        this.earlyOperationEvents.delete(operation)
        queueMicrotask(() => this.onEvent(early))
      }
    })
  }

  private async cancel(operation: bigint): Promise<void> {
    await this.request(OP_CANCEL, new Uint8Array(), operation).catch(() => undefined)
  }

  private async request(operation: number, payload: Uint8Array, handle?: bigint, signal?: AbortSignal): Promise<bigint> {
    await this.ensureConnected()
    if (signal?.aborted) throw signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
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
    const result = new Promise<bigint>((resolve, reject) => this.pendingBridge.set(requestId, { resolve, reject }))
    try {
      socket.send(frame)
    } catch (error) {
      this.pendingBridge.delete(requestId)
      throw error
    }
    return await result
  }

  private async ensureConnected(): Promise<void> {
    if (this.closed) throw new Error('Go binding client is closed')
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
      socket.onerror = () => reject(new Error('Go binding bridge connection failed'))
      socket.onclose = () => reject(new Error('Go binding bridge closed during authentication'))
      socket.onopen = () => {
        const token = new TextEncoder().encode(endpoint.token)
        const auth = new Uint8Array(1 + token.byteLength)
        auth[0] = OP_AUTH
        auth.set(token, 1)
        try {
          socket.send(auth)
        } catch (error) {
          reject(error)
        }
      }
      const authenticate = (event: MessageEvent<ArrayBuffer>) => {
        let frame: ReturnType<typeof decodeBridgeFrame>
        try {
          frame = decodeBridgeFrame(new Uint8Array(event.data))
        } catch (error) {
          socket.close()
          reject(error)
          return
        }
        if (frame.operation !== OP_ACK) {
          reject(new Error('Go binding bridge authentication failed'))
          return
        }
        socket.onmessage = (message) => {
          try {
            this.onMessage(new Uint8Array(message.data as ArrayBuffer))
          } catch (error) {
            socket.onclose = null
            socket.close()
            this.onClosed(error instanceof Error ? error : new Error('invalid Go binding bridge frame'))
          }
        }
        socket.onclose = () => this.onClosed(new Error('Go binding bridge disconnected'))
        resolve()
      }
      socket.onmessage = authenticate
    })
  }

  private onMessage(bytes: Uint8Array): void {
    const frame = decodeBridgeFrame(bytes)
    if (frame.operation === OP_EVENT) {
      this.onEvent(fromBinary(TermxClientBinding.EventEnvelopeSchema, frame.payload))
      return
    }
    const pending = this.pendingBridge.get(frame.requestId)
    if (!pending) return
    this.pendingBridge.delete(frame.requestId)
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

  private onEvent(envelope: TermxClientBinding.EventEnvelope): void {
    const event = envelope.event
    const operationHandle = bindingOperationHandle(envelope)
    if (operationHandle !== undefined && !this.hasPendingOperation(event.case, operationHandle)) {
      this.earlyOperationEvents.set(operationHandle, envelope)
      return
    }
    switch (event.case) {
      case 'openSession': {
        const pending = this.openOperations.get(event.value.operationHandle)
        if (!pending) return
        this.openOperations.delete(event.value.operationHandle)
        if (event.value.error || !event.value.session || event.value.sessionHandle === 0n) {
          pending.reject(apiError(event.value.error, 'open session failed'))
          return
        }
        const session = new GoProtoSession(this, event.value.sessionHandle, event.value.session)
        this.sessions.set(event.value.sessionHandle, session)
        pending.resolve(session)
        void this.release(event.value.operationHandle)
        return
      }
      case 'execute': {
        const pending = this.executeOperations.get(event.value.operationHandle)
        if (!pending) return
        this.executeOperations.delete(event.value.operationHandle)
        if (event.value.error || !event.value.result) pending.reject(apiError(event.value.error, 'application command failed'))
        else pending.resolve(event.value.result)
        void this.release(event.value.operationHandle)
        return
      }
      case 'importPairing': {
        const pending = this.importOperations.get(event.value.operationHandle)
        if (!pending) return
        this.importOperations.delete(event.value.operationHandle)
        if (event.value.error) pending.reject(apiError(event.value.error, 'pairing import failed'))
        else pending.resolve(event.value)
        void this.release(event.value.operationHandle)
        return
      }
      case 'deleteCredential': {
        const pending = this.deleteOperations.get(event.value.operationHandle)
        if (!pending) return
        this.deleteOperations.delete(event.value.operationHandle)
        if (event.value.error) pending.reject(apiError(event.value.error, 'credential delete failed'))
        else pending.resolve()
        void this.release(event.value.operationHandle)
        return
      }
      case 'application':
        this.sessions.get(event.value.sessionHandle)?.publish(event.value.event)
        return
      case 'sessionClosed':
        this.sessions.get(event.value.sessionHandle)?.markClosed(apiError(event.value.error, 'session closed'))
        this.sessions.delete(event.value.sessionHandle)
        void this.release(event.value.sessionHandle)
        return
      case 'resourceStreamFrame': {
        const stream = this.streams.get(event.value.streamHandle)
        if (stream) stream.publish(event.value.type, event.value.payload)
        else this.queueEarlyStreamEvent(event.value.streamHandle, envelope)
        return
      }
      case 'resourceStreamClosed': {
        const stream = this.streams.get(event.value.streamHandle)
        if (!stream) {
          this.queueEarlyStreamEvent(event.value.streamHandle, envelope)
          return
        }
        stream.markClosed(apiError(event.value.error, 'resource stream closed'))
        this.streams.delete(event.value.streamHandle)
        void this.release(event.value.streamHandle)
        return
      }
    }
  }

  private hasPendingOperation(eventCase: TermxClientBinding.EventEnvelope['event']['case'], handle: bigint): boolean {
    switch (eventCase) {
      case 'openSession': return this.openOperations.has(handle)
      case 'execute': return this.executeOperations.has(handle)
      case 'importPairing': return this.importOperations.has(handle)
      case 'deleteCredential': return this.deleteOperations.has(handle)
      default: return true
    }
  }

  private onClosed(error: Error): void {
    this.socket = null
    this.rejectAll(error)
    this.sessions.forEach((session) => session.markClosed(error))
    this.streams.forEach((stream) => stream.markClosed(error))
    this.sessions.clear()
    this.streams.clear()
    if (!this.intentionalClose) document.dispatchEvent(new CustomEvent('termx:binding-closed', { detail: error.message }))
  }

  private rejectAll(error: Error): void {
    for (const pending of this.pendingBridge.values()) pending.reject(error)
    for (const registry of [this.openOperations, this.executeOperations, this.importOperations, this.deleteOperations]) {
      for (const pending of registry.values()) pending.reject(error)
      registry.clear()
    }
    this.pendingBridge.clear()
    this.earlyOperationEvents.clear()
    this.earlyStreamEvents.clear()
  }

  private queueEarlyStreamEvent(handle: bigint, envelope: TermxClientBinding.EventEnvelope): void {
    const pending = this.earlyStreamEvents.get(handle) ?? []
    if (pending.length >= MAX_EARLY_STREAM_EVENTS) {
      this.onClosed(new Error('Go binding early stream event queue overflow'))
      return
    }
    pending.push(envelope)
    this.earlyStreamEvents.set(handle, pending)
  }
}

export interface GoManagedEndpointInput {
  endpointId: string
  targetDeviceId: string
  deviceFingerprint: string
  credentialRef: string
  relayMode: 'auto' | 'direct' | 'relay_only' | 'smart_route'
}

/** GoBindingConnector builds only bindingpb.OpenSessionRequest; route/auth/reconnect ownership remains in Go. */
export class GoBindingConnector {
  constructor(private readonly client: () => GoBindingClient, private readonly endpoint: GoManagedEndpointInput) {}

  async connect(input: { machineId: string }, options?: { signal?: AbortSignal; forceRelay?: boolean; onStatus?: (status: string) => void; onConnectionState?: (snapshot: { machineId: string; phase: 'connecting' | 'connected' | 'failed'; statusText: string; relayInUse: boolean }) => void }): Promise<ProtoClientSession> {
    if (input.machineId !== this.endpoint.endpointId) throw new Error('managed endpoint identity mismatch')
    options?.onStatus?.('Connecting...')
    options?.onConnectionState?.({ machineId: input.machineId, phase: 'connecting', statusText: 'Connecting...', relayInUse: options.forceRelay === true })
    const managed = create(TermxClientBinding.ManagedEndpointConfigSchema, {
      targetDeviceId: this.endpoint.targetDeviceId,
      deviceFingerprint: this.endpoint.deviceFingerprint,
      credentialRef: this.endpoint.credentialRef,
      relayMode: options?.forceRelay ? TermxClientBinding.ManagedRelayMode.RELAY_ONLY : managedRelayMode(this.endpoint.relayMode),
    })
    try {
      const session = await this.client().openSession(create(TermxClientBinding.OpenSessionRequestSchema, {
        requestId: crypto.randomUUID(), endpointId: this.endpoint.endpointId,
        intent: TermxClientBinding.ConnectIntent.INTERACTIVE, managed,
      }), options?.signal)
      options?.onStatus?.('Connected')
      options?.onConnectionState?.({ machineId: input.machineId, phase: 'connected', statusText: 'Connected', relayInUse: options.forceRelay === true })
      return session
    } catch (error) {
      options?.onConnectionState?.({ machineId: input.machineId, phase: 'failed', statusText: error instanceof Error ? error.message : String(error), relayInUse: false })
      throw error
    }
  }
}

class GoProtoSession implements ProtoClientSession {
  private alive = true
  private readonly eventHandlers = new Set<(event: TermxApiApplication.EventEnvelope) => void>()

  constructor(
    private readonly client: GoBindingClient,
    private readonly handle: bigint,
    readonly stamp: NonNullable<TermxClientBinding.OpenSessionResult['session']>,
  ) {}

  execute(command: TermxApiApplication.CommandEnvelope, options?: { signal?: AbortSignal }): Promise<TermxApiApplication.ResultEnvelope> {
    if (!this.alive) return Promise.reject(new Error('Proto client session is closed'))
    return this.client.execute(this.handle, command, options?.signal)
  }

  subscribeEvents(handler: (event: TermxApiApplication.EventEnvelope) => void): ProtoClientSubscription {
    this.eventHandlers.add(handler)
    return { close: () => this.eventHandlers.delete(handler) }
  }

  openResourceStream(resource: NonNullable<TermxClientBinding.OpenResourceStreamRequest['resource']>): Promise<ProtoResourceStream> {
    if (!this.alive) return Promise.reject(new Error('Proto client session is closed'))
    return this.client.openResourceStream(this.handle, resource)
  }

  isAlive(): boolean { return this.alive }

  async close(): Promise<void> {
    if (!this.alive) return
    this.alive = false
    try {
      await this.client.closeSession(this.handle)
    } finally {
      await this.client.release(this.handle).catch(() => undefined)
    }
  }

  publish(event: TermxApiApplication.EventEnvelope | undefined): void {
    if (!event || !this.alive) return
    this.eventHandlers.forEach((handler) => handler(event))
  }

  markClosed(_error?: Error): void {
    this.alive = false
    this.eventHandlers.clear()
  }
}

class GoProtoResourceStream implements ProtoResourceStream {
  private closed = false
  private readonly handlers = new Set<(type: TermxClientBinding.ResourceStreamFrameType, payload: Uint8Array) => void>()
  private readonly closeHandlers = new Set<(error: Error) => void>()

  constructor(private readonly client: GoBindingClient, readonly handle: bigint) {}

  send(type: TermxClientBinding.ResourceStreamFrameType, payload: Uint8Array): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Proto resource stream is closed'))
    return this.client.sendResourceStreamFrame(this.handle, type, payload)
  }

  subscribe(handler: (type: TermxClientBinding.ResourceStreamFrameType, payload: Uint8Array) => void): ProtoClientSubscription {
    this.handlers.add(handler)
    return { close: () => this.handlers.delete(handler) }
  }

  subscribeClosed(handler: (error: Error) => void): ProtoClientSubscription {
    this.closeHandlers.add(handler)
    return { close: () => this.closeHandlers.delete(handler) }
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    try {
      await this.client.closeResourceStream(this.handle)
    } finally {
      await this.client.release(this.handle).catch(() => undefined)
    }
  }

  publish(type: TermxClientBinding.ResourceStreamFrameType, payload: Uint8Array): void {
    if (this.closed) return
    this.handlers.forEach((handler) => handler(type, payload.slice()))
  }

  markClosed(error: Error): void {
    if (this.closed) return
    this.closed = true
    this.closeHandlers.forEach((handler) => handler(error))
    this.handlers.clear()
    this.closeHandlers.clear()
  }
}

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

function apiError(error: { message?: string } | undefined, fallback: string): Error {
  return new Error(error?.message || fallback)
}

function bindingOperationHandle(envelope: TermxClientBinding.EventEnvelope): bigint | undefined {
  switch (envelope.event.case) {
    case 'openSession':
    case 'execute':
    case 'importPairing':
    case 'deleteCredential':
      return envelope.event.value.operationHandle
    default:
      return undefined
  }
}

function managedRelayMode(value: GoManagedEndpointInput['relayMode']): TermxClientBinding.ManagedRelayMode {
  switch (value) {
    case 'direct': return TermxClientBinding.ManagedRelayMode.DIRECT
    case 'relay_only': return TermxClientBinding.ManagedRelayMode.RELAY_ONLY
    case 'smart_route': return TermxClientBinding.ManagedRelayMode.SMART_ROUTE
    case 'auto': return TermxClientBinding.ManagedRelayMode.AUTO
  }
}
