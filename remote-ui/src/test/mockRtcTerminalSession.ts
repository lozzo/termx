import type { ConnectionInfo, ConnectionPath, RtcBinaryChannel, RtcEvent, RtcJsonRpcChannel, RtcSession, RtcSubscription } from '../core/transport'
import {
  TERMX_FRAME_TYPES,
  decodeTermxFrame,
  encodeTermxFrame,
} from '../terminal/termxProtocol'
import type {
  TerminalInfoPayload,
  TerminalResizeControl,
  TerminalSnapshotPayload,
} from '../terminal/terminalClient'

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
  private readonly snapshotPages = new Map<string, MockSnapshotPage[]>()
  private readonly failingSends = new Set<string>()

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
    if (this.failingSends.delete(terminalId)) {
      channel.failNextSend()
    }
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

  closeTerminalDataChannel(terminalId: string): void {
    this.closedTerminalIds.push(terminalId)
    this.channels.get(terminalId)?.close()
    this.channels.delete(terminalId)
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

  emitTerminalScreenUpdate(terminalId: string): void {
    this.channels.get(terminalId)?.emitFrame(TERMX_FRAME_TYPES.screenUpdate, new Uint8Array([1]))
  }

  emitTerminalSnapshot(terminalId: string, snapshot: TerminalSnapshotPayload): void {
    this.snapshots.set(terminalId, snapshot)
    this.channels.get(terminalId)?.respondToNextSnapshot(snapshot)
  }

  setTerminalSnapshot(terminalId: string, snapshot: TerminalSnapshotPayload & { pages?: MockSnapshotPage[] }): void {
    this.snapshots.set(terminalId, snapshot)
    this.snapshotPages.set(terminalId, snapshot.pages ?? [])
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

  failNextTerminalSend(terminalId: string): void {
    const channel = this.channels.get(terminalId)
    if (channel) {
      channel.failNextSend()
      return
    }
    this.failingSends.add(terminalId)
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

  snapshotPageFor(terminalId: string, offset: number): MockSnapshotPage | undefined {
    return this.snapshotPages.get(terminalId)?.find((page) => page.offset === offset)
  }

  snapshotRequests(terminalId: string): Array<{ offset: number; limit: number }> {
    return this.channels.get(terminalId)?.snapshotRequests ?? []
  }
}

export interface MockSnapshotPage {
  offset: number
  rows: string[]
}

class MockBinaryChannel implements RtcBinaryChannel {
  readyState: RtcBinaryChannel['readyState'] = 'open'
  sentText = ''
  lastResize: { cols: number; rows: number } | undefined
  readonly snapshotRequests: Array<{ offset: number; limit: number }> = []
  private messageHandler: ((data: Uint8Array) => void) | undefined
  private closeHandler: (() => void) | undefined
  private streamChannel = 7
  private attachResizeControl: TerminalResizeControl = { canResize: false, reason: 'follower' }
  private ensureResizeControl: TerminalResizeControl | undefined
  private failSendOnce = false

  constructor(
    readonly label: string,
    private readonly terminalId: string,
    private readonly owner: Pick<MockRtcTerminalSession, 'snapshotFor' | 'snapshotPageFor'>,
  ) {}

  send(data: Uint8Array): void {
    if (this.readyState !== 'open') {
      throw new Error('channel closed')
    }
    if (this.failSendOnce) {
      this.failSendOnce = false
      this.close('terminal channel send failed')
      throw new Error('terminal channel send failed')
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

  failNextSend(): void {
    this.failSendOnce = true
  }

  respondToNextSnapshot(snapshot: TerminalSnapshotPayload, requestId = 2): void {
    const raw = snapshot.raw
    if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
      this.emitFrame(TERMX_FRAME_TYPES.response, new TextEncoder().encode(JSON.stringify({
        id: requestId,
        result: JSON.stringify(raw),
      })), 0)
      return
    }
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
    const request = JSON.parse(new TextDecoder().decode(frame.payload)) as {
      id: number
      method: string
      params?: { scrollback_offset?: number; scrollback_limit?: number }
    }
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
      const offset = request.params?.scrollback_offset ?? 0
      const limit = request.params?.scrollback_limit ?? 0
      this.snapshotRequests.push({ offset, limit })
      const page = limit > 1 ? this.owner.snapshotPageFor(this.terminalId, offset) : undefined
      if (page) {
        this.respondToNextSnapshot({
          text: '',
          cols: 80,
          rows: 24,
          raw: {
            size: { cols: 80, rows: 24 },
            screen: { rows: [] },
            scrollback: page.rows.map((row) => ({
              cells: Array.from(row).map((char) => ({ r: char })),
            })),
          },
        }, request.id)
        return
      }
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
