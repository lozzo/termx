import { normalizeTerminal, type Terminal } from '../core/model'
import type { ConnectionInfo } from '../core/transport'
import type { ConnectionMessage } from '../connection/connectionMessageReducer'

export interface TerminalSnapshotPayload {
  text: string
  cols: number
  rows: number
  replay?: string
  raw?: unknown
  scrollbackRows?: unknown[]
  alternateScreen?: boolean
  history?: {
    revision: number
    prependedRows: number
    loadedRows: number
    hasMore: boolean
  }
}

export interface TerminalScrollbackPage {
  offset: number
  limit: number
  rows: unknown[]
  rawSnapshot: unknown
  snapshot: TerminalSnapshotPayload
  hasMore: boolean
}

export interface TerminalScrollbackLoadResult {
  loadedRows: number
  totalRows: number
  hasMore: boolean
}

export type TerminalInfoPayload = Record<string, unknown>

export type TerminalResizePolicy = 'owner' | 'follower'
export type TerminalResizeControlReason = 'owner' | 'follower' | 'observer' | 'size_locked' | 'unknown'

export interface TerminalResizeControl {
  canResize: boolean
  reason: TerminalResizeControlReason
  sizeLocked?: boolean
}

export const defaultTerminalResizeControl: TerminalResizeControl = {
  canResize: false,
  reason: 'unknown',
}

export type TerminalProtocolEvent =
  | { type: 'output'; data: Uint8Array }
  | { type: 'snapshot'; snapshot: TerminalSnapshotPayload }
  | { type: 'info'; info: TerminalInfoPayload }
  | { type: 'resizeControl'; control: TerminalResizeControl }
  | { type: 'closed'; reason?: string }

export interface TerminalProtocolChannel {
  readonly label: string
  readonly readyState: 'connecting' | 'open' | 'closing' | 'closed'
  send(data: Uint8Array): void
  close(): void
}

export interface TerminalInputSize {
  cols: number
  rows: number
}

export interface TerminalProtocolSession {
  openTerminal(terminalId: string): Promise<TerminalProtocolChannel>
  getConnectionInfo(): Promise<ConnectionInfo>
  subscribeTerminal(terminalId: string, handler: (event: TerminalProtocolEvent) => void): () => void
  loadScrollback(terminalId: string, offset: number, limit: number): Promise<TerminalScrollbackPage>
  closeTerminalChannel(terminalId: string): void
}

export interface TerminalClientCallbacks {
  onOutput: (data: Uint8Array) => void
  onSnapshot: (snapshot: TerminalSnapshotPayload) => void
  onTerminalInfo?: (info: Terminal) => void
  onResizeControl?: (control: TerminalResizeControl) => void
  onLifecycle?: (message: Extract<ConnectionMessage, { type: 'terminal.channelOpen' | 'terminal.channelClosed' }>) => void
  onError: (message: string) => void
  onClose: (reason?: string) => void
  onOpen?: () => void
  onInputDropped?: () => void
  onInputSendFailed?: (message: string) => void
}

export class TerminalClient {
  private callbacks: TerminalClientCallbacks
  private session: TerminalProtocolSession | null = null
  private terminalId = ''
  private channel: TerminalProtocolChannel | null = null
  private unsubscribe: (() => void) | null = null
  private machineId = ''
  private resizeControl: TerminalResizeControl = defaultTerminalResizeControl
  private bindingGeneration = 0
  private readonly maxOpenAttempts = 2

  constructor(callbacks: TerminalClientCallbacks) {
    this.callbacks = callbacks
  }

  connect(terminalId: string, session: TerminalProtocolSession): void {
    void this.bind(terminalId, session, { closePrevious: true }).catch(() => {})
  }

  reattach(session: TerminalProtocolSession): Promise<void> {
    if (!this.terminalId) {
      this.callbacks.onError('terminal client is not connected')
      return Promise.resolve()
    }
    return this.bind(this.terminalId, session, { closePrevious: false })
  }

  disconnect(): void {
    this.bindingGeneration += 1
    const terminalId = this.terminalId
    this.cleanupSubscription()
    if (this.session && terminalId) {
      this.session.closeTerminalChannel(terminalId)
    }
    this.channel = null
    this.session = null
    this.terminalId = ''
  }

  sendInput(data: string, size?: TerminalInputSize): boolean {
    return this.sendMessage(
      { type: 'input', data, ...(size ? { cols: size.cols, rows: size.rows } : {}) },
      { reportInputFailure: true },
    )
  }

  sendResize(cols: number, rows: number): boolean {
    if (!this.resizeControl.canResize) {
      return false
    }
    return this.sendMessage({ type: 'resize', cols, rows }, { reportInputFailure: false })
  }

