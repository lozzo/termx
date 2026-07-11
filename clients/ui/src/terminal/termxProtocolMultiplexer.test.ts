import { describe, expect, it, vi } from 'vitest'
import type { RtcBinaryChannel, RtcSubscription } from '../core/transport'
import { createTermxProtocolMultiplexer } from './termxProtocolMultiplexer'
import { TERMX_FRAME_TYPES, TERMX_PROTOCOL_VERSION, decodeTermxFrame, encodeTermxFrame } from './termxProtocol'
import {
  decodeTerminalErrorPayload,
  decodeTerminalHelloPayload,
  decodeTerminalRequestPayload,
  decodeTerminalResponsePayload,
  encodeTerminalHelloPayload,
  encodeTerminalMethodResult,
  encodeTerminalRequestPayload,
  encodeTerminalResponseEnvelope,
} from './terminalWireProtocol'

describe('TermxProtocolMultiplexer', () => {
  it('uses one physical request id space and decodes daemon list results', async () => {
    const physical = new FakeBinaryChannel('protocol')
    const mux = createTermxProtocolMultiplexer(physical)

    const resultPromise = mux.request<{ terminals: Array<{ terminal_id: string }> }>('list')
    const requestFrame = decodeTermxFrame(physical.sent[0]!)
    const request = decodeTerminalRequestPayload(requestFrame.payload)
    expect(requestFrame.type).toBe(TERMX_FRAME_TYPES.request)
    expect(request.method).toBe('list')

    physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeTerminalResponseEnvelope({
      id: request.id,
      result: encodeTerminalMethodResult('list', {
        terminals: [{ terminal_id: 'terminal-1', name: 'shell', state: 'running', size: { cols: 120, rows: 36 } }],
      }),
    })))

    await expect(resultPromise).resolves.toMatchObject({
      terminals: [{ terminal_id: 'terminal-1', name: 'shell', state: 'running' }],
    })
  })

  it('projects local Hello and routes each attached stream to its virtual terminal channel', async () => {
    const physical = new FakeBinaryChannel('protocol')
    const mux = createTermxProtocolMultiplexer(physical)
    const first = await mux.openTerminalChannel('terminal-1')
    const second = await mux.openTerminalChannel('terminal-2')
    const firstFrames: Uint8Array[] = []
    const secondFrames: Uint8Array[] = []
    first.onMessage((frame) => firstFrames.push(frame))
    second.onMessage((frame) => secondFrames.push(frame))

    first.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.hello, encodeTerminalHelloPayload({
      version: TERMX_PROTOCOL_VERSION,
      client: 'test-first',
    })))
    await Promise.resolve()
    expect(decodeTerminalHelloPayload(decodeTermxFrame(firstFrames[0]!).payload).version).toBe(TERMX_PROTOCOL_VERSION)
    expect(physical.sent).toHaveLength(0)

    first.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(1, 'attach', {
      terminal_id: 'terminal-1',
      mode: 'collaborator',
    })))
    second.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(1, 'attach', {
      terminal_id: 'terminal-2',
      mode: 'collaborator',
    })))
    const firstRequest = decodeTerminalRequestPayload(decodeTermxFrame(physical.sent[0]!).payload)
    const secondRequest = decodeTerminalRequestPayload(decodeTermxFrame(physical.sent[1]!).payload)
    expect(firstRequest.id).not.toBe(secondRequest.id)

    physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeTerminalResponseEnvelope({
      id: secondRequest.id,
      result: encodeTerminalMethodResult('attach', { mode: 'collaborator', channel: 22 }),
    })))
    physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeTerminalResponseEnvelope({
      id: firstRequest.id,
      result: encodeTerminalMethodResult('attach', { mode: 'collaborator', channel: 11 }),
    })))
    physical.receive(encodeTermxFrame(22, TERMX_FRAME_TYPES.streamReady))
    physical.receive(encodeTermxFrame(11, TERMX_FRAME_TYPES.streamReady))
    await Promise.resolve()
    await Promise.resolve()

    expect(decodeTerminalResponsePayload(decodeTermxFrame(firstFrames[1]!).payload).id).toBe(1)
    expect(decodeTermxFrame(firstFrames[2]!).channel).toBe(11)
    expect(decodeTerminalResponsePayload(decodeTermxFrame(secondFrames[0]!).payload).id).toBe(1)
    expect(decodeTermxFrame(secondFrames[1]!).channel).toBe(22)
  })

  it('removes a virtual request when the physical send fails', async () => {
    const physical = new FakeBinaryChannel('protocol')
    const mux = createTermxProtocolMultiplexer(physical)
    const virtual = await mux.openTerminalChannel('terminal-1')
    const received: Uint8Array[] = []
    virtual.onMessage((frame) => received.push(frame))
    physical.sendFailure = new Error('physical send failed')

    expect(() => virtual.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(7, 'attach', {
      terminal_id: 'terminal-1',
      mode: 'collaborator',
    })))).toThrow('physical send failed')

    physical.sendFailure = null
    physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeTerminalResponseEnvelope({
      id: 1,
      result: encodeTerminalMethodResult('attach', { mode: 'collaborator', channel: 11 }),
    })))
    await Promise.resolve()
    expect(received).toHaveLength(0)
  })

  it('expires unanswered virtual requests without retaining their physical ids', async () => {
    vi.useFakeTimers()
    try {
      const physical = new FakeBinaryChannel('protocol')
      const mux = createTermxProtocolMultiplexer(physical)
      const virtual = await mux.openTerminalChannel('terminal-1')
      const received: Uint8Array[] = []
      virtual.onMessage((frame) => received.push(frame))

      virtual.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(9, 'attach', {
        terminal_id: 'terminal-1',
        mode: 'collaborator',
      })))
      await vi.advanceTimersByTimeAsync(10_000)
      await Promise.resolve()

      const timeout = decodeTerminalResponseError(received[0]!)
      expect(timeout).toMatchObject({ id: 9, code: 503 })
      physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeTerminalResponseEnvelope({
        id: 1,
        result: encodeTerminalMethodResult('attach', { mode: 'collaborator', channel: 11 }),
      })))
      await Promise.resolve()
      expect(received).toHaveLength(1)
      mux.close()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps one-shot live invalidation requests pending beyond the ordinary request timeout', async () => {
    vi.useFakeTimers()
    try {
      const physical = new FakeBinaryChannel('protocol')
      const mux = createTermxProtocolMultiplexer(physical)
      const virtual = await mux.openTerminalChannel('terminal-1')
      const received: Uint8Array[] = []
      virtual.onMessage((frame) => received.push(frame))

      virtual.send(encodeTermxFrame(0, TERMX_FRAME_TYPES.request, encodeTerminalRequestPayload(12, 'live.invalidation.next', {
        terminal_id: 'terminal-1',
        observed_revision: 7,
      })))
      const physicalRequest = decodeTerminalRequestPayload(decodeTermxFrame(physical.sent[0]!).payload)
      await vi.advanceTimersByTimeAsync(30_000)
      expect(received).toEqual([])

      physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.responseBinary, encodeTerminalResponseEnvelope({
        id: physicalRequest.id,
        result: encodeTerminalMethodResult('live.invalidation.next', {
          type: 7,
          terminal_id: 'terminal-1',
          live_revision: 8,
        }),
      })))
      await Promise.resolve()

      const response = decodeTerminalResponsePayload(decodeTermxFrame(received[0]!).payload)
      expect(response.id).toBe(12)
      mux.close()
    } finally {
      vi.useRealTimers()
    }
  })

  it('fails closed when the daemon sends a malformed control envelope', async () => {
    const physical = new FakeBinaryChannel('protocol')
    const mux = createTermxProtocolMultiplexer(physical)
    const pending = mux.request('list')

    physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, new Uint8Array([0xff])))

    await expect(pending).rejects.toThrow('invalid termx protocol control frame')
    expect(physical.closed).toBe(true)
  })

  it('fails closed when a valid response envelope contains a malformed method result', async () => {
    const physical = new FakeBinaryChannel('protocol')
    const mux = createTermxProtocolMultiplexer(physical)
    const pending = mux.request('list')
    const request = decodeTerminalRequestPayload(decodeTermxFrame(physical.sent[0]!).payload)

    physical.receive(encodeTermxFrame(0, TERMX_FRAME_TYPES.response, encodeTerminalResponseEnvelope({
      id: request.id,
      result: new Uint8Array([0xff]),
    })))

    await expect(pending).rejects.toThrow()
    expect(physical.closed).toBe(true)
  })
})

