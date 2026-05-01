import type { BinaryChannel, ConnectionInfo, ConnectionMode, JsonRpcChannel, PeerTransport } from '../transport'
import type {
  TerminalInfoPayload,
  TerminalResizeControl,
  TerminalSnapshotPayload,
  TerminalTransport,
} from '../terminalClient'

export function createMockTerminalTransport(
  machineId = 'machine-local',
  mode: ConnectionMode = 'local',
): MockTerminalTransport {
  return new MockTerminalTransport(machineId, mode)
}

export class MockTerminalTransport implements TerminalTransport, PeerTransport {
  readonly openedTerminalIds: string[] = []
  readonly openedLabels: string[] = []
  readonly closedTerminalIds: string[] = []

  private readonly channels = new Map<string, MockBinaryChannel>()
  private readonly subscriptions = new Map<string, Set<(event: TerminalTransportEvent) => void>>()

  constructor(
    private readonly machineId: string,
    private readonly mode: ConnectionMode,
  ) {}

  async connect(): Promise<void> {}

  async disconnect(): Promise<void> {}

  async openTerminal(terminalId: string): Promise<BinaryChannel> {
    this.openedTerminalIds.push(terminalId)
    const label = `terminal:${terminalId}`
    this.openedLabels.push(label)
    const channel = new MockBinaryChannel(label)
    this.channels.set(terminalId, channel)
    return channel
  }

  async openApi(): Promise<JsonRpcChannel> {
    return {
      async request() {
        return undefined as never
      },
      close() {},
    }
  }

  async openFileTransfer(transferId: string): Promise<BinaryChannel> {
    return new MockBinaryChannel(`file:${transferId}`)
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return {
      mode: this.mode,
      connectionId: 'mock-connection',
      machineId: this.machineId,
      relayInUse: false,
    }
  }

  subscribeTerminal(terminalId: string, handler: (event: TerminalTransportEvent) => void): () => void {
    let handlers = this.subscriptions.get(terminalId)
    if (!handlers) {
      handlers = new Set()
      this.subscriptions.set(terminalId, handlers)
    }
    handlers.add(handler)
    return () => {
      handlers?.delete(handler)
    }
  }

  closeTerminalChannel(terminalId: string): void {
    this.closedTerminalIds.push(terminalId)
    this.channels.get(terminalId)?.close()
  }

  emitTerminalOutput(terminalId: string, data: Uint8Array): void {
    this.emit(terminalId, { type: 'output', data })
  }

  emitTerminalSnapshot(terminalId: string, snapshot: TerminalSnapshotPayload): void {
    this.emit(terminalId, { type: 'snapshot', snapshot })
  }

  emitTerminalInfo(terminalId: string, info: TerminalInfoPayload): void {
    this.emit(terminalId, { type: 'info', info })
  }

  emitResizeControl(terminalId: string, control: TerminalResizeControl): void {
    this.emit(terminalId, { type: 'resizeControl', control })
  }

  closeTerminal(terminalId: string, reason?: string): void {
    this.channels.get(terminalId)?.close()
    this.emit(terminalId, { type: 'closed', ...(reason ? { reason } : {}) })
  }

  sentText(terminalId: string): string {
    return this.channels.get(terminalId)?.sentText ?? ''
  }

  sentResize(terminalId: string): { cols: number; rows: number } | undefined {
    return this.channels.get(terminalId)?.lastResize
  }

  private emit(terminalId: string, event: TerminalTransportEvent): void {
    for (const handler of this.subscriptions.get(terminalId) ?? []) {
      handler(event)
    }
  }
}

class MockBinaryChannel implements BinaryChannel {
  readyState: BinaryChannel['readyState'] = 'open'
  sentText = ''
  lastResize: { cols: number; rows: number } | undefined

  constructor(readonly label: string) {}

  send(data: Uint8Array): void {
    if (this.readyState !== 'open') {
      throw new Error('channel closed')
    }
    const message = JSON.parse(new TextDecoder().decode(data)) as
      | { type: 'input'; data: string }
      | { type: 'resize'; cols: number; rows: number }
    if (message.type === 'input') this.sentText += message.data
    if (message.type === 'resize') this.lastResize = { cols: message.cols, rows: message.rows }
  }

  close(): void {
    this.readyState = 'closed'
  }
}

type TerminalTransportEvent =
  | { type: 'output'; data: Uint8Array }
  | { type: 'snapshot'; snapshot: TerminalSnapshotPayload }
  | { type: 'info'; info: TerminalInfoPayload }
  | { type: 'resizeControl'; control: TerminalResizeControl }
  | { type: 'closed'; reason?: string }
