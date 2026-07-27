import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { terminalThemeCssVariables } from '../terminal/terminalSettings'

export interface RemoteControlEntryOptions {
  root?: HTMLElement | null | undefined
}

export function mountRemoteControlApp(options: RemoteControlEntryOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('remote app root element is required')
  }
  const root = createRoot(rootElement)
  root.render(
    <StrictMode>
      <CloudUnavailableApp />
    </StrictMode>,
  )
  return root
}

function CloudUnavailableApp() {
  return (
    <section
      className="flex h-[100dvh] w-screen items-center justify-center bg-[var(--anytty-bg)] px-6 text-[var(--anytty-text)] antialiased"
      data-testid="anytty-cloud-unavailable"
      style={terminalThemeCssVariables(undefined)}
    >
      <div className="w-full max-w-sm border-l-2 border-emerald-600 pl-5">
        <h1 className="text-lg font-semibold text-zinc-950">AnyTTY Cloud 暂不可用</h1>
        <p className="mt-2 text-sm leading-6 text-zinc-600">云端服务正在重构。Direct 和 SSH 客户端不受影响。</p>
      </div>
    </section>
  )
}
