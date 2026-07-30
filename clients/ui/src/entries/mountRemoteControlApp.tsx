import { StrictMode, useEffect, useRef, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { readTerminalSettings, terminalThemeCssVariables, type TerminalThemeId } from '../terminal/terminalSettings'

export interface RemoteControlEntryOptions {
  root?: HTMLElement | null | undefined
  themeId?: TerminalThemeId | undefined
}

export function mountRemoteControlApp(options: RemoteControlEntryOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('remote app root element is required')
  }
  const root = createRoot(rootElement)
  const themeId = options.themeId ?? readTerminalSettings().themeId
  let renderVersion = 0
  const start = () => {
    root.render(
      <StrictMode>
        <CloudUnavailableApp key={renderVersion++} onRetry={start} themeId={themeId} />
      </StrictMode>,
    )
  }
  start()
  return root
}

function CloudUnavailableApp({ onRetry, themeId }: { onRetry: () => void; themeId?: TerminalThemeId | undefined }) {
  const [isRetrying, setIsRetrying] = useState(false)
  const retryPending = useRef(false)
  const retryTimer = useRef<number | undefined>(undefined)

  useEffect(() => () => {
    if (retryTimer.current !== undefined) window.clearTimeout(retryTimer.current)
  }, [])

  const retry = () => {
    if (retryPending.current) return
    retryPending.current = true
    setIsRetrying(true)
    retryTimer.current = window.setTimeout(onRetry, 0)
  }

  return (
    <section
      aria-describedby="anytty-cloud-unavailable-description"
      aria-labelledby="anytty-cloud-unavailable-title"
      className="flex h-[100dvh] w-screen items-center justify-center bg-[var(--anytty-bg)] px-6 text-[var(--anytty-text)] antialiased"
      data-testid="anytty-cloud-unavailable"
      role="alert"
      style={terminalThemeCssVariables(themeId)}
    >
      <div className="w-full max-w-sm border-l-2 border-emerald-600 pl-5">
        <h1 className="text-lg font-semibold text-[var(--anytty-text)]" id="anytty-cloud-unavailable-title">AnyTTY Cloud 暂不可用</h1>
        <p className="mt-2 text-sm leading-6 text-[var(--anytty-muted)]" id="anytty-cloud-unavailable-description">云端服务正在重构。Direct 和 SSH 客户端不受影响。</p>
        <button
          aria-busy={isRetrying}
          aria-label={isRetrying ? '正在重试 AnyTTY Cloud 挂载' : '重试 AnyTTY Cloud 挂载'}
          className="mt-5 inline-flex min-h-11 cursor-pointer items-center justify-center border border-[var(--anytty-border)] bg-[var(--anytty-surface-raised)] px-4 text-sm font-semibold text-[var(--anytty-text)] transition-colors duration-200 active:opacity-80 disabled:cursor-wait disabled:opacity-60 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--anytty-accent)]"
          disabled={isRetrying}
          onClick={retry}
          type="button"
        >
          {isRetrying ? '正在重试' : '重试'}
        </button>
      </div>
    </section>
  )
}
