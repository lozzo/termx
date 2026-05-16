import { describe, expect, it } from 'vitest'
import {
  TERMX_FRAME_TYPES,
  decodeTermxFrame,
  encodeHistoryRequestPayload,
  encodeResizePayload,
  encodeTermxFrame,
  rowsToText,
  screenUpdatePayloadToReplay,
  snapshotUsesAlternateScreen,
  snapshotToReplay,
} from './termxProtocol'
import { encodeMockScreenUpdatePayload } from '../test/mockRtcTerminalSession'

describe('termxProtocol', () => {
  it('encodes and decodes Go-compatible binary frames', () => {
    const frame = encodeTermxFrame(7, TERMX_FRAME_TYPES.screenUpdate, new TextEncoder().encode('hello'))

    expect(Array.from(frame.slice(0, 7))).toEqual([
      0x00, 0x07,
      TERMX_FRAME_TYPES.screenUpdate,
      0x00, 0x00, 0x00, 0x05,
    ])
    const decoded = decodeTermxFrame(frame)
    expect(decoded.channel).toBe(7)
    expect(decoded.type).toBe(TERMX_FRAME_TYPES.screenUpdate)
    expect(Array.from(decoded.payload)).toEqual(Array.from(new TextEncoder().encode('hello')))
  })

  it('encodes resize payloads as Go big-endian uint16 cols and rows', () => {
    expect(Array.from(encodeResizePayload(100, 40))).toEqual([0x00, 0x64, 0x00, 0x28])
  })

  it('encodes alternate history requests with a trailing mode byte', () => {
    const payload = new DataView(encodeHistoryRequestPayload(100, 50, true).buffer)

    expect(payload.byteLength).toBe(9)
    expect(payload.getUint32(0)).toBe(100)
    expect(payload.getUint32(4)).toBe(50)
    expect(payload.getUint8(8)).toBe(1)
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

  it('recognizes alternate screen snapshots from screen fields', () => {
    const snapshot = {
      screen_is_alternate: true,
      modes: { alternate_screen: false },
      scrollback: [
        { cells: [{ r: 'o' }, { r: 'l' }, { r: 'd' }] },
      ],
      screen: {
        rows: [
          { cells: [{ r: 't' }, { r: 'u' }, { r: 'i' }] },
        ],
      },
    }

    expect(snapshotUsesAlternateScreen(snapshot)).toBe(true)
    const replay = snapshotToReplay(snapshot)
    expect(replay).toContain('\x1b[?1049h')
    expect(replay).not.toContain('old')
  })

  it('decodes screen update payloads into replayable terminal output', () => {
    const replay = screenUpdatePayloadToReplay(encodeMockScreenUpdatePayload('stream-data'))

    expect(replay).toContain('\x1b[1;1H')
    expect(replay).toContain('stream-data')
    expect(replay).toContain('\x1b[?25h')
  })
})
