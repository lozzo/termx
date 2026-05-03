import {
  Activity,
  Cable,
  Database,
  KeyRound,
  Laptop,
  Network,
  RefreshCw,
  ShieldCheck,
  TerminalSquare,
} from 'lucide-react'
import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useState } from 'react'

const paths = ['local', 'public_p2p', 'managed'] as const
const accessTokenKey = 'termx.webControl.accessToken'

type HealthResponse = {
  service?: string
  status?: string
  version?: string
  runtime?: string
  transport?: string
}

type User = {
  id: string
  email: string
  role: string
}

type Plan = {
  id: string
  name: string
  allow_public_p2p: boolean
  allow_relay: boolean
  monthly_relay_bytes: number
  relay_session_limit: number
  relay_throttle_bps?: number
  relay_transfer_files: boolean
}

type AuthResponse = {
  user: User
  plan: Plan
  access_token?: string
  refresh_token?: string
}

type Machine = {
  id: string
  display_name?: string
  hostname?: string
  platform?: string
  last_seen_at?: string
}

type Terminal = {
  id: string
  machine_id: string
  name?: string
  state?: string
  last_seen_at?: string
}

type ManagedTicket = {
  id: string
  machine_id: string
  terminal_id: string
  path: 'managed'
  allow_relay: boolean
  relay_in_use: boolean
  relay_bytes_remaining: number
  relay_throttled: boolean
  expires_at: string
}

type ConsoleState = {
  health: HealthResponse | null
  user: User | null
  plan: Plan | null
  machines: Machine[]
  terminals: Terminal[]
  ticket: ManagedTicket | null
  loading: boolean
  message: string
}

const initialState: ConsoleState = {
  health: null,
  user: null,
  plan: null,
  machines: [],
  terminals: [],
  ticket: null,
  loading: false,
  message: '',
}