  loadScrollback(offset: number, limit: number): Promise<TerminalScrollbackPage> {
    if (!this.session || !this.terminalId) {
      return Promise.reject(new Error('terminal client is not connected'))
    }
    return this.session.loadScrollback(this.terminalId, offset, limit)
  }

  private bind(
    terminalId: string,
    session: TerminalProtocolSession,
    options: { closePrevious: boolean },
  ): Promise<void> {
    if (!terminalId) {
      throw new Error('terminalId is required')
    }

    if (options.closePrevious) {
      this.cleanupSubscription()
      if (this.session && this.terminalId) {
        this.session.closeTerminalChannel(this.terminalId)
      }
    } else {
      this.cleanupSubscription()
    }

    this.session = session
    this.terminalId = terminalId
    const generation = ++this.bindingGeneration

    const openPromise = this.openForBinding(session, terminalId, generation, 1).then(([machineId, channel]) => {
      if (!this.isCurrentBinding(session, terminalId, generation)) {
        channel.close()
        return
      }
      this.machineId = machineId
      if (channel.label !== `terminal:${terminalId}`) {
        channel.close()
        this.callbacks.onError(`unexpected terminal channel label ${channel.label}`)
        throw new Error(`unexpected terminal channel label ${channel.label}`)
      }
      this.channel = channel
      this.callbacks.onOpen?.()
      this.callbacks.onLifecycle?.({
        type: 'terminal.channelOpen',
        machineId,
        terminalId,
      })
      return
    }).catch((err: unknown) => {
      if (!this.isCurrentBinding(session, terminalId, generation)) return
      this.callbacks.onError(errorMessage(err))
      this.callbacks.onClose()
    })

    this.unsubscribe = session.subscribeTerminal(terminalId, (event) => {
      if (!this.isCurrentBinding(session, terminalId, generation)) return
      this.handleProtocolEvent(event)
    })
    return openPromise
  }

  private async openForBinding(
    session: TerminalProtocolSession,
    terminalId: string,
    generation: number,
    attempt: number,
  ): Promise<[string, TerminalProtocolChannel]> {
    try {
      return await Promise.all([
        this.resolveMachineId(session),
        session.openTerminal(terminalId),
      ])
    } catch (err) {
      if (!this.isCurrentBinding(session, terminalId, generation) || attempt >= this.maxOpenAttempts) throw err
      return this.openForBinding(session, terminalId, generation, attempt + 1)
    }
  }

  private isCurrentBinding(session: TerminalProtocolSession, terminalId: string, generation: number): boolean {
    return this.session === session && this.terminalId === terminalId && this.bindingGeneration === generation
  }

  private handleProtocolEvent(event: TerminalProtocolEvent): void {
    switch (event.type) {
      case 'output':
        this.callbacks.onOutput(event.data)
        return
      case 'snapshot':
        this.callbacks.onSnapshot(event.snapshot)
        return
      case 'info':
        try {
          this.callbacks.onTerminalInfo?.(normalizeTerminal(event.info))
        } catch (err) {
          this.callbacks.onError(errorMessage(err))
        }
        return
      case 'resizeControl':
        this.resizeControl = event.control
        this.callbacks.onResizeControl?.(event.control)
        return
      case 'closed':
        this.callbacks.onLifecycle?.({
          type: 'terminal.channelClosed',
          machineId: this.machineId || 'machine-local',
          terminalId: this.terminalId,
          ...(event.reason ? { reason: event.reason } : {}),
        })
        this.callbacks.onClose(event.reason)
        return
    }
  }

  private sendMessage(
    message: { type: 'input'; data: string; cols?: number; rows?: number } | { type: 'resize'; cols: number; rows: number },
    options: { reportInputFailure: boolean },
  ): boolean {
    const channel = this.channel
    if (!channel || channel.readyState !== 'open') {
      if (options.reportInputFailure) {
        this.callbacks.onInputDropped?.()
        this.callbacks.onInputSendFailed?.('terminal channel is not open')
      }
      return false
    }

    try {
      channel.send(new TextEncoder().encode(JSON.stringify(message)))
      return true
    } catch (err) {
      if (options.reportInputFailure) {
        this.callbacks.onInputDropped?.()
        this.callbacks.onInputSendFailed?.(errorMessage(err))
      }
      return false
    }
  }

  private cleanupSubscription(): void {
    this.unsubscribe?.()
    this.unsubscribe = null
  }

  private async resolveMachineId(session: TerminalProtocolSession): Promise<string> {
    const info: ConnectionInfo = await session.getConnectionInfo()
    return info.machineId
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
