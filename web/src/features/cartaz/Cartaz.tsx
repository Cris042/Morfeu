import { Link } from 'react-router'

import { useFilmes } from '../../api/consultas'
import { duracao } from '../../ui/formato'
import { Carregando, Falha, Vazio } from '../../ui/Estado'
import { Poster } from '../../ui/Poster'
import styles from './Cartaz.module.css'

export function Cartaz() {
  const filmes = useFilmes()

  return (
    <section aria-labelledby="titulo-cartaz">
      <p className={styles.eyebrow}>Em cartaz</p>
      <h1 id="titulo-cartaz" className={styles.titulo}>
        Escolha o filme da noite
      </h1>
      {filmes.isPending && <Carregando />}
      {filmes.isError && (
        <Falha texto="Não conseguimos carregar o cartaz." aoTentar={() => void filmes.refetch()} />
      )}
      {filmes.data?.length === 0 && <Vazio>Nenhum filme em cartaz agora. Volte mais tarde.</Vazio>}
      {filmes.data && filmes.data.length > 0 && (
        <ul className={styles.grade}>
          {filmes.data.map((f) => (
            <li key={f.id}>
              <Link to={`/filmes/${String(f.id)}`} className={styles.filme}>
                <Poster id={f.id} titulo={f.titulo} url={f.poster_url} />
                <h2 className={styles.nome}>{f.titulo}</h2>
                <p className={styles.meta}>
                  {[f.ano, f.duracao_min ? duracao(f.duracao_min) : undefined].filter(Boolean).join(' · ')}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
