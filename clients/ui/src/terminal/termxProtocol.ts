export const TERMX_PROTOCOL_VERSION = 4
export const TERMX_MAX_FRAME_SIZE = 4 << 20

export const TERMX_FRAME_TYPES = {
  hello: 0x00,
  request: 0x01,
  response: 0x02,
  event: 0x03,
  error: 0x04,
  responseBinary: 0x05,
  input: 0x11,
  resize: 0x12,
  bootstrapDone: 0x13,
  screenUpdate: 0x14,
  streamReady: 0x15,
  syncLost: 0x16,
  closed: 0x17,
  historyRequest: 0x18,
  historyReplay: 0x19,
  fileData: 0x21,
  fileAck: 0x22,
  fileFinish: 0x23,
  fileResult: 0x24,
} as const

export type TermxFrameType = typeof TERMX_FRAME_TYPES[keyof typeof TERMX_FRAME_TYPES]

export interface TermxFrame {
  channel: number
  type: TermxFrameType | number
  payload: Uint8Array
}

export function encodeTermxFrame(channel: number, type: number, payload: Uint8Array = new Uint8Array()): Uint8Array {
  if (payload.length > TERMX_MAX_FRAME_SIZE) {
    throw new Error('termx frame payload too large')
  }
  const frame = new Uint8Array(7 + payload.length)
  const view = new DataView(frame.buffer)
  view.setUint16(0, channel)
  view.setUint8(2, type)
  view.setUint32(3, payload.length)
  frame.set(payload, 7)
  return frame
}

export function decodeTermxFrame(frame: Uint8Array): TermxFrame {
  if (frame.length < 7) {
    throw new Error('termx frame too short')
  }
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength)
  const channel = view.getUint16(0)
  const type = view.getUint8(2)
  const length = view.getUint32(3)
  if (length > TERMX_MAX_FRAME_SIZE) {
    throw new Error('termx frame payload too large')
  }
  if (length !== frame.length - 7) {
    throw new Error('termx frame malformed length')
  }
  return {
    channel,
    type,
    payload: frame.slice(7),
  }
}

export function encodeResizePayload(cols: number, rows: number): Uint8Array {
  const payload = new Uint8Array(4)
  const view = new DataView(payload.buffer)
  view.setUint16(0, cols)
  view.setUint16(2, rows)
  return payload
}

export function encodeHistoryRequestPayload(beforeOffset: number, limit: number, alternate = false): Uint8Array {
  const payload = new Uint8Array(9)
  const view = new DataView(payload.buffer)
  view.setUint32(0, Math.max(0, beforeOffset))
  view.setUint32(4, Math.max(0, limit))
  view.setUint8(8, alternate ? 1 : 0)
  return payload
}

export function decodeHistoryReplayPayload(payload: Uint8Array): { rows: number; hasMore: boolean; replay: Uint8Array } {
  if (payload.byteLength < 5) {
    throw new Error('termx history replay payload too short')
  }
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
  return {
    rows: view.getUint32(0),
    hasMore: view.getUint8(4) === 1,
    replay: payload.slice(5),
  }
}

interface CellStyleLike {
  fg: string
  bg: string
  bold: boolean
  italic: boolean
  underline: boolean
  blink: boolean
  reverse: boolean
  strikethrough: boolean
  linkUrl: string
  linkParams: string
}

interface DecodedCell {
  content: string
  width: number
  style: CellStyleLike
  linkUrl: string
  linkParams: string
}

interface DecodedCursor {
  row: number
  col: number
  visible: boolean
  shape: string
  blink: boolean
}

interface DecodedModes {
  alternateScreen: boolean
  alternateScroll: boolean
  mouseTracking: boolean
  mouseX10: boolean
  mouseNormal: boolean
  mouseButtonEvent: boolean
  mouseAnyEvent: boolean
  mouseSGR: boolean
  bracketedPaste: boolean
  applicationCursor: boolean
  autoWrap: boolean
}

interface DecodedScreenRect {
  x: number
  y: number
  width: number
  height: number
}

interface DecodedScreenOp {
  code: number
  rect: DecodedScreenRect
  src: DecodedScreenRect
  dstX: number
  dstY: number
  dx: number
  dy: number
  row: number
  col: number
  cells: DecodedCell[]
  cursor: DecodedCursor
  modes: DecodedModes
  size: { cols: number; rows: number }
  title: string
}

interface DecodedScreenUpdate {
  fullReplace: boolean
  resetScrollback: boolean
  size: { cols: number; rows: number }
  screenScroll: number
  title: string
  screen: { rows: DecodedCell[][]; alternateScreen: boolean }
  ops: DecodedScreenOp[]
  scrollbackTrim: number
  scrollbackAppend: DecodedScrollbackRow[]
  cursor: DecodedCursor
  modes: DecodedModes
}

