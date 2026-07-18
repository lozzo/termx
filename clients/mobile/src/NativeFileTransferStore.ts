import { Capacitor } from '@capacitor/core'
import { create } from '@bufbuild/protobuf'
import {
  TermxApiApplication,
  TermxApiFile,
  TermxClientBinding,
  type ProtoClientSession,
  type ProtoResourceStream,
  decodeFileStreamErrorPayload,
  decodeFileTransferAckPayload,
  decodeFileTransferDataPayload,
  decodeFileTransferFinishPayload,
  decodeFileTransferResultPayload,
  encodeFileTransferAckPayload,
  encodeFileTransferDataPayload,
} from '@termx/ui'

export type TransferStatus = 'pending' | 'transferring' | 'paused' | 'completed' | 'failed' | 'cancelled' | 'missing'

export interface TransferInfo {
  id: string
  machineId?: string
  name: string
  direction: 'download' | 'upload'
  totalSize: number
  transferredSize: number
  status: TransferStatus
  startedAt: number
  updatedAt?: number
  bytesPerSecond?: number
  error?: string
  filePath?: string
  localUri?: string | undefined
  targetDir?: string | undefined
  savedPath?: string | undefined
  savedUri?: string | undefined
}

export interface FileTransferStoreSnapshot {
  transfers: TransferInfo[]
  hasActiveTransfers: boolean
}

type NativeTransferSessionResolver = (machineId: string) => Promise<ProtoClientSession | null | undefined>

type ActiveTransfer = {
  session: ProtoClientSession
  stream: ProtoResourceStream
  resource: NonNullable<TermxApiFile.FileTransferHandle['resource']>
  cancel: AbortController
}

/** NativeFileTransferStore consumes apipb and Go-owned resource streams; it owns only UI progress and local Blob/URI access. */
export class NativeFileTransferStore {
  private transfers: TransferInfo[] = []
  private readonly listeners = new Set<() => void>()
  private readonly active = new Map<string, ActiveTransfer>()
  private resolver: NativeTransferSessionResolver | null = null
  private version = 0
  private cachedVersion = -1
  private cachedSnapshot: FileTransferStoreSnapshot | null = null

  setSessionResolver(resolver: NativeTransferSessionResolver | null): void { this.resolver = resolver }

  startDownload(machineId: string, fileName: string, fileSize: number, filePath: string, _offset = 0): void {
    const id = transferID('download', machineId, filePath)
    this.upsert({ id, machineId, name: fileName, direction: 'download', totalSize: fileSize, transferredSize: 0, status: 'pending', startedAt: Date.now(), updatedAt: Date.now(), filePath })
    void this.runDownload(id).catch((error) => this.fail(id, error))
  }

  async getDownloadResumeOffset(_machineId?: string, _filePath?: string, _fileSize?: number): Promise<number> { return 0 }

  startUpload(machineId: string, contentUri: string, fileName: string, fileSize: number, targetDir: string): void {
    const id = transferID('upload', machineId, `${targetDir}/${fileName}`)
    this.upsert({ id, machineId, name: fileName, direction: 'upload', totalSize: fileSize, transferredSize: 0, status: 'pending', startedAt: Date.now(), updatedAt: Date.now(), localUri: contentUri, targetDir })
    void this.runUpload(id).catch((error) => this.fail(id, error))
  }

  cancelTransfer(id: string): void {
    const task = this.active.get(id)
    task?.cancel.abort()
    if (task) void this.cancelRemote(task).finally(() => this.closeTask(id, task))
    this.update(id, { status: 'cancelled', updatedAt: Date.now(), bytesPerSecond: 0 })
  }

  pauseTransfer(id: string): void {
    const task = this.active.get(id)
    task?.cancel.abort()
    if (task) void this.cancelRemote(task).finally(() => this.closeTask(id, task))
    this.update(id, { status: 'paused', updatedAt: Date.now(), bytesPerSecond: 0 })
  }

