import { defineConfig, devices } from '@playwright/test'

// E2E das jornadas críticas (PRD 0021, ADR 0006): Chromium contra a SPA
// servida pelo `vite preview`, que repete o proxy /api → API em :8080
// (a mesma fronteira do Caddy no deploy — ADR 0009).
export default defineConfig({
  testDir: 'e2e',
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  // Polling de 4 s + cache de 3 s do servidor: um estado novo aparece em ≤ 7 s.
  expect: { timeout: 15_000 },
  use: {
    baseURL: 'http://localhost:4173',
    trace: 'retain-on-failure',
    locale: 'pt-BR',
    timezoneId: 'America/Sao_Paulo',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run build && npx vite preview',
    url: 'http://localhost:4173',
    reuseExistingServer: true,
    timeout: 120_000,
  },
})
