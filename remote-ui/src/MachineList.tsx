import { useRef, useState } from 'react'
import { LogIn, Plus, QrCode, Server, Wifi, WifiOff, X } from 'lucide-react'
import type { AppMachineRecord } from './appMachine'
import {
  formatConnectionPath,
  formatLastSeen,
  formatMachineState,
  formatTerminalCount,
} from './appMachine'

export interface MachineListProps {
  machines: AppMachineRecord[]
  authState?: 'anonymous' | 'signed_in' | undefined
  onAddMachine: () => void
  onScanMachine: () => void
  onSelectMachine: (machine: AppMachineRecord) => void
  onSignIn?: (() => void) | undefined
  className?: string | undefined
}

export function MachineList({
  machines,
  authState = 'signed_in',
  onAddMachine,
  onScanMachine,
  onSelectMachine,
  onSignIn,
  className,
}: MachineListProps) {
  const [detailMachine, setDetailMachine] = useState<AppMachineRecord | null>(null)

  return (
    <section
      className={`flex min-h-0 flex-1 flex-col bg-zinc-50 text-zinc-950 ${className ?? ''}`}
      data-testid="termx-machine-list"
    >
      <header className="flex min-h-14 shrink-0 items-center justify-between gap-3 border-b border-zinc-200 bg-white px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)]">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold leading-6 text-zinc-950">Machines</h1>
          <p className="truncate text-xs font-medium text-zinc-500">
            {authState === 'anonymous' ? 'Local and LAN records' : `${machines.length} available`}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            aria-label="Scan pairing QR"
            className="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-200 bg-white text-zinc-700 shadow-sm transition-colors hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onScanMachine}
          >
            <QrCode className="h-5 w-5" />
          </button>
          <button
            aria-label="Add machine"
            className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-zinc-900 text-white shadow-sm transition-colors hover:bg-zinc-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            type="button"
            onClick={onAddMachine}
          >
            <Plus className="h-5 w-5" />
          </button>
        </div>
      </header>

      {machines.length === 0 ? (
        <div className="flex flex-1 items-center justify-center px-4 py-8">
          <div
            className="flex w-full max-w-sm flex-col items-center gap-4 rounded-lg border border-dashed border-zinc-300 bg-white px-5 py-7 text-center"
            data-testid="termx-machine-empty-state"
          >
            <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-zinc-100 text-zinc-500">
              <Server className="h-6 w-6" />
            </div>
            <div className="space-y-1">
              <h2 className="text-base font-semibold text-zinc-950">No machines yet</h2>
              <p className="text-sm leading-5 text-zinc-500">Add or scan a TermX QR to keep a machine here.</p>
            </div>
            <div className="grid w-full grid-cols-2 gap-2">
              <button
                className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-zinc-200 bg-white px-3 text-sm font-semibold text-zinc-800 hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                type="button"
                onClick={onScanMachine}
              >
                <QrCode className="h-4 w-4" />
                Scan
              </button>
              <button
                className="inline-flex h-10 items-center justify-center gap-2 rounded-lg bg-zinc-900 px-3 text-sm font-semibold text-white hover:bg-zinc-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                type="button"
                onClick={onAddMachine}
              >
                <Plus className="h-4 w-4" />
                Add
              </button>
            </div>
            {authState === 'anonymous' ? (
              <button
                className="inline-flex h-9 items-center justify-center gap-2 rounded-lg px-3 text-sm font-semibold text-blue-700 hover:bg-blue-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                type="button"
                onClick={onSignIn}
              >
                <LogIn className="h-4 w-4" />
                Sign in
              </button>
            ) : null}
          </div>
        </div>
      ) : (
        <ul aria-label="Machines" className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
          {machines.map((machine) => (
            <li key={machine.machineId} className="mb-2 last:mb-0">
              <MachineRow
                machine={machine}
                onSelectMachine={onSelectMachine}
                onShowDetails={setDetailMachine}
              />
            </li>
          ))}
        </ul>
      )}
      {detailMachine ? <MachineDetailSheet machine={detailMachine} onClose={() => setDetailMachine(null)} /> : null}
    </section>
  )
}

