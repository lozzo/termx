/**
 * NativeFileTransferStore — 管理 Native 文件传输状态
 *
 * 与 NativeRtcSession 通过 FRAME_TRANSFER_SYNC / FRAME_TRANSFER_REQUEST 通信。
 * 实现 useSyncExternalStore 接口（subscribe/getSnapshot），供 React 消费。
 *
 * 下载流程：
 *   JS 调用 fileApi.downloadInit(path) → 获得 transfer_id, name, size
 *   → sendTransferRequest({ action: 'start_download', ... }) → Native 接管
 *   → Native 发送 FRAME_TRANSFER_SYNC 进度更新 → UI 显示进度
 *
 * 上传流程：
 *   NativeFilePicker.pickFiles() → 获得 content:// URI
 *   → sendTransferRequest({ action: 'start_upload', ... }) → Native 接管
 *   → Native 发送 FRAME_TRANSFER_SYNC 进度更新 → UI 显示进度
 */

import type { TransferSyncPayload } from './NativeConnectionProxy'
import { NativeConnection } from './plugins/nativeConnection'

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
  filePath?: string  // for download retry
  localUri?: string | undefined
  targetDir?: string | undefined
  savedPath?: string | undefined
  savedUri?: string | undefined
}

export interface FileTransferStoreSnapshot {
  transfers: TransferInfo[]
  hasActiveTransfers: boolean
}

export interface NativeTransferSession {
  onTransferSync(handler: (data: TransferSyncPayload) => void): () => void
  onSyncResponse(handler: (data: { transfers?: TransferSyncPayload['transfers'] | undefined }) => void): () => void
  sendTransferRequest(request: Record<string, unknown>): void
  sendSyncRequest(): void
  isAlive(): boolean
}

type NativeTransferSessionResolver = (machineId: string) => Promise<NativeTransferSession | null | undefined>

export class NativeFileTransferStore {
  private _transfers: TransferInfo[] = []
  private _listeners = new Set<() => void>()
  private _session: NativeTransferSession | null = null
  private _unsub: (() => void) | null = null
  private _snapshotVersion = 0
  private _cachedSnapshot?: FileTransferStoreSnapshot
  private _cachedVersion = -1
  private _cachedScopedSnapshots = new Map<string, {
    version: number
    snapshot: FileTransferStoreSnapshot
  }>()
  private _speedSamples = new Map<string, {
    bytesPerSecond: number
    timestamp: number
    transferredSize: number
  }>()
  private _sessionResolver: NativeTransferSessionResolver | null = null

  constructor() {
    void this.refreshFromNative()
  }

  // ─── Session binding ───────────────────────────────────────────────────────

  setSession(session: NativeTransferSession | null): void {
    if (this._unsub) {
      this._unsub()
      this._unsub = null
    }
    this._session = session

    if (session) {
      const unsubSync = session.onTransferSync((data) => {
        this._handleTransferSync(data)
      })
      const unsubResp = session.onSyncResponse((data) => {
        if (data.transfers) {
          this._handleTransferSync({ transfers: data.transfers as TransferSyncPayload['transfers'] })
        }
      })
      const onResume = () => {
        session.sendSyncRequest()
        void this.refreshFromNative()
      }
      document.addEventListener('termx:resume', onResume)
      this._unsub = () => {
        unsubSync()
        unsubResp()
        document.removeEventListener('termx:resume', onResume)
      }
      // Request current state immediately after binding
      session.sendSyncRequest()
    }
  }

  setSessionResolver(resolver: NativeTransferSessionResolver | null): void {
    this._sessionResolver = resolver
  }

  async refreshFromNative(): Promise<void> {
    try {
      const snapshot = await NativeConnection.getTransferSnapshot()
      this._handleTransferSync({ transfers: snapshot.transfers as TransferSyncPayload['transfers'] })
    } catch {
      // The native plugin is not available in browser tests/dev.
    }
  }

  // ─── Transfer operations ──────────────────────────────────────────────────

  startDownload(
    machineId: string,
    transferId: string,
    fileName: string,
    fileSize: number,
    filePath: string,
    offset = 0,
  ): void {
    const info: TransferInfo = {
      id: transferId,
      machineId,
      name: fileName,
      direction: 'download',
      totalSize: fileSize,
      transferredSize: offset,
      status: 'pending',
      startedAt: Date.now(),
      updatedAt: Date.now(),
      filePath,
    }
    this._addOrUpdate(info)
    void this._ensureSession(machineId).then((session) => {
      session.sendTransferRequest({
        action: 'start_download',
        transfer_id: transferId,
        file_name: fileName,
        file_size: fileSize,
        file_path: filePath,
        offset,
        machine_id: machineId,
      })
      void this.refreshFromNative()
    }).catch((err) => {
      this._update(transferId, {
        status: 'failed',
        error: err instanceof Error ? err.message : String(err),
      })
    })
  }

