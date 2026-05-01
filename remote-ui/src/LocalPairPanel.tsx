import { useState } from 'react'
import { pairLocalApp, type LocalAppCrypto, type LocalAppIdentityStore } from './localAppIdentity'
import type { LocalAgentApi } from './transport'

export interface LocalPairPanelProps {
  api: Pick<LocalAgentApi, 'pair'>
  storage: LocalAppIdentityStore
  crypto: LocalAppCrypto
  appName: string
  onPaired?: ((machineId: string) => void) | undefined
}

export function LocalPairPanel({ api, storage, crypto, appName, onPaired }: LocalPairPanelProps) {
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
    <section data-testid="termx-local-pair-panel">
      <form onSubmit={(event) => { void submit(event) }}>
        <label>
          Pair ID
          <input
            autoComplete="off"
            name="pairSessionId"
            value={pairSessionId}
            onChange={(event) => setPairSessionId(event.currentTarget.value)}
          />
        </label>
        <label>
          Pair secret
          <input
            autoComplete="off"
            name="pairSecret"
            type="password"
            value={pairSecret}
            onChange={(event) => setPairSecret(event.currentTarget.value)}
          />
        </label>
        <button type="submit" disabled={submitting || !pairSessionId.trim() || !pairSecret.trim()}>
          Pair
        </button>
      </form>
      {status ? <div role="status">{status}</div> : null}
      {error ? <div role="alert">{error}</div> : null}
    </section>
  )
}
