import { expect, test } from '@playwright/test'
import type { APIRequestContext } from '@playwright/test'

import { PaginaSessao } from './PaginaSessao'

interface SessaoPublica {
  id: number
  sala_nome: string
}

/** A sessão semeada mais recente (e2e/seed.sql), achada pela API pública. */
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

test('M3: dois navegadores disputam o mesmo assento e só um fica com ele', async ({ browser, request }) => {
  const sessao = await sessaoE2E(request)
  // Dois contextos = dois navegadores = dois carrinhos (cookies HttpOnly distintos).
  const contextoA = await browser.newContext()
  const contextoB = await browser.newContext()
  const a = new PaginaSessao(await contextoA.newPage())
  const b = new PaginaSessao(await contextoB.newPage())
  await a.abrir(sessao.id)
  await b.abrir(sessao.id)

  // B escolhe C4 enquanto ainda está livre…
  await b.alternar('C', 4)
  await expect(b.assento('C', 4)).toHaveAccessibleName(/selecionado$/)

  // …mas A reserva primeiro.
  await a.alternar('C', 4)
  await a.reservar()
  await expect(a.assento('C', 4)).toHaveAccessibleName(/reservado para você$/)

  // B tenta: o servidor recusa, o assento é nomeado e o mapa de B se corrige.
  await b.reservar()
  await expect(b.aviso).toHaveText('O assento C4 acabou de ser reservado por outra pessoa. Escolha outro.')
  await expect(b.assento('C', 4)).toHaveAccessibleName(/ocupado$/)

  // A desiste: B vê o assento voltar pelo polling, sem recarregar a página.
  await a.liberar('C4')
  await expect(b.assento('C', 4)).toHaveAccessibleName(/livre$/)

  // Agora é de B.
  await b.alternar('C', 4)
  await b.reservar()
  await expect(b.assento('C', 4)).toHaveAccessibleName(/reservado para você$/)
  await expect(a.assento('C', 4)).toHaveAccessibleName(/ocupado$/)

  await b.liberar('C4')
  await contextoA.close()
  await contextoB.close()
})

test('caminho feliz: cartaz → filme → escolha de assentos', async ({ page, request }) => {
  const sessao = await sessaoE2E(request)
  await page.goto('/')
  await page.locator('a[href="/filmes/1"]').click()
  await expect(page.getByRole('heading', { level: 2, name: 'Sessões' })).toBeVisible()
  await page.locator(`a[href="/sessoes/${String(sessao.id)}"]`).click()
  await expect(page.getByRole('heading', { name: 'Escolha seus assentos' })).toBeVisible()
  await expect(page.getByRole('grid', { name: new RegExp(`^Assentos da ${sessao.sala_nome}$`) })).toBeVisible()
})
