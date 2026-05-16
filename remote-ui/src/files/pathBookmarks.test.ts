import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMockFileSession } from '../test/mockFileSession'
import { bookmarkLabel, createPathBookmarkApi } from './pathBookmarks'

describe('createPathBookmarkApi', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('stores and lists path bookmarks in daemon storage', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-05-16T08:00:00Z'))
    const session = createMockFileSession({
      '/storage/put': ({ key, value }: { key?: string; value?: Uint8Array } = {}) => ({
        app_id: 'termx.paths',
        scope: 'public',
        key,
        value,
        version: 7,
      }),
      '/storage/list': {
        entries: [{
          app_id: 'termx.paths',
          scope: 'public',
          key: 'bookmarks/~2FUsers~2Flozzow~2Fproject',
          value: new TextEncoder().encode(JSON.stringify({
            schema_version: 1,
            id: '~2FUsers~2Flozzow~2Fproject',
            path: '/Users/lozzow/project',
            label: 'project',
            created_at: '2026-05-16T08:00:00Z',
            updated_at: '2026-05-16T08:00:00Z',
          })),
          version: 7,
        }],
      },
      '/storage/delete': { deleted: true, version: 8 },
    })
    const api = createPathBookmarkApi(session)

    await expect(api.add('/Users/lozzow/project/')).resolves.toMatchObject({
      path: '/Users/lozzow/project',
      label: 'project',
      version: 7,
    })
    await expect(api.list()).resolves.toEqual([{
      id: '~2FUsers~2Flozzow~2Fproject',
      path: '/Users/lozzow/project',
      label: 'project',
      createdAt: '2026-05-16T08:00:00Z',
      updatedAt: '2026-05-16T08:00:00Z',
      version: 7,
    }])
    await api.remove('~2FUsers~2Flozzow~2Fproject')

    expect(session.requests.map((request) => request.path)).toEqual([
      '/storage/put',
      '/storage/list',
      '/storage/delete',
    ])
    expect(session.requests[0]?.params).toEqual(expect.objectContaining({
      app_id: 'termx.paths',
      scope: 'public',
      key: 'bookmarks/~2FUsers~2Flozzow~2Fproject',
    }))
  })

  it('derives compact labels from absolute paths', () => {
    expect(bookmarkLabel('/')).toBe('/')
    expect(bookmarkLabel('/srv/app/')).toBe('app')
  })
})
