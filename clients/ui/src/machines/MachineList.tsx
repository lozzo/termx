import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronRight, LaptopMinimal, QrCode, Server, X } from 'lucide-react'
import { hapticImpact, hapticSelection } from '../platform/haptics'
import type { AppMachineRecord } from '../state/appMachine'
import { anyttyIntlLocale } from '../i18n'

export interface MachineListProps {
  machines: AppMachineRecord[]
  onScanMachine: () => void
  onSelectMachine: (machine: AppMachineRecord) => void
  className?: string | undefined
}

export function MachineList({
  machines,
  onScanMachine,
  onSelectMachine,
  className,
}: MachineListProps) {
  const { t } = useTranslation()
  const [detailMachine, setDetailMachine] = useState<AppMachineRecord | null>(null)
  const onlineMachines = machines.filter((machine) => machine.state === 'online' || machine.state === 'connecting')
  const otherMachines = machines.filter((machine) => machine.state !== 'online' && machine.state !== 'connecting')

  return (
    <section
      className={`anytty-app-page flex min-h-0 flex-1 flex-col ${className ?? ''}`}
      data-testid="anytty-machine-list"
    >
      <header className="anytty-app-header flex min-h-14 shrink-0 items-center justify-between gap-3 border-b px-4 pb-2 pt-[calc(env(safe-area-inset-top)+0.5rem)]">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold leading-6 text-zinc-950">{t('machines.title')}</h1>
          <p className="truncate text-xs font-medium text-zinc-500">
            {t('machines.savedCount', { count: machines.length })}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            aria-label={t('machines.scanPairing')}
            className="anytty-app-icon-button focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--anytty-app-accent)]"
            type="button"
            onClick={() => { hapticImpact(); onScanMachine() }}
          >
            <QrCode className="h-5 w-5" />
          </button>
        </div>
      </header>

      {machines.length === 0 ? (
        <div className="flex flex-1 items-start justify-center pt-16 md:items-center md:py-8 md:pt-8">
          <div
            className="anytty-app-panel flex w-full max-w-md flex-col items-start gap-5 border-x-0 px-6 py-8 text-left sm:border-x"
            data-testid="anytty-machine-empty-state"
          >
            <div className="flex h-12 w-12 items-center justify-center border border-[var(--anytty-app-line)] bg-[var(--anytty-app-soft)] text-[var(--anytty-app-accent)]">
              <Server className="h-6 w-6" />
            </div>
            <div className="space-y-1.5">
              <h2 className="text-base font-semibold text-zinc-950">{t('machines.emptyTitle')}</h2>
              <p className="text-sm leading-5 text-zinc-500">{t('machines.emptyServiceCopy')}</p>
            </div>
            <div className="w-full">
              <button
                className="anytty-app-primary-button h-11 w-full gap-2 px-3 text-sm font-semibold focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--anytty-app-accent)]"
                type="button"
                onClick={() => { hapticImpact(); onScanMachine() }}
              >
                <QrCode className="h-4 w-4" />
                {t('machines.scanService')}
              </button>
            </div>
          </div>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto py-4">
          {onlineMachines.length > 0 ? (
            <MachineSection
              title={t('machines.available')}
              machines={onlineMachines}
              onSelectMachine={onSelectMachine}
              onShowDetails={setDetailMachine}
            />
          ) : null}
          {otherMachines.length > 0 ? (
            <MachineSection
              title={onlineMachines.length > 0 ? t('machines.offline') : t('machines.list')}
              machines={otherMachines}
              onSelectMachine={onSelectMachine}
              onShowDetails={setDetailMachine}
            />
          ) : null}
        </div>
      )}
      {detailMachine ? <MachineDetailSheet machine={detailMachine} onClose={() => setDetailMachine(null)} /> : null}
    </section>
  )
}

