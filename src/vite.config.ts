import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  // Важно: бекенд ожидает статику по пути /ui/
  base: '/ui/',
  build: {
  target: 'esnext',
  outDir: '../static',
      
  emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom', 'react-router-dom'],
          novnc: ['@novnc/novnc']
        }
      }
    }
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true // Разрешаем проксирование WebSocket для VNC
      }
    }
  }
})