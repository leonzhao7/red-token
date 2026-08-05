import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    host: true,
    allowedHosts: ['.monkeycode-ai.online'],
    proxy: {
      '/admin/api': 'http://127.0.0.1:4000',
      '/healthz': 'http://127.0.0.1:4000',
      '/v1': 'http://127.0.0.1:4000'
    }
  },
  build: {
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        manualChunks: {
          echarts: ['echarts'],
          vue: ['vue', 'vue-router'],
          lucide: ['lucide-vue-next']
        }
      }
    }
  }
})
