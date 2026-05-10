import { Loader2 } from 'lucide-react'
import { connectionPhaseLabel } from '../connection/connectionState'
import type { RtcConnectionPhase } from '../core/transport'

export interface MachineNetworkStatusOverlayProps {
  phase: RtcConnectionPhase | null
  status: string | null
}

export function MachineNetworkStatusOverlay({
  phase,
  status,
}: MachineNetworkStatusOverlayProps) {
  const label = status || connectionPhaseLabel(phase)
  return (
    <div
      className="pointer-events-none absolute inset-0 z-[70] flex items-center justify-center bg-zinc-950/22 px-5 backdrop-blur-[5px]"
      data-testid="termx-machine-network-overlay"
      role="status"
      aria-live="polite"
      aria-busy="true"
    >
      <div className="w-full max-w-sm rounded-lg border border-white/12 bg-zinc-950/82 px-4 py-3 text-zinc-100 shadow-2xl backdrop-blur-xl">
        <div className="flex items-center gap-3">
          <Loader2 className="h-5 w-5 shrink-0 animate-spin text-blue-300" />
          <div className="min-w-0">
            <p className="text-[13px] font-semibold leading-5">Network status</p>
            <p className="mt-0.5 break-words text-[12px] font-medium leading-5 text-zinc-300">{label}</p>
          </div>
        </div>
      </div>
    </div>
  )
}
