#!/usr/bin/env node

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { ESLint } from 'eslint'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const eslint = new ESLint({ cwd: repoRoot, overrideConfigFile: join(repoRoot, 'eslint.config.mjs') })

async function lintTypeScript(source) {
  const [result] = await eslint.lintText(source, { filePath: join(repoRoot, 'scripts', 'fixture.ts') })
  return result.messages
}

test('TypeScript declaration forms supported by the language remain valid', async () => {
  const messages = await lintTypeScript(`
function convert(value: string): string
function convert(value: number): number
function convert(value: string | number): string | number {
  return value
}

interface Options { name: string }
interface Options { retries: number }

class Registry {}
namespace Registry { export const kind = 'registry' }

void convert
void Registry.kind
`)

  assert.deepEqual(messages, [])
})

test('unsafe declaration merging and duplicate enum values remain errors', async () => {
  const messages = await lintTypeScript(`
interface UnsafeMerge {}
class UnsafeMerge {}

enum DuplicateValue {
  First = 1,
  Second = 1,
}
`)
  const ruleIds = new Set(messages.map(({ ruleId }) => ruleId))

  assert.ok(ruleIds.has('@typescript-eslint/no-unsafe-declaration-merging'))
  assert.ok(ruleIds.has('@typescript-eslint/no-duplicate-enum-values'))
})
