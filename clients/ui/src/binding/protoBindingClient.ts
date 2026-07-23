import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import * as MuxviaApiApplication from '../generated/apipb/application_pb'
import * as MuxviaApiCommon from '../generated/apipb/common_pb'
import * as MuxviaApiFile from '../generated/apipb/file_pb'
import * as MuxviaClientBinding from '../generated/bindingpb/client_binding_pb'
import * as MuxviaRemoteAuth from '../generated/remoteauthpb/remote_auth_pb'
import type { ProtoClientSession, ProtoClientSubscription, ProtoResourceStream } from '../core/protoClientSession'

export const BindingOperation = {
  OPEN_SESSION: 0x10,
  EXECUTE: 0x11,
  ENGINE_COMMAND: 0x12,
  CANCEL: 0x14,
  CLOSE_SESSION: 0x15,
  RELEASE: 0x16,
  OPEN_RESOURCE_STREAM: 0x17,
  SEND_RESOURCE_STREAM_FRAME: 0x18,
  CLOSE_RESOURCE_STREAM: 0x19,
} as const

export type BindingOperationCode = typeof BindingOperation[keyof typeof BindingOperation]

export interface ProtoBindingBackend {
  start(onEvent: (payload: Uint8Array) => void, onClosed: (error: Error) => void): void
  request(operation: BindingOperationCode, payload: Uint8Array, handle?: bigint, signal?: AbortSignal): Promise<bigint>
  close(): Promise<void>
}

type PendingOperation<T> = {
  resolve(value: T): void
  reject(error: Error): void
}

const MAX_EARLY_STREAM_EVENTS = 64
const MAX_EARLY_OPERATION_EVENTS = 4096
const MAX_ABANDONED_STREAM_HANDLES = 4096
const CANCELLED_CLEANUP_TIMEOUT_MS = 5_000

/** ProtoBindingClient is the shared Android-JNI and Web-WASM session/resource contract owner. */
export class ProtoBindingClient {
  private readonly openOperations = new Map<bigint, PendingOperation<ProtoBindingSession>>()
  private readonly executeOperations = new Map<bigint, PendingOperation<MuxviaApiApplication.ResultEnvelope>>()
  private readonly importOperations = new Map<bigint, PendingOperation<MuxviaClientBinding.ImportPairingResult>>()
  private readonly deleteOperations = new Map<bigint, PendingOperation<void>>()
  private readonly registryGetOperations = new Map<bigint, PendingOperation<MuxviaRemoteAuth.EndpointRegistryV1>>()
  private readonly endpointUpsertOperations = new Map<bigint, PendingOperation<MuxviaClientBinding.EndpointUpsertResult>>()
  private readonly endpointDeleteOperations = new Map<bigint, PendingOperation<MuxviaClientBinding.EndpointDeleteResult>>()
  private readonly endpointShareReceiveOperations = new Map<bigint, PendingOperation<MuxviaClientBinding.EndpointShareReceiveResult>>()
  private readonly endpointShareCommitOperations = new Map<bigint, PendingOperation<MuxviaClientBinding.EndpointShareCommitResult>>()
  private readonly sshCredentialProvisionOperations = new Map<bigint, PendingOperation<MuxviaClientBinding.SSHCredentialProvisionResult>>()
  private readonly sessions = new Map<bigint, ProtoBindingSession>()
  private readonly streams = new Map<bigint, ProtoBindingResourceStream>()
  private readonly earlyOperationEvents = new Map<bigint, MuxviaClientBinding.EventEnvelope>()
  private readonly cancelledOperations = new Set<bigint>()
  private readonly earlyStreamEvents = new Map<bigint, MuxviaClientBinding.EventEnvelope[]>()
  private readonly abandonedStreamHandles = new Set<bigint>()
  private readonly retiredSessionHandles = new Set<bigint>()
  private readonly releasedSessionHandles = new Set<bigint>()
  private readonly releasedSessionOrder: bigint[] = []
  private readonly releasedStreamHandles = new Set<bigint>()
  private readonly releasedStreamOrder: bigint[] = []
  private closed = false
  private intentionalClose = false

