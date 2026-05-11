import { hapticImpact } from '../platform/haptics'

export interface PasteConfirmDialogProps {
  text: string
  onCancel: () => void
  onConfirm: () => void
}

export function PasteConfirmDialog({ text, onCancel, onConfirm }: PasteConfirmDialogProps) {
  const lineCount = text.split(/\r\n|\r|\n/).length
  const preview = text.length > 600 ? `${text.slice(0, 600)}...` : text

  return (
    <div className="absolute inset-0 z-50 flex items-end bg-black/60 backdrop-blur-sm md:items-center md:justify-center" data-testid="termx-paste-confirm" onClick={onCancel}>
      <section
        className="w-full overflow-hidden border-y border-zinc-700 bg-zinc-950 text-zinc-100 shadow-2xl md:max-w-md md:rounded-2xl md:border"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="border-b border-zinc-800 px-4 py-3">
          <h2 className="text-[16px] font-bold">Confirm paste</h2>
          <p className="mt-1 text-[12px] font-medium text-zinc-500">
            {lineCount} lines, {text.length} characters
          </p>
        </header>
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words bg-black px-4 py-3 font-mono text-[12px] leading-5 text-zinc-300">
          {preview}
        </pre>
        <div className="grid grid-cols-2 gap-3 border-t border-zinc-800 p-3">
          <button
            type="button"
            className="h-11 rounded-xl bg-zinc-800 text-[14px] font-semibold text-zinc-200 active:bg-zinc-700"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            type="button"
            className="h-11 rounded-xl bg-blue-600 text-[14px] font-semibold text-white active:bg-blue-500"
            onClick={() => { hapticImpact(); onConfirm() }}
          >
            Paste
          </button>
        </div>
      </section>
    </div>
  )
}
