import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const localWebTarget = (
  (
    globalThis as typeof globalThis & {
      process?: {
        env?: Record<string, string | undefined>
      }
    }
  ).process?.env?.TERMX_LOCAL_WEB_ORIGIN?.trim()
) || 'http://127.0.0.1:18888'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: localWebTarget,
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        app: new URL('./index.html', import.meta.url).pathname,
        localweb: new URL('./localweb.html', import.meta.url).pathname,
      },
    },
  },
})
