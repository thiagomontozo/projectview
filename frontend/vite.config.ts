/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const apiTarget = process.env.VITE_API_PROXY_TARGET || 'http://localhost:4000';

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': { target: apiTarget, changeOrigin: true },
      '/ws': { target: apiTarget, ws: true, changeOrigin: true }
    }
  },
  build: {
    // Splitting the heaviest third-party code out of the app chunk means a
    // deploy that only touches application code does not invalidate the
    // vendor bundle in everyone's browser cache.
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          charts: ['recharts'],
          query: ['@tanstack/react-query']
        }
      }
    }
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: true,
    // Excluded from coverage: generated or declarative files where a
    // percentage would measure nothing useful.
    coverage: {
      provider: 'v8',
      exclude: ['src/test/**', 'src/i18n/**', '**/*.module.css', '**/*.d.ts']
    }
  }
});
