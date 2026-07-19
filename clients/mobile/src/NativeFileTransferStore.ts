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
  encodeFileTransferFinishPayload,
} from '@termx/ui'
import NativeFilePicker from './plugins/nativeFilePicker'

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
  uploadResumeToken?: Uint8Array | undefined
  remoteModifiedAtUnixNano?: bigint | undefined
}

export interface FileTransferStoreSnapshot {
  transfers: TransferInfo[]
  hasActiveTransfers: boolean
}

type NativeTransferSessionResolver = (machineId: string, signal: AbortSignal) => Promise<ProtoClientSession | null | undefined>
type CleanupConfirmation = { confirmed: boolean, error?: Error }

type ActiveTransfer = {
  epoch: number
  machineId: string
  direction: TransferInfo['direction']
  cancel: AbortController
  session?: ProtoClientSession
  stream?: ProtoResourceStream
  resource?: NonNullable<TermxApiFile.FileTransferHandle['resource']>
  uploadResume?: NonNullable<TermxApiFile.FileTransferHandle['resume']>
  destructiveCancel: boolean
  readyForClose: Promise<void>
  markReadyForClose: () => void
}

/** NativeFileTransferStore consumes apipb and Go-owned resource streams; it owns only UI progress and local Blob/URI access. */
export class NativeFileTransferStore {
  private transfers: TransferInfo[] = []
  private readonly listeners = new Set<() => void>()
  private readonly active = new Map<string, ActiveTransfer>()
  private readonly downloadChunks = new Map<string, Uint8Array[]>()
  private readonly taskTeardowns = new WeakMap<ActiveTransfer, Promise<void>>()
  private readonly pendingTeardowns = new Map<string, Promise<void>>()
  private readonly failedCleanupOwners = new Map<string, ActiveTransfer>()
  private readonly detachedCleanupOwners = new Map<string, ActiveTransfer>()
  private readonly destructiveRetries = new Map<string, Promise<void>>()
  private readonly pendingDismissals = new Set<string>()
  private readonly resumeTransitions = new Map<string, Promise<void>>()
  private readonly transitionEpochs = new Map<string, number>()
  private resolver: NativeTransferSessionResolver | null = null
  private version = 0
  private cachedVersion = -1
  private cachedSnapshot: FileTransferStoreSnapshot | null = null
  private readonly cachedMachineSnapshots = new Map<string, FileTransferStoreSnapshot>()

  setSessionResolver(resolver: NativeTransferSessionResolver | null): void { this.resolver = resolver }

  startDownload(machineId: string, fileName: string, fileSize: number, filePath: string, _offset = 0): void {
    const id = transferID('download', machineId, filePath)
    this.advanceTransition(id)
    this.upsert({ id, machineId, name: fileName, direction: 'download', totalSize: fileSize, transferredSize: 0, status: 'pending', startedAt: Date.now(), updatedAt: Date.now(), filePath })
    void this.runDownload(id).catch((error) => this.fail(id, error))
  }

  async getDownloadResumeOffset(machineId?: string, filePath?: string, fileSize?: number): Promise<number> {
    const transfer = this.transfers.find((item) => item.direction === 'download' && item.machineId === machineId && item.filePath === filePath && item.totalSize === fileSize)
    return transfer && this.downloadChunks.has(transfer.id) ? transfer.transferredSize : 0
  }

  startUpload(machineId: string, contentUri: string, fileName: string, fileSize: number, targetDir: string): void {
    const id = transferID('upload', machineId, `${targetDir}/${fileName}`)
    this.advanceTransition(id)
    this.upsert({ id, machineId, name: fileName, direction: 'upload', totalSize: fileSize, transferredSize: 0, status: 'pending', startedAt: Date.now(), updatedAt: Date.now(), localUri: contentUri, targetDir })
    void this.runUpload(id).catch((error) => this.fail(id, error))
  }

  cancelTransfer(id: string): void {
    void this.requestCancel(id)?.catch(() => undefined)
  }

