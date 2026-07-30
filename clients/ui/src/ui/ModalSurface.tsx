import { useEffect, useRef, type ComponentPropsWithoutRef, type KeyboardEvent, type RefObject } from 'react'

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'summary',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export interface ModalSurfaceProps extends Omit<ComponentPropsWithoutRef<'section'>, 'role'> {
  onRequestClose: () => void
  initialFocusRef?: RefObject<HTMLElement | null> | undefined
}

export function ModalSurface({ children, initialFocusRef, onKeyDown, onRequestClose, tabIndex = -1, ...props }: ModalSurfaceProps) {
  const surfaceRef = useRef<HTMLElement>(null)

  useEffect(() => {
    const surface = surfaceRef.current
    if (!surface) return
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const outside = collectOutsideElements(surface)
    const previous = outside.map((element) => ({
      element,
      ariaHidden: element.getAttribute('aria-hidden'),
      inert: element.hasAttribute('inert'),
    }))
    for (const element of outside) {
      element.setAttribute('aria-hidden', 'true')
      element.setAttribute('inert', '')
    }
    const focusTarget = initialFocusRef?.current ?? focusableElements(surface)[0] ?? surface
    focusTarget.focus()
    return () => {
      for (const item of previous) {
        if (item.ariaHidden === null) item.element.removeAttribute('aria-hidden')
        else item.element.setAttribute('aria-hidden', item.ariaHidden)
        if (!item.inert) item.element.removeAttribute('inert')
      }
      if (previousFocus?.isConnected) previousFocus.focus()
    }
  }, [initialFocusRef])

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    onKeyDown?.(event)
    if (event.defaultPrevented) return
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      onRequestClose()
      return
    }
    if (event.key !== 'Tab') return
    const surface = surfaceRef.current
    if (!surface) return
    const focusable = focusableElements(surface)
    if (focusable.length === 0) {
      event.preventDefault()
      surface.focus()
      return
    }
    const first = focusable[0]!
    const last = focusable[focusable.length - 1]!
    if (event.shiftKey && (document.activeElement === first || !surface.contains(document.activeElement))) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !surface.contains(document.activeElement))) {
      event.preventDefault()
      first.focus()
    }
  }

  return (
    <section {...props} aria-modal="true" ref={surfaceRef} role="dialog" tabIndex={tabIndex} onKeyDown={handleKeyDown}>
      {children}
    </section>
  )
}

function focusableElements(surface: HTMLElement): HTMLElement[] {
  return Array.from(surface.querySelectorAll<HTMLElement>(focusableSelector))
    .filter((element) => !element.hasAttribute('hidden') && element.getAttribute('aria-hidden') !== 'true')
}

function collectOutsideElements(surface: HTMLElement): HTMLElement[] {
  const outside: HTMLElement[] = []
  let current: HTMLElement | null = surface
  while (current?.parentElement) {
    for (const sibling of current.parentElement.children) {
      if (sibling !== current && sibling instanceof HTMLElement && sibling.tagName !== 'SCRIPT') outside.push(sibling)
    }
    current = current.parentElement
    if (current === document.body) break
  }
  return outside
}
