import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import { hapticImpact } from '../platform/haptics'

export interface ActionSheetItem {
  label: string
  ariaLabel?: string | undefined
  subtitle?: string | undefined
  icon: ReactNode
  onClick: () => void
  danger?: boolean
  closeOnClick?: boolean | undefined
  secondaryAction?: ActionSheetSecondaryAction | undefined
}

export interface ActionSheetSecondaryAction {
  label: string
  icon: ReactNode
  onClick: () => void
  danger?: boolean | undefined
  closeOnClick?: boolean | undefined
}

export interface ActionSheetProps {
  isOpen: boolean
  onClose: () => void
  title?: string | undefined
  subtitle?: string | undefined
  actions: ActionSheetItem[]
}

export function ActionSheet({ isOpen, onClose, title, subtitle, actions }: ActionSheetProps) {
  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-[100] flex items-end justify-center bg-black/40 backdrop-blur-[2px] md:items-center"
      onClick={onClose}
      data-testid="action-sheet-backdrop"
    >
      <div
        className="w-full max-w-xl animate-slide-up rounded-t-[20px] bg-white pb-[env(safe-area-inset-bottom,20px)] shadow-2xl md:rounded-2xl md:pb-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-col">
          {/* Handle for dragging feel */}
          <div className="mx-auto mt-3 h-1.5 w-12 rounded-full bg-zinc-200 md:hidden" />

          <div className="flex items-center justify-between px-5 pt-4 pb-2">
            <div className="flex flex-col">
              {title && <h3 className="text-[17px] font-bold text-zinc-900">{title}</h3>}
              {subtitle && <p className="text-[13px] font-medium text-zinc-500">{subtitle}</p>}
            </div>
            <button
              type="button"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-zinc-100 text-zinc-500 active:bg-zinc-200"
              onClick={onClose}
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="mt-2 grid grid-cols-1 divide-y divide-zinc-100">
            {actions.map((action, index) => {
              if (action.secondaryAction) {
                return (
                  <div
                    key={index}
                    className="flex w-full items-stretch gap-2 px-5 py-2 transition-colors hover:bg-zinc-50 active:bg-zinc-50"
                    data-testid="action-sheet-item"
                  >
                    <button
                      type="button"
                      aria-label={action.ariaLabel ?? action.label}
                      className={`flex min-w-0 flex-1 items-center gap-4 rounded-xl py-2 text-left ${
                        action.danger ? 'text-red-600' : 'text-zinc-700'
                      }`}
                      onClick={() => runSheetAction(action, onClose)}
                    >
                      <ActionIcon danger={action.danger}>{action.icon}</ActionIcon>
                      <ActionText action={action} />
                    </button>
                    <button
                      type="button"
                      aria-label={action.secondaryAction.label}
                      title={action.secondaryAction.label}
                      className={`my-auto flex h-10 w-10 shrink-0 items-center justify-center rounded-xl transition-colors ${
                        action.secondaryAction.danger
                          ? 'bg-red-50 text-red-600 hover:bg-red-100 active:bg-red-100'
                          : 'bg-zinc-50 text-zinc-500 hover:bg-zinc-100 active:bg-zinc-100'
                      }`}
                      onClick={(event) => {
                        event.stopPropagation()
                        runSheetAction(action.secondaryAction!, onClose)
                      }}
                    >
                      {action.secondaryAction.icon}
                    </button>
                  </div>
                )
              }
              return (
                <button
                  key={index}
                  type="button"
                  aria-label={action.ariaLabel ?? action.label}
                  data-testid="action-sheet-item"
                  className={`flex w-full items-center gap-4 px-5 py-4 text-left transition-colors hover:bg-zinc-50 active:bg-zinc-50 ${
                    action.danger ? 'text-red-600' : 'text-zinc-700'
                  }`}
                  onClick={() => runSheetAction(action, onClose)}
                >
                  <ActionIcon danger={action.danger}>{action.icon}</ActionIcon>
                  <ActionText action={action} />
                </button>
              )
            })}
          </div>
        </div>
      </div>
    </div>
  )
}

function runSheetAction(
  action: {
    onClick: () => void
    danger?: boolean | undefined
    closeOnClick?: boolean | undefined
  },
  onClose: () => void,
) {
  if (action.danger) hapticImpact()
  action.onClick()
  if (action.closeOnClick !== false) onClose()
}

function ActionIcon({ children, danger }: { children: ReactNode; danger?: boolean | undefined }) {
  return (
    <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl ${
      danger ? 'bg-red-50' : 'bg-zinc-50'
    }`}>
      <span className={danger ? 'text-red-600' : 'text-zinc-500'}>
        {children}
      </span>
    </div>
  )
}

function ActionText({ action }: { action: Pick<ActionSheetItem, 'label' | 'subtitle'> }) {
  return (
    <span className="min-w-0">
      <span className="block truncate text-[16px] font-semibold">{action.label}</span>
      {action.subtitle ? (
        <span className="mt-0.5 block truncate text-[12px] font-medium text-zinc-500">{action.subtitle}</span>
      ) : null}
    </span>
  )
}
