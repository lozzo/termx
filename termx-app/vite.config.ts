import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@termx/remote-ui': fileURLToPath(new URL('../remote-ui/src/index.ts', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
  },
})
