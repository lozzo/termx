import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
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

  it('replaces stale root files in embedded static output with the local web entry', () => {
    const root = mkdtempSync(resolve(tmpdir(), 'termx-localweb-sync-'))
    roots.push(root)
    const distDir = resolve(root, 'dist')
    const staticDir = resolve(root, 'static')
    const fontDir = resolve(root, 'fonts')
    mkdirSync(resolve(distDir, 'assets'), { recursive: true })
    mkdirSync(staticDir, { recursive: true })
    mkdirSync(fontDir, { recursive: true })
    writeFileSync(resolve(distDir, 'index.html'), '<script src="/assets/app.js"></script>')
    writeFileSync(resolve(distDir, 'localweb.html'), '<script src="/assets/localweb.js"></script>')
    writeFileSync(resolve(distDir, 'assets/index.js'), 'console.log("termx")')
    writeFileSync(resolve(fontDir, 'TermxTest.woff2'), 'font bytes')
    writeFileSync(resolve(staticDir, 'stale-manifest.webmanifest'), '{}')

    syncLocalWebAssets({ distDir, staticDir, fontDir })

    expect(existsSync(resolve(staticDir, 'index.html'))).toBe(true)
    expect(readFileSync(resolve(staticDir, 'index.html'), 'utf8')).toContain('/assets/localweb.js')
    expect(existsSync(resolve(staticDir, 'assets/index.js'))).toBe(true)
    expect(readFileSync(resolve(staticDir, 'assets/fonts/TermxTest.woff2'), 'utf8')).toBe('font bytes')
    expect(existsSync(resolve(staticDir, 'stale-manifest.webmanifest'))).toBe(false)
  })
})
