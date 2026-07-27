import { create } from '@bufbuild/protobuf'
import type { ProtoClientSession } from '../core/protoClientSession'
import { openProtoEventSubscription } from '../core/protoEventSubscription'
import { CommandEnvelopeSchema } from '../generated/apipb/application_pb'
import { ApplicationEventType, EventSubscribeCommandSchema } from '../generated/apipb/events_pb'
import {
  LiveScreenGetCommandSchema,
  CursorShape,
  type CellStyle,
  type NativeScreenResult,
  type ScreenRow,
} from '../generated/apipb/history_pb'
import {
  AttachmentMode,
  ResizeControlReason,
  ResizePolicy,
  TerminalAttachCommandSchema,
  TerminalDetachCommandSchema,
  TerminalGetCommandSchema,
  TerminalInputCommandSchema,
  TerminalRefSchema,
  TerminalResizeCommandSchema,
  TerminalSizeSchema,
  TerminalState,
  type AttachmentHandle,
  type ResizeControl,
  type TerminalInfo,
} from '../generated/apipb/terminal_pb'
import type {
  TerminalInputSize,
  TerminalProtocolChannel,
  TerminalProtocolEvent,
  TerminalProtocolSession,
  TerminalResizeControl,
  TerminalScrollbackPage,
  TerminalSnapshotPayload,
} from './terminalClient'
import { coreV2HistoryRowsANSI } from './coreV2HistoryANSI'
import { createCoreV2HistorySource } from './coreV2HistorySource'
import { CoreV2ScrollbackPager } from './coreV2ScrollbackPager'

type ProtoAttachment = {
  handle: AttachmentHandle
  channel: ProtoTerminalChannel
}

/** createProtoTerminalProtocolSession projects generated apipb into the existing UI-only TerminalClient contract. */
export function createProtoTerminalProtocolSession(session: ProtoClientSession): TerminalProtocolSession {
  return new ProtoTerminalProtocolSession(session)
}

class ProtoTerminalProtocolSession implements TerminalProtocolSession {
  private readonly attachments = new Map<string, ProtoAttachment>()
  private readonly subscribers = new Map<string, Set<(event: TerminalProtocolEvent) => void>>()
  private readonly terminalSizes = new Map<string, TerminalInputSize>()
  private readonly liveRevisions = new Map<string, bigint>()
  private readonly inputTails = new Map<string, Promise<void>>()
  private readonly eventSubscriptionReady
  private readonly scrollbackPager: CoreV2ScrollbackPager

  constructor(private readonly session: ProtoClientSession) {
    this.scrollbackPager = new CoreV2ScrollbackPager(createCoreV2HistorySource(session, session.stamp.endpointId))
    this.eventSubscriptionReady = openProtoEventSubscription(session, create(EventSubscribeCommandSchema, {
      types: [ApplicationEventType.TERMINAL_LIVE_INVALIDATED, ApplicationEventType.TERMINAL_LIFECYCLE],
    }), (event) => {
      if (event.event.case === 'liveInvalidated' && event.event.value.terminal?.terminalId) {
        this.refreshLiveScreen(event.event.value.terminal.terminalId, 'live_invalidated', event.event.value.liveRevision)
      }
      if (event.event.case === 'terminalLifecycle') {
        const terminal = event.event.value.terminal
        const terminalId = terminal?.ref?.terminalId
        if (terminal && terminalId) {
          this.rememberSize(terminalId, terminal.size)
          this.publish(terminalId, { type: 'info', info: terminalInfoView(terminal) })
        }
      }
    })
  }

  async openTerminal(terminalId: string): Promise<TerminalProtocolChannel> {
    await this.eventSubscriptionReady
    const existing = this.attachments.get(terminalId)
    if (existing) return existing.channel
    const terminal = this.terminalRef(terminalId)
    const attach = create(TerminalAttachCommandSchema, {
      terminal,
      mode: AttachmentMode.COLLABORATOR,
      resizePolicy: ResizePolicy.OWNER,
      surfaceId: `mobile-${terminalId}`,
      viewId: crypto.randomUUID(),
    })
    const result = await this.session.execute(command('terminalAttach', attach))
    if (result.result.case !== 'terminalAttach' || !result.result.value.attachment?.resource) {
      throw new Error('terminal attach returned no attachment resource')
    }
    const channel = new ProtoTerminalChannel(this, terminalId)
    this.attachments.set(terminalId, { handle: result.result.value.attachment, channel })
    this.publish(terminalId, { type: 'resizeControl', control: resizeControlView(result.result.value.resizeControl) })
    await Promise.all([this.publishTerminalInfo(terminalId), this.publishLiveScreen(terminalId, 'open')])
    return channel
  }

