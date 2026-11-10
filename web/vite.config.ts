/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';
import fs from 'fs';

function readVersion(): string {
  try {
    return fs.readFileSync(path.resolve(__dirname, '../VERSION'), 'utf-8').trim();
  } catch {
    return '2.0.0';
  }
}

export default defineConfig(({ mode }) => {
  const isProd = mode === 'production';
  const appVersion = readVersion();

  return {
    plugins: [react()],
    define: {
      __APP_VERSION__: JSON.stringify(appVersion),
      __APP_BUILD_MODE__: JSON.stringify(isProd ? 'production' : 'development'),
      __ENABLE_PREVIEW__: JSON.stringify(!isProd),
    },
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
  };
});