export function App() {
  const [state, setState] = useState<ConsoleState>(initialState)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [authMode, setAuthMode] = useState<'login' | 'register'>('login')
  const [accessToken, setAccessToken] = useState(() => readStoredToken())

  const authedHeaders = useMemo(
    (): Record<string, string> => (accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
    [accessToken],
  )

  const loadInventory = useCallback(
    async (token: string) => {
      const headers = { Authorization: `Bearer ${token}` }
      const [devices, terminals] = await Promise.all([
        apiGet<{ devices: Machine[] }>('/api/devices', headers),
        apiGet<{ terminals: Terminal[] }>('/api/terminals', headers),
      ])
      setState((current) => ({
        ...current,
        machines: devices.devices ?? [],
        terminals: terminals.terminals ?? [],
      }))
    },
    [],
  )

  const loadSession = useCallback(
    async (token: string) => {
      setState((current) => ({ ...current, loading: true, message: '' }))
      try {
        const [me] = await Promise.all([
          apiGet<AuthResponse>('/api/v1/auth/me', { Authorization: `Bearer ${token}` }),
          loadInventory(token),
        ])
        setState((current) => ({
          ...current,
          user: me.user,
          plan: me.plan,
          loading: false,
        }))
      } catch (error) {
        clearStoredToken()
        setAccessToken('')
        setState((current) => ({
          ...current,
          loading: false,
          message: errorMessage(error),
        }))
      }
    },
    [loadInventory],
  )

  useEffect(() => {
    let cancelled = false
    apiGet<HealthResponse>('/api/health')
      .then((health) => {
        if (!cancelled) {
          setState((current) => ({ ...current, health }))
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setState((current) => ({ ...current, message: errorMessage(error) }))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (accessToken) {
      void loadSession(accessToken)
    }
  }, [accessToken, loadSession])

  async function handleLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setState((current) => ({ ...current, loading: true, message: '' }))
    try {
      const auth = await apiPost<AuthResponse>(
        authMode === 'register' ? '/api/v1/auth/register' : '/api/v1/auth/login',
        { email, password },
      )
      const token = auth.access_token ?? ''
      storeToken(token)
      setAccessToken(token)
      await loadInventory(token)
      setState((current) => ({
        ...current,
        user: auth.user,
        plan: auth.plan,
        loading: false,
        message: '',
      }))
    } catch (error) {
      setState((current) => ({ ...current, loading: false, message: errorMessage(error) }))
    }
  }

  async function createManagedTicket(terminal: Terminal) {
    setState((current) => ({ ...current, loading: true, message: '' }))
    try {
      const ticket = await apiPost<ManagedTicket>(
        '/api/v1/managed/connect-tickets',
        {
          machine_id: terminal.machine_id,
          terminal_id: terminal.id,
          ttl_seconds: 300,
        },
        authedHeaders,
      )
      setState((current) => ({
        ...current,
        ticket,
        loading: false,
        message: 'Managed ticket created for signaling inspection',
      }))
    } catch (error) {
      setState((current) => ({ ...current, loading: false, message: errorMessage(error) }))
    }
  }

  function logout() {
    clearStoredToken()
    setAccessToken('')
    setState((current) => ({
      ...current,
      user: null,
      plan: null,
      machines: [],
      terminals: [],
      ticket: null,
      message: '',
    }))
  }

  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-100">
      <section className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-6 px-5 py-5 sm:px-8">
        <header className="flex flex-col gap-4 border-b border-zinc-800 pb-5 lg:flex-row lg:items-end lg:justify-between">
          <div className="flex flex-col gap-2">
            <p className="text-sm font-medium uppercase tracking-normal text-emerald-300">
              Control Plane
            </p>
            <h1 className="text-3xl font-semibold tracking-normal text-white">
              TermX Control
            </h1>
            <p className="max-w-3xl text-sm leading-6 text-zinc-400">
              Manage users, machines, hub policy, public P2P rendezvous, and paid managed relay
              controls without becoming a runtime terminal proxy.
            </p>
          </div>
          {state.user ? (
            <div className="flex flex-wrap items-center gap-3 text-sm">
              <span className="text-zinc-300">{state.user.email}</span>
              <button
                type="button"
                onClick={logout}
                className="border border-zinc-700 px-3 py-2 font-medium text-zinc-200 hover:border-zinc-500"
              >
                Sign out
              </button>
            </div>
          ) : null}
        </header>

        <div className="grid gap-3 md:grid-cols-3">
          <StatusPanel
            icon={<Activity aria-hidden="true" className="h-5 w-5" />}
            title="Backend"
            value={state.health?.service ?? 'Go health API'}
            detail={state.health?.status ?? 'checking'}
          />
          <StatusPanel
            icon={<Database aria-hidden="true" className="h-5 w-5" />}
            title="Database"
            value="SQLite dev database"
            detail={state.health?.runtime ?? 'control-plane'}
          />
          <StatusPanel
            icon={<Network aria-hidden="true" className="h-5 w-5" />}
            title="Runtime"
            value="WebRTC DataChannel runtime only"
            detail={state.health?.transport ?? 'signaling-control-only'}
          />
        </div>

        <div className="grid gap-4 lg:grid-cols-[360px_1fr]">
          <aside className="flex flex-col gap-4">
            <Panel title="Account" icon={<KeyRound aria-hidden="true" className="h-5 w-5" />}>
              {state.user ? (
                <div className="flex flex-col gap-3 text-sm">
                  <InfoRow label="Signed in" value={state.user.email} />
                  <InfoRow label="Plan" value={state.plan?.name ?? state.plan?.id ?? 'unknown'} />
                  <InfoRow
                    label="Managed relay"
                    value={
                      state.plan?.allow_relay
                        ? 'Managed relay allowed'
                        : 'Managed relay unavailable on this plan'
                    }
                  />
                </div>
              ) : (
                <form className="flex flex-col gap-3" onSubmit={handleLogin}>
                  <div className="grid grid-cols-2 border border-zinc-800">
                    <button
                      type="button"
                      onClick={() => setAuthMode('login')}
                      className={`px-3 py-2 text-sm font-medium ${
                        authMode === 'login'
                          ? 'bg-emerald-500 text-zinc-950'
                          : 'text-zinc-300 hover:bg-zinc-800'
                      }`}
                    >
                      Log in
                    </button>
                    <button
                      type="button"
                      onClick={() => setAuthMode('register')}
                      className={`px-3 py-2 text-sm font-medium ${
                        authMode === 'register'
                          ? 'bg-emerald-500 text-zinc-950'
                          : 'text-zinc-300 hover:bg-zinc-800'
                      }`}
                    >
                      Register
                    </button>
                  </div>
                  <label className="flex flex-col gap-1 text-sm text-zinc-300">
                    Email
                    <input
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                      className="border border-zinc-700 bg-zinc-950 px-3 py-2 text-zinc-100 outline-none focus:border-emerald-400"
                      autoComplete="email"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-sm text-zinc-300">
                    Password
                    <input
                      type="password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      className="border border-zinc-700 bg-zinc-950 px-3 py-2 text-zinc-100 outline-none focus:border-emerald-400"
                      autoComplete="current-password"
                    />
                  </label>
                  <button
                    type="submit"
                    aria-label={authMode === 'register' ? 'Create account' : 'Submit login'}
                    className="flex items-center justify-center gap-2 border border-emerald-500 bg-emerald-500 px-3 py-2 text-sm font-semibold text-zinc-950 hover:bg-emerald-400"
                    disabled={state.loading}
                  >
                    <ShieldCheck aria-hidden="true" className="h-4 w-4" />
                    {authMode === 'register' ? 'Create account' : 'Log in'}
                  </button>
                </form>
              )}
            </Panel>

            <Panel title="Connection paths" icon={<Cable aria-hidden="true" className="h-5 w-5" />}>
              <div className="flex flex-wrap gap-2">
                {paths.map((path) => (
                  <span
                    key={path}
                    className="border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm font-medium text-zinc-200"
                  >
                    {path}
                  </span>
                ))}
              </div>
              <p className="text-sm leading-6 text-zinc-400">
                Relay is reported as policy and quota state only. Public STUN-only runtime can still
                fail to open a terminal DataChannel across NAT.
              </p>
            </Panel>
          </aside>

          <section className="flex flex-col gap-4">
            {state.message ? (
              <div className="border border-amber-500/60 bg-amber-500/10 px-4 py-3 text-sm text-amber-100">
                {state.message}
              </div>
            ) : null}

            <Panel
              title="Machines"
              icon={<Laptop aria-hidden="true" className="h-5 w-5" />}
              action={
                state.user ? (
                  <button
                    type="button"
                    onClick={() => void loadInventory(accessToken)}
                    className="inline-flex items-center gap-2 border border-zinc-700 px-3 py-2 text-sm font-medium text-zinc-200 hover:border-zinc-500"
                  >
                    <RefreshCw aria-hidden="true" className="h-4 w-4" />
                    Refresh
                  </button>
                ) : null
              }
            >
              {state.machines.length === 0 ? (
                <p className="text-sm text-zinc-400">No registered daemon machines for this user.</p>
              ) : (
                <div className="grid gap-3 md:grid-cols-2">
                  {state.machines.map((machine) => (
                    <article key={machine.id} className="border border-zinc-800 bg-zinc-950 p-3">
                      <h3 className="text-sm font-semibold text-zinc-100">
                        {machine.display_name || machine.hostname || machine.id}
                      </h3>
                      <p className="mt-2 break-all text-sm text-zinc-400">{machine.id}</p>
                      <p className="mt-2 text-sm text-zinc-500">
                        {[machine.hostname, machine.platform].filter(Boolean).join(' / ') ||
                          'metadata pending'}
                      </p>
                    </article>
                  ))}
                </div>
              )}
            </Panel>

            <Panel
              title="Terminals"
              icon={<TerminalSquare aria-hidden="true" className="h-5 w-5" />}
            >
              {state.terminals.length === 0 ? (
                <p className="text-sm text-zinc-400">No terminals reported by the daemon.</p>
              ) : (
                <div className="flex flex-col gap-3">
                  {state.terminals.map((terminal) => (
                    <article
                      key={`${terminal.machine_id}:${terminal.id}`}
                      className="grid gap-3 border border-zinc-800 bg-zinc-950 p-3 md:grid-cols-[1fr_auto]"
                    >
                      <div>
                        <h3 className="text-sm font-semibold text-zinc-100">
                          Terminal {terminal.id}
                        </h3>
                        <p className="mt-1 break-all text-sm text-zinc-400">
                          {terminal.machine_id}
                        </p>
                        <p className="mt-1 text-sm text-zinc-500">
                          {terminal.name || 'shell'} / {terminal.state || 'unknown'}
                        </p>
                      </div>
                      <button
                        type="button"
                        onClick={() => void createManagedTicket(terminal)}
                        className="inline-flex min-w-48 items-center justify-center gap-2 border border-emerald-500 px-3 py-2 text-sm font-semibold text-emerald-200 hover:bg-emerald-500/10"
                        disabled={!state.user || state.loading}
                      >
                        <Cable aria-hidden="true" className="h-4 w-4" />
                        Create managed ticket for Terminal {terminal.id}
                      </button>
                    </article>
                  ))}
                </div>
              )}
            </Panel>

            {state.ticket ? (
              <Panel title="Managed Ticket" icon={<Cable aria-hidden="true" className="h-5 w-5" />}>
                <div className="grid gap-3 text-sm md:grid-cols-2">
                  <InfoRow label="Ticket" value={state.ticket.id} />
                  <InfoRow label="Path" value={state.ticket.path} />
                  <InfoRow label="Machine" value={state.ticket.machine_id} />
                  <InfoRow label="Terminal" value={state.ticket.terminal_id} />
                  <InfoRow
                    label="Relay policy"
                    value={state.ticket.allow_relay ? 'relay allowed by policy' : 'relay denied'}
                  />
                  <InfoRow
                    label="Expires"
                    value={new Date(state.ticket.expires_at).toLocaleString()}
                  />
                </div>
              </Panel>
            ) : null}
          </section>
        </div>
      </section>
    </main>
  )
}

function StatusPanel({
  icon,
  title,
  value,
  detail,
}: {
  icon: ReactNode
  title: string
  value: string
  detail: string
}) {
  return (
    <article className="flex min-h-28 flex-col justify-between border border-zinc-800 bg-zinc-900 p-4">
      <div className="flex items-center gap-2 text-emerald-300">
        {icon}
        <h2 className="text-sm font-medium text-zinc-300">{title}</h2>
      </div>
      <div className="flex flex-col gap-1">
        <p className="break-all text-lg font-semibold text-white">{value}</p>
        <p className="break-all text-xs text-zinc-500">{detail}</p>
      </div>
    </article>
  )
}

function Panel({
  title,
  icon,
  action,
  children,
}: {
  title: string
  icon: ReactNode
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="border border-zinc-800 bg-zinc-900 p-4">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-emerald-300">
          {icon}
          <h2 className="text-base font-semibold text-zinc-100">{title}</h2>
        </div>
        {action}
      </div>
      <div className="flex flex-col gap-3">{children}</div>
    </section>
  )
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium uppercase tracking-normal text-zinc-500">{label}</span>
      <span className="break-all text-sm text-zinc-200">{value}</span>
    </div>
  )
}

async function apiGet<T>(path: string, headers: Record<string, string> = {}): Promise<T> {
  const response = await fetch(path, { headers })
  return decodeResponse<T>(response)
}

async function apiPost<T>(
  path: string,
  body: unknown,
  headers: Record<string, string> = {},
): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: {
      ...headers,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })
  return decodeResponse<T>(response)
}

async function decodeResponse<T>(response: Response): Promise<T> {
  let body: unknown = null
  const text = await response.text()
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      body = text
    }
  }
  if (!response.ok) {
    const message =
      typeof body === 'object' &&
      body !== null &&
      'error' in body &&
      typeof (body as { error?: { message?: unknown } }).error?.message === 'string'
        ? (body as { error: { message: string } }).error.message
        : `request failed with ${response.status}`
    throw new Error(message)
  }
  return body as T
}

function readStoredToken() {
  try {
    return window.localStorage.getItem(accessTokenKey) ?? ''
  } catch {
    return ''
  }
}

function storeToken(token: string) {
  try {
    window.localStorage.setItem(accessTokenKey, token)
  } catch {
    // Private browsing or locked-down test environments may deny storage.
  }
}

function clearStoredToken() {
  try {
    window.localStorage.removeItem(accessTokenKey)
  } catch {
    // Ignore unavailable local storage.
  }
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'request failed'
}
