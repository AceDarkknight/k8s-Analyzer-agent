import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('react-markdown')) return 'markdown'
            if (id.includes('recharts')) return 'charts'
            if (id.includes('antd') || id.includes('@ant-design/icons')) return 'antd'
            if (id.includes('react-router-dom') || id.includes('react-dom') || id.includes('/react/')) return 'react-vendor'
          }
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/metrics': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
