import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const remoteUiDir = resolve(scriptDir, '..')
const distDir = resolve(remoteUiDir, 'dist')
const staticDir = resolve(remoteUiDir, '../termx-remote/localweb/static')
const fontDir = resolve(remoteUiDir, 'src/assets/fonts')

export function syncLocalWebAssets({ distDir, staticDir, fontDir }) {
  const localWebHtml = resolve(distDir, 'localweb.html')
  if (!existsSync(localWebHtml)) {
    throw new Error('remote-ui dist/localweb.html is missing; run vite build first')
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
  cpSync(localWebHtml, resolve(staticDir, 'index.html'))
  if (fontDir && existsSync(fontDir)) {
    const targetFontDir = resolve(staticDir, 'assets/fonts')
    mkdirSync(targetFontDir, { recursive: true })
    cpSync(fontDir, targetFontDir, { recursive: true })
  }
}

if (import.meta.url === `file://${process.argv[1]}`) {
  syncLocalWebAssets({ distDir, staticDir, fontDir })
  console.log(`synced ${distDir} -> ${staticDir}`)
}
