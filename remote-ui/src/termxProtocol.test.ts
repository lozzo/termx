import { describe, expect, it } from 'vitest'
import {
  TERMX_FRAME_TYPES,
  decodeTermxFrame,
  encodeResizePayload,
  encodeTermxFrame,
  rowsToText,
} from './termxProtocol'

describe('termxProtocol', () => {
  it('encodes and decodes Go-compatible binary frames', () => {
    const frame = encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('hello'))

    expect(Array.from(frame.slice(0, 7))).toEqual([
      0x00, 0x07,
      TERMX_FRAME_TYPES.output,
      0x00, 0x00, 0x00, 0x05,
    ])
    const decoded = decodeTermxFrame(frame)
    expect(decoded.channel).toBe(7)
    expect(decoded.type).toBe(TERMX_FRAME_TYPES.output)
    expect(Array.from(decoded.payload)).toEqual(Array.from(new TextEncoder().encode('hello')))
  })

  it('encodes resize payloads as Go big-endian uint16 cols and rows', () => {
    expect(Array.from(encodeResizePayload(100, 40))).toEqual([0x00, 0x64, 0x00, 0x28])
  })

  it('flattens snapshot screen and scrollback rows into terminal text without pane/session concepts', () => {
    const text = rowsToText({
      screen: {
        rows: [
          { cells: [{ r: 'h' }, { r: 'i' }] },
          { cells: [{ r: '!' }] },
        ],
      },
      scrollback: [
        { cells: [{ r: 'o' }, { r: 'k' }] },
      ],
    })

    expect(text).toBe('ok\nhi\n!')
    expect(JSON.stringify({ text })).not.toMatch(/workspace|tab|window|pane|session/i)
  })
})
