/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: path.resolve(__dirname, '../internal/web/dist'),
    emptyOutDir: true,
    // Keep Vite's default content-hashed filenames (assets/[name]-[hash].ext).
    // The Go server serves /assets/* as immutable for a year and index.html as
    // no-cache, so a new build only reaches browsers if the hashes change.
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:49125',
        changeOrigin: true,
      },
      '/current-match': {
        target: 'http://localhost:49125',
        changeOrigin: true,
      },
      '/players': {
        target: 'http://localhost:49125',
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'node',
  },
});
