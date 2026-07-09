export const CORE_V2_TERMINAL_METHODS = {
  attach: 'attach',
  ensureResize: 'ensure_resize',
  snapshot: 'snapshot',
  historyWindow: 'history.window',
  historyCopy: 'history.copy',
  historyRelease: 'history.release',
} as const

export const CORE_V2_HISTORY_WINDOW_OPS = ['replace', 'prepend', 'append'] as const
export type CoreV2HistoryWindowOp = typeof CORE_V2_HISTORY_WINDOW_OPS[number]

export const CORE_V2_HISTORY_WINDOW_MODES = ['latest', 'older', 'newer', 'oldest', 'range'] as const
export type CoreV2HistoryWindowMode = typeof CORE_V2_HISTORY_WINDOW_MODES[number]

export interface CoreV2HistoryCursor {
  lineId: string
  rowInLine: number
}

export interface CoreV2HistoryRange {
  startLineId: string
  startCol: number
  endLineId: string
  endCol: number
}

export interface CoreV2HistoryWindowRequest {
  terminalId: string
  mode: CoreV2HistoryWindowMode
  limit: number
  cols: number
  token?: string | undefined
  generation?: string | number | bigint | undefined
  beforeOffset?: number | undefined
  beforeCursor?: CoreV2HistoryCursor | undefined
  afterCursor?: CoreV2HistoryCursor | undefined
  boundaryFirstLineId?: string | undefined
  boundaryLastLineId?: string | undefined
  range?: CoreV2HistoryRange | undefined
}

export interface CoreV2HistoryWindowParams {
  terminal_id: string
  before_offset: number
  limit: number
  cols: number
  mode: string
  token: string
  history_generation: number
  cursor_valid: boolean
  before_line_id: number
  before_row_in_line: number
  after_cursor_valid: boolean
  after_line_id: number
  after_row_in_line: number
  boundary_first_line_id: number
  boundary_last_line_id: number
  range_valid: boolean
  range_start_line_id: number
  range_start_col: number
  range_end_line_id: number
  range_end_col: number
}

export interface CoreV2HistoryCellStyle {
  fg?: string | undefined
  bg?: string | undefined
  bold?: boolean | undefined
  italic?: boolean | undefined
  underline?: boolean | undefined
  blink?: boolean | undefined
  reverse?: boolean | undefined
  strikethrough?: boolean | undefined
}

export interface CoreV2HistoryCell {
  text: string
  width: number
  style?: CoreV2HistoryCellStyle | undefined
  linkUrl?: string | undefined
  linkParams?: string | undefined
}

export interface CoreV2HistoryRow {
  index: number
  cells: CoreV2HistoryCell[]
  kind?: string | undefined
  wrapped?: boolean | undefined
  ownership?: string | undefined
  timestampUnixMs?: number | undefined
  logicalLineId?: string | undefined
  rowInLine?: number | undefined
}

export interface CoreV2HistoryLineSpan {
  startRow: number
  endRow: number
  rowKind?: string | undefined
  logicalLineId: string
  timestampStartUnixMs?: number | undefined
  timestampEndUnixMs?: number | undefined
  clippedBefore: boolean
  clippedAfter: boolean
}

export interface CoreV2HistoryWindow {
  terminalId: string
  token: string
  op: CoreV2HistoryWindowOp
  cols: number
  rows: number
  renderRows: CoreV2HistoryRow[]
  lines: CoreV2HistoryLineSpan[]
  beforeOffset: number
  loadedRows: number
  totalRows: number
  loadedLines: number
  logicalTotal: number
  hasMore: boolean
  generation: string
  firstRowId?: string | undefined
  lastRowId?: string | undefined
  firstLineId?: string | undefined
  lastLineId?: string | undefined
  cursor?: CoreV2HistoryCursor | undefined
  timestampUnixMs?: number | undefined
}

export interface CoreV2HistoryCopyRequest {
  terminalId: string
  token: string
  cols: number
  generation?: string | number | bigint | undefined
  boundaryFirstLineId?: string | undefined
  boundaryLastLineId?: string | undefined
  range: CoreV2HistoryRange
}

export interface CoreV2HistoryReleaseRequest {
  terminalId: string
  token: string
}

export interface CoreV2TerminalProtocolRequest {
  method: typeof CORE_V2_TERMINAL_METHODS.historyWindow | typeof CORE_V2_TERMINAL_METHODS.historyCopy | typeof CORE_V2_TERMINAL_METHODS.historyRelease
  params: CoreV2HistoryWindowParams
}

