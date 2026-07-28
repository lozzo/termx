import { describe, expect, it } from 'vitest'
import { basename, joinPath, normalizeFilePath, parentPath, pathBreadcrumbs } from './fileUtils'

describe('file path utilities', () => {
  it('normalizes and navigates Windows drive paths', () => {
    expect(normalizeFilePath('C:\\Users\\Ada\\')).toBe('C:/Users/Ada')
    expect(joinPath('C:\\Users\\Ada', 'Documents')).toBe('C:/Users/Ada/Documents')
    expect(parentPath('C:\\Users\\Ada')).toBe('C:/Users')
    expect(parentPath('C:\\')).toBe('C:/')
    expect(basename('C:\\Users\\Ada\\notes.txt')).toBe('notes.txt')
  })

  it('keeps UNC share roots stable while navigating', () => {
    expect(normalizeFilePath('\\\\server\\share\\folder\\')).toBe('//server/share/folder')
    expect(parentPath('//server/share/folder')).toBe('//server/share')
    expect(parentPath('//server/share')).toBe('//server/share')
  })

  it('builds navigable breadcrumbs without changing the path root kind', () => {
    expect(pathBreadcrumbs('/var/log')).toEqual([
      { label: '/', path: '/' },
      { label: 'var', path: '/var' },
      { label: 'log', path: '/var/log' },
    ])
    expect(pathBreadcrumbs('C:\\Users\\lozzow')).toEqual([
      { label: 'C:', path: 'C:/' },
      { label: 'Users', path: 'C:/Users' },
      { label: 'lozzow', path: 'C:/Users/lozzow' },
    ])
    expect(pathBreadcrumbs('\\\\server\\share\\folder')).toEqual([
      { label: '//server/share', path: '//server/share' },
      { label: 'folder', path: '//server/share/folder' },
    ])
  })
})
