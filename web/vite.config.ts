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
    // O Echo emite o cookie de refresh com Path=/auth/refresh; o navegador o
    // vê em /api/auth/refresh. Sem reescrever o Path, o cookie nunca volta
    // (PRD 0032). Mesma reescrita no Caddy (header_down Set-Cookie) no deploy.
    cookiePathRewrite: { '/auth/refresh': '/api/auth/refresh' },
  },
}

// CSP do Caddy (configs/caddy/seguranca.caddy) repetida no `vite preview`: o
// E2E roda sob a política de produção e falha em securitypolicyviolation
// (PRD 0035). O web-ci confere que as duas cópias são idênticas.
const csp =
  "default-src 'self'; img-src 'self' https://image.tmdb.org https://*.stripe.com; connect-src 'self' https://api.stripe.com; script-src 'self' https://js.stripe.com https://*.js.stripe.com; frame-src https://js.stripe.com https://*.js.stripe.com https://hooks.stripe.com; style-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

export default defineConfig({
  plugins: [react()],
  server: { port: 5173, strictPort: true, proxy },
  // vite preview (E2E da 0020) usa o mesmo proxy.
  preview: { port: 4173, strictPort: true, proxy, headers: { 'Content-Security-Policy': csp } },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'], // e2e/ é do Playwright (PRD 0021)
    setupFiles: ['./src/test/setup.ts'],
    restoreMocks: true,
  },
})
