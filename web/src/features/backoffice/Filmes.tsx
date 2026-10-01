import { useState } from 'react'
import type { SubmitEvent } from 'react'

import { buscarNoTMDB, useArquivarFilme, useFilmesBackoffice, useImportarFilme } from '../../api/backoffice'
import type { ResultadoTMDB } from '../../api/backoffice'
import { ErroApi } from '../../api/client'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import styles from './Backoffice.module.css'

function mensagemDoTMDB(erro: unknown): string {
  if (erro instanceof ErroApi && erro.codigo === 'tmdb_indisponivel') {
    return 'O TMDB não respondeu. Tente de novo em instantes.'
  }
  if (erro instanceof ErroApi && erro.codigo === 'tmdb_nao_configurado') {
    return 'A integração com o TMDB não está configurada neste ambiente.'
  }
  if (erro instanceof ErroApi && erro.codigo === 'tmdb_sem_duracao') {
    return 'O TMDB não informa a duração deste filme: não dá para programar sessões com ele.'
  }
  return 'Não foi possível falar com o TMDB agora.'
}

/** Catálogo do operador (PRD 0038): lista, importação do TMDB e arquivamento. */
export function Filmes() {
  const filmes = useFilmesBackoffice()
  const arquivar = useArquivarFilme()
  return (
    <>
      <Importar />
      <h2 className={styles.subtitulo}>Catálogo</h2>
      {filmes.isPending && <Carregando texto="Buscando o catálogo…" />}
      {filmes.isError && <Falha texto="Não foi possível carregar o catálogo." aoTentar={() => void filmes.refetch()} />}
      {filmes.isSuccess && filmes.data.length === 0 && <Vazio>Nenhum filme ainda — importe o primeiro do TMDB.</Vazio>}
      {filmes.isSuccess && filmes.data.length > 0 && (
        <ul className={styles.lista}>
          {filmes.data.map((f) => (
            <li key={f.id} className={styles.item}>
              <span className={styles.nome}>{f.titulo}</span>
              <span className={styles.meta}>
                {[f.ano, f.duracao_min && `${String(f.duracao_min)} min`].filter(Boolean).join(' · ')}
              </span>
              {f.arquivado_em ? (
                <span className={styles.meta}>Arquivado</span>
              ) : (
                <button
                  type="button"
                  className={styles.secundario}
                  disabled={arquivar.isPending}
                  onClick={() => {
                    if (window.confirm(`Arquivar "${f.titulo}"? Ele sai do cartaz.`)) {
                      arquivar.mutate(f.id)
                    }
                  }}
                >
                  Arquivar
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <p className={styles.erro} role="alert">
        {arquivar.isError ? 'Não foi possível arquivar o filme.' : ''}
      </p>
    </>
  )
}

function Importar() {
  const importar = useImportarFilme()
  const [resultados, setResultados] = useState<ResultadoTMDB[]>()
  const [erro, setErro] = useState('')
  const [aviso, setAviso] = useState('')
  const [buscando, setBuscando] = useState(false)

  async function buscar(ev: SubmitEvent<HTMLFormElement>) {
    ev.preventDefault()
    const q = new FormData(ev.currentTarget).get('q')
    setErro('')
    setAviso('')
    setBuscando(true)
    try {
      setResultados((await buscarNoTMDB(typeof q === 'string' ? q : '')).resultados)
    } catch (e) {
      setErro(mensagemDoTMDB(e))
    } finally {
      setBuscando(false)
    }
  }

  async function trazer(r: ResultadoTMDB) {
    setErro('')
    setAviso('')
    try {
      const { criado } = await importar.mutateAsync(r.tmdb_id)
      setAviso(criado ? `"${r.titulo}" entrou no catálogo.` : `"${r.titulo}" já estava no catálogo — dados atualizados.`)
    } catch (e) {
      setErro(mensagemDoTMDB(e))
    }
  }

  return (
    <section aria-labelledby="titulo-importar">
      <h2 id="titulo-importar" className={styles.subtitulo}>
        Importar do TMDB
      </h2>
      <form className={styles.linha} onSubmit={(ev) => void buscar(ev)}>
        <label className={styles.campo}>
          Título
          <input name="q" required minLength={2} maxLength={100} />
        </label>
        <button type="submit" className={styles.primario} disabled={buscando}>
          {buscando ? 'Buscando…' : 'Buscar'}
        </button>
      </form>
      <p className={styles.erro} role="alert">
        {erro}
      </p>
      <p className={styles.aviso} role="status">
        {aviso}
      </p>
      {resultados && resultados.length === 0 && <Vazio>Nada encontrado com esse título.</Vazio>}
      {resultados && resultados.length > 0 && (
        <ul className={styles.lista}>
          {resultados.map((r) => (
            <li key={r.tmdb_id} className={styles.item}>
              <span className={styles.nome}>{r.titulo}</span>
              <span className={styles.meta}>{r.ano}</span>
              <button type="button" className={styles.secundario} disabled={importar.isPending} onClick={() => void trazer(r)}>
                Importar
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