interface DecodedScrollbackRow {
  cells: DecodedCell[]
  wrapped: boolean
}

const screenUpdatePayloadMagic = 'TSU7'

const screenUpdateFlagFullReplace = 1 << 0
const screenUpdateFlagResetScrollback = 1 << 1
const screenUpdateFlagHasTitle = 1 << 2
const screenUpdateFlagHasScreenScroll = 1 << 3

const screenOpWriteSpan = 0
const screenOpScrollRect = 1
const screenOpCopyRect = 2
const screenOpClearRect = 3
const screenOpClearToEOL = 4
const screenOpCursor = 5
const screenOpModes = 6
const screenOpResize = 7
const screenOpTitle = 8

const emptyStyle: CellStyleLike = {
  fg: '',
  bg: '',
  bold: false,
  italic: false,
  underline: false,
  blink: false,
  reverse: false,
  strikethrough: false,
  linkUrl: '',
  linkParams: '',
}

const emptyCursor: DecodedCursor = {
  row: -1,
  col: -1,
  visible: false,
  shape: '',
  blink: false,
}

const emptyModes: DecodedModes = {
  alternateScreen: false,
  alternateScroll: false,
  mouseTracking: false,
  mouseX10: false,
  mouseNormal: false,
  mouseButtonEvent: false,
  mouseAnyEvent: false,
  mouseSGR: false,
  bracketedPaste: false,
  applicationCursor: false,
  autoWrap: false,
}

export function screenUpdatePayloadToReplay(payload: Uint8Array): string | null {
  const update = decodeScreenUpdatePayload(payload)
  return screenUpdateToReplay(update)
}

function decodeScreenUpdatePayload(payload: Uint8Array): DecodedScreenUpdate {
  const magic = new TextDecoder().decode(payload.slice(0, 4))
  if (magic !== screenUpdatePayloadMagic) {
    throw new Error('unsupported terminal screen update payload')
  }
  return decodeScreenUpdatePayloadCurrent(payload)
}

function decodeScreenUpdatePayloadCurrent(payload: Uint8Array): DecodedScreenUpdate {
  const dec = new ScreenUpdateDecoder(payload)
  dec.consumeMagic(screenUpdatePayloadMagic)
  const update = dec.readHeader()
  const styles = dec.readStyles()
  if (update.fullReplace) {
    update.screen.alternateScreen = dec.readByte() !== 0
    update.screen.rows = dec.readRows(styles)
    dec.skipTimeSlice()
    dec.skipStringSlice()
    dec.skipBoolSlice()
  }
  const opCount = dec.readUvarint()
  for (let index = 0; index < opCount; index += 1) {
    update.ops.push(dec.readScreenOp(styles))
  }
  update.scrollbackTrim = dec.readUvarint()
  const appendCount = dec.readUvarint()
  for (let index = 0; index < appendCount; index += 1) {
    dec.skipTime()
    dec.skipString()
    const wrapped = dec.readWrapped()
    update.scrollbackAppend.push({ cells: dec.readCells(styles), wrapped })
  }
  dec.assertEOF()
  return update
}

class ScreenUpdateDecoder {
  private off = 0
  private readonly textDecoder = new TextDecoder()

  constructor(private readonly data: Uint8Array) {}

  consumeMagic(magic: string): void {
    if (this.remaining() < magic.length || this.textDecoder.decode(this.data.slice(this.off, this.off + magic.length)) !== magic) {
      throw new Error('invalid terminal screen update magic')
    }
    this.off += magic.length
  }

  readHeader(): DecodedScreenUpdate {
    const flags = this.readByte()
    const cols = this.readUint16()
    const rows = this.readUint16()
    const update: DecodedScreenUpdate = {
      fullReplace: (flags & screenUpdateFlagFullReplace) !== 0,
      resetScrollback: (flags & screenUpdateFlagResetScrollback) !== 0,
      size: { cols, rows },
      screenScroll: 0,
      title: '',
      screen: { rows: [], alternateScreen: false },
      ops: [],
      scrollbackTrim: 0,
      scrollbackAppend: [],
      cursor: { ...emptyCursor },
      modes: { ...emptyModes },
    }
    if ((flags & screenUpdateFlagHasScreenScroll) !== 0) {
      update.screenScroll = this.readInt32()
    }
    if ((flags & screenUpdateFlagHasTitle) !== 0) {
      update.title = this.readString()
    }
    update.modes = decodeTerminalModesMask(this.readUint16())
    update.cursor = {
      row: this.readInt32(),
      col: this.readInt32(),
      visible: this.readByte() !== 0,
      shape: decodeScreenUpdateCursorShape(this.readByte()),
      blink: this.readByte() !== 0,
    }
    if (!update.fullReplace) {
      update.screen.alternateScreen = update.modes.alternateScreen
    }
    return update
  }