export interface CoreV2TerminalProtocolEvent {
  type: 'terminal.created' | 'terminal.state_changed' | 'terminal.resized' | 'terminal.removed' | 'terminal.metadata_changed' | 'storage.changed' | 'terminal.read_error' | 'unknown'
  protocolType: number
  terminalId?: string | undefined
  timestampUnixMs?: number | undefined
  payload?: unknown
}

export function coreV2HistoryWindowRequestToParams(request: CoreV2HistoryWindowRequest): CoreV2HistoryWindowParams {
  assertHistoryWindowRequest(request)
  const beforeCursor = request.beforeCursor
  const afterCursor = request.afterCursor
  const range = request.range
  return {
    terminal_id: request.terminalId,
    before_offset: nonNegativeInt(request.beforeOffset),
    limit: positiveInt(request.limit),
    cols: positiveInt(request.cols),
    mode: request.mode === 'latest' || request.mode === 'older' ? '' : request.mode,
    token: request.token ?? '',
    history_generation: numberFromID(request.generation),
    cursor_valid: Boolean(beforeCursor),
    before_line_id: beforeCursor ? numberFromID(beforeCursor.lineId) : 0,
    before_row_in_line: beforeCursor ? nonNegativeInt(beforeCursor.rowInLine) : 0,
    after_cursor_valid: Boolean(afterCursor),
    after_line_id: afterCursor ? numberFromID(afterCursor.lineId) : 0,
    after_row_in_line: afterCursor ? nonNegativeInt(afterCursor.rowInLine) : 0,
    boundary_first_line_id: numberFromID(request.boundaryFirstLineId),
    boundary_last_line_id: numberFromID(request.boundaryLastLineId),
    range_valid: Boolean(range),
    range_start_line_id: range ? numberFromID(range.startLineId) : 0,
    range_start_col: range ? nonNegativeInt(range.startCol) : 0,
    range_end_line_id: range ? numberFromID(range.endLineId) : 0,
    range_end_col: range ? nonNegativeInt(range.endCol) : 0,
  }
}

export function coreV2HistoryCopyRequestToProtocolRequest(request: CoreV2HistoryCopyRequest): CoreV2TerminalProtocolRequest {
  if (!request.token) throw new Error('history.copy requires a frozen history token')
  return {
    method: CORE_V2_TERMINAL_METHODS.historyCopy,
    params: coreV2HistoryWindowRequestToParams({
      terminalId: request.terminalId,
      mode: 'range',
      limit: 1,
      cols: request.cols,
      token: request.token,
      generation: request.generation,
      boundaryFirstLineId: request.boundaryFirstLineId,
      boundaryLastLineId: request.boundaryLastLineId,
      range: request.range,
    }),
  }
}

export function coreV2HistoryReleaseRequestToProtocolRequest(request: CoreV2HistoryReleaseRequest): CoreV2TerminalProtocolRequest {
  if (!request.terminalId.trim()) throw new Error('history.release requires terminalId')
  if (!request.token.trim()) throw new Error('history.release requires token')
  return {
    method: CORE_V2_TERMINAL_METHODS.historyRelease,
    params: coreV2HistoryWindowRequestToParams({
      terminalId: request.terminalId,
      mode: 'latest',
      limit: 1,
      cols: 1,
      token: request.token,
    }),
  }
}

export function coreV2HistoryWindowFromAPI(value: unknown): CoreV2HistoryWindow {
  const record = asRecord(value)
  const cols = numberValue(asRecord(record.size).cols)
  const rows = numberValue(asRecord(record.size).rows)
  const renderRows = recordArray(record.rows).map((row, index) => historyRowFromAPI(row, index, record))
  return {
    terminalId: stringValue(record.terminal_id ?? record.terminalId),
    token: stringValue(record.token),
    op: historyWindowOp(stringValue(record.op)),
    cols,
    rows,
    renderRows,
    lines: historyLinesFromAPI(record),
    beforeOffset: numberValue(record.before_offset ?? record.beforeOffset),
    loadedRows: numberValue(record.loaded_rows ?? record.loadedRows),
    totalRows: numberValue(record.total_rows ?? record.totalRows),
    loadedLines: numberValue(record.loaded_lines ?? record.loadedLines),
    logicalTotal: numberValue(record.logical_total ?? record.logicalTotal),
    hasMore: booleanValue(record.has_more ?? record.hasMore),
    generation: idString(record.history_generation ?? record.historyGeneration),
    firstRowId: optionalID(record.first_row_id ?? record.firstRowId),
    lastRowId: optionalID(record.last_row_id ?? record.lastRowId),
    firstLineId: optionalID(record.first_line_id ?? record.firstLineId),
    lastLineId: optionalID(record.last_line_id ?? record.lastLineId),
    cursor: booleanValue(record.cursor_valid ?? record.cursorValid)
      ? {
          lineId: idString(record.cursor_before_line_id ?? record.cursorBeforeLineId),
          rowInLine: numberValue(record.cursor_before_row_in_line ?? record.cursorBeforeRowInLine),
        }
      : undefined,
    timestampUnixMs: unixNanoToMs(record.timestamp_unix_nano ?? record.timestampUnixNano),
  }
}

