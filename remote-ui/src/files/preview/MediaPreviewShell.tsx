import type { ReactNode } from 'react'

export function MediaPreviewShell({ toolbar, children }: { toolbar?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex h-full min-h-full flex-col bg-black text-white">
      {toolbar}
      <div className="relative min-h-0 flex-1">
        {children}
      </div>
    </div>
  )
}
