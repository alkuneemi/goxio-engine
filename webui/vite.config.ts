import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

const fromRoot = (name: string) => fileURLToPath(new URL(name, import.meta.url))

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // The map benchmark page is not part of the shipped bundle unless BENCH=1.
  ...(process.env.BENCH === '1'
    ? {
        build: {
          sourcemap: true,
          rollupOptions: {
            input: {
              main: fromRoot('./index.html'),
              bench: fromRoot('./bench.html'),
            },
          },
        },
      }
    : {}),
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/websocket': {
        target: 'ws://localhost:8080',
        ws: true,
      },
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
