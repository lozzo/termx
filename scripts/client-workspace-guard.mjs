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
if (uiPackage.name !== '@anytty/ui' || mobilePackage.name !== '@anytty/mobile' || cloudWebPackage.name !== '@anytty/cloud-web') {
  fail('workspace package names must be @anytty/ui, @anytty/mobile, and @anytty/cloud-web')
}
if (mobilePackage.dependencies?.['@anytty/ui'] !== uiPackage.version) {
  fail('mobile must depend on the exact local UI workspace version')
}
for (const [name, command] of Object.entries(uiPackage.scripts ?? {})) {
  if (command.includes('./node_modules/.bin/')) {
    fail(`UI script ${name} must resolve tools from the npm workspace PATH`)
  }
}

for (const [packageName, workspace] of [
  ['@anytty/ui', 'clients/ui'],
  ['@anytty/mobile', 'clients/mobile'],
  ['@anytty/cloud-web', 'cloud/web'],
]) {
  const link = lock.packages[`node_modules/${packageName}`]
  if (!link?.link || link.resolved !== workspace) {
    fail(`${packageName} is not linked to ${workspace} in the root lock`)
  }
}

const reactVersion = rootPackage.devDependencies?.react
if (
  !reactVersion ||
  rootPackage.devDependencies?.['react-dom'] !== reactVersion ||
  uiPackage.peerDependencies?.react !== reactVersion ||
  uiPackage.peerDependencies?.['react-dom'] !== reactVersion ||
  mobilePackage.dependencies?.react !== reactVersion ||
  mobilePackage.dependencies?.['react-dom'] !== reactVersion ||
  cloudWebPackage.dependencies?.react !== reactVersion ||
  cloudWebPackage.dependencies?.['react-dom'] !== reactVersion ||
  lock.packages['node_modules/react']?.version !== reactVersion ||
  lock.packages['node_modules/react-dom']?.version !== reactVersion
) {
  fail('root, UI peer, mobile, Cloud Web, and root lock React versions must be identical')
}

const viteVersion = uiPackage.devDependencies?.vite
const rootEsbuildVersion = rootPackage.devDependencies?.esbuild
if (
  !viteVersion ||
  mobilePackage.devDependencies?.vite !== viteVersion ||
  cloudWebPackage.devDependencies?.vite !== viteVersion ||
  !rootEsbuildVersion
) {
  fail('UI, mobile, and Cloud Web Vite versions must be identical, and root esbuild must be explicit')
}
if (
  lock.packages['node_modules/vite']?.version !== viteVersion ||
  lock.packages['node_modules/esbuild']?.version !== rootEsbuildVersion
) {
  fail('root lock Vite and esbuild versions must match the explicit dependencies')
}

for (const packageName of ['react', 'react-dom', 'vite', 'esbuild']) {
  const rootPath = `node_modules/${packageName}`
  const installedPaths = Object.keys(lock.packages)
    .filter((packagePath) => packagePath === rootPath || packagePath.endsWith(`/${rootPath}`))
    .sort()
  if (installedPaths.length !== 1 || installedPaths[0] !== rootPath) {
    fail(`${packageName} must be installed only at ${rootPath}; found ${JSON.stringify(installedPaths)}`)
  }
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