  async getDownloadResumeOffset(machineId: string, filePath: string, fileSize: number): Promise<number> {
    try {
      const result = await NativeConnection.getDownloadResumeOffset({ machineId, filePath, fileSize })
      const offset = Number(result.offset)
      if (!Number.isFinite(offset) || offset <= 0 || offset >= fileSize) return 0
      return Math.floor(offset)
    } catch {
      return 0
    }
  }

  startUpload(
    machineId: string,
    contentUri: string,
    fileName: string,
    fileSize: number,
    targetDir: string,
  ): void {
    const pendingId = `pending_upload_${Date.now()}`
    const info: TransferInfo = {
      id: pendingId,
      machineId,
      name: fileName,
      direction: 'upload',
      totalSize: fileSize,
      transferredSize: 0,
      status: 'pending',
      startedAt: Date.now(),
      updatedAt: Date.now(),
    }
    this._addOrUpdate(info)

    if (!this._session) {
      this._update(pendingId, {
        status: 'failed',
        error: 'Native transfer bridge is not connected',
      })
      return
    }

    try {
      this._session.sendTransferRequest({
        action: 'start_upload',
        content_uri: contentUri,
        file_name: fileName,
        file_size: fileSize,
        target_dir: targetDir,
        machine_id: machineId,
      })
    } catch (err) {
      this._update(pendingId, {
        status: 'failed',
        error: err instanceof Error ? err.message : String(err),
      })
    }
  }

  cancelTransfer(id: string): void {
    const t = this._transfers.find((x) => x.id === id)
    if (t?.direction === 'download') {
      this._session?.sendTransferRequest({
        action: 'cancel_download',
        transfer_id: id,
        ...(t.machineId ? { machine_id: t.machineId } : {}),
      })
    } else if (t?.direction === 'upload') {
      this._session?.sendTransferRequest({
        action: 'cancel_upload',
        transfer_id: id,
        ...(t.machineId ? { machine_id: t.machineId } : {}),
      })
    }
    this._update(id, { status: 'cancelled' })
  }

  pauseTransfer(id: string): void {
    const t = this._transfers.find((x) => x.id === id)
    if (!t || (t.status !== 'pending' && t.status !== 'transferring')) return
    if (t.direction === 'download') {
      this._session?.sendTransferRequest({
        action: 'pause_download',
        transfer_id: id,
        ...(t.machineId ? { machine_id: t.machineId } : {}),
      })
    } else {
      this._session?.sendTransferRequest({
        action: 'pause_upload',
        transfer_id: id,
        ...(t.machineId ? { machine_id: t.machineId } : {}),
      })
    }
    this._speedSamples.set(id, {
      bytesPerSecond: 0,
      timestamp: Date.now(),
      transferredSize: t.transferredSize,
    })
    this._update(id, { status: 'paused', bytesPerSecond: 0, updatedAt: Date.now() })
  }

  async resumeTransfer(id: string): Promise<void> {
    const t = this._transfers.find((x) => x.id === id)
    if (!t || !canResumeStatus(t.status)) return
    this._speedSamples.set(id, {
      bytesPerSecond: 0,
      timestamp: Date.now(),
      transferredSize: t.transferredSize,
    })
    this._update(id, { status: 'pending', error: undefined, bytesPerSecond: 0, updatedAt: Date.now() })
    try {
      const session = await this._ensureSession(t.machineId)
      if (t.direction === 'download') {
        session.sendTransferRequest({
          action: 'resume_download',
          transfer_id: id,
          ...(t.machineId ? { machine_id: t.machineId } : {}),
        })
      } else {
        session.sendTransferRequest({
          action: 'resume_upload',
          transfer_id: id,
          ...(t.machineId ? { machine_id: t.machineId } : {}),
        })
      }
      void this.refreshFromNative()
    } catch (err) {
      this._update(id, {
        status: 'paused',
        error: err instanceof Error ? err.message : String(err),
        bytesPerSecond: 0,
        updatedAt: Date.now(),
      })
    }
  }

  dismissTransfer(id: string): void {
    this._session?.sendTransferRequest({
      action: 'clear_transfer',
      transfer_id: id,
    })
    void NativeConnection.clearTransfer({ transferId: id }).catch(() => {})
    this._remove(id)
  }

