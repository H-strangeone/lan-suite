import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

/*
  CONCEPT: vite.config.js
  ────────────────────────
  Vite reads this file at startup to configure the dev server and build.
  defineConfig() is just a helper that gives you TypeScript autocomplete
  even in a .js file — it's a no-op at runtime.

  What each section does:

  plugins: []
    Vite's plugin system. @vitejs/plugin-react does two things:
    1. Transforms JSX → React.createElement() calls via Babel
    2. Enables HMR (Hot Module Replacement) — React components update
       in the browser WITHOUT a full page reload when you save a file.
       State is preserved during HMR. This is why Vite dev is so fast.

  server: {}
    Dev server settings. Only active during `npm run dev`.
    port: 5173 is Vite's default. Change if you need to.
    proxy: forwards /api and /ws requests to your Go backend.
    Without proxy: browser would block the request (different port = CORS).
    With proxy: browser talks to :5173, Vite forwards to :8080 silently.
    This is the cleanest way to avoid CORS issues in development.

  build: {}
    Production build settings. Active during `npm run build`.
    outDir: where the compiled files go. Go server will serve these.
    sourcemap: true in dev so browser DevTools shows original JSX source,
    not the compiled JavaScript.

  resolve: {}
    Path aliases. Instead of ../../components/Button
    you write @/components/Button. Cleaner imports.
*/

export default defineConfig({
  plugins: [
    react(),
    /*
      @vitejs/plugin-react uses Babel to transform JSX.
      If you want faster transforms, use @vitejs/plugin-react-swc instead
      (SWC is a Rust-based JS compiler, ~20x faster than Babel).
      Both are drop-in compatible. For now, the default Babel version is fine.
    */
  ],

  server: {
    port: 5173,
    strictPort: true, // fail if port is taken instead of trying next port

    proxy: {
      /*
        CONCEPT: Dev proxy
        ───────────────────
        When your React code calls fetch('/api/auth'), Vite intercepts it
        and forwards it to http://localhost:8080/api/auth.
        The browser sees it as a same-origin request — no CORS needed.
        In production, Nginx/Caddy handles this (real reverse proxy).
        In development, Vite handles it (fake reverse proxy).
      */
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        // changeOrigin: rewrites the Host header to match the target.
        // Without it, the Go server sees Host: localhost:5173 which might
        // confuse virtual-host-based routing.
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
    /*
      We put the build output inside cmd/node/static so the Go server
      can serve it with http.FileServer. One binary, no separate static hosting.
      In production: go run ./cmd/node serves both API and frontend.
    */
    emptyOutDir: true,    // clear old files before each build
    sourcemap: false,     // disable in production (don't leak source code)
  },

  resolve: {
    alias: {
      '@': '/src',
      /*
        Now you can write:
          import { ws } from '@/lib/ws'
        instead of:
          import { ws } from '../../lib/ws'
        Works regardless of how deeply nested the importing file is.
      */
    },
  },
})