  async resumeTransfer(id: string): Promise<void> {
    const transfer = this.transfers.find((item) => item.id === id)
    if (!transfer || !canResume(transfer.status)) return
    this.update(id, { status: 'pending', error: undefined, transferredSize: 0, updatedAt: Date.now() })
    try {
      if (transfer.direction === 'download') await this.runDownload(id)
      else await this.runUpload(id)
    } catch (error) {
      this.fail(id, error)
    }
  }

  async resumeAllTransfers(machineId?: string): Promise<void> {
    for (const transfer of [...this.transfers]) {
      if ((!machineId || transfer.machineId === machineId) && canResume(transfer.status)) await this.resumeTransfer(transfer.id)
    }
  }

  dismissTransfer(id: string): void {
    this.cancelTransfer(id)
    this.transfers = this.transfers.filter((item) => item.id !== id)
    this.notify()
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot(machineId?: string): FileTransferStoreSnapshot {
    if (!machineId && this.cachedSnapshot && this.cachedVersion === this.version) return this.cachedSnapshot
    const transfers = machineId ? this.transfers.filter((item) => item.machineId === machineId) : this.transfers
    const snapshot = { transfers, hasActiveTransfers: transfers.some((item) => item.status === 'pending' || item.status === 'transferring') }
    if (!machineId) {
      this.cachedSnapshot = snapshot
      this.cachedVersion = this.version
    }
    return snapshot
  }

  private async runDownload(id: string): Promise<void> {
    const transfer = this.requiredTransfer(id, 'download')
    const session = await this.session(transfer.machineId)
    const opened = await session.execute(command('fileDownloadOpen', create(TermxApiFile.FileDownloadOpenCommandSchema, { path: transfer.filePath ?? '' })))
    if (opened.result.case !== 'fileTransferOpen' || !opened.result.value.transfer) throw new Error('download returned no transfer resource')
    const remote = opened.result.value.transfer
    const resource = remote.resource
    if (!resource) throw new Error('download returned no resource handle')
    const stream = await session.openResourceStream(resource)
    const task = { session, stream, resource, cancel: new AbortController() }
    this.active.set(id, task)
    this.update(id, { status: 'transferring', totalSize: Number(remote.size), transferredSize: Number(remote.offset), updatedAt: Date.now() })
    try {
      const blob = await receiveDownload(stream, remote, task.cancel.signal, (received) => this.progress(id, received))
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = transfer.name
      anchor.click()
      setTimeout(() => URL.revokeObjectURL(url), 60_000)
      this.update(id, { status: 'completed', transferredSize: Number(remote.size), savedUri: url, updatedAt: Date.now(), bytesPerSecond: 0 })
    } finally {
      await this.closeTask(id, task)
    }
  }

  private async runUpload(id: string): Promise<void> {
    const transfer = this.requiredTransfer(id, 'upload')
    if (!transfer.localUri || !transfer.targetDir) throw new Error('upload local URI is missing')
    const session = await this.session(transfer.machineId)
    const target = `${transfer.targetDir.replace(/\/$/, '')}/${transfer.name}`
    const opened = await session.execute(command('fileUploadOpen', create(TermxApiFile.FileUploadOpenCommandSchema, {
      path: target, size: BigInt(transfer.totalSize), overwrite: true,
    })))
    if (opened.result.case !== 'fileTransferOpen' || !opened.result.value.transfer) throw new Error('upload returned no transfer resource')
    const remote = opened.result.value.transfer
    const resource = remote.resource
    if (!resource) throw new Error('upload returned no resource handle')
    const stream = await session.openResourceStream(resource)
    const task = { session, stream, resource, cancel: new AbortController() }
    this.active.set(id, task)
    this.update(id, { status: 'transferring', updatedAt: Date.now() })
    try {
      const response = await fetch(Capacitor.convertFileSrc(transfer.localUri), { signal: task.cancel.signal })
      if (!response.ok) throw new Error(`local upload file could not be read (${response.status})`)
      const blob = await response.blob()
      await sendUpload(stream, remote, blob, task.cancel.signal, (sent) => this.progress(id, sent))
      this.update(id, { status: 'completed', transferredSize: transfer.totalSize, savedPath: target, updatedAt: Date.now(), bytesPerSecond: 0 })
    } finally {
      await this.closeTask(id, task)
    }
  }

  private async cancelRemote(task: ActiveTransfer): Promise<void> {
    await task.session.execute(command('fileTransferCancel', create(TermxApiFile.FileTransferCancelCommandSchema, { transfer: task.resource }))).catch(() => undefined)
  }

  private async closeTask(id: string, task: ActiveTransfer): Promise<void> {
    if (this.active.get(id) === task) this.active.delete(id)
    await task.stream.close().catch(() => undefined)
    await task.session.close().catch(() => undefined)
  }

  private async session(machineId?: string): Promise<ProtoClientSession> {
    if (!machineId || !this.resolver) throw new Error('Connect this machine before starting a transfer')
    const session = await this.resolver(machineId)
    if (!session?.isAlive()) throw new Error('Go client session is unavailable')
    return session
  }

  private requiredTransfer(id: string, direction: TransferInfo['direction']): TransferInfo {
    const transfer = this.transfers.find((item) => item.id === id)
    if (!transfer || transfer.direction !== direction) throw new Error('transfer is unavailable')
    return transfer
  }

  private progress(id: string, transferredSize: number): void {
    const current = this.transfers.find((item) => item.id === id)
    const now = Date.now()
    const elapsed = Math.max(1, now - (current?.updatedAt ?? now))
    const speed = Math.max(0, transferredSize - (current?.transferredSize ?? 0)) * 1000 / elapsed
    this.update(id, { transferredSize, bytesPerSecond: speed, updatedAt: now })
  }

  private fail(id: string, error: unknown): void {
    if (error instanceof DOMException && error.name === 'AbortError') return
    this.update(id, { status: 'failed', error: error instanceof Error ? error.message : String(error), updatedAt: Date.now(), bytesPerSecond: 0 })
  }

  private upsert(info: TransferInfo): void {
    const index = this.transfers.findIndex((item) => item.id === info.id)
    this.transfers = index < 0 ? [...this.transfers, info] : this.transfers.map((item) => item.id === info.id ? info : item)
    this.notify()
  }

  private update(id: string, patch: Partial<TransferInfo>): void {
    this.transfers = this.transfers.map((item) => item.id === id ? { ...item, ...patch } : item)
    this.notify()
  }

  private notify(): void {
    this.version += 1
    for (const listener of this.listeners) listener()
  }
}

async function receiveDownload(
  stream: ProtoResourceStream,
  transfer: TermxApiFile.FileTransferHandle,
  signal: AbortSignal,
  progress: (bytes: number) => void,
): Promise<Blob> {
  const chunks: Uint8Array[] = []
  let offset = Number(transfer.offset)
  let credit = 0
  return await new Promise<Blob>((resolve, reject) => {
    let closeSubscription: { close(): void } | null = null
    const cleanup = () => {
      subscription.close()
      closeSubscription?.close()
      signal.removeEventListener('abort', abort)
    }
    const abort = () => {
      cleanup()
      reject(new DOMException('Aborted', 'AbortError'))
    }
    signal.addEventListener('abort', abort, { once: true })
    const subscription = stream.subscribe((type, payload) => {
      try {
        if (type === TermxClientBinding.ResourceStreamFrameType.FILE_DATA) {
          const data = decodeFileTransferDataPayload(payload)
          if (data.offset !== offset || data.data.byteLength === 0 || data.data.byteLength > transfer.chunkBytes) throw new Error('download chunk is invalid')
          chunks.push(data.data)
          offset += data.data.byteLength
          credit += data.data.byteLength
          progress(offset)
          if (credit >= Number(transfer.windowBytes)) {
            const windowBytes = credit
            credit = 0
            void stream.send(TermxClientBinding.ResourceStreamFrameType.FILE_ACK, encodeFileTransferAckPayload({ offset, windowBytes })).catch(reject)
          }
        } else if (type === TermxClientBinding.ResourceStreamFrameType.FILE_FINISH) {
          const finish = decodeFileTransferFinishPayload(payload)
          if (finish.size !== Number(transfer.size) || offset !== finish.size) throw new Error('download completed with the wrong size')
          cleanup()
          const blob = new Blob(chunks.map((chunk) => chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.byteLength) as ArrayBuffer))
          void verifyBlobDigest(blob, finish.sha256).then(() => resolve(blob), reject)
        } else if (type === TermxClientBinding.ResourceStreamFrameType.ERROR) {
          throw new Error(decodeFileStreamErrorPayload(payload))
        }
      } catch (error) {
        cleanup()
        reject(error)
      }
    })
    closeSubscription = stream.subscribeClosed((error) => {
      cleanup()
      reject(error)
    })
  })
}

