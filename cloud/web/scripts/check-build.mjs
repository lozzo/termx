import { gzipSync } from 'node:zlib'
import { readFileSync, readdirSync, rmSync, statSync } from 'node:fs'
import { resolve } from 'node:path'

const outputDirectory = resolve(import.meta.dirname, '../../controller/apihttp/web')
const manifestPath = resolve(outputDirectory, '.build-manifest.json')

const userPageModules = [
  'src/pages/UserOverviewPage.tsx',
  'src/pages/DevicesPage.tsx',
  'src/pages/UserSubscriptionPage.tsx',
  'src/pages/UserOrdersPage.tsx',
  'src/pages/UserUsagePage.tsx',
  'src/pages/SecurityPage.tsx',
  'src/pages/ForbiddenPage.tsx',
]

const adminPageModules = [
  'src/pages/OverviewPage.tsx',
  'src/pages/EdgesPage.tsx',
  'src/pages/DaemonsPage.tsx',
  'src/pages/ConnectionsPage.tsx',
  'src/pages/AccountsPage.tsx',
  'src/pages/PlansPage.tsx',
  'src/pages/SubscriptionsPage.tsx',
  'src/pages/OrdersPage.tsx',
  'src/pages/CertificatesPage.tsx',
  'src/pages/UsagePage.tsx',
  'src/pages/AuditPage.tsx',
  'src/pages/SystemPage.tsx',
]

// 2026-07-30 build: initial 405.8/124.8 KiB, largest route 41.9/15.2 KiB,
// total 523.1/169.2 KiB (raw/gzip). Budgets retain 12-25% headroom.
const budgets = {
  initialRaw: 460 * 1024,
  initialGzip: 145 * 1024,
  largestRouteRaw: 52 * 1024,
  largestRouteGzip: 19 * 1024,
  totalRaw: 620 * 1024,
  totalGzip: 190 * 1024,
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

function checkBudget(label, actual, limit) {
  invariant(actual <= limit, `${label} is ${formatSize(actual)}, budget is ${formatSize(limit)}`)
}

function formatSize(bytes) {
  return `${(bytes / 1024).toFixed(1)} KiB`
}

try {
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
  const entries = Object.entries(manifest)
  const entry = entries.find(([, record]) => record.isEntry)
  invariant(entry, 'entry chunk is missing')

  const [entryKey, entryRecord] = entry
  const initialFiles = collectJavaScript(manifest, entryKey)
  const initialKeys = new Set([entryKey])
  const visitInitialImports = (key) => {
    for (const importedKey of manifest[key]?.imports ?? []) {
      if (initialKeys.has(importedKey)) continue
      initialKeys.add(importedKey)
      visitInitialImports(importedKey)
    }
  }
  visitInitialImports(entryKey)

  for (const source of ['src/pages/LandingPage.tsx', 'src/pages/LoginPage.tsx']) {
    invariant(!manifest[source]?.isDynamicEntry, `${source} must stay eager`)
  }

  const routeRecords = [...userPageModules, ...adminPageModules].map((source) => {
    const record = manifest[source]
    invariant(record?.isDynamicEntry, `${source} must be a lazy route entry`)
    invariant(record.file !== entryRecord.file, `${source} was bundled into the landing/login entry`)
    invariant(!initialKeys.has(source), `${source} is reachable through static entry imports`)
    return [source, record]
  })

  const adminFiles = new Set(adminPageModules.map((source) => manifest[source].file))
  invariant(adminFiles.size === adminPageModules.length, 'admin page modules must remain independently split')

  const initialSize = sizes([...initialFiles])
  const routeSizes = routeRecords.map(([source]) => {
    const routeFiles = [...collectJavaScript(manifest, source)].filter((file) => !initialFiles.has(file))
    return { source, ...sizes(routeFiles) }
  })
  const largestRouteRaw = Math.max(...routeSizes.map(({ raw }) => raw))
  const largestRouteGzip = Math.max(...routeSizes.map(({ gzip }) => gzip))
  const allJavaScript = readdirSync(outputDirectory).filter((file) => file.endsWith('.js') && statSync(resolve(outputDirectory, file)).isFile())
  const totalSize = sizes(allJavaScript)

  checkBudget('initial JavaScript raw size', initialSize.raw, budgets.initialRaw)
  checkBudget('initial JavaScript gzip size', initialSize.gzip, budgets.initialGzip)
  checkBudget('largest route chunk raw size', largestRouteRaw, budgets.largestRouteRaw)
  checkBudget('largest route chunk gzip size', largestRouteGzip, budgets.largestRouteGzip)
  checkBudget('total JavaScript raw size', totalSize.raw, budgets.totalRaw)
  checkBudget('total JavaScript gzip size', totalSize.gzip, budgets.totalGzip)

  console.log(`Cloud bundle: initial ${formatSize(initialSize.raw)} raw / ${formatSize(initialSize.gzip)} gzip`)
  console.log(`Cloud bundle: largest route ${formatSize(largestRouteRaw)} raw / ${formatSize(largestRouteGzip)} gzip`)
  console.log(`Cloud bundle: total ${formatSize(totalSize.raw)} raw / ${formatSize(totalSize.gzip)} gzip across ${allJavaScript.length} chunks`)
  console.log(`Cloud bundle: ${userPageModules.length} user and ${adminPageModules.length} admin page modules are lazy entries`)
} finally {
  rmSync(manifestPath, { force: true })
}
