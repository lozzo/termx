import { useState } from 'react'
import { pairLocalApp, type LocalAppCrypto, type LocalAppIdentityStore } from './localAppIdentity'
import type { LocalAgentApi } from './transport'
import { KeyRound, ShieldCheck, AlertCircle } from 'lucide-react'

export interface LocalPairPanelProps {
  api: Pick<LocalAgentApi, 'pair'>
  storage: LocalAppIdentityStore
  crypto: LocalAppCrypto
  appName: string
  onPaired?: ((machineId: string) => void) | undefined
  className?: string | undefined
}

export function LocalPairPanel({ api, storage, crypto, appName, onPaired, className }: LocalPairPanelProps) {
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
      const result = await pairLocalApp({
        api,
        storage,
        crypto,
        appName,
        pairSessionId,
        pairSecret,
      })
      setStatus(`Paired with ${result.machineId}`)
      onPaired?.(result.machineId)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className={`flex flex-col bg-white rounded-lg border border-zinc-200 shadow-sm p-4 ${className || ''}`} data-testid="termx-local-pair-panel">
      <div className="mb-4 flex items-center gap-2">
        <div className="flex h-8 w-8 items-center justify-center rounded-md bg-zinc-100 text-zinc-600">
           <KeyRound className="h-4 w-4" />
        </div>
        <div>
           <h3 className="text-sm font-semibold text-zinc-900">Authorize Device</h3>
           <p className="text-xs text-zinc-500">Enter credentials to connect</p>
        </div>
      </div>

      <form className="flex flex-col gap-3" onSubmit={(event) => { void submit(event) }}>
        <label className="flex flex-col gap-1.5 text-sm font-medium text-zinc-700">
          Pair ID
          <input
            autoComplete="off"
            className="min-h-10 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 placeholder:text-zinc-400 outline-none transition-colors focus:border-zinc-500 focus:ring-2 focus:ring-zinc-200"
            name="pairSessionId"
            placeholder="e.g. 12345678"
            value={pairSessionId}
            onChange={(event) => setPairSessionId(event.currentTarget.value)}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-sm font-medium text-zinc-700">
          Pair secret
          <input
            autoComplete="off"
            className="min-h-10 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 placeholder:text-zinc-400 outline-none transition-colors focus:border-zinc-500 focus:ring-2 focus:ring-zinc-200"
            name="pairSecret"
            type="password"
            placeholder="••••••••"
            value={pairSecret}
            onChange={(event) => setPairSecret(event.currentTarget.value)}
          />
        </label>
        <button
          className="mt-2 flex min-h-10 w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-4 text-sm font-medium text-white transition-colors hover:bg-zinc-800 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2 disabled:cursor-not-allowed disabled:bg-zinc-200 disabled:text-zinc-500"
          type="submit"
          disabled={submitting || !pairSessionId.trim() || !pairSecret.trim()}
        >
          {submitting ? (
            <>
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-400 border-t-white"></div>
              Pairing...
            </>
          ) : (
            'Pair'
          )}
        </button>
      </form>

      {status && (
        <div className="mt-4 flex items-start gap-2 rounded-md bg-emerald-50 p-3 text-sm text-emerald-800" role="status">
          <ShieldCheck className="h-5 w-5 shrink-0 text-emerald-600" />
          <p className="mt-0.5 leading-tight">{status}</p>
        </div>
      )}
      {error && (
        <div className="mt-4 flex items-start gap-2 rounded-md bg-red-50 p-3 text-sm text-red-800" role="alert">
          <AlertCircle className="h-5 w-5 shrink-0 text-red-600" />
          <p className="mt-0.5 leading-tight">{error}</p>
        </div>
      )}
    </div>
  )
}
