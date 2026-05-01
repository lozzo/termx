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
  if (typeof snapshot !== 'object' || snapshot === null || Array.isArray(snapshot)) {
    return ''
  }
  const record = snapshot as Record<string, unknown>
  const chunks = [
    ...rowsFrom(record.scrollback),
    ...rowsFrom((record.screen as Record<string, unknown> | undefined)?.rows),
  ]
  return chunks.map(rowText).join('\n')
}

function rowsFrom(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function rowText(value: unknown): string {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return ''
  const cells = (value as Record<string, unknown>).cells
  if (!Array.isArray(cells)) return ''
  return cells.map((cell) => {
    if (typeof cell !== 'object' || cell === null || Array.isArray(cell)) return ''
    const content = (cell as Record<string, unknown>).r
    return typeof content === 'string' ? content : ''
  }).join('')
}
