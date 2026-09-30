import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { APIRequestContext, BrowserContext, Page } from '@playwright/test'

import { PaginaSessao } from './PaginaSessao'

// M4 (PRD 0035): cartaz → assento → checkout → pagamento (gateway fake, pelo
// webhook real) → ingresso no e-mail (fake verificável) → link /i/… com QR.
// Roda sob a CSP de produção (vite preview) e com varredura do axe. Compra de
// verdade (D3, D4): cada execução precisa de uma sala nova (web/e2e/seed.sql).

interface SessaoPublica {
  id: number
  sala_nome: string
}

async function sessaoE2E(request: APIRequestContext): Promise<SessaoPublica> {
  const resposta = await request.get('/api/filmes/1/sessoes')
  expect(resposta.ok()).toBe(true)
  const sessoes = ((await resposta.json()) as SessaoPublica[]).filter((s) => s.sala_nome.startsWith('Sala E2E'))
  const ultima = sessoes.sort((a, b) => b.id - a.id)[0]
  if (!ultima) {
    throw new Error('sessão de E2E ausente: rode web/e2e/seed.sql antes')
  }
  return ultima
}

type ComViolacoes = Window & { __violacoesCSP?: string[] }

/** Toda violação de CSP da página vira falha do teste. */
async function vigiarCSP(contexto: BrowserContext) {
  await contexto.addInitScript(() => {
    const w = window as ComViolacoes
    w.__violacoesCSP = []
    document.addEventListener('securitypolicyviolation', (e) => {
      w.__violacoesCSP?.push(`${e.violatedDirective} ${e.blockedURI}`)
    })
  })
}

async function semViolacaoDeCSP(pagina: Page) {
  expect(await pagina.evaluate(() => (window as ComViolacoes).__violacoesCSP ?? ['script de vigia ausente'])).toEqual([])
}

