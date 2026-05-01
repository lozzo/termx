import { describe, expect, it } from 'vitest'
import { createLocalTerminalProtocolTransport } from './localTerminalProtocolTransport'
import { TERMX_FRAME_TYPES, decodeTermxFrame, encodeTermxFrame } from './termxProtocol'
import type { BinaryChannel, ConnectionInfo } from './transport'

describe('createLocalTerminalProtocolTransport', () => {
  it('performs hello and attach over the Go binary protocol before exposing terminal output', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const transport = createLocalTerminalProtocolTransport({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))

    const opened = transport.openTerminal('terminal-1')
    const hello = decodeSentFrame(channel, 0)
    expect(hello.channel).toBe(0)
    expect(hello.type).toBe(TERMX_FRAME_TYPES.hello)
    expect(JSON.parse(new TextDecoder().decode(hello.payload))).toMatchObject({ version: 1, client: 'termx-local-web' })
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()

    const attach = decodeSentFrame(channel, 1)
    expect(attach.type).toBe(TERMX_FRAME_TYPES.request)
    const attachRequest = JSON.parse(new TextDecoder().decode(attach.payload))
    expect(attachRequest.method).toBe('attach')
    expect(attachRequest.params).toEqual({ terminal_id: 'terminal-1', mode: 'collaborator' })
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))

    const terminal = await opened
    expect(terminal.label).toBe('terminal:terminal-1')
    channel.emitFrame(encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('stream-data')))

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({ type: 'output' })
    expect(new TextDecoder().decode((events[0] as { data: Uint8Array }).data)).toBe('stream-data')
  })

  it('maps BinaryChannel JSON input and resize messages to Go TypeInput and TypeResize frames', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const transport = createLocalTerminalProtocolTransport({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const terminalPromise = transport.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    const terminal = await terminalPromise

    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'input', data: 'echo hi\n' })))
    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'resize', cols: 100, rows: 40 })))

    const snapshotRequest = decodeSentFrame(channel, 2)
    expect(snapshotRequest).toMatchObject({ channel: 0, type: TERMX_FRAME_TYPES.request })

    const input = decodeSentFrame(channel, 3)
    expect(input).toMatchObject({ channel: 7, type: TERMX_FRAME_TYPES.input })
    expect(new TextDecoder().decode(input.payload)).toBe('echo hi\n')

    const resize = decodeSentFrame(channel, 4)
    expect(resize.channel).toBe(7)
    expect(resize.type).toBe(TERMX_FRAME_TYPES.resize)
    expect(Array.from(resize.payload)).toEqual([0x00, 0x64, 0x00, 0x28])
  })

  it('buffers stream frames that arrive before the attach response names the stream channel', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const transport = createLocalTerminalProtocolTransport({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))
    const terminalPromise = transport.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))

    channel.emitFrame(encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('early-output')))
    expect(events).toEqual([])
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    await terminalPromise

    expect(events).toHaveLength(1)
    expect(new TextDecoder().decode((events[0] as { data: Uint8Array }).data)).toBe('early-output')
  })

  it('requests a snapshot and emits text fallback through the terminal transport interface', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const transport = createLocalTerminalProtocolTransport({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const events: unknown[] = []
    transport.subscribeTerminal('terminal-1', (event) => events.push(event))
    const terminalPromise = transport.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))
    await terminalPromise

    const snapshot = decodeSentFrame(channel, 2)
    const snapshotRequest = JSON.parse(new TextDecoder().decode(snapshot.payload))
    expect(snapshotRequest.method).toBe('snapshot')
    expect(snapshotRequest.params).toEqual({
      terminal_id: 'terminal-1',
      scrollback_offset: 0,
      scrollback_limit: 500,
    })
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: snapshotRequest.id,
      result: JSON.stringify({
        terminal_id: 'terminal-1',
        size: { cols: 80, rows: 24 },
        screen: { rows: [{ cells: [{ r: 'h' }, { r: 'i' }] }] },
        scrollback: [{ cells: [{ r: 'o' }, { r: 'k' }] }],
      }),
    })))
    await Promise.resolve()

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({
      type: 'snapshot',
      snapshot: { text: 'ok\nhi', cols: 80, rows: 24 },
    })
  })

  it('rejects machine or terminal mismatch before writing protocol frames', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const transport = createLocalTerminalProtocolTransport({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })

    await expect(transport.openTerminal('terminal-2')).rejects.toThrow(/terminal-2.*terminal-1/)
    expect(channel.sent).toEqual([])
  })
})

function connectionInfo(): ConnectionInfo {
  return {
    mode: 'local',
    connectionId: 'rtc-local-1',
    machineId: 'machine-local',
    terminalId: 'terminal-1',
    relayInUse: false,
  }
}

function encodeJSON(value: unknown): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(value))
}

function decodeSentFrame(channel: MockBinaryDataChannel, index: number) {
  const sent = channel.sent[index]
  if (!sent) throw new Error(`missing frame ${index}`)
  return decodeTermxFrame(sent)
}

class MockBinaryDataChannel implements BinaryChannel {
  readyState: BinaryChannel['readyState'] = 'open'
  readonly sent: Uint8Array[] = []
  private messageHandler: ((data: Uint8Array) => void) | null = null

  constructor(readonly label: string) {}

  send(data: Uint8Array): void {
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 'closed'
  }

  onMessage(handler: (data: Uint8Array) => void): void {
    this.messageHandler = handler
  }

  emitFrame(data: Uint8Array): void {
    this.messageHandler?.(data)
  }
}
