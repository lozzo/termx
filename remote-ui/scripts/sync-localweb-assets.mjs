import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const remoteUiDir = resolve(scriptDir, '..')
const distDir = resolve(remoteUiDir, 'dist')
const staticDir = resolve(remoteUiDir, '../termx-core/internal/remote/localweb/static')

export function syncLocalWebAssets({ distDir, staticDir }) {
  if (!existsSync(resolve(distDir, 'index.html'))) {
    throw new Error('remote-ui dist/index.html is missing; run vite build first')
  }

  const assetDir = resolve(distDir, 'assets')
  const jsAssets = existsSync(assetDir)
    ? readdirSync(assetDir).filter((name) => name.endsWith('.js'))
    : []
  if (jsAssets.length === 0) {
    throw new Error('remote-ui dist/assets does not contain a JavaScript module asset')
  }

  rmSync(staticDir, { force: true, recursive: true })
  mkdirSync(staticDir, { recursive: true })
  cpSync(distDir, staticDir, { recursive: true })
}

if (import.meta.url === `file://${process.argv[1]}`) {
  syncLocalWebAssets({ distDir, staticDir })
  console.log(`synced ${distDir} -> ${staticDir}`)
}
