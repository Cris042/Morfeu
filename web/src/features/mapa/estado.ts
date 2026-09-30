import type { Assento } from '../../api/tipos'

export type EstadoAssento = 'meu' | 'selecionado' | 'ocupado' | 'bloqueado' | 'livre'

export interface Situacao {
  ocupados: ReadonlySet<string>
  meus: ReadonlySet<string>
  selecionados: ReadonlySet<string>
  limiteAtingido: boolean
}

/**
 * Precedência: meu > selecionado > ocupado > bloqueado > livre. Um hold do
 * próprio usuário também aparece na ocupação pública (que não distingue
 * dono) — por isso "meu" vem antes de "ocupado".
 */
export function estadoDoAssento(codigo: string, s: Situacao): EstadoAssento {
  if (s.meus.has(codigo)) {
    return 'meu'
  }
  if (s.selecionados.has(codigo)) {
    return 'selecionado'
  }
  if (s.ocupados.has(codigo)) {
    return 'ocupado'
  }
  return s.limiteAtingido ? 'bloqueado' : 'livre'
}

const DESCRICAO: Record<EstadoAssento, string> = {
  meu: 'reservado para você',
  selecionado: 'selecionado',
  ocupado: 'ocupado',
  bloqueado: 'indisponível: limite de assentos atingido',
  livre: 'livre',
}

/** "Fileira C, assento 7, PCD, ocupado" — o significado vai no rótulo, não na cor. */
export function rotuloDoAssento(a: Assento, estado: EstadoAssento): string {
  const partes = [`Fileira ${a.fileira}`, `assento ${String(a.coluna)}`]
  if (a.pcd) {
    partes.push('PCD')
  }
  partes.push(DESCRICAO[estado])
  return partes.join(', ')
}

/** Pode alternar? Ocupado de outro e bloqueado pelo limite, não. */
export function alternavel(estado: EstadoAssento): boolean {
  return estado === 'livre' || estado === 'selecionado' || estado === 'meu'
}
