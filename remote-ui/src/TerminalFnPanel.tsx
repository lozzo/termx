import { Settings } from 'lucide-react'
import { useMemo, useState } from 'react'
import { matchTerminalFnPreset, SYSTEM_FN_GROUPS, type TerminalFnGroup } from './terminalFnPresets'

export interface TerminalFnPanelProps {
  command?: string | undefined
  onSend: (data: string) => void
}

export function TerminalFnPanel({ command, onSend }: TerminalFnPanelProps) {
  const programPreset = useMemo(() => matchTerminalFnPreset(command), [command])
  const [activeTab, setActiveTab] = useState<'program' | 'system'>(programPreset ? 'program' : 'system')
  const selectedTab = programPreset && activeTab === 'program' ? 'program' : 'system'
  const groups = selectedTab === 'program' ? programPreset?.groups ?? SYSTEM_FN_GROUPS : SYSTEM_FN_GROUPS

  return (
    <div className="border-t border-zinc-800/80 bg-zinc-950/90 text-zinc-100 shadow-2xl backdrop-blur-lg md:hidden" data-testid="termx-fn-panel">
      <div className="max-h-[42vh] overflow-y-auto px-2 py-2">
        <div className="mb-2 flex items-center gap-1.5">
          {programPreset ? (
            <button
              type="button"
              className={`h-7 rounded-md px-2 text-[10px] font-semibold ${selectedTab === 'program' ? 'bg-blue-500 text-white' : 'bg-zinc-800 text-zinc-300'}`}
              onClick={() => setActiveTab('program')}
            >
              {programPreset.name}
            </button>
          ) : null}
          <button
            type="button"
            className={`h-7 rounded-md px-2 text-[10px] font-semibold ${selectedTab === 'system' ? 'bg-blue-500 text-white' : 'bg-zinc-800 text-zinc-300'}`}
            onClick={() => setActiveTab('system')}
          >
            System
          </button>
          <div className="flex-1" />
          <Settings className="h-3.5 w-3.5 text-zinc-500" />
        </div>
        <div className="space-y-3">
          {groups.map((group) => (
            <FnGroupView key={group.name} group={group} onSend={onSend} />
          ))}
        </div>
      </div>
    </div>
  )
}

function FnGroupView({ group, onSend }: { group: TerminalFnGroup; onSend: (data: string) => void }) {
  return (
    <section>
      <h3 className="mb-1.5 px-0.5 text-[9px] font-bold uppercase tracking-wider text-zinc-500">{group.name}</h3>
      <div className="grid grid-cols-3 gap-1.5">
        {group.items.map((item) => (
          <button
            key={`${group.name}:${item.label}:${item.data}`}
            type="button"
            className="min-h-10 rounded-lg bg-zinc-800 px-2 py-1.5 text-left active:scale-[0.98] active:bg-zinc-700"
            onPointerDown={(event) => event.preventDefault()}
            onClick={() => onSend(item.data)}
          >
            <span className="block truncate font-mono text-[11px] font-semibold text-blue-300">{item.label}</span>
            {item.description ? <span className="mt-0.5 block truncate text-[9px] font-medium text-zinc-500">{item.description}</span> : null}
          </button>
        ))}
      </div>
    </section>
  )
}
