import { useEffect, useState } from 'react'
import { ArrowDownToLine, ArrowUpFromLine, Download, Pause, Play, RotateCw, Trash2, X } from 'lucide-react'
import { hapticImpact } from '../platform/haptics'
import type { TransferInfo } from './fileApi'

interface Props {
  transfers: TransferInfo[]
  hasActiveTransfers: boolean
  onCancel: (id: string) => void
  onDismiss: (id: string) => void
  onPause?: ((id: string) => void | Promise<void>) | undefined
  onResume?: ((id: string) => void | Promise<void>) | undefined
  onResumeAll?: (() => void | Promise<void>) | undefined
  variant?: 'inline' | 'floating' | 'icon' | undefined
  open?: boolean | undefined
  onOpenChange?: ((open: boolean) => void) | undefined
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

function formatSpeed(bytesPerSecond: number): string {
  const speed = Math.max(0, bytesPerSecond)
  if (speed < 1024) return `${Math.round(speed)} B/s`
  if (speed < 1024 * 1024) return `${(speed / 1024).toFixed(1)} KB/s`
  return `${(speed / (1024 * 1024)).toFixed(1)} MB/s`
}

function transferSpeed(transfer: TransferInfo, now: number): string {
  if (transfer.status === 'pending' || transfer.status === 'paused' || transfer.status === 'missing') return '0 B/s'
  const updatedAt = transfer.updatedAt ?? transfer.startedAt
  if (now - updatedAt > 4000) return '0 B/s'
  const measured = transfer.bytesPerSecond
  if (typeof measured === 'number' && Number.isFinite(measured) && measured >= 0) {
    return formatSpeed(measured)
  }
  const elapsed = (now - transfer.startedAt) / 1000
  if (!Number.isFinite(elapsed) || elapsed <= 0) return '0 B/s'
  return formatSpeed(Math.max(0, transfer.transferredSize / elapsed))
}

export function FileTransferPanel({
  transfers,
  hasActiveTransfers,
  onCancel,
  onDismiss,
  onPause,
  onResume,
  onResumeAll,
  variant = 'floating',
  open: controlledOpen,
  onOpenChange,
}: Props) {
  const active = transfers
  const [uncontrolledOpen, setUncontrolledOpen] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const open = controlledOpen ?? uncontrolledOpen
  const setOpen = (next: boolean) => {
    if (controlledOpen === undefined) setUncontrolledOpen(next)
    onOpenChange?.(next)
  }

  useEffect(() => {
    if (active.length === 0) return undefined
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [active.length])

  if (active.length === 0 && !open) return null

  const inProgressCount = active.filter((t) => t.status === 'pending' || t.status === 'transferring').length
  const pausedCount = active.filter((t) => t.status === 'paused' || t.status === 'missing').length
  const failedCount = active.filter((t) => t.status === 'failed' || t.status === 'missing').length
  const completedCount = active.filter((t) => t.status === 'completed').length
  const summaryParts: string[] = []
  if (inProgressCount > 0) summaryParts.push(`${inProgressCount} active`)
  if (pausedCount > 0) summaryParts.push(`${pausedCount} paused`)
  if (failedCount > 0) summaryParts.push(`${failedCount} failed`)
  if (completedCount > 0) summaryParts.push(`${completedCount} done`)
  const summary = summaryParts.join(' · ') || (active.length > 0 ? `${active.length} transfers` : 'No transfers')
  const measurable = active.filter((t) => t.totalSize > 0 && (t.status === 'pending' || t.status === 'transferring' || t.status === 'paused' || t.status === 'missing'))
  const totalSize = measurable.reduce((sum, t) => sum + t.totalSize, 0)
  const totalTransferred = measurable.reduce((sum, t) => sum + Math.min(t.transferredSize, t.totalSize), 0)
  const totalProgress = totalSize > 0 ? (totalTransferred / totalSize) * 100 : 0
  const rootClass = variant === 'inline'
    ? 'border-t border-zinc-200 bg-white'
    : variant === 'icon'
      ? 'relative'
      : 'pointer-events-auto fixed bottom-[calc(env(safe-area-inset-bottom)+1rem)] right-3 z-40'

  return (
    <div className={rootClass}>
      <button
        onClick={() => setOpen(true)}
        aria-label={`Open transfer center, ${summary}`}
        className={variant === 'inline'
          ? 'flex w-full items-center justify-between px-4 py-2 text-left'
          : variant === 'icon'
            ? 'flex h-8 w-8 items-center justify-center rounded-md text-zinc-400 transition-colors active:scale-95 active:bg-zinc-800'
            : 'flex min-h-12 items-center gap-3 rounded-full border border-zinc-200 bg-white px-3 py-2 text-left shadow-[0_12px_36px_rgba(15,23,42,0.18)] active:scale-[0.98]'}
      >
        <span className={variant === 'icon' ? 'relative flex h-8 w-8 items-center justify-center' : 'relative flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-blue-50 text-blue-600'}>
          <Download className={variant === 'icon' ? 'h-4 w-4' : 'h-4 w-4'} />
          {hasActiveTransfers ? <span className="absolute right-0 top-0 h-2.5 w-2.5 rounded-full border-2 border-white bg-emerald-500" /> : null}
        </span>
        {variant === 'icon' ? null : (
          <span className={variant === 'inline' ? 'text-xs font-medium text-zinc-500' : 'min-w-0 pr-1'}>
            <span className="block text-[12px] font-semibold leading-4 text-zinc-900">Data Transfers</span>
            <span className="block text-[11px] font-medium leading-4 text-zinc-500">{summary}</span>
          </span>
        )}
      </button>

      {variant === 'floating' && hasActiveTransfers && totalProgress > 0 ? (
        <div className="absolute inset-x-4 bottom-1 h-0.5 overflow-hidden rounded-full bg-zinc-100">
          <div className="h-full rounded-full bg-blue-500 transition-all duration-500" style={{ width: `${Math.min(100, totalProgress)}%` }} />
        </div>
      ) : null}

      {open ? (
        <TransferCenterDialog
          transfers={active}
          now={now}
          summary={summary}
          onCancel={onCancel}
          onDismiss={onDismiss}
          onPause={onPause}
          onResume={onResume}
          onResumeAll={onResumeAll}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </div>
  )
}

function TransferCenterDialog({
  transfers,
  now,
  summary,
  onCancel,
  onDismiss,
  onPause,
  onResume,
  onResumeAll,
  onClose,
}: {
  transfers: TransferInfo[]
  now: number
  summary: string
  onCancel: (id: string) => void
  onDismiss: (id: string) => void
  onPause?: ((id: string) => void | Promise<void>) | undefined
  onResume?: ((id: string) => void | Promise<void>) | undefined
  onResumeAll?: (() => void | Promise<void>) | undefined
  onClose: () => void
}) {
  const resumableCount = transfers.filter((t) => isResumable(t.status)).length
  return (
    <div className="fixed inset-0 z-50 flex bg-white" role="dialog" aria-modal="true">
      <section className="flex h-full min-h-0 w-full flex-col bg-white">
        <header className="flex shrink-0 items-center justify-between gap-3 border-b border-zinc-200 bg-white px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
          <div className="min-w-0">
            <h2 className="text-[18px] font-semibold text-zinc-950">Data Transfer Center</h2>
            <p className="mt-0.5 text-[12px] font-medium text-zinc-500">{summary}</p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {onResumeAll && resumableCount > 0 ? (
              <button
                type="button"
                aria-label="Resume all transfers"
                className="flex h-9 items-center gap-1.5 rounded-md bg-zinc-900 px-3 text-[12px] font-semibold text-white active:bg-zinc-800"
                onClick={() => { hapticImpact(); onResumeAll() }}
              >
                <RotateCw className="h-4 w-4" />
                Resume All
              </button>
            ) : null}
            <button type="button" aria-label="Close data transfer center" className="flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-100" onClick={onClose}>
              <X className="h-5 w-5" />
            </button>
          </div>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto pb-[env(safe-area-inset-bottom)]">
          {transfers.length === 0 ? (
            <div className="flex h-48 items-center justify-center px-6 text-center text-[14px] font-medium text-zinc-500">
              No transfer tasks.
            </div>
          ) : null}
          {transfers.map((t) => {
            const progress = t.totalSize > 0 ? (t.transferredSize / t.totalSize) * 100 : 0
            const isFailed = t.status === 'failed'
            const isMissing = t.status === 'missing'
            const isActive = t.status === 'pending' || t.status === 'transferring'
            const isPaused = t.status === 'paused' || isMissing
            const isCompleted = t.status === 'completed'
            return (
              <div key={t.id} className="border-b border-zinc-100 px-4 py-3 last:border-b-0">
                <div className="flex items-start gap-3">
                  <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-md ${t.direction === 'download' ? 'bg-blue-50 text-blue-600' : 'bg-violet-50 text-violet-600'}`}>
                    {t.direction === 'download' ? <ArrowDownToLine className="h-4 w-4" /> : <ArrowUpFromLine className="h-4 w-4" />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <p className="truncate text-[13px] font-semibold text-zinc-900">{t.name}</p>
                        <p className="mt-0.5 text-[11px] font-medium text-zinc-500">
                          {isActive && t.totalSize > 0 ? `${formatSize(t.transferredSize)} / ${formatSize(t.totalSize)} · ${transferSpeed(t, now)}` : null}
                          {t.status === 'paused' ? `${formatSize(t.transferredSize)} / ${formatSize(t.totalSize)} · Paused` : null}
                          {isMissing ? `${formatSize(t.transferredSize)} / ${formatSize(t.totalSize)} · File missing` : null}
                          {isCompleted ? `${formatSize(t.totalSize)}${t.savedPath ? ` · ${t.savedPath}` : ' · Saved to Downloads'}` : null}
                          {isFailed ? t.error || 'Transfer failed' : null}
                          {!isActive && !isPaused && !isCompleted && !isFailed && !isMissing ? t.status : null}
                        </p>
                      </div>
                      <div className="flex shrink-0 items-center gap-1">
                        {isActive && onPause ? (
                          <button
                            type="button"
                            aria-label={`Pause ${t.name}`}
                            className="flex h-7 w-7 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-100 active:text-zinc-800"
                            onClick={() => { hapticImpact(); onPause(t.id) }}
                          >
                            <Pause className="h-3.5 w-3.5" />
                          </button>
                        ) : null}
                        {isPaused && onResume ? (
                          <button
                            type="button"
                            aria-label={`${isMissing ? 'Retry' : 'Resume'} ${t.name}`}
                            className="flex h-7 w-7 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-100 active:text-zinc-800"
                            onClick={() => { hapticImpact(); onResume(t.id) }}
                          >
                            <Play className="h-3.5 w-3.5" />
                          </button>
                        ) : null}
                        <button
                          aria-label={`${isActive || t.status === 'paused' ? 'Cancel' : 'Clear'} ${t.name}`}
                          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-zinc-500 active:bg-zinc-100 active:text-zinc-800"
                          onClick={() => {
                            if (isActive || t.status === 'paused') hapticImpact()
                            if (isActive || t.status === 'paused') onCancel(t.id)
                            else onDismiss(t.id)
                          }}
                        >
                          {isActive || t.status === 'paused' ? <X className="h-3.5 w-3.5" /> : <Trash2 className="h-3.5 w-3.5" />}
                        </button>
                      </div>
                    </div>
                    <div className={`mt-2 h-1.5 overflow-hidden rounded-full ${isFailed || isMissing ? 'bg-red-100' : 'bg-zinc-100'}`}>
                      <div
                        className={`h-full rounded-full transition-all duration-500 ${isFailed || isMissing ? 'bg-red-500' : isCompleted ? 'bg-emerald-500' : 'bg-blue-500'}`}
                        style={{ width: `${Math.min(100, isCompleted ? 100 : progress)}%` }}
                      />
                    </div>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      </section>
    </div>
  )
}

function isResumable(status: TransferInfo['status']): boolean {
  return status === 'paused' || status === 'failed' || status === 'missing' || status === 'pending'
}
