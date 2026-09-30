import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { mapaFake, ocupadosFake } from './fixture'
import { MapaDeAssentos } from './MapaDeAssentos'

function montar(props: Partial<{ ocupados: string[]; meus: string[]; selecionados: string[] }> = {}) {
  const onAlternar = vi.fn()
  render(
    <MapaDeAssentos
      mapa={mapaFake}
      ocupados={props.ocupados ?? ocupadosFake}
      meus={props.meus ?? []}
      selecionados={props.selecionados ?? []}
      onAlternar={onAlternar}
    />,
  )
  return onAlternar
}

const assento = (nome: RegExp) => screen.getByRole('button', { name: nome })

describe('mapa de assentos', () => {
  it('desenha a grade do layout: fileiras, vãos sem botão, um único tab stop', () => {
    montar()
    expect(screen.getByRole('grid', { name: 'Assentos da Sala 1' })).toBeInTheDocument()
    expect(screen.getAllByRole('row')).toHaveLength(6)
    expect(screen.getAllByRole('button')).toHaveLength(mapaFake.assentos.length)
    expect(document.querySelector('[data-codigo="A5"]')).toBeNull() // corredor
    expect(document.querySelector('[data-codigo="F1"]')).toBeNull() // fileira curta
    expect(document.querySelectorAll('button[tabindex="0"]')).toHaveLength(1)
    expect(assento(/^Fileira A, assento 1, PCD, livre$/)).toBeInTheDocument()
    expect(assento(/^Fileira C, assento 6, ocupado$/)).toHaveAttribute('aria-disabled', 'true')
  })

  it('teclado: Tab entra, setas pulam o corredor, Home/End, ↓ acha o vizinho', async () => {
    montar()
    const u = userEvent.setup()
    await u.tab()
    expect(document.activeElement).toBe(assento(/^Fileira A, assento 1,/))

    await u.keyboard('{ArrowRight}{ArrowRight}{ArrowRight}{ArrowRight}')
    expect(document.activeElement).toBe(assento(/^Fileira A, assento 6,/)) // pulou A5

    await u.keyboard('{End}')
    expect(document.activeElement).toBe(assento(/^Fileira A, assento 10,/))
    await u.keyboard('{Home}')
    expect(document.activeElement).toBe(assento(/^Fileira A, assento 1,/))

    // E1 → F1 é vão: vai ao mais próximo da fileira F (F2).
    await u.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}{ArrowDown}{ArrowDown}')
    expect(document.activeElement).toBe(assento(/^Fileira F, assento 2,/))
    await u.keyboard('{ArrowDown}') // não há fileira abaixo: fica
    expect(document.activeElement).toBe(assento(/^Fileira F, assento 2,/))

    // Roving tabindex acompanha o foco.
    expect(assento(/^Fileira F, assento 2,/)).toHaveAttribute('tabindex', '0')
    expect(document.querySelectorAll('button[tabindex="0"]')).toHaveLength(1)
  })

  it('Enter e Espaço alternam; ocupado não alterna', async () => {
    const onAlternar = montar()
    const u = userEvent.setup()
    await u.tab()
    await u.keyboard('{Enter}')
    await u.keyboard('{ArrowRight}')
    await u.keyboard(' ')
    expect(onAlternar.mock.calls).toEqual([['A1'], ['A2']])

    await u.click(assento(/^Fileira C, assento 6,/))
    expect(onAlternar).toHaveBeenCalledTimes(2)
  })

  it('selecionado e meu com aria-pressed; o meu prevalece sobre ocupado', () => {
    montar({ selecionados: ['B2'], meus: ['C6'] })
    expect(assento(/^Fileira B, assento 2, selecionado$/)).toHaveAttribute('aria-pressed', 'true')
    expect(assento(/^Fileira C, assento 6, reservado para você$/)).toHaveAttribute('aria-pressed', 'true')
    expect(assento(/^Fileira B, assento 3, livre$/)).toHaveAttribute('aria-pressed', 'false')
  })

  it('limite de 6: livres bloqueados, aviso visível, selecionados ainda alternam', async () => {
    const onAlternar = montar({ selecionados: ['B1', 'B2', 'B3', 'B4'], meus: ['A9', 'A10'] })
    expect(screen.getByText('Você já escolheu 6 assentos — o máximo por compra.')).toBeInTheDocument()
    const livre = assento(/^Fileira B, assento 6, indisponível: limite de assentos atingido$/)
    expect(livre).toHaveAttribute('aria-disabled', 'true')
    const u = userEvent.setup()
    await u.click(livre)
    expect(onAlternar).not.toHaveBeenCalled()
    await u.click(assento(/^Fileira B, assento 1, selecionado$/))
    expect(onAlternar).toHaveBeenCalledWith('B1')
  })
})
