import { describe, expect, it } from 'vitest'
import {
  TERMX_FRAME_TYPES,
  decodeTermxFrame,
  encodeResizePayload,
  encodeTermxFrame,
  rowsToText,
  snapshotToReplay,
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

  it('encodes a structured snapshot into replayable terminal output with cursor and style state', () => {
    const replay = snapshotToReplay({
      screen: {
        rows: [
          {
            cells: [
              { r: 'h', s: { fg: 'ansi:1', b: true } },
              { r: 'i', s: { fg: '#00ff00' } },
            ],
          },
        ],
      },
      scrollback: [
        {
          cells: [
            { r: 'o', s: { fg: 'idx:208' } },
            { r: 'k' },
          ],
        },
      ],
      cursor: { row: 0, col: 1, visible: true, shape: 'bar', blink: true },
      modes: { auto_wrap: true, bracketed_paste: true },
    })

    expect(replay).toContain('\x1b[H\x1b[2J\x1b[H')
    expect(replay).toContain('\x1b[?1049l')
    expect(replay).toContain('\x1b[0;38;5;208mo')
    expect(replay).toContain('\x1b[1;1H\x1b[0;1;31mh')
    expect(replay).toContain('\x1b[1;2H\x1b[0;38;2;0;255;0mi')
    expect(replay).toContain('\x1b[5 q')
    expect(replay).toContain('\x1b[1;2H')
    expect(replay).toContain('\x1b[?25h')
  })

  it('emits alternate screen enter and exit sequences from snapshot modes', () => {
    expect(snapshotToReplay({ modes: { alternate_screen: true }, screen: { rows: [] } })).toContain('\x1b[?1049h')
    expect(snapshotToReplay({ modes: { alternate_screen: false }, screen: { rows: [] } })).toContain('\x1b[?1049l')
    expect(snapshotToReplay({ screen: { rows: [] } })).toContain('\x1b[?1049l')
  })
})
