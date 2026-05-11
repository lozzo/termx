import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import { hapticImpact } from '../platform/haptics'

export interface ActionSheetItem {
  label: string
  icon: ReactNode
  onClick: () => void
  danger?: boolean
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
      className="fixed inset-0 z-[100] flex items-end justify-center bg-black/40 backdrop-blur-[2px]"
      onClick={onClose}
      data-testid="action-sheet-backdrop"
    >
      <div
        className="w-full max-w-xl animate-slide-up rounded-t-[20px] bg-white pb-[env(safe-area-inset-bottom,20px)] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-col">
          {/* Handle for dragging feel */}
          <div className="mx-auto mt-3 h-1.5 w-12 rounded-full bg-zinc-200" />

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
            {actions.map((action, index) => (
              <button
                key={index}
                type="button"
                className={`flex w-full items-center gap-4 px-5 py-4 text-left transition-colors active:bg-zinc-50 ${
                  action.danger ? 'text-red-600' : 'text-zinc-700'
                }`}
                onClick={() => {
                  if (action.danger) hapticImpact()
                  action.onClick()
                  onClose()
                }}
              >
                <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl ${
                  action.danger ? 'bg-red-50' : 'bg-zinc-50'
                }`}>
                  <span className={action.danger ? 'text-red-600' : 'text-zinc-500'}>
                    {action.icon}
                  </span>
                </div>
                <span className="text-[16px] font-semibold">{action.label}</span>
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