  async getConnectionInfo() {
    return {
      path: 'hub' as const,
      connectionId: `${this.session.stamp.endpointId}:${this.session.stamp.generation}`,
      machineId: this.session.stamp.endpointId,
      relayInUse: false,
    }
  }

  subscribeTerminal(terminalId: string, handler: (event: TerminalProtocolEvent) => void): () => void {
    const handlers = this.subscribers.get(terminalId) ?? new Set()
    handlers.add(handler)
    this.subscribers.set(terminalId, handlers)
    return () => {
      handlers.delete(handler)
      if (handlers.size === 0) this.subscribers.delete(terminalId)
    }
  }

  async loadScrollback(
    terminalId: string,
    offset: number,
    limit: number,
    alternate = false,
    options?: { signal?: AbortSignal },
  ): Promise<TerminalScrollbackPage> {
    if (alternate) return { beforeOffset: offset, limit, rows: 0, replay: '', operation: 'replace', hasMore: false, alternate: true }
    const cols = this.terminalSizes.get(terminalId)?.cols ?? 80
    const page = await this.scrollbackPager.load({
      terminalId,
      offset,
      limit,
      cols,
      ...(options?.signal ? { signal: options.signal } : {}),
    })
    return {
      beforeOffset: offset,
      limit,
      rows: page.loadedRows,
      replay: coreV2HistoryRowsANSI(page.rows, cols),
      operation: page.operation,
      committedTotalRows: page.committedTotalRows,
      logicalTotalRows: page.logicalTotalRows,
      historyGeneration: Number(page.historyGeneration),
      ...(page.firstRowId ? { firstRowId: Number(page.firstRowId) } : {}),
      ...(page.lastRowId ? { lastRowId: Number(page.lastRowId) } : {}),
      hasMore: page.hasMore,
      alternate: false,
    }
  }

  closeTerminalChannel(terminalId: string): void {
    this.scrollbackPager.forget(terminalId)
    const attachment = this.attachments.get(terminalId)
    if (!attachment) return
    this.attachments.delete(terminalId)
    attachment.channel.markClosed()
    void this.session.execute(command('terminalDetach', create(TerminalDetachCommandSchema, {
      attachment: attachment.handle.resource,
    }))).catch(() => undefined)
  }

  markSyncLost(terminalId: string): void {
    this.refreshLiveScreen(terminalId, 'manual_sync_lost')
  }

  private refreshLiveScreen(terminalId: string, reason: TerminalSnapshotPayload['refreshReason'], minimumRevision = 0n): void {
    // live screen refresh 属于当前 Proto session；bridge/generation 切换时旧请求必须被消费，
    // 存活 session 的真实失败则通过 terminal channel closed 交给现有恢复状态机。
    void this.publishLiveScreen(terminalId, reason, minimumRevision).catch((error) => {
      if (!this.session.isAlive()) return
      this.publish(terminalId, { type: 'closed', reason: errorMessage(error) })
    })
  }

  requestResizeOwner(terminalId: string, size?: TerminalInputSize): Promise<TerminalResizeControl> {
    return this.resize(terminalId, size ?? { cols: 80, rows: 24 }, ResizePolicy.OWNER)
  }

  releaseResizeOwner(terminalId: string): Promise<TerminalResizeControl> {
    return this.resize(terminalId, this.terminalSizes.get(terminalId) ?? { cols: 80, rows: 24 }, ResizePolicy.FOLLOWER)
  }

  sendInput(terminalId: string, data: string, size?: TerminalInputSize): void {
    const attachment = this.attachments.get(terminalId)
    const resource = attachment?.handle.resource
    if (!attachment || !resource) throw new Error('terminal attachment is unavailable')
    // PTY input 的真值顺序是用户事件顺序；同一 terminal 必须串行等待 ACK，不能按并发 RPC 完成顺序写入。
    const send = async () => {
      if (this.attachments.get(terminalId) !== attachment) return
      if (size) await this.resize(terminalId, size, ResizePolicy.OWNER)
      if (this.attachments.get(terminalId) !== attachment) return
      const result = await this.session.execute(command('terminalInput', create(TerminalInputCommandSchema, {
        attachment: resource,
        data: new TextEncoder().encode(data),
      })))
      if (result.result.case !== 'acknowledge') throw new Error('terminal input was not acknowledged')
    }
    const previous = this.inputTails.get(terminalId) ?? Promise.resolve()
    const next = previous.then(send).catch((error) => {
      if (this.attachments.get(terminalId) === attachment) {
        this.publish(terminalId, { type: 'closed', reason: errorMessage(error) })
      }
    })
    this.inputTails.set(terminalId, next)
    void next.finally(() => {
      if (this.inputTails.get(terminalId) === next) this.inputTails.delete(terminalId)
    })
  }