async function sendUpload(
  stream: ProtoResourceStream,
  transfer: TermxApiFile.FileTransferHandle,
  blob: Blob,
  signal: AbortSignal,
  progress: (bytes: number) => void,
): Promise<void> {
  let offset = Number(transfer.offset)
  let credit = Number(transfer.windowBytes)
  let wake: (() => void) | null = null
  let resultResolve: (() => void) | null = null
  let resultReject: ((error: unknown) => void) | null = null
  const result = new Promise<void>((resolve, reject) => { resultResolve = resolve; resultReject = reject })
  const subscription = stream.subscribe((type, payload) => {
    if (type === TermxClientBinding.ResourceStreamFrameType.FILE_ACK) {
      const ack = decodeFileTransferAckPayload(payload)
      if (ack.offset !== offset) { resultReject?.(new Error('upload ACK offset mismatch')); return }
      credit += ack.windowBytes
      wake?.()
      wake = null
    } else if (type === TermxClientBinding.ResourceStreamFrameType.FILE_RESULT) {
      const completed = decodeFileTransferResultPayload(payload)
      if (completed.size !== blob.size) {
        resultReject?.(new Error('upload completed with the wrong size'))
        return
      }
      resultResolve?.()
    } else if (type === TermxClientBinding.ResourceStreamFrameType.ERROR) {
      resultReject?.(new Error(decodeFileStreamErrorPayload(payload)))
    }
  })
  const closeSubscription = stream.subscribeClosed((error) => {
    wake?.()
    wake = null
    resultReject?.(error)
  })
  const abort = () => {
    wake?.()
    wake = null
    resultReject?.(new DOMException('Aborted', 'AbortError'))
  }
  signal.addEventListener('abort', abort, { once: true })
  try {
    while (offset < blob.size) {
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      if (credit <= 0) await new Promise<void>((resolve) => { wake = resolve })
      const length = Math.min(transfer.chunkBytes, credit, blob.size - offset)
      const data = new Uint8Array(await blob.slice(offset, offset + length).arrayBuffer())
      const chunkOffset = offset
      offset += data.byteLength
      credit -= data.byteLength
      await stream.send(TermxClientBinding.ResourceStreamFrameType.FILE_DATA, encodeFileTransferDataPayload({ offset: chunkOffset, data }))
      progress(offset)
    }
    await stream.send(TermxClientBinding.ResourceStreamFrameType.FILE_FINISH_AUTO, new Uint8Array())
    await result
  } finally {
    subscription.close()
    closeSubscription.close()
    signal.removeEventListener('abort', abort)
  }
}

async function verifyBlobDigest(blob: Blob, expected: Uint8Array): Promise<void> {
  if (expected.byteLength !== 32) throw new Error('download digest is invalid')
  const actual = new Uint8Array(await crypto.subtle.digest('SHA-256', await blob.arrayBuffer()))
  if (!bytesEqual(actual, expected)) throw new Error('download digest mismatch')
}

function bytesEqual(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false
  let mismatch = 0
  for (let index = 0; index < left.byteLength; index += 1) mismatch |= left[index]! ^ right[index]!
  return mismatch === 0
}

function command(caseName: string, value: object) {
  return create(TermxApiApplication.CommandEnvelopeSchema, { command: { case: caseName, value } } as never)
}

function canResume(status: TransferStatus): boolean { return status === 'paused' || status === 'failed' || status === 'missing' }

function transferID(direction: string, machineId: string, path: string): string {
  return `${direction}-${machineId}-${path}-${Date.now()}`
}
