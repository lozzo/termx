import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMockFileSession } from '../test/mockFileSession'
import { createRemoteClipboardApi } from './clipboardApi'

describe('createRemoteClipboardApi', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('reads TUI clipboard history records from daemon storage', async () => {
    const session = createMockFileSession({
      '/storage/list': {
        entries: [{
          app_id: 'termx.clipboard',
          scope: 'public',
          key: 'history/clip-1',
          value: new TextEncoder().encode(JSON.stringify({
            schema_version: 1,
            id: 'clip-1',
            text: 'hello from tui',
            preview: 'hello from tui',
            pane_id: 'pane-a',
            source_app: 'tuiv2',
            created_at: '2026-05-16T08:00:00Z',
          })),
          version: 5,
        }],
      },
    })

    await expect(createRemoteClipboardApi(session).list()).resolves.toEqual([{
      id: 'clip-1',
      text: 'hello from tui',
      preview: 'hello from tui',
      paneId: 'pane-a',
      sourceApp: 'tuiv2',
      createdAt: '2026-05-16T08:00:00Z',
      version: 5,
    }])
    expect(session.requests).toEqual([{
      method: 'POST',
      path: '/storage/list',
      params: {
        app_id: 'termx.clipboard',
        scope: 'public',
        prefix: 'history/',
      },
    }])
  })

  it('creates, updates, and deletes clipboard history entries', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-05-16T08:00:00Z'))
    const session = createMockFileSession({
      '/storage/get': {
        app_id: 'termx.clipboard',
        scope: 'public',
        key: 'history/clip-existing',
        value: new TextEncoder().encode(JSON.stringify({
          schema_version: 1,
          id: 'clip-existing',
          text: 'old text',
          preview: 'old text',
          created_at: '2026-05-15T08:00:00Z',
        })),
        version: 1,
      },
      '/storage/put': ({ key, value }: { key?: string; value?: Uint8Array } = {}) => ({
        app_id: 'termx.clipboard',
        scope: 'public',
        key,
        value,
        version: 2,
      }),
      '/storage/delete': { deleted: true, version: 3 },
    })
    const api = createRemoteClipboardApi(session)

    const created = await api.putText('new text')
    const updated = await api.updateText('clip-existing', 'updated text')
    await api.delete('clip-existing')

    expect(created.text).toBe('new text')
    expect(updated).toMatchObject({
      id: 'clip-existing',
      text: 'updated text',
      createdAt: '2026-05-15T08:00:00Z',
    })
    expect(session.requests.map((request) => request.path)).toEqual([
      '/storage/put',
      '/storage/get',
      '/storage/put',
      '/storage/delete',
    ])
    expect(session.requests[2]?.params).toEqual(expect.objectContaining({
      app_id: 'termx.clipboard',
      scope: 'public',
      key: 'history/clip-existing',
    }))
  })
})
