import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { syncWebControlRemote } from './sync-web-control-remote.mjs'

const roots = []

describe('syncWebControlRemote', () => {
  afterEach(() => {
    for (const root of roots.splice(0)) {
      rmSync(root, { force: true, recursive: true })
    }
  })

  it('copies the Web Control remote entry under /remote assets', () => {
    const root = mkdtempSync(resolve(tmpdir(), 'termx-web-control-remote-sync-'))
    roots.push(root)
    const distDir = resolve(root, 'dist')
    const publicRemoteDir = resolve(root, 'public/remote')
    mkdirSync(resolve(distDir, 'assets'), { recursive: true })
    writeFileSync(resolve(distDir, 'web-control.html'), [
      '<div id="root"></div>',
      '<script type="module" src="/assets/webControl.js"></script>',
      '<link rel="stylesheet" href="/assets/index.css">',
    ].join(''))
    writeFileSync(resolve(distDir, 'assets/webControl.js'), 'console.log("remote")')
    writeFileSync(resolve(distDir, 'assets/index.css'), 'body{}')

    syncWebControlRemote({ distDir, publicRemoteDir })

    const html = readFileSync(resolve(publicRemoteDir, 'index.html'), 'utf8')
    expect(html).toContain('src="/remote/assets/webControl.js"')
    expect(html).toContain('href="/remote/assets/index.css"')
    expect(existsSync(resolve(publicRemoteDir, 'assets/webControl.js'))).toBe(true)
  })
})
