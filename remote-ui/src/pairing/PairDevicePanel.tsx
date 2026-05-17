import { useState } from 'react'
import type { MachineSessionStore } from '../state/localAppIdentity'
import type { LocalPairingApi } from '../core/transport'
import { AlertCircle, Camera, KeyRound, ShieldCheck } from 'lucide-react'
import { hapticError, hapticSuccess } from '../platform/haptics'
import { parsePairingPayload } from '../state/pairingPayload'

export interface PairDevicePanelProps {
  api: LocalPairingApi
  sessionStore: MachineSessionStore
  appName: string
  machineId?: string | undefined
  onPaired?: ((machineId: string) => void) | undefined
  className?: string | undefined
}

export function PairDevicePanel({ api, sessionStore, appName, machineId, onPaired, className }: PairDevicePanelProps) {
  const [pairPayload, setPairPayload] = useState('')
  const [pairSessionId, setPairSessionId] = useState('')
  const [pairSecret, setPairSecret] = useState('')
  const [status, setStatus] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [scanning, setScanning] = useState(false)

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await pairDevice()
  }

  async function pairDevice() {
    setError(null)
    setStatus(null)
    setSubmitting(true)
    try {
      const parsed = parseLocalPairingInput(pairPayload)
      const targetMachineId = parsed?.machineId ?? machineId
      const result = await api.pair({
        ...(targetMachineId ? { machineId: targetMachineId } : {}),
        pairSessionId: parsed?.pairSessionId ?? pairSessionId,
        pairSecret: parsed?.pairSecret ?? pairSecret,
        appDeviceId: createBrowserAppDeviceId(),
        appName,
        requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
      })
      sessionStore.saveSessionToken(result.machineId, result.sessionToken, result.expiresAt, parsed?.answerProofSecret)
      setStatus(`Paired with ${result.machineId}`)
      hapticSuccess()
      onPaired?.(result.machineId)
    } catch (err) {
      hapticError()
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  async function scanQRCode() {
    setError(null)
    setScanning(true)
    try {
      const scanned = await scanPairingQRCode()
      if (!scanned) return
      setPairPayload(scanned)
      await pairDeviceFromRaw(scanned)
    } catch (err) {
      hapticError()
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setScanning(false)
    }
  }

  async function pairDeviceFromRaw(rawValue: string) {
    setStatus(null)
    setSubmitting(true)
    try {
      const parsed = parseLocalPairingInput(rawValue)
      if (!parsed) throw new Error('TermX QR content is required')
      const targetMachineId = parsed.machineId ?? machineId
      const result = await api.pair({
        ...(targetMachineId ? { machineId: targetMachineId } : {}),
        pairSessionId: parsed.pairSessionId,
        pairSecret: parsed.pairSecret,
        appDeviceId: createBrowserAppDeviceId(),
        appName,
        requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
      })
      sessionStore.saveSessionToken(result.machineId, result.sessionToken, result.expiresAt, parsed.answerProofSecret)
      setStatus(`Paired with ${result.machineId}`)
      hapticSuccess()
      onPaired?.(result.machineId)
    } catch (err) {
      hapticError()
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className={`flex flex-col rounded-2xl bg-white p-5 shadow-sm ring-1 ring-zinc-200/60 ${className || ''}`} data-testid="termx-local-pair-panel">
      <div className="mb-6 flex items-center gap-4">
        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-zinc-100 text-zinc-600">
           <KeyRound className="h-6 w-6" />
        </div>
        <div>
           <h3 className="text-[17px] font-bold tracking-tight text-zinc-900">Authorize Device</h3>
           <p className="text-[13px] font-medium text-zinc-500">Scan or paste a TermX pairing code</p>
        </div>
      </div>

      <form className="flex flex-col gap-4" onSubmit={(event) => { void submit(event) }}>
        <button
          className="flex min-h-11 w-full items-center justify-center gap-2 rounded-xl border border-zinc-200 bg-white px-4 text-[14px] font-semibold text-zinc-800 shadow-sm transition-all active:scale-[0.98] hover:bg-zinc-50 disabled:pointer-events-none disabled:opacity-50"
          type="button"
          disabled={submitting || scanning}
          onClick={() => { void scanQRCode() }}
        >
          <Camera className="h-4 w-4" />
          {scanning ? 'Scanning...' : 'Scan QR'}
        </button>
        <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
          TermX QR content
          <textarea
            autoComplete="off"
            className="min-h-28 w-full resize-none rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-3 font-mono text-[12px] leading-5 text-zinc-900 placeholder:text-zinc-400 outline-none transition-all focus:border-blue-500 focus:bg-white focus:ring-4 focus:ring-blue-500/10"
            name="pairPayload"
            placeholder="termx://pair?payload=..."
            spellCheck={false}
            value={pairPayload}
            onChange={(event) => setPairPayload(event.currentTarget.value)}
          />
        </label>
        <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-2 text-[11px] font-semibold uppercase tracking-wide text-zinc-400">
          <span className="h-px bg-zinc-200" />
          <span>Fallback</span>
          <span className="h-px bg-zinc-200" />
        </div>
        <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
          Pair ID
          <input
            autoComplete="off"
            className="min-h-12 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 placeholder:text-zinc-400 outline-none transition-all focus:border-blue-500 focus:bg-white focus:ring-4 focus:ring-blue-500/10"
            name="pairSessionId"
            placeholder="e.g. 12345678"
            value={pairSessionId}
            onChange={(event) => setPairSessionId(event.currentTarget.value)}
          />
        </label>
        <label className="flex flex-col gap-2 text-[14px] font-semibold text-zinc-700">
          Pair secret
          <input
            autoComplete="off"
            className="min-h-12 w-full rounded-xl border border-zinc-200 bg-zinc-50 px-4 py-2 text-[15px] text-zinc-900 placeholder:text-zinc-400 outline-none transition-all focus:border-blue-500 focus:bg-white focus:ring-4 focus:ring-blue-500/10"
            name="pairSecret"
            type="password"
            placeholder="••••••••"
            value={pairSecret}
            onChange={(event) => setPairSecret(event.currentTarget.value)}
          />
        </label>
        <button
          className="mt-4 flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-zinc-900 px-4 text-[15px] font-semibold text-white shadow-md transition-all active:scale-[0.98] hover:bg-zinc-800 active:bg-zinc-800 disabled:pointer-events-none disabled:opacity-50"
          type="submit"
          disabled={submitting || scanning || !canSubmitPairing(pairPayload, pairSessionId, pairSecret)}
        >
          {submitting ? (
            <>
              <div className="h-5 w-5 animate-spin rounded-full border-2 border-zinc-400 border-t-white"></div>
              Pairing...
            </>
          ) : (
            'Pair Device'
          )}
        </button>
      </form>

      {status && (
        <div className="mt-5 flex items-start gap-3 rounded-xl bg-emerald-50 p-4 text-[14px] text-emerald-800 ring-1 ring-emerald-200/60" role="status">
          <ShieldCheck className="h-6 w-6 shrink-0 text-emerald-600" />
          <p className="mt-0.5 font-medium leading-tight">{status}</p>
        </div>
      )}
      {error && (
        <div className="mt-5 flex items-start gap-3 rounded-xl bg-red-50 p-4 text-[14px] text-red-800 ring-1 ring-red-200/60" role="alert">
          <AlertCircle className="h-6 w-6 shrink-0 text-red-600" />
          <p className="mt-0.5 font-medium leading-tight">{error}</p>
        </div>
      )}
    </div>
  )
}

interface ParsedLocalPairingInput {
  machineId?: string | undefined
  pairSessionId: string
  pairSecret: string
  answerProofSecret?: string | undefined
}

function parseLocalPairingInput(value: string): ParsedLocalPairingInput | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  const payload = parsePairingPayload(trimmed)
  return {
    machineId: payload.machine.id,
    pairSessionId: payload.pairing.sessionId,
    pairSecret: payload.pairing.secret,
    ...(payload.pairing.answerProofSecret ? { answerProofSecret: payload.pairing.answerProofSecret } : {}),
  }
}

function canSubmitPairing(pairPayload: string, pairSessionId: string, pairSecret: string): boolean {
  return pairPayload.trim() !== '' || (pairSessionId.trim() !== '' && pairSecret.trim() !== '')
}

async function scanPairingQRCode(): Promise<string | null> {
  const win = window as Window & {
    BarcodeDetector?: new (options?: { formats?: string[] }) => {
      detect(source: CanvasImageSource): Promise<Array<{ rawValue?: string }>>
    }
  }
  if (!win.BarcodeDetector || !navigator.mediaDevices?.getUserMedia) {
    throw new Error('QR scanning is not available in this browser. Paste the termx://pair content instead.')
  }
  const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
  const video = document.createElement('video')
  video.muted = true
  video.playsInline = true
  video.srcObject = stream
  const detector = new win.BarcodeDetector({ formats: ['qr_code'] })
  try {
    await video.play()
    const deadline = Date.now() + 15000
    while (Date.now() < deadline) {
      const [code] = await detector.detect(video)
      const rawValue = code?.rawValue?.trim()
      if (rawValue) return rawValue
      await delay(250)
    }
    throw new Error('No QR code was detected. Paste the termx://pair content instead.')
  } finally {
    for (const track of stream.getTracks()) track.stop()
    video.srcObject = null
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function createBrowserAppDeviceId(): string {
  const cryptoImpl = globalThis.crypto
  if (cryptoImpl?.randomUUID) {
    return `appweb_${cryptoImpl.randomUUID()}`
  }
  const bytes = new Uint8Array(16)
  cryptoImpl?.getRandomValues?.(bytes)
  if (bytes.some((value) => value !== 0)) {
    return `appweb_${Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')}`
  }
  return `appweb_${Date.now().toString(36)}_${Math.random().toString(36).slice(2)}`
}