  readStyles(): CellStyleLike[] {
    const count = this.readUvarint()
    const styles: CellStyleLike[] = [{ ...emptyStyle }]
    for (let index = 0; index < count; index += 1) {
      const fg = this.readString()
      const bg = this.readString()
      const mask = this.readByte()
      styles.push({
        fg,
        bg,
        bold: (mask & (1 << 0)) !== 0,
        italic: (mask & (1 << 1)) !== 0,
        underline: (mask & (1 << 2)) !== 0,
        blink: (mask & (1 << 3)) !== 0,
        reverse: (mask & (1 << 4)) !== 0,
        strikethrough: (mask & (1 << 5)) !== 0,
        linkUrl: '',
        linkParams: '',
      })
    }
    return styles
  }

  readScreenOp(styles: CellStyleLike[]): DecodedScreenOp {
    const code = this.readByte()
    const op: DecodedScreenOp = {
      code,
      rect: { x: 0, y: 0, width: 0, height: 0 },
      src: { x: 0, y: 0, width: 0, height: 0 },
      dstX: 0,
      dstY: 0,
      dx: 0,
      dy: 0,
      row: 0,
      col: 0,
      cells: [],
      cursor: { ...emptyCursor },
      modes: { ...emptyModes },
      size: { cols: 0, rows: 0 },
      title: '',
    }
    switch (code) {
      case screenOpWriteSpan:
        op.row = this.readUvarint()
        op.col = this.readUvarint()
        this.skipTime()
        this.skipString()
        this.skipWrapped()
        op.cells = this.readCells(styles)
        break
      case screenOpScrollRect:
        op.rect = this.readScreenRect()
        op.dx = this.readInt32()
        op.dy = this.readInt32()
        break
      case screenOpCopyRect:
        op.src = this.readScreenRect()
        op.dstX = this.readInt32()
        op.dstY = this.readInt32()
        break
      case screenOpClearRect:
        op.rect = this.readScreenRect()
        this.skipTime()
        this.skipString()
        this.skipWrapped()
        break
      case screenOpClearToEOL:
        op.row = this.readUvarint()
        op.col = this.readUvarint()
        this.skipTime()
        this.skipString()
        this.skipWrapped()
        break
      case screenOpCursor:
        op.cursor = {
          row: this.readInt32(),
          col: this.readInt32(),
          visible: this.readByte() !== 0,
          shape: decodeScreenUpdateCursorShape(this.readByte()),
          blink: this.readByte() !== 0,
        }
        break
      case screenOpModes:
        op.modes = decodeTerminalModesMask(this.readUint16())
        break
      case screenOpResize:
        op.size = { cols: this.readUint16(), rows: this.readUint16() }
        break
      case screenOpTitle:
        op.title = this.readString()
        break
      default:
        throw new Error(`invalid terminal screen op ${code}`)
    }
    return op
  }

  readRows(styles: CellStyleLike[]): DecodedCell[][] {
    const count = this.readUvarint()
    const rows: DecodedCell[][] = []
    for (let index = 0; index < count; index += 1) {
      rows.push(this.readCells(styles))
    }
    return rows
  }

  readCells(styles: CellStyleLike[]): DecodedCell[] {
    const count = this.readUvarint()
    const cells: DecodedCell[] = []
    for (let index = 0; index < count; index += 1) {
      const styleIndex = this.readUvarint()
      if (styleIndex >= styles.length) {
        throw new Error(`invalid terminal cell style ${styleIndex}`)
      }
      const style = styles[styleIndex]
      if (!style) {
        throw new Error(`invalid terminal cell style ${styleIndex}`)
      }
      cells.push({
        style,
        width: this.readUvarint(),
        content: this.readString(),
        linkUrl: this.readString(),
        linkParams: this.readString(),
      })
    }
    return cells
  }

  readScreenRect(): DecodedScreenRect {
    return {
      x: this.readInt32(),
      y: this.readInt32(),
      width: this.readInt32(),
      height: this.readInt32(),
    }
  }

  readByte(): number {
    this.ensure(1)
    const value = this.data[this.off] ?? 0
    this.off += 1
    return value
  }

  readUint16(): number {
    this.ensure(2)
    const view = new DataView(this.data.buffer, this.data.byteOffset + this.off, 2)
    const value = view.getUint16(0, true)
    this.off += 2
    return value
  }

  readInt32(): number {
    this.ensure(4)
    const view = new DataView(this.data.buffer, this.data.byteOffset + this.off, 4)
    const value = view.getInt32(0, true)
    this.off += 4
    return value
  }

  readUvarint(): number {
    let value = 0
    let shift = 0
    for (let index = 0; index < 10; index += 1) {
      const byte = this.readByte()
      if (byte < 0x80) {
        value += byte * (2 ** shift)
        return value
      }
      value += (byte & 0x7f) * (2 ** shift)
      shift += 7
    }
    throw new Error('invalid terminal screen update varint')
  }

