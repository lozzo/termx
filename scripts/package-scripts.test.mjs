#!/usr/bin/env node

import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, unlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { test } from 'node:test'
import { basename, dirname, join, resolve } from 'node:path'
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

function npmCommand(args, platform = process.platform, comSpec = process.env.ComSpec) {
  if (platform === 'win32') {
    return { command: comSpec ?? 'cmd.exe', args: ['/d', '/s', '/c', 'npm.cmd', ...args] }
  }
  return { command: 'npm', args }
}

function runNpm(args, cwd) {
  const invocation = npmCommand(args)
  const result = spawnSync(invocation.command, invocation.args, { cwd, encoding: 'utf8' })
  if (result.error) throw result.error
  return result
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

    const result = runNpm(['run', lifecycle, '--silent'], fixture)
    assert.equal(result.status, 0, `${lifecycle} fixture failed:\n${result.stdout}${result.stderr}`)
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

function withLintWarningFixture(workspace, callback) {
  const id = `${process.pid}-${randomUUID()}`
  const warningPath = join(repoRoot, workspace.path, `eslint-warning-fixture-${id}.js`)
  const configPath = join(repoRoot, `eslint-warning-fixture-${id}.config.mjs`)
  try {
    writeFileSync(configPath, `
import baseConfig from './eslint.config.mjs'

export default [...baseConfig, {
  files: ['**/${basename(warningPath)}'],
  rules: { 'no-warning-comments': ['warn', { terms: ['TODO'] }] },
}]
`)
    writeFileSync(warningPath, '// TODO: warning fixture\n')
    return { result: callback({ configPath, warningPath }), configPath, warningPath }
  } finally {
    if (existsSync(warningPath)) unlinkSync(warningPath)
    if (existsSync(configPath)) unlinkSync(configPath)
  }
}

test('root workspaces match the UI, Mobile, and Cloud product contract', () => {
  const byPathAndName = (left, right) => `${left.path}\0${left.name}`.localeCompare(`${right.path}\0${right.name}`)
  const expected = [...productWorkspaces].sort(byPathAndName)
  const actual = rootWorkspaces.map(({ path, name }) => ({ path, name })).sort(byPathAndName)
  assert.deepEqual(actual, expected)
})

test('npm runner builds Windows and POSIX command arguments', () => {
  const args = ['run', 'lint', '--workspace', '@anytty/ui']
  assert.deepEqual(npmCommand(args, 'win32', 'C:\\Windows\\System32\\cmd.exe'), {
    command: 'C:\\Windows\\System32\\cmd.exe',
    args: ['/d', '/s', '/c', 'npm.cmd', ...args],
  })
  assert.deepEqual(npmCommand(args, 'linux'), { command: 'npm', args })
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
    const fixture = withLintWarningFixture(workspace, ({ configPath, warningPath }) => runNpm([
      'run',
      'lint',
      '--workspace',
      workspace.name,
      '--',
      '--config',
      configPath,
      warningPath,
    ], repoRoot))
    const { result } = fixture
    const output = `${result.stdout}${result.stderr}`
    assert.equal(result.status, 1, `${workspace.path} lint must fail on a warning:\n${output}`)
    assert.match(output, /no-warning-comments/, `${workspace.path} lint did not report the fixture warning`)
    assert.match(output, /too many warnings/i, `${workspace.path} lint did not fail because of the warning`)
    assert.equal(existsSync(fixture.warningPath), false, `${workspace.path} warning fixture was not removed`)
    assert.equal(existsSync(fixture.configPath), false, `${workspace.path} config fixture was not removed`)
  }
})

test('lint warning fixture removes both files when execution throws', () => {
  let paths
  assert.throws(() => withLintWarningFixture(rootWorkspaces[0], (fixturePaths) => {
    paths = fixturePaths
    throw new Error('fixture execution failed')
  }), /fixture execution failed/)
  assert.equal(existsSync(paths.warningPath), false)
  assert.equal(existsSync(paths.configPath), false)
})
