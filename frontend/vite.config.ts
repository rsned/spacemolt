import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/ws': {
        target: 'ws://localhost:8090',
        ws: true,
      },
      '/api/overmind': {
        target: 'http://localhost:8091',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://localhost:8090',
      },
    },
  },
})
