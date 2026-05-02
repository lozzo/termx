import { normalizeTerminal, type Terminal } from './model'
import type { ConnectionInfo } from './transport'
import type { ConnectionMessage } from './connectionMessageReducer'

export interface TerminalSnapshotPayload {
  text: string
  cols: number
  rows: number
  replay?: string
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

export interface TerminalProtocolSession {
  openTerminal(terminalId: string): Promise<TerminalProtocolChannel>
  getConnectionInfo(): Promise<ConnectionInfo>
  subscribeTerminal(terminalId: string, handler: (event: TerminalProtocolEvent) => void): () => void
  closeTerminalChannel(terminalId: string): void
}

export interface TerminalClientCallbacks {
  onOutput: (data: Uint8Array) => void
  onSnapshot: (snapshot: TerminalSnapshotPayload) => void
  onTerminalInfo?: (info: Terminal) => void
  onResizeControl?: (control: TerminalResizeControl) => void
  onLifecycle?: (message: Extract<ConnectionMessage, { type: 'terminal.channelOpen' | 'terminal.channelClosed' }>) => void
  onError: (message: string) => void
  onClose: () => void
  onOpen?: () => void
  onInputDropped?: () => void
}

export class TerminalClient {
  private callbacks: TerminalClientCallbacks
  private session: TerminalProtocolSession | null = null
  private terminalId = ''
  private channel: TerminalProtocolChannel | null = null
  private unsubscribe: (() => void) | null = null
  private machineId = ''
  private resizeControl: TerminalResizeControl = defaultTerminalResizeControl

  constructor(callbacks: TerminalClientCallbacks) {
    this.callbacks = callbacks
  }

  connect(terminalId: string, session: TerminalProtocolSession): void {
    this.bind(terminalId, session, { closePrevious: true })
  }

  reattach(session: TerminalProtocolSession): void {
    if (!this.terminalId) {
      this.callbacks.onError('terminal client is not connected')
      return
    }
    this.bind(this.terminalId, session, { closePrevious: false })
  }

  disconnect(): void {
    const terminalId = this.terminalId
    this.cleanupSubscription()
    if (this.session && terminalId) {
      this.session.closeTerminalChannel(terminalId)
    }
    this.channel = null
    this.session = null
    this.terminalId = ''
  }

  sendInput(data: string): void {
    this.sendMessage({ type: 'input', data })
  }

  sendResize(cols: number, rows: number): void {
    if (!this.resizeControl.canResize) {
      return
    }
    this.sendMessage({ type: 'resize', cols, rows })
  }

  private bind(
    terminalId: string,
    session: TerminalProtocolSession,
    options: { closePrevious: boolean },
  ): void {
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

    Promise.all([
      this.resolveMachineId(session),
      session.openTerminal(terminalId),
    ]).then(([machineId, channel]) => {
      if (this.session !== session || this.terminalId !== terminalId) {
        channel.close()
        return
      }
      this.machineId = machineId
      if (channel.label !== `terminal:${terminalId}`) {
        channel.close()
        this.callbacks.onError(`unexpected terminal channel label ${channel.label}`)
        return
      }
      this.channel = channel
      this.callbacks.onOpen?.()
      this.callbacks.onLifecycle?.({
        type: 'terminal.channelOpen',
        machineId,
        terminalId,
      })
    }).catch((err: unknown) => {
      this.callbacks.onError(errorMessage(err))
      this.callbacks.onClose()
    })

    this.unsubscribe = session.subscribeTerminal(terminalId, (event) => this.handleProtocolEvent(event))
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
        this.callbacks.onClose()
        return
    }
  }

  private sendMessage(message: { type: 'input'; data: string } | { type: 'resize'; cols: number; rows: number }): void {
    const channel = this.channel
    if (!channel || channel.readyState !== 'open') {
      this.callbacks.onInputDropped?.()
      return
    }

    try {
      channel.send(new TextEncoder().encode(JSON.stringify(message)))
    } catch {
      this.callbacks.onInputDropped?.()
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
