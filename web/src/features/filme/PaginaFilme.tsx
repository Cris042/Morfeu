import { Link, useParams } from 'react-router'

import { naoEncontrado, useFilme, useSessoesDoFilme } from '../../api/consultas'
import type { SessaoPublica } from '../../api/tipos'
import { chaveDia, dia, duracao, hora, preco } from '../../ui/formato'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import { Poster } from '../../ui/Poster'
import styles from './PaginaFilme.module.css'

export function PaginaFilme() {
  const { id } = useParams()
  const filmeID = Number(id)
  if (!Number.isSafeInteger(filmeID) || filmeID <= 0) {
    return <ForaDeCartaz />
  }
  return <Filme id={filmeID} />
}

function Filme({ id }: { id: number }) {
  const filme = useFilme(id)
  const sessoes = useSessoesDoFilme(id)

  if (filme.isPending) {
    return <Carregando />
  }
  if (filme.isError) {
    return naoEncontrado(filme.error) ? (
      <ForaDeCartaz />
    ) : (
      <Falha texto="Não conseguimos carregar este filme." aoTentar={() => void filme.refetch()} />
    )
  }

  const f = filme.data
  return (
    <article className={styles.filme}>
      <Poster id={f.id} titulo={f.titulo} url={f.poster_url} grande />
      <div className={styles.corpo}>
        <h1 className={styles.titulo}>{f.titulo}</h1>
        <p className={styles.meta}>
          {[f.ano, f.duracao_min ? duracao(f.duracao_min) : undefined].filter(Boolean).join(' · ')}
        </p>
        {f.sinopse && <p className={styles.sinopse}>{f.sinopse}</p>}

        <section aria-labelledby="titulo-sessoes" className={styles.sessoes}>
          <h2 id="titulo-sessoes" className={styles.subtitulo}>
            Sessões
          </h2>
          {sessoes.isPending && <Carregando texto="Buscando horários…" />}
          {sessoes.isError && (
            <Falha texto="Não conseguimos carregar os horários." aoTentar={() => void sessoes.refetch()} />
          )}
          {sessoes.data?.length === 0 && <Vazio>Sem sessões programadas para este filme.</Vazio>}
          {sessoes.data && sessoes.data.length > 0 && <SessoesPorDia sessoes={sessoes.data} />}
        </section>
      </div>
    </article>
  )
}

function SessoesPorDia({ sessoes }: { sessoes: SessaoPublica[] }) {
  const dias = new Map<string, SessaoPublica[]>()
  for (const s of sessoes) {
    const chave = chaveDia(s.inicio)
    dias.set(chave, [...(dias.get(chave) ?? []), s])
  }
  return (
    <div className={styles.dias}>
      {[...dias.entries()].map(([chave, doDia]) => (
        <div key={chave}>
          <h3 className={styles.rotulo}>{dia(doDia[0]?.inicio ?? chave)}</h3>
          <ul className={styles.chips}>
            {doDia.map((s) => (
              <li key={s.id}>
                <Link
                  to={`/sessoes/${String(s.id)}`}
                  className={styles.chip}
                  aria-label={`Sessão das ${hora(s.inicio)}, ${s.sala_nome}, ${preco(s.preco_centavos)}`}
                >
                  <time dateTime={s.inicio}>{hora(s.inicio)}</time> · {s.sala_nome} · {preco(s.preco_centavos)}
                </Link>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

function ForaDeCartaz() {
  return (
    <section>
      <h1>Este filme não está em cartaz</h1>
      <p>
        Ele pode ter saído de cartaz ou o endereço está incompleto. <Link to="/">Ver o cartaz</Link>
      </p>
    </section>
  )
}
