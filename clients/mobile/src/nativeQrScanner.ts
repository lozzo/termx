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
    restoreFocus(previousFocus)
    return Promise.reject(normalizeScanError(error))
  }

  let resolveScan!: (value: string | null) => void
  let rejectScan!: (reason: Error) => void
  const result = new Promise<string | null>((resolve, reject) => {
    resolveScan = resolve
    rejectScan = reject
  })
  let startFailure: unknown
  let startResult = Promise.resolve(false)
  let finalizePromise: Promise<void> | null = null
  let removeNativeBackHandler = () => {}

  const onAbort = () => { void finalize(null) }
  const finalize = (value: string | null, error?: unknown): Promise<void> => {
    if (finalizePromise) return finalizePromise
    finalizePromise = (async () => {
      cancelButton.disabled = true
      options?.signal?.removeEventListener('abort', onAbort)
      const started = await startResult
      if (started) {
        try {
          await scanner.stop()
        } catch {}
      }
      try {
        scanner.clear()
      } catch {}
      root.remove()
      removeNativeBackHandler()
      restoreFocus(previousFocus)

      if (error !== undefined) {
        const normalized = normalizeScanError(error)
        console.warn('[anytty:scan] camera scan failed', normalized.message)
        rejectScan(normalized)
        return
      }
      console.info(value ? '[anytty:scan] QR decoded' : '[anytty:scan] scan cancelled')
      resolveScan(value)
    })()
    return finalizePromise
  }

  cancelButton.onclick = () => { void finalize(null) }
  removeNativeBackHandler = addNativeBackHandler(() => {
    void finalize(null)
    return true
  }, NATIVE_BACK_PRIORITY.NESTED_OVERLAY)
  options?.signal?.addEventListener('abort', onAbort, { once: true })

  try {
    startResult = scanner.start(
      { facingMode: 'environment' },
      { fps: 10, qrbox: { width: qrboxSize, height: qrboxSize }, aspectRatio: 1.0 },
      (decodedText) => {
        try {
          scanner.pause(true)
        } catch {}
        void finalize(decodedText)
      },
      () => {},
    ).then(
      () => true,
      (error) => {
        startFailure = error
        return false
      },
    )
  } catch (error) {
    startFailure = error
    startResult = Promise.resolve(false)
  }
  void startResult.then((started) => {
    if (!started) void finalize(null, startFailure)
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

function restoreFocus(target: HTMLElement | null): void {
  if (target?.isConnected && !target.closest('[inert]')) target.focus()
}
