import { create } from '@bufbuild/protobuf'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import '../i18n'
import { SSHCredentialProvisionResultSchema } from '../generated/bindingpb/client_binding_pb'
import {
  DirectWebRTCTCPRouteConfigSchema,
  EndpointConfigV1Schema,
  EndpointRouteConfigV1Schema,
  EndpointSource,
  ManagedWebRTCRouteConfigSchema,
  SSHWebRTCTCPRouteConfigSchema,
} from '../generated/remoteauthpb/remote_auth_pb'
import { ConnectionRouteManager, moveRoute, orderedRoutes, removeRoute } from './ConnectionRouteManager'

afterEach(() => cleanup())

describe('ConnectionRouteManager', () => {
  it('reorders routes with stable priorities and keeps at least one route', () => {
    const endpoint = testEndpoint()

    const moved = moveRoute(endpoint, 'cloud', -1)
    expect(orderedRoutes(moved).map((route) => [route.routeId, route.priority])).toEqual([
      ['cloud', 10],
      ['direct', 20],
    ])
    expect(orderedRoutes(moved).every((route) => route.policySource === EndpointSource.USER)).toBe(true)

    const one = removeRoute(moved, 'direct')
    expect(one.routes.map((route) => route.routeId)).toEqual(['cloud'])
    expect(removeRoute(one, 'cloud').routes.map((route) => route.routeId)).toEqual(['cloud'])
  })

  it('tests the selected route through the supplied Go-owned connector', async () => {
    const user = userEvent.setup()
    const endpoint = testEndpoint()
    const test = vi.fn(async () => {})

    render(<ConnectionRouteManager
      endpointId="studio"
      adapter={{
        load: vi.fn(async () => endpoint),
        save: vi.fn(async (value) => value),
        test,
        provisionSSH: vi.fn(async () => { throw new Error('not used') }),
      }}
    />)

    await screen.findByText('Office LAN')
    await user.click(screen.getAllByRole('button', { name: 'Test route' })[0]!)

    await waitFor(() => expect(test).toHaveBeenCalledWith('direct'))
    expect(await screen.findByText('Connection test passed')).toBeTruthy()
  })

  it('maps a stable entitlement failure to actionable route copy', async () => {
    const user = userEvent.setup()
    const failure = Object.assign(new Error('internal provider text'), { code: 'cloud_entitlement_required' })
    const test = vi.fn(async () => { throw failure })

    render(<ConnectionRouteManager
      endpointId="studio"
      adapter={{
        load: vi.fn(async () => testEndpoint()),
        save: vi.fn(async (value) => value),
        test,
        provisionSSH: vi.fn(async () => { throw new Error('not used') }),
      }}
    />)

    await screen.findByText('Muxvia Cloud')
    await user.click(screen.getAllByRole('button', { name: 'Test route' })[1]!)

    await waitFor(() => expect(test).toHaveBeenCalledWith('cloud'))
    expect((await screen.findByRole('alert')).textContent).toBe('Your current plan does not include this Cloud connection path.')
    expect(screen.queryByText('internal provider text')).toBeNull()
  })

  it('localizes a stable unavailable error instead of exposing transport text', async () => {
    const user = userEvent.setup()
    const failure = Object.assign(new Error('dial tcp 192.0.2.10: connection refused'), { code: 'unavailable' })

    render(<ConnectionRouteManager
      endpointId="studio"
      adapter={{
        load: vi.fn(async () => testEndpoint()),
        save: vi.fn(async (value) => value),
        test: vi.fn(async () => { throw failure }),
        provisionSSH: vi.fn(async () => { throw new Error('not used') }),
      }}
    />)

    await screen.findByText('Office LAN')
    await user.click(screen.getAllByRole('button', { name: 'Test route' })[0]!)

    expect((await screen.findByRole('alert')).textContent).toBe('This route is currently unreachable. Check both networks and try again.')
    expect(screen.queryByText(/connection refused/)).toBeNull()
  })

  it('does not expose transport text when an adapter omits a stable error code', async () => {
    const user = userEvent.setup()

    render(<ConnectionRouteManager
      endpointId="studio"
      adapter={{
        load: vi.fn(async () => testEndpoint()),
        save: vi.fn(async (value) => value),
        test: vi.fn(async () => { throw new Error('dial tcp 192.0.2.10: connection refused') }),
        provisionSSH: vi.fn(async () => { throw new Error('not used') }),
      }}
    />)

    await screen.findByText('Office LAN')
    await user.click(screen.getAllByRole('button', { name: 'Test route' })[0]!)

    expect((await screen.findByRole('alert')).textContent).toBe('Muxvia could not test this route. Try again.')
    expect(screen.queryByText(/192\.0\.2\.10|connection refused/)).toBeNull()
  })

  it('provisions an SSH credential through the platform and shows the authorized key', async () => {
    const user = userEvent.setup()
    const endpoint = testEndpoint()
    endpoint.routes.push(create(EndpointRouteConfigV1Schema, {
      schemaVersion: 1,
      routeId: 'ssh-office',
      displayName: 'Office SSH',
      priority: 30,
      enabled: true,
      route: {
        case: 'sshWebrtcTcp',
        value: create(SSHWebRTCTCPRouteConfigSchema, {
          host: 'ssh.example.test',
          port: 22,
          user: 'muxvia',
          hostKeyFingerprints: ['SHA256:host'],
        }),
      },
    }))
    const provisionSSH = vi.fn(async () => create(SSHCredentialProvisionResultSchema, {
      endpoint,
      authorizedKey: 'ssh-ed25519 AAAATEST muxvia',
      keyFingerprint: 'SHA256:client',
    }))

    render(<ConnectionRouteManager
      endpointId="studio"
      adapter={{
        load: vi.fn(async () => endpoint),
        save: vi.fn(async (value) => value),
        test: vi.fn(async () => {}),
        provisionSSH,
      }}
    />)

    await screen.findByText('Office SSH')
    await user.click(screen.getByRole('button', { name: 'Prepare SSH key' }))

    await waitFor(() => expect(provisionSSH).toHaveBeenCalledWith('ssh-office'))
    expect((await screen.findByLabelText('SSH public key') as HTMLTextAreaElement).value).toBe('ssh-ed25519 AAAATEST muxvia')
    expect(screen.getByText('SHA256:client')).toBeTruthy()
  })
})

function testEndpoint() {
  return create(EndpointConfigV1Schema, {
    schemaVersion: 1,
    endpointId: 'studio',
    identity: { deviceId: 'daemon-studio', deviceFingerprint: 'ed25519-sha256:test' },
    routes: [
      create(EndpointRouteConfigV1Schema, {
        schemaVersion: 1,
        routeId: 'direct',
        displayName: 'Office LAN',
        priority: 10,
        enabled: true,
        route: {
          case: 'directWebrtcTcp',
          value: create(DirectWebRTCTCPRouteConfigSchema, {
            signalingAddresses: ['192.0.2.10:41120'],
            iceTcpAddresses: ['192.0.2.10:41121'],
          }),
        },
      }),
      create(EndpointRouteConfigV1Schema, {
        schemaVersion: 1,
        routeId: 'cloud',
        displayName: 'Muxvia Cloud',
        priority: 20,
        enabled: true,
        route: {
          case: 'managedWebrtc',
          value: create(ManagedWebRTCRouteConfigSchema, { targetDeviceId: 'daemon-studio' }),
        },
      }),
    ],
  })
}
