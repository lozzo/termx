import { StrictMode, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createBrowserRemoteNetworkRuntime } from '../connection/browserNetworkRuntime'
import { terminalThemeCssVariables } from '../terminal/terminalSettings'
import { RemoteControlApp } from '../app/RemoteControlApp'
import { BrowserBindingRuntime } from '../binding/browserBindingRuntime'

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
  if (!networkRuntime.storage) throw new Error('browser storage is required for the managed client')
  const bindingRuntime = new BrowserBindingRuntime(networkRuntime, networkRuntime.storage)
  root.render(
    <StrictMode>
      <BrowserRemoteApp bindingRuntime={bindingRuntime} networkRuntime={networkRuntime} />
    </StrictMode>,
  )
  return root
}

function BrowserRemoteApp({ bindingRuntime, networkRuntime }: { bindingRuntime: BrowserBindingRuntime; networkRuntime: ReturnType<typeof createBrowserRemoteNetworkRuntime> }) {
  useEffect(() => () => { void bindingRuntime.dispose() }, [bindingRuntime])
  return (
    <section
      className="flex h-[100dvh] w-screen flex-col overflow-hidden bg-[var(--termx-bg)] text-[var(--termx-text)] antialiased"
      data-testid="termx-remote-app-entry"
      style={terminalThemeCssVariables(undefined)}
    >
      <RemoteControlApp
        defaultControlUrl={import.meta.env.VITE_CONTROL_URL || undefined}
        externalPairingAdapter={bindingRuntime.externalPairingAdapter}
        machineRuntimeFactory={bindingRuntime.machineRuntimeFactory}
        networkRuntime={networkRuntime}
      />
    </section>
  )
}