  readString(): string {
    const length = this.readUvarint()
    this.ensure(length)
    const value = this.textDecoder.decode(this.data.slice(this.off, this.off + length))
    this.off += length
    return value
  }

  skipTime(): void {
    this.ensure(8)
    this.off += 8
  }

  skipString(): void {
    const length = this.readUvarint()
    this.ensure(length)
    this.off += length
  }

  skipTimeSlice(): void {
    const count = this.readUvarint()
    this.ensure(count * 8)
    this.off += count * 8
  }

  skipStringSlice(): void {
    const count = this.readUvarint()
    for (let index = 0; index < count; index += 1) {
      this.skipString()
    }
  }

  skipBoolSlice(): void {
    const count = this.readUvarint()
    this.ensure(count)
    this.off += count
  }

  skipWrapped(): void {
    void this.readWrapped()
  }

  readWrapped(): boolean {
    const hasWrapped = this.readByte() !== 0
    return hasWrapped ? this.readByte() !== 0 : false
  }

  assertEOF(): void {
    if (this.off !== this.data.length) {
      throw new Error('trailing bytes in terminal screen update payload')
    }
  }

  private ensure(length: number): void {
    if (length < 0 || this.remaining() < length) {
      throw new Error('terminal screen update payload ended early')
    }
  }

  private remaining(): number {
    return this.data.length - this.off
  }
}

function screenUpdateToReplay(update: DecodedScreenUpdate): string | null {
  const parts: string[] = []
  let cursor = update.cursor
  let modes = update.modes

  if (update.title) {
    parts.push(writeTitleANSI(update.title))
  }

  if (update.fullReplace) {
    parts.push(writePrivateModeANSI(1049, modes.alternateScreen))
    if (!modes.alternateScreen && update.resetScrollback && update.scrollbackAppend.length > 0) {
      parts.push(writeSequentialDecodedRows(update.scrollbackAppend))
      parts.push('\r\n')
      const visibleRows = Math.max(1, update.screen.rows.length)
      for (let index = 0; index < visibleRows - 1; index += 1) {
        parts.push('\n')
      }
      parts.push('\x1b[0m')
    }
    parts.push('\x1b[H\x1b[2J\x1b[H')
    parts.push(writeDecodedRowsAbsolute(update.screen.rows))
  } else {
    for (const op of update.ops) {
      switch (op.code) {
        case screenOpWriteSpan:
          parts.push(writeCellsAt(op.row, op.col, op.cells))
          break
        case screenOpScrollRect: {
          const scrollReplay = writeScrollRectANSI(op.rect, op.dx, op.dy, update.size.cols, update.size.rows)
          if (scrollReplay === null) return null
          parts.push(scrollReplay)
          break
        }
        case screenOpCopyRect:
          return null
        case screenOpClearRect:
          parts.push(writeClearRectANSI(op.rect))
          break
        case screenOpClearToEOL:
          parts.push(moveCursorANSI(op.row, op.col), '\x1b[K')
          break
        case screenOpCursor:
          cursor = op.cursor
          break
        case screenOpModes:
          modes = op.modes
          break
        case screenOpResize:
          break
        case screenOpTitle:
          if (op.title) parts.push(writeTitleANSI(op.title))
          break
        default:
          return null
      }
    }
  }

  parts.push(writeTerminalModesANSI(modes))
  parts.push(writeCursorShapeANSI(cursor))
  if (cursor.row >= 0 && cursor.col >= 0) {
    parts.push(moveCursorANSI(cursor.row, cursor.col))
  }
  parts.push(cursor.visible ? '\x1b[?25h' : '\x1b[?25l')
  return parts.join('')
}

function decodeTerminalModesMask(mask: number): DecodedModes {
  return {
    alternateScreen: (mask & (1 << 0)) !== 0,
    alternateScroll: (mask & (1 << 1)) !== 0,
    mouseTracking: (mask & (1 << 2)) !== 0,
    mouseX10: (mask & (1 << 3)) !== 0,
    mouseNormal: (mask & (1 << 4)) !== 0,
    mouseButtonEvent: (mask & (1 << 5)) !== 0,
    mouseAnyEvent: (mask & (1 << 6)) !== 0,
    mouseSGR: (mask & (1 << 7)) !== 0,
    bracketedPaste: (mask & (1 << 8)) !== 0,
    applicationCursor: (mask & (1 << 9)) !== 0,
    autoWrap: (mask & (1 << 10)) !== 0,
  }
}

function decodeScreenUpdateCursorShape(shape: number): string {
  switch (shape) {
    case 1:
      return 'underline'
    case 2:
      return 'bar'
    default:
      return 'block'
  }
}