  constructor(private readonly backend: ProtoBindingBackend) {
    backend.start((payload) => this.onEvent(fromBinary(MuxviaClientBinding.EventEnvelopeSchema, payload)), (error) => this.onClosed(error))
  }

  async openSession(request: MuxviaClientBinding.OpenSessionRequest, signal?: AbortSignal): Promise<ProtoClientSession> {
    const operation = await this.backend.request(BindingOperation.OPEN_SESSION, toBinary(MuxviaClientBinding.OpenSessionRequestSchema, request), undefined, signal)
    return await this.waitOperation(this.openOperations, operation, signal)
  }

  async importPairing(request: MuxviaClientBinding.ImportPairingRequest, signal?: AbortSignal): Promise<MuxviaClientBinding.ImportPairingResult> {
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'importPairing', value: request } }), signal)
    return await this.waitOperation(this.importOperations, operation, signal)
  }

  async deleteCredential(request: MuxviaClientBinding.DeleteCredentialRequest, signal?: AbortSignal): Promise<void> {
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'deleteCredential', value: request } }), signal)
    await this.waitOperation(this.deleteOperations, operation, signal)
  }

  async getEndpointRegistry(signal?: AbortSignal): Promise<MuxviaRemoteAuth.EndpointRegistryV1> {
    const request = create(MuxviaClientBinding.EndpointRegistryGetRequestSchema, { requestId: crypto.randomUUID() })
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'endpointRegistryGet', value: request } }), signal)
    return await this.waitOperation(this.registryGetOperations, operation, signal)
  }

  async upsertEndpoint(endpoint: MuxviaRemoteAuth.EndpointConfigV1, makeDefault = false, signal?: AbortSignal): Promise<MuxviaClientBinding.EndpointUpsertResult> {
    const request = create(MuxviaClientBinding.EndpointUpsertRequestSchema, { requestId: crypto.randomUUID(), endpoint, makeDefault })
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'endpointUpsert', value: request } }), signal)
    return await this.waitOperation(this.endpointUpsertOperations, operation, signal)
  }

  async deleteEndpoint(endpointId: string, signal?: AbortSignal): Promise<MuxviaClientBinding.EndpointDeleteResult> {
    const request = create(MuxviaClientBinding.EndpointDeleteRequestSchema, { requestId: crypto.randomUUID(), endpointId })
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'endpointDelete', value: request } }), signal)
    return await this.waitOperation(this.endpointDeleteOperations, operation, signal)
  }

  async receiveEndpointShare(portableOffer: string, signal?: AbortSignal): Promise<MuxviaClientBinding.EndpointShareReceiveResult> {
    const request = create(MuxviaClientBinding.EndpointShareReceiveRequestSchema, { requestId: crypto.randomUUID(), portableOffer })
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'endpointShareReceive', value: request } }), signal)
    return await this.waitOperation(this.endpointShareReceiveOperations, operation, signal)
  }

  async commitEndpointShare(importToken: string, signal?: AbortSignal): Promise<MuxviaClientBinding.EndpointShareCommitResult> {
    const request = create(MuxviaClientBinding.EndpointShareCommitRequestSchema, { requestId: crypto.randomUUID(), importToken })
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'endpointShareCommit', value: request } }), signal)
    return await this.waitOperation(this.endpointShareCommitOperations, operation, signal)
  }

  async provisionSSHCredential(endpointId: string, routeId: string, signal?: AbortSignal): Promise<MuxviaClientBinding.SSHCredentialProvisionResult> {
    const request = create(MuxviaClientBinding.SSHCredentialProvisionRequestSchema, { requestId: crypto.randomUUID(), endpointId, routeId })
    const operation = await this.engineCommand(create(MuxviaClientBinding.EngineCommandSchema, { command: { case: 'sshCredentialProvision', value: request } }), signal)
    return await this.waitOperation(this.sshCredentialProvisionOperations, operation, signal)
  }

  private async engineCommand(command: MuxviaClientBinding.EngineCommand, signal?: AbortSignal): Promise<bigint> {
    return await this.backend.request(BindingOperation.ENGINE_COMMAND, toBinary(MuxviaClientBinding.EngineCommandSchema, command), undefined, signal)
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    this.intentionalClose = true
    await this.backend.close()
    const error = new Error('Proto binding client is closed')
    this.rejectAll(error)
    this.sessions.forEach((session) => session.markClosed())
    this.streams.forEach((stream) => stream.markClosed(error))
    this.sessions.clear()
    this.streams.clear()
  }

  async execute(session: bigint, command: MuxviaApiApplication.CommandEnvelope, signal?: AbortSignal): Promise<MuxviaApiApplication.ResultEnvelope> {
    const operation = await this.backend.request(BindingOperation.EXECUTE, toBinary(MuxviaApiApplication.CommandEnvelopeSchema, command), session, signal)
    return await this.waitOperation(this.executeOperations, operation, signal)
  }

  async openResourceStream(session: bigint, resource: NonNullable<MuxviaClientBinding.OpenResourceStreamRequest['resource']>, options?: { initialUploadOffset?: bigint; signal?: AbortSignal }): Promise<ProtoBindingResourceStream> {
    const request = create(MuxviaClientBinding.OpenResourceStreamRequestSchema, { resource, initialUploadOffset: options?.initialUploadOffset ?? 0n })
    const handle = await awaitAbortableHandle(
      this.backend.request(BindingOperation.OPEN_RESOURCE_STREAM, toBinary(MuxviaClientBinding.OpenResourceStreamRequestSchema, request), session, options?.signal),
      options?.signal,
      (late) => this.abandonResourceStream(late),
    )
    const stream = new ProtoBindingResourceStream(this, handle)
    this.streams.set(handle, stream)
    const early = this.earlyStreamEvents.get(handle)
    if (early) {
      this.earlyStreamEvents.delete(handle)
      for (const event of early) queueMicrotask(() => this.onEvent(event))
    }
    return stream
  }

  async sendResourceStreamFrame(handle: bigint, type: MuxviaClientBinding.ResourceStreamFrameType, payload: Uint8Array): Promise<void> {
    const frame = create(MuxviaClientBinding.ResourceStreamFrameSchema, { streamHandle: handle, type, payload })
    await this.backend.request(BindingOperation.SEND_RESOURCE_STREAM_FRAME, toBinary(MuxviaClientBinding.ResourceStreamFrameSchema, frame), handle)
  }

  async closeResourceStream(handle: bigint): Promise<void> {
    await this.backend.request(BindingOperation.CLOSE_RESOURCE_STREAM, new Uint8Array(), handle)
  }

  async closeSession(handle: bigint): Promise<void> {
    await this.backend.request(BindingOperation.CLOSE_SESSION, new Uint8Array(), handle)
  }

  async release(handle: bigint): Promise<void> {
    await this.backend.request(BindingOperation.RELEASE, new Uint8Array(), handle)
  }

  private async waitOperation<T>(registry: Map<bigint, PendingOperation<T>>, operation: bigint, signal?: AbortSignal): Promise<T> {
    if (signal?.aborted) {
      this.abandonOperation(registry, operation)
      throw abortError(signal)
    }
    return await new Promise<T>((resolve, reject) => {
      registry.set(operation, { resolve, reject })
      const abort = () => {
        this.abandonOperation(registry, operation)
        reject(signal ? abortError(signal) : new DOMException('Aborted', 'AbortError'))
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
    await this.backend.request(BindingOperation.CANCEL, new Uint8Array(), operation).catch(() => undefined)
  }

  private abandonOperation<T>(registry: Map<bigint, PendingOperation<T>>, operation: bigint): void {
    registry.delete(operation)
    const completed = this.earlyOperationEvents.get(operation)
    if (completed) {
      this.earlyOperationEvents.delete(operation)
      this.retireCancelledOperation(operation, completed)
      return
    }
    this.cancelledOperations.add(operation)
    void this.cancel(operation)
  }

  private onEvent(envelope: MuxviaClientBinding.EventEnvelope): void {
    const event = envelope.event
    const operationHandle = bindingOperationHandle(envelope)
    if (operationHandle !== undefined && this.cancelledOperations.delete(operationHandle)) {
      this.retireCancelledOperation(operationHandle, envelope)
      return
    }
    if (operationHandle !== undefined && !this.hasPendingOperation(event.case, operationHandle)) {
      if (!this.earlyOperationEvents.has(operationHandle) && this.earlyOperationEvents.size >= MAX_EARLY_OPERATION_EVENTS) {
        this.onClosed(new Error('Proto binding early operation event queue overflow'))
        return
      }
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
          void this.release(event.value.operationHandle)
          return
        }
        const session = new ProtoBindingSession(this, event.value.sessionHandle, event.value.session)
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
      case 'endpointRegistryGet': {
        const pending = this.registryGetOperations.get(event.value.operationHandle)
        if (!pending) return
        this.registryGetOperations.delete(event.value.operationHandle)
        if (event.value.error || !event.value.registry) pending.reject(apiError(event.value.error, 'endpoint registry get failed'))
        else pending.resolve(event.value.registry)
        void this.release(event.value.operationHandle)
        return
      }
      case 'endpointUpsert': {
        const pending = this.endpointUpsertOperations.get(event.value.operationHandle)
        if (!pending) return
        this.endpointUpsertOperations.delete(event.value.operationHandle)
        if (event.value.error) pending.reject(apiError(event.value.error, 'endpoint upsert failed'))
        else pending.resolve(event.value)
        void this.release(event.value.operationHandle)
        return
      }
      case 'endpointDelete': {
        const pending = this.endpointDeleteOperations.get(event.value.operationHandle)
        if (!pending) return
        this.endpointDeleteOperations.delete(event.value.operationHandle)
        if (event.value.error) pending.reject(apiError(event.value.error, 'endpoint delete failed'))
        else pending.resolve(event.value)
        void this.release(event.value.operationHandle)
        return
      }
      case 'endpointShareReceive': {
        const pending = this.endpointShareReceiveOperations.get(event.value.operationHandle)
        if (!pending) return
        this.endpointShareReceiveOperations.delete(event.value.operationHandle)
        if (event.value.error || !event.value.preview) pending.reject(apiError(event.value.error, 'endpoint share receive failed'))
        else pending.resolve(event.value)
        void this.release(event.value.operationHandle)
        return
      }
      case 'endpointShareCommit': {
        const pending = this.endpointShareCommitOperations.get(event.value.operationHandle)
        if (!pending) return
        this.endpointShareCommitOperations.delete(event.value.operationHandle)
        if (event.value.error || !event.value.endpoint || !event.value.registry) pending.reject(apiError(event.value.error, 'endpoint share commit failed'))
        else pending.resolve(event.value)
        void this.release(event.value.operationHandle)
        return
      }
      case 'sshCredentialProvision': {
        const pending = this.sshCredentialProvisionOperations.get(event.value.operationHandle)
        if (!pending) return
        this.sshCredentialProvisionOperations.delete(event.value.operationHandle)
        if (event.value.error || !event.value.endpoint || !event.value.registry || !event.value.authorizedKey) {
          pending.reject(apiError(event.value.error, 'SSH credential provision failed'))
        } else {
          pending.resolve(event.value)
        }
        void this.release(event.value.operationHandle)
        return
      }
      case 'application':
        this.sessions.get(event.value.sessionHandle)?.publish(event.value.event)
        return
      case 'sessionClosed':
		if (this.releasedSessionHandles.has(event.value.sessionHandle)) return
		rememberHandle(this.releasedSessionHandles, this.releasedSessionOrder, event.value.sessionHandle)
		if (!this.retiredSessionHandles.delete(event.value.sessionHandle)) {
		  this.sessions.get(event.value.sessionHandle)?.markClosed(apiError(event.value.error, 'session closed'))
		  this.sessions.delete(event.value.sessionHandle)
		}
		void this.release(event.value.sessionHandle).catch((error) => this.onClosed(new Error(`closed session release failed: ${errorMessage(error)}`)))
        return
      case 'resourceStreamFrame': {
        if (this.abandonedStreamHandles.has(event.value.streamHandle)) return
        const stream = this.streams.get(event.value.streamHandle)
        if (stream) stream.publish(event.value.type, event.value.payload)
        else this.queueEarlyStreamEvent(event.value.streamHandle, envelope)
        return
      }
      case 'resourceStreamClosed': {
        if (this.abandonedStreamHandles.delete(event.value.streamHandle)) {
          this.earlyStreamEvents.delete(event.value.streamHandle)
			if (this.releasedStreamHandles.has(event.value.streamHandle)) return
			rememberHandle(this.releasedStreamHandles, this.releasedStreamOrder, event.value.streamHandle)
          return
        }
        const stream = this.streams.get(event.value.streamHandle)
        if (!stream) {
          this.queueEarlyStreamEvent(event.value.streamHandle, envelope)
          return
        }
		if (this.releasedStreamHandles.has(event.value.streamHandle)) return
		rememberHandle(this.releasedStreamHandles, this.releasedStreamOrder, event.value.streamHandle)
        stream.markClosed(apiError(event.value.error, 'resource stream closed'))
        this.streams.delete(event.value.streamHandle)
        void this.release(event.value.streamHandle).catch((error) => this.onClosed(new Error(`closed resource stream release failed: ${errorMessage(error)}`)))
        return
      }
    }
  }

  private hasPendingOperation(eventCase: MuxviaClientBinding.EventEnvelope['event']['case'], handle: bigint): boolean {
    switch (eventCase) {
      case 'openSession': return this.openOperations.has(handle)
      case 'execute': return this.executeOperations.has(handle)
      case 'importPairing': return this.importOperations.has(handle)
      case 'deleteCredential': return this.deleteOperations.has(handle)
      case 'endpointRegistryGet': return this.registryGetOperations.has(handle)
      case 'endpointUpsert': return this.endpointUpsertOperations.has(handle)
      case 'endpointDelete': return this.endpointDeleteOperations.has(handle)
      case 'endpointShareReceive': return this.endpointShareReceiveOperations.has(handle)
      case 'endpointShareCommit': return this.endpointShareCommitOperations.has(handle)
      case 'sshCredentialProvision': return this.sshCredentialProvisionOperations.has(handle)
      default: return true
    }
  }

  private onClosed(error: Error): void {
    if (this.closed) return
    this.closed = true
    void this.backend.close().catch(() => undefined)
    this.rejectAll(error)
    this.sessions.forEach((session) => session.markClosed(error))
    this.streams.forEach((stream) => stream.markClosed(error))
    this.sessions.clear()
    this.streams.clear()
    if (!this.intentionalClose) document.dispatchEvent(new CustomEvent('muxvia:binding-closed', { detail: error.message }))
  }

  private rejectAll(error: Error): void {
    for (const registry of [this.openOperations, this.executeOperations, this.importOperations, this.deleteOperations, this.registryGetOperations, this.endpointUpsertOperations, this.endpointDeleteOperations, this.endpointShareReceiveOperations, this.endpointShareCommitOperations, this.sshCredentialProvisionOperations]) {
      for (const pending of registry.values()) pending.reject(error)
      registry.clear()
    }
    this.earlyOperationEvents.clear()
    this.cancelledOperations.clear()
    this.earlyStreamEvents.clear()
    this.abandonedStreamHandles.clear()
    this.retiredSessionHandles.clear()
	this.releasedSessionHandles.clear()
	this.releasedSessionOrder.length = 0
	this.releasedStreamHandles.clear()
	this.releasedStreamOrder.length = 0
  }

  private queueEarlyStreamEvent(handle: bigint, envelope: MuxviaClientBinding.EventEnvelope): void {
    if (this.abandonedStreamHandles.has(handle)) return
    const pending = this.earlyStreamEvents.get(handle) ?? []
    if (pending.length >= MAX_EARLY_STREAM_EVENTS) {
      this.onClosed(new Error('Proto binding early stream event queue overflow'))
      return
    }
    pending.push(envelope)
    this.earlyStreamEvents.set(handle, pending)
  }

  private abandonResourceStream(handle: bigint): void {
    const early = this.earlyStreamEvents.get(handle)
    const terminalClosed = early?.some((event) => event.event.case === 'resourceStreamClosed') === true
    this.earlyStreamEvents.delete(handle)
	if (terminalClosed) rememberHandle(this.releasedStreamHandles, this.releasedStreamOrder, handle)
    if (!terminalClosed) {
      if (this.abandonedStreamHandles.size >= MAX_ABANDONED_STREAM_HANDLES) {
        this.onClosed(new Error('Proto binding abandoned stream handle capacity is exhausted'))
        return
      }
      this.abandonedStreamHandles.add(handle)
    }
    void this.closeResourceStream(handle)
      .then(() => this.release(handle))
      .catch((error) => this.onClosed(new Error(`abandoned resource stream cleanup failed: ${errorMessage(error)}`)))
  }

  private async cleanupCancelledExecute(result: MuxviaClientBinding.ExecuteResult): Promise<void> {
    if (result.error && result.error.code !== MuxviaApiCommon.ApiErrorCode.CANCELLED) {
      throw apiError(result.error, 'cancelled operation cleanup failed')
    }
    const session = this.sessions.get(result.sessionHandle)
    const transfer = result.result?.result.case === 'fileTransferOpen' ? result.result.result.value.transfer : undefined
    if (!session || !transfer?.resource) return
    const cleanup = transfer.resume
      ? create(MuxviaApiApplication.CommandEnvelopeSchema, { command: { case: 'fileTransferCancel', value: create(MuxviaApiFile.FileTransferCancelCommandSchema, { uploadResume: transfer.resume }) } })
      : create(MuxviaApiApplication.CommandEnvelopeSchema, { command: { case: 'releaseResource', value: create(MuxviaApiApplication.ReleaseResourceCommandSchema, { resource: transfer.resource }) } })
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(new DOMException('cancelled operation cleanup timed out', 'TimeoutError')), CANCELLED_CLEANUP_TIMEOUT_MS)
    try {
      const cleanupResult = await session.execute(cleanup, { signal: controller.signal })
      if (transfer.resume && (cleanupResult.result.case !== 'fileTransferCancel' || !cleanupResult.result.value.cancelled)) {
        throw new Error('cancelled upload cleanup was not confirmed')
      }
    } finally {
      clearTimeout(timeout)
    }
  }

  private retireCancelledOperation(operationHandle: bigint, envelope: MuxviaClientBinding.EventEnvelope): void {
    const event = envelope.event
    if (event.case === 'execute') {
      void this.cleanupCancelledExecute(event.value).catch((error) => this.onClosed(new Error(`cancelled operation cleanup failed: ${errorMessage(error)}`)))
    } else if (event.case === 'openSession' && event.value.sessionHandle !== 0n) {
	  if (this.retiredSessionHandles.size >= MAX_ABANDONED_STREAM_HANDLES) {
		this.onClosed(new Error('Proto binding retired session handle capacity is exhausted'))
		return
	  }
	  this.retiredSessionHandles.add(event.value.sessionHandle)
      void this.closeSession(event.value.sessionHandle)
        .catch((error) => this.onClosed(new Error(`cancelled session cleanup failed: ${errorMessage(error)}`)))
    }
    void this.release(operationHandle).catch((error) => this.onClosed(new Error(`cancelled operation release failed: ${errorMessage(error)}`)))
  }
}

