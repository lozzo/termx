import { useCallback, useEffect, useMemo, useState } from 'react'
import { LogIn, QrCode, RefreshCw, Server, Wifi, WifiOff } from 'lucide-react'
import { createMachineStore } from './machineStore'
import { parsePairingPayload, type PairingPayload } from './pairingPayload'
import { createWebControlApi, type WebControlMachine, type WebControlUser } from './webControlApi'

const storageKeys = {
  controlUrl: 'termx.remote.controlUrl',
  accessToken: 'termx.remote.accessToken',
} as const

const defaultWebControlUrl = 'http://114.66.58.243:12306'

export interface WebControlRemoteAppProps {
  defaultControlUrl?: string | undefined
  storage?: Storage | undefined
}

export function WebControlRemoteApp({ defaultControlUrl, storage = globalThis.localStorage }: WebControlRemoteAppProps) {
  const [controlUrl, setControlUrl] = useState(() => initialControlUrl(storage, defaultControlUrl))
  const [login, setLogin] = useState('')
  const [password, setPassword] = useState('')
  const [accessToken, setAccessToken] = useState(() => storage?.getItem(storageKeys.accessToken) ?? '')
  const [user, setUser] = useState<WebControlUser | null>(null)
  const [machines, setMachines] = useState<WebControlMachine[]>([])
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(null)
  const [manualScanValue, setManualScanValue] = useState('')
  const [lastImported, setLastImported] = useState<PairingPayload | null>(null)
  const [pairedMachineIds, setPairedMachineIds] = useState(() => readPairedMachineIds(storage))
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const selectedMachine = machines.find((machine) => machine.id === selectedMachineId) ?? machines[0] ?? null
  const selectedMachinePaired = selectedMachine ? pairedMachineIds.has(selectedMachine.id) : false
  const signedIn = accessToken.trim() !== ''

  const api = useMemo(() => createWebControlApi({
    baseUrl: controlUrl,
    ...(accessToken ? { accessToken } : {}),
  }), [accessToken, controlUrl])

  const refreshMachines = useCallback(async () => {
    if (!accessToken) return
    setLoading(true)
    try {
      const [profile, cloudMachines] = await Promise.all([
        api.me(),
        api.listMachines(),
      ])
      setUser(profile)
      setMachines(cloudMachines)
      setSelectedMachineId((current) => {
        if (current && cloudMachines.some((machine) => machine.id === current)) return current
        return cloudMachines[0]?.id ?? null
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [accessToken, api])

  useEffect(() => {
    void refreshMachines()
  }, [refreshMachines])

  useEffect(() => {
    setPairedMachineIds(readPairedMachineIds(storage))
  }, [storage])

  const submitLogin = useCallback(async () => {
    setLoading(true)
    try {
      const auth = await api.login({ login, password })
      storage?.setItem(storageKeys.controlUrl, controlUrl)
      storage?.setItem(storageKeys.accessToken, auth.accessToken)
      setAccessToken(auth.accessToken)
      setUser(auth.user)
      setPassword('')
      setError(null)
      setMessage('Signed in')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [api, controlUrl, login, password, storage])

  const signOut = useCallback(() => {
    storage?.removeItem(storageKeys.accessToken)
    setAccessToken('')
    setUser(null)
    setMachines([])
    setSelectedMachineId(null)
    setMessage(null)
  }, [storage])

  const importManualScan = useCallback(() => {
    if (!signedIn) {
      setError('Sign in before pairing a device')
      setMessage(null)
      return
    }
    if (!storage) {
      setError('Local storage is required before importing a TermX QR')
      setMessage(null)
      return
    }
    try {
      const payload = parsePairingPayload(manualScanValue)
      const cloudMachine = machines.find((machine) => machine.id === payload.machine.id)
      if (!cloudMachine) {
        throw new Error('This pairing code does not match a Web Control device in this account')
      }
      const store = createMachineStore({ storage })
      store.saveFromPairingPayload(payload)
      setSelectedMachineId(cloudMachine.id)
      setPairedMachineIds(readPairedMachineIds(storage))
      setLastImported(payload)
      setError(null)
      setMessage(`Paired ${cloudMachine.name}`)
      setManualScanValue('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setMessage(null)
    }
  }, [manualScanValue, machines, signedIn, storage])

  return (
    <main className="min-h-[100dvh] bg-zinc-50 text-zinc-950" data-testid="termx-web-control-remote">
      <header className="border-b border-zinc-200 bg-white px-4 py-3">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-3">
          <div>
            <h1 className="text-lg font-semibold leading-6">TermX Remote</h1>
            <p className="text-xs font-medium text-zinc-500">{signedIn ? user?.email ?? 'Web Control' : 'Web Control sign in'}</p>
          </div>
          {signedIn ? (
            <button
              className="inline-flex h-9 items-center justify-center rounded-md border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-700 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              type="button"
              onClick={signOut}
            >
              Sign out
            </button>
          ) : null}
        </div>
      </header>

      <div className="mx-auto grid max-w-6xl gap-4 px-4 py-4 lg:grid-cols-[360px_minmax(0,1fr)]">
        <section className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
          <div className="space-y-3">
            <label className="block text-sm font-semibold text-zinc-800">
              Web Control
              <input
                className="mt-1 h-10 w-full rounded-md border border-zinc-300 px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                value={controlUrl}
                onChange={(event) => setControlUrl(event.target.value)}
                placeholder="http://114.66.58.243:12306"
              />
            </label>
            {!signedIn ? (
              <>
                <label className="block text-sm font-semibold text-zinc-800">
                  Email or username
                  <input
                    className="mt-1 h-10 w-full rounded-md border border-zinc-300 px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                    value={login}
                    onChange={(event) => setLogin(event.target.value)}
                    autoComplete="username"
                  />
                </label>
                <label className="block text-sm font-semibold text-zinc-800">
                  Password
                  <input
                    className="mt-1 h-10 w-full rounded-md border border-zinc-300 px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    type="password"
                    autoComplete="current-password"
                  />
                </label>
                <button
                  className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
                  type="button"
                  onClick={submitLogin}
                  disabled={loading}
                >
                  <LogIn className="h-4 w-4" />
                  Sign in
                </button>
              </>
            ) : (
              <button
                className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-800 hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-60"
                type="button"
                onClick={() => void refreshMachines()}
                disabled={loading}
              >
                <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                Refresh
              </button>
            )}
          </div>

          {error ? <p className="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm font-medium text-red-700">{error}</p> : null}
          {message ? <p className="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-700">{message}</p> : null}
        </section>

        <section className="grid min-h-[560px] gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
          <div className="rounded-lg border border-zinc-200 bg-white shadow-sm">
            <div className="flex items-center justify-between border-b border-zinc-200 px-4 py-3">
              <div>
                <h2 className="text-base font-semibold">Machines</h2>
                <p className="text-xs font-medium text-zinc-500">{machines.length} available</p>
              </div>
            </div>
            {!signedIn ? (
              <div className="flex min-h-[320px] items-center justify-center p-6 text-center text-sm text-zinc-500">
                <div>
                  <Server className="mx-auto mb-3 h-8 w-8 text-zinc-400" />
                  Sign in to view your devices.
                </div>
              </div>
            ) : machines.length === 0 ? (
              <div className="flex min-h-[320px] items-center justify-center p-6 text-center text-sm text-zinc-500">
                <div>
                  <Server className="mx-auto mb-3 h-8 w-8 text-zinc-400" />
                  No devices in this account.
                </div>
              </div>
            ) : (
              <ul className="divide-y divide-zinc-100">
                {machines.map((machine) => {
                  const locallyPaired = pairedMachineIds.has(machine.id)
                  return (
                    <li key={machine.id}>
                      <button
                        className={`grid w-full grid-cols-[auto_minmax(0,1fr)] gap-3 px-4 py-3 text-left hover:bg-zinc-50 ${machine.id === selectedMachine?.id ? 'bg-blue-50' : ''}`}
                        type="button"
                        onClick={() => setSelectedMachineId(machine.id)}
                      >
                        <div className="flex h-10 w-10 items-center justify-center rounded-md bg-zinc-100 text-zinc-600">
                          {machine.online ? <Wifi className="h-5 w-5" /> : <WifiOff className="h-5 w-5" />}
                        </div>
                        <div className="min-w-0">
                          <div className="flex items-center justify-between gap-2">
                            <span className="truncate text-sm font-semibold text-zinc-950">{machine.name}</span>
                            <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold ring-1 ${machine.online ? 'bg-emerald-50 text-emerald-700 ring-emerald-200' : 'bg-zinc-100 text-zinc-600 ring-zinc-200'}`}>
                              {machine.online ? 'Online' : 'Offline'}
                            </span>
                          </div>
                          <p className="mt-0.5 truncate text-xs font-medium text-zinc-500">{machine.hostname || machine.id}</p>
                          <div className="mt-2 flex flex-wrap gap-1.5">
                            <InfoPill>Public P2P</InfoPill>
                            <InfoPill>{`${machine.terminalCount} terminals`}</InfoPill>
                            <InfoPill>{locallyPaired ? 'Ready' : 'Scan QR'}</InfoPill>
                          </div>
                        </div>
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </div>

          <aside className="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
            <div className="flex items-center gap-2">
              <QrCode className="h-5 w-5 text-zinc-600" />
              <h2 className="text-base font-semibold">Pair Device</h2>
            </div>
            {signedIn && selectedMachine ? (
              <div className="mt-4 space-y-4">
                <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
                  <div className="truncate text-sm font-semibold text-zinc-950">{selectedMachine.name}</div>
                  <div className="mt-0.5 truncate text-xs font-medium text-zinc-500">{selectedMachine.hostname || selectedMachine.id}</div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <InfoPill>{selectedMachine.online ? 'Online' : 'Offline'}</InfoPill>
                    <InfoPill>{selectedMachinePaired ? 'Ready' : 'Scan QR'}</InfoPill>
                  </div>
                </div>
                <label className="block text-xs font-semibold text-zinc-600">
                  TermX QR content
                  <textarea
                    className="mt-1 h-44 w-full resize-none rounded-md border border-zinc-300 p-2 font-mono text-xs leading-5 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                    value={manualScanValue}
                    onChange={(event) => setManualScanValue(event.target.value)}
                    placeholder="termx://pair?payload=..."
                    spellCheck={false}
                  />
                </label>
                <button
                  className="inline-flex h-10 w-full items-center justify-center rounded-md bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-60"
                  type="button"
                  onClick={importManualScan}
                  disabled={manualScanValue.trim() === ''}
                >
                  Import Pairing Code
                </button>
                {lastImported && lastImported.machine.id === selectedMachine.id ? (
                  <div className="rounded-md bg-emerald-50 px-3 py-2 text-xs font-medium text-emerald-700">
                    <div className="truncate text-emerald-900">{lastImported.machine.name}</div>
                    <div className="truncate">{lastImported.machine.id}</div>
                  </div>
                ) : null}
              </div>
            ) : signedIn ? (
              <p className="mt-4 text-sm leading-5 text-zinc-500">Select a device to pair.</p>
            ) : (
              <p className="mt-4 text-sm leading-5 text-zinc-500">Sign in to pair a device.</p>
            )}
          </aside>
        </section>
      </div>
    </main>
  )
}

function InfoPill({ children }: { children: string }) {
  return (
    <span className="inline-flex h-6 items-center rounded-md bg-zinc-100 px-2 text-[11px] font-semibold text-zinc-600">
      {children}
    </span>
  )
}

function initialControlUrl(storage: Storage | undefined, fallback: string | undefined): string {
  const fromQuery = new URLSearchParams(globalThis.location?.search ?? '').get('control')
  const queryValue = cleanControlUrl(fromQuery)
  if (queryValue) return queryValue
  const storedValue = cleanControlUrl(storage?.getItem(storageKeys.controlUrl))
  if (storedValue && !isRemoteUiLocalUrl(storedValue)) return storedValue
  return cleanControlUrl(fallback) || defaultWebControlUrl
}

function cleanControlUrl(value: string | null | undefined): string {
  return value?.trim() ?? ''
}

function isRemoteUiLocalUrl(value: string): boolean {
  try {
    const url = new URL(value)
    const host = url.hostname.toLowerCase()
    return (host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1') &&
      (url.port === '5173' || url.port === '5174' || url.port === '18888')
  } catch {
    return false
  }
}

function readPairedMachineIds(storage: Storage | undefined): Set<string> {
  if (!storage) return new Set()
  try {
    return new Set(createMachineStore({ storage }).listMachines().map((machine) => machine.machineId))
  } catch {
    return new Set()
  }
}
