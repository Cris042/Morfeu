import { describe, expect, it } from 'vitest'

import { alternavel, estadoDoAssento, rotuloDoAssento } from './estado'

const vazio = new Set<string>()

function situacao(parcial: Partial<{ ocupados: string[]; meus: string[]; selecionados: string[]; limiteAtingido: boolean }>) {
  return {
    ocupados: new Set(parcial.ocupados ?? []),
    meus: new Set(parcial.meus ?? []),
    selecionados: new Set(parcial.selecionados ?? []),
    limiteAtingido: parcial.limiteAtingido ?? false,
  }
}

describe('estado do assento', () => {
  it('segue a precedência meu > selecionado > ocupado > bloqueado > livre', () => {
    expect(estadoDoAssento('A1', situacao({ meus: ['A1'], ocupados: ['A1'] }))).toBe('meu')
    expect(estadoDoAssento('A1', situacao({ selecionados: ['A1'], ocupados: ['A1'] }))).toBe('selecionado')
    expect(estadoDoAssento('A1', situacao({ ocupados: ['A1'], limiteAtingido: true }))).toBe('ocupado')
    expect(estadoDoAssento('A1', situacao({ limiteAtingido: true }))).toBe('bloqueado')
    expect(estadoDoAssento('A1', { ocupados: vazio, meus: vazio, selecionados: vazio, limiteAtingido: false })).toBe('livre')
  })

  it('só livre, selecionado e meu alternam', () => {
    expect(alternavel('livre')).toBe(true)
    expect(alternavel('selecionado')).toBe(true)
    expect(alternavel('meu')).toBe(true)
    expect(alternavel('ocupado')).toBe(false)
    expect(alternavel('bloqueado')).toBe(false)
  })

  it('rótulo completo carrega fileira, número, PCD e estado', () => {
    expect(rotuloDoAssento({ codigo: 'C7', fileira: 'C', coluna: 7, pcd: true }, 'ocupado')).toBe(
      'Fileira C, assento 7, PCD, ocupado',
    )
    expect(rotuloDoAssento({ codigo: 'A1', fileira: 'A', coluna: 1, pcd: false }, 'meu')).toBe(
      'Fileira A, assento 1, reservado para você',
    )
  })
})
