import { StrictMode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createBrowserRemoteNetworkRuntime } from '../connection/browserNetworkRuntime'
import { createBrowserRtcSession } from '../webrtc/browserRtcSession'
import { consoleConnectionLogger } from '../connection/connectionLogger'
import { terminalThemeCssVariables } from '../terminal/terminalSettings'
import { RemoteControlApp } from '../app/RemoteControlApp'

export interface RemoteControlEntryOptions {
  root?: HTMLElement | null | undefined
}

export function mountRemoteControlApp(options: RemoteControlEntryOptions = {}): Root {
  const rootElement = options.root ?? document.getElementById('root')
  if (!rootElement) {
    throw new Error('remote app root element is required')
  }
  const root = createRoot(rootElement)
  const networkRuntime = createBrowserRemoteNetworkRuntime()
  root.render(
    <StrictMode>
      <section
        className="flex h-[100dvh] w-screen flex-col overflow-hidden bg-[var(--termx-bg)] text-[var(--termx-text)] antialiased"
        data-testid="termx-remote-app-entry"
        style={terminalThemeCssVariables(undefined)}
      >
        <RemoteControlApp
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
