import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    dedupe: ['react', 'react-dom'],
  },
  server: {
    port: 4177,
    proxy: {
      '/api': { target: 'https://127.0.0.1:18444', secure: false },
    },
  },
  test: {
    server: {
      deps: {
        inline: [/@anytty\/ui/, /@tanstack\/react-query/, /lucide-react/, /react-router/],
      },
    },
  },
  build: {
    outDir: resolve(__dirname, '../controller/apihttp/web'),
    emptyOutDir: true,
    manifest: 'asset-manifest.json',
    // Controller 的严格 CSP 不允许 data: 图片；产品图标必须作为同源静态文件交付。
    assetsInlineLimit: 0,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
})
