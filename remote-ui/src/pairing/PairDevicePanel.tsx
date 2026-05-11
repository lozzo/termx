import { useState } from 'react'
import type { MachineSessionStore } from '../state/localAppIdentity'
import type { LocalPairingApi } from '../core/transport'
import { KeyRound, ShieldCheck, AlertCircle } from 'lucide-react'
import { hapticError, hapticSuccess } from '../platform/haptics'

export interface PairDevicePanelProps {
  api: LocalPairingApi
  sessionStore: MachineSessionStore
  appName: string
  machineId?: string | undefined
  onPaired?: ((machineId: string) => void) | undefined
  className?: string | undefined
}

export function PairDevicePanel({ api, sessionStore, appName, machineId, onPaired, className }: PairDevicePanelProps) {
  const [pairSessionId, setPairSessionId] = useState('')
  const [pairSecret, setPairSecret] = useState('')
  const [status, setStatus] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setStatus(null)
    setSubmitting(true)
    try {
      const result = await api.pair({
        ...(machineId ? { machineId } : {}),
        pairSessionId,
        pairSecret,
        appDeviceId: createBrowserAppDeviceId(),
        appName,
        requestedCapabilities: ['terminal', 'file_manager', 'terminal_management'],
      })
      sessionStore.saveSessionToken(result.machineId, result.sessionToken, result.expiresAt)
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
           <p className="text-[13px] font-medium text-zinc-500">Enter credentials to connect</p>
        </div>
      </div>

      <form className="flex flex-col gap-4" onSubmit={(event) => { void submit(event) }}>
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
          className="mt-4 flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-zinc-900 px-4 text-[15px] font-semibold text-white shadow-md transition-all active:scale-[0.98] active:bg-zinc-800 disabled:pointer-events-none disabled:opacity-50"
          type="submit"
          disabled={submitting || !pairSessionId.trim() || !pairSecret.trim()}
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
