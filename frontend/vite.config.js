import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [
    react(),
  ],

  server: {
    port: 5173,
    strictPort: true, // fail if port is taken instead of trying next port

    proxy: {
      
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,           // enable WebSocket proxying
        changeOrigin: true,
      },
    },
  },

  build: {
    outDir: '../cmd/node/static',
    emptyOutDir: true,    
    sourcemap: false,     
  },

  resolve: {
    alias: {
      '@': '/src',
      },
  },
})