function MachineRow({
  machine,
  onSelectMachine,
  onShowDetails,
}: {
  machine: AppMachineRecord
  onSelectMachine: (machine: AppMachineRecord) => void
  onShowDetails?: ((machine: AppMachineRecord) => void) | undefined
}) {
  const path = machine.lastConnectionPath ?? machine.preferredPath
  const longPressTimerRef = useRef<number | null>(null)
  const longPressTriggeredRef = useRef(false)

  const clearLongPress = () => {
    if (longPressTimerRef.current !== null) {
      window.clearTimeout(longPressTimerRef.current)
      longPressTimerRef.current = null
    }
  }

  return (
    <button
      aria-label={`Connect to ${machine.name}`}
      className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-3 rounded-lg border border-zinc-200 bg-white px-3 py-3 text-left shadow-sm transition-colors hover:border-zinc-300 hover:bg-zinc-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
      type="button"
      onClick={() => {
        if (longPressTriggeredRef.current) {
          longPressTriggeredRef.current = false
          return
        }
        onSelectMachine(machine)
      }}
      onContextMenu={(event) => {
        if (!onShowDetails) return
        event.preventDefault()
        clearLongPress()
        onShowDetails(machine)
      }}
      onPointerDown={(event) => {
        if (!onShowDetails || event.pointerType === 'mouse') return
        longPressTriggeredRef.current = false
        clearLongPress()
        longPressTimerRef.current = window.setTimeout(() => {
          longPressTriggeredRef.current = true
          onShowDetails(machine)
        }, 450)
      }}
      onPointerUp={clearLongPress}
      onPointerLeave={clearLongPress}
      onPointerCancel={clearLongPress}
    >
      <div className="flex h-11 w-11 items-center justify-center rounded-lg bg-zinc-100 text-zinc-600">
        {machine.state === 'offline' ? <WifiOff className="h-5 w-5" /> : <Wifi className="h-5 w-5" />}
      </div>
      <div className="min-w-0">
        <div className="flex min-w-0 items-center justify-between gap-2">
          <span className="truncate text-[15px] font-semibold leading-5 text-zinc-950">{machine.name}</span>
          <StateBadge state={machine.state} />
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-xs font-medium text-zinc-500">
          <span className="truncate">{machine.hostname ?? machine.machineId}</span>
          <span className="shrink-0 text-zinc-300">/</span>
          <span className="shrink-0">{formatTerminalCount(machine.terminalCount)}</span>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {path ? <InfoPill>{formatConnectionPath(path)}</InfoPill> : null}
          <InfoPill>{machine.source === 'cloud' ? 'Cloud' : machine.source === 'manual' ? 'Manual' : 'Saved'}</InfoPill>
          {machine.relayInUse ? <InfoPill>Relay active</InfoPill> : null}
          <InfoPill>{formatLastSeen(machine.lastSeenAt)}</InfoPill>
        </div>
      </div>
    </button>
  )
}

function MachineDetailSheet({ machine, onClose }: { machine: AppMachineRecord; onClose: () => void }) {
  const path = machine.lastConnectionPath ?? machine.preferredPath
  const fields = [
    ['Name', machine.name],
    ['Machine ID', machine.machineId],
    ['Hostname', machine.hostname ?? '-'],
    ['State', formatMachineState(machine.state)],
    ['Terminals', formatTerminalCount(machine.terminalCount)],
    ['Source', machine.source === 'cloud' ? 'Cloud' : machine.source === 'manual' ? 'Manual' : 'Saved'],
    ['Connection', path ? formatConnectionPath(path) : '-'],
    ['Relay', machine.relayInUse ? 'Active' : 'Inactive'],
    ['Last seen', formatLastSeen(machine.lastSeenAt)],
  ] as const

  return (
    <div className="absolute inset-0 z-40 flex items-end bg-black/40 backdrop-blur-sm md:items-center md:justify-center" data-testid="termx-machine-detail-sheet" onClick={onClose}>
      <section
        className="w-full max-h-[85vh] overflow-hidden rounded-t-2xl bg-white shadow-2xl md:max-w-md md:rounded-2xl"
        onClick={(event) => event.stopPropagation()}
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <header className="flex h-16 items-center justify-between border-b border-zinc-200 px-4">
          <div className="min-w-0">
            <h2 className="truncate text-[17px] font-bold text-zinc-950">{machine.name}</h2>
            <p className="truncate text-xs font-medium text-zinc-500">Machine details</p>
          </div>
          <button
            type="button"
            aria-label="Close machine details"
            className="flex h-9 w-9 items-center justify-center rounded-full bg-zinc-100 text-zinc-500 active:bg-zinc-200"
            onClick={onClose}
          >
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="max-h-[calc(85vh-4rem)] overflow-y-auto p-4">
          <dl className="space-y-2">
            {fields.map(([label, value]) => (
              <div key={label} className="rounded-xl bg-zinc-50 px-3 py-2.5">
                <dt className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500">{label}</dt>
                <dd className="mt-1 break-all font-mono text-[13px] font-medium text-zinc-900">{value}</dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </div>
  )
}

function StateBadge({ state }: { state: AppMachineRecord['state'] }) {
  const tone = state === 'online'
    ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
    : state === 'offline'
      ? 'bg-zinc-100 text-zinc-600 ring-zinc-200'
      : state === 'stale'
        ? 'bg-amber-50 text-amber-700 ring-amber-200'
        : 'bg-blue-50 text-blue-700 ring-blue-200'
  return (
    <span className={`shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold leading-4 ring-1 ${tone}`}>
      {formatMachineState(state)}
    </span>
  )
}

function InfoPill({ children }: { children: string }) {
  return (
    <span className="inline-flex h-6 items-center rounded-md bg-zinc-100 px-2 text-[11px] font-semibold leading-none text-zinc-600">
      {children}
    </span>
  )
}
