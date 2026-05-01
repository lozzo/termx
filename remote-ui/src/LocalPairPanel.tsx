import { useState } from 'react'
import { pairLocalApp, type LocalAppCrypto, type LocalAppIdentityStore } from './localAppIdentity'
import type { LocalAgentApi } from './transport'

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
    <section
      className={className}
      data-testid="termx-local-pair-panel"
    >
      <form className="grid gap-3 md:grid-cols-[1fr_1fr_auto]" onSubmit={(event) => { void submit(event) }}>
        <label className="grid gap-1 text-sm font-medium text-zinc-700">
          Pair ID
          <input
            autoComplete="off"
            className="min-h-10 rounded-md border border-slate-300 bg-white px-3 text-sm text-zinc-950 outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
            name="pairSessionId"
            value={pairSessionId}
            onChange={(event) => setPairSessionId(event.currentTarget.value)}
          />
        </label>
        <label className="grid gap-1 text-sm font-medium text-zinc-700">
          Pair secret
          <input
            autoComplete="off"
            className="min-h-10 rounded-md border border-slate-300 bg-white px-3 text-sm text-zinc-950 outline-none focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
            name="pairSecret"
            type="password"
            value={pairSecret}
            onChange={(event) => setPairSecret(event.currentTarget.value)}
          />
        </label>
        <button
          className="min-h-10 self-end rounded-md border border-slate-800 bg-slate-900 px-4 text-sm font-medium text-white disabled:cursor-not-allowed disabled:border-slate-300 disabled:bg-slate-200 disabled:text-slate-500"
          type="submit"
          disabled={submitting || !pairSessionId.trim() || !pairSecret.trim()}
        >
          Pair
        </button>
      </form>
      {status ? <div className="mt-3 text-sm text-emerald-700" role="status">{status}</div> : null}
      {error ? <div className="mt-3 text-sm text-red-700" role="alert">{error}</div> : null}
    </section>
  )
}
