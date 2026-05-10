import { useRef, useCallback, useEffect, useState } from 'react'
import type { TerminalHandle } from './Terminal'
import { addNativeKeyboardListener } from '../platform/nativeKeyboard'

export interface UseTerminalKeyboardOptions {
  containerRef: React.RefObject<HTMLDivElement | null>
  mainRef: React.RefObject<HTMLDivElement | null>
  termWrapperRef: React.RefObject<HTMLDivElement | null>
  getTermRef: () => TerminalHandle | null
  shouldResize: () => boolean
  onKeyboardHide?: () => void
}

export interface UseTerminalKeyboardReturn {
  keyboardVisible: boolean
  fullMainHeightRef: React.MutableRefObject<number>
  markKeyboardVisible: () => void
  markKeyboardHidden: () => void
  reapplyKeyboardLayout: () => void
  resetKeyboardLayout: () => void
  handleBufferChange: (isAlternate: boolean) => void
  handleCursorMove: () => void
}

export function useTerminalKeyboard(opts: UseTerminalKeyboardOptions): UseTerminalKeyboardReturn {
  const { containerRef, mainRef, termWrapperRef, getTermRef, shouldResize, onKeyboardHide } = opts
  const onKeyboardHideRef = useRef(onKeyboardHide)
  onKeyboardHideRef.current = onKeyboardHide
  const shouldResizeRef = useRef(shouldResize)
  shouldResizeRef.current = shouldResize
  const fullMainHeightRef = useRef(0)
  const [keyboardVisible, setKeyboardVisible] = useState(false)
  const shiftRafRef = useRef(0)
  const terminalFocusedRef = useRef(false)
  const nativeKeyboardHeightRef = useRef(0)
  // Tracks the largest observed window.innerHeight — the "no keyboard" baseline.
  // Android adjustResize shrinks window.innerHeight when keyboard shows; by comparing
  // against this we get a keyboard height estimate even when vv.height tracks innerHeight.
  const fullWindowHeightRef = useRef(typeof window !== 'undefined' ? window.innerHeight : 0)

  // Helper: compute effective keyboard height from both detection methods.
  // iOS:     vv.height shrinks, innerHeight stays → vvKbH > 0
  // Android: innerHeight shrinks, vv.height ≈ innerHeight → winShrink > 0
  const getEffectiveKbH = useCallback(() => {
    const vv = window.visualViewport
    const vvKbH = vv ? window.innerHeight - vv.height : 0
    const winShrink = Math.max(0, fullWindowHeightRef.current - window.innerHeight)
    return Math.max(vvKbH, winShrink, nativeKeyboardHeightRef.current)
  }, [])

  const clearKeyboardLayout = useCallback(() => {
    if (shiftRafRef.current) {
      cancelAnimationFrame(shiftRafRef.current)
      shiftRafRef.current = 0
    }
    if (containerRef.current) {
      containerRef.current.style.height = ''
    }
    if (termWrapperRef.current) {
      termWrapperRef.current.style.height = ''
      termWrapperRef.current.style.transform = ''
    }
    getTermRef()?.adjustInputPosition(0)
  }, [containerRef, termWrapperRef, getTermRef])

  const resetKeyboardLayout = useCallback(() => {
    nativeKeyboardHeightRef.current = 0
    clearKeyboardLayout()
    setKeyboardVisible(false)
    onKeyboardHideRef.current?.()
  }, [clearKeyboardLayout])

  const markKeyboardVisible = useCallback(() => {
    terminalFocusedRef.current = true
    setKeyboardVisible(true)
  }, [])

  const markKeyboardHidden = useCallback(() => {
    terminalFocusedRef.current = false
    resetKeyboardLayout()
  }, [resetKeyboardLayout])

  const adjustShift = useCallback(() => {
    if (shouldResizeRef.current()) return
    if (!termWrapperRef.current || fullMainHeightRef.current <= 0) return
    const vv = window.visualViewport
    if (!vv) return
    const kbH = getEffectiveKbH()
    if (kbH <= 1) return
    const termRef = getTermRef()
    const cursorInfo = termRef?.getCursorInfo()
    let shift = 0
    if (cursorInfo) {
      const cursorPxFromTop = cursorInfo.cursorY * cursorInfo.lineHeight
      const visibleMainHeight = fullMainHeightRef.current - kbH
      if (cursorPxFromTop >= visibleMainHeight) {
        shift = Math.min(kbH, cursorPxFromTop - visibleMainHeight + cursorInfo.lineHeight * 2)
      }
    }
    termWrapperRef.current.style.transform = `translateY(${-shift}px)`
    termRef?.adjustInputPosition(kbH - shift)
  }, [termWrapperRef, getTermRef, getEffectiveKbH])

  const reapplyKeyboardLayout = useCallback(() => {
    if (!termWrapperRef.current || fullMainHeightRef.current <= 0) return
    const kbH = getEffectiveKbH()
    if (kbH <= 1) {
      if (terminalFocusedRef.current) {
        clearKeyboardLayout()
      } else {
        resetKeyboardLayout()
      }
      return
    }
    if (shouldResizeRef.current()) {
      termWrapperRef.current.style.height = ''
      termWrapperRef.current.style.transform = ''
      getTermRef()?.adjustInputPosition(0)
    } else {
      termWrapperRef.current.style.height = `${fullMainHeightRef.current}px`
      adjustShift()
    }
  }, [termWrapperRef, getTermRef, adjustShift, getEffectiveKbH, clearKeyboardLayout, resetKeyboardLayout])

  const handleBufferChange = useCallback((_isAlternate: boolean) => {
    reapplyKeyboardLayout()
  }, [reapplyKeyboardLayout])

  const handleCursorMove = useCallback(() => {
    if (shiftRafRef.current) return
    shiftRafRef.current = requestAnimationFrame(() => {
      shiftRafRef.current = 0
      adjustShift()
    })
  }, [adjustShift])

  useEffect(() => {
    const vv = window.visualViewport
    fullWindowHeightRef.current = window.innerHeight
    let kbDebounce: ReturnType<typeof setTimeout> | undefined
    let scrollRafId = 0
    let fallbackTimer: ReturnType<typeof setTimeout> | undefined

    const update = () => {
      if (!containerRef.current) return

      // Keep fullWindowHeight up-to-date: it's the max observed innerHeight
      // (keyboard hidden = full size, keyboard shown = shrunk).
      if (window.innerHeight > fullWindowHeightRef.current) {
        fullWindowHeightRef.current = window.innerHeight
      }

      const vvKbH = vv ? window.innerHeight - vv.height : 0
      const winShrink = Math.max(0, fullWindowHeightRef.current - window.innerHeight)
      const kbH = Math.max(vvKbH, winShrink, nativeKeyboardHeightRef.current)

      if (fullMainHeightRef.current <= 0 && mainRef.current) {
        fullMainHeightRef.current = mainRef.current.clientHeight
      }
      if (kbH > 100) {
        const visibleHeight = nativeKeyboardHeightRef.current > 0 && winShrink <= 1
          ? Math.max(0, window.innerHeight - nativeKeyboardHeightRef.current)
          : vv?.height ?? window.innerHeight
        containerRef.current.style.height = `${visibleHeight}px`
      } else {
        containerRef.current.style.height = ''
      }
      if (kbH <= 1 && mainRef.current) {
        fullMainHeightRef.current = mainRef.current.clientHeight
      }
      if (kbH > 1 && fullMainHeightRef.current > 0 && termWrapperRef.current) {
        if (shouldResizeRef.current()) {
          termWrapperRef.current.style.height = ''
          termWrapperRef.current.style.transform = ''
          getTermRef()?.adjustInputPosition(0)
        } else {
          termWrapperRef.current.style.height = `${fullMainHeightRef.current}px`
          adjustShift()
        }
      } else if (termWrapperRef.current) {
        if (terminalFocusedRef.current) {
          clearKeyboardLayout()
        } else {
          resetKeyboardLayout()
        }
      }

      const isKb = kbH > 100 || nativeKeyboardHeightRef.current > 0 || terminalFocusedRef.current
      clearTimeout(kbDebounce)
      if (isKb) {
        setKeyboardVisible(true)
      } else {
        kbDebounce = setTimeout(() => setKeyboardVisible(false), 120)
      }
    }

    update()

    const preventScroll = () => {
      if (scrollRafId) return
      scrollRafId = requestAnimationFrame(() => {
        scrollRafId = 0
        window.scrollTo(0, 0)
        document.documentElement.scrollTop = 0
        document.body.scrollTop = 0
        document.documentElement.scrollLeft = 0
        document.body.scrollLeft = 0
      })
    }

    // window.resize fires on Android (adjustResize mode) when keyboard shows/hides.
    // Update fullWindowHeight and run the full update so height-based detection kicks in.
    const handleWindowResize = () => {
      if (window.innerHeight > fullWindowHeightRef.current) {
        fullWindowHeightRef.current = window.innerHeight
      }
      update()
    }

    const onFocusChange = () => {
      clearTimeout(fallbackTimer)
      fallbackTimer = setTimeout(update, 300)
    }

    const onTerminalFocusIn = (e: FocusEvent) => {
      if (termWrapperRef.current?.contains(e.target as Node)) {
        terminalFocusedRef.current = true
        clearTimeout(kbDebounce)
        setKeyboardVisible(true)
      }
    }
    const onTerminalFocusOut = () => {
      setTimeout(() => {
        if (termWrapperRef.current && !termWrapperRef.current.contains(document.activeElement)) {
          terminalFocusedRef.current = false
          const kbH = getEffectiveKbH()
          if (kbH <= 1) {
            clearTimeout(kbDebounce)
            kbDebounce = setTimeout(() => setKeyboardVisible(false), 120)
          }
        }
      }, 0)
    }

    const removeNativeKeyboardListener = addNativeKeyboardListener((event) => {
      nativeKeyboardHeightRef.current = event.visible ? event.keyboardHeight ?? getEffectiveKbH() : 0
      if (event.visible) {
        terminalFocusedRef.current = true
        clearTimeout(kbDebounce)
        setKeyboardVisible(true)
        update()
        return
      }
      terminalFocusedRef.current = false
      resetKeyboardLayout()
    })

    const onResume = () => {
      terminalFocusedRef.current = false
      resetKeyboardLayout()
      window.setTimeout(update, 120)
    }

    vv?.addEventListener('resize', update)
    vv?.addEventListener('scroll', update)
    window.addEventListener('resize', handleWindowResize)
    window.addEventListener('scroll', preventScroll, { passive: false })
    document.addEventListener('visibilitychange', onResume)
    document.addEventListener('termx:resume', onResume)
    document.addEventListener('focusin', onFocusChange)
    document.addEventListener('focusout', onFocusChange)
    document.addEventListener('focusin', onTerminalFocusIn)
    document.addEventListener('focusout', onTerminalFocusOut)
    return () => {
      removeNativeKeyboardListener()
      vv?.removeEventListener('resize', update)
      vv?.removeEventListener('scroll', update)
      window.removeEventListener('resize', handleWindowResize)
      window.removeEventListener('scroll', preventScroll)
      document.removeEventListener('visibilitychange', onResume)
      document.removeEventListener('termx:resume', onResume)
      document.removeEventListener('focusin', onFocusChange)
      document.removeEventListener('focusout', onFocusChange)
      document.removeEventListener('focusin', onTerminalFocusIn)
      document.removeEventListener('focusout', onTerminalFocusOut)
      clearTimeout(kbDebounce)
      clearTimeout(fallbackTimer)
      if (scrollRafId) cancelAnimationFrame(scrollRafId)
    }
  }, [])

  return {
    keyboardVisible,
    fullMainHeightRef,
    markKeyboardVisible,
    markKeyboardHidden,
    reapplyKeyboardLayout,
    resetKeyboardLayout,
    handleBufferChange,
    handleCursorMove,
  }
}