export function coreV2EventFromRuntimeEvent(event: {
  protocolType?: number | undefined
  protocol_type?: number | undefined
  terminalId?: string | undefined
  terminal_id?: string | undefined
  timestampUnixNano?: bigint | number | undefined
  timestamp_unix_nano?: bigint | number | undefined
  payload?: unknown
}): CoreV2TerminalProtocolEvent {
  const protocolType = numberValue(event.protocolType ?? event.protocol_type)
  return {
    type: eventTypeName(protocolType),
    protocolType,
    terminalId: optionalString(event.terminalId ?? event.terminal_id),
    timestampUnixMs: unixNanoToMs(event.timestampUnixNano ?? event.timestamp_unix_nano),
    payload: event.payload,
  }
}

export function assertLiveCacheOnlyAPIName(name: string): void {
  if (/snapshot|scrollback|historyReplay|loadScrollback|xterm/i.test(name)) {
    throw new Error(`${name} is a live display cache API; App copy/history must use core-v2 history.window/history.copy`)
  }
}

function assertHistoryWindowRequest(request: CoreV2HistoryWindowRequest): void {
  if (!request.terminalId.trim()) throw new Error('history.window requires terminalId')
  if (!CORE_V2_HISTORY_WINDOW_MODES.includes(request.mode)) throw new Error(`invalid history.window mode ${request.mode}`)
  if (!Number.isFinite(request.limit) || request.limit <= 0) throw new Error('history.window requires positive limit')
  if (!Number.isFinite(request.cols) || request.cols <= 0) throw new Error('history.window requires positive cols')
  if (request.mode === 'older' && (!request.token || !request.beforeCursor)) {
    throw new Error('older history.window requires token and beforeCursor')
  }
  if (request.mode === 'newer' && (!request.token || !request.afterCursor)) {
    throw new Error('newer history.window requires token and afterCursor')
  }
  if (request.mode === 'oldest' && !request.token) {
    throw new Error('oldest history.window requires token')
  }
  if (request.mode === 'range' && !request.range) {
    throw new Error('range history.window requires logical range')
  }
}

function historyRowFromAPI(row: Record<string, unknown>, index: number, owner: Record<string, unknown>): CoreV2HistoryRow {
  const rowLineIds = arrayValue(owner.row_logical_line_ids ?? owner.rowLogicalLineIds)
  const rowInLine = arrayValue(owner.row_in_line ?? owner.rowInLine)
  return {
    index,
    cells: recordArray(row.cells).map(historyCellFromAPI),
    kind: optionalString(row.kind ?? row.row_kind ?? row.rowKind),
    wrapped: optionalBool(row.wrapped),
    ownership: optionalString(arrayValue(owner.row_ownership ?? owner.rowOwnership)[index]),
    timestampUnixMs: unixNanoToMs(arrayValue(owner.row_timestamps_unix_nano ?? owner.rowTimestampsUnixNano)[index] ?? row.timestamp_unix_nano ?? row.timestampUnixNano),
    logicalLineId: optionalID(rowLineIds[index]),
    rowInLine: optionalNumber(rowInLine[index]),
  }
}

function historyCellFromAPI(cell: Record<string, unknown>): CoreV2HistoryCell {
  const styleRecord = asRecord(cell.style ?? cell.s)
  return {
    text: stringValue(cell.content ?? cell.r ?? cell.text),
    width: numberValue(cell.width ?? cell.w) || 1,
    style: cleanStyle({
      fg: optionalString(styleRecord.fg),
      bg: optionalString(styleRecord.bg),
      bold: optionalBool(styleRecord.bold ?? styleRecord.b),
      italic: optionalBool(styleRecord.italic ?? styleRecord.i),
      underline: optionalBool(styleRecord.underline ?? styleRecord.u),
      blink: optionalBool(styleRecord.blink),
      reverse: optionalBool(styleRecord.reverse),
      strikethrough: optionalBool(styleRecord.strikethrough),
    }),
    linkUrl: optionalString(cell.link_url ?? cell.linkUrl),
    linkParams: optionalString(cell.link_params ?? cell.linkParams),
  }
}