export interface EndpointInput {
	endpointId: string
	routeId?: string
}

/** ProtoBindingConnector builds only OpenSessionRequest; route/auth/reconnect ownership remains in Go. */
export class ProtoBindingConnector {
  constructor(private readonly client: () => ProtoBindingClient, private readonly input: EndpointInput) {}

  async connect(input: { machineId: string }, options?: { signal?: AbortSignal; forceRelay?: boolean; onStatus?: (status: string) => void; onConnectionState?: (snapshot: { machineId: string; phase: 'connecting' | 'connected' | 'failed'; statusText: string; relayInUse: boolean }) => void }): Promise<ProtoClientSession> {
	const endpointId = this.input.endpointId.trim()
    if (!endpointId || input.machineId !== endpointId) throw new Error('endpoint identity mismatch')
    options?.onStatus?.('Connecting...')
    options?.onConnectionState?.({ machineId: input.machineId, phase: 'connecting', statusText: 'Connecting...', relayInUse: options.forceRelay === true })
    try {
	  await this.applyManagedRelayPolicy(options?.forceRelay, options?.signal)
      const session = await this.client().openSession(create(MuxviaClientBinding.OpenSessionRequestSchema, {
		requestId: crypto.randomUUID(), endpointId, routeOverride: this.input.routeId ?? '',
		intent: MuxviaClientBinding.ConnectIntent.INTERACTIVE,
      }), options?.signal)
      options?.onStatus?.('Connected')
      options?.onConnectionState?.({ machineId: input.machineId, phase: 'connected', statusText: 'Connected', relayInUse: options?.forceRelay === true })
      return session
    } catch (error) {
      options?.onConnectionState?.({ machineId: input.machineId, phase: 'failed', statusText: error instanceof Error ? error.message : 'Connection failed', relayInUse: options?.forceRelay === true })
      throw error
    }
  }

