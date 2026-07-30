import { readdirSync, readFileSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const APP_ENTRY_KEY = 'index.html'
const FILE_MANAGER_KEY = '../ui/src/files/FileManager.tsx'
// Transitional baseline with Terminal static and FileManager split; the QR follow-up owns the final budget.
const INITIAL_RAW_BASELINE = 1_887_311
const INITIAL_GZIP_BASELINE = 524_193
const INITIAL_RAW_LIMIT = INITIAL_RAW_BASELINE + 15_000
const INITIAL_GZIP_LIMIT = INITIAL_GZIP_BASELINE + 6_000
const WOFF2_TOTAL_LIMIT = 2_200_000
const distDir = fileURLToPath(new URL('../dist', import.meta.url))
const manifest = JSON.parse(readFileSync(join(distDir, '.vite/manifest.json'), 'utf8'))
const entry = recordFor(APP_ENTRY_KEY)
if (entry.isEntry !== true || entry.src !== APP_ENTRY_KEY) {
  fail(`manifest record ${APP_ENTRY_KEY} is not the application entry`)
}
const fileManager = recordFor(FILE_MANAGER_KEY)
if (fileManager.src !== FILE_MANAGER_KEY || fileManager.isDynamicEntry !== true) {
  fail(`manifest record ${FILE_MANAGER_KEY} is not the FileManager dynamic entry`)
}

const initialKeys = new Set()
const visitInitial = (key) => {
  if (initialKeys.has(key)) return
  const chunk = recordFor(key)
  initialKeys.add(key)
  for (const imported of chunk.imports ?? []) visitInitial(imported)
}
visitInitial(APP_ENTRY_KEY)
if (initialKeys.has(FILE_MANAGER_KEY)) {
  fail('FileManager is in the application initial import closure')
}
const fileManagerPath = findDynamicPath(APP_ENTRY_KEY, FILE_MANAGER_KEY)
if (!fileManagerPath) {
  fail('FileManager is not dynamically reachable from the application entry')
}

const initialFiles = new Set()
for (const key of initialKeys) {
  const chunk = recordFor(key)
  if (chunk.file?.endsWith('.js')) initialFiles.add(chunk.file)
  for (const css of chunk.css ?? []) initialFiles.add(css)
}
const initialBuffers = [...initialFiles].map((file) => readFileSync(join(distDir, file)))
const initialRaw = initialBuffers.reduce((total, buffer) => total + buffer.byteLength, 0)
const initialGzip = initialBuffers.reduce((total, buffer) => total + gzipSync(buffer).byteLength, 0)

const woff2Total = filesBelow(distDir)
  .filter((file) => file.endsWith('.woff2'))
  .reduce((total, file) => total + statSync(file).size, 0)

if (initialRaw > INITIAL_RAW_LIMIT) fail(`initial raw ${initialRaw} exceeds ${INITIAL_RAW_LIMIT}`)
if (initialGzip > INITIAL_GZIP_LIMIT) fail(`initial gzip ${initialGzip} exceeds ${INITIAL_GZIP_LIMIT}`)
if (woff2Total > WOFF2_TOTAL_LIMIT) fail(`woff2 total ${woff2Total} exceeds ${WOFF2_TOTAL_LIMIT}`)

console.log([
  `mobile bundle transition: initial ${format(initialRaw)} raw / ${format(initialGzip)} gzip`,
  `limits ${format(INITIAL_RAW_LIMIT)} raw / ${format(INITIAL_GZIP_LIMIT)} gzip`,
  `woff2 ${format(woff2Total)}`,
  `FileManager path ${fileManagerPath.join(' -> ')} (${fileManager.file})`,
].join('; '))

function recordFor(key) {
  const record = manifest[key]
  if (!record || typeof record !== 'object') fail(`Vite manifest is missing record ${key}`)
  return record
}

function findDynamicPath(start, target) {
  const queue = [{ key: start, path: [start], crossedDynamic: false }]
  const visited = new Set([`${start}:false`])
  while (queue.length > 0) {
    const current = queue.shift()
    const record = recordFor(current.key)
    const edges = [
      ...(record.imports ?? []).map((key) => ({ key, dynamic: false })),
      ...(record.dynamicImports ?? []).map((key) => ({ key, dynamic: true })),
    ]
    for (const edge of edges) {
      recordFor(edge.key)
      const crossedDynamic = current.crossedDynamic || edge.dynamic
      const path = [...current.path, edge.key]
      if (edge.key === target && crossedDynamic) return path
      const visitKey = `${edge.key}:${crossedDynamic}`
      if (visited.has(visitKey)) continue
      visited.add(visitKey)
      queue.push({ key: edge.key, path, crossedDynamic })
    }
  }
  return null
}

function filesBelow(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? filesBelow(path) : [path]
  })
}

function format(value) {
  return value.toLocaleString('en-US')
}

function fail(message) {
  throw new Error(`mobile bundle check failed: ${message}`)
}
