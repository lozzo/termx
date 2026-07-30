import { useState } from 'react'
import { anyttyI18n } from '@anytty/ui'

export interface RegistryStartupScreenProps {
  error: string | null
  diagnosticsAvailable: boolean
  onRetry: () => Promise<void>
  onExportDiagnostics: () => Promise<void>
  onResetLocalPairings: () => Promise<void>
}

type StartupAction = 'retry' | 'export' | 'reset' | null

export function RegistryStartupScreen({
  error,
  diagnosticsAvailable,
  onRetry,
  onExportDiagnostics,
  onResetLocalPairings,
}: RegistryStartupScreenProps) {
  const [action, setAction] = useState<StartupAction>(null)
  const [confirmReset, setConfirmReset] = useState(false)
  const [actionFailed, setActionFailed] = useState(false)

  const run = async (nextAction: Exclude<StartupAction, null>, callback: () => Promise<void>) => {
    setAction(nextAction)
    setActionFailed(false)
    try {
      await callback()
    } catch {
      setActionFailed(true)
    } finally {
      setAction(null)
    }
  }

  if (!error) {
    return (
      <section aria-live="polite" className="anytty-app-page flex h-[100dvh] w-screen items-center justify-center bg-[var(--anytty-app-bg)]">
        <span aria-label={anyttyI18n.t('common.loading')} className="anytty-square-spinner h-6 w-6 text-[var(--anytty-app-accent)]" role="status" />
      </section>
    )
  }

  return (
    <main className="anytty-app-page flex min-h-[100dvh] w-full items-center justify-center bg-[var(--anytty-app-bg)] px-4 py-8">
      <section aria-labelledby="registry-startup-title" className="anytty-app-panel w-full max-w-md p-5" data-testid="registry-startup-error">
        <h1 className="text-lg font-semibold text-zinc-950" id="registry-startup-title">{anyttyI18n.t('startup.registryTitle')}</h1>
        <p className="mt-2 text-sm leading-5 text-zinc-600" role="alert">{anyttyI18n.t('startup.registryCopy')}</p>
        <details className="mt-4 border border-[var(--anytty-app-line)] bg-[var(--anytty-app-soft)] px-3 py-2 text-xs text-zinc-600">
          <summary className="min-h-11 cursor-pointer py-3 font-semibold">{anyttyI18n.t('startup.technicalDetails')}</summary>
          <code className="block break-words pb-2 font-mono">{error}</code>
        </details>
        {actionFailed ? <p className="mt-3 text-sm font-medium text-red-600" role="alert">{anyttyI18n.t('startup.actionFailed')}</p> : null}
        <div className="mt-5 grid gap-2 sm:grid-cols-2">
          <button className="anytty-app-primary-button min-h-11 px-4 text-sm font-semibold disabled:opacity-60" disabled={action !== null} type="button" onClick={() => void run('retry', onRetry)}>
            {action === 'retry' ? anyttyI18n.t('startup.retrying') : anyttyI18n.t('startup.retry')}
          </button>
          {diagnosticsAvailable ? (
            <button className="anytty-app-secondary-button min-h-11 px-4 text-sm font-semibold disabled:opacity-60" disabled={action !== null} type="button" onClick={() => void run('export', onExportDiagnostics)}>
              {action === 'export' ? anyttyI18n.t('startup.exporting') : anyttyI18n.t('startup.exportDiagnostics')}
            </button>
          ) : null}
        </div>
        <div className="mt-5 border-t border-[var(--anytty-app-line)] pt-4">
          {confirmReset ? (
            <div role="group" aria-label={anyttyI18n.t('startup.resetLocalPairings')}>
              <p className="text-sm leading-5 text-red-700">{anyttyI18n.t('startup.resetWarning')}</p>
              <div className="mt-3 grid grid-cols-2 gap-2">
                <button className="anytty-app-secondary-button min-h-11 px-3 text-sm font-semibold" disabled={action !== null} type="button" onClick={() => setConfirmReset(false)}>{anyttyI18n.t('common.cancel')}</button>
                <button className="anytty-app-primary-button min-h-11 bg-red-700 px-3 text-sm font-semibold disabled:opacity-60" disabled={action !== null} type="button" onClick={() => void run('reset', onResetLocalPairings)}>
                  {action === 'reset' ? anyttyI18n.t('startup.resetting') : anyttyI18n.t('startup.confirmReset')}
                </button>
              </div>
            </div>
          ) : (
            <button className="min-h-11 w-full border border-red-300 px-4 text-sm font-semibold text-red-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-600" disabled={action !== null} type="button" onClick={() => setConfirmReset(true)}>{anyttyI18n.t('startup.resetLocalPairings')}</button>
          )}
        </div>
      </section>
    </main>
  )
}

export function UnsupportedWebPreview() {
  return (
    <main className="anytty-app-page flex min-h-[100dvh] w-full items-center justify-center bg-[var(--anytty-app-bg)] px-4 py-8">
      <section aria-labelledby="unsupported-preview-title" className="anytty-app-panel w-full max-w-md p-5" data-testid="unsupported-web-preview">
        <h1 className="text-lg font-semibold text-zinc-950" id="unsupported-preview-title">{anyttyI18n.t('startup.previewTitle')}</h1>
        <p className="mt-2 text-sm leading-5 text-zinc-600">{anyttyI18n.t('startup.previewCopy')}</p>
      </section>
    </main>
  )
}