function MachineSection({
  title,
  machines,
  onSelectMachine,
  onShowDetails,
}: {
  title: string
  machines: AppMachineRecord[]
  onSelectMachine: (machine: AppMachineRecord) => void
  onShowDetails?: ((machine: AppMachineRecord) => void) | undefined
}) {
  return (
    <section className="mb-5 last:mb-0">
      <div className="mb-2 px-4 text-[10px] font-semibold uppercase text-[var(--anytty-app-muted)]">{title}</div>
      <ul aria-label={title} className="anytty-app-panel overflow-hidden border-x-0 sm:border-x">
        {machines.map((machine, index) => (
          <li key={machine.machineId} className={index > 0 ? 'border-t border-[var(--anytty-app-line)]' : ''}>
            <MachineRow
              machine={machine}
              onSelectMachine={onSelectMachine}
              onShowDetails={onShowDetails}
            />
          </li>
        ))}
      </ul>
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
  const { t } = useTranslation()
  const longPressTimerRef = useRef<number | null>(null)
  const longPressTriggeredRef = useRef(false)
  const subtitle = machine.hostname ?? t('machines.daemonHost')
  const availability = machine.state === 'online'
    ? t('machines.tapToConnect')
    : machine.state === 'connecting'
      ? t('machines.connecting')
      : t('machines.lastOnline', { time: formatMachineTime(machine.lastSeenAt) })
  const sourceLabel = machine.source === 'hub'
    ? t('machines.source.hub')
    : machine.source === 'manual'
      ? t('machines.source.manual')
      : t('machines.source.local')
  const DeviceIcon = machine.source === 'hub' ? Server : LaptopMinimal

  const clearLongPress = () => {
    if (longPressTimerRef.current !== null) {
      window.clearTimeout(longPressTimerRef.current)
      longPressTimerRef.current = null
    }
  }

  return (
    <button
      aria-label={t('machines.connectTo', { name: machine.name })}
      className="grid min-h-[108px] w-full grid-cols-[auto_minmax(0,1fr)] gap-3 px-4 py-3.5 text-left transition-colors duration-200 hover:bg-zinc-50 active:bg-[var(--anytty-app-soft)] focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--anytty-app-accent)]"
      type="button"
      onClick={() => {
        if (longPressTriggeredRef.current) {
          longPressTriggeredRef.current = false
          return
        }
        hapticImpact()
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
          hapticImpact()
          onShowDetails(machine)
        }, 450)
      }}
      onPointerUp={clearLongPress}
      onPointerLeave={clearLongPress}
      onPointerCancel={clearLongPress}
    >
      <div className="relative flex h-11 w-11 items-center justify-center border border-[var(--anytty-app-line)] bg-[var(--anytty-app-soft)] text-zinc-700">
        <DeviceIcon className="h-5 w-5" />
        <span className={`absolute bottom-0.5 right-0.5 h-2.5 w-2.5 border-2 border-white ${
          machine.state === 'online'
            ? 'bg-emerald-500'
            : machine.state === 'connecting'
              ? 'bg-blue-500'
              : machine.state === 'stale'
                ? 'bg-amber-500'
                : 'bg-zinc-400'
        }`} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center justify-between gap-2">
          <span className="truncate text-[15px] font-semibold leading-5 text-zinc-950">{machine.name}</span>
          <StateBadge state={machine.state} />
        </div>
        <div className="mt-1 truncate text-xs font-medium text-zinc-500">{subtitle}</div>
        <div className="mt-2 flex min-w-0 items-center justify-between gap-3">
          <span className={`truncate text-[12px] font-medium ${machine.state === 'online' ? 'text-zinc-900' : machine.state === 'connecting' ? 'text-blue-700' : 'text-zinc-500'}`}>
            {availability}
          </span>
          <div className="flex shrink-0 items-center gap-1.5">
            <InfoPill>{sourceLabel}</InfoPill>
          </div>
        </div>
      </div>
      <div className="flex h-11 w-11 shrink-0 items-center justify-center self-center text-zinc-400">
        <ChevronRight className="h-4 w-4" />
      </div>
    </button>
  )
}

function MachineDetailSheet({ machine, onClose }: { machine: AppMachineRecord; onClose: () => void }) {
  const { t } = useTranslation()
  const fields = [
    [t('machines.fields.name'), machine.name],
    [t('machines.fields.id'), machine.machineId],
    [t('machines.fields.hostname'), machine.hostname ?? '-'],
    [t('machines.fields.state'), t(`machines.state.${machine.state}`)],
    [t('machines.fields.source'), machine.source === 'hub' ? t('machines.source.hub') : machine.source === 'manual' ? t('machines.source.manual') : t('machines.source.local')],
    [t('machines.fields.terminals'), String(machine.terminalCount)],
    [t('machines.fields.path'), machine.lastConnectionPath ?? machine.preferredPath ?? '-'],
    [t('machines.fields.relay'), machine.relayInUse ? t('machines.relayInUse') : t('machines.relayNo')],
    [t('machines.fields.lastOnline'), formatMachineTime(machine.lastSeenAt)],
  ] as const

  return (
    <div className="absolute inset-0 z-40 flex items-end bg-black/40 backdrop-blur-sm md:items-center md:justify-center" data-testid="anytty-machine-detail-sheet" onClick={() => { hapticSelection(); onClose() }}>
      <section
        className="w-full max-h-[85vh] overflow-hidden border-t border-[var(--anytty-app-line)] bg-white md:max-w-md md:border"
        onClick={(event) => event.stopPropagation()}
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <header className="flex h-16 items-center justify-between border-b border-zinc-200 px-4">
          <div className="min-w-0">
            <h2 className="truncate text-[17px] font-bold text-zinc-950">{machine.name}</h2>
            <p className="truncate text-xs font-medium text-zinc-500">{t('machines.details')}</p>
          </div>
          <button
            type="button"
            aria-label={t('machines.closeDetails')}
            className="anytty-app-icon-button border-transparent bg-transparent"
            onClick={() => { hapticSelection(); onClose() }}
          >
            <X className="h-5 w-5" />
          </button>
        </header>
        <div className="max-h-[calc(85vh-4rem)] overflow-y-auto p-4">
          <dl className="border border-[var(--anytty-app-line)]">
            {fields.map(([label, value]) => (
              <div key={label} className="border-b border-[var(--anytty-app-line)] bg-zinc-50 px-3 py-2.5 last:border-b-0">
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
  const { t } = useTranslation()
  const tone = state === 'online'
    ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
    : state === 'offline'
      ? 'border-zinc-200 bg-zinc-100 text-zinc-600'
      : state === 'stale'
        ? 'border-amber-200 bg-amber-50 text-amber-700'
        : 'border-blue-200 bg-blue-50 text-blue-700'
  return (
    <span className={`shrink-0 border px-2 py-0.5 text-[11px] font-semibold leading-4 ${tone}`}>
      {t(`machines.state.${state}`)}
    </span>
  )
}

function InfoPill({ children }: { children: string }) {
  return (
    <span className="inline-flex h-6 items-center border border-[var(--anytty-app-line)] bg-zinc-100 px-2 text-[11px] font-semibold leading-none text-zinc-600">
      {children}
    </span>
  )
}

function formatMachineTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(anyttyIntlLocale(), { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
