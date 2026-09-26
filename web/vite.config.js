import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    // 单包曾达 538KB（>500KB 警告）。把框架与图标库拆成独立 chunk：
    // 它们版本变化频繁度远低于业务代码，分开后业务包的每次发布不再让用户重下这部分。
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('lucide')) return 'vendor-icons'
            if (id.includes('@vue') || id.includes('/vue/') || id.includes('vue-router')) return 'vendor-vue'
            return 'vendor'
          }
        }
      }
    },
    chunkSizeWarningLimit: 600
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080'
    }
  }
})
