import { useMemo, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'

import type { Assento, MapaSessao } from '../../api/tipos'
import { alternavel, estadoDoAssento, rotuloDoAssento } from './estado'
import type { EstadoAssento } from './estado'
import styles from './MapaDeAssentos.module.css'

export const MAXIMO_ASSENTOS = 6

interface Props {
  mapa: MapaSessao
  ocupados: readonly string[]
  meus: readonly string[]
  selecionados: readonly string[]
  maximo?: number
  onAlternar: (codigo: string) => void
}

// Marca visível de cada estado (redundante com a cor — nunca só a cor).
const MARCA: Record<EstadoAssento, string> = {
  meu: '●',
  selecionado: '✓',
  ocupado: '×',
  bloqueado: '',
  livre: '',
}

/** Letras das fileiras da grade (A, B, …) na ordem do layout. */
function letras(n: number): string[] {
  return Array.from({ length: n }, (_, i) => String.fromCharCode(65 + i))
}

/**
 * Mapa de assentos (PRD 0019): componente puro — recebe layout e estados,
 * emite a intenção de alternar. Padrão ARIA grid com roving tabindex.
 */
export function MapaDeAssentos({ mapa, ocupados, meus, selecionados, maximo = MAXIMO_ASSENTOS, onAlternar }: Props) {
  // Assentos por fileira, ordenados por coluna; vãos são posições sem assento.
  const porFileira = useMemo(() => {
    const m = new Map<string, Assento[]>()
    for (const a of mapa.assentos) {
      m.set(a.fileira, [...(m.get(a.fileira) ?? []), a])
    }
    for (const lista of m.values()) {
      lista.sort((x, y) => x.coluna - y.coluna)
    }
    return m
  }, [mapa.assentos])
  const fileiras = useMemo(() => letras(mapa.fileiras).filter((f) => (porFileira.get(f)?.length ?? 0) > 0), [
    mapa.fileiras,
    porFileira,
  ])

  const situacao = {
    ocupados: new Set(ocupados),
    meus: new Set(meus),
    selecionados: new Set(selecionados),
    limiteAtingido: selecionados.length + meus.length >= maximo,
  }

  const primeiro = mapa.assentos.find((a) => !situacao.ocupados.has(a.codigo)) ?? mapa.assentos[0]
  const [foco, setFoco] = useState<string | undefined>(undefined)
  const focado = foco ?? primeiro?.codigo
  const botoes = useRef(new Map<string, HTMLButtonElement>())

  function mover(codigo: string | undefined) {
    if (!codigo) {
      return
    }
    setFoco(codigo)
    botoes.current.get(codigo)?.focus()
  }

  function vizinho(a: Assento, tecla: string): string | undefined {
    const fila = porFileira.get(a.fileira) ?? []
    const i = fila.findIndex((x) => x.codigo === a.codigo)
    switch (tecla) {
      case 'ArrowRight':
        return fila[i + 1]?.codigo
      case 'ArrowLeft':
        return fila[i - 1]?.codigo
      case 'Home':
        return fila[0]?.codigo
      case 'End':
        return fila[fila.length - 1]?.codigo
      case 'ArrowDown':
      case 'ArrowUp': {
        const f = fileiras[fileiras.indexOf(a.fileira) + (tecla === 'ArrowDown' ? 1 : -1)]
        const outra = f ? (porFileira.get(f) ?? []) : []
        // Mesma coluna; senão, o assento mais próximo (empate: o da esquerda).
        let melhor: Assento | undefined
        for (const x of outra) {
          if (!melhor || Math.abs(x.coluna - a.coluna) < Math.abs(melhor.coluna - a.coluna)) {
            melhor = x
          }
        }
        return melhor?.codigo
      }
      default:
        return undefined
    }
  }

  function aoTeclar(evento: KeyboardEvent<HTMLButtonElement>, a: Assento) {
    const alvo = vizinho(a, evento.key)
    if (alvo !== undefined || ['ArrowRight', 'ArrowLeft', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(evento.key)) {
      evento.preventDefault()
      mover(alvo)
    }
  }

  return (
    <div className={styles.mapa}>
      <div className={styles.tela} aria-hidden="true">
        Tela
      </div>
      <div role="grid" aria-label={`Assentos da ${mapa.sala_nome}`} className={styles.grade}>
        {fileiras.map((f) => (
          <div role="row" key={f} className={styles.fileira}>
            <div role="rowheader" className={styles.letra} aria-label={`Fileira ${f}`}>
              {f}
            </div>
            {Array.from({ length: mapa.colunas }, (_, c) => {
              const a = porFileira.get(f)?.find((x) => x.coluna === c + 1)
              if (!a) {
                return <div role="gridcell" key={c} className={styles.vao} />
              }
              const estado = estadoDoAssento(a.codigo, situacao)
              const pode = alternavel(estado)
              return (
                <div role="gridcell" key={c} className={styles.celula}>
                  <button
                    type="button"
                    ref={(el) => {
                      if (el) {
                        botoes.current.set(a.codigo, el)
                      } else {
                        botoes.current.delete(a.codigo)
                      }
                    }}
                    className={[styles.assento, styles[estado], a.pcd ? styles.pcd : undefined].filter(Boolean).join(' ')}
                    tabIndex={a.codigo === focado ? 0 : -1}
                    aria-label={rotuloDoAssento(a, estado)}
                    aria-pressed={estado === 'selecionado' || estado === 'meu'}
                    aria-disabled={pode ? undefined : true}
                    data-codigo={a.codigo}
                    onFocus={() => {
                      setFoco(a.codigo)
                    }}
                    onKeyDown={(e) => {
                      aoTeclar(e, a)
                    }}
                    onClick={() => {
                      if (pode) {
                        onAlternar(a.codigo)
                      }
                    }}
                  >
                    <span aria-hidden="true">{MARCA[estado] || (a.pcd ? '♿' : '')}</span>
                  </button>
                </div>
              )
            })}
          </div>
        ))}
      </div>
      <p className={styles.aviso} aria-live="polite">
        {situacao.limiteAtingido ? `Você já escolheu ${String(maximo)} assentos — o máximo por compra.` : ''}
      </p>
    </div>
  )
}
