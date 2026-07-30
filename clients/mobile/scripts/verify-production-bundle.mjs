#!/usr/bin/env node

import { readdirSync, readFileSync } from 'node:fs'
import { dirname, extname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { minifySync, parseSync } from 'rolldown/utils'
import { productionMinify } from '../production-minify.mjs'

const mobileRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distRoot = join(mobileRoot, 'dist')
const forbiddenText = ['camera scan failed', 'pair claim failed', 'terminal-inventory']
const scripts = collectScripts(distRoot)

if (scripts.length === 0) throw new Error('production bundle contains no JavaScript')
for (const path of scripts) {
  const source = readFileSync(path, 'utf8')
  assertNoDirectConsoleCalls(path, source)
  for (const marker of forbiddenText) {
    if (source.includes(marker)) fail(path, JSON.stringify(marker))
  }
}
assertDropConsoleRemovesArgumentEvaluation()

console.log(`Production mobile JavaScript contract passed (${scripts.length} files)`)

function collectScripts(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return collectScripts(path)
    return entry.isFile() && extname(entry.name) === '.js' ? [path] : []
  })
}

function fail(path, marker) {
  throw new Error(`production JavaScript contains ${marker}: ${relative(mobileRoot, path)}`)
}

function assertNoDirectConsoleCalls(path, source) {
  const parsed = parseSync(path, source, { lang: 'js', sourceType: 'module' })
  if (parsed.errors.length !== 0) throw new Error(`could not parse production JavaScript: ${relative(mobileRoot, path)}`)
  visit(parsed.program, (node) => {
    if (node.type !== 'CallExpression') return
    const callee = node.callee
    if (callee?.type === 'MemberExpression' && callee.object?.type === 'Identifier' && callee.object.name === 'console') {
      fail(path, 'direct console call')
    }
  })
}

function assertDropConsoleRemovesArgumentEvaluation() {
  const marker = '__anyttyDropConsoleSideEffect'
  const result = minifySync(
    'drop-console-fixture.js',
    `globalThis.__anyttyFixtureKept = true; console.error(globalThis.${marker}())`,
    { ...productionMinify, module: true },
  )
  if (result.errors.length !== 0 || result.code.includes(marker)) {
    throw new Error('Oxc dropConsole did not remove console argument evaluation')
  }
  assertNoDirectConsoleCalls('drop-console-fixture.js', result.code)
}

function visit(value, callback) {
  if (Array.isArray(value)) {
    value.forEach((entry) => visit(entry, callback))
    return
  }
  if (value === null || typeof value !== 'object') return
  if (typeof value.type === 'string') callback(value)
  for (const child of Object.values(value)) visit(child, callback)
}
