import path from 'node:path'
import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  // Only used by `npm run dev`: in production m3m0s serves the WebUI
  // same-origin (docs/api-contract.md, section "`/api/v1` prefix and embedded
  // WebUI"), with no proxy. VITE_M3M0S_PROXY_TARGET lives in .env (not
  // hardcoded here) because the local port varies per instance — see
  // webui/.env.example.
  const proxyTarget = env.VITE_M3M0S_PROXY_TARGET || 'http://localhost:19234'

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      proxy: {
        '/api/v1': {
          target: proxyTarget,
          changeOrigin: true,
        },
      },
    },
  }
})
