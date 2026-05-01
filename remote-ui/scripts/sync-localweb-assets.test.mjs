import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { syncLocalWebAssets } from './sync-localweb-assets.mjs'

const roots = []

describe('syncLocalWebAssets', () => {
  afterEach(() => {
    for (const root of roots.splice(0)) {
      rmSync(root, { force: true, recursive: true })
    }
  })

  it('replaces stale root files in embedded static output', () => {
    const root = mkdtempSync(resolve(tmpdir(), 'termx-localweb-sync-'))
    roots.push(root)
    const distDir = resolve(root, 'dist')
    const staticDir = resolve(root, 'static')
    mkdirSync(resolve(distDir, 'assets'), { recursive: true })
    mkdirSync(staticDir, { recursive: true })
    writeFileSync(resolve(distDir, 'index.html'), '<div id="root"></div>')
    writeFileSync(resolve(distDir, 'assets/index.js'), 'console.log("termx")')
    writeFileSync(resolve(staticDir, 'stale-manifest.webmanifest'), '{}')

    syncLocalWebAssets({ distDir, staticDir })

    expect(existsSync(resolve(staticDir, 'index.html'))).toBe(true)
    expect(existsSync(resolve(staticDir, 'assets/index.js'))).toBe(true)
    expect(existsSync(resolve(staticDir, 'stale-manifest.webmanifest'))).toBe(false)
  })
})
