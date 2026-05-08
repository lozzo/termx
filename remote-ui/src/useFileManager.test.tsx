import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useFileManager } from './useFileManager'
import { createDeferredFileResponder, createMockFileSession } from './test/mockFileSession'

describe('useFileManager', () => {
  it('loads the current directory and navigates through file session interfaces', async () => {
    const session = createMockFileSession({
      '/files/list': ({ path }: { path?: string }) => ({
        path,
        parent: path === '/' ? '' : '/',
        total: 1,
        entries: [{ name: path === '/' ? 'tmp' : 'log.txt', type: path === '/' ? 'dir' : 'file', size: 0 }],
      }),
    }, {}, { terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.currentPath).toBe('/')
    expect(result.current.entries[0]?.name).toBe('tmp')

    await act(async () => {
      await result.current.navigate('/tmp')
    })

    expect(result.current.currentPath).toBe('/tmp')
    expect(result.current.entries[0]?.name).toBe('log.txt')
    expect(session.openApiCount).toBeGreaterThan(0)
  })

  it('keeps file errors as visible state without throwing through the component tree', async () => {
    const session = createMockFileSession({}, {
      '/files/list': { status: 500, body: { error: 'disk unavailable' } },
    }, { terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toEqual({
      message: 'disk unavailable',
      surface: 'banner',
      recoverable: true,
    })
  })

  it('falls back to the nearest readable parent when the requested directory cannot be opened', async () => {
    const session = createMockFileSession({
      '/files/list': ({ path }: { path?: string }) => {
        if (path === '/srv') {
          return {
            path: '/srv',
            parent: '/',
            total: 1,
            entries: [{ name: 'app', type: 'dir', size: 0 }],
          }
        }
        throw new Error(`denied ${path}`)
      },
    }, {}, { terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session,
      initialPath: '/srv/app/private',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBeNull()
    expect(result.current.currentPath).toBe('/srv')
    expect(result.current.actionMessage).toBe('Opened /srv instead')
    expect(session.requests.map((request) => request.params?.path)).toEqual([
      '/srv/app/private',
      '/srv/app',
      '/srv',
    ])
  })

  it('rejects a session connected to a different machine before issuing file requests', async () => {
    const session = createMockFileSession({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    }, {}, { machineId: 'machine-b', terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-a',
      terminalId: 'terminal-1',
      session,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error?.message).toMatch(/machine-b.*machine-a/)
    expect(session.openApiCount).toBe(0)
    expect(session.requests).toEqual([])
  })

  it('ignores stale directory responses when navigation races with initial load', async () => {
    const rootLoad = createDeferredFileResponder()
    const session = createMockFileSession({
      '/files/list': ({ path }: { path?: string }) => {
        if (path === '/') return rootLoad.promise
        return {
          path,
          parent: '/',
          total: 1,
          entries: [{ name: 'fresh.txt', type: 'file', size: 1 }],
        }
      },
    }, {}, { terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session,
      initialPath: '/',
    }))

    await waitFor(() => expect(session.requests.length).toBeGreaterThan(0))
    await act(async () => {
      await result.current.navigate('/tmp')
    })

    expect(result.current.currentPath).toBe('/tmp')
    expect(result.current.entries[0]?.name).toBe('fresh.txt')

    rootLoad.resolve({
      path: '/',
      parent: '',
      total: 1,
      entries: [{ name: 'stale.txt', type: 'file', size: 1 }],
    })

    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(result.current.currentPath).toBe('/tmp')
    expect(result.current.entries[0]?.name).toBe('fresh.txt')
  })

  it('allows machine-scoped file sessions that do not report a terminal id', async () => {
    const session = createMockFileSession({
      '/files/list': { path: '/', parent: '', total: 1, entries: [{ name: 'machine.log', type: 'file', size: 12 }] },
    })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBeNull()
    expect(result.current.entries[0]?.name).toBe('machine.log')
    expect(session.openApiCount).toBeGreaterThan(0)
  })

  it('rejects transports connected to another terminal before issuing file requests', async () => {
    const session = createMockFileSession({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    }, {}, { terminalId: 'terminal-2' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      session,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error?.message).toMatch(/terminal-2.*terminal-1/)
    expect(session.openApiCount).toBe(0)
    expect(session.requests).toEqual([])
  })
})
