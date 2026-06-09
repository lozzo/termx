import { describe, expect, it } from 'vitest'
import { createMockFileSession } from '../test/mockFileSession'
import { createRemoteStorageApi, storageText } from './remoteStorageApi'

describe('createRemoteStorageApi', () => {
  it('routes storage CRUD through runtime api requests', async () => {
    const session = createMockFileSession({
      '/storage/put': ({ value }: { value?: Uint8Array } = {}) => ({
        app_id: 'termx.paths',
        scope: 'public',
        key: 'bookmarks/~Users~lozzow',
        value,
        version: 3,
        updated_at: '2026-05-16T08:00:00Z',
      }),
      '/storage/get': {
        app_id: 'termx.paths',
        scope: 'public',
        key: 'bookmarks/~Users~lozzow',
        value: new TextEncoder().encode('hello'),
        version: 3,
      },
      '/storage/list': {
        entries: [{
          app_id: 'termx.paths',
          scope: 'public',
          key: 'bookmarks/~Users~lozzow',
          value: new TextEncoder().encode('hello'),
          version: 3,
        }],
      },
      '/storage/delete': { deleted: true, version: 4 },
    })
    const api = createRemoteStorageApi(session)

    const put = await api.put({
      appId: 'termx.paths',
      key: 'bookmarks/~Users~lozzow',
      value: 'hello',
      expectedVersion: 2,
    })
    const got = await api.get({ appId: 'termx.paths', key: 'bookmarks/~Users~lozzow' })
    const list = await api.list({ appId: 'termx.paths', prefix: 'bookmarks/' })
    const deleted = await api.delete({ appId: 'termx.paths', key: 'bookmarks/~Users~lozzow' })

    expect(storageText(put)).toBe('hello')
    expect(storageText(got)).toBe('hello')
    expect(list).toHaveLength(1)
    expect(deleted).toEqual({ deleted: true, version: 4 })
    expect(session.requests).toEqual([
      {
        method: 'POST',
        path: '/storage/put',
        params: expect.objectContaining({
          app_id: 'termx.paths',
          scope: 'public',
          key: 'bookmarks/~Users~lozzow',
          check_version: true,
          expected_version: 2,
        }),
      },
      {
        method: 'POST',
        path: '/storage/get',
        params: { app_id: 'termx.paths', scope: 'public', key: 'bookmarks/~Users~lozzow' },
      },
      {
        method: 'POST',
        path: '/storage/list',
        params: { app_id: 'termx.paths', scope: 'public', prefix: 'bookmarks/' },
      },
      {
        method: 'POST',
        path: '/storage/delete',
        params: { app_id: 'termx.paths', scope: 'public', key: 'bookmarks/~Users~lozzow' },
      },
    ])
  })
})
