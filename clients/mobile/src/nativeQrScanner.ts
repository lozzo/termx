import { Html5Qrcode } from 'html5-qrcode'
import { NATIVE_BACK_PRIORITY, addNativeBackHandler, anyttyI18n } from '@anytty/ui'

const qrScannerRootId = 'anytty-camera-qr-scanner'
const qrScannerReaderId = 'anytty-camera-qr-reader'
const qrScannerTitleId = 'anytty-camera-qr-title'

export interface NativeQrScannerOptions {
  signal?: AbortSignal | undefined
}

export function scanPairingCode(options?: NativeQrScannerOptions): Promise<string | null> {
  console.info('[anytty:scan] camera scan requested')
  if (options?.signal?.aborted) return Promise.resolve(null)

  const scannerSize = scannerSquareSize()
  const qrboxSize = Math.min(220, Math.max(180, scannerSize - 32))
  const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const root = document.createElement('div')
  root.id = qrScannerRootId
  root.className = 'anytty-app-page fixed inset-0 z-[2147483647] flex flex-col items-stretch px-4 pb-[calc(env(safe-area-inset-bottom)+12px)] pt-[calc(env(safe-area-inset-top)+12px)]'
  root.setAttribute('aria-labelledby', qrScannerTitleId)
  root.setAttribute('aria-modal', 'true')
  root.setAttribute('role', 'dialog')
  root.tabIndex = -1

  const scannerStyle = document.createElement('style')
  scannerStyle.textContent = `
    #${qrScannerReaderId} {
      width: ${scannerSize}px !important;
      height: ${scannerSize}px !important;
      aspect-ratio: 1 / 1 !important;
      overflow: hidden !important;
      border: none !important;
    }
    #${qrScannerReaderId} > div,
    #${qrScannerReaderId}__scan_region,
    #${qrScannerReaderId}__scan_region > div,
    #${qrScannerReaderId} video,
    #${qrScannerReaderId} canvas {
      width: 100% !important;
      height: 100% !important;
    }
    #${qrScannerReaderId} video,
    #${qrScannerReaderId} canvas {
      object-fit: cover !important;
    }
    #${qrScannerReaderId} img {
      display: none !important;
    }
  `

  const header = document.createElement('div')
  header.className = 'flex items-center justify-between gap-3 min-h-[44px]'
  const title = document.createElement('div')
  title.id = qrScannerTitleId
  title.textContent = anyttyI18n.t('scanner.title')
  title.className = 'text-[17px] font-bold tracking-tight text-zinc-900'
  const cancelButton = document.createElement('button')
  cancelButton.type = 'button'
  cancelButton.textContent = anyttyI18n.t('common.cancel')
  cancelButton.className = 'anytty-app-secondary-button px-4 text-[14px] font-semibold'

  const reader = document.createElement('div')
  reader.id = qrScannerReaderId
  reader.className = 'mt-4 self-center overflow-hidden border border-[var(--anytty-app-line)] bg-black'
  reader.style.width = `${scannerSize}px`
  reader.style.height = `${scannerSize}px`
  reader.style.minWidth = `${scannerSize}px`
  reader.style.minHeight = `${scannerSize}px`
  reader.style.maxWidth = `${scannerSize}px`
  reader.style.maxHeight = `${scannerSize}px`

  const hint = document.createElement('div')
  hint.textContent = anyttyI18n.t('scanner.hint')
  hint.className = 'mt-4 px-4 text-center text-[13px] font-medium leading-[20px] text-zinc-500'

  header.append(title, cancelButton)
  root.append(scannerStyle, header, reader, hint)
  document.body.append(root)
  const restoreBackground = isolateBackground(root)
  cancelButton.focus()

  // Android WebView 的 BarcodeDetector 可能在缺少 GMS provider 时让进程直接崩溃；扫码只使用库内 decoder。
  let scanner: Html5Qrcode
  try {
    scanner = new Html5Qrcode(qrScannerReaderId, {
      verbose: false,
      useBarCodeDetectorIfSupported: false,
    })
  } catch (error) {
    root.remove()
    restoreBackground()
    scheduleFocusRestore(previousFocus)
    return Promise.reject(normalizeScanError(error))
  }

  let resolveScan!: (value: string | null) => void
  let rejectScan!: (reason: Error) => void
  const result = new Promise<string | null>((resolve, reject) => {
    resolveScan = resolve
    rejectScan = reject
  })
  let promptSettled = false
  let promptUiCleaned = false
  let cameraCleanupStarted = false
  let removeNativeBackHandler = () => {}
  let startOutcomeSettled = false
  let settleStartOutcome!: (outcome: CameraStartOutcome) => void
  const startOutcome = new Promise<CameraStartOutcome>((resolve) => {
    settleStartOutcome = resolve
  })
  const settleCameraStart = (outcome: CameraStartOutcome) => {
    if (startOutcomeSettled) return
    startOutcomeSettled = true
    settleStartOutcome(outcome)
  }

  const cleanupCamera = () => {
    if (cameraCleanupStarted) return
    cameraCleanupStarted = true
    void startOutcome.then(async (outcome) => {
      if (outcome.started) {
        try {
          await scanner.stop()
        } catch {}
      }
      try {
        scanner.clear()
      } catch {}
    }).catch(() => undefined)
  }

  const cleanupPromptUi = (restorePreviousFocus: boolean) => {
    if (promptUiCleaned) return
    promptUiCleaned = true
    cancelButton.disabled = true
    options?.signal?.removeEventListener('abort', onAbort)
    cancelButton.removeEventListener('click', onCancel)
    root.removeEventListener('keydown', onKeyDown)
    removeNativeBackHandler()
    root.remove()
    restoreBackground()
    if (restorePreviousFocus) scheduleFocusRestore(previousFocus)
  }

  const finishPrompt = (value: string | null, error?: unknown, restorePreviousFocus = true) => {
    if (promptSettled) return
    promptSettled = true
    cleanupPromptUi(restorePreviousFocus)
    cleanupCamera()

    if (error !== undefined) {
      const normalized = normalizeScanError(error)
      console.warn('[anytty:scan] camera scan failed', normalized.message)
      rejectScan(normalized)
      return
    }
    console.info(value ? '[anytty:scan] QR decoded' : '[anytty:scan] scan cancelled')
    resolveScan(value)
  }

  function onCancel() {
    finishPrompt(null)
  }
  function onAbort() {
    finishPrompt(null)
  }
  function onKeyDown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      finishPrompt(null)
      return
    }
    if (event.key !== 'Tab') return
    const focusable = scannerFocusableElements(root)
    if (focusable.length === 0) {
      event.preventDefault()
      root.focus()
      return
    }
    const first = focusable[0]!
    const last = focusable[focusable.length - 1]!
    const activeElement = document.activeElement
    if (!root.contains(activeElement)) {
      event.preventDefault()
      first.focus()
      return
    }
    if (event.shiftKey && activeElement === first) {
      event.preventDefault()
      last.focus()
      return
    }
    if (!event.shiftKey && activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  cancelButton.addEventListener('click', onCancel)
  root.addEventListener('keydown', onKeyDown)
  removeNativeBackHandler = addNativeBackHandler(() => {
    finishPrompt(null)
  }, NATIVE_BACK_PRIORITY.NESTED_OVERLAY)
  options?.signal?.addEventListener('abort', onAbort, { once: true })

  try {
    void Promise.resolve(scanner.start(
      { facingMode: 'environment' },
      { fps: 10, qrbox: { width: qrboxSize, height: qrboxSize }, aspectRatio: 1.0 },
      (decodedText) => {
        if (promptSettled) return
        try {
          scanner.pause(true)
        } catch {}
        finishPrompt(decodedText, undefined, false)
      },
      () => {},
    )).then(
      () => settleCameraStart({ started: true }),
      (error: unknown) => settleCameraStart({ started: false, error }),
    )
  } catch (error) {
    settleCameraStart({ started: false, error })
  }
  void startOutcome.then((outcome) => {
    if (!outcome.started) finishPrompt(null, outcome.error)
  })

  return result
}

