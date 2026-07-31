#!/usr/bin/env node

import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { test } from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const guardPath = join(repoRoot, 'scripts', 'client-workspace-guard.mjs')
const versions = {
  react: '19.2.7',
  'react-dom': '19.2.7',
  vite: '8.1.5',
  esbuild: '0.28.1',
}

function writeJSON(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`)
}

function runGuard(mutateLock) {
  const fixture = mkdtempSync(join(tmpdir(), 'anytty-client-workspace-guard-'))
  try {
    for (const directory of ['scripts', 'clients/ui', 'clients/mobile', 'cloud/web']) {
      mkdirSync(join(fixture, directory), { recursive: true })
    }
    copyFileSync(guardPath, join(fixture, 'scripts', 'client-workspace-guard.mjs'))

    writeJSON(join(fixture, 'package.json'), {
      name: 'guard-fixture',
      private: true,
      workspaces: ['clients/ui', 'clients/mobile', 'cloud/web'],
      devDependencies: {
        react: versions.react,
        'react-dom': versions['react-dom'],
        esbuild: versions.esbuild,
      },
    })
    writeJSON(join(fixture, 'clients/ui/package.json'), {
      name: '@anytty/ui',
      version: '0.0.0',
      scripts: {},
      devDependencies: { vite: versions.vite },
      peerDependencies: {
        react: versions.react,
        'react-dom': versions['react-dom'],
      },
    })
    writeJSON(join(fixture, 'clients/mobile/package.json'), {
      name: '@anytty/mobile',
      version: '0.1.0',
      dependencies: {
        '@anytty/ui': '0.0.0',
        react: versions.react,
        'react-dom': versions['react-dom'],
      },
      devDependencies: { vite: versions.vite },
    })
    writeJSON(join(fixture, 'cloud/web/package.json'), {
      name: '@anytty/cloud-web',
      version: '0.1.0',
      dependencies: {
        react: versions.react,
        'react-dom': versions['react-dom'],
      },
      devDependencies: { vite: versions.vite },
    })

    const lock = {
      name: 'guard-fixture',
      lockfileVersion: 3,
      packages: {
        '': {},
        'clients/ui': {},
        'clients/mobile': {},
        'cloud/web': {},
        'node_modules/@anytty/ui': { link: true, resolved: 'clients/ui' },
        'node_modules/@anytty/mobile': { link: true, resolved: 'clients/mobile' },
        'node_modules/@anytty/cloud-web': { link: true, resolved: 'cloud/web' },
        'node_modules/react': { version: versions.react },
        'node_modules/react-dom': { version: versions['react-dom'] },
        'node_modules/vite': { version: versions.vite },
        'node_modules/esbuild': { version: versions.esbuild },
      },
    }
    mutateLock?.(lock)
    writeJSON(join(fixture, 'package-lock.json'), lock)

    const result = spawnSync(process.execPath, ['scripts/client-workspace-guard.mjs'], {
      cwd: fixture,
      encoding: 'utf8',
    })
    if (result.error) throw result.error
    return result
  } finally {
    rmSync(fixture, { recursive: true, force: true })
  }
}

test('accepts one root-hoisted installation of each guarded package', () => {
  const result = runGuard()
  assert.equal(result.status, 0, `${result.stdout}${result.stderr}`)
  assert.match(result.stdout, /client workspace guard passed/)
})

for (const [packageName, nestedPath] of [
  ['react', 'cloud/web/node_modules/react'],
  ['react-dom', 'clients/mobile/node_modules/react-dom'],
  ['vite', 'clients/ui/node_modules/vite'],
  ['esbuild', 'node_modules/vite/node_modules/esbuild'],
]) {
  test(`rejects a nested duplicate of ${packageName}`, () => {
    const result = runGuard((lock) => {
      lock.packages[nestedPath] = { version: versions[packageName] }
    })
    const output = `${result.stdout}${result.stderr}`
    assert.equal(result.status, 1, output)
    assert.match(output, new RegExp(`${packageName} must be installed only at node_modules/${packageName}`))
  })
}

test('rejects an incorrect root package version', () => {
  const result = runGuard((lock) => {
    lock.packages['node_modules/vite'].version = '8.1.4'
  })
  const output = `${result.stdout}${result.stderr}`
  assert.equal(result.status, 1, output)
  assert.match(output, /root lock Vite and esbuild versions must match the explicit dependencies/)
})
