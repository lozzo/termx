import type { ReactNode } from 'react'
import { Clipboard, Copy, MousePointer2, PanelTopOpen, X } from 'lucide-react'

export type TerminalToolbarMode = 'default' | 'selection'

export interface TerminalActionToolbarProps {
  mode: TerminalToolbarMode
  hasSelection: boolean
  onModeChange: (mode: TerminalToolbarMode) => void
  onSelectAll: () => void
  onSelectVisible: () => void
  onCopy: () => void
  onPaste: () => void
  onOpenSnippets: () => void
}

export function TerminalActionToolbar({
  mode,
  hasSelection,
  onModeChange,
  onSelectAll,
  onSelectVisible,
  onCopy,
  onPaste,
  onOpenSnippets,
}: TerminalActionToolbarProps) {
  if (mode === 'selection') {
    return (
      <div className="absolute inset-x-0 top-10 z-40 border-y border-zinc-800/80 bg-zinc-950/90 px-1.5 py-1.5 text-zinc-100 shadow-lg backdrop-blur-lg md:hidden">
        <div className="flex items-center gap-1.5 overflow-x-auto">
          <ToolbarButton label="All" onClick={onSelectAll} />
          <ToolbarButton label="Visible" onClick={onSelectVisible} />
          <div className="h-5 w-px shrink-0 bg-zinc-800" />
          <ToolbarButton
            label="Copy"
            icon={<Copy className="h-3 w-3" />}
            onClick={onCopy}
            disabled={!hasSelection}
            primary={hasSelection}
          />
          <div className="flex-1" />
          <ToolbarIconButton label="Close selection tools" onClick={() => onModeChange('default')}>
            <X className="h-3.5 w-3.5" />
          </ToolbarIconButton>
        </div>
      </div>
    )
  }

  return (
    <div className="absolute inset-x-0 top-10 z-40 border-y border-zinc-800/80 bg-zinc-950/90 px-1.5 py-1.5 text-zinc-100 shadow-lg backdrop-blur-lg md:hidden">
      <div className="grid grid-cols-3 gap-1.5">
        <ToolbarButton
          label="Select"
          icon={<MousePointer2 className="h-3 w-3" />}
          onClick={() => onModeChange('selection')}
        />
        <ToolbarButton label="Paste" icon={<Clipboard className="h-3 w-3" />} onClick={onPaste} />
        <ToolbarButton label="Fn" icon={<PanelTopOpen className="h-3 w-3" />} onClick={onOpenSnippets} />
      </div>
    </div>
  )
}

function ToolbarButton({
  disabled,
  icon,
  label,
  onClick,
  primary,
}: {
  disabled?: boolean
  icon?: ReactNode
  label: string
  onClick: () => void
  primary?: boolean
}) {
  return (
    <button
      type="button"
      className={`flex h-7 min-w-0 items-center justify-center gap-1 rounded-md px-1.5 text-[10px] font-semibold transition-colors active:scale-[0.98] disabled:opacity-40 ${
        primary ? 'bg-blue-500/20 text-blue-300' : 'bg-zinc-800 text-zinc-300 active:bg-zinc-700'
      }`}
      disabled={disabled}
      onPointerDown={(event) => event.preventDefault()}
      onClick={onClick}
    >
      {icon}
      <span className="truncate">{label}</span>
    </button>
  )
}

function ToolbarIconButton({
  children,
  label,
  onClick,
}: {
  children: ReactNode
  label: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-label={label}
      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-red-500/15 text-red-300 active:bg-red-500/25"
      onPointerDown={(event) => event.preventDefault()}
      onClick={onClick}
    >
      {children}
    </button>
  )
}
