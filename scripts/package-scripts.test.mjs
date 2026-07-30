#!/usr/bin/env node

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const rootPackage = readPackage('package.json')
const workspaces = [
  { path: 'clients/ui', name: '@anytty/ui' },
  { path: 'clients/mobile', name: '@anytty/mobile' },
  { path: 'cloud/web', name: '@anytty/cloud-web' },
].map((workspace) => ({
  ...workspace,
  package: readPackage(join(workspace.path, 'package.json')),
}))

function readPackage(relativePath) {
  return JSON.parse(readFileSync(join(repoRoot, relativePath), 'utf8'))
}

function workspaceInvocations(command, lifecycle) {
  return command.split(/\s*&&\s*/).map((step) => {
    const actionPattern = lifecycle === 'test'
      ? /^npm\s+(?:run\s+)?test(?:\s|$)/
      : new RegExp(`^npm\\s+run\\s+${lifecycle}(?:\\s|$)`)
    assert.match(step, actionPattern, `root ${lifecycle} contains a non-${lifecycle} command: ${step}`)

    const match = step.match(/(?:^|\s)--workspace(?:=|\s+)([^\s]+)(?:\s|$)/)
    assert.ok(match, `root ${lifecycle} command does not select a workspace: ${step}`)
    return match[1].replace(/^(['"])(.*)\1$/, '$2')
  })
}

for (const lifecycle of ['typecheck', 'test', 'build']) {
  test(`root ${lifecycle} covers every client workspace`, () => {
    for (const workspace of workspaces) {
      assert.equal(workspace.package.name, workspace.name)
      assert.equal(
        typeof workspace.package.scripts?.[lifecycle],
        'string',
        `${workspace.path} must expose ${lifecycle}`,
      )
    }

    const expectedPackages = workspaces.map(({ name }) => name).sort()
    const actualPackages = workspaceInvocations(rootPackage.scripts[lifecycle], lifecycle).sort()
    assert.deepEqual(actualPackages, expectedPackages)
  })
}
