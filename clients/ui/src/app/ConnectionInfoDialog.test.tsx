import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ConnectionInfoDialog } from './MachineWorkspace'
import '../i18n'

describe('ConnectionInfoDialog', () => {
  it('applies an explicit Cloud relay TCP policy and keeps unavailable routes disabled', async () => {
    const user = userEvent.setup()
    const onApply = vi.fn()
    render(<ConnectionInfoDialog
      info={{ path: 'hub', routeKind: 'cloud', observedPath: 'single_relay', relayTransport: 'TCP', connectionId: 'studio:7', machineId: 'studio', relayInUse: true, type: 'relay', generation: 7n }}
      loading={false}
      error={null}
      policyState={{ policy: { route: 'auto', cloud: 'auto', relayTransport: 'auto' }, available: { direct: true, ssh: false, cloud: true }, unavailableReasons: { ssh: 'credential_unavailable' } }}
      applying={false}
      onClose={vi.fn()}
      onRefresh={vi.fn()}
      onRetry={vi.fn()}
      onApply={onApply}
      onRestoreAuto={vi.fn()}
    />)

    expect((screen.getByRole('radio', { name: 'SSH tunnel' }) as HTMLInputElement).disabled).toBe(true)
    expect(screen.getByText('Credential unavailable')).toBeTruthy()
    await user.click(screen.getByRole('radio', { name: 'Muxvia Cloud' }))
    await user.click(screen.getByRole('radio', { name: 'Relay only' }))
    await user.click(screen.getByRole('radio', { name: 'TCP only' }))
    await user.click(screen.getByRole('button', { name: 'Apply & reconnect' }))

    expect(onApply).toHaveBeenCalledWith({ route: 'cloud', cloud: 'relay', relayTransport: 'tcp' })
    expect(screen.getAllByText('Single relay').length).toBeGreaterThan(0)
    expect(screen.getAllByText('TCP').length).toBeGreaterThan(0)
  })

  it('offers retry and Restore Auto for a policy or reconnect failure', async () => {
    const user = userEvent.setup()
    const onRestoreAuto = vi.fn()
    render(<ConnectionInfoDialog
      info={null}
      loading={false}
      error="TCP relay is unavailable"
      policyState={{ policy: { route: 'cloud', cloud: 'relay', relayTransport: 'tcp' }, available: { direct: true, ssh: true, cloud: true }, unavailableReasons: {} }}
      applying={false}
      onClose={vi.fn()}
      onRefresh={vi.fn()}
      onRetry={vi.fn()}
      onApply={vi.fn()}
      onRestoreAuto={onRestoreAuto}
    />)

    expect(screen.getByRole('alert').textContent).toContain('TCP relay is unavailable')
    await user.click(screen.getByRole('button', { name: 'Restore Auto' }))
    expect(onRestoreAuto).toHaveBeenCalledOnce()
  })

  it('traps focus, hides background content, closes on Escape, and restores focus', async () => {
    const user = userEvent.setup()
    const trigger = document.createElement('button')
    trigger.textContent = 'Open network settings'
    document.body.appendChild(trigger)
    trigger.focus()
    const onClose = vi.fn()
    const rendered = render(<>
      <button type="button">Background action</button>
      <ConnectionInfoDialog
        info={null}
        loading={false}
        error={null}
        policyState={{ policy: { route: 'auto', cloud: 'auto', relayTransport: 'auto' }, available: { direct: true, ssh: true, cloud: true }, unavailableReasons: {} }}
        applying={false}
        onClose={onClose}
        onRefresh={vi.fn()}
        onRetry={vi.fn()}
        onApply={vi.fn()}
        onRestoreAuto={vi.fn()}
      />
    </>)

    const background = rendered.getByText('Background action')
    expect(background.hasAttribute('inert')).toBe(true)
    const close = rendered.container.querySelector<HTMLButtonElement>('button[aria-label="Close connection and network"]')!
    expect(document.activeElement).toBe(close)
    await user.keyboard('{Shift>}{Tab}{/Shift}')
    const refresh = Array.from(rendered.container.querySelectorAll('button')).find((button) => button.textContent === 'Refresh')
    expect(document.activeElement).toBe(refresh)
    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()

    rendered.unmount()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })
})
