import { copyFileSync, mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { productionRolldownOutput } from './production-minify.mjs'

const require = createRequire(import.meta.url)
const capacitorCore = join(dirname(require.resolve('@capacitor/core')), 'index.js')

export default defineConfig({
  plugins: [react(), removeCapacitorReleaseConsole(), assertInitialSourceClosure(), copySharedUiPreviewWorker()],
  build: {
    outDir: 'dist',
    manifest: true,
    minify: 'oxc',
    rolldownOptions: {
      output: productionRolldownOutput(),
    },
  },
})

const forbiddenInitialSources = [
  { label: 'three examples loader', matches: (id: string) => id.includes('/node_modules/three/examples/jsm/loaders/') },
  { label: 'html5-qrcode', matches: (id: string) => id.includes('/node_modules/html5-qrcode/') },
  { label: 'FileManager', matches: (id: string) => id.endsWith('/clients/ui/src/files/FileManager.tsx') },
  { label: 'modelPreviewLoaders.ts', matches: (id: string) => id.endsWith('/modelPreviewLoaders.ts') },
  { label: 'three', matches: (id: string) => id.includes('/node_modules/three/') },
]

function assertInitialSourceClosure(): Plugin {
  return {
    name: 'assert-mobile-initial-source-closure',
    apply: 'build',
    enforce: 'post',
    generateBundle(_options, bundle) {
      const chunkManifest = new Map(
        Object.values(bundle)
          .filter((output) => output.type === 'chunk')
          .map((chunk) => [chunk.fileName, chunk]),
      )
      const entry = [...chunkManifest.values()].find((chunk) => (
        chunk.isEntry && normalizeModuleId(chunk.facadeModuleId ?? '').endsWith('/clients/mobile/index.html')
      ))
      if (!entry) throw new Error('mobile initial source gate could not find the index.html entry chunk')

      const initialFiles = new Set<string>()
      const visitInitial = (fileName: string) => {
        if (initialFiles.has(fileName)) return
        const chunk = chunkManifest.get(fileName)
        if (!chunk) throw new Error(`mobile initial source gate is missing imported chunk ${fileName}`)
        initialFiles.add(fileName)
        for (const imported of chunk.imports) visitInitial(imported)
      }
      visitInitial(entry.fileName)

      for (const fileName of initialFiles) {
        const chunk = chunkManifest.get(fileName)!
        for (const sourceId of Object.keys(chunk.modules)) {
          const normalizedId = normalizeModuleId(sourceId)
          const forbidden = forbiddenInitialSources.find((rule) => rule.matches(normalizedId))
          if (forbidden) {
            throw new Error(`mobile initial source gate found ${forbidden.label} in ${fileName}: ${normalizedId}`)
          }
        }
      }
    },
  }
}

function normalizeModuleId(id: string): string {
  return id.replaceAll('\\', '/').split('?')[0]!.replace(/^\0+/, '')
}

function removeCapacitorReleaseConsole() {
  const diagnostic = 'const handleError = (err) => win.console.error(err);'
  return {
    name: 'remove-capacitor-release-console',
    apply: 'build' as const,
    transform(code: string, id: string) {
      if (id.split('?')[0] !== capacitorCore) return
      const first = code.indexOf(diagnostic)
      if (first < 0 || code.indexOf(diagnostic, first + diagnostic.length) >= 0) {
        throw new Error('unexpected @capacitor/core handleError implementation')
      }
      return code.replace(diagnostic, 'const handleError = () => undefined;')
    },
  }
}

function copySharedUiPreviewWorker() {
  const source = require.resolve('@anytty/ui/preview-worker')
  const target = fileURLToPath(new URL('./dist/anytty-file-preview-sw.js', import.meta.url))
  return {
    name: 'copy-shared-ui-preview-worker',
    apply: 'build' as const,
    closeBundle() {
      mkdirSync(dirname(target), { recursive: true })
      copyFileSync(source, target)
    },
  }
}
