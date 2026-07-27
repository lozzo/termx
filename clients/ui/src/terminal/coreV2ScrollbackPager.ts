import type { CoreV2HistorySource } from './coreV2HistorySource'
import type {
  CoreV2HistoryCursor,
  CoreV2HistoryRow,
  CoreV2HistoryWindow,
} from './coreV2TerminalProtocol'

export interface CoreV2ScrollbackPage {
  rows: CoreV2HistoryRow[]
  operation: 'replace' | 'prepend'
  loadedRows: number
  totalLoadedRows: number
  committedTotalRows: number
  logicalTotalRows: number
  historyGeneration: string
  firstRowId?: string | undefined
  lastRowId?: string | undefined
  hasMore: boolean
}

interface CoreV2ScrollbackState {
  cols: number
  token: string
  generation: string
  firstCursor?: CoreV2HistoryCursor | undefined
  firstLineId?: string | undefined
  lastLineId?: string | undefined
  totalLoadedRows: number
  hasMore: boolean
}

/** Bridges the legacy xterm scrollback consumer onto frozen cursor-based history windows. */
export class CoreV2ScrollbackPager {
  private readonly stateByTerminal = new Map<string, CoreV2ScrollbackState>()

  constructor(private readonly source: CoreV2HistorySource) {}

  async load(input: {
    terminalId: string
    offset: number
    limit: number
    cols: number
    signal?: AbortSignal
  }): Promise<CoreV2ScrollbackPage> {
    const current = this.stateByTerminal.get(input.terminalId)
    const loadLatest = input.offset <= 0 || !current || current.cols !== input.cols
    if (!loadLatest && !current.hasMore) {
      return emptyPage(current)
    }
    const sourceOptions = input.signal ? { signal: input.signal } : undefined

    const window = loadLatest
      ? await this.source.window({
          terminalId: input.terminalId,
          mode: 'latest',
          limit: input.limit,
          cols: input.cols,
        }, sourceOptions)
      : await this.source.window({
          terminalId: input.terminalId,
          mode: 'older',
          limit: input.limit,
          cols: input.cols,
          token: current.token,
          generation: current.generation,
          beforeCursor: requireFirstCursor(current),
          boundaryFirstLineId: current.firstLineId,
          boundaryLastLineId: current.lastLineId,
        }, sourceOptions)

    if (!window.token.trim() || !window.generation.trim()) {
      throw new Error('history scrollback requires a frozen token and generation')
    }
    if (!loadLatest && (window.token !== current.token || window.generation !== current.generation)) {
      this.stateByTerminal.delete(input.terminalId)
      throw new Error('history scrollback window changed token or generation')
    }

    const next = stateFromWindow(window, input.cols, loadLatest ? 0 : current.totalLoadedRows)
    this.stateByTerminal.set(input.terminalId, next)
    if (loadLatest && current && current.token !== next.token) {
      this.release(input.terminalId, current)
    }
    return pageFromWindow(window, next.totalLoadedRows)
  }

  forget(terminalId: string): void {
    const state = this.stateByTerminal.get(terminalId)
    this.stateByTerminal.delete(terminalId)
    if (state) this.release(terminalId, state)
  }

  private release(terminalId: string, state: CoreV2ScrollbackState): void {
    void this.source.release?.({
      terminalId,
      token: state.token,
      generation: state.generation,
    }).catch(() => undefined)
  }
}

function stateFromWindow(window: CoreV2HistoryWindow, cols: number, previouslyLoadedRows: number): CoreV2ScrollbackState {
  return {
    cols,
    token: window.token,
    generation: window.generation,
    firstCursor: cursorFromRow(window.renderRows[0]),
    firstLineId: window.firstLineId ?? window.renderRows[0]?.logicalLineId,
    lastLineId: window.lastLineId ?? window.renderRows.at(-1)?.logicalLineId,
    totalLoadedRows: previouslyLoadedRows + window.renderRows.length,
    hasMore: window.hasMore,
  }
}

function pageFromWindow(window: CoreV2HistoryWindow, totalLoadedRows: number): CoreV2ScrollbackPage {
  return {
    rows: window.renderRows,
    operation: window.op === 'prepend' ? 'prepend' : 'replace',
    loadedRows: window.renderRows.length,
    totalLoadedRows,
    committedTotalRows: window.totalRows,
    logicalTotalRows: window.logicalTotal,
    historyGeneration: window.generation,
    firstRowId: window.firstRowId,
    lastRowId: window.lastRowId,
    hasMore: window.hasMore,
  }
}

function emptyPage(state: CoreV2ScrollbackState): CoreV2ScrollbackPage {
  return {
    rows: [],
    operation: 'prepend',
    loadedRows: 0,
    totalLoadedRows: state.totalLoadedRows,
    committedTotalRows: state.totalLoadedRows,
    logicalTotalRows: state.totalLoadedRows,
    historyGeneration: state.generation,
    hasMore: false,
  }
}

function requireFirstCursor(state: CoreV2ScrollbackState): CoreV2HistoryCursor {
  if (!state.firstCursor) throw new Error('older history scrollback requires a logical first cursor')
  return state.firstCursor
}

function cursorFromRow(row: CoreV2HistoryRow | undefined): CoreV2HistoryCursor | undefined {
  if (!row?.logicalLineId || row.rowInLine === undefined) return undefined
  return { lineId: row.logicalLineId, rowInLine: row.rowInLine }
}
