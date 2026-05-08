import { describe, expect, it, vi } from 'vitest'
import { createTerminalProtocolClient } from './terminalProtocolClient'
import { TERMX_FRAME_TYPES, decodeTermxFrame, encodeTermxFrame } from './termxProtocol'
import type { ConnectionInfo, RtcBinaryChannel } from './transport'
import type { TerminalProtocolEvent } from './terminalClient'

describe('TerminalProtocolClient', () => {
  it('performs hello and attach over the Go binary protocol before exposing terminal output', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const events: unknown[] = []
    client.subscribeTerminal('terminal-1', (event: TerminalProtocolEvent) => events.push(event))

    const opened = client.openTerminal('terminal-1')
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
    expect(attachRequest.params).toEqual({ terminal_id: 'terminal-1', mode: 'collaborator', resize_policy: 'owner' })
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({ mode: 'collaborator', channel: 7 }),
    })))

    const terminal = await opened
    expect(terminal.label).toBe('terminal:terminal-1')
    channel.emitFrame(encodeTermxFrame(7, TERMX_FRAME_TYPES.output, new TextEncoder().encode('stream-data')))

    expect(events).toHaveLength(2)
    expect(events[0]).toMatchObject({ type: 'resizeControl', control: { canResize: true, reason: 'owner' } })
    expect(events[1]).toMatchObject({ type: 'output' })
    expect(new TextDecoder().decode((events[1] as { data: Uint8Array }).data)).toBe('stream-data')
  })

  it('maps BinaryChannel JSON input and resize messages to Go TypeInput and TypeResize frames', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
      resizePolicy: 'owner',
    })
    const terminalPromise = client.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({
        mode: 'collaborator',
        channel: 7,
        resize_control: { can_resize: true, reason: 'owner' },
      }),
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

  it('ensures resize ownership and target dimensions before forwarding sized input', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
      resizePolicy: 'owner',
    })
    const events: unknown[] = []
    client.subscribeTerminal('terminal-1', (event: TerminalProtocolEvent) => events.push(event))
    const terminalPromise = client.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({
        mode: 'collaborator',
        channel: 7,
        resize_control: { can_resize: true, reason: 'owner' },
      }),
    })))
    const terminal = await terminalPromise

    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'input', data: 'a', cols: 120, rows: 40 })))
    const ensureResize = decodeSentFrame(channel, 3)
    expect(ensureResize).toMatchObject({ channel: 0, type: TERMX_FRAME_TYPES.request })
    const ensureResizeRequest = JSON.parse(new TextDecoder().decode(ensureResize.payload))
    expect(ensureResizeRequest.method).toBe('ensure_resize')
    expect(ensureResizeRequest.params).toEqual({
      terminal_id: 'terminal-1',
      channel: 7,
      cols: 120,
      rows: 40,
      resize_policy: 'owner',
    })
    expect(channel.sent).toHaveLength(4)

    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: ensureResizeRequest.id,
      result: JSON.stringify({
        resize_control: { can_resize: true, reason: 'owner' },
        size: { cols: 120, rows: 40 },
        resized: true,
      }),
    })))
    await vi.waitFor(() => expect(channel.sent).toHaveLength(5))

    const input = decodeSentFrame(channel, 4)
    expect(input).toMatchObject({ channel: 7, type: TERMX_FRAME_TYPES.input })
    expect(new TextDecoder().decode(input.payload)).toBe('a')
    expect(events).toContainEqual({
      type: 'resizeControl',
      control: { canResize: true, reason: 'owner' },
    })
  })

  it('refreshes resize control for sized input even when size lock is already known', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
      resizePolicy: 'owner',
    })
    const terminalPromise = client.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({
        mode: 'collaborator',
        channel: 7,
        resize_control: { can_resize: false, reason: 'size_locked', size_locked: true },
      }),
    })))
    const terminal = await terminalPromise

    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'input', data: 'a', cols: 120, rows: 40 })))
    const ensureResize = decodeSentFrame(channel, 3)
    expect(ensureResize).toMatchObject({ channel: 0, type: TERMX_FRAME_TYPES.request })
    const ensureResizeRequest = JSON.parse(new TextDecoder().decode(ensureResize.payload))
    expect(ensureResizeRequest.method).toBe('ensure_resize')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: ensureResizeRequest.id,
      result: JSON.stringify({
        resize_control: { can_resize: false, reason: 'size_locked', size_locked: true },
        size: { cols: 80, rows: 24 },
      }),
    })))
    await vi.waitFor(() => expect(channel.sent).toHaveLength(5))

    expect(decodeSentFrame(channel, 4)).toMatchObject({ channel: 7, type: TERMX_FRAME_TYPES.input })
  })

  it('suppresses resize frames when attach grants follower resize control', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
      resizePolicy: 'follower',
    })
    const events: unknown[] = []
    client.subscribeTerminal('terminal-1', (event: TerminalProtocolEvent) => events.push(event))
    const terminalPromise = client.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({
        mode: 'collaborator',
        channel: 7,
        resize_control: { can_resize: false, reason: 'follower' },
      }),
    })))
    const terminal = await terminalPromise

    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'resize', cols: 100, rows: 40 })))

    expect(channel.sent).toHaveLength(3)
    expect(events).toContainEqual({
      type: 'resizeControl',
      control: { canResize: false, reason: 'follower' },
    })
  })

  it('emits resize control and forwards resize when attach grants owner control', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
      resizePolicy: 'owner',
    })
    const events: unknown[] = []
    client.subscribeTerminal('terminal-1', (event: TerminalProtocolEvent) => events.push(event))
    const terminalPromise = client.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    const attachRequest = JSON.parse(new TextDecoder().decode(decodeSentFrame(channel, 1).payload))
    expect(attachRequest.params).toMatchObject({ resize_policy: 'owner' })
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeJSON({
      id: attachRequest.id,
      result: JSON.stringify({
        mode: 'collaborator',
        channel: 7,
        resize_control: { can_resize: true, reason: 'owner' },
      }),
    })))
    const terminal = await terminalPromise

    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'resize', cols: 100, rows: 40 })))

    const resize = decodeSentFrame(channel, 3)
    expect(resize.channel).toBe(7)
    expect(resize.type).toBe(TERMX_FRAME_TYPES.resize)
    expect(events).toContainEqual({
      type: 'resizeControl',
      control: { canResize: true, reason: 'owner' },
    })
  })

  it('buffers stream frames that arrive before the attach response names the stream channel', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const events: unknown[] = []
    client.subscribeTerminal('terminal-1', (event: TerminalProtocolEvent) => events.push(event))
    const terminalPromise = client.openTerminal('terminal-1')
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

    expect(events).toHaveLength(2)
    expect(events[0]).toMatchObject({ type: 'resizeControl' })
    expect(new TextDecoder().decode((events[1] as { data: Uint8Array }).data)).toBe('early-output')
  })

  it('requests a snapshot and emits replayable snapshot content through the terminal protocol interface', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })
    const events: unknown[] = []
    client.subscribeTerminal('terminal-1', (event: TerminalProtocolEvent) => events.push(event))
    const terminalPromise = client.openTerminal('terminal-1')
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

    expect(events).toHaveLength(2)
    expect(events[0]).toMatchObject({ type: 'resizeControl' })
    expect(events[1]).toMatchObject({
      type: 'snapshot',
      snapshot: { text: 'ok\nhi', cols: 80, rows: 24 },
    })
    expect((events[1] as { snapshot: { replay?: string } }).snapshot.replay).toContain('\x1b[H\x1b[2J\x1b[H')
    expect((events[1] as { snapshot: { replay?: string } }).snapshot.replay).toContain('\x1b[1;1H')
  })

  it('rejects machine or terminal mismatch before writing protocol frames', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })

    await expect(client.openTerminal('terminal-2')).rejects.toThrow(/terminal-2.*terminal-1/)
    expect(channel.sent).toEqual([])
  })

  it('rejects pending handshake requests when the channel closes before attach completes', async () => {
    const channel = new MockBinaryDataChannel('terminal:terminal-1')
    const client = createTerminalProtocolClient({
      channel,
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      connectionInfo: connectionInfo(),
    })

    const opened = client.openTerminal('terminal-1')
    channel.emitFrame(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeJSON({ version: 1, server: 'termx' })))
    await Promise.resolve()
    expect(decodeSentFrame(channel, 1).type).toBe(TERMX_FRAME_TYPES.request)
    channel.close()

    await expect(opened).rejects.toThrow(/closed/i)
  })
})

function connectionInfo(): ConnectionInfo {
  return {
    path: 'local',
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

class MockBinaryDataChannel implements RtcBinaryChannel {
  readyState: RtcBinaryChannel['readyState'] = 'open'
  readonly sent: Uint8Array[] = []
  private messageHandler: ((data: Uint8Array) => void) | null = null
  private closeHandler: (() => void) | null = null

  constructor(readonly label: string) {}

  send(data: Uint8Array): void {
    this.sent.push(data)
  }

  close(): void {
    this.readyState = 'closed'
    this.closeHandler?.()
  }

  onMessage(handler: (data: Uint8Array) => void) {
    this.messageHandler = handler
    return { close: () => { this.messageHandler = null } }
  }

  onClose(handler: () => void) {
    this.closeHandler = handler
    return { close: () => { this.closeHandler = null } }
  }

  waitOpen(): Promise<void> {
    return Promise.resolve()
  }

  emitFrame(data: Uint8Array): void {
    this.messageHandler?.(data)
  }
}
