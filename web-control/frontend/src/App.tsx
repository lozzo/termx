import { Activity, Database, Network } from 'lucide-react'
import type { ReactNode } from 'react'

const paths = ['local', 'public_p2p', 'managed'] as const

export function App() {
  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-100">
      <section className="mx-auto flex min-h-screen w-full max-w-6xl flex-col gap-8 px-5 py-6 sm:px-8">
        <header className="flex flex-col gap-2 border-b border-zinc-800 pb-5">
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
        </header>

        <div className="grid gap-4 md:grid-cols-3">
          <StatusPanel
            icon={<Activity aria-hidden="true" className="h-5 w-5" />}
            title="Backend"
            value="Go health API"
          />
          <StatusPanel
            icon={<Database aria-hidden="true" className="h-5 w-5" />}
            title="Database"
            value="SQLite dev database"
          />
          <StatusPanel
            icon={<Network aria-hidden="true" className="h-5 w-5" />}
            title="Runtime"
            value="WebRTC DataChannel runtime only"
          />
        </div>

        <section className="flex flex-col gap-3">
          <h2 className="text-base font-semibold text-zinc-100">Connection paths</h2>
          <div className="flex flex-wrap gap-2">
            {paths.map((path) => (
              <span
                key={path}
                className="border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm font-medium text-zinc-200"
              >
                {path}
              </span>
            ))}
          </div>
        </section>
      </section>
    </main>
  )
}

function StatusPanel({
  icon,
  title,
  value,
}: {
  icon: ReactNode
  title: string
  value: string
}) {
  return (
    <article className="flex min-h-28 flex-col justify-between border border-zinc-800 bg-zinc-900 p-4">
      <div className="flex items-center gap-2 text-emerald-300">
        {icon}
        <h2 className="text-sm font-medium text-zinc-300">{title}</h2>
      </div>
      <p className="text-lg font-semibold text-white">{value}</p>
    </article>
  )
}
