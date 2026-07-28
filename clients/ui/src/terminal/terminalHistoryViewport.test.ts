import { describe, expect, it } from 'vitest'
import { historyRequestAwaitingApply, historyViewportAfterApply, terminalScrollLineDelta, TerminalHistoryViewportController } from './terminalHistoryViewport'

describe('TerminalHistoryViewportController', () => {
  it('stages the first page before a later gesture enters history', () => {
    const viewport = new TerminalHistoryViewportController()

    viewport.prime()

    expect(viewport.shouldRenderLiveUpdate()).toBe(false)
    expect(viewport.isPrimed).toBe(true)
    expect(viewport.isFrozen).toBe(false)
    expect(viewport.resumeAtBottom(true, false)).toBe(false)
    expect(viewport.enterHistory()).toBe(true)
    expect(viewport.isFrozen).toBe(true)
    expect(viewport.resumeAtBottom(true, false)).toBe(false)
    expect(viewport.confirmHistoryMovement(false)).toBe(true)
    expect(viewport.hasDeferredLiveUpdate).toBe(true)
  })

  it('resumes once at the bottom and ignores duplicate render notifications', () => {
    const viewport = new TerminalHistoryViewportController()
    viewport.prime()
    viewport.enterHistory()
    viewport.confirmHistoryMovement(false)
    viewport.shouldRenderLiveUpdate()

    expect(viewport.resumeAtBottom(false, false)).toBe(false)
    expect(viewport.resumeAtBottom(true, true)).toBe(false)
    expect(viewport.resumeAtBottom(true, false)).toBe(true)
    expect(viewport.resumeAtBottom(true, false)).toBe(false)
    expect(viewport.isFrozen).toBe(false)
    expect(viewport.shouldRenderLiveUpdate()).toBe(true)
  })

  it('can abandon a failed priming load at the live bottom', () => {
    const viewport = new TerminalHistoryViewportController()
    viewport.prime()

    expect(viewport.resumeAtBottom(true, false, true)).toBe(true)
    expect(viewport.isLiveUpdateDeferred).toBe(false)
  })
})

describe('historyViewportAfterApply', () => {
  it('keeps the first replacement page staged at the live bottom', () => {
    expect(historyViewportAfterApply({
      operation: 'replace',
      previouslyAppliedRows: 0,
      restoreViewportY: 0,
      actualPrependedRows: 510,
      fallbackPrependedRows: 250,
      bufferLength: 550,
      viewportRows: 40,
    })).toBe(510)
  })

  it('uses the actual xterm row delta to preserve the anchor for older prepends', () => {
    expect(historyViewportAfterApply({
      operation: 'prepend',
      previouslyAppliedRows: 250,
      restoreViewportY: 18,
      actualPrependedRows: 267,
      fallbackPrependedRows: 250,
      bufferLength: 817,
      viewportRows: 40,
    })).toBe(285)
  })
})

describe('terminalScrollLineDelta', () => {
  it('always returns an integer for fractional WebView viewport coordinates', () => {
    expect(terminalScrollLineDelta(12.8, 7.2)).toBe(6)
    expect(Number.isInteger(terminalScrollLineDelta(12.8, 7.2))).toBe(true)
  })

  it('ignores invalid viewport coordinates', () => {
    expect(terminalScrollLineDelta(Number.NaN, 7)).toBe(0)
  })
})

describe('historyRequestAwaitingApply', () => {
  it('blocks another page until xterm has applied every requested row', () => {
    expect(historyRequestAwaitingApply(100, 0)).toBe(true)
    expect(historyRequestAwaitingApply(200, 100)).toBe(true)
    expect(historyRequestAwaitingApply(100, 100)).toBe(false)
  })
})
