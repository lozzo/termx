import { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, Loader2 } from 'lucide-react'
import { MachineList } from '../machines/MachineList'
import type { AppMachineRecord, ConnectionFlowSnapshot } from '../state/appMachine'
import { formatConnectionStage } from '../state/appMachine'

export interface MachineBrowserShellProps {
  machines: AppMachineRecord[]
  authState?: 'anonymous' | 'signed_in' | undefined
  onAddMachine: () => void
  onScanMachine: () => void
  onStartConnection?: ((machine: AppMachineRecord) => Promise<ConnectionFlowSnapshot> | ConnectionFlowSnapshot) | undefined
  onSignIn?: (() => void) | undefined
  className?: string | undefined
}

export function MachineBrowserShell({
  machines,
  authState,
  onAddMachine,
  onScanMachine,
  onStartConnection,
  onSignIn,
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
      message: 'Checking saved local and LAN addresses.',
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
      .catch((err: unknown) => {
        resultTimerRef.current = window.setTimeout(() => {
          resultTimerRef.current = null
          if (attemptSeqRef.current !== attemptSeq) return
          setConnection({
            stage: 'failed',
            path: 'managed',
            relayInUse: false,
            message: err instanceof Error ? err.message : String(err),
          })
        }, 50)
      })
  }, [clearPendingResult, onStartConnection])

  return (
    <main
      className={`flex h-full min-h-0 flex-col bg-zinc-50 text-zinc-950 ${className ?? ''}`}
      data-testid="termx-remote-app-shell"
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
          authState={authState}
          onAddMachine={onAddMachine}
          onScanMachine={onScanMachine}
          onSelectMachine={selectMachine}
          onSignIn={onSignIn}
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
  const active = connection.stage.startsWith('trying_')
  return (
    <section className="flex min-h-0 flex-1 flex-col bg-zinc-50" data-testid="termx-connection-flow">
      <header className="flex min-h-14 shrink-0 items-center gap-3 border-b border-zinc-200 bg-white px-4 pb-3 pt-[calc(env(safe-area-inset-top)+0.75rem)]">
        <button
          aria-label="Back to machines"
          className="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-200 bg-white text-zinc-700 shadow-sm hover:bg-zinc-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          type="button"
          onClick={onBack}
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="min-w-0">
          <h1 className="truncate text-base font-semibold leading-6 text-zinc-950">{machine.name}</h1>
          <p className="truncate text-xs font-medium text-zinc-500">{machine.hostname ?? machine.machineId}</p>
        </div>
      </header>
      <div className="flex flex-1 items-center justify-center px-4 py-8">
        <div className="w-full max-w-sm rounded-lg border border-zinc-200 bg-white p-5 shadow-sm">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-lg bg-blue-50 text-blue-700">
              {active ? <Loader2 className="h-5 w-5 animate-spin" /> : null}
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-semibold text-zinc-950">{formatConnectionStage(connection.stage)}</h2>
              <p className="mt-1 text-sm leading-5 text-zinc-500">{connection.message ?? 'Preparing connection.'}</p>
            </div>
          </div>
          <div className="mt-4 flex flex-wrap gap-2">
            {connection.path ? (
              <span className="rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600">
                {connection.path === 'public_p2p' ? 'Public P2P' : connection.path === 'managed' ? 'Managed' : 'Local'}
              </span>
            ) : null}
            {connection.relayInUse ? (
              <span className="rounded-md bg-amber-50 px-2 py-1 text-xs font-semibold text-amber-700">Relay active</span>
            ) : null}
          </div>
        </div>
      </div>
    </section>
  )
}
