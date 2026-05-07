import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createBrowserRemoteNetworkRuntime } from './browserNetworkRuntime'
import { createBrowserRtcSession } from './browserRtcSession'
import { consoleConnectionLogger } from './connectionLogger'
import { WebControlRemoteApp } from './WebControlRemoteApp'

export interface RemoteAppEntryOptions {
  root?: HTMLElement | null | undefined
}

export function mountRemoteApp(options: RemoteAppEntryOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('remote app root element is required')
  }
  const root = createRoot(rootElement)
  const networkRuntime = createBrowserRemoteNetworkRuntime()
  root.render(
    <StrictMode>
      <section
        className="flex h-[100dvh] w-screen flex-col overflow-hidden bg-zinc-50 text-zinc-950 antialiased"
        data-testid="termx-remote-app-entry"
      >
        <WebControlRemoteApp
          defaultControlUrl={import.meta.env.VITE_CONTROL_URL || undefined}
          managedRtcSessionFactory={({ machineId }) => createBrowserRtcSession({
            machineId,
            logger: consoleConnectionLogger,
          })}
          networkRuntime={networkRuntime}
        />
      </section>
    </StrictMode>,
  )
  return root
}
