#!/usr/bin/env node

import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { test } from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const rootPackage = readPackage('package.json')
const productWorkspaces = [
  { path: 'clients/ui', name: '@anytty/ui' },
  { path: 'clients/mobile', name: '@anytty/mobile' },
  { path: 'cloud/web', name: '@anytty/cloud-web' },
]
const rootWorkspaces = rootPackage.workspaces.map((path) => {
  const workspacePackage = readPackage(join(path, 'package.json'))
  return { path, name: workspacePackage.name, package: workspacePackage }
})

function readPackage(relativePath) {
  return JSON.parse(readFileSync(join(repoRoot, relativePath), 'utf8'))
}

function runLifecycleFixture(lifecycle) {
  const fixture = mkdtempSync(join(tmpdir(), 'anytty-package-scripts-'))
  try {
    writeFileSync(join(fixture, 'package.json'), JSON.stringify({
      private: true,
      workspaces: rootPackage.workspaces,
      scripts: { [lifecycle]: rootPackage.scripts[lifecycle] },
    }))
    writeFileSync(join(fixture, 'record-workspace.mjs'), [
      "import { appendFileSync } from 'node:fs'",
      "appendFileSync(new URL('./calls.log', import.meta.url), `${process.argv[2]} ${process.env.npm_package_name}\\n`)",
    ].join('\n'))

    for (const workspace of rootWorkspaces) {
      const workspaceDirectory = join(fixture, workspace.path)
      mkdirSync(workspaceDirectory, { recursive: true })
      writeFileSync(join(workspaceDirectory, 'package.json'), JSON.stringify({
        name: workspace.name,
        version: '0.0.0',
        private: true,
        scripts: { [lifecycle]: `node ../../record-workspace.mjs ${lifecycle}` },
      }))
    }

    execFileSync(process.platform === 'win32' ? 'npm.cmd' : 'npm', ['run', lifecycle, '--silent'], {
      cwd: fixture,
      stdio: 'pipe',
    })
    return readFileSync(join(fixture, 'calls.log'), 'utf8').trim().split('\n').sort()
  } finally {
    rmSync(fixture, { recursive: true, force: true })
  }
}

function requireWorkspaceLifecycle(lifecycle) {
  for (const workspace of rootWorkspaces) {
    assert.equal(
      typeof workspace.package.scripts?.[lifecycle],
      'string',
      `${workspace.path} must expose ${lifecycle}`,
    )
  }
  assert.equal(typeof rootPackage.scripts?.[lifecycle], 'string', `root must expose ${lifecycle}`)
}

test('root workspaces match the UI, Mobile, and Cloud product contract', () => {
  const byPathAndName = (left, right) => `${left.path}\0${left.name}`.localeCompare(`${right.path}\0${right.name}`)
  const expected = [...productWorkspaces].sort(byPathAndName)
  const actual = rootWorkspaces.map(({ path, name }) => ({ path, name })).sort(byPathAndName)
  assert.deepEqual(actual, expected)
})

for (const lifecycle of ['lint', 'typecheck', 'test', 'build']) {
  test(`root ${lifecycle} runs every product workspace exactly once`, () => {
    requireWorkspaceLifecycle(lifecycle)
    const expectedCalls = rootWorkspaces.map(({ name }) => `${lifecycle} ${name}`).sort()
    assert.deepEqual(runLifecycleFixture(lifecycle), expectedCalls)
  })
}

test('workspace lint scripts reject warnings', () => {
  for (const workspace of rootWorkspaces) {
    assert.match(
      workspace.package.scripts.lint,
      /(?:^|\s)--max-warnings(?:=|\s+)0(?:\s|$)/,
      `${workspace.path} lint must set max warnings to zero`,
    )
  }
})
