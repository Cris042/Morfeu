import styles from './Poster.module.css'

// Só o CDN do TMDB entra como imagem (CSP img-src da 0017; hotlink decidido no
// refinamento E2). Qualquer outra URL vira pôster tipográfico (protótipo).
const HOST_TMDB = 'image.tmdb.org'

function urlDoTmdb(url: string | undefined): string | undefined {
  if (!url) {
    return undefined
  }
  try {
    const u = new URL(url)
    return u.protocol === 'https:' && u.hostname === HOST_TMDB ? u.toString() : undefined
  } catch {
    return undefined
  }
}

// Gradientes escuros da paleta, escolhidos pelo id (determinístico). Classes,
// não style inline: a CSP de produção é style-src 'self'.
const FUNDOS = [styles.fundo0, styles.fundo1, styles.fundo2, styles.fundo3]

interface Props {
  id: number
  titulo: string
  url?: string | undefined
  grande?: boolean
}

export function Poster({ id, titulo, url, grande = false }: Props) {
  const classe = [styles.poster, grande ? styles.grande : undefined].filter(Boolean).join(' ')
  const src = urlDoTmdb(url)
  if (src) {
    return <img className={classe} src={src} alt={`Pôster de ${titulo}`} loading="lazy" />
  }
  return (
    <div className={[classe, FUNDOS[id % FUNDOS.length]].filter(Boolean).join(' ')} role="img" aria-label={`Pôster de ${titulo}`}>
      <span className={styles.titulo} aria-hidden="true">
        {titulo}
      </span>
    </div>
  )
}
