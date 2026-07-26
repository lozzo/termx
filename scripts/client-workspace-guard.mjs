#!/usr/bin/env node

import { existsSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const expectedWorkspaces = ['clients/mobile', 'clients/ui', 'cloud/web']

function readJSON(relativePath) {
  return JSON.parse(readFileSync(join(repoRoot, relativePath), 'utf8'))
}

function fail(message) {
  throw new Error(`client workspace guard failed: ${message}`)
}

const rootPackage = readJSON('package.json')
const actualWorkspaces = [...(rootPackage.workspaces ?? [])].sort()
if (JSON.stringify(actualWorkspaces) !== JSON.stringify(expectedWorkspaces)) {
  fail(`root workspaces differ: ${JSON.stringify(actualWorkspaces)}`)
}

const lock = readJSON('package-lock.json')
if (lock.lockfileVersion !== 3 || typeof lock.packages !== 'object') {
  fail('root package-lock.json must use lockfileVersion 3')
}

const uiPackage = readJSON('clients/ui/package.json')
const mobilePackage = readJSON('clients/mobile/package.json')
const cloudWebPackage = readJSON('cloud/web/package.json')
if (uiPackage.name !== '@muxvia/ui' || mobilePackage.name !== '@muxvia/mobile' || cloudWebPackage.name !== '@muxvia/cloud-web') {
  fail('workspace package names must be @muxvia/ui, @muxvia/mobile, and @muxvia/cloud-web')
}
if (mobilePackage.dependencies?.['@muxvia/ui'] !== uiPackage.version) {
  fail('mobile must depend on the exact local UI workspace version')
}
for (const [name, command] of Object.entries(uiPackage.scripts ?? {})) {
  if (command.includes('./node_modules/.bin/')) {
    fail(`UI script ${name} must resolve tools from the npm workspace PATH`)
  }
}

for (const [packageName, workspace] of [
  ['@muxvia/ui', 'clients/ui'],
  ['@muxvia/mobile', 'clients/mobile'],
  ['@muxvia/cloud-web', 'cloud/web'],
]) {
  const link = lock.packages[`node_modules/${packageName}`]
  if (!link?.link || link.resolved !== workspace) {
    fail(`${packageName} is not linked to ${workspace} in the root lock`)
  }
}

// Web Controller 使用独立的 React 版本，并由 Vite alias 固定解析到自己的 workspace，避免与共享客户端 UI 混装。
const cloudReactVersion = cloudWebPackage.dependencies?.react
if (
  !cloudReactVersion ||
  cloudWebPackage.dependencies?.['react-dom'] !== cloudReactVersion ||
  lock.packages['cloud/web/node_modules/react']?.version !== cloudReactVersion ||
  lock.packages['cloud/web/node_modules/react-dom']?.version !== cloudReactVersion
) {
  fail('Cloud Web React dependencies must be explicit, equal, and workspace-local')
}

const reactVersion = rootPackage.devDependencies?.react
if (
  !reactVersion ||
  rootPackage.devDependencies?.['react-dom'] !== reactVersion ||
  uiPackage.peerDependencies?.react !== reactVersion ||
  uiPackage.peerDependencies?.['react-dom'] !== reactVersion ||
  mobilePackage.dependencies?.react !== reactVersion ||
  mobilePackage.dependencies?.['react-dom'] !== reactVersion
) {
  fail('root, UI peer, and mobile React versions must be identical')
}

// workspace 合并后各产品 Vite 必须解析到锁定版本，不能依赖 npm 偶然 hoist。
const uiViteVersion = uiPackage.devDependencies?.vite
const mobileViteVersion = mobilePackage.devDependencies?.vite
const cloudWebViteVersion = cloudWebPackage.devDependencies?.vite
const rootEsbuildVersion = rootPackage.devDependencies?.esbuild
if (!uiViteVersion || !mobileViteVersion || !cloudWebViteVersion || !rootEsbuildVersion) {
  fail('UI Vite, mobile Vite, Cloud Web Vite, and root esbuild must be explicit dependencies')
}
if (
  lock.packages['clients/ui/node_modules/vite']?.version !== uiViteVersion ||
  lock.packages['node_modules/vite']?.version !== mobileViteVersion ||
  lock.packages['cloud/web/node_modules/vite']?.version !== cloudWebViteVersion ||
  lock.packages['node_modules/esbuild']?.version !== rootEsbuildVersion ||
  !lock.packages['node_modules/vite/node_modules/esbuild']
) {
  fail('Vite and esbuild lock placement must keep UI, mobile, and Cloud Web toolchains isolated')
}

for (const nestedLock of [
  'clients/ui/package-lock.json',
  'clients/mobile/package-lock.json',
  'cloud/web/package-lock.json',
]) {
  if (existsSync(join(repoRoot, nestedLock))) {
    fail(`nested lockfile must not exist: ${nestedLock}`)
  }
}
if (existsSync(join(repoRoot, 'clients/mobile/native/android'))) {
  fail('clients/mobile/native/android must not exist')
}

console.log('client workspace guard passed')