function writeCellsAt(row: number, col: number, cells: DecodedCell[]): string {
  if (row < 0 || col < 0) return ''
  return `${moveCursorANSI(row, col)}${writeDecodedCells(cells)}`
}

function writeDecodedRowsAbsolute(rows: DecodedCell[][]): string {
  const parts: string[] = []
  for (let row = 0; row < rows.length; row += 1) {
    const cells = rows[row] ?? []
    for (let col = 0; col < cells.length; col += 1) {
      const cell = cells[col]
      if (!cell) continue
      if (cell.width === 0 || (cell.content === '' && isEmptyStyle(cellStyleWithCellLink(cell)))) {
        continue
      }
      parts.push(moveCursorANSI(row, col))
      parts.push(cellStyleANSI(cellStyleWithCellLink(cell)))
      parts.push(cell.content || ' ')
    }
  }
  if (parts.length > 0) parts.push(resetCellStyleANSI())
  return parts.join('')
}

function writeDecodedCells(cells: DecodedCell[]): string {
  const parts: string[] = []
  for (const cell of cells) {
    if (cell.width === 0) continue
    parts.push(cellStyleANSI(cellStyleWithCellLink(cell)))
    parts.push(cell.content || ' ')
  }
  if (parts.length > 0) parts.push(resetCellStyleANSI())
  return parts.join('')
}

function writeSequentialDecodedRows(rows: DecodedScrollbackRow[]): string {
  const parts: string[] = []
  for (let index = 0; index < rows.length; index += 1) {
    const row = rows[index]
    parts.push(writeSequentialDecodedRow(row?.cells ?? []))
    if (index < rows.length - 1 && !row?.wrapped) {
      parts.push('\r\n')
    }
  }
  return parts.join('')
}

function writeSequentialDecodedRow(row: DecodedCell[]): string {
  const parts: string[] = []
  for (let index = 0; index < row.length; index += 1) {
    const cell = row[index]
    if (!cell) continue
    if (cell.width === 0) continue
    parts.push(cellStyleANSI(cellStyleWithCellLink(cell)))
    parts.push(cell.content || ' ')
  }
  parts.push(resetCellStyleANSI())
  return parts.join('')
}

function writeScrollRectANSI(rect: DecodedScreenRect, dx: number, dy: number, cols: number, rows: number): string | null {
  if (dy === 0 && dx === 0) return ''
  if (dx !== 0 || rect.width <= 0 || rect.height <= 0 || rect.y < 0) return null
  const screenCols = cols > 0 ? cols : rect.width
  const screenRows = rows > 0 ? rows : rect.y + rect.height
  if (rect.x !== 0 || rect.width < screenCols) return null
  const top = rect.y + 1
  const bottom = Math.min(screenRows, rect.y + rect.height)
  if (top < 1 || bottom < top) return null
  const count = Math.abs(dy)
  const command = dy < 0 ? 'S' : 'T'
  return `\x1b[${top};${bottom}r\x1b[${count}${command}\x1b[r`
}

function writeClearRectANSI(rect: DecodedScreenRect): string {
  if (rect.width <= 0 || rect.height <= 0 || rect.y < 0) return ''
  const parts: string[] = []
  for (let row = rect.y; row < rect.y + rect.height; row += 1) {
    parts.push(moveCursorANSI(row, Math.max(0, rect.x)), `\x1b[${rect.width}X`)
  }
  return parts.join('')
}

function moveCursorANSI(row: number, col: number): string {
  return `\x1b[${row + 1};${col + 1}H`
}

function writeTitleANSI(title: string): string {
  return `\x1b]0;${title.replace(/[\x00-\x1f\x7f]/g, '')}\x07`
}

function isEmptyStyle(style: CellStyleLike): boolean {
  return !style.fg &&
    !style.bg &&
    !style.bold &&
    !style.italic &&
    !style.underline &&
    !style.blink &&
    !style.reverse &&
    !style.strikethrough &&
    !style.linkUrl &&
    !style.linkParams
}

export function rowsToText(snapshot: unknown): string {
  if (!isRecord(snapshot)) {
    return ''
  }
  const record = snapshot
  const chunks = [
    ...rowsFrom(record.scrollback),
    ...rowsFrom(record.screen && isRecord(record.screen) ? record.screen.rows : undefined),
  ]
  return rowsText(chunks)
}

export function snapshotScrollbackRows(snapshot: unknown): unknown[] {
  if (!isRecord(snapshot)) {
    return []
  }
  return rowsFrom(snapshot.scrollback)
}

export function snapshotUsesAlternateScreen(snapshot: unknown): boolean {
  return snapshotAlternateScreen(snapshot)
}

export function rowsToPlainText(rows: unknown[]): string {
  return rowsText(rows)
}

export function rowsToReplay(rows: unknown[]): string {
  return writeSequentialRows(rows)
}

