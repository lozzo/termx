import { copyFileSync, mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react(), copySharedUiPreviewWorker()],
  build: {
    outDir: 'dist',
  },
})

function copySharedUiPreviewWorker() {
  const source = createRequire(import.meta.url).resolve('@muxvia/ui/preview-worker')
  const target = fileURLToPath(new URL('./dist/muxvia-file-preview-sw.js', import.meta.url))
  return {
    name: 'copy-shared-ui-preview-worker',
    apply: 'build' as const,
    closeBundle() {
      mkdirSync(dirname(target), { recursive: true })
      copyFileSync(source, target)
    },
  }
}