  private requestCancel(id: string): Promise<void> | null {
    this.advanceTransition(id)
    const task = this.active.get(id) ?? this.failedCleanupOwners.get(id) ?? this.detachedCleanupOwners.get(id)
    task?.cancel.abort()
    let cleanup: Promise<void> | null = null
    if (task) {
      task.destructiveCancel = true
      cleanup = this.active.get(id) === task ? this.closeTask(id, task) : this.retryDestructiveCleanup(id, task)
    } else {
      this.pendingTeardowns.delete(id)
    }
    this.downloadChunks.delete(id)
    if (cleanup) {
      void cleanup.then(
        () => this.update(id, { status: 'cancelled', error: undefined, updatedAt: Date.now(), bytesPerSecond: 0 }),
        (error) => this.fail(id, error),
      )
    } else {
      this.update(id, { status: 'cancelled', updatedAt: Date.now(), bytesPerSecond: 0 })
    }
    return cleanup
  }

  pauseTransfer(id: string): void {
    this.advanceTransition(id)
    const task = this.active.get(id)
    task?.cancel.abort()
    if (task) void this.closeTask(id, task).catch((error) => this.fail(id, error))
    this.update(id, { status: 'paused', updatedAt: Date.now(), bytesPerSecond: 0 })
  }

  async resumeTransfer(id: string): Promise<void> {
    const transfer = this.transfers.find((item) => item.id === id)
    if (!transfer || !canResume(transfer.status)) return
    const requestEpoch = this.transitionEpochs.get(id) ?? 0
    const previous = this.resumeTransitions.get(id) ?? Promise.resolve()
    let transition!: Promise<void>
    transition = previous.catch(() => undefined).then(() => this.runResumeTransition(id, requestEpoch))
    this.resumeTransitions.set(id, transition)
    try {
      await transition
    } finally {
      if (this.resumeTransitions.get(id) === transition) this.resumeTransitions.delete(id)
    }
  }

  private async runResumeTransition(id: string, requestEpoch: number): Promise<void> {
    try {
      let transfer = this.transfers.find((item) => item.id === id)
      if (!transfer || !canResume(transfer.status) || (this.transitionEpochs.get(id) ?? 0) !== requestEpoch) return
      const teardown = this.pendingTeardowns.get(id)
      if (teardown) await teardown
      transfer = this.transfers.find((item) => item.id === id)
      if (!transfer || !canResume(transfer.status) || (this.transitionEpochs.get(id) ?? 0) !== requestEpoch) return
      this.advanceTransition(id)
      this.update(id, { status: 'pending', error: undefined, updatedAt: Date.now() })
      if (transfer.direction === 'download') await this.runDownload(id)
      else await this.runUpload(id)
    } catch (error) {
      if (this.transfers.find((item) => item.id === id)?.status === 'cancelled') return
      this.fail(id, error)
    }
  }

  async resumeAllTransfers(machineId?: string): Promise<void> {
    for (const transfer of [...this.transfers]) {
      if ((!machineId || transfer.machineId === machineId) && canResume(transfer.status)) await this.resumeTransfer(transfer.id)
    }
  }

