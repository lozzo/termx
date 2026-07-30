import { copyFileSync, mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { productionMinify } from './production-minify.mjs'

export default defineConfig({
  plugins: [react(), copySharedUiPreviewWorker()],
  build: {
    outDir: 'dist',
    manifest: true,
    minify: 'oxc',
    rolldownOptions: {
      output: {
        minify: productionMinify,
      },
    },
  },
})

function copySharedUiPreviewWorker() {
  const source = createRequire(import.meta.url).resolve('@anytty/ui/preview-worker')
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