function decodeTerminalResponseError(frame: Uint8Array): { id: number; code: number; message: string } {
  const decoded = decodeTermxFrame(frame)
  expect(decoded.type).toBe(TERMX_FRAME_TYPES.error)
  return decodeTerminalErrorPayload(decoded.payload)
}

class FakeBinaryChannel implements RtcBinaryChannel {
  readonly sent: Uint8Array[] = []
  readonly readyState = 'open' as const
  sendFailure: Error | null = null
  closed = false
  private readonly messageHandlers = new Set<(data: Uint8Array) => void>()
  private readonly closeHandlers = new Set<() => void>()

  constructor(readonly label: string) {}

  send(data: Uint8Array): void {
    if (this.sendFailure) throw this.sendFailure
    this.sent.push(data)
  }

  close(): void {
    this.closed = true
    for (const handler of this.closeHandlers) handler()
  }

  onMessage(handler: (data: Uint8Array) => void): RtcSubscription {
    this.messageHandlers.add(handler)
    return { close: () => this.messageHandlers.delete(handler) }
  }

  onClose(handler: () => void): RtcSubscription {
    this.closeHandlers.add(handler)
    return { close: () => this.closeHandlers.delete(handler) }
  }

  async waitOpen(): Promise<void> {}

  receive(data: Uint8Array): void {
    for (const handler of this.messageHandlers) handler(data)
  }
}
