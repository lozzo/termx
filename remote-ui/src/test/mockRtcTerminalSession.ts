import type { ConnectionInfo, ConnectionPath, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from '../transport'
import {
  TERMX_FRAME_TYPES,
  decodeTermxFrame,
  encodeTermxFrame,
} from '../termxProtocol'
import type {
  TerminalInfoPayload,
  TerminalResizeControl,
  TerminalSnapshotPayload,
} from '../terminalClient'

export function createMockRtcTerminalSession(
  machineId = 'machine-local',
  path: ConnectionPath = 'local',
): MockRtcTerminalSession {
  return new MockRtcTerminalSession(machineId, path)
}

export class MockRtcTerminalSession implements RtcSession {
  readonly openedTerminalIds: string[] = []
  readonly openedLabels: string[] = []
  readonly closedTerminalIds: string[] = []

  private readonly channels = new Map<string, MockBinaryChannel>()
  private readonly resizeControls = new Map<string, TerminalResizeControl>()
  private readonly snapshots = new Map<string, TerminalSnapshotPayload>()

  constructor(
    private readonly machineId: string,
    private readonly path: ConnectionPath,
  ) {}

  async disconnect(): Promise<void> {}

  async openTerminal(terminalId: string): Promise<RtcBinaryChannel> {
    this.openedTerminalIds.push(terminalId)
    const label = `terminal:${terminalId}`
    this.openedLabels.push(label)
    const channel = new MockBinaryChannel(label, terminalId, this)
    const resizeControl = this.resizeControls.get(terminalId)
    if (resizeControl) {
      channel.setResizeControl(resizeControl)
    }
    this.channels.set(terminalId, channel)
    return channel
  }

  async openApi(): Promise<RtcJsonRpcChannel> {
    return {
      async request() {
        return undefined as never
      },
      close() {},
    }
  }

  async openFileTransfer(transferId: string): Promise<RtcBinaryChannel> {
    return new MockBinaryChannel(`file:${transferId}`, transferId, this)
  }

  subscribeEvents(_handler: (event: RtcEvent) => void): RtcSubscription {
    return { close() {} }
  }

  async getCapabilities() {
    return {
      terminalAllowed: true,
      apiAllowed: true,
      eventsAllowed: true,
      fileTransferAllowed: true,
      terminalManagementAllowed: true,
      relayInUse: false,
    }
  }

  async getConnectionInfo(): Promise<ConnectionInfo> {
    return {
      path: this.path,
      connectionId: 'mock-connection',
      machineId: this.machineId,
      relayInUse: false,
    }
  }

  emitTerminalOutput(terminalId: string, data: Uint8Array): void {
    this.channels.get(terminalId)?.emitFrame(TERMX_FRAME_TYPES.output, data)
  }

  emitTerminalSnapshot(terminalId: string, snapshot: TerminalSnapshotPayload): void {
    this.snapshots.set(terminalId, snapshot)
    this.channels.get(terminalId)?.respondToNextSnapshot(snapshot)
  }

  emitTerminalInfo(terminalId: string, info: TerminalInfoPayload): void {
    this.channels.get(terminalId)?.emitFrame(TERMX_FRAME_TYPES.response, new TextEncoder().encode(JSON.stringify({
      id: 9999,
      result: JSON.stringify(info),
    })), 0)
  }

  emitResizeControl(terminalId: string, control: TerminalResizeControl): void {
    this.resizeControls.set(terminalId, control)
    this.channels.get(terminalId)?.setResizeControl(control)
  }

  closeTerminal(terminalId: string, reason?: string): void {
    this.channels.get(terminalId)?.close(reason)
  }

  sentText(terminalId: string): string {
    return this.channels.get(terminalId)?.sentText ?? ''
  }

  sentResize(terminalId: string): { cols: number; rows: number } | undefined {
    return this.channels.get(terminalId)?.lastResize
  }

  snapshotFor(terminalId: string): TerminalSnapshotPayload | undefined {
    return this.snapshots.get(terminalId)
  }
}

class MockBinaryChannel implements RtcBinaryChannel {
  readyState: RtcBinaryChannel['readyState'] = 'open'
  sentText = ''
  lastResize: { cols: number; rows: number } | undefined
  private messageHandler: ((data: Uint8Array) => void) | undefined
  private closeHandler: (() => void) | undefined
  private streamChannel = 7
  private attachResizeControl: TerminalResizeControl = { canResize: false, reason: 'follower' }
  private ensureResizeControl: TerminalResizeControl | undefined

  constructor(
    readonly label: string,
    private readonly terminalId: string,
    private readonly owner: Pick<MockRtcTerminalSession, 'snapshotFor'>,
  ) {}

  send(data: Uint8Array): void {
    if (this.readyState !== 'open') {
      throw new Error('channel closed')
    }
    const frame = decodeTermxFrame(data)
    if (frame.channel === 0) {
      this.handleControlFrame(frame)
      return
    }
    if (frame.channel !== this.streamChannel) return
    if (frame.type === TERMX_FRAME_TYPES.input) {
      this.sentText += new TextDecoder().decode(frame.payload)
      return
    }
    if (frame.type === TERMX_FRAME_TYPES.resize) {
      const view = new DataView(frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength)
      this.lastResize = { cols: view.getUint16(0), rows: view.getUint16(2) }
    }
  }

  close(reason?: string): void {
    this.readyState = 'closed'
    this.emitFrame(TERMX_FRAME_TYPES.closed, reason ? new TextEncoder().encode(reason) : new Uint8Array())
    this.closeHandler?.()
  }

  onMessage(handler: (data: Uint8Array) => void) {
    this.messageHandler = handler
    return { close: () => { this.messageHandler = undefined } }
  }

  onClose(handler: () => void) {
    this.closeHandler = handler
    return { close: () => { this.closeHandler = undefined } }
  }

  waitOpen(): Promise<void> {
    return Promise.resolve()
  }

  setResizeControl(control: TerminalResizeControl): void {
    this.attachResizeControl = control
  }

  setEnsureResizeControl(control: TerminalResizeControl): void {
    this.ensureResizeControl = control
  }

  respondToNextSnapshot(snapshot: TerminalSnapshotPayload, requestId = 2): void {
    this.emitFrame(TERMX_FRAME_TYPES.response, new TextEncoder().encode(JSON.stringify({
      id: requestId,
      result: JSON.stringify({
        size: { cols: snapshot.cols, rows: snapshot.rows },
        screen: {
          rows: snapshot.text.split('\n').map((row) => ({
            cells: Array.from(row).map((char) => ({ r: char })),
          })),
        },
      }),
    })), 0)
  }

  emitFrame(type: number, payload: Uint8Array, channel = this.streamChannel): void {
    this.messageHandler?.(encodeTermxFrame(channel, type, payload))
  }

  private handleControlFrame(frame: ReturnType<typeof decodeTermxFrame>): void {
    if (frame.type !== TERMX_FRAME_TYPES.hello && frame.type !== TERMX_FRAME_TYPES.request) return
    if (frame.type === TERMX_FRAME_TYPES.hello) {
      this.messageHandler?.(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, new TextEncoder().encode(JSON.stringify({
        version: 1,
        server: 'termx-test',
      }))))
      return
    }
    const request = JSON.parse(new TextDecoder().decode(frame.payload)) as { id: number; method: string }
    if (request.method === 'attach') {
      this.messageHandler?.(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, new TextEncoder().encode(JSON.stringify({
        id: request.id,
        result: JSON.stringify({
          mode: 'collaborator',
          channel: this.streamChannel,
          resize_control: {
            can_resize: this.attachResizeControl.canResize,
            reason: this.attachResizeControl.reason,
            ...(this.attachResizeControl.sizeLocked ? { size_locked: true } : {}),
          },
        }),
      }))))
      return
    }
    if (request.method === 'snapshot') {
      this.respondToNextSnapshot(
        this.terminalSnapshot() ?? { text: '', cols: 80, rows: 24 },
        request.id,
      )
      return
    }
    if (request.method === 'ensure_resize') {
      const control = this.ensureResizeControl ?? this.attachResizeControl
      this.messageHandler?.(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, new TextEncoder().encode(JSON.stringify({
        id: request.id,
        result: JSON.stringify({
          resize_control: {
            can_resize: control.canResize,
            reason: control.reason,
            ...(control.sizeLocked ? { size_locked: true } : {}),
          },
          size: { cols: 80, rows: 24 },
        }),
      }))))
    }
  }

  private terminalSnapshot(): TerminalSnapshotPayload | undefined {
    return this.owner?.snapshotFor(this.terminalId)
  }
}