  dismissTransfer(id: string): void {
    this.pendingDismissals.add(id)
    const cleanup = this.requestCancel(id)
    if (!cleanup) {
      this.completeDismiss(id)
      return
    }
    void cleanup.then(() => this.completeDismiss(id), () => undefined)
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot(machineId?: string): FileTransferStoreSnapshot {
    if (!machineId && this.cachedSnapshot && this.cachedVersion === this.version) return this.cachedSnapshot
    if (machineId) {
      const cached = this.cachedMachineSnapshots.get(machineId)
      if (cached) return cached
    }
    const transfers = machineId ? this.transfers.filter((item) => item.machineId === machineId) : this.transfers
    const snapshot = { transfers, hasActiveTransfers: transfers.some((item) => item.status === 'pending' || item.status === 'transferring') }
    if (machineId) {
      this.cachedMachineSnapshots.set(machineId, snapshot)
    } else {
      this.cachedSnapshot = snapshot
      this.cachedVersion = this.version
    }
    return snapshot
  }

  private async runDownload(id: string): Promise<void> {
    const transfer = this.requiredTransfer(id, 'download')
    const task = this.beginAttempt(id)
    try {
      const session = await this.session(transfer.machineId, task.cancel.signal)
      task.session = session
      this.assertCurrentAttempt(id, task)
      const chunks = this.downloadChunks.get(id) ?? []
      const bufferedBytes = chunks.reduce((total, chunk) => total + chunk.byteLength, 0)
      const resumeOffset = bufferedBytes === transfer.transferredSize ? bufferedBytes : 0
      if (resumeOffset === 0) this.downloadChunks.set(id, [])
      const opened = await session.execute(command('fileDownloadOpen', create(TermxApiFile.FileDownloadOpenCommandSchema, {
        path: transfer.filePath ?? '', offset: BigInt(resumeOffset),
        expectedSize: transfer.remoteModifiedAtUnixNano ? BigInt(transfer.totalSize) : 0n,
        expectedModifiedAtUnixNano: transfer.remoteModifiedAtUnixNano ?? 0n,
      })), { signal: task.cancel.signal })
      if (opened.result.case !== 'fileTransferOpen' || !opened.result.value.transfer) throw new Error('download returned no transfer resource')
      const remote = opened.result.value.transfer
      if (Number(remote.offset) !== resumeOffset) {
        if (Number(remote.offset) !== 0) throw new Error('download resume offset was not accepted')
        this.downloadChunks.set(id, [])
      }
      const resource = remote.resource
      if (!resource) throw new Error('download returned no resource handle')
      task.resource = resource
      this.detachedCleanupOwners.delete(id)
      this.assertCurrentAttempt(id, task)
      const stream = await awaitAbortable(
        session.openResourceStream(resource, { signal: task.cancel.signal }),
        task.cancel.signal,
        (late) => { void late.close().catch(() => undefined) },
      )
      task.stream = stream
      this.assertCurrentAttempt(id, task)
      this.update(id, {
        status: 'transferring', totalSize: Number(remote.size), transferredSize: Number(remote.offset),
        remoteModifiedAtUnixNano: remote.modifiedAtUnixNano, updatedAt: Date.now(),
      })
      const retained = this.downloadChunks.get(id) ?? []
      const blob = await receiveDownload(stream, remote, retained, task.cancel.signal, (received) => this.progress(id, received))
      this.assertCurrentAttempt(id, task)
      if (Capacitor.isNativePlatform()) {
        const saved = await NativeFilePicker.saveFile({
          name: transfer.name,
          mimeType: blob.type || 'application/octet-stream',
          dataBase64: await blobBase64(blob),
        })
        if (saved.bytes !== blob.size) throw new Error('Android download persistence size mismatch')
        this.update(id, {
          status: 'completed', transferredSize: Number(remote.size), savedUri: saved.uri, savedPath: saved.path,
          updatedAt: Date.now(), bytesPerSecond: 0,
        })
      } else {
        const url = URL.createObjectURL(blob)
        const anchor = document.createElement('a')
        anchor.href = url
        anchor.download = transfer.name
        anchor.click()
        setTimeout(() => URL.revokeObjectURL(url), 60_000)
        this.update(id, { status: 'completed', transferredSize: Number(remote.size), savedUri: url, updatedAt: Date.now(), bytesPerSecond: 0 })
      }
      this.downloadChunks.delete(id)
    } finally {
      task.markReadyForClose()
      await this.closeTask(id, task)
    }
  }

  private async runUpload(id: string): Promise<void> {
    const transfer = this.requiredTransfer(id, 'upload')
    if (!transfer.localUri || !transfer.targetDir) throw new Error('upload local URI is missing')
    const task = this.beginAttempt(id)
    try {
      const session = await this.session(transfer.machineId, task.cancel.signal)
      task.session = session
      this.assertCurrentAttempt(id, task)
      const target = `${transfer.targetDir.replace(/\/$/, '')}/${transfer.name}`
      const opened = await session.execute(command('fileUploadOpen', create(TermxApiFile.FileUploadOpenCommandSchema, {
        path: target, size: BigInt(transfer.totalSize), overwrite: true,
        resume: transfer.uploadResumeToken ? create(TermxApiFile.FileUploadResumeHandleSchema, { opaqueToken: transfer.uploadResumeToken }) : undefined,
      })), { signal: task.cancel.signal })
      if (opened.result.case !== 'fileTransferOpen' || !opened.result.value.transfer) throw new Error('upload returned no transfer resource')
      const remote = opened.result.value.transfer
      const resource = remote.resource
      if (!resource) throw new Error('upload returned no resource handle')
      task.resource = resource
      task.uploadResume = remote.resume
      this.detachedCleanupOwners.delete(id)
      this.assertCurrentAttempt(id, task)
      const stream = await awaitAbortable(
        session.openResourceStream(resource, { initialUploadOffset: remote.offset, signal: task.cancel.signal }),
        task.cancel.signal,
        (late) => { void late.close().catch(() => undefined) },
      )
      task.stream = stream
      this.assertCurrentAttempt(id, task)
      this.update(id, {
        status: 'transferring', transferredSize: Number(remote.offset),
        uploadResumeToken: remote.resume?.opaqueToken.slice(), updatedAt: Date.now(),
      })
      const response = await fetch(Capacitor.convertFileSrc(transfer.localUri), { signal: task.cancel.signal })
      this.assertCurrentAttempt(id, task)
      if (!response.ok) throw new Error(`local upload file could not be read (${response.status})`)
      const blob = await response.blob()
      this.assertCurrentAttempt(id, task)
      await sendUpload(stream, remote, blob, task.cancel.signal, (sent) => this.progress(id, sent))
      this.assertCurrentAttempt(id, task)
      this.update(id, { status: 'completed', transferredSize: transfer.totalSize, savedPath: target, updatedAt: Date.now(), bytesPerSecond: 0 })
    } finally {
      task.markReadyForClose()
      await this.closeTask(id, task)
    }
  }

  private async cancelRemote(task: ActiveTransfer): Promise<CleanupConfirmation> {
    if (!task.session || (!task.resource && !task.uploadResume)) {
      return { confirmed: false, error: new Error('remote file transfer cancellation has no credential') }
    }
    const useSessionResource = task.resource && sameSession(task.resource.session, task.session.stamp)
    let cancelError: Error | undefined
    try {
      const result = await task.session.execute(command('fileTransferCancel', create(TermxApiFile.FileTransferCancelCommandSchema, {
        transfer: useSessionResource ? task.resource : undefined,
        uploadResume: useSessionResource ? undefined : task.uploadResume,
      })))
      if (result.result.case === 'fileTransferCancel') {
        // Download 的 false 表示 daemon 已处理命令且 resource 已不存在；upload 临时文件仍要求明确 cancelled=true。
        if (result.result.value.cancelled || task.direction === 'download') return { confirmed: true }
        return { confirmed: false, error: new Error('daemon did not confirm upload cancellation') }
      }
      cancelError = new Error('daemon returned no file transfer cancellation result')
    } catch (error) {
      cancelError = error instanceof Error ? error : new Error(String(error))
    }
    const released = await this.releaseCancelledDownload(task, useSessionResource)
    if (released.confirmed) return released
    return {
      confirmed: false,
      error: new Error(`file transfer cancel failed: ${cancelError.message}; release failed: ${released.error?.message ?? 'unavailable'}`),
    }
  }

  private async releaseCancelledDownload(task: ActiveTransfer, useSessionResource: boolean | undefined): Promise<CleanupConfirmation> {
    if (task.direction !== 'download' || !useSessionResource || !task.session || !task.resource) {
      return { confirmed: false, error: new Error('download resource is not bound to the current session') }
    }
    try {
      await this.releaseRemote(task.session, task.resource)
      return { confirmed: true }
    } catch (error) {
      return { confirmed: false, error: error instanceof Error ? error : new Error(String(error)) }
    }
  }

  private async releaseRemote(session: ProtoClientSession, resource: NonNullable<ActiveTransfer['resource']>): Promise<void> {
    await session.execute(command('releaseResource', create(TermxApiApplication.ReleaseResourceCommandSchema, { resource })))
  }

  private closeTask(id: string, task: ActiveTransfer): Promise<void> {
    const existing = this.taskTeardowns.get(task)
    if (existing) return existing
    const teardown = this.finishCloseTask(id, task)
    this.taskTeardowns.set(task, teardown)
    this.pendingTeardowns.set(id, teardown)
    void teardown.then(
      () => this.clearPendingTeardown(id, teardown),
      () => undefined,
    )
    return teardown
  }

  private async finishCloseTask(id: string, task: ActiveTransfer): Promise<void> {
    await task.readyForClose
    let cleanupError: unknown
    let destructiveConfirmed = false
    let destructiveAttempted = false
    if (!task.destructiveCancel && task.session && task.resource) {
      try {
        await this.releaseRemote(task.session, task.resource)
      } catch (error) {
        cleanupError = error
      }
    }
    if (task.destructiveCancel && task.session && task.resource) {
      destructiveAttempted = true
      const cancellation = await this.cancelRemote(task)
      destructiveConfirmed = cancellation.confirmed
      cleanupError = cancellation.confirmed ? undefined : cancellation.error
    }
    await task.stream?.close().catch(() => undefined)
    if (task.destructiveCancel && !destructiveAttempted && task.session && task.resource) {
      const cancellation = await this.cancelRemote(task)
      destructiveConfirmed = cancellation.confirmed
      cleanupError = cancellation.confirmed ? undefined : cancellation.error
    }
    if (task.destructiveCancel && !task.resource && !task.uploadResume) {
      // FileUploadOpen/FileDownloadOpen 尚未交付 resource 时，binding cancelled-execute owner 负责销毁迟到结果。
      // Store 不能要求一个尚未产生的 resume credential，也不能建立第二套 operation cleanup。
      destructiveConfirmed = true
      cleanupError = undefined
    }
    if (cleanupError) {
      if (this.active.get(id) === task) this.active.delete(id)
      this.failedCleanupOwners.set(id, task)
      throw cleanupError
    }
    this.failedCleanupOwners.delete(id)
    await task.session?.close().catch(() => undefined)
    task.session = undefined
    task.stream = undefined
    if (task.destructiveCancel && !destructiveConfirmed) {
      try {
        const cancellation = await this.cancelWithFreshSession(task)
        destructiveConfirmed = cancellation.confirmed
        cleanupError = cancellation.error
      } catch (error) {
        if (this.active.get(id) === task) this.active.delete(id)
        this.failedCleanupOwners.set(id, task)
        throw error
      }
      if (!destructiveConfirmed) {
        if (this.active.get(id) === task) this.active.delete(id)
        this.failedCleanupOwners.set(id, task)
        throw cleanupError ?? new Error('remote file transfer cancellation was not confirmed')
      }
    }
    const transfer = this.transfers.find((item) => item.id === id)
    if (!task.destructiveCancel && task.direction === 'upload' && transfer?.status !== 'completed' && transfer?.status !== 'cancelled' && task.uploadResume) {
      this.detachedCleanupOwners.set(id, task)
    } else {
      this.detachedCleanupOwners.delete(id)
    }
    if (this.active.get(id) === task) this.active.delete(id)
  }

  private retryDestructiveCleanup(id: string, task: ActiveTransfer): Promise<void> {
    const existing = this.destructiveRetries.get(id)
    if (existing) return existing
    const retry = this.finishDestructiveCleanup(id, task)
    this.destructiveRetries.set(id, retry)
    this.pendingTeardowns.set(id, retry)
    void retry.then(
      () => {
        if (this.destructiveRetries.get(id) === retry) this.destructiveRetries.delete(id)
        this.clearPendingTeardown(id, retry)
      },
      () => {
        if (this.destructiveRetries.get(id) === retry) this.destructiveRetries.delete(id)
      },
    )
    return retry
  }

  private async finishDestructiveCleanup(id: string, task: ActiveTransfer): Promise<void> {
    const cancellation = await this.cancelWithFreshSession(task)
    if (!cancellation.confirmed) throw cancellation.error ?? new Error('remote file transfer cancellation was not confirmed')
    this.failedCleanupOwners.delete(id)
    this.detachedCleanupOwners.delete(id)
    await task.stream?.close().catch(() => undefined)
	await task.session?.close().catch(() => undefined)
  }

  private async cancelWithFreshSession(task: ActiveTransfer): Promise<CleanupConfirmation> {
	let priorError: Error | undefined
	if (task.session?.isAlive()) {
	  const cancellation = await this.cancelRemote(task)
	  if (cancellation.confirmed) return cancellation
	  priorError = cancellation.error
	  await task.session.close().catch(() => undefined)
	  task.session = undefined
	}
	if (!task.uploadResume) {
	  return { confirmed: false, error: priorError ?? new Error('fresh-session cancellation has no upload resume credential') }
	}
	const controller = new AbortController()
	try {
	  task.session = await this.session(task.machineId, controller.signal)
	  const cancellation = await this.cancelRemote(task)
	  if (cancellation.confirmed || !priorError) return cancellation
	  return { confirmed: false, error: new Error(`existing-session cleanup failed: ${priorError.message}; fresh-session cleanup failed: ${cancellation.error?.message ?? 'unavailable'}`) }
	} catch (error) {
	  const freshError = error instanceof Error ? error : new Error(String(error))
	  return { confirmed: false, error: new Error(`existing-session cleanup failed: ${priorError?.message ?? 'unavailable'}; fresh-session resolution failed: ${freshError.message}`) }
	}
  }

  private completeDismiss(id: string): void {
    if (!this.pendingDismissals.delete(id)) return
    this.transfers = this.transfers.filter((item) => item.id !== id)
    this.transitionEpochs.delete(id)
    this.resumeTransitions.delete(id)
    this.pendingTeardowns.delete(id)
    this.failedCleanupOwners.delete(id)
    this.detachedCleanupOwners.delete(id)
    this.notify()
  }

  private clearPendingTeardown(id: string, teardown: Promise<void>): void {
    if (this.pendingTeardowns.get(id) === teardown) this.pendingTeardowns.delete(id)
  }

  private beginAttempt(id: string): ActiveTransfer {
    if (this.active.has(id)) throw new Error('transfer attempt is already active')
    const transfer = this.transfers.find((item) => item.id === id)
    if (!transfer?.machineId) throw new Error('transfer machine is unavailable')
    let markReadyForClose!: () => void
    const readyForClose = new Promise<void>((resolve) => { markReadyForClose = resolve })
    const task: ActiveTransfer = {
      epoch: this.transitionEpochs.get(id) ?? 0,
      machineId: transfer.machineId,
      direction: transfer.direction,
      cancel: new AbortController(),
      destructiveCancel: false,
      readyForClose,
      markReadyForClose,
    }
    this.active.set(id, task)
    return task
  }

  private assertCurrentAttempt(id: string, task: ActiveTransfer): void {
    const transfer = this.transfers.find((item) => item.id === id)
    if (this.active.get(id) !== task || (this.transitionEpochs.get(id) ?? 0) !== task.epoch || task.cancel.signal.aborted || !transfer || transfer.status === 'paused' || transfer.status === 'cancelled') {
      throw new DOMException('Aborted', 'AbortError')
    }
  }

  private advanceTransition(id: string): number {
    const next = (this.transitionEpochs.get(id) ?? 0) + 1
    this.transitionEpochs.set(id, next)
    return next
  }

  private async session(machineId: string | undefined, signal: AbortSignal): Promise<ProtoClientSession> {
    if (!machineId || !this.resolver) throw new Error('Connect this machine before starting a transfer')
    const session = await awaitAbortable(
      this.resolver(machineId, signal),
      signal,
      (late) => { void late?.close().catch(() => undefined) },
    )
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
    if (this.transfers.find((item) => item.id === id)?.status === 'cancelled' && !this.failedCleanupOwners.has(id)) return
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
    this.cachedSnapshot = null
    this.cachedMachineSnapshots.clear()
    for (const listener of this.listeners) listener()
  }
}

async function blobBase64(blob: Blob): Promise<string> {
  const dataUrl = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('download file encoding failed'))
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.readAsDataURL(blob)
  })
  const separator = dataUrl.indexOf(',')
  if (separator < 0) throw new Error('download file encoding failed')
  return dataUrl.slice(separator + 1)
}