  private async applyManagedRelayPolicy(forceRelay: boolean | undefined, signal?: AbortSignal): Promise<void> {
	if (forceRelay === undefined) return
	const client = this.client()
	const registry = await client.getEndpointRegistry(signal)
	const endpoint = registry.endpoints.find((candidate) => candidate.endpointId === this.input.endpointId)
	if (!endpoint) throw new Error(`endpoint ${this.input.endpointId} is unavailable`)
	const relayMode = forceRelay
	  ? MuxviaRemoteAuth.ManagedWebRTCRelayMode.MANAGED_WEBRTC_RELAY_MODE_RELAY_ONLY
	  : MuxviaRemoteAuth.ManagedWebRTCRelayMode.MANAGED_WEBRTC_RELAY_MODE_AUTO
	let changed = false
	const routes = endpoint.routes.map((route) => {
	  const selected = !this.input.routeId || route.routeId === this.input.routeId
	  if (!selected || route.route.case !== 'managedWebrtc' || route.route.value.relayMode === relayMode) return route
	  changed = true
	  return create(MuxviaRemoteAuth.EndpointRouteConfigV1Schema, {
		...route,
		route: {
		  case: 'managedWebrtc',
		  value: create(MuxviaRemoteAuth.ManagedWebRTCRouteConfigSchema, { ...route.route.value, relayMode }),
		},
	  })
	})
	if (!changed) return
	await client.upsertEndpoint(create(MuxviaRemoteAuth.EndpointConfigV1Schema, { ...endpoint, routes }), false, signal)
  }
}