  async resumeAllTransfers(machineId?: string): Promise<void> {
    for (const t of this._transfers) {
      if (machineId && t.machineId !== machineId) continue
      if (!canResumeStatus(t.status)) continue
      this._speedSamples.set(t.id, {
        bytesPerSecond: 0,
        timestamp: Date.now(),
        transferredSize: t.transferredSize,
      })
      this._update(t.id, { status: 'pending', error: undefined, bytesPerSecond: 0, updatedAt: Date.now() })
    }
    const machineIds = machineId ? [machineId] : uniqueMachineIds(this._transfers)
    try {
      for (const id of machineIds) {
        await this._ensureSession(id)
      }
      this._session?.sendTransferRequest({
        action: 'resume_all',
        ...(machineId ? { machine_id: machineId } : {}),
      })
      await NativeConnection.resumeAllTransfers(machineId ? { machineId } : undefined)
      void this.refreshFromNative()
    } catch (err) {
      for (const t of this._transfers) {
        if (machineId && t.machineId !== machineId) continue
        if (!canResumeStatus(t.status)) continue
        this._update(t.id, {
          status: 'paused',
          error: err instanceof Error ? err.message : String(err),
          bytesPerSecond: 0,
          updatedAt: Date.now(),
        })
      }
    }
  }

  // ─── useSyncExternalStore interface ──────────────────────────────────────

  subscribe(listener: () => void): () => void {
    this._listeners.add(listener)
    return () => { this._listeners.delete(listener) }
  }

  getSnapshot(machineId?: string): FileTransferStoreSnapshot {
    if (machineId) {
      const cached = this._cachedScopedSnapshots.get(machineId)
      if (cached?.version === this._snapshotVersion) return cached.snapshot
      const transfers = this._transfers.filter((t) => t.machineId === machineId)
      const snapshot = {
        transfers,
        hasActiveTransfers: transfers.some(
          (t) => t.status === 'pending' || t.status === 'transferring',
        ),
      }
      this._cachedScopedSnapshots.set(machineId, {
        version: this._snapshotVersion,
        snapshot,
      })
      return snapshot
    }
    if (this._cachedVersion === this._snapshotVersion && this._cachedSnapshot) {
      return this._cachedSnapshot
    }
    this._cachedSnapshot = {
      transfers: this._transfers,
      hasActiveTransfers: this._transfers.some(
        (t) => t.status === 'pending' || t.status === 'transferring',
      ),
    }
    this._cachedVersion = this._snapshotVersion
    return this._cachedSnapshot
  }

  // ─── Internal ─────────────────────────────────────────────────────────────

  private _handleTransferSync(data: TransferSyncPayload): void {
    if (!data.transfers) return
    const syncReceivedAt = Date.now()

    // Remove pending placeholders when real native entries arrive
    const hasRealUploads = data.transfers.some(
      (t) => t.direction === 'upload' && !t.id.startsWith('pending_'),
    )
    if (hasRealUploads) {
      const placeholders = this._transfers.filter(
        (t) => t.id.startsWith('pending_upload_') && t.direction === 'upload',
      )
      for (const p of placeholders) this._remove(p.id)
    }

    for (const nt of data.transfers) {
      const existing = this._transfers.find((t) => t.id === nt.id)
      const status = normalizeTransferStatus(nt.status)
      const measured = status === 'paused' || status === 'missing'
        ? { bytesPerSecond: 0, updatedAt: syncReceivedAt }
        : this._measureSpeed(nt.id, nt.transferredSize, nt.bytesPerSecond)
      const machineId = nt.machineId ?? nt.storeKey ?? existing?.machineId
      if (existing) {
        this._update(nt.id, {
          machineId,
          name: nt.name || existing.name,
          direction: nt.direction || existing.direction,
          totalSize: nt.totalSize || existing.totalSize,
          transferredSize: nt.transferredSize,
          status,
          startedAt: nt.startedAt || existing.startedAt,
          updatedAt: measured.updatedAt,
          bytesPerSecond: measured.bytesPerSecond,
          filePath: nt.filePath ?? existing.filePath,
          ...(nt.localUri ?? existing.localUri ? { localUri: nt.localUri ?? existing.localUri } : {}),
          ...(nt.targetDir ?? existing.targetDir ? { targetDir: nt.targetDir ?? existing.targetDir } : {}),
          ...(nt.savedPath ?? existing.savedPath ? { savedPath: nt.savedPath ?? existing.savedPath } : {}),
          ...(nt.savedUri ?? existing.savedUri ? { savedUri: nt.savedUri ?? existing.savedUri } : {}),
          error: nt.error,
        })
      } else {
        this._addOrUpdate({
          id: nt.id,
          machineId,
          name: nt.name,
          direction: nt.direction,
          totalSize: nt.totalSize,
          transferredSize: nt.transferredSize,
          status,
          startedAt: nt.startedAt,
          updatedAt: measured.updatedAt,
          bytesPerSecond: measured.bytesPerSecond,
          filePath: nt.filePath,
          ...(nt.localUri ? { localUri: nt.localUri } : {}),
          ...(nt.targetDir ? { targetDir: nt.targetDir } : {}),
          ...(nt.savedPath ? { savedPath: nt.savedPath } : {}),
          ...(nt.savedUri ? { savedUri: nt.savedUri } : {}),
          error: nt.error,
        })
      }
    }
  }

