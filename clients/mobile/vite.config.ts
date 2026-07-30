import { copyFileSync, mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { productionRolldownOutput } from './production-minify.mjs'

const require = createRequire(import.meta.url)
const capacitorCore = join(dirname(require.resolve('@capacitor/core')), 'index.js')

export default defineConfig({
  plugins: [react(), removeCapacitorReleaseConsole(), copySharedUiPreviewWorker()],
  build: {
    outDir: 'dist',
    manifest: true,
    minify: 'oxc',
    rolldownOptions: {
      output: productionRolldownOutput(),
    },
  },
})

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