class ProtoBindingSession implements ProtoClientSession {
  private alive = true
  private readonly eventHandlers = new Set<(event: MuxviaApiApplication.EventEnvelope) => void>()

  constructor(private readonly client: ProtoBindingClient, readonly handle: bigint, readonly stamp: NonNullable<MuxviaClientBinding.OpenSessionResult['session']>) {}

  execute(command: MuxviaApiApplication.CommandEnvelope, options?: { signal?: AbortSignal }): Promise<MuxviaApiApplication.ResultEnvelope> {
    if (!this.alive) return Promise.reject(new Error('Proto session is closed'))
    return this.client.execute(this.handle, command, options?.signal)
  }

  subscribeEvents(handler: (event: MuxviaApiApplication.EventEnvelope) => void): ProtoClientSubscription {
    this.eventHandlers.add(handler)
    return { close: () => this.eventHandlers.delete(handler) }
  }

  openResourceStream(resource: NonNullable<MuxviaClientBinding.OpenResourceStreamRequest['resource']>, options?: { initialUploadOffset?: bigint; signal?: AbortSignal }): Promise<ProtoResourceStream> {
    if (!this.alive) return Promise.reject(new Error('Proto session is closed'))
    return this.client.openResourceStream(this.handle, resource, options)
  }

  isAlive(): boolean { return this.alive }

