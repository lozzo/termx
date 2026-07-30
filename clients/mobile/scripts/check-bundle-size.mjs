import { readdirSync, readFileSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const INITIAL_RAW_LIMIT = 1_550_000
const INITIAL_GZIP_LIMIT = 430_000
const WOFF2_TOTAL_LIMIT = 2_200_000
const distDir = fileURLToPath(new URL('../dist', import.meta.url))
const manifest = JSON.parse(readFileSync(join(distDir, '.vite/manifest.json'), 'utf8'))
const entries = Object.entries(manifest)
const entry = entries.find(([, chunk]) => chunk.isEntry)
if (!entry) fail('Vite manifest has no application entry')

const initialKeys = new Set()
const visitInitial = (key) => {
  if (initialKeys.has(key)) return
  const chunk = manifest[key]
  if (!chunk) fail(`Vite manifest is missing imported chunk ${key}`)
  initialKeys.add(key)
  for (const imported of chunk.imports ?? []) visitInitial(imported)
}
visitInitial(entry[0])

const initialFiles = new Set()
for (const key of initialKeys) {
  const chunk = manifest[key]
  if (chunk.file?.endsWith('.js')) initialFiles.add(chunk.file)
  for (const css of chunk.css ?? []) initialFiles.add(css)
}
const initialBuffers = [...initialFiles].map((file) => readFileSync(join(distDir, file)))
const initialRaw = initialBuffers.reduce((total, buffer) => total + buffer.byteLength, 0)
const initialGzip = initialBuffers.reduce((total, buffer) => total + gzipSync(buffer).byteLength, 0)

const woff2Total = filesBelow(distDir)
  .filter((file) => file.endsWith('.woff2'))
  .reduce((total, file) => total + statSync(file).size, 0)

const fileManager = entries.find(([key]) => key.endsWith('/files/FileManager.tsx'))
if (!fileManager) fail('FileManager is missing from the Vite manifest')
if (!fileManager[1].isDynamicEntry || initialKeys.has(fileManager[0])) {
  fail('FileManager must be a dynamic entry outside the initial closure')
}
if (initialRaw > INITIAL_RAW_LIMIT) fail(`initial raw ${initialRaw} exceeds ${INITIAL_RAW_LIMIT}`)
if (initialGzip > INITIAL_GZIP_LIMIT) fail(`initial gzip ${initialGzip} exceeds ${INITIAL_GZIP_LIMIT}`)
if (woff2Total > WOFF2_TOTAL_LIMIT) fail(`woff2 total ${woff2Total} exceeds ${WOFF2_TOTAL_LIMIT}`)

console.log([
  `mobile bundle: initial ${format(initialRaw)} raw / ${format(initialGzip)} gzip`,
  `woff2 ${format(woff2Total)}`,
  `FileManager dynamic ${fileManager[1].file}`,
].join('; '))

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
