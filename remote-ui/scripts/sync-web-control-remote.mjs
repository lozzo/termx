import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const remoteUiDir = resolve(scriptDir, '..')
const distDir = resolve(remoteUiDir, 'dist')
const publicRemoteDir = resolve(remoteUiDir, '../web-control/public/remote')

export function syncWebControlRemote({ distDir, publicRemoteDir }) {
  const htmlPath = resolve(distDir, 'web-control.html')
  if (!existsSync(htmlPath)) {
    throw new Error('remote-ui dist/web-control.html is missing; run vite build first')
  }

  const assetDir = resolve(distDir, 'assets')
  const jsAssets = existsSync(assetDir)
    ? readdirSync(assetDir).filter((name) => name.endsWith('.js'))
    : []
  if (jsAssets.length === 0) {
    throw new Error('remote-ui dist/assets does not contain a JavaScript module asset')
  }

  rmSync(publicRemoteDir, { force: true, recursive: true })
  mkdirSync(publicRemoteDir, { recursive: true })
  cpSync(assetDir, resolve(publicRemoteDir, 'assets'), { recursive: true })

  const html = readFileSync(htmlPath, 'utf8')
    .replaceAll('src="/assets/', 'src="/remote/assets/')
    .replaceAll('href="/assets/', 'href="/remote/assets/')
  writeFileSync(resolve(publicRemoteDir, 'index.html'), html)
}

if (import.meta.url === `file://${process.argv[1]}`) {
  syncWebControlRemote({ distDir, publicRemoteDir })
  console.log(`synced ${distDir}/web-control.html -> ${publicRemoteDir}`)
}
