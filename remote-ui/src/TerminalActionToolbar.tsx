import { type ReactNode, useEffect, useRef } from 'react'
import { Clipboard, Copy, Cpu, MousePointer2, PanelTopOpen, X, Minus, Plus } from 'lucide-react'
import { haptic } from './haptics'
import type { TerminalRenderer } from './Terminal'

export type TerminalToolbarMode = 'default' | 'selection'

export interface TerminalActionToolbarProps {
  mode: TerminalToolbarMode
  hasSelection: boolean
  renderer?: TerminalRenderer | undefined
  fontSize?: number
  onModeChange: (mode: TerminalToolbarMode) => void
  onSelectAll: () => void
  onSelectVisible: () => void
  onCopy: () => void
  onPaste: () => void
  onOpenSnippets: () => void
  onRendererChange?: ((renderer: TerminalRenderer) => void) | undefined
  onFontSizeChange?: ((size: number) => void) | undefined
  onClose?: () => void
}

const RENDERER_LABELS: Record<TerminalRenderer, string> = {
  auto: 'Auto',
  webgl: 'WebGL',
  canvas: 'Canvas',
  dom: 'DOM',
}
const RENDERER_CYCLE: TerminalRenderer[] = ['auto', 'webgl', 'canvas', 'dom']

export function TerminalActionToolbar({
  mode,
  hasSelection,
  renderer = 'auto',
  fontSize = 14,
  onModeChange,
  onSelectAll,
  onSelectVisible,
  onCopy,
  onPaste,
  onOpenSnippets,
  onRendererChange,
  onFontSizeChange,
  onClose,
}: TerminalActionToolbarProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (mode === 'selection' || !onClose) return
    const handler = (e: PointerEvent) => {
      const target = e.target as HTMLElement
      // Allow clicking buttons that open this menu (they usually stop propagation or we can check closest)
      if (target.closest('button[aria-label="Terminal tools"]')) return
      if (panelRef.current && !panelRef.current.contains(target)) {
        onClose()
      }
    }
    document.addEventListener('pointerdown', handler, true)
    return () => document.removeEventListener('pointerdown', handler, true)
  }, [mode, onClose])

  if (mode === 'selection') {
    return (
      <div className="absolute inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+5rem)] z-40 border-y border-[var(--termx-border-subtle)] bg-[var(--termx-overlay)] px-2 py-1.5 text-[var(--termx-text)] shadow-[0_-4px_18px_rgba(0,0,0,0.28)] backdrop-blur-lg md:hidden">
        <div className="flex items-center justify-between gap-1.5 overflow-x-auto">
          <div className="flex items-center gap-1.5">
            <ToolbarButton label="全选" onClick={onSelectAll} />
            <ToolbarButton label="可见区域" onClick={onSelectVisible} />
            <div className="mx-1 h-5 w-px shrink-0 bg-[var(--termx-border-subtle)]" />
            <ToolbarButton
              label="复制"
              icon={<Copy className="h-3 w-3" />}
              onClick={onCopy}
              disabled={!hasSelection}
              primary={hasSelection}
            />
          </div>
          <ToolbarIconButton label="取消选择" onClick={() => onModeChange('default')}>
            <X className="h-3.5 w-3.5" />
          </ToolbarIconButton>
        </div>
      </div>
    )
  }

  const nextRenderer = RENDERER_CYCLE[(RENDERER_CYCLE.indexOf(renderer) + 1) % RENDERER_CYCLE.length]!

  return (
    <div ref={panelRef} className="absolute inset-x-0 top-10 z-40 border-b border-[var(--termx-border-subtle)] bg-[var(--termx-overlay)] px-3 py-3 text-[var(--termx-text)] shadow-xl backdrop-blur-lg md:hidden animate-in slide-in-from-top-2">
      <div className="flex flex-col gap-3">
        {/* Settings Row: Font Size */}
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-[var(--termx-muted)]">字体大小</span>
          <div className="flex items-center gap-2">
            <button
              onPointerDown={(e) => { e.preventDefault(); haptic(); onFontSizeChange?.(Math.max(6, fontSize - 1)) }}
              className="flex h-7 w-7 items-center justify-center rounded-md bg-[var(--termx-surface-raised)] text-[var(--termx-text)] active:opacity-75"
            >
              <Minus className="h-3.5 w-3.5" />
            </button>
            <span className="w-10 text-center font-mono text-xs font-semibold tabular-nums text-[var(--termx-text)]">{fontSize}px</span>
            <button
              onPointerDown={(e) => { e.preventDefault(); haptic(); onFontSizeChange?.(Math.min(32, fontSize + 1)) }}
              className="flex h-7 w-7 items-center justify-center rounded-md bg-[var(--termx-surface-raised)] text-[var(--termx-text)] active:opacity-75"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>

        {/* Settings Row: Renderer */}
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-[var(--termx-muted)]">渲染模式</span>
          <button
            onPointerDown={(e) => { e.preventDefault(); haptic(); onRendererChange?.(nextRenderer) }}
            className="flex h-7 items-center justify-center gap-1.5 rounded-md bg-[var(--termx-surface-raised)] px-3 text-xs font-semibold text-[var(--termx-text)] active:opacity-75"
          >
            <Cpu className="h-3.5 w-3.5" />
            {RENDERER_LABELS[renderer]}
          </button>
        </div>

        <div className="my-1 h-px w-full bg-[var(--termx-border-subtle)]" />

        {/* Tools Row */}
        <div className="grid grid-cols-3 gap-2">
          <ToolbarButton
            label="选择"
            icon={<MousePointer2 className="h-3 w-3" />}
            onClick={() => onModeChange('selection')}
          />
          <ToolbarButton label="粘贴" icon={<Clipboard className="h-3 w-3" />} onClick={onPaste} />
          <ToolbarButton label="快捷短语" icon={<PanelTopOpen className="h-3 w-3" />} onClick={onOpenSnippets} />
        </div>
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
  title,
}: {
  disabled?: boolean
  icon?: ReactNode
  label: string
  onClick: () => void
  primary?: boolean
  title?: string
}) {
  return (
    <button
      type="button"
      title={title}
      className={`flex h-8 min-w-0 items-center justify-center gap-1.5 rounded-md px-2 text-xs font-semibold transition-colors active:scale-[0.98] disabled:opacity-40 ${
        primary ? 'bg-[var(--termx-accent)]/20 text-[var(--termx-accent)]' : 'bg-[var(--termx-surface-raised)] text-[var(--termx-text)] active:opacity-75'
      }`}
      disabled={disabled}
      onPointerDown={(event) => event.preventDefault()}
      onClick={() => { haptic(); onClick() }}
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
      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-red-500/15 text-red-300 transition-colors active:scale-[0.98] active:bg-red-500/25"
      onPointerDown={(event) => event.preventDefault()}
      onClick={() => { haptic(); onClick() }}
    >
      {children}
    </button>
  )
}