  async close(): Promise<void> {
    if (!this.alive) return
    this.alive = false
    this.eventHandlers.clear()
    await this.client.closeSession(this.handle)
  }

  publish(event: MuxviaApiApplication.EventEnvelope | undefined): void {
    if (!event || !this.alive) return
    this.eventHandlers.forEach((handler) => handler(event))
  }

  markClosed(_error?: Error): void {
    this.alive = false
    this.eventHandlers.clear()
  }
}

class ProtoBindingResourceStream implements ProtoResourceStream {
  private closed = false
  private readonly handlers = new Set<(type: MuxviaClientBinding.ResourceStreamFrameType, payload: Uint8Array) => void>()
  private readonly closeHandlers = new Set<(error: Error) => void>()

  constructor(private readonly client: ProtoBindingClient, readonly handle: bigint) {}

  send(type: MuxviaClientBinding.ResourceStreamFrameType, payload: Uint8Array): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Proto resource stream is closed'))
    return this.client.sendResourceStreamFrame(this.handle, type, payload)
  }

  subscribe(handler: (type: MuxviaClientBinding.ResourceStreamFrameType, payload: Uint8Array) => void): ProtoClientSubscription {
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
    await this.client.closeResourceStream(this.handle)
  }

  publish(type: MuxviaClientBinding.ResourceStreamFrameType, payload: Uint8Array): void {
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

function apiError(error: { code?: MuxviaApiCommon.ApiErrorCode, message?: string, retryable?: boolean } | undefined, fallback: string): Error {
  const result = new Error(error?.message || fallback) as Error & { code?: string, retryable?: boolean }
  const code = error ? apiErrorCode(error.code) : ''
  if (code) result.code = code
  if (typeof error?.retryable === 'boolean') result.retryable = error.retryable
  return result
}

function apiErrorCode(code: MuxviaApiCommon.ApiErrorCode | undefined): string {
  switch (code) {
    case MuxviaApiCommon.ApiErrorCode.INVALID_REQUEST: return 'invalid_request'
    case MuxviaApiCommon.ApiErrorCode.UNSUPPORTED_VERSION: return 'unsupported_version'
    case MuxviaApiCommon.ApiErrorCode.UNSUPPORTED_CAPABILITY: return 'unsupported_capability'
    case MuxviaApiCommon.ApiErrorCode.UNAUTHORIZED: return 'unauthenticated'
    case MuxviaApiCommon.ApiErrorCode.FORBIDDEN: return 'forbidden'
    case MuxviaApiCommon.ApiErrorCode.NOT_FOUND: return 'not_found'
    case MuxviaApiCommon.ApiErrorCode.CONFLICT: return 'conflict'
    case MuxviaApiCommon.ApiErrorCode.STALE_SESSION: return 'stale_session'
    case MuxviaApiCommon.ApiErrorCode.CANCELLED: return 'cancelled'
    case MuxviaApiCommon.ApiErrorCode.UNAVAILABLE: return 'unavailable'
    case MuxviaApiCommon.ApiErrorCode.INTERNAL: return 'internal'
    default: return ''
  }
}

function bindingOperationHandle(envelope: MuxviaClientBinding.EventEnvelope): bigint | undefined {
  switch (envelope.event.case) {
    case 'openSession':
    case 'execute':
    case 'importPairing':
    case 'deleteCredential':
    case 'endpointRegistryGet':
    case 'endpointUpsert':
    case 'endpointDelete':
    case 'endpointShareReceive':
    case 'endpointShareCommit':
    case 'sshCredentialProvision':
      return envelope.event.value.operationHandle
    default:
      return undefined
  }
}

function abortError(signal: AbortSignal): Error {
  return signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function rememberHandle(handles: Set<bigint>, order: bigint[], handle: bigint): void {
  if (handles.has(handle)) return
  if (order.length >= MAX_ABANDONED_STREAM_HANDLES) {
    const oldest = order.shift()
    if (oldest !== undefined) handles.delete(oldest)
  }
  handles.add(handle)
  order.push(handle)
}

async function awaitAbortableHandle(promise: Promise<bigint>, signal: AbortSignal | undefined, onLate: (handle: bigint) => void): Promise<bigint> {
  if (!signal) return await promise
  if (signal.aborted) {
    void promise.then(onLate, () => undefined)
    throw abortError(signal)
  }
  return await new Promise<bigint>((resolve, reject) => {
    let settled = false
    const abort = () => {
      if (settled) return
      settled = true
      signal.removeEventListener('abort', abort)
      reject(abortError(signal))
    }
    signal.addEventListener('abort', abort, { once: true })
    void promise.then(
      (handle) => {
        if (settled) {
          onLate(handle)
          return
        }
        settled = true
        signal.removeEventListener('abort', abort)
        resolve(handle)
      },
      (error) => {
        if (settled) return
        settled = true
        signal.removeEventListener('abort', abort)
        reject(error)
      },
    )
  })
}
