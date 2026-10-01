import { useState } from 'react'
import type { SubmitEvent } from 'react'

import {
  listarPedidosOperador,
  PEDIDOS_OPERADOR_POR_PAGINA,
  useCancelarSessao,
  useCriarSessao,
  useFilmesBackoffice,
  useSalas,
  useSessoesBackoffice,
} from '../../api/backoffice'
import type { SessaoBackoffice } from '../../api/backoffice'
import { ErroApi } from '../../api/client'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import { dia, hora, paraUTCDoCinema, preco } from '../../ui/formato'
import styles from './Backoffice.module.css'

function mensagemDe(erro: unknown): string {
  if (erro instanceof ErroApi) {
    switch (erro.codigo) {
      case 'conflito_horario':
        return 'Conflito de horário: a sala já tem sessão nesse intervalo (inclui 20 min de limpeza).'
      case 'filme_indisponivel':
        return 'O filme está arquivado ou sem duração.'
      case 'sala_inexistente':
        return 'A sala não existe mais.'
      case 'dados_invalidos':
        return 'Confira o início (no futuro) e o preço (de R$ 1,00 a R$ 1.000,00).'
      case 'sessao_iniciada':
        return 'A sessão já começou: não pode mais ser cancelada.'
    }
  }
  return 'Não foi possível concluir a operação.'
}

/** Programação do operador (PRD 0039): criar e cancelar sessões. */
export function Sessoes() {
  const sessoes = useSessoesBackoffice()
  const filmes = useFilmesBackoffice()
  const salas = useSalas()
  const titulo = (id: number) => filmes.data?.find((f) => f.id === id)?.titulo ?? `Filme ${String(id)}`
  const sala = (id: number) => salas.data?.find((s) => s.id === id)?.nome ?? `Sala ${String(id)}`
  return (
    <>
      <NovaSessao />
      <h2 className={styles.subtitulo}>Programação</h2>
      {sessoes.isPending && <Carregando texto="Buscando a programação…" />}
      {sessoes.isError && <Falha texto="Não foi possível carregar as sessões." aoTentar={() => void sessoes.refetch()} />}
      {sessoes.isSuccess && sessoes.data.length === 0 && <Vazio>Nenhuma sessão programada.</Vazio>}
      {sessoes.isSuccess && sessoes.data.length > 0 && (
        <ul className={styles.lista}>
          {sessoes.data.map((s) => (
            <LinhaSessao key={s.id} s={s} titulo={titulo(s.filme_id)} sala={sala(s.sala_id)} />
          ))}
        </ul>
      )}
    </>
  )
}

function LinhaSessao({ s, titulo, sala }: { s: SessaoBackoffice; titulo: string; sala: string }) {
  const cancelar = useCancelarSessao()
  // Antes de cancelar, o operador vê quantos pedidos pagos vão para estorno.
  const [afetados, setAfetados] = useState<string>()
  const [erro, setErro] = useState('')
  const [aviso, setAviso] = useState('')

  async function preparar() {
    setErro('')
    try {
      const n = (await listarPedidosOperador({ sessao: s.id, status: 'pago' })).pedidos.length
      setAfetados(n >= PEDIDOS_OPERADOR_POR_PAGINA ? `${String(n)} ou mais` : String(n))
    } catch (e) {
      setErro(mensagemDe(e))
    }
  }

  async function confirmar() {
    setErro('')
    try {
      const r = await cancelar.mutateAsync(s.id)
      setAfetados(undefined)
      setAviso(`Sessão cancelada; ${String(r.pedidos_estornados)} pedido(s) em estorno.`)
    } catch (e) {
      setErro(mensagemDe(e))
    }
  }

  const rotulo = `${titulo} — ${sala}, ${dia(s.inicio)} ${hora(s.inicio)}`
  return (
    <li className={styles.item}>
      <span className={styles.nome}>{titulo}</span>
      <span className={styles.meta}>
        {sala} · {dia(s.inicio)} {hora(s.inicio)} · {preco(s.preco_centavos)}
      </span>
      {s.status === 'cancelada' ? (
        <span className={styles.meta}>Cancelada</span>
      ) : afetados === undefined ? (
        <button type="button" className={styles.secundario} aria-label={`Cancelar sessão ${rotulo}`} onClick={() => void preparar()}>
          Cancelar sessão
        </button>
      ) : (
        <div role="group" aria-label={`Confirmar cancelamento de ${rotulo}`} className={styles.linha}>
          <span>{afetados} pedido(s) pago(s) serão estornados.</span>
          <button type="button" className={styles.primario} disabled={cancelar.isPending} onClick={() => void confirmar()}>
            Confirmar cancelamento
          </button>
          <button type="button" className={styles.secundario} onClick={() => { setAfetados(undefined) }}>
            Manter sessão
          </button>
        </div>
      )}
      <p className={styles.erro} role="alert">
        {erro}
      </p>
      <p className={styles.aviso} role="status">
        {aviso}
      </p>
    </li>
  )
}

function NovaSessao() {
  const filmes = useFilmesBackoffice()
  const salas = useSalas()
  const criar = useCriarSessao()
  const [erro, setErro] = useState('')
  const [aviso, setAviso] = useState('')

  async function enviar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const form = ev.currentTarget
    const d = new FormData(form)
    const texto = (c: string) => {
      const v = d.get(c)
      return typeof v === 'string' ? v : ''
    }
    setErro('')
    setAviso('')
    try {
      await criar.mutateAsync({
        filme_id: Number(texto('filme')),
        sala_id: Number(texto('sala')),
        inicio: paraUTCDoCinema(texto('inicio')),
        preco_centavos: Math.round(Number(texto('preco').replace(',', '.')) * 100),
      })
      setAviso('Sessão programada.')
      form.reset()
    } catch (e) {
      setErro(mensagemDe(e))
    }
  }

  const ativos = filmes.data?.filter((f) => !f.arquivado_em) ?? []
  return (
    <section aria-labelledby="titulo-sessao">
      <h2 id="titulo-sessao" className={styles.subtitulo}>
        Nova sessão
      </h2>
      <form className={styles.formulario} onSubmit={(ev) => void enviar(ev)}>
        <label className={styles.campo}>
          Filme
          <select name="filme" required>
            <option value="">Escolha…</option>
            {ativos.map((f) => (
              <option key={f.id} value={f.id}>
                {f.titulo}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.campo}>
          Sala
          <select name="sala" required>
            <option value="">Escolha…</option>
            {salas.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.nome}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.campo}>
          Início (horário do cinema)
          <input name="inicio" type="datetime-local" required />
        </label>
        <label className={styles.campo}>
          Preço por assento (R$)
          <input name="preco" inputMode="decimal" required pattern="[0-9]+([,.][0-9]{1,2})?" />
        </label>
        <p className={styles.erro} role="alert">
          {erro}
        </p>
        <p className={styles.aviso} role="status">
          {aviso}
        </p>
        <button type="submit" className={styles.primario} disabled={criar.isPending}>
          Programar sessão
        </button>
      </form>
    </section>
  )
}