  sendResize(terminalId: string, cols: number, rows: number): void {
    void this.resize(terminalId, { cols, rows }, ResizePolicy.OWNER).catch((error) => {
      this.publish(terminalId, { type: 'closed', reason: errorMessage(error) })
    })
  }

  private async resize(terminalId: string, size: TerminalInputSize, policy: ResizePolicy): Promise<TerminalResizeControl> {
    const resource = this.attachments.get(terminalId)?.handle.resource
    if (!resource) throw new Error('terminal attachment is unavailable')
    const result = await this.session.execute(command('terminalResize', create(TerminalResizeCommandSchema, {
      attachment: resource,
      size: create(TerminalSizeSchema, { cols: size.cols, rows: size.rows }),
      resizePolicy: policy,
    })))
    if (result.result.case !== 'terminalResize') throw new Error('terminal resize returned no result')
    this.terminalSizes.set(terminalId, size)
    const control = resizeControlView(result.result.value.resizeControl)
    this.publish(terminalId, { type: 'resizeControl', control })
    return control
  }

  private async publishTerminalInfo(terminalId: string): Promise<void> {
    const result = await this.session.execute(command('terminalGet', create(TerminalGetCommandSchema, { terminal: this.terminalRef(terminalId) })))
    if (result.result.case === 'terminalGet' && result.result.value.terminal) {
      this.rememberSize(terminalId, result.result.value.terminal.size)
      this.publish(terminalId, { type: 'info', info: terminalInfoView(result.result.value.terminal) })
    }
  }

  private async publishLiveScreen(terminalId: string, reason: TerminalSnapshotPayload['refreshReason'], minimumRevision = 0n): Promise<void> {
    if (!this.attachments.has(terminalId)) return
    const result = await this.session.execute(command('liveScreenGet', create(LiveScreenGetCommandSchema, { terminal: this.terminalRef(terminalId) })))
    if (result.result.case !== 'liveScreen') throw new Error('live screen returned no result')
    if (result.result.value.liveRevision < minimumRevision || result.result.value.liveRevision < (this.liveRevisions.get(terminalId) ?? 0n)) return
    this.liveRevisions.set(terminalId, result.result.value.liveRevision)
    this.rememberSize(terminalId, result.result.value.size)
    this.publish(terminalId, { type: 'snapshot', snapshot: screenSnapshot(result.result.value, reason) })
  }

  private rememberSize(terminalId: string, size: { cols: number; rows: number } | undefined): void {
    if (size && size.cols > 0 && size.rows > 0) this.terminalSizes.set(terminalId, { cols: size.cols, rows: size.rows })
  }

  private terminalRef(terminalId: string) {
    return create(TerminalRefSchema, { endpointId: this.session.stamp.endpointId, terminalId })
  }

  private publish(terminalId: string, event: TerminalProtocolEvent): void {
    this.subscribers.get(terminalId)?.forEach((handler) => handler(event))
  }
}

class ProtoTerminalChannel implements TerminalProtocolChannel {
  readonly label: string
  readyState: TerminalProtocolChannel['readyState'] = 'open'

  constructor(private readonly owner: ProtoTerminalProtocolSession, private readonly terminalId: string) {
    this.label = `proto-terminal:${terminalId}`
  }

  send(_data: Uint8Array): void { throw new Error('Proto terminal channel accepts semantic input only') }
  sendInput(data: string, size?: TerminalInputSize): void { this.owner.sendInput(this.terminalId, data, size) }
  sendResize(cols: number, rows: number): void { this.owner.sendResize(this.terminalId, cols, rows) }
  close(): void { this.owner.closeTerminalChannel(this.terminalId) }
  markClosed(): void { this.readyState = 'closed' }
}

function command(caseName: string, value: object) {
  return create(CommandEnvelopeSchema, { command: { case: caseName, value } } as never)
}

