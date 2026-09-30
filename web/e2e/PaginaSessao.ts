import { expect } from '@playwright/test'
import type { Locator, Page } from '@playwright/test'

/** Page Object da escolha de assentos (ADR 0006): só role e nome acessível. */
export class PaginaSessao {
  readonly aviso: Locator

  constructor(readonly pagina: Page) {
    this.aviso = pagina.getByRole('alert')
  }

  async abrir(sessaoID: number) {
    await this.pagina.goto(`/sessoes/${String(sessaoID)}`)
    await expect(this.pagina.getByRole('grid')).toBeVisible()
  }

  assento(fileira: string, numero: number): Locator {
    return this.pagina.getByRole('button', { name: new RegExp(`^Fileira ${fileira}, assento ${String(numero)},`) })
  }

  async alternar(fileira: string, numero: number) {
    await this.assento(fileira, numero).click()
  }

  async reservar() {
    await this.pagina.getByRole('button', { name: /^Reservar/ }).click()
  }

  async liberar(codigo: string) {
    await this.pagina.getByRole('button', { name: `Liberar ${codigo}` }).click()
  }
}
