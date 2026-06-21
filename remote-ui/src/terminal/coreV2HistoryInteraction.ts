import type { CoreV2HistorySource } from './coreV2HistorySource'
import type { CoreV2HistorySurfaceSnapshot } from './coreV2HistorySurface'
import type {
  CoreV2HistoryCursor,
  CoreV2HistoryRange,
  CoreV2HistoryRow,
} from './coreV2TerminalProtocol'

export interface CoreV2HistoryCellPoint {
  lineId: string
  col: number
}

export interface CoreV2HistorySelection {
  anchor: CoreV2HistoryCellPoint
  focus: CoreV2HistoryCellPoint
}

export interface CoreV2HistorySearchMatch {
  range: CoreV2HistoryRange
  text: string
}

export function selectionFromSurfaceRows(
  snapshot: CoreV2HistorySurfaceSnapshot,
  anchor: CoreV2HistoryCursor & { col: number },
  focus: CoreV2HistoryCursor & { col: number },
): CoreV2HistorySelection {
  const anchorPoint = pointFromCursor(snapshot, anchor)
  const focusPoint = pointFromCursor(snapshot, focus)
  return {
    anchor: anchorPoint,
    focus: focusPoint,
  }
}

export function rangeFromHistorySelection(
  snapshot: CoreV2HistorySurfaceSnapshot,
  selection: CoreV2HistorySelection,
): CoreV2HistoryRange {
  ensureUsableSnapshot(snapshot)
  const anchor = clampPointToSnapshot(snapshot, selection.anchor)
  const focus = clampPointToSnapshot(snapshot, selection.focus)
  return comparePoints(snapshot, anchor, focus) <= 0
    ? {
        startLineId: anchor.lineId,
        startCol: anchor.col,
        endLineId: focus.lineId,
        endCol: focus.col,
      }
    : {
        startLineId: focus.lineId,
        startCol: focus.col,
        endLineId: anchor.lineId,
        endCol: anchor.col,
      }
}

export async function copyHistorySelection(
  source: Pick<CoreV2HistorySource, 'copy'>,
  snapshot: CoreV2HistorySurfaceSnapshot,
  selection: CoreV2HistorySelection,
): Promise<string> {
  ensureUsableSnapshot(snapshot)
  if (!snapshot.token) throw new Error('history copy requires a frozen history token')
  const range = rangeFromHistorySelection(snapshot, selection)
  // App 只提交 logical range；最终文本由 core-v2 history.copy 从冻结快照生成。
  return await source.copy({
    terminalId: snapshot.terminalId,
    token: snapshot.token,
    cols: snapshot.cols,
    generation: snapshot.generation ?? undefined,
    boundaryFirstLineId: snapshot.firstLineId,
    boundaryLastLineId: snapshot.lastLineId,
    range,
  })
}

export function searchHistorySurface(
  snapshot: CoreV2HistorySurfaceSnapshot,
  query: string,
  options: { caseSensitive?: boolean | undefined } = {},
): CoreV2HistorySearchMatch[] {
  ensureUsableSnapshot(snapshot)
  if (query === '') return []
  const lines = logicalLineTexts(snapshot)
  const needle = options.caseSensitive === true ? query : query.toLocaleLowerCase()
  const matches: CoreV2HistorySearchMatch[] = []
  for (const line of lines) {
    const haystack = options.caseSensitive === true ? line.text : line.text.toLocaleLowerCase()
    let from = 0
    while (from <= haystack.length) {
      const index = haystack.indexOf(needle, from)
      if (index < 0) break
      matches.push({
        text: line.text.slice(index, index + query.length),
        range: {
          startLineId: line.lineId,
          startCol: index,
          endLineId: line.lineId,
          endCol: index + query.length,
        },
      })
      from = index + Math.max(1, query.length)
    }
  }
  return matches
}

function ensureUsableSnapshot(snapshot: CoreV2HistorySurfaceSnapshot): void {
  if (snapshot.stale) throw new Error(`history surface is stale: ${snapshot.staleReason ?? 'unknown'}`)
  if (snapshot.rows.length === 0) throw new Error('history surface has no logical rows')
}

function pointFromCursor(
  snapshot: CoreV2HistorySurfaceSnapshot,
  cursor: CoreV2HistoryCursor & { col: number },
): CoreV2HistoryCellPoint {
  const row = snapshot.rows.find((item) => item.logicalLineId === cursor.lineId && item.rowInLine === cursor.rowInLine)
  if (!row) throw new Error(`history selection cursor is outside loaded surface: ${cursor.lineId}:${cursor.rowInLine}`)
  return {
    lineId: cursor.lineId,
    col: clampColumn(cursor.col, lineWidth(row)),
  }
}

function clampPointToSnapshot(
  snapshot: CoreV2HistorySurfaceSnapshot,
  point: CoreV2HistoryCellPoint,
): CoreV2HistoryCellPoint {
  const width = lineWidthForID(snapshot, point.lineId)
  return {
    lineId: point.lineId,
    col: clampColumn(point.col, width),
  }
}

function comparePoints(
  snapshot: CoreV2HistorySurfaceSnapshot,
  left: CoreV2HistoryCellPoint,
  right: CoreV2HistoryCellPoint,
): number {
  const leftIndex = lineOrder(snapshot).get(left.lineId)
  const rightIndex = lineOrder(snapshot).get(right.lineId)
  if (leftIndex === undefined) throw new Error(`history selection line is outside loaded surface: ${left.lineId}`)
  if (rightIndex === undefined) throw new Error(`history selection line is outside loaded surface: ${right.lineId}`)
  if (leftIndex !== rightIndex) return leftIndex - rightIndex
  return left.col - right.col
}

function lineWidthForID(snapshot: CoreV2HistorySurfaceSnapshot, lineId: string): number {
  const text = logicalLineTexts(snapshot).find((line) => line.lineId === lineId)
  if (!text) throw new Error(`history selection line is outside loaded surface: ${lineId}`)
  return text.width
}

function logicalLineTexts(snapshot: CoreV2HistorySurfaceSnapshot): Array<{ lineId: string; text: string; width: number }> {
  const out: Array<{ lineId: string; text: string; width: number }> = []
  let current: { lineId: string; text: string; width: number } | null = null
  for (const row of snapshot.rows) {
    if (!row.logicalLineId) continue
    if (!current || current.lineId !== row.logicalLineId) {
      current = { lineId: row.logicalLineId, text: '', width: 0 }
      out.push(current)
    }
    current.text += rowText(row)
    current.width += lineWidth(row)
  }
  return out
}

function lineOrder(snapshot: CoreV2HistorySurfaceSnapshot): Map<string, number> {
  const order = new Map<string, number>()
  for (const line of logicalLineTexts(snapshot)) {
    if (!order.has(line.lineId)) {
      order.set(line.lineId, order.size)
    }
  }
  return order
}

function rowText(row: CoreV2HistoryRow): string {
  return row.cells.map((cell) => cell.text).join('')
}

function lineWidth(row: CoreV2HistoryRow): number {
  return row.cells.reduce((width, cell) => width + Math.max(0, cell.width), 0)
}

function clampColumn(value: number, max: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(Math.trunc(value), max))
}
