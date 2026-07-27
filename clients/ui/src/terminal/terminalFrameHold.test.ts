import { describe, expect, it, vi } from 'vitest'
import { holdTerminalFrame } from './terminalFrameHold'

describe('holdTerminalFrame', () => {
  it('keeps a cloned frame for two paints and then removes it', () => {
    const frames: FrameRequestCallback[] = []
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      frames.push(callback)
      return frames.length
    })
    const container = document.createElement('div')
    const screen = document.createElement('div')
    screen.className = 'xterm-screen'
    screen.textContent = 'stable output'
    container.append(screen)
    document.body.append(container)
    vi.spyOn(container, 'getBoundingClientRect').mockReturnValue(rect(0, 0, 320, 200))
    vi.spyOn(screen, 'getBoundingClientRect').mockReturnValue(rect(8, 12, 300, 160))

    const hold = holdTerminalFrame(container, screen)

    expect(hold).not.toBeNull()
    expect(container.querySelector('[data-anytty-terminal-frame-hold]')?.textContent).toBe('stable output')
    hold?.releaseAfterPaint()
    frames.shift()?.(0)
    expect(container.querySelector('[data-anytty-terminal-frame-hold]')).not.toBeNull()
    frames.shift()?.(16)
    expect(container.querySelector('[data-anytty-terminal-frame-hold]')).toBeNull()
  })
})

function rect(x: number, y: number, width: number, height: number): DOMRect {
  return {
    x,
    y,
    width,
    height,
    top: y,
    left: x,
    right: x + width,
    bottom: y + height,
    toJSON: () => ({}),
  }
}
