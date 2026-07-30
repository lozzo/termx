import { gzipSync } from 'node:zlib'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const outputDirectory = resolve(import.meta.dirname, '../../controller/apihttp/web')
const manifestPath = resolve(outputDirectory, 'asset-manifest.json')

// 2026-07-31 grouped-route build: initial 404.4/122.9 KiB and total
// 515.7/152.1 KiB (raw/gzip). Raw budgets keep 21-22% headroom; gzip is report-only.
const budgets = {
  initialRaw: 490 * 1024,
  totalRaw: 630 * 1024,
}

function invariant(value, message) {
  if (!value) throw new Error(`Cloud bundle assertion failed: ${message}`)
}

function collectJavaScript(manifest, key, result = new Set(), visited = new Set()) {
  if (visited.has(key)) return result
  visited.add(key)
  const record = manifest[key]
  invariant(record, `manifest import ${key} is missing`)
  if (record.file.endsWith('.js')) result.add(record.file)
  for (const importedKey of record.imports ?? []) collectJavaScript(manifest, importedKey, result, visited)
  return result
}

function sizes(files) {
  return files.reduce((total, file) => {
    const payload = readFileSync(resolve(outputDirectory, file))
    total.raw += payload.byteLength
    total.gzip += gzipSync(payload, { level: 9 }).byteLength
    return total
  }, { raw: 0, gzip: 0 })
}

function checkRawBudget(label, actual, limit) {
  invariant(actual <= limit, `${label} is ${formatSize(actual)}, raw budget is ${formatSize(limit)}`)
}

function formatSize(bytes) {
  return `${(bytes / 1024).toFixed(1)} KiB`
}

const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
const entries = Object.entries(manifest)
const entry = entries.find(([, record]) => record.isEntry)
invariant(entry, 'entry chunk is missing')

const [entryKey, entryRecord] = entry
const initialFiles = collectJavaScript(manifest, entryKey)
const routeGroupKeys = entryRecord.dynamicImports ?? []
const routeGroups = routeGroupKeys.map((key) => {
  const files = [...collectJavaScript(manifest, key)].filter((file) => !initialFiles.has(file))
  return { key, files, ...sizes(files) }
})
const allJavaScript = new Set(entries.map(([, record]) => record.file).filter((file) => file.endsWith('.js')))
const initialSize = sizes([...initialFiles])
const totalSize = sizes([...allJavaScript])

checkRawBudget('initial JavaScript', initialSize.raw, budgets.initialRaw)
checkRawBudget('total JavaScript', totalSize.raw, budgets.totalRaw)

console.log(`Cloud bundle: initial ${formatSize(initialSize.raw)} raw / ${formatSize(initialSize.gzip)} gzip across ${initialFiles.size} requests`)
for (const group of routeGroups) {
  console.log(`Cloud bundle: route group ${group.key} ${formatSize(group.raw)} raw / ${formatSize(group.gzip)} gzip across ${group.files.length} requests`)
}
console.log(`Cloud bundle: total ${formatSize(totalSize.raw)} raw / ${formatSize(totalSize.gzip)} gzip across ${allJavaScript.size} requests`)