function historyLinesFromAPI(record: Record<string, unknown>): CoreV2HistoryLineSpan[] {
  const starts = arrayValue(record.line_start_rows ?? record.lineStartRows)
  const ends = arrayValue(record.line_end_rows ?? record.lineEndRows)
  const kinds = arrayValue(record.line_row_kinds ?? record.lineRowKinds)
  const ids = arrayValue(record.line_logical_line_ids ?? record.lineLogicalLineIds)
  const clippedBefore = arrayValue(record.line_clipped_before ?? record.lineClippedBefore)
  const clippedAfter = arrayValue(record.line_clipped_after ?? record.lineClippedAfter)
  const timestampStart = arrayValue(record.line_timestamp_start_unix_nano ?? record.lineTimestampStartUnixNano)
  const timestampEnd = arrayValue(record.line_timestamp_end_unix_nano ?? record.lineTimestampEndUnixNano)
  const count = Math.max(starts.length, ends.length, ids.length)
  const lines: CoreV2HistoryLineSpan[] = []
  for (let index = 0; index < count; index += 1) {
    lines.push({
      startRow: numberValue(starts[index]),
      endRow: numberValue(ends[index]),
      rowKind: optionalString(kinds[index]),
      logicalLineId: idString(ids[index]),
      timestampStartUnixMs: unixNanoToMs(timestampStart[index]),
      timestampEndUnixMs: unixNanoToMs(timestampEnd[index]),
      clippedBefore: booleanValue(clippedBefore[index]),
      clippedAfter: booleanValue(clippedAfter[index]),
    })
  }
  return lines
}

function eventTypeName(protocolType: number): CoreV2TerminalProtocolEvent['type'] {
  switch (protocolType) {
    case 1:
      return 'terminal.created'
    case 2:
      return 'terminal.state_changed'
    case 3:
      return 'terminal.resized'
    case 4:
      return 'terminal.removed'
    case 5:
      return 'terminal.read_error'
    case 10:
      return 'terminal.metadata_changed'
    case 12:
      return 'storage.changed'
    default:
      return 'unknown'
  }
}

function historyWindowOp(value: string): CoreV2HistoryWindowOp {
  if (value === 'prepend' || value === 'append' || value === 'replace') return value
  return 'replace'
}

function cleanStyle(style: CoreV2HistoryCellStyle): CoreV2HistoryCellStyle | undefined {
  return Object.values(style).some((value) => value !== undefined) ? style : undefined
}

function nonNegativeInt(value: unknown): number {
  const number = numberValue(value)
  return number > 0 ? number : 0
}

function positiveInt(value: unknown): number {
  const number = numberValue(value)
  return number > 0 ? number : 1
}

function numberFromID(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return Math.max(0, Math.trunc(value))
  if (typeof value === 'bigint') return Number(value)
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? Math.max(0, Math.trunc(parsed)) : 0
  }
  return 0
}

function idString(value: unknown): string {
  if (typeof value === 'bigint') return value.toString()
  if (typeof value === 'number' && Number.isFinite(value)) return Math.trunc(value).toString()
  return typeof value === 'string' ? value : ''
}

function optionalID(value: unknown): string | undefined {
  const id = idString(value)
  return id && id !== '0' ? id : undefined
}

function unixNanoToMs(value: unknown): number | undefined {
  if (typeof value === 'bigint') return value === 0n ? undefined : Number(value / 1_000_000n)
  if (typeof value === 'number' && Number.isFinite(value)) return value === 0 ? undefined : Math.floor(value / 1_000_000)
  return undefined
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function recordArray(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value) ? value.filter((item): item is Record<string, unknown> => typeof item === 'object' && item !== null && !Array.isArray(item)) : []
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function optionalString(value: unknown): string | undefined {
  const text = stringValue(value)
  return text ? text : undefined
}

function numberValue(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return Math.trunc(value)
  if (typeof value === 'bigint') return Number(value)
  return 0
}

function optionalNumber(value: unknown): number | undefined {
  const number = numberValue(value)
  return Number.isFinite(number) ? number : undefined
}

function booleanValue(value: unknown): boolean {
  return value === true
}

function optionalBool(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}