async function receiveDownload(
  stream: ProtoResourceStream,
  transfer: TermxApiFile.FileTransferHandle,
  chunks: Uint8Array[],
  signal: AbortSignal,
  progress: (bytes: number) => void,
): Promise<Blob> {
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
  let acknowledgedOffset = offset
  let credit = Number(transfer.windowBytes)
  let wake: (() => void) | null = null
  let terminalError: unknown
  let resultResolve: (() => void) | null = null
  let resultReject: ((error: unknown) => void) | null = null
  const result = new Promise<void>((resolve, reject) => { resultResolve = resolve; resultReject = reject })
  void result.catch(() => undefined)
  const fail = (error: unknown) => {
    if (terminalError) return
    terminalError = error
    wake?.()
    wake = null
    resultReject?.(error)
  }
  const subscription = stream.subscribe((type, payload) => {
    if (type === TermxClientBinding.ResourceStreamFrameType.FILE_ACK) {
      const ack = decodeFileTransferAckPayload(payload)
      const acknowledgedBytes = ack.offset - acknowledgedOffset
      if (ack.offset <= acknowledgedOffset || ack.offset > offset || ack.windowBytes !== acknowledgedBytes) {
        fail(new Error('upload ACK offset mismatch'))
        return
      }
      acknowledgedOffset = ack.offset
      credit += ack.windowBytes
      wake?.()
      wake = null
    } else if (type === TermxClientBinding.ResourceStreamFrameType.FILE_RESULT) {
      const completed = decodeFileTransferResultPayload(payload)
      if (completed.size !== blob.size) {
        fail(new Error('upload completed with the wrong size'))
        return
      }
      resultResolve?.()
    } else if (type === TermxClientBinding.ResourceStreamFrameType.ERROR) {
      fail(new Error(decodeFileStreamErrorPayload(payload)))
    }
  })
  const closeSubscription = stream.subscribeClosed(fail)
  const abort = () => fail(new DOMException('Aborted', 'AbortError'))
  signal.addEventListener('abort', abort, { once: true })
  try {
    while (offset < blob.size) {
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      if (terminalError) throw terminalError
      if (credit <= 0) await new Promise<void>((resolve) => { wake = resolve })
      if (terminalError) throw terminalError
      const length = Math.min(transfer.chunkBytes, credit, blob.size - offset)
      const data = new Uint8Array(await blob.slice(offset, offset + length).arrayBuffer())
      const chunkOffset = offset
      offset += data.byteLength
      credit -= data.byteLength
      await stream.send(TermxClientBinding.ResourceStreamFrameType.FILE_DATA, encodeFileTransferDataPayload({ offset: chunkOffset, data }))
      progress(offset)
    }
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', await blob.arrayBuffer()))
    await stream.send(TermxClientBinding.ResourceStreamFrameType.FILE_FINISH, encodeFileTransferFinishPayload({ size: blob.size, sha256: digest }))
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

async function awaitAbortable<T>(promise: Promise<T>, signal: AbortSignal, onLate: (value: T) => void): Promise<T> {
  if (signal.aborted) {
    void promise.then(onLate, () => undefined)
    throw abortError(signal)
  }
  return await new Promise<T>((resolve, reject) => {
    let settled = false
    const abort = () => {
      if (settled) return
      settled = true
      signal.removeEventListener('abort', abort)
      reject(abortError(signal))
    }
    signal.addEventListener('abort', abort, { once: true })
    void promise.then(
      (value) => {
        if (settled) {
          onLate(value)
          return
        }
        settled = true
        signal.removeEventListener('abort', abort)
        resolve(value)
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

function abortError(signal: AbortSignal): Error {
  return signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError')
}

function transferID(direction: string, machineId: string, path: string): string {
  return `${direction}-${machineId}-${path}-${Date.now()}`
}

function sameSession(left: { endpointId: string; routeId: string; generation: bigint } | undefined, right: ProtoClientSession['stamp']): boolean {
  return left?.endpointId === right.endpointId && left.routeId === right.routeId && left.generation === right.generation
}
