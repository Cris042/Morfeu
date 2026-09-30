import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Sessão do SPA entre abas (PRD 0032, refinamento E8 T2): o refresh rotativo
// com detecção de reuso não pode derrubar a família quando duas abas
// restauram a sessão ao mesmo tempo (Web Locks + BroadcastChannel).

function contarRefresh(pagina: Page, respostas: number[]) {
  pagina.on('response', (r) => {
    const req = r.request()
    if (req.method() === 'POST' && new URL(r.url()).pathname === '/api/auth/refresh') {
      respostas.push(r.status())
    }
  })
}

test('duas abas restauram a mesma sessão sem revogar a família do refresh', async ({ browser, request }) => {
  const email = `e2e-sessao-${String(Date.now())}@exemplo.com`
  const senha = 'segredo-e2e-123'
  const registro = await request.post('/api/auth/registro', { data: { nome: 'Ana E2E', email, senha } })
  expect(registro.status()).toBe(201)

  // Um contexto = um navegador: as abas compartilham o cookie de refresh.
  const contexto = await browser.newContext()
  const a = await contexto.newPage()
  const b = await contexto.newPage()
  const respostas: number[] = []
  contarRefresh(a, respostas)
  contarRefresh(b, respostas)

  await a.goto('/entrar')
  await a.getByLabel('E-mail').fill(email)
  await a.getByLabel('Senha').fill(senha)
  await a.getByRole('button', { name: 'Entrar' }).click()
  await expect(a.getByRole('heading', { name: 'Meus pedidos' })).toBeVisible()

  // Token só em memória: nada em storage.
  expect(await a.evaluate(() => window.localStorage.length + window.sessionStorage.length)).toBe(0)

  // O boot anônimo antes do login já registrou um 401 (sem cookie): zera.
  respostas.length = 0

  // As duas abas recarregam juntas → cada uma tenta restaurar pelo cookie.
  await Promise.all([a.goto('/conta/pedidos'), b.goto('/conta/pedidos')])
  for (const aba of [a, b]) {
    await expect(aba.getByRole('navigation', { name: 'Conta' })).toContainText('Ana E2E')
    await expect(aba.getByText(/Você ainda não comprou ingressos/)).toBeVisible()
  }
  // Nenhum refresh recusado (reuso = família revogada = 401).
  expect(respostas.length).toBeGreaterThan(0)
  expect(respostas.every((s) => s === 200)).toBe(true)

  // A família segue viva: uma recarga posterior ainda restaura.
  await b.reload()
  await expect(b.getByRole('navigation', { name: 'Conta' })).toContainText('Ana E2E')

  // Logout numa aba desloga a outra (BroadcastChannel) e mata o cookie.
  await a.getByRole('button', { name: 'Sair' }).click()
  await expect(a.getByRole('link', { name: 'Entrar' })).toBeVisible()
  await expect(b.getByRole('link', { name: 'Entrar' })).toBeVisible()
  await b.reload()
  await expect(b.getByRole('link', { name: 'Entrar' })).toBeVisible()
  expect(respostas.at(-1)).toBe(401)

  await contexto.close()
})
