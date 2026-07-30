import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowLeft } from 'lucide-react'
import { MachineList } from '../machines/MachineList'
import { hapticSelection } from '../platform/haptics'
import type { AppMachineRecord, ConnectionFlowSnapshot } from '../state/appMachine'

export interface MachineBrowserShellProps {
  machines: AppMachineRecord[]
  onScanMachine: () => void
  onStartConnection?: ((machine: AppMachineRecord) => Promise<ConnectionFlowSnapshot> | ConnectionFlowSnapshot) | undefined
  className?: string | undefined
}

export function MachineBrowserShell({
  machines,
  onScanMachine,
  onStartConnection,
  className,
}: MachineBrowserShellProps) {
  const [selectedMachine, setSelectedMachine] = useState<AppMachineRecord | null>(null)
  const [connection, setConnection] = useState<ConnectionFlowSnapshot | null>(null)
  const attemptSeqRef = useRef(0)
  const resultTimerRef = useRef<number | null>(null)

  const clearPendingResult = useCallback(() => {
    if (resultTimerRef.current === null) return
    window.clearTimeout(resultTimerRef.current)
    resultTimerRef.current = null
  }, [])

  useEffect(() => {
    return () => {
      attemptSeqRef.current += 1
      clearPendingResult()
    }
  }, [clearPendingResult])

  const selectMachine = useCallback((machine: AppMachineRecord) => {
    const attemptSeq = attemptSeqRef.current + 1
    attemptSeqRef.current = attemptSeq
    clearPendingResult()
    setSelectedMachine(machine)
    setConnection({
      stage: 'trying_local',
      path: 'local',
      relayInUse: false,
    })
    if (!onStartConnection) return
    void Promise.resolve(onStartConnection(machine))
      .then((snapshot) => {
        resultTimerRef.current = window.setTimeout(() => {
          resultTimerRef.current = null
          if (attemptSeqRef.current !== attemptSeq) return
          setConnection(snapshot)
        }, 50)
      })
      .catch(() => {
        resultTimerRef.current = window.setTimeout(() => {
          resultTimerRef.current = null
          if (attemptSeqRef.current !== attemptSeq) return
          setConnection({
            stage: 'failed',
            path: 'hub',
            relayInUse: false,
          })
        }, 50)
      })
  }, [clearPendingResult, onStartConnection])

  return (
    <main
      className={`anytty-app-page flex h-full min-h-0 flex-col ${className ?? ''}`}
      data-testid="anytty-remote-app-shell"
    >
      {selectedMachine && connection ? (
        <ConnectionFlowView
          connection={connection}
          machine={selectedMachine}
          onBack={() => {
            attemptSeqRef.current += 1
            clearPendingResult()
            setSelectedMachine(null)
            setConnection(null)
          }}
        />
      ) : (
        <MachineList
          machines={machines}
          onScanMachine={onScanMachine}
          onSelectMachine={selectMachine}
        />
      )}
    </main>
  )
}

function ConnectionFlowView({
  connection,
  machine,
  onBack,
}: {
  connection: ConnectionFlowSnapshot
  machine: AppMachineRecord
  onBack: () => void
}) {
  const { t } = useTranslation()
  const active = connection.stage.startsWith('trying_')
  const failed = connection.stage === 'failed'
  const title = active
    ? t('workspace.connection.progressTitle')
    : failed
      ? t('errors.connectionProblemTitle')
      : t('workspace.connection.phase.connected')
  const message = active
    ? t('workspace.connection.phase.probing')
    : failed
      ? t('errors.connectionInterrupted')
      : t('workspace.connection.phase.connected')
  return (
    <section className="anytty-app-page flex min-h-0 flex-1 flex-col animate-in fade-in slide-in-from-right-4 duration-200" data-testid="anytty-connection-flow">
      <header className="anytty-app-header flex min-h-14 shrink-0 items-center gap-3 border-b px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
        <button
          aria-label={t('common.backToMachines')}
          className="anytty-app-icon-button focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--anytty-app-accent)]"
          type="button"
          onClick={() => { hapticSelection(); onBack() }}
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="min-w-0">
          <h1 className="truncate text-base font-semibold leading-6 text-zinc-950">{machine.name}</h1>
          <p className="truncate text-xs font-medium text-zinc-500">{machine.hostname ?? machine.machineId}</p>
        </div>
      </header>
      <div className="flex flex-1 items-center justify-center px-4 py-8">
        <div className="anytty-app-panel w-full max-w-sm p-5">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center border border-blue-200 bg-blue-50 text-blue-700">
              {active ? <span className="anytty-square-spinner h-5 w-5" aria-hidden="true" /> : null}
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-semibold text-zinc-950">{title}</h2>
              <p className="mt-1 text-sm leading-5 text-zinc-500">{message}</p>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
