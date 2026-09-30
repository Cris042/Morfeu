import { useCallback, useState } from 'react'
import { Link, useParams } from 'react-router'

import { ErroApi } from '../../api/client'
import { naoEncontrado, useLiberar, useMapa, useMeusHolds, useOcupacao, useTravar } from '../../api/consultas'
import type { Ocupacao } from '../../api/tipos'
import { Carregando, Falha } from '../../ui/Estado'
import { useAgora } from '../../ui/useAgora'
import { Legenda } from '../mapa/Legenda'
import { MapaDeAssentos, MAXIMO_ASSENTOS } from '../mapa/MapaDeAssentos'
import styles from './PaginaSessao.module.css'
import { SeusAssentos } from './SeusAssentos'

export function PaginaSessao() {
  const { id } = useParams()
  const sessaoID = Number(id)
  if (!Number.isSafeInteger(sessaoID) || sessaoID <= 0) {
    return <SessaoIndisponivel />
  }
  return <Sessao id={sessaoID} />
}

/** Texto do erro da trava, na voz da bilheteria (PRD 0020 RF04). */
function mensagemDoErro(erro: unknown): string {
  if (erro instanceof ErroApi) {
    if (erro.codigo === 'assento_indisponivel') {
      const assentos = (erro.corpo as { assentos?: string[] } | undefined)?.assentos ?? []
      return assentos.length === 1
        ? `O assento ${assentos[0] ?? ''} acabou de ser reservado por outra pessoa. Escolha outro.`
        : `Os assentos ${assentos.join(', ')} acabaram de ser reservados por outra pessoa. Escolha outros.`
    }
    if (erro.codigo === 'limite_holds') {
      return `Você já tem ${String(MAXIMO_ASSENTOS)} assentos reservados — o máximo por compra.`
    }
    if (erro.status === 429) {
      return 'Muitas tentativas seguidas. Espere um minuto e tente de novo.'
    }
    if (erro.status === 404) {
      return 'Esta sessão não está mais disponível.'
    }
  }
  return 'Não conseguimos reservar agora. Tente de novo em instantes.'
}

function recusados(erro: unknown): string[] {
  if (erro instanceof ErroApi && erro.codigo === 'assento_indisponivel') {
    return (erro.corpo as { assentos?: string[] } | undefined)?.assentos ?? []
  }
  return []
}

function Sessao({ id }: { id: number }) {
  const mapa = useMapa(id)
  const ocupacao = useOcupacao(id, mapa.isSuccess)
  const holds = useMeusHolds()
  const refetchHolds = holds.refetch
  // Referência estável: o painel reconsulta uma vez quando um hold vence
  // (auditoria 0020 — antes o efeito rodava a cada render).
  const aoVencer = useCallback(() => {
    void refetchHolds()
  }, [refetchHolds])
  const travar = useTravar(id)
  const liberar = useLiberar(id)

  const [selecionados, setSelecionados] = useState<string[]>([])
  const [aviso, setAviso] = useState('')
  const [ocupacaoVista, setOcupacaoVista] = useState<Ocupacao | undefined>(undefined)

  const meusHolds = (holds.data ?? []).filter((h) => h.sessao_id === id)
  const agora = useAgora(meusHolds.length > 0)
  const vivos = meusHolds.filter((h) => Date.parse(h.expira_em) > agora)
  const meus = vivos.map((h) => h.assento)

  // Selecionado que a ocupação passou a mostrar como de outra pessoa sai da
  // seleção (o servidor é a verdade — RN01). Ajuste de estado na renderização
  // quando chega uma ocupação nova (padrão recomendado pelo React).
  if (ocupacao.data && ocupacao.data !== ocupacaoVista) {
    setOcupacaoVista(ocupacao.data)
    const tomados = selecionados.filter((c) => ocupacao.data.ocupados.includes(c) && !meus.includes(c))
    if (tomados.length > 0) {
      setSelecionados(selecionados.filter((c) => !tomados.includes(c)))
      setAviso(
        tomados.length === 1
          ? `${tomados[0] ?? ''} acabou de ser reservado por outra pessoa.`
          : `${tomados.join(', ')} acabaram de ser reservados por outra pessoa.`,
      )
    }
  }

  if (mapa.isPending) {
    return <Carregando texto="Arrumando a sala…" />
  }
  if (mapa.isError) {
    return naoEncontrado(mapa.error) ? (
      <SessaoIndisponivel />
    ) : (
      <Falha texto="Não conseguimos abrir o mapa da sala." aoTentar={() => void mapa.refetch()} />
    )
  }

  function alternar(codigo: string) {
    setAviso('')
    const hold = vivos.find((h) => h.assento === codigo)
    if (hold) {
      liberar.mutate(hold.id)
      return
    }
    setSelecionados((atual) => (atual.includes(codigo) ? atual.filter((c) => c !== codigo) : [...atual, codigo]))
  }

  function reservar() {
    setAviso('')
    const pedido = [...selecionados]
    travar.mutate(pedido, {
      onSuccess: () => {
        setSelecionados((atual) => atual.filter((c) => !pedido.includes(c)))
      },
      onError: (erro) => {
        const perdidos = recusados(erro)
        setSelecionados((atual) => atual.filter((c) => !perdidos.includes(c)))
        setAviso(mensagemDoErro(erro))
      },
    })
  }

  const n = selecionados.length
  return (
    <section className={styles.sessao} aria-labelledby="titulo-sessao">
      <p className={styles.eyebrow}>{mapa.data.sala_nome}</p>
      <h1 id="titulo-sessao" className={styles.titulo}>
        Escolha seus assentos
      </h1>

      <MapaDeAssentos
        mapa={mapa.data}
        ocupados={ocupacao.data?.ocupados ?? []}
        meus={meus}
        selecionados={selecionados}
        onAlternar={alternar}
      />
      <Legenda />

      <p className={styles.aviso} role="alert">
        {aviso}
      </p>

      <div className={styles.acoes}>
        <button
          type="button"
          className={styles.primario}
          disabled={n === 0 || travar.isPending}
          onClick={reservar}
        >
          {travar.isPending ? 'Reservando…' : n <= 1 ? 'Reservar assento' : `Reservar ${String(n)} assentos`}
        </button>
      </div>

      <SeusAssentos sessaoID={id} holds={meusHolds} agora={agora} aoVencer={aoVencer} />
    </section>
  )
}

function SessaoIndisponivel() {
  return (
    <section>
      <h1>Esta sessão não está mais disponível</h1>
      <p>
        Ela pode ter começado ou sido cancelada. <Link to="/">Voltar ao cartaz</Link>
      </p>
    </section>
  )
}
