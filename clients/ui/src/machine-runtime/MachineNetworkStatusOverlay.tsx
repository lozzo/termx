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
      className="pointer-events-none absolute inset-0 z-[70] flex items-center justify-center bg-[var(--muxvia-overlay)] px-5 backdrop-blur-[5px]"
      data-testid="muxvia-machine-network-overlay"
      role="status"
      aria-live="polite"
      aria-busy="true"
    >
      <div className="w-full max-w-sm border border-[var(--muxvia-border)] bg-[var(--muxvia-surface)] px-4 py-3 text-[var(--muxvia-text)] backdrop-blur-xl">
        <div className="flex items-center gap-3">
          <span className="muxvia-square-spinner h-5 w-5 text-[var(--muxvia-accent)]" aria-hidden="true" />
          <div className="min-w-0">
            <p className="text-[13px] font-semibold leading-5">Network status</p>
            <p className="mt-0.5 break-words text-[12px] font-medium leading-5 text-[var(--muxvia-muted)]">{label}</p>
          </div>
        </div>
      </div>
    </div>
  )
}
