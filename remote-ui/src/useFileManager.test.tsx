import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useFileManager } from './useFileManager'
import { createDeferredFileResponder, createMockFilePeerTransport } from './test/mockFileTransport'

describe('useFileManager', () => {
  it('loads the current directory and navigates through file transport interfaces', async () => {
    const transport = createMockFilePeerTransport({
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
      transport,
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
    expect(transport.openApiCount).toBeGreaterThan(0)
  })

  it('keeps file errors as visible state without throwing through the component tree', async () => {
    const transport = createMockFilePeerTransport({}, {
      '/files/list': { status: 500, body: { error: 'disk unavailable' } },
    }, { terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      transport,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toEqual({
      message: 'disk unavailable',
      surface: 'banner',
      recoverable: true,
    })
  })

  it('rejects a transport connected to a different machine before issuing file requests', async () => {
    const transport = createMockFilePeerTransport({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    }, {}, { machineId: 'machine-b', terminalId: 'terminal-1' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-a',
      terminalId: 'terminal-1',
      transport,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error?.message).toMatch(/machine-b.*machine-a/)
    expect(transport.openApiCount).toBe(0)
    expect(transport.requests).toEqual([])
  })

  it('ignores stale directory responses when navigation races with initial load', async () => {
    const rootLoad = createDeferredFileResponder()
    const transport = createMockFilePeerTransport({
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
      transport,
      initialPath: '/',
    }))

    await waitFor(() => expect(transport.requests.length).toBeGreaterThan(0))
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

  it('rejects transports that do not report the requested terminal before issuing file requests', async () => {
    const transport = createMockFilePeerTransport({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      transport,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error?.message).toMatch(/terminal.*missing.*terminal-1/i)
    expect(transport.openApiCount).toBe(0)
    expect(transport.requests).toEqual([])
  })

  it('rejects transports connected to another terminal before issuing file requests', async () => {
    const transport = createMockFilePeerTransport({
      '/files/list': { path: '/', parent: '', total: 0, entries: [] },
    }, {}, { terminalId: 'terminal-2' })

    const { result } = renderHook(() => useFileManager({
      machineId: 'machine-local',
      terminalId: 'terminal-1',
      transport,
      initialPath: '/',
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error?.message).toMatch(/terminal-2.*terminal-1/)
    expect(transport.openApiCount).toBe(0)
    expect(transport.requests).toEqual([])
  })
})
