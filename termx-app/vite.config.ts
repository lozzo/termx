import { copyFileSync, mkdirSync } from 'node:fs'
import { dirname } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react(), copyRemoteUiPreviewWorker()],
  resolve: {
    alias: {
      '@termx/remote-ui': fileURLToPath(new URL('../remote-ui/src/index.ts', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
  },
})

function copyRemoteUiPreviewWorker() {
  const source = fileURLToPath(new URL('../remote-ui/public/termx-file-preview-sw.js', import.meta.url))
  const target = fileURLToPath(new URL('./dist/termx-file-preview-sw.js', import.meta.url))
  return {
    name: 'copy-remote-ui-preview-worker',
    apply: 'build' as const,
    closeBundle() {
      mkdirSync(dirname(target), { recursive: true })
      copyFileSync(source, target)
    },
  }
}