function screenSnapshot(screen: NativeScreenResult, reason: TerminalSnapshotPayload['refreshReason']): TerminalSnapshotPayload {
  const cols = screen.size?.cols ?? 0
  const text = `\u001b[2J\u001b[H${rowsANSI(screen.rows, cols)}${cursorANSI(screen)}`
  return {
    text,
    screenReplay: text,
    screenText: rowsText(screen.rows),
    cols: screen.size?.cols ?? 0,
    rows: screen.size?.rows ?? screen.rows.length,
    alternateScreen: screen.alternateScreen,
    ...(reason ? { refreshReason: reason } : {}),
    raw: screen,
  }
}

function rowsText(rows: ScreenRow[]): string {
  return rows.map((row) => row.cells.map((cell) => cell.content || ' '.repeat(Math.max(1, cell.width))).join('').replace(/\s+$/, '')).join('\r\n')
}

function rowsANSI(rows: ScreenRow[], cols: number): string {
  return rows.map((row) => {
    let current = ''
    let width = 0
    let output = ''
    for (const cell of row.cells) {
      const style = styleANSI(cell.style)
      if (style !== current) {
        output += `\u001b[0m${style}`
        current = style
      }
      const content = cell.content || ' '.repeat(Math.max(1, cell.width))
      output += content
      width += Math.max(1, cell.width)
    }
    if (cols > width) output += `\u001b[0m${styleANSI(row.tailFill)}${' '.repeat(cols - width)}`
    return `${output}\u001b[0m`
  }).join('\r\n')
}

function styleANSI(style: CellStyle | undefined): string {
  if (!style) return ''
  const codes: string[] = []
  if (style.bold) codes.push('1')
  if (style.italic) codes.push('3')
  if (style.underline) codes.push('4')
  if (style.blink) codes.push('5')
  if (style.reverse) codes.push('7')
  if (style.strikethrough) codes.push('9')
  const foreground = colorANSI(style.foreground, true)
  const background = colorANSI(style.background, false)
  if (foreground) codes.push(foreground)
  if (background) codes.push(background)
  return codes.length > 0 ? `\u001b[${codes.join(';')}m` : ''
}

function colorANSI(value: string, foreground: boolean): string {
  const token = value.trim()
  if (!token) return ''
  if (/^\d{1,3}$/.test(token)) return `${foreground ? 38 : 48};5;${Number(token)}`
  const rgb = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(token)
  if (!rgb) return ''
  return `${foreground ? 38 : 48};2;${parseInt(rgb[1]!, 16)};${parseInt(rgb[2]!, 16)};${parseInt(rgb[3]!, 16)}`
}

function cursorANSI(screen: NativeScreenResult): string {
  const cursor = screen.cursor
  if (!cursor) return ''
  const visibility = cursor.visible ? '\u001b[?25h' : '\u001b[?25l'
  const shape = cursor.shape === CursorShape.UNDERLINE ? (cursor.blink ? 3 : 4)
    : cursor.shape === CursorShape.BAR ? (cursor.blink ? 5 : 6)
      : cursor.blink ? 1 : 2
  return `\u001b[${Math.max(1, cursor.row + 1)};${Math.max(1, cursor.col + 1)}H\u001b[${shape} q${visibility}`
}

function terminalInfoView(info: TerminalInfo): Record<string, unknown> {
  return {
    terminal_id: info.ref?.terminalId ?? '', machine_id: info.ref?.endpointId ?? '', name: info.name, command: info.command, tags: info.tags,
    state: info.state === TerminalState.RUNNING ? 'running' : info.state === TerminalState.EXITED ? 'exited' : info.state === TerminalState.REMOVED ? 'removed' : 'created', cwd: info.cwd, live_cwd: info.liveCwd,
    created_at_unix_nano: info.createdAtUnixNano, exit_code: info.exitCode, exited_at_unix_nano: info.exitedAtUnixNano,
  }
}

function resizeControlView(control: ResizeControl | undefined): TerminalResizeControl {
  if (!control) return { canResize: false, reason: 'unknown' }
  const reason = control.reason === ResizeControlReason.OWNER ? 'owner'
    : control.reason === ResizeControlReason.FOLLOWER ? 'follower'
      : control.reason === ResizeControlReason.OBSERVER ? 'observer'
        : control.reason === ResizeControlReason.SIZE_LOCKED ? 'size_locked' : 'unknown'
  return {
    canResize: control.canResize, reason, sizeLocked: control.sizeLocked,
    ...(control.surfaceId ? { surfaceId: control.surfaceId } : {}),
    ...(control.ownerSurfaceId ? { ownerSurfaceId: control.ownerSurfaceId } : {}),
    ...(control.ownerViewId ? { ownerViewId: control.ownerViewId } : {}),
  }
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error) }
