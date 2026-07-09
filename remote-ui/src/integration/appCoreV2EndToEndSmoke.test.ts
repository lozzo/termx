import { describe, expect, it } from 'vitest'
import { createTerminalManagementApi } from '../terminal/terminalManagementApi'
import { createTerminalProtocolClient } from '../terminal/terminalProtocolClient'
import { createCoreV2HistorySource } from '../terminal/coreV2HistorySource'
import { createCoreV2HistorySurface } from '../terminal/coreV2HistorySurface'
import {
  copyHistorySelection,
  selectionFromSurfaceRows,
} from '../terminal/coreV2HistoryInteraction'
import { MockRtcTerminalSession } from '../test/mockRtcTerminalSession'
import type { ConnectionPath, RtcJsonRpcChannel } from '../core/transport'
import type { CoreV2HistoryWindowParams } from '../terminal/coreV2TerminalProtocol'

describe('App core-v2 end-to-end smoke', () => {
  it('connects, creates, attaches, writes, rolls back history, and copies through one core-v2 session', async () => {
    const session = new AppCoreV2SmokeSession('machine-local', 'local')
    session.emitResizeControl('terminal-1', { canResize: true, reason: 'owner' })
    session.setTerminalSnapshot('terminal-1', { text: 'ready', cols: 100, rows: 30 })

    const management = createTerminalManagementApi(session, 'machine-local')
    const created = await management.createTerminal({
      name: 'smoke',
      command: ['/bin/zsh', '-l'],
      cwd: '/work',
      environment: 'TERM=xterm-256color',
      sizeLockMode: 'off',
    })

    const rawChannel = await session.openTerminal(created.terminalId)
    const protocol = createTerminalProtocolClient({
      channel: rawChannel,
      machineId: 'machine-local',
      terminalId: created.terminalId,
      connectionInfo: await session.getConnectionInfo(),
      resizePolicy: 'owner',
      surfaceId: `app:machine-local:terminal:${created.terminalId}`,
      handshakeTimeoutMs: 200,
    })
    const terminal = await protocol.openTerminal(created.terminalId)
    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'input', data: 'printf smoke\n' })))
    terminal.send(new TextEncoder().encode(JSON.stringify({ type: 'resize', cols: 100, rows: 30 })))

    const historySource = createCoreV2HistorySource(session, 'machine-local')
    const surface = createCoreV2HistorySurface(historySource, {
      terminalId: created.terminalId,
      cols: 100,
      viewportRows: 2,
      requestRows: 2,
      renderOverscanRows: 1,
    })
    const latest = await surface.loadLatest()
    expect(latest.rows.map(rowText)).toEqual(['beta', 'gamma'])
    const older = await surface.loadOlder(1)
    expect(older.rows.map(rowText)).toEqual(['alpha', 'beta', 'gamma'])

    const selection = selectionFromSurfaceRows(
      older,
      { lineId: '41', rowInLine: 0, col: 2 },
      { lineId: '43', rowInLine: 0, col: 3 },
    )
    const copied = await copyHistorySelection(historySource, older, selection)

    expect(copied).toBe('pha\nbeta\ngam')
    expect(session.openedTerminalIds).toEqual(['terminal-1'])
    expect(session.sentText('terminal-1')).toBe('printf smoke\n')
    expect(session.sentResize('terminal-1')).toEqual({ cols: 100, rows: 30 })
    expect(session.historyReplayRequests('terminal-1')).toEqual([])
    expect(session.apiRequests).toEqual([
      {
        method: 'create',
        params: expect.objectContaining({
          command: ['/bin/zsh', '-l'],
          name: 'smoke',
          dir: '/work',
          env: ['TERM=xterm-256color'],
          tags: { 'termx.size_lock': 'off', cwd: '/work', environment: 'TERM=xterm-256color' },
        }),
      },
      {
        method: 'history.window',
        params: expect.objectContaining({
          terminal_id: 'terminal-1',
          token: '',
          cursor_valid: false,
          limit: 2,
          cols: 100,
        }),
      },
      {
        method: 'history.window',
        params: expect.objectContaining({
          terminal_id: 'terminal-1',
          token: 'token-smoke',
          history_generation: 9,
          cursor_valid: true,
          before_line_id: 42,
          before_row_in_line: 0,
          boundary_first_line_id: 42,
          boundary_last_line_id: 43,
        }),
      },
      {
        method: 'history.copy',
        params: expect.objectContaining({
          terminal_id: 'terminal-1',
          token: 'token-smoke',
          history_generation: 9,
          boundary_first_line_id: 41,
          boundary_last_line_id: 43,
          range_valid: true,
          range_start_line_id: 41,
          range_start_col: 2,
          range_end_line_id: 43,
          range_end_col: 3,
        }),
      },
    ])
  })
})

class AppCoreV2SmokeSession extends MockRtcTerminalSession {
  readonly apiRequests: Array<{ method: string; params: unknown }> = []
  private readonly copyText = 'pha\nbeta\ngam'

  constructor(machineId: string, path: ConnectionPath) {
    super(machineId, path)
  }

  override async openApi(): Promise<RtcJsonRpcChannel> {
    return {
      request: async <TResponse>(method: string, params?: unknown): Promise<TResponse> => {
        const requestParams = params ?? {}
        this.apiRequests.push({ method, params: requestParams })
        if (method === 'create') {
          return { terminal_id: 'terminal-1', state: 'running' } as TResponse
        }
        if (method === 'history.window') {
          const historyParams = requestParams as CoreV2HistoryWindowParams
          return (historyParams.cursor_valid ? olderHistoryWindow() : latestHistoryWindow()) as TResponse
        }
        if (method === 'history.copy') {
          return { bytes: new TextEncoder().encode(this.copyText), method } as TResponse
        }
        throw new Error(`unexpected api method ${method}`)
      },
      close() {},
    }
  }
}

function latestHistoryWindow() {
  return historyWindowPayload({
    op: 'replace',
    startLine: 42,
    texts: ['beta', 'gamma'],
    hasMore: true,
  })
}

function olderHistoryWindow() {
  return historyWindowPayload({
    op: 'prepend',
    startLine: 41,
    texts: ['alpha'],
    hasMore: false,
  })
}

function historyWindowPayload(input: {
  op: 'replace' | 'prepend'
  startLine: number
  texts: string[]
  hasMore: boolean
}) {
  return {
    terminal_id: 'terminal-1',
    token: 'token-smoke',
    op: input.op,
    size: { cols: 100, rows: 30 },
    rows: input.texts.map((text) => ({
      cells: [{ r: text, w: text.length }],
    })),
    row_logical_line_ids: input.texts.map((_, index) => input.startLine + index),
    row_in_line: input.texts.map(() => 0),
    line_start_rows: input.texts.map((_, index) => index),
    line_end_rows: input.texts.map((_, index) => index),
    line_logical_line_ids: input.texts.map((_, index) => input.startLine + index),
    line_clipped_before: input.texts.map(() => false),
    line_clipped_after: input.texts.map(() => false),
    before_offset: input.startLine,
    loaded_rows: input.texts.length,
    total_rows: input.startLine + input.texts.length,
    loaded_lines: input.texts.length,
    logical_total: input.startLine + input.texts.length,
    has_more: input.hasMore,
    history_generation: 9,
    first_line_id: input.startLine,
    last_line_id: input.startLine + input.texts.length - 1,
    cursor_valid: true,
    cursor_before_line_id: input.startLine,
    cursor_before_row_in_line: 0,
  }
}

function rowText(row: { cells: Array<{ text: string }> }): string {
  return row.cells.map((cell) => cell.text).join('')
}
