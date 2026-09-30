/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Fronteira do ADR 0009: o navegador chama /api/* e o proxy remove o prefixo —
// o Echo não conhece "/api". Mesma regra no Caddy (handle_path) no deploy.
const api = 'http://localhost:8080' // porta padrão da API (APP_PORT) em dev e no compose

const proxy = {
  '/api': {
    target: api,
    changeOrigin: true,
    rewrite: (caminho: string) => caminho.replace(/^\/api/, ''),
  },
}

export default defineConfig({
  plugins: [react()],
  server: { port: 5173, strictPort: true, proxy },
  // vite preview (E2E da 0020) usa o mesmo proxy.
  preview: { port: 4173, strictPort: true, proxy },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'], // e2e/ é do Playwright (PRD 0021)
    setupFiles: ['./src/test/setup.ts'],
    restoreMocks: true,
  },
})
