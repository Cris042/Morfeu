import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { PaginaSessao } from './PaginaSessao'

// Backoffice do operador (PRD 0039): sala → sessão → aparece para o público;
// o operador cancela um pedido pago. O operador de E2E é criado no CI pelo
// `seed-operador` (credenciais em E2E_OPERADOR_EMAIL/SENHA); sem elas, pula.
// Assento comprado aqui: D5, numa sala NOVA criada pelo próprio teste (o
// layout modelo tem vão em D6) — sem colisão com M3/M4.

// O projeto não traz os tipos do Node; o spec roda no Node do Playwright.
declare const process: { env: Record<string, string | undefined> }

const email = process.env.E2E_OPERADOR_EMAIL ?? ''
const senha = process.env.E2E_OPERADOR_SENHA ?? ''

async function semProblemaDeAcessibilidade(pagina: Page) {
  const r = await new AxeBuilder({ page: pagina }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.flatMap((v) => v.nodes.map((n) => `${v.id} ${n.target.join(' ')}: ${n.failureSummary ?? ''}`))).toEqual([])
}

/** "AAAA-MM-DDTHH:MM" daqui a `dias` dias, às 15:00 (valor de datetime-local). */
function diaLocal(dias: number): string {
  const d = new Date(Date.now() + dias * 86_400_000)
  return `${d.toISOString().slice(0, 10)}T15:00`
}

test.describe('backoffice do operador', () => {
  test.skip(!email || !senha, 'sem operador de E2E (E2E_OPERADOR_EMAIL/SENHA)')

  test('cria sala e sessão; a sessão aparece para o público; cancela um pedido pago', async ({ browser, request }) => {
    const contexto = await browser.newContext()
    const pagina = await contexto.newPage()
    await pagina.goto('/entrar?volta=/backoffice/salas')
    await pagina.getByLabel('E-mail').fill(email)
    await pagina.getByLabel('Senha').fill(senha)
    await pagina.getByRole('button', { name: 'Entrar' }).click()
    await expect(pagina.getByRole('heading', { name: 'Nova sala' })).toBeVisible()
    await semProblemaDeAcessibilidade(pagina)

    // Sala nova (nome único por execução) com o layout modelo.
    const nomeSala = `Sala Op ${String(Date.now())}`
    await pagina.getByLabel('Nome').fill(nomeSala)
    await pagina.getByRole('button', { name: 'Criar sala' }).click()
    await expect(pagina.getByText(`Sala "${nomeSala}" criada.`)).toBeVisible()

    // Sessão nessa sala daqui a 3 dias.
    await pagina.getByRole('link', { name: 'Sessões' }).click()
    const filme = pagina.getByLabel('Filme', { exact: true })
    await expect(filme.locator('option')).not.toHaveCount(1)
    await filme.selectOption({ index: 1 })
    const filmeID = await filme.inputValue()
    await pagina.getByLabel('Sala', { exact: true }).selectOption({ label: nomeSala })
    await pagina.getByLabel('Início (horário do cinema)').fill(diaLocal(3))
    await pagina.getByLabel('Preço por assento (R$)').fill('30')
    await pagina.getByRole('button', { name: 'Programar sessão' }).click()
    await expect(pagina.getByText('Sessão programada.')).toBeVisible()
    await semProblemaDeAcessibilidade(pagina)

    const publicas = (await (await request.get(`/api/filmes/${filmeID}/sessoes`)).json()) as { id: number; sala_nome: string }[]
    const nova = publicas.find((s) => s.sala_nome === nomeSala)
    expect(nova, 'a sessão criada aparece na lista pública do filme').toBeDefined()

    // Um convidado compra D5 nessa sessão; o operador cancela o pedido.
    const cliente = await browser.newContext()
    const compra = await cliente.newPage()
    const sala = new PaginaSessao(compra)
    await sala.abrir(nova?.id ?? 0)
    await sala.alternar('D', 5)
    await sala.reservar()
    await compra.getByRole('link', { name: 'Continuar para o pagamento' }).click()
    await compra.getByLabel('E-mail para receber os ingressos').fill(`e9-operador-${String(Date.now())}@exemplo.com`)
    await compra.getByRole('button', { name: 'Ir para o pagamento' }).click()
    await compra.getByRole('button', { name: 'Pagar (teste)' }).click()
    await expect(compra.getByRole('heading', { name: 'Pagamento confirmado' })).toBeVisible()
    const pedidoID = new URL(compra.url()).pathname.split('/').pop() ?? ''

    await pagina.goto(`/backoffice/pedidos/${pedidoID}`)
    await expect(pagina.getByText(/^Pago$/)).toBeVisible()
    await pagina.getByRole('button', { name: 'Cancelar pedido' }).click()
    await semProblemaDeAcessibilidade(pagina)
    await pagina.getByRole('button', { name: 'Confirmar cancelamento' }).click()
    await expect(pagina.getByText('Estorno em andamento (cancelado pela operação)')).toBeVisible()

    await cliente.close()
    await contexto.close()
  })
})