  private async _ensureSession(machineId?: string): Promise<NativeTransferSession> {
    if (machineId && this._sessionResolver) {
      const session = await this._sessionResolver(machineId)
      if (session) this.setSession(session)
      if (this._isSessionUsable(this._session)) return this._session
      throw new Error('Native transfer bridge is not connected')
    }
    if (this._isSessionUsable(this._session)) return this._session
    if (!machineId || !this._sessionResolver) {
      throw new Error('Connect this machine before resuming the transfer')
    }
    const session = await this._sessionResolver(machineId)
    if (session) this.setSession(session)
    if (!this._isSessionUsable(this._session)) {
      throw new Error('Native transfer bridge is not connected')
    }
    return this._session
  }

  private _isSessionUsable(session: NativeTransferSession | null): session is NativeTransferSession {
    if (!session) return false
    return session.isAlive()
  }

  private _measureSpeed(id: string, transferredSize: number, nativeSpeed?: number): {
    bytesPerSecond: number
    updatedAt: number
  } {
    const now = Date.now()
    const previous = this._speedSamples.get(id)
    if (typeof nativeSpeed === 'number' && Number.isFinite(nativeSpeed) && nativeSpeed >= 0) {
      const bytesPerSecond = previous && previous.bytesPerSecond > 0
        ? previous.bytesPerSecond * 0.75 + nativeSpeed * 0.25
        : nativeSpeed
      this._speedSamples.set(id, { bytesPerSecond, timestamp: now, transferredSize })
      return { bytesPerSecond, updatedAt: now }
    }

    if (!previous) {
      this._speedSamples.set(id, { bytesPerSecond: 0, timestamp: now, transferredSize })
      return { bytesPerSecond: 0, updatedAt: now }
    }

    const elapsedSeconds = (now - previous.timestamp) / 1000
    if (elapsedSeconds < 0.8) {
      return { bytesPerSecond: previous.bytesPerSecond, updatedAt: previous.timestamp }
    }

    const deltaBytes = Math.max(0, transferredSize - previous.transferredSize)
    const instantSpeed = deltaBytes / elapsedSeconds
    const bytesPerSecond = deltaBytes > 0
      ? previous.bytesPerSecond > 0
        ? previous.bytesPerSecond * 0.75 + instantSpeed * 0.25
        : instantSpeed
      : previous.bytesPerSecond * 0.6

    this._speedSamples.set(id, { bytesPerSecond, timestamp: now, transferredSize })
    return { bytesPerSecond, updatedAt: now }
  }

  private _addOrUpdate(info: TransferInfo): void {
    const idx = this._transfers.findIndex((t) => t.id === info.id)
    if (idx >= 0) {
      this._transfers = this._transfers.map((t) => (t.id === info.id ? { ...t, ...info } : t))
    } else {
      this._transfers = [...this._transfers, info]
    }
    this._notify()
  }

  private _update(id: string, updates: Partial<TransferInfo>): void {
    this._transfers = this._transfers.map((t) => (t.id === id ? { ...t, ...updates } : t))
    this._notify()
  }

  private _remove(id: string): void {
    this._speedSamples.delete(id)
    this._transfers = this._transfers.filter((t) => t.id !== id)
    this._notify()
  }

  private _notify(): void {
    this._snapshotVersion++
    for (const fn of this._listeners) fn()
  }
}

function canResumeStatus(status: TransferStatus): boolean {
  return status === 'paused' || status === 'failed' || status === 'missing' || status === 'pending'
}

function uniqueMachineIds(transfers: TransferInfo[]): string[] {
  return Array.from(new Set(
    transfers
      .filter((transfer) => canResumeStatus(transfer.status))
      .map((transfer) => transfer.machineId)
      .filter((machineId): machineId is string => Boolean(machineId)),
  ))
}

function normalizeTransferStatus(status: string): TransferStatus {
  if (
    status === 'pending' ||
    status === 'transferring' ||
    status === 'paused' ||
    status === 'completed' ||
    status === 'failed' ||
    status === 'cancelled' ||
    status === 'missing'
  ) {
    return status
  }
  return 'failed'
}
