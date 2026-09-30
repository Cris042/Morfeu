import { describe, expect, it } from 'vitest'

import { chaveDia, contagem, dia, duracao, hora, preco } from './formato'

describe('formato (fuso do cinema)', () => {
  it('hora em America/Sao_Paulo, qualquer que seja o fuso da máquina', () => {
    expect(hora('2099-10-01T23:30:00Z')).toBe('20:30')
  })

  it('madrugada UTC cai no dia anterior em São Paulo', () => {
    expect(chaveDia('2099-10-02T02:00:00Z')).toBe('2099-10-01')
    expect(hora('2099-10-02T02:00:00Z')).toBe('23:00')
  })

  it('rótulo do dia no locale pt-BR', () => {
    const esperado = new Intl.DateTimeFormat('pt-BR', {
      timeZone: 'America/Sao_Paulo', weekday: 'short', day: 'numeric', month: 'short',
    }).format(new Date('2099-10-01T15:00:00Z'))
    expect(dia('2099-10-01T15:00:00Z')).toBe(esperado)
    expect(dia('2099-10-01T15:00:00Z')).toMatch(/1 de out/)
  })

  it('preço em reais a partir de centavos', () => {
    expect(preco(3200)).toBe('R$ 32,00')
    expect(preco(150)).toBe('R$ 1,50')
  })

  it('duração legível', () => {
    expect(duracao(142)).toBe('2h 22min')
    expect(duracao(120)).toBe('2h')
    expect(duracao(45)).toBe('45min')
  })

  it('contagem mm:ss arredonda para cima e nunca fica negativa', () => {
    expect(contagem(10 * 60 * 1000)).toBe('10:00')
    expect(contagem(9 * 60 * 1000 + 41_000)).toBe('09:41')
    expect(contagem(500)).toBe('00:01')
    expect(contagem(-3000)).toBe('00:00')
  })
})