function scannerSquareSize(): number {
  const width = Math.max(0, window.innerWidth || document.documentElement.clientWidth || 360)
  const height = Math.max(0, window.innerHeight || document.documentElement.clientHeight || 640)
  const availableHeight = Math.max(180, height - 340)
  return Math.floor(Math.max(220, Math.min(width * 0.78, availableHeight, 280)))
}

function normalizeScanError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error))
}

type CameraStartOutcome = { started: true } | { started: false; error: unknown }

function isolateBackground(scannerRoot: HTMLElement): () => void {
  const states = Array.from(document.body.children)
    .filter((element): element is HTMLElement => element instanceof HTMLElement && element !== scannerRoot)
    .map((element) => ({
      element,
      ariaHidden: element.getAttribute('aria-hidden'),
      inert: element.getAttribute('inert'),
    }))
  for (const { element } of states) {
    element.setAttribute('aria-hidden', 'true')
    element.setAttribute('inert', '')
  }
  let restored = false
  return () => {
    if (restored) return
    restored = true
    for (const { element, ariaHidden, inert } of states) {
      if (ariaHidden === null) element.removeAttribute('aria-hidden')
      else element.setAttribute('aria-hidden', ariaHidden)
      if (inert === null) element.removeAttribute('inert')
      else element.setAttribute('inert', inert)
    }
  }
}

function scannerFocusableElements(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
  )).filter((element) => (
    !element.matches(':disabled')
      && element.getAttribute('aria-disabled') !== 'true'
      && !element.closest('[inert], [aria-hidden="true"]')
  ))
}

function scheduleFocusRestore(target: HTMLElement | null): void {
  if (!target) return
  window.requestAnimationFrame(() => {
    if (!target.isConnected) return
    if (target.matches(':disabled') || target.getAttribute('aria-disabled') === 'true') return
    if (target.closest('[inert], [aria-hidden="true"]')) return
    target.focus()
  })
}