export function screenRowsToPlainText(snapshot: unknown): string {
  if (!isRecord(snapshot)) {
    return ''
  }
  const screenRecord = snapshot.screen && isRecord(snapshot.screen) ? snapshot.screen : null
  return rowsText(rowsFrom(screenRecord?.rows))
}

export function screenRowsToReplay(snapshot: unknown): string {
  if (!isRecord(snapshot)) {
    return ''
  }
  const screenRecord = snapshot.screen && isRecord(snapshot.screen) ? snapshot.screen : null
  return encodeScreenSnapshot(rowsFrom(screenRecord?.rows))
}

function rowsFrom(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function rowsText(rows: unknown[]): string {
  const parts: string[] = []
  for (let index = 0; index < rows.length; index += 1) {
    const row = rows[index]
    parts.push(rowText(row))
    if (index < rows.length - 1 && !rowWrapped(row)) {
      parts.push('\n')
    }
  }
  return parts.join('')
}

function rowText(value: unknown): string {
  const cells = rowCells(value)
  if (!Array.isArray(cells)) return ''
  return cells.map((cell) => {
    if (!isRecord(cell)) return ''
    const content = typeof cell.r === 'string'
      ? cell.r
      : typeof cell.content === 'string'
        ? cell.content
        : ''
    return content
  }).join('')
}

function rowWrapped(value: unknown): boolean {
  if (!isRecord(value)) return false
  return value.wrapped === true
}

export function snapshotToReplay(snapshot: unknown): string {
  if (!isRecord(snapshot)) {
    return ''
  }

  const screenRecord = snapshot.screen && isRecord(snapshot.screen) ? snapshot.screen : null
  const screenRows = rowsFrom(screenRecord?.rows)
  const scrollbackRows = rowsFrom(snapshot.scrollback)
  const cursor = cursorFrom(snapshot.cursor)
  const modes = {
    ...modesFrom(snapshot.modes),
    alternateScreen: snapshotAlternateScreen(snapshot),
  }
  const parts: string[] = []

  parts.push(writePrivateModeANSI(1049, modes.alternateScreen))
  if (!modes.alternateScreen && scrollbackRows.length > 0) {
    parts.push(writeSequentialRows(scrollbackRows))
    parts.push('\r\n')
    const visibleRows = Math.max(1, screenRows.length)
    for (let index = 0; index < visibleRows - 1; index += 1) {
      parts.push('\n')
    }
    parts.push('\x1b[0m')
  }

  parts.push('\x1b[H\x1b[2J\x1b[H')
  parts.push(encodeScreenSnapshot(screenRows))
  parts.push(writeTerminalModesANSI(modes))
  parts.push(writeCursorShapeANSI(cursor))
  if (cursor.row >= 0 && cursor.col >= 0) {
    parts.push(`\x1b[${cursor.row + 1};${cursor.col + 1}H`)
  }
  parts.push(cursor.visible ? '\x1b[?25h' : '\x1b[?25l')

  return parts.join('')
}

function encodeScreenSnapshot(rows: unknown[]): string {
  const parts: string[] = []
  let hasContent = false
  for (let rowIndex = 0; rowIndex < rows.length; rowIndex += 1) {
    const cells = rowCells(rows[rowIndex])
    for (let colIndex = 0; colIndex < cells.length; colIndex += 1) {
      const cell = cellFrom(cells[colIndex])
      if (cell.content === '' && cell.width === 0) {
        continue
      }
      hasContent = true
      parts.push(`\x1b[${rowIndex + 1};${colIndex + 1}H`)
      parts.push(cellStyleANSI(cellStyleWithCellLink(cell)))
      parts.push(cell.content || ' ')
    }
  }
  if (!hasContent) {
    return ''
  }
  parts.push(resetCellStyleANSI())
  return parts.join('')
}

function writeSequentialRows(rows: unknown[]): string {
  const parts: string[] = []
  for (let index = 0; index < rows.length; index += 1) {
    const row = rows[index]
    parts.push(writeSequentialRow(row))
    if (index < rows.length - 1 && !rowWrapped(row)) {
      parts.push('\r\n')
    }
  }
  return parts.join('')
}

function writeSequentialRow(row: unknown): string {
  const cells = rowCells(row)
  const parts: string[] = []
  let currentStyle = emptyStyle
  for (let index = 0; index < cells.length; index += 1) {
    const cell = cellFrom(cells[index])
    if (cell.content === '' && cell.width === 0) {
      continue
    }
    const style = cellStyleWithCellLink(cell)
    if (!stylesEqual(style, currentStyle)) {
      parts.push(cellStyleANSI(style))
      currentStyle = style
    }
    parts.push(cell.content || ' ')
  }
  if (!stylesEqual(currentStyle, emptyStyle)) {
    parts.push(resetCellStyleANSI())
  }
  return parts.join('')
}

function writeTerminalModesANSI(modes: ReturnType<typeof modesFrom>): string {
  const parts: string[] = []
  parts.push(writePrivateModeANSI(1, modes.applicationCursor))
  parts.push(writePrivateModeANSI(7, modes.autoWrap))
  parts.push(writePrivateModeANSI(1007, modes.alternateScroll))
  parts.push(writePrivateModeANSI(2004, modes.bracketedPaste))

  let mouseX10 = modes.mouseX10
  let mouseNormal = modes.mouseNormal
  let mouseButton = modes.mouseButtonEvent
  let mouseAny = modes.mouseAnyEvent
  if (modes.mouseTracking && !mouseX10 && !mouseNormal && !mouseButton && !mouseAny) {
    mouseNormal = true
  }
  parts.push(writePrivateModeANSI(9, mouseX10))
  parts.push(writePrivateModeANSI(1000, mouseNormal))
  parts.push(writePrivateModeANSI(1002, mouseButton))
  parts.push(writePrivateModeANSI(1003, mouseAny))
  parts.push(writePrivateModeANSI(1005, false))
  parts.push(writePrivateModeANSI(1006, modes.mouseSGR))
  return parts.join('')
}

function writeCursorShapeANSI(cursor: ReturnType<typeof cursorFrom>): string {
  let code = 0
  switch (cursor.shape) {
    case 'underline':
      code = cursor.blink ? 3 : 4
      break
    case 'bar':
      code = cursor.blink ? 5 : 6
      break
    case 'block':
      code = cursor.blink ? 1 : 2
      break
    default:
      break
  }
  return code > 0 ? `\x1b[${code} q` : ''
}

function writePrivateModeANSI(mode: number, enabled: boolean): string {
  return `\x1b[?${mode}${enabled ? 'h' : 'l'}`
}

function cellStyleANSI(style: ReturnType<typeof styleFrom>): string {
  let ansi = `${resetHyperlinkANSI()}\x1b[0`
  if (style.bold) ansi += ';1'
  if (style.italic) ansi += ';3'
  if (style.underline) ansi += ';4'
  if (style.blink) ansi += ';5'
  if (style.reverse) ansi += ';7'
  if (style.strikethrough) ansi += ';9'
  ansi += colorANSI(style.fg, true)
  ansi += colorANSI(style.bg, false)
  return `${ansi}m${setHyperlinkANSI(style.linkUrl, style.linkParams)}`
}

function resetCellStyleANSI(): string {
  return `${resetHyperlinkANSI()}\x1b[0m`
}

function cellStyleWithCellLink(cell: DecodedCell): CellStyleLike {
  if (!cell.linkUrl && !cell.linkParams) return cell.style
  return { ...cell.style, linkUrl: cell.linkUrl, linkParams: cell.linkParams }
}

function stylesEqual(a: CellStyleLike, b: CellStyleLike): boolean {
  return a.fg === b.fg &&
    a.bg === b.bg &&
    a.bold === b.bold &&
    a.italic === b.italic &&
    a.underline === b.underline &&
    a.blink === b.blink &&
    a.reverse === b.reverse &&
    a.strikethrough === b.strikethrough &&
    a.linkUrl === b.linkUrl &&
    a.linkParams === b.linkParams
}

function setHyperlinkANSI(linkUrl: string, linkParams: string): string {
  if (!linkUrl && !linkParams) return ''
  return `\x1b]8;${sanitizeHyperlinkANSI(linkParams)};${sanitizeHyperlinkANSI(linkUrl)}\x07`
}

function resetHyperlinkANSI(): string {
  return '\x1b]8;;\x07'
}

function sanitizeHyperlinkANSI(value: string): string {
  return value.replace(/[\x00-\x1f\x7f]/g, '')
}

function colorANSI(value: string, foreground: boolean): string {
  const color = value.trim()
  if (!color) {
    return ''
  }
  if (color.startsWith('ansi:')) {
    const code = Number.parseInt(color.slice('ansi:'.length), 10)
    if (!Number.isFinite(code) || code < 0 || code > 15) {
      return ''
    }
    if (code < 8) {
      return `;${(foreground ? 30 : 40) + code}`
    }
    return `;${(foreground ? 90 : 100) + (code - 8)}`
  }
  if (color.startsWith('idx:')) {
    const code = Number.parseInt(color.slice('idx:'.length), 10)
    if (!Number.isFinite(code) || code < 0 || code > 255) {
      return ''
    }
    return `;${foreground ? 38 : 48};5;${code}`
  }
  const rgb = parseHexColor(color)
  if (!rgb) {
    return ''
  }
  return `;${foreground ? 38 : 48};2;${rgb.r};${rgb.g};${rgb.b}`
}

function parseHexColor(value: string): { r: number; g: number; b: number } | null {
  const match = /^#([0-9a-f]{6})$/i.exec(value.trim())
  const hex = match?.[1]
  if (!hex) {
    return null
  }
  return {
    r: Number.parseInt(hex.slice(0, 2), 16),
    g: Number.parseInt(hex.slice(2, 4), 16),
    b: Number.parseInt(hex.slice(4, 6), 16),
  }
}

function rowCells(value: unknown): unknown[] {
  if (Array.isArray(value)) {
    return value
  }
  if (!isRecord(value) || !Array.isArray(value.cells)) {
    return []
  }
  return value.cells
}

function cellFrom(value: unknown): {
  content: string
  width: number
  style: ReturnType<typeof styleFrom>
  linkUrl: string
  linkParams: string
} {
  if (!isRecord(value)) {
    return { content: '', width: 0, style: styleFrom(undefined), linkUrl: '', linkParams: '' }
  }
  return {
    content: typeof value.r === 'string'
      ? value.r
      : typeof value.content === 'string'
        ? value.content
        : '',
    width: typeof value.w === 'number'
      ? value.w
      : typeof value.width === 'number'
        ? value.width
        : 0,
    style: styleFrom(value.s ?? value.style),
    linkUrl: typeof value.link_url === 'string'
      ? value.link_url
      : typeof value.linkUrl === 'string'
        ? value.linkUrl
        : '',
    linkParams: typeof value.link_params === 'string'
      ? value.link_params
      : typeof value.linkParams === 'string'
        ? value.linkParams
        : '',
  }
}

function styleFrom(value: unknown) {
  if (!isRecord(value)) {
    return {
      fg: '',
      bg: '',
      bold: false,
      italic: false,
      underline: false,
      blink: false,
      reverse: false,
      strikethrough: false,
      linkUrl: '',
      linkParams: '',
    }
  }
  return {
    fg: typeof value.fg === 'string' ? value.fg : '',
    bg: typeof value.bg === 'string' ? value.bg : '',
    bold: value.b === true || value.bold === true,
    italic: value.i === true || value.italic === true,
    underline: value.u === true || value.underline === true,
    blink: value.k === true || value.blink === true,
    reverse: value.rv === true || value.reverse === true,
    strikethrough: value.st === true || value.strikethrough === true,
    linkUrl: typeof value.link_url === 'string'
      ? value.link_url
      : typeof value.linkUrl === 'string'
        ? value.linkUrl
        : '',
    linkParams: typeof value.link_params === 'string'
      ? value.link_params
      : typeof value.linkParams === 'string'
        ? value.linkParams
        : '',
  }
}

function cursorFrom(value: unknown) {
  if (!isRecord(value)) {
    return {
      row: -1,
      col: -1,
      visible: false,
      shape: '',
      blink: false,
    }
  }
  return {
    row: typeof value.row === 'number' ? value.row : -1,
    col: typeof value.col === 'number' ? value.col : -1,
    visible: value.visible === true,
    shape: typeof value.shape === 'string' ? value.shape : '',
    blink: value.blink === true,
  }
}

function modesFrom(value: unknown) {
  if (!isRecord(value)) {
    return {
      alternateScreen: false,
      alternateScroll: false,
      mouseTracking: false,
      mouseX10: false,
      mouseNormal: false,
      mouseButtonEvent: false,
      mouseAnyEvent: false,
      mouseSGR: false,
      bracketedPaste: false,
      applicationCursor: false,
      autoWrap: false,
    }
  }
  return {
    alternateScreen: value.alternate_screen === true || value.alternateScreen === true,
    alternateScroll: value.alternate_scroll === true || value.alternateScroll === true,
    mouseTracking: value.mouse_tracking === true || value.mouseTracking === true,
    mouseX10: value.mouse_x10 === true || value.mouseX10 === true,
    mouseNormal: value.mouse_normal === true || value.mouseNormal === true,
    mouseButtonEvent: value.mouse_button_event === true || value.mouseButtonEvent === true,
    mouseAnyEvent: value.mouse_any_event === true || value.mouseAnyEvent === true,
    mouseSGR: value.mouse_sgr === true || value.mouseSGR === true,
    bracketedPaste: value.bracketed_paste === true || value.bracketedPaste === true,
    applicationCursor: value.application_cursor === true || value.applicationCursor === true,
    autoWrap: value.auto_wrap === true || value.autoWrap === true,
  }
}

function snapshotAlternateScreen(snapshot: unknown): boolean {
  if (!isRecord(snapshot)) {
    return false
  }
  const screen = snapshot.screen
  return snapshot.screen_is_alternate === true ||
    snapshot.screenIsAlternate === true ||
    (isRecord(screen) && (screen.alternateScreen === true || screen.alternate_screen === true)) ||
    modesFrom(snapshot.modes).alternateScreen
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