async function semProblemaDeAcessibilidade(pagina: Page) {
  const r = await new AxeBuilder({ page: pagina }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  // id + seletor + resumo: o relatório do CI já diz onde corrigir.
  expect(r.violations.flatMap((v) => v.nodes.map((n) => `${v.id} ${n.target.join(' ')}: ${n.failureSummary ?? ''}`))).toEqual([])
}

/** Do mapa da sessão até "Pagamento confirmado"; devolve o código do pedido. */
async function comprar(pagina: Page, sessaoID: number, fileira: string, numero: number, email?: string) {
  const sala = new PaginaSessao(pagina)
  if (!pagina.url().includes(`/sessoes/${String(sessaoID)}`)) {
    await sala.abrir(sessaoID)
  }
  await sala.alternar(fileira, numero)
  await sala.reservar()
  await expect(sala.assento(fileira, numero)).toHaveAccessibleName(/reservado para você$/)
  await semProblemaDeAcessibilidade(pagina)

  await pagina.getByRole('link', { name: 'Continuar para o pagamento' }).click()
  const campo = pagina.getByLabel('E-mail para receber os ingressos')
  if (email) {
    await campo.fill(email)
  }
  await expect(pagina.getByRole('heading', { name: 'Finalizar compra' })).toBeVisible()
  await semProblemaDeAcessibilidade(pagina)
  await pagina.getByRole('button', { name: 'Ir para o pagamento' }).click()
  await pagina.getByRole('button', { name: 'Pagar (teste)' }).click()

  await expect(pagina.getByRole('heading', { name: 'Pagamento confirmado' })).toBeVisible()
  const codigo = await pagina.getByText(/^[A-Z2-7]{16}$/).textContent()
  if (!codigo) {
    throw new Error('código do pedido ausente')
  }
  return codigo
}

/** O e-mail "enviado" pelo fake da API (rota de teste — só com gateway fake). */
async function linksDoEmail(request: APIRequestContext, para: string): Promise<string[]> {
  let links: string[] = []
  await expect
    .poll(
      async () => {
        const r = await request.get(`/api/__teste/emails?para=${encodeURIComponent(para)}`)
        const emails = (await r.json()) as { links: string[] }[]
        links = emails.flatMap((e) => e.links)
        return links.length
      },
      { message: 'e-mail de confirmação com o link do ingresso' },
    )
    .toBeGreaterThan(0)
  return links
}

test('M4 convidado: compra, recebe o ingresso por e-mail, abre o QR e consulta o pedido', async ({ browser, request }) => {
  const sessao = await sessaoE2E(request)
  const contexto = await browser.newContext()
  await vigiarCSP(contexto)
  const pagina = await contexto.newPage()
  const email = `m4-convidado-${String(Date.now())}@exemplo.com`

  // Cartaz → filme → sessão, como o cliente.
  await pagina.goto('/')
  await semProblemaDeAcessibilidade(pagina)
  await pagina.locator('a[href="/filmes/1"]').click()
  await pagina.locator(`a[href="/sessoes/${String(sessao.id)}"]`).click()
  await expect(pagina.getByRole('heading', { name: 'Escolha seus assentos' })).toBeVisible()

  const codigo = await comprar(pagina, sessao.id, 'D', 3, email)

  // O e-mail traz o link do ingresso; o link abre a página com o QR.
  const [link] = await linksDoEmail(request, email)
  if (!link) {
    throw new Error('e-mail sem link de ingresso')
  }
  await pagina.goto(link)
  const qr = pagina.getByRole('img', { name: 'QR do ingresso do assento D3' })
  await expect(qr).toBeVisible()
  await expect.poll(() => qr.evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
  await semProblemaDeAcessibilidade(pagina)

  // Convidado recupera o pedido por e-mail + código.
  await pagina.goto('/consulta')
  await pagina.getByLabel('E-mail').fill(email.toUpperCase())
  await pagina.getByLabel('Código do pedido').fill(codigo.toLowerCase())
  await pagina.getByRole('button', { name: 'Consultar' }).click()
  await expect(pagina.getByRole('link', { name: 'Ingresso D3' })).toHaveAttribute('href', link)
  await semProblemaDeAcessibilidade(pagina)

  // O assento vendido aparece ocupado para qualquer um.
  const outro = await browser.newContext()
  const sala = new PaginaSessao(await outro.newPage())
  await sala.abrir(sessao.id)
  await expect(sala.assento('D', 3)).toHaveAccessibleName(/ocupado$/)

  await semViolacaoDeCSP(pagina)
  await outro.close()
  await contexto.close()
})

test('M4 conta: logado, o e-mail vem da conta e o pedido aparece em "Meus pedidos"', async ({ browser, request }) => {
  const sessao = await sessaoE2E(request)
  const email = `m4-conta-${String(Date.now())}@exemplo.com`
  const senha = 'segredo-e2e-123'
  expect((await request.post('/api/auth/registro', { data: { nome: 'Bia E2E', email, senha } })).status()).toBe(201)

  const contexto = await browser.newContext()
  await vigiarCSP(contexto)
  const pagina = await contexto.newPage()
  await pagina.goto(`/entrar?volta=/sessoes/${String(sessao.id)}`)
  await pagina.getByLabel('E-mail').fill(email)
  await pagina.getByLabel('Senha').fill(senha)
  await pagina.getByRole('button', { name: 'Entrar' }).click()
  await expect(pagina.getByRole('heading', { name: 'Escolha seus assentos' })).toBeVisible()

  const sala = new PaginaSessao(pagina)
  await sala.alternar('D', 4)
  await sala.reservar()
  await pagina.getByRole('link', { name: 'Continuar para o pagamento' }).click()
  await expect(pagina.getByLabel('E-mail para receber os ingressos')).toHaveValue(email)
  await pagina.getByRole('button', { name: 'Ir para o pagamento' }).click()
  await pagina.getByRole('button', { name: 'Pagar (teste)' }).click()
  await expect(pagina.getByRole('heading', { name: 'Pagamento confirmado' })).toBeVisible()

  await pagina.getByRole('link', { name: 'Meus pedidos' }).click()
  await expect(pagina.getByRole('listitem').filter({ hasText: 'Assentos D4' })).toContainText('Pago')
  expect(await linksDoEmail(request, email)).toHaveLength(1)

  await semViolacaoDeCSP(pagina)
  await contexto.close()
})
