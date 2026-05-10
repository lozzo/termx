export const TERMX_PROTOCOL_VERSION = 1
export const TERMX_MAX_FRAME_SIZE = 4 << 20

export const TERMX_FRAME_TYPES = {
  hello: 0x00,
  request: 0x01,
  response: 0x02,
  event: 0x03,
  error: 0x04,
  output: 0x10,
  input: 0x11,
  resize: 0x12,
  bootstrapDone: 0x13,
  screenUpdate: 0x14,
  syncLost: 0x16,
  closed: 0x17,
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

export function rowsToText(snapshot: unknown): string {
  if (!isRecord(snapshot)) {
    return ''
  }
  const record = snapshot
  const chunks = [
    ...rowsFrom(record.scrollback),
    ...rowsFrom(record.screen && isRecord(record.screen) ? record.screen.rows : undefined),
  ]
  return chunks.map(rowText).join('\n')
}

function rowsFrom(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
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

export function snapshotToReplay(snapshot: unknown): string {
  if (!isRecord(snapshot)) {
    return ''
  }

  const screenRecord = snapshot.screen && isRecord(snapshot.screen) ? snapshot.screen : null
  const screenRows = rowsFrom(screenRecord?.rows)
  const scrollbackRows = rowsFrom(snapshot.scrollback)
  const cursor = cursorFrom(snapshot.cursor)
  const modes = modesFrom(snapshot.modes)
  const parts: string[] = []

  if (!modes.alternateScreen && scrollbackRows.length > 0) {
    parts.push(writeSequentialRows(scrollbackRows))
    parts.push('\r\n')
    const visibleRows = Math.max(1, screenRows.length)
    for (let index = 0; index < visibleRows - 1; index += 1) {
      parts.push('\n')
    }
    parts.push('\x1b[0m')
  }

  if (modes.alternateScreen) {
    parts.push('\x1b[?1049h')
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
      parts.push(cellStyleANSI(cell.style))
      parts.push(cell.content || ' ')
    }
  }
  if (!hasContent) {
    return ''
  }
  parts.push('\x1b[0m')
  return parts.join('')
}

function writeSequentialRows(rows: unknown[]): string {
  return rows.map((row) => writeSequentialRow(row)).join('\r\n')
}

function writeSequentialRow(row: unknown): string {
  const cells = rowCells(row)
  let last = cells.length - 1
  while (last >= 0) {
    const cell = cellFrom(cells[last])
    if (cell.content !== '' && cell.content.trim() !== '') {
      break
    }
    last -= 1
  }
  const parts: string[] = []
  for (let index = 0; index <= last; index += 1) {
    const cell = cellFrom(cells[index])
    if (cell.content === '' && cell.width === 0) {
      continue
    }
    parts.push(cellStyleANSI(cell.style))
    parts.push(cell.content || ' ')
  }
  parts.push('\x1b[0m')
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
  let ansi = '\x1b[0'
  if (style.bold) ansi += ';1'
  if (style.italic) ansi += ';3'
  if (style.underline) ansi += ';4'
  if (style.blink) ansi += ';5'
  if (style.reverse) ansi += ';7'
  if (style.strikethrough) ansi += ';9'
  ansi += colorANSI(style.fg, true)
  ansi += colorANSI(style.bg, false)
  return `${ansi}m`
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
} {
  if (!isRecord(value)) {
    return { content: '', width: 0, style: styleFrom(undefined) }
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
