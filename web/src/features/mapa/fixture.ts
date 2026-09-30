import type { Assento, MapaSessao } from '../../api/tipos'

// Sala fake (PRD 0019 RF05): 6 × 10, corredor na coluna 5, fileira F mais
// curta (vãos nas pontas), PCD em A1/A2. Mesmo formato de GET /sessoes/{id}/mapa.
const VAOS = new Set(['A5', 'B5', 'C5', 'D5', 'E5', 'F1', 'F5', 'F10'])
const PCD = new Set(['A1', 'A2'])

function assentos(): Assento[] {
  const out: Assento[] = []
  for (const fileira of ['A', 'B', 'C', 'D', 'E', 'F']) {
    for (let coluna = 1; coluna <= 10; coluna++) {
      const codigo = `${fileira}${String(coluna)}`
      if (!VAOS.has(codigo)) {
        out.push({ codigo, fileira, coluna, pcd: PCD.has(codigo) })
      }
    }
  }
  return out
}

export const mapaFake: MapaSessao = {
  sessao_id: 1,
  sala_id: 1,
  sala_nome: 'Sala 1',
  fileiras: 6,
  colunas: 10,
  assentos: assentos(),
}

export const ocupadosFake = ['C6', 'C7', 'D3', 'E8']
