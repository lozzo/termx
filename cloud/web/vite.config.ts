import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    // Cloud Web 使用 React Router 8 所需的 React patch；所有 hoisted 依赖必须解析到同一实例。
    alias: {
      react: resolve(__dirname, 'node_modules/react'),
      'react-dom': resolve(__dirname, 'node_modules/react-dom'),
    },
  },
  server: {
    port: 4177,
    proxy: {
      '/api': { target: 'https://127.0.0.1:18444', secure: false },
    },
  },
  build: {
    outDir: resolve(__dirname, '../controller/apihttp/web'),
    emptyOutDir: true,
    // Controller 的严格 CSP 不允许 data: 图片；产品图标必须作为同源静态文件交付。
    assetsInlineLimit: 0,
    rollupOptions: {
      output: {
        entryFileNames: 'app.js',
        chunkFileNames: 'chunk-[name].js',
        assetFileNames: (asset) => asset.names.some((name) => name.endsWith('.css')) ? 'styles.css' : 'asset-[name][extname]',
      },
    },
  },
})
