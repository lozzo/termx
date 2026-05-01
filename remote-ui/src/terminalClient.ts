import { normalizeTerminal, type Terminal } from './model'
import type { BinaryChannel, ConnectionInfo, PeerTransport } from './transport'
import type { ConnectionMessage } from './connectionMessageReducer'

export interface TerminalSnapshotPayload {
  text: string
  cols: number
  rows: number
}

export type TerminalInfoPayload = Record<string, unknown>

export type TerminalTransportEvent =
  | { type: 'output'; data: Uint8Array }
  | { type: 'snapshot'; snapshot: TerminalSnapshotPayload }
  | { type: 'info'; info: TerminalInfoPayload }
  | { type: 'closed'; reason?: string }

export interface TerminalTransport extends Pick<PeerTransport, 'openTerminal' | 'getConnectionInfo'> {
  subscribeTerminal(terminalId: string, handler: (event: TerminalTransportEvent) => void): () => void
  closeTerminalChannel(terminalId: string): void
}

export interface TerminalClientCallbacks {
  onOutput: (data: Uint8Array) => void
  onSnapshot: (snapshot: TerminalSnapshotPayload) => void
  onTerminalInfo?: (info: Terminal) => void
  onLifecycle?: (message: Extract<ConnectionMessage, { type: 'terminal.channelOpen' | 'terminal.channelClosed' }>) => void
  onError: (message: string) => void
  onClose: () => void
  onOpen?: () => void
  onInputDropped?: () => void
}

export class TerminalClient {
  private callbacks: TerminalClientCallbacks
  private transport: TerminalTransport | null = null
  private terminalId = ''
  private channel: BinaryChannel | null = null
  private unsubscribe: (() => void) | null = null
  private machineId = ''

  constructor(callbacks: TerminalClientCallbacks) {
    this.callbacks = callbacks
  }

  connect(terminalId: string, transport: TerminalTransport): void {
    this.bind(terminalId, transport, { closePrevious: true })
  }

  reattach(transport: TerminalTransport): void {
    if (!this.terminalId) {
      this.callbacks.onError('terminal client is not connected')
      return
    }
    this.bind(this.terminalId, transport, { closePrevious: false })
  }

  disconnect(): void {
    const terminalId = this.terminalId
    this.cleanupSubscription()
    if (this.transport && terminalId) {
      this.transport.closeTerminalChannel(terminalId)
    }
    this.channel = null
    this.transport = null
    this.terminalId = ''
  }

  sendInput(data: string): void {
    this.sendMessage({ type: 'input', data })
  }

  sendResize(cols: number, rows: number): void {
    this.sendMessage({ type: 'resize', cols, rows })
  }

  private bind(
    terminalId: string,
    transport: TerminalTransport,
    options: { closePrevious: boolean },
  ): void {
    if (!terminalId) {
      throw new Error('terminalId is required')
    }

    if (options.closePrevious) {
      this.cleanupSubscription()
      if (this.transport && this.terminalId) {
        this.transport.closeTerminalChannel(this.terminalId)
      }
    } else {
      this.cleanupSubscription()
    }

    this.transport = transport
    this.terminalId = terminalId

    Promise.all([
      this.resolveMachineId(transport),
      transport.openTerminal(terminalId),
    ]).then(([machineId, channel]) => {
      if (this.transport !== transport || this.terminalId !== terminalId) {
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

    this.unsubscribe = transport.subscribeTerminal(terminalId, (event) => this.handleTransportEvent(event))
  }

  private handleTransportEvent(event: TerminalTransportEvent): void {
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

  private async resolveMachineId(transport: TerminalTransport): Promise<string> {
    const info: ConnectionInfo = await transport.getConnectionInfo()
    return info.machineId
